package fasteval

import (
	"errors"
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"math/cmplx"
	"reflect"
	"time"

	"go.dw1.io/safemath"
)

func unaryOperation(operator string, operand any, span Span) (any, error) {
	if operator == "!" {
		boolean, ok := operand.(bool)
		if !ok {
			return nil, evalError(ErrType, "evaluate !", span, "", newDetailError("operand has type %T, want bool", operand))
		}

		return !boolean, nil
	}

	if literal, ok := operand.(numberLiteral); ok {
		return unaryLiteral(operator, literal, span)
	}

	operand = normalizeValue(operand)

	return unaryNormalized(operator, operand, span)
}

func unaryLiteral(operator string, literal numberLiteral, span Span) (any, error) {
	switch operator {
	case "+":
		return literal, nil
	case "-":
		return makeNumberLiteral(constant.UnaryOp(gotoken.SUB, literal.exactValue(), 0), "-"+literal.text), nil
	case "~":
		if literal.constantKind() != constant.Int {
			return nil, evalError(ErrType, "evaluate ~", span, "", newDetailError("operand must be an integer"))
		}

		return makeNumberLiteral(constant.UnaryOp(gotoken.XOR, literal.exactValue(), 0), "~"+literal.text), nil
	default:
		return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("invalid numeric prefix"))
	}
}

func unaryNormalized(operator string, operand any, span Span) (any, error) {
	reflected := reflect.ValueOf(operand)
	if !reflected.IsValid() {
		return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("operand is nil"))
	}

	switch {
	case isSignedKind(reflected.Kind()):
		return unarySigned(operator, operand, span)
	case isUnsignedKind(reflected.Kind()):
		return unaryUnsigned(operator, operand, span)
	default:
		return unaryReal(operator, operand, span)
	}
}

func unarySigned(operator string, operand any, span Span) (any, error) {
	switch value := operand.(type) {
	case int:
		return unarySignedInteger(operator, value, span)
	case int8:
		return unarySignedInteger(operator, value, span)
	case int16:
		return unarySignedInteger(operator, value, span)
	case int32:
		return unarySignedInteger(operator, value, span)
	case int64:
		return unarySignedInteger(operator, value, span)
	case time.Duration:
		return unarySignedInteger(operator, value, span)
	}

	return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("operand has type %T", operand))
}

func unaryUnsigned(operator string, operand any, span Span) (any, error) {
	switch value := operand.(type) {
	case uint:
		return unaryUnsignedInteger(operator, value, span)
	case uint8:
		return unaryUnsignedInteger(operator, value, span)
	case uint16:
		return unaryUnsignedInteger(operator, value, span)
	case uint32:
		return unaryUnsignedInteger(operator, value, span)
	case uint64:
		return unaryUnsignedInteger(operator, value, span)
	case uintptr:
		return unaryUnsignedInteger(operator, value, span)
	}

	return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("operand has type %T", operand))
}

func unaryReal(operator string, operand any, span Span) (any, error) {
	switch value := operand.(type) {
	case float32:
		return unaryPlusMinus(operator, value, span)
	case float64:
		return unaryPlusMinus(operator, value, span)
	case complex64:
		return unaryPlusMinus(operator, value, span)
	case complex128:
		return unaryPlusMinus(operator, value, span)
	}

	return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("operand has type %T", operand))
}

func unaryPlusMinus[T float32 | float64 | complex64 | complex128](operator string, value T, span Span) (any, error) {
	switch operator {
	case "+":
		return value, nil
	case "-":
		return -value, nil
	default:
		return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("invalid numeric prefix"))
	}
}

func unarySignedInteger[T safemath.Signed](operator string, value T, span Span) (any, error) {
	switch operator {
	case "+":
		return value, nil
	case "~":
		return ^value, nil
	case "-":
		result, err := safemath.Neg(value)
		if err != nil {
			return nil, evalError(ErrOverflow, "evaluate -", span, "", err)
		}

		return result, nil
	default:
		return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("invalid integer prefix"))
	}
}

func unaryUnsignedInteger[T safemath.Unsigned](operator string, value T, span Span) (any, error) {
	switch operator {
	case "+":
		return value, nil
	case "~":
		return ^value, nil
	case "-":
		return nil, evalError(
			ErrType, "evaluate -", span, "",
			newDetailError("cannot negate unsigned type %T", value),
		)
	default:
		return nil, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("invalid integer prefix"))
	}
}

