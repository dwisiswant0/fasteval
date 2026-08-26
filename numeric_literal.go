package fasteval

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const bitsPerHexDigit = 4

// maxConstantOperationBits limits the memory used by exact constant folding.
const maxConstantOperationBits = 1 << 20

type constantSizeLimitError struct {
	operation string
}

func (l numberLiteral) exactValue() constant.Value {
	if l.value != nil {
		return l.value
	}

	if value, ok := simpleDecimalConstant(l.text); ok {
		return value
	}

	return constant.MakeFromLiteral(l.text, gotoken.FLOAT, 0)
}

func (l numberLiteral) constantKind() constant.Kind {
	if l.value == nil {
		return constant.Float
	}

	return l.value.Kind()
}

func (l numberLiteral) simpleDecimalInteger() (uint64, bool) {
	if l.value != nil {
		return 0, false
	}

	mantissa, fractionalDigits, ok := scanSimpleDecimal(l.text)
	if !ok {
		return 0, false
	}

	denominator := uint64(1)
	for range fractionalDigits {
		denominator *= decimalRadix
	}

	if mantissa%denominator != 0 {
		return 0, false
	}

	return mantissa / denominator, true
}

func (e *constantSizeLimitError) Error() string {
	return fmt.Sprintf("constant %s exceeds the %d-bit size limit", e.operation, maxConstantOperationBits)
}

func materializeLiteral(value any, span Span) (any, error) {
	literal, ok := value.(numberLiteral)
	if !ok {
		return normalizeValue(value), nil
	}

	if literal.defaultOK {
		return literal.defaultValue, nil
	}

	switch literal.constantKind() {
	case constant.Int:
		return materializeIntegerLiteral(literal, span)
	case constant.Float:
		return materializeFloatLiteral(literal, span)
	case constant.Complex:
		return materializeComplexLiteral(literal, span)
	case constant.Unknown, constant.Bool, constant.String:
		return nil, evalError(
			ErrType, "materialize numeric literal", span, "",
			newDetailError("invalid literal %s", literal.text),
		)
	}

	return nil, evalError(ErrType, "materialize numeric literal", span, "", newDetailError("unknown literal kind"))
}

func materializeIntegerLiteral(literal numberLiteral, span Span) (any, error) {
	integer, exact := constant.Int64Val(literal.exactValue())
	if !exact || int64(int(integer)) != integer {
		return nil, evalError(
			ErrOverflow, "materialize integer literal", span, "",
			newDetailError("%s does not fit in int", literal.text),
		)
	}

	return int(integer), nil
}

func materializeFloatLiteral(literal numberLiteral, span Span) (any, error) {
	floating, _ := constant.Float64Val(literal.exactValue())
	if math.IsInf(floating, 0) {
		return nil, evalError(
			ErrOverflow, "materialize floating-point literal", span, "",
			newDetailError("%s does not fit in float64", literal.text),
		)
	}

	return floating, nil
}

func materializeComplexLiteral(literal numberLiteral, span Span) (any, error) {
	exactValue := literal.exactValue()
	realPart, _ := constant.Float64Val(constant.Real(exactValue))
	imaginaryPart, _ := constant.Float64Val(constant.Imag(exactValue))

	if math.IsInf(realPart, 0) || math.IsInf(imaginaryPart, 0) {
		return nil, evalError(
			ErrOverflow, "materialize complex literal", span, "",
			newDetailError("%s does not fit in complex128", literal.text),
		)
	}

	return complex(realPart, imaginaryPart), nil
}

func literalNumericOperation(operator string, left, right numberLiteral) (numberLiteral, bool, error) {
	if (operator == "/" || operator == "%") && constant.Sign(right.exactValue()) == 0 {
		return emptyNumberLiteral(), false, nil
	}

	switch operator {
	case "**":
		return literalPower(left, right)
	case "<<", ">>":
		return literalShift(operator, left, right)
	default:
		return literalBasicOperation(operator, left, right)
	}
}