func binaryNumericOrString(operator string, left, right any, span Span) (any, error) {
	if leftValue, leftOK := left.(float64); leftOK {
		if rightValue, rightOK := right.(float64); rightOK {
			if operator == "<<" || operator == ">>" {
				return numericShift(operator, left, right, span)
			}

			return floatOperation(operator, leftValue, rightValue, span)
		}
	}

	return genericNumericOrString(operator, left, right, span)
}

func genericNumericOrString(operator string, left, right any, span Span) (any, error) {
	if value, handled, err := concatenateStrings(operator, left, right, span); handled {
		return value, err
	}

	if value, handled, err := operateLiterals(operator, left, right, span); handled {
		return value, err
	}

	if operator == "<<" || operator == ">>" {
		return numericShift(operator, left, right, span)
	}

	if value, matched, err := sameTypeNumericOperation(operator, left, right, span); matched {
		return value, err
	}

	left, right, err := coerceNumbers(left, right)
	if err != nil {
		return nil, evalError(ErrType, "evaluate "+operator, span, "", err)
	}

	value, matched, operationErr := sameTypeNumericOperation(operator, left, right, span)
	if !matched {
		return nil, evalError(
			ErrType, "evaluate "+operator, span, "",
			newDetailError("operands have unsupported type %T", left),
		)
	}

	return value, operationErr
}

func numericShift(operator string, left, right any, span Span) (any, error) {
	if literal, ok := left.(numberLiteral); ok {
		converted, err := convertNumberLiteral(literal, reflect.TypeOf(right))
		if err != nil {
			return nil, evalError(ErrType, "evaluate "+operator, span, "", err)
		}

		left = converted.Interface()
	}

	count, err := numericShiftCount(right)
	if err != nil {
		return nil, evalError(ErrType, "evaluate "+operator, span, "", err)
	}

	typeOf := reflect.TypeOf(left)
	if typeOf != nil && isSignedKind(typeOf.Kind()) {
		return signedIndependentShift(operator, left, count, span)
	}

	if typeOf != nil && isUnsignedKind(typeOf.Kind()) {
		return unsignedIndependentShift(operator, left, count, span)
	}

	return nil, evalError(
		ErrType, "evaluate "+operator, span, "",
		newDetailError("shifted operand has type %T, want integer", left),
	)
}

func signedIndependentShift(operator string, left any, count uint64, span Span) (any, error) {
	switch value := left.(type) {
	case int:
		return checkedIndependentShift(operator, value, count, span)
	case int8:
		return checkedIndependentShift(operator, value, count, span)
	case int16:
		return checkedIndependentShift(operator, value, count, span)
	case int32:
		return checkedIndependentShift(operator, value, count, span)
	case int64:
		return checkedIndependentShift(operator, value, count, span)
	default:
		return nil, newDetailError("shifted operand has type %T, want signed integer", left)
	}
}

func unsignedIndependentShift(operator string, left any, count uint64, span Span) (any, error) {
	switch value := left.(type) {
	case uint:
		return checkedIndependentShift(operator, value, count, span)
	case uint8:
		return checkedIndependentShift(operator, value, count, span)
	case uint16:
		return checkedIndependentShift(operator, value, count, span)
	case uint32:
		return checkedIndependentShift(operator, value, count, span)
	case uint64:
		return checkedIndependentShift(operator, value, count, span)
	case uintptr:
		return checkedIndependentShift(operator, value, count, span)
	default:
		return nil, newDetailError("shifted operand has type %T, want unsigned integer", left)
	}
}

func numericShiftCount(value any) (uint64, error) {
	if literal, ok := value.(numberLiteral); ok {
		count, exact := constant.Uint64Val(constant.ToInt(literal.exactValue()))
		if !exact {
			return 0, newDetailError("shift count must be non-negative and fit in uint64")
		}

		return count, nil
	}

	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return 0, newDetailError("shift count is nil, want integer")
	}

	if _, duration := value.(time.Duration); duration {
		return 0, newDetailError("shift count has type %T, want integer", value)
	}

	if isSignedKind(reflected.Kind()) {
		count := reflected.Int()
		if count < 0 {
			return 0, newDetailError("count cannot be negative")
		}

		return uint64(count), nil
	}

	if isUnsignedKind(reflected.Kind()) {
		return reflected.Uint(), nil
	}

	return 0, newDetailError("shift count has type %T, want integer", value)
}

func checkedIndependentShift[T safemath.Integer](
	operator string,
	left T,
	count uint64,
	span Span,
) (T, error) {
	value, err := integerShiftCount(operator, left, count)
	if err == nil {
		return value, nil
	}

	code := ErrType
	if errors.Is(err, safemath.ErrOverflow) {
		code = ErrOverflow
	}

	return 0, evalError(code, "evaluate "+operator, span, "", err)
}

func concatenateStrings(operator string, left, right any, span Span) (any, bool, error) {
	if operator != "+" {
		return nil, false, nil
	}

	leftString, leftOK := left.(string)

	rightString, rightOK := right.(string)
	if !leftOK && !rightOK {
		return nil, false, nil
	}

	if !leftOK || !rightOK {
		return nil, true, evalError(
			ErrType, "evaluate +", span, "",
			newDetailError("string concatenation requires two strings, got %T and %T", left, right),
		)
	}

	return leftString + rightString, true, nil
}

func operateLiterals(operator string, left, right any, span Span) (any, bool, error) {
	leftLiteral, leftOK := left.(numberLiteral)

	rightLiteral, rightOK := right.(numberLiteral)
	if !leftOK || !rightOK {
		return nil, false, nil
	}

	value, handled, err := literalNumericOperation(operator, leftLiteral, rightLiteral)
	if err != nil {
		return nil, handled, evalError(ErrType, "evaluate "+operator, span, "", err)
	}

	return value, handled, nil
}

func sameTypeNumericOperation(operator string, left, right any, span Span) (any, bool, error) {
	reflected := reflect.ValueOf(left)
	if !reflected.IsValid() {
		return nil, false, nil
	}

	switch {
	case isSignedKind(reflected.Kind()):
		return sameSignedOperation(operator, left, right, span)
	case isUnsignedKind(reflected.Kind()):
		return sameUnsignedOperation(operator, left, right, span)
	default:
		return sameRealOperation(operator, left, right, span)
	}
}

func sameSignedOperation(operator string, left, right any, span Span) (any, bool, error) {
	switch leftValue := left.(type) {
	case int:
		return sameIntegerOperation(operator, leftValue, right, span)
	case int8:
		return sameIntegerOperation(operator, leftValue, right, span)
	case int16:
		return sameIntegerOperation(operator, leftValue, right, span)
	case int32:
		return sameIntegerOperation(operator, leftValue, right, span)
	case int64:
		return sameIntegerOperation(operator, leftValue, right, span)
	}

	return nil, false, nil
}

func sameUnsignedOperation(operator string, left, right any, span Span) (any, bool, error) {
	switch leftValue := left.(type) {
	case uint:
		return sameIntegerOperation(operator, leftValue, right, span)
	case uint8:
		return sameIntegerOperation(operator, leftValue, right, span)
	case uint16:
		return sameIntegerOperation(operator, leftValue, right, span)
	case uint32:
		return sameIntegerOperation(operator, leftValue, right, span)
	case uint64:
		return sameIntegerOperation(operator, leftValue, right, span)
	case uintptr:
		return sameIntegerOperation(operator, leftValue, right, span)
	}

	return nil, false, nil
}

func sameIntegerOperation[T safemath.Integer](operator string, left T, right any, span Span) (any, bool, error) {
	rightValue, ok := right.(T)
	if !ok {
		return nil, false, nil
	}

	value, err := integerOperation(operator, left, rightValue, span)

	return value, true, err
}

func sameRealOperation(operator string, left, right any, span Span) (any, bool, error) {
	switch leftValue := left.(type) {
	case float32:
		rightValue, ok := right.(float32)
		if ok {
			value, err := floatOperation(operator, leftValue, rightValue, span)

			return value, true, err
		}
	case float64:
		rightValue, ok := right.(float64)
		if ok {
			value, err := floatOperation(operator, leftValue, rightValue, span)

			return value, true, err
		}
	case complex64:
		rightValue, ok := right.(complex64)
		if ok {
			value, err := complexOperation(operator, leftValue, rightValue, span)

			return value, true, err
		}
	case complex128:
		rightValue, ok := right.(complex128)
		if ok {
			value, err := complexOperation(operator, leftValue, rightValue, span)

			return value, true, err
		}
	}

	return nil, false, nil
}

func integerOperation[T safemath.Integer](operator string, left, right T, span Span) (T, error) {
	value, err := rawIntegerOperation(operator, left, right)
	if err == nil {
		return value, nil
	}

	var code ErrorCode

	switch {
	case errors.Is(err, safemath.ErrOverflow):
		code = ErrOverflow
	case errors.Is(err, safemath.ErrDivisionByZero):
		code = ErrDivisionByZero
	default:
		code = ErrType
	}

	return 0, evalError(code, "evaluate "+operator, span, "", err)
}