func literalPower(left, right numberLiteral) (numberLiteral, bool, error) {
	leftValue, rightValue := left.exactValue(), right.exactValue()
	exponent := constant.ToInt(rightValue)

	count, exact := constant.Uint64Val(exponent)
	if !exact {
		if leftValue.Kind() != constant.Int || rightValue.Kind() != constant.Int {
			return emptyNumberLiteral(), false, nil
		}

		return emptyNumberLiteral(), true, newDetailError("integer exponent must be non-negative and fit in uint64")
	}

	if literalPowerExceedsSizeLimit(leftValue, count) {
		return emptyNumberLiteral(), true, &constantSizeLimitError{operation: "power"}
	}

	result := constant.MakeInt64(1)
	base := leftValue

	for count > 0 {
		if count&1 != 0 {
			result = constant.BinaryOp(result, gotoken.MUL, base)
		}

		count >>= 1
		if count > 0 {
			base = constant.BinaryOp(base, gotoken.MUL, base)
		}
	}

	return makeNumberLiteral(result, left.text+"**"+right.text), true, nil
}

func literalShift(operator string, left, right numberLiteral) (numberLiteral, bool, error) {
	leftValue := left.exactValue()
	countValue := constant.ToInt(right.exactValue())

	count, exact := constant.Uint64Val(countValue)
	if !exact {
		return emptyNumberLiteral(), true, newDetailError("shift count must be non-negative and fit in uint64")
	}

	if leftValue.Kind() != constant.Int {
		return emptyNumberLiteral(), true, newDetailError("shifted operand must be an integer")
	}

	bitLength := constant.BitLen(leftValue)
	if operator == "<<" && (count > maxConstantOperationBits ||
		uint64(bitLength)+count > maxConstantOperationBits) { //nolint:gosec // constant.BitLen returns a non-negative int.
		return emptyNumberLiteral(), true, &constantSizeLimitError{operation: "shift"}
	}

	if operator == ">>" && count >= uint64(bitLength) { //nolint:gosec // constant.BitLen returns a non-negative int.
		return saturatedRightShift(left, right), true, nil
	}

	if count > uint64(^uint(0)) {
		return emptyNumberLiteral(), true, newDetailError("shift count must fit in uint")
	}

	direction := gotoken.SHL
	if operator == ">>" {
		direction = gotoken.SHR
	}

	result := constant.Shift(leftValue, direction, uint(count))

	return makeNumberLiteral(result, left.text+operator+right.text), true, nil
}

func saturatedRightShift(left, right numberLiteral) numberLiteral {
	result := constant.MakeInt64(0)
	if constant.Sign(left.exactValue()) < 0 {
		result = constant.MakeInt64(-1)
	}

	return makeNumberLiteral(result, left.text+">>"+right.text)
}

func literalPowerExceedsSizeLimit(value constant.Value, count uint64) bool {
	if count == 0 {
		return false
	}

	if constant.Compare(value, gotoken.EQL, constant.MakeInt64(0)) ||
		constant.Compare(value, gotoken.EQL, constant.MakeInt64(1)) ||
		constant.Compare(value, gotoken.EQL, constant.MakeInt64(-1)) {
		return false
	}

	if count > maxConstantOperationBits {
		return true
	}

	bitsPerFactor := uint64(literalComponentBitLen(value) + 1) //nolint:gosec // The helper returns a non-negative size.

	return bitsPerFactor > maxConstantOperationBits/count
}

func literalComponentBitLen(value constant.Value) int {
	switch value.Kind() {
	case constant.Int:
		return constant.BitLen(value)
	case constant.Float:
		return literalFloatBitLen(value)
	case constant.Complex:
		return max(literalComponentBitLen(constant.Real(value)), literalComponentBitLen(constant.Imag(value)))
	case constant.Unknown, constant.Bool, constant.String:
		return 0
	}

	return 0
}

func literalFloatBitLen(value constant.Value) int {
	exact := value.ExactString()

	exponentOffset := strings.LastIndexByte(exact, 'p')

	if exponentOffset < 0 {
		return len(exact) * bitsPerHexDigit
	}

	exponent, err := strconv.ParseInt(exact[exponentOffset+1:], 10, 64)
	if err != nil {
		return maxConstantOperationBits + 1
	}

	if exponent < 0 {
		exponent = -exponent
	}

	mantissaBits := 0

	for _, character := range exact[:exponentOffset] {
		if character >= '0' && character <= '9' || character >= 'a' && character <= 'f' {
			mantissaBits += 4
		}
	}

	if exponent > int64(maxConstantOperationBits) {
		return maxConstantOperationBits + 1
	}

	return int(exponent) + mantissaBits
}