func rawIntegerOperation[T safemath.Integer](operator string, left, right T) (T, error) {
	switch operator {
	case "+", "-", "*", "/":
		return checkedIntegerOperation(operator, left, right)
	case "%":
		value, err := safemath.Mod(left, right)
		if err != nil {
			return 0, fmt.Errorf("integer remainder: %w", err)
		}

		return value, nil
	case "**":
		return integerPower(left, right)
	case "&", "|", "^":
		return integerBitwise(operator, left, right), nil
	case "<<", ">>":
		return integerShift(operator, left, right)
	default:
		return 0, newDetailError("operator is not valid for integers")
	}
}

func checkedIntegerOperation[T safemath.Integer](operator string, left, right T) (T, error) {
	var (
		value T
		err   error
	)

	switch operator {
	case "+":
		value, err = safemath.Add(left, right)
	case "-":
		value, err = safemath.Sub(left, right)
	case "*":
		value, err = safemath.Mul(left, right)
	case "/":
		value, err = safemath.Div(left, right)
	default:
		return 0, newDetailError("operator is not a checked integer operation")
	}

	if err != nil {
		return 0, fmt.Errorf("checked integer %s: %w", operator, err)
	}

	return value, nil
}

func integerBitwise[T safemath.Integer](operator string, left, right T) T {
	switch operator {
	case "&":
		return left & right
	case "|":
		return left | right
	case "^":
		return left ^ right
	default:
		return 0
	}
}

func integerShift[T safemath.Integer](operator string, left, right T) (T, error) {
	shift, err := shiftCount(right)
	if err != nil {
		return 0, err
	}

	return integerShiftCount(operator, left, shift)
}

func integerShiftCount[T safemath.Integer](operator string, left T, count uint64) (T, error) {
	if operator == "<<" {
		return integerShiftLeftCount(left, count)
	}

	return left >> count, nil
}

func integerPower[T safemath.Integer](base, exponent T) (T, error) {
	count, err := shiftCount(exponent)
	if err != nil {
		return 0, fmt.Errorf("integer exponent: %w", err)
	}

	if count > math.MaxUint {
		switch {
		case base == 0:
			return 0, nil
		case base == 1:
			return 1, nil
		case integerSigned(base) && base == ^T(0):
			if count&1 == 0 {
				return 1, nil
			}

			return base, nil
		default:
			return 0, safemath.ErrOverflow
		}
	}

	value, err := safemath.Pow(base, uint(count))
	if err != nil {
		return 0, fmt.Errorf("integer power: %w", err)
	}

	return value, nil
}

func integerShiftLeftCount[T safemath.Integer](left T, count uint64) (T, error) {
	if count > math.MaxUint {
		if left == 0 {
			return 0, nil
		}

		return 0, safemath.ErrOverflow
	}

	value, err := safemath.Lsh(left, uint(count))
	if err != nil {
		return 0, fmt.Errorf("integer left shift: %w", err)
	}

	return value, nil
}

func shiftCount[T safemath.Integer](value T) (uint64, error) {
	reflected := reflect.ValueOf(value)
	if integerSigned(value) {
		signed := reflected.Int()
		if signed < 0 {
			return 0, newDetailError("count cannot be negative")
		}

		return uint64(signed), nil
	}

	return reflected.Uint(), nil
}

func integerSigned[T safemath.Integer](value T) bool {
	return reflect.TypeOf(value).Kind() >= reflect.Int && reflect.TypeOf(value).Kind() <= reflect.Int64
}

func floatOperation[T float32 | float64](operator string, left, right T, span Span) (T, error) {
	switch operator {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		return left / right, nil
	case "%":
		return T(math.Mod(float64(left), float64(right))), nil
	case "**":
		return T(math.Pow(float64(left), float64(right))), nil
	default:
		return 0, evalError(
			ErrType, "evaluate "+operator, span, "",
			newDetailError("operator is not valid for floating-point values"),
		)
	}
}

func complexOperation[T complex64 | complex128](operator string, left, right T, span Span) (T, error) {
	switch operator {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		return left / right, nil
	case "**":
		return T(cmplx.Pow(complex128(left), complex128(right))), nil
	default:
		return 0, evalError(
			ErrType, "evaluate "+operator, span, "",
			newDetailError("operator is not valid for complex values"),
		)
	}
}