func literalBasicOperation(operator string, left, right numberLiteral) (numberLiteral, bool, error) {
	token, supported := literalOperatorToken(operator)
	if !supported {
		return emptyNumberLiteral(), false, nil
	}

	handled, err := validateLiteralBasicOperands(operator, left, right)
	if err != nil || !handled {
		return emptyNumberLiteral(), handled, err
	}

	result := constant.BinaryOp(left.exactValue(), token, right.exactValue())
	if literalComponentBitLen(result) > maxConstantOperationBits {
		return emptyNumberLiteral(), true, &constantSizeLimitError{operation: operator}
	}

	return makeNumberLiteral(result, left.text+operator+right.text), true, nil
}

func literalOperatorToken(operator string) (gotoken.Token, bool) {
	switch operator {
	case "+":
		return gotoken.ADD, true
	case "-":
		return gotoken.SUB, true
	case "*":
		return gotoken.MUL, true
	case "/":
		return gotoken.QUO, true
	case "%":
		return gotoken.REM, true
	case "&":
		return gotoken.AND, true
	case "|":
		return gotoken.OR, true
	case "^":
		return gotoken.XOR, true
	default:
		return gotoken.ILLEGAL, false
	}
}

func validateLiteralBasicOperands(operator string, left, right numberLiteral) (bool, error) {
	integers := left.constantKind() == constant.Int && right.constantKind() == constant.Int
	if operator == "%" && !integers {
		return false, nil
	}

	bitwise := operator == "&" || operator == "|" || operator == "^"
	if bitwise && !integers {
		return true, newDetailError("operator requires integer operands")
	}

	return true, nil
}

func coerceNumbers(left, right any) (any, any, error) {
	leftLiteral, leftIsLiteral := left.(numberLiteral)

	rightLiteral, rightIsLiteral := right.(numberLiteral)

	if leftIsLiteral && rightIsLiteral {
		return coerceLiteralPair(leftLiteral, rightLiteral)
	}

	if leftIsLiteral {
		converted, err := convertLiteralLike(leftLiteral, right)
		if err != nil {
			return nil, nil, err
		}

		return converted, right, nil
	}

	if rightIsLiteral {
		converted, err := convertLiteralLike(rightLiteral, left)
		if err != nil {
			return nil, nil, err
		}

		return left, converted, nil
	}

	return nil, nil, newDetailError("numeric operands must have the same concrete type, got %T and %T", left, right)
}

func coerceLiteralPair(left, right numberLiteral) (any, any, error) {
	target := reflect.TypeFor[int]()
	if left.constantKind() == constant.Complex || right.constantKind() == constant.Complex {
		target = reflect.TypeFor[complex128]()
	} else if left.constantKind() == constant.Float || right.constantKind() == constant.Float {
		target = reflect.TypeFor[float64]()
	}

	leftValue, err := convertNumberLiteral(left, target)
	if err != nil {
		return nil, nil, err
	}

	rightValue, err := convertNumberLiteral(right, target)
	if err != nil {
		return nil, nil, err
	}

	return leftValue.Interface(), rightValue.Interface(), nil
}

func convertLiteralLike(literal numberLiteral, exemplar any) (any, error) {
	switch exemplar.(type) {
	case int:
		if value, ok := literal.defaultValue.(int); ok {
			return value, nil
		}
	case float64:
		if literal.float64OK {
			return literal.float64Box, nil
		}
	case complex128:
		if literal.complexOK {
			return literal.complexBox, nil
		}
	}

	converted, err := convertNumberLiteral(literal, reflect.TypeOf(exemplar))
	if err != nil {
		return nil, err
	}

	return converted.Interface(), nil
}

func convertNumberLiteral(literal numberLiteral, target reflect.Type) (reflect.Value, error) {
	if target == nil {
		return reflect.Value{}, newDetailError("numeric target type is nil")
	}

	if literal.defaultOK && reflect.TypeOf(literal.defaultValue) == target {
		return reflect.ValueOf(literal.defaultValue), nil
	}

	if cached, ok := cachedNumberLiteral(literal, target); ok {
		return cached, nil
	}

	value := reflect.New(target).Elem()

	return convertNumberLiteralValue(literal, target, value)
}

func cachedNumberLiteral(literal numberLiteral, target reflect.Type) (reflect.Value, bool) {
	if target.PkgPath() != "" {
		return reflect.Value{}, false
	}

	switch target.Kind() {
	case reflect.Float64:
		return reflect.ValueOf(literal.float64Value), literal.float64OK
	case reflect.Float32:
		if literal.float32OK {
			return reflect.ValueOf(literal.float32Value), true
		}

		value, _ := constant.Float32Val(constant.ToFloat(literal.exactValue()))

		return reflect.ValueOf(value), literal.float64OK && !math.IsInf(float64(value), 0)
	case reflect.Complex128:
		if literal.complexOK {
			return reflect.ValueOf(literal.complexBox), true
		}

		return reflect.Value{}, false
	case reflect.Complex64:
		value, valid := complex64LiteralValue(literal)

		return reflect.ValueOf(value), valid
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return reflect.Value{}, false
}

func convertNumberLiteralValue(
	literal numberLiteral,
	target reflect.Type,
	value reflect.Value,
) (reflect.Value, error) {
	switch target.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return convertLiteralSigned(literal, target, value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return convertLiteralUnsigned(literal, target, value)
	case reflect.Float32:
		return convertLiteralFloat32(literal, value)
	case reflect.Float64:
		return convertLiteralFloat64(literal, value)
	case reflect.Complex64, reflect.Complex128:
		return convertLiteralComplex(literal, target, value)
	case reflect.Invalid, reflect.Bool, reflect.Array, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String,
		reflect.Struct, reflect.UnsafePointer:
		return reflect.Value{}, newDetailError("target %v is not numeric", target)
	}

	return value, nil
}

func convertLiteralSigned(literal numberLiteral, target reflect.Type, value reflect.Value) (reflect.Value, error) {
	if number, ok := literal.simpleDecimalInteger(); ok && number <= math.MaxInt64 {
		integer := int64(number)
		if !value.OverflowInt(integer) {
			value.SetInt(integer)

			return value, nil
		}
	}

	integer := constant.ToInt(literal.exactValue())
	if integer.Kind() == constant.Unknown {
		return reflect.Value{}, newDetailError("literal %s is not an integer", literal.text)
	}

	number, exact := constant.Int64Val(integer)
	if !exact || value.OverflowInt(number) {
		return reflect.Value{}, newDetailError("literal %s does not fit in %v", literal.text, target)
	}

	value.SetInt(number)

	return value, nil
}

func convertLiteralUnsigned(literal numberLiteral, target reflect.Type, value reflect.Value) (reflect.Value, error) {
	if number, ok := literal.simpleDecimalInteger(); ok && !value.OverflowUint(number) {
		value.SetUint(number)

		return value, nil
	}

	integer := constant.ToInt(literal.exactValue())
	if integer.Kind() == constant.Unknown || constant.Sign(integer) < 0 {
		return reflect.Value{}, newDetailError("literal %s is not a non-negative integer", literal.text)
	}

	number, exact := constant.Uint64Val(integer)
	if !exact || value.OverflowUint(number) {
		return reflect.Value{}, newDetailError("literal %s does not fit in %v", literal.text, target)
	}

	value.SetUint(number)

	return value, nil
}

func convertLiteralFloat32(literal numberLiteral, value reflect.Value) (reflect.Value, error) {
	if literal.float32OK {
		value.SetFloat(float64(literal.float32Value))

		return value, nil
	}

	realValue := constant.ToFloat(literal.exactValue())
	if realValue.Kind() == constant.Unknown {
		return reflect.Value{}, newDetailError("literal %s is not real", literal.text)
	}

	number, _ := constant.Float32Val(realValue)
	if math.IsInf(float64(number), 0) {
		return reflect.Value{}, newDetailError("literal %s does not fit in float32", literal.text)
	}

	value.SetFloat(float64(number))

	return value, nil
}

func convertLiteralFloat64(literal numberLiteral, value reflect.Value) (reflect.Value, error) {
	realValue := constant.ToFloat(literal.exactValue())
	if realValue.Kind() == constant.Unknown {
		return reflect.Value{}, newDetailError("literal %s is not real", literal.text)
	}

	number, _ := constant.Float64Val(realValue)
	if math.IsInf(number, 0) {
		return reflect.Value{}, newDetailError("literal %s does not fit in float64", literal.text)
	}

	value.SetFloat(number)

	return value, nil
}

func convertLiteralComplex(literal numberLiteral, target reflect.Type, value reflect.Value) (reflect.Value, error) {
	if target.Kind() == reflect.Complex64 {
		number, valid := complex64LiteralValue(literal)
		if !valid {
			return reflect.Value{}, newDetailError("literal %s does not fit in %v", literal.text, target)
		}

		value.SetComplex(complex128(number))

		return value, nil
	}

	exactValue := literal.exactValue()
	realPart, _ := constant.Float64Val(constant.ToFloat(constant.Real(exactValue)))
	imaginaryPart, _ := constant.Float64Val(constant.ToFloat(constant.Imag(exactValue)))

	number := complex(realPart, imaginaryPart)
	if math.IsInf(real(number), 0) || math.IsInf(imag(number), 0) {
		return reflect.Value{}, newDetailError("literal %s does not fit in %v", literal.text, target)
	}

	value.SetComplex(number)

	return value, nil
}

func complex64LiteralValue(literal numberLiteral) (complex64, bool) {
	exactValue := literal.exactValue()
	realPart, _ := constant.Float32Val(constant.ToFloat(constant.Real(exactValue)))
	imaginaryPart, _ := constant.Float32Val(constant.ToFloat(constant.Imag(exactValue)))

	valid := !math.IsInf(float64(realPart), 0) && !math.IsInf(float64(imaginaryPart), 0)

	return complex(realPart, imaginaryPart), valid
}

func makeNumberLiteral(value constant.Value, text string) numberLiteral {
	literal := emptyNumberLiteral()
	literal.value = value
	literal.text = text

	switch value.Kind() {
	case constant.Int:
		populateIntegerLiteral(&literal, value)
	case constant.Float:
		populateFloatLiteral(&literal, value)
	case constant.Complex:
		populateComplexLiteral(&literal, value)
	case constant.Unknown, constant.Bool, constant.String:
	}

	return literal
}

func makeSimpleDecimalLiteral(text string) (numberLiteral, bool) {
	if len(text) > maxSimpleDecimalDigits+1 {
		return emptyNumberLiteral(), false
	}

	if _, _, ok := scanSimpleDecimal(text); !ok {
		return emptyNumberLiteral(), false
	}

	floating, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(floating, 0) {
		return emptyNumberLiteral(), false
	}

	literal := emptyNumberLiteral()
	literal.text = text
	populateRealLiteral(&literal, floating)
	literal.defaultValue, literal.defaultOK = literal.float64Box, true

	narrow, narrowErr := strconv.ParseFloat(text, 32)
	if narrowErr == nil && !math.IsInf(narrow, 0) {
		literal.float32Value, literal.float32OK = float32(narrow), true
	}

	return literal, true
}

func populateIntegerLiteral(literal *numberLiteral, value constant.Value) {
	integer, exact := constant.Int64Val(value)
	unsigned, unsignedExact := constant.Uint64Val(value)

	floating := integerLiteralFloat64(value, integer, exact, unsigned, unsignedExact)
	if !math.IsInf(floating, 0) {
		populateRealLiteral(literal, floating)
	}

	if exact && int64(int(integer)) == integer {
		literal.defaultValue = int(integer)
		literal.defaultOK = true
	}
}

func integerLiteralFloat64(
	value constant.Value,
	signed int64,
	signedOK bool,
	unsigned uint64,
	unsignedOK bool,
) float64 {
	if signedOK {
		return float64(signed)
	}

	if unsignedOK {
		return float64(unsigned)
	}

	floating, _ := constant.Float64Val(constant.ToFloat(value))

	return floating
}

func populateFloatLiteral(literal *numberLiteral, value constant.Value) {
	floating, _ := constant.Float64Val(value)
	if math.IsInf(floating, 0) {
		return
	}

	populateRealLiteral(literal, floating)
	literal.defaultValue = literal.float64Box
	literal.defaultOK = true
}

func populateRealLiteral(literal *numberLiteral, floating float64) {
	literal.float64Value, literal.float64OK = floating, true
	literal.float64Box = any(floating)
	literal.complexOK = true
	literal.complexBox = any(complex(floating, 0))
}

func populateComplexLiteral(literal *numberLiteral, value constant.Value) {
	realPart, _ := constant.Float64Val(constant.Real(value))
	imaginaryPart, _ := constant.Float64Val(constant.Imag(value))

	if math.IsInf(realPart, 0) || math.IsInf(imaginaryPart, 0) {
		return
	}

	complexValue := complex(realPart, imaginaryPart)
	literal.complexOK = true
	literal.complexBox = any(complexValue)
	literal.defaultValue = literal.complexBox
	literal.defaultOK = true
}

func isNumericType(typeOf reflect.Type) bool {
	if typeOf == nil {
		return false
	}

	switch typeOf.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	case reflect.Invalid, reflect.Bool, reflect.Array, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String,
		reflect.Struct, reflect.UnsafePointer:
		return false
	}

	return false
}
