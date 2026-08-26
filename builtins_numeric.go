package fasteval

import (
	"context"
	"fmt"
	"math"
	"math/cmplx"
	"reflect"
	"strconv"
	"time"

	"go.dw1.io/safemath"
)

const int64TypeName = "int64"

func registerNumericBuiltins(functions map[string]*callable) {
	types := map[string]reflect.Type{
		"int": reflect.TypeFor[int](), "int8": reflect.TypeFor[int8](), "int16": reflect.TypeFor[int16](),
		"int32": reflect.TypeFor[int32](), int64TypeName: reflect.TypeFor[int64](),
		"uint": reflect.TypeFor[uint](), "uint8": reflect.TypeFor[uint8](), "uint16": reflect.TypeFor[uint16](),
		"uint32": reflect.TypeFor[uint32](), "uint64": reflect.TypeFor[uint64](), "uintptr": reflect.TypeFor[uintptr](),
		"float": reflect.TypeFor[float64](), "float32": reflect.TypeFor[float32](), "float64": reflect.TypeFor[float64](),
		"complex64": reflect.TypeFor[complex64](), "complex128": reflect.TypeFor[complex128](),
	}
	for name, target := range types {
		registerBuiltin(functions, name, conversionBuiltin(target))
	}

	functions["min"] = newBuiltin("min", extremaBuiltin(false), allArgumentsRaw)
	functions["max"] = newBuiltin("max", extremaBuiltin(true), allArgumentsRaw)
	registerBuiltin(functions, "abs", absBuiltin)
	registerBuiltin(functions, "ceil", floatMathBuiltin(math.Ceil))
	registerBuiltin(functions, "floor", floatMathBuiltin(math.Floor))
	registerBuiltin(functions, "round", floatMathBuiltin(math.Round))
	registerBuiltin(functions, "real", realBuiltin)
	registerBuiltin(functions, "imag", imaginaryBuiltin)
	registerBuiltin(functions, "complex", complexBuiltin)
	registerBuiltin(functions, "conj", conjugateBuiltin)
	registerBuiltin(functions, "bitand", bitBinaryBuiltin("&"))
	registerBuiltin(functions, "bitor", bitBinaryBuiltin("|"))
	registerBuiltin(functions, "bitxor", bitBinaryBuiltin("^"))
	registerBuiltin(functions, "bitnand", bitNANDBuiltin)
	registerBuiltin(functions, "bitnot", bitNotBuiltin)
	registerBuiltin(functions, "bitshl", bitBinaryBuiltin("<<"))
	registerBuiltin(functions, "bitshr", bitBinaryBuiltin(">>"))
	registerBuiltin(functions, "bitushr", bitUnsignedShiftBuiltin)
}

func conversionBuiltin(target reflect.Type) builtinFunc {
	return func(ctx context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, 1, 1)
		if err != nil {
			return nil, err
		}

		if text, ok := arguments[0].(string); ok {
			return parseNumericString(text, target)
		}

		converted, err := convertReflectContext(ctx, arguments[0], target)
		if err != nil {
			return nil, err
		}

		return converted.Interface(), nil
	}
}

func parseNumericString(text string, target reflect.Type) (any, error) {
	result := reflect.New(target).Elem()

	switch target.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(text, 0, target.Bits())
		if err != nil {
			return nil, fmt.Errorf("parse signed integer: %w", err)
		}

		result.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value, err := strconv.ParseUint(text, 0, target.Bits())
		if err != nil {
			return nil, fmt.Errorf("parse unsigned integer: %w", err)
		}

		result.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(text, target.Bits())
		if err != nil {
			return nil, fmt.Errorf("parse floating-point number: %w", err)
		}

		result.SetFloat(value)
	case reflect.Complex64, reflect.Complex128:
		value, err := strconv.ParseComplex(text, target.Bits())
		if err != nil {
			return nil, fmt.Errorf("parse complex number: %w", err)
		}

		result.SetComplex(value)
	case reflect.Invalid, reflect.Bool, reflect.Array, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String,
		reflect.Struct, reflect.UnsafePointer:
		return nil, newDetailError("target %v is not numeric", target)
	}

	return result.Interface(), nil
}

type extremaCandidate struct {
	raw        any
	comparable any
}

func extremaBuiltin(maximum bool) builtinFunc {
	return func(ctx context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, 1, -1)
		if err != nil {
			return nil, err
		}

		candidates, err := extremaValues(ctx, arguments)
		if err != nil {
			return nil, err
		}

		if len(candidates) == 0 {
			return nil, newDetailError("min/max requires a non-empty sequence")
		}

		items := make([]extremaCandidate, len(candidates))
		for position, candidate := range candidates {
			items[position] = extremaCandidate{raw: candidate, comparable: normalizeValue(candidate)}
		}

		err = adaptExtremaLiterals(items)
		if err != nil {
			return nil, err
		}

		operator := "<"
		if maximum {
			operator = ">"
		}

		selected, err := selectExtrema(ctx, items, operator)
		if err != nil {
			return nil, err
		}

		return extremaResult(items[selected]), nil
	}
}

func selectExtrema(ctx context.Context, items []extremaCandidate, operator string) (int, error) {
	selected := 0
	if isNaN(items[selected].comparable) {
		return selected, nil
	}

	for position, item := range items[1:] {
		err := contextCheck(ctx)
		if err != nil {
			return 0, err
		}

		if isNaN(item.comparable) {
			return position + 1, nil
		}

		better, err := compareOperation(operator, item.comparable, items[selected].comparable, emptySpan())
		if err != nil {
			return 0, err
		}

		if better {
			selected = position + 1
		}
	}

	return selected, nil
}

func extremaResult(candidate extremaCandidate) any {
	if _, literal := candidate.raw.(numberLiteral); literal {
		return candidate.comparable
	}

	return candidate.raw
}

func extremaValues(ctx context.Context, arguments []any) ([]any, error) {
	if len(arguments) != 1 {
		return arguments, nil
	}

	value, valid, err := indirectValue(arguments[0])
	if err != nil {
		return nil, err
	}

	if !valid || !isSequenceKind(value.Kind()) {
		return arguments, nil
	}

	values, err := (sequenceView{value: value}).rawValues(ctx)

	return []any(values), err
}

func adaptExtremaLiterals(values []extremaCandidate) error {
	var target reflect.Type

	for _, value := range values {
		if _, literal := value.comparable.(numberLiteral); literal {
			continue
		}

		candidate := reflect.TypeOf(value.comparable)
		if isNumericType(candidate) {
			target = candidate

			break
		}
	}

	if target == nil {
		return nil
	}

	for position, value := range values {
		literal, ok := value.comparable.(numberLiteral)
		if !ok {
			continue
		}

		converted, err := convertNumberLiteral(literal, target)
		if err != nil {
			return err
		}

		values[position].comparable = converted.Interface()
	}

	return nil
}

func absBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value, err := materializeLiteral(arguments[0], emptySpan())
	if err != nil {
		return nil, err
	}

	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return nil, newDetailError("value has type %T, want numeric", value)
	}

	switch {
	case isSignedKind(reflected.Kind()):
		return absoluteSigned(value)
	case isUnsignedKind(reflected.Kind()):
		return value, nil
	case isFloatKind(reflected.Kind()):
		result := reflect.New(reflected.Type()).Elem()
		result.SetFloat(math.Abs(reflected.Float()))

		return result.Interface(), nil
	case reflected.Kind() == reflect.Complex64:
		return float32(cmplx.Abs(reflected.Complex())), nil
	case reflected.Kind() == reflect.Complex128:
		return cmplx.Abs(reflected.Complex()), nil
	default:
		return nil, newDetailError("value has type %T, want numeric", value)
	}
}

func absoluteSigned(value any) (any, error) {
	switch integer := value.(type) {
	case int:
		return checkedAbsolute(integer)
	case int8:
		return checkedAbsolute(integer)
	case int16:
		return checkedAbsolute(integer)
	case int32:
		return checkedAbsolute(integer)
	case int64:
		return checkedAbsolute(integer)
	case time.Duration:
		return checkedAbsolute(integer)
	default:
		return nil, newDetailError("value has type %T, want signed integer", value)
	}
}

func checkedAbsolute[T safemath.Signed](value T) (any, error) {
	result, err := safemath.Abs(value)
	if err != nil {
		return nil, evalError(
			ErrOverflow, "evaluate abs", emptySpan(), "",
			fmt.Errorf("absolute value overflows %T: %w", value, err),
		)
	}

	return result, nil
}

func floatMathBuiltin(function func(float64) float64) builtinFunc {
	return func(ctx context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, 1, 1)
		if err != nil {
			return nil, err
		}

		value, err := float64Value(ctx, arguments[0])
		if err != nil {
			return nil, err
		}

		return function(value), nil
	}
}

func float64Value(ctx context.Context, value any) (float64, error) {
	if literal, ok := value.(numberLiteral); ok {
		converted, err := convertNumberLiteral(literal, reflect.TypeFor[float64]())
		if err != nil {
			return 0, err
		}

		return converted.Float(), nil
	}

	if value == nil {
		return 0, newDetailError("value has type %T, want real number", value)
	}

	converted, err := convertReflectContext(ctx, value, reflect.TypeFor[float64]())
	if err != nil {
		return 0, newDetailError("value has type %T, want real number", value)
	}

	return converted.Float(), nil
}

func realBuiltin(ctx context.Context, arguments []any) (any, error) {
	value, err := complexValue(ctx, arguments)
	if err != nil {
		return nil, err
	}

	switch number := value.(type) {
	case complex64:
		return real(number), nil
	case complex128:
		return real(number), nil
	default:
		return nil, newDetailError("value has type %T, want complex number", value)
	}
}

func imaginaryBuiltin(ctx context.Context, arguments []any) (any, error) {
	value, err := complexValue(ctx, arguments)
	if err != nil {
		return nil, err
	}

	switch number := value.(type) {
	case complex64:
		return imag(number), nil
	case complex128:
		return imag(number), nil
	default:
		return nil, newDetailError("value has type %T, want complex number", value)
	}
}

func complexBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	leftWidth := classifyComplexOperand(arguments[0])
	rightWidth := classifyComplexOperand(arguments[1])

	narrow := leftWidth == complexOperandNarrow || rightWidth == complexOperandNarrow

	if narrow && (leftWidth == complexOperandWide || rightWidth == complexOperandWide) {
		return nil, newDetailError("complex operands have incompatible types %T and %T", arguments[0], arguments[1])
	}

	target := reflect.TypeFor[float64]()
	if narrow {
		target = reflect.TypeFor[float32]()
	}

	var parts [2]float64

	for position, argument := range arguments {
		converted, conversionErr := convertReflectContext(ctx, argument, target)
		if conversionErr != nil {
			return nil, conversionErr
		}

		parts[position] = converted.Float()
	}

	if narrow {
		return complex(float32(parts[0]), float32(parts[1])), nil
	}

	return complex(parts[0], parts[1]), nil
}

type complexOperandWidth uint8

const (
	complexOperandLiteral complexOperandWidth = iota
	complexOperandNarrow
	complexOperandWide
)

func classifyComplexOperand(value any) complexOperandWidth {
	if _, literal := value.(numberLiteral); literal {
		return complexOperandLiteral
	}

	typeOf := reflect.TypeOf(value)
	if typeOf != nil && typeOf.Kind() == reflect.Float32 {
		return complexOperandNarrow
	}

	return complexOperandWide
}

func conjugateBuiltin(ctx context.Context, arguments []any) (any, error) {
	value, err := complexValue(ctx, arguments)
	if err != nil {
		return nil, err
	}

	switch number := value.(type) {
	case complex64:
		return complex(real(number), -imag(number)), nil
	case complex128:
		return cmplx.Conj(number), nil
	default:
		return nil, newDetailError("value has type %T, want complex number", value)
	}
}

func complexValue(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value := normalizeValue(arguments[0])
	switch value.(type) {
	case complex64, complex128:
		return value, nil
	}

	converted, err := convertReflectContext(ctx, value, reflect.TypeFor[complex128]())
	if err != nil {
		return nil, err
	}

	return converted.Interface(), nil
}

func bitBinaryBuiltin(operator string) builtinFunc {
	return func(_ context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, twoArguments, twoArguments)
		if err != nil {
			return nil, err
		}

		left := arguments[0]
		right := arguments[1]
		leftLiteral, leftIsLiteral := left.(numberLiteral)

		_, rightIsLiteral := right.(numberLiteral)

		if leftIsLiteral && !rightIsLiteral {
			converted, conversionErr := convertNumberLiteral(leftLiteral, reflect.TypeOf(right))
			if conversionErr != nil {
				return nil, conversionErr
			}

			left = converted.Interface()
		} else {
			left, err = materializeLiteral(left, emptySpan())
			if err != nil {
				return nil, err
			}
		}

		if literal, ok := right.(numberLiteral); ok && operator != "<<" && operator != ">>" {
			converted, conversionErr := convertNumberLiteral(literal, reflect.TypeOf(left))
			if conversionErr != nil {
				return nil, conversionErr
			}

			right = converted.Interface()
		}

		return binaryNumericOrString(operator, left, right, emptySpan())
	}
}

func bitNANDBuiltin(ctx context.Context, arguments []any) (any, error) {
	value, err := bitBinaryBuiltin("&")(ctx, arguments)
	if err != nil {
		return nil, err
	}

	return unaryOperation("~", value, emptySpan())
}

func bitNotBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value, err := materializeLiteral(arguments[0], emptySpan())
	if err != nil {
		return nil, err
	}

	return unaryOperation("~", value, emptySpan())
}

func bitUnsignedShiftBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	value, err := convertReflectContext(ctx, arguments[0], reflect.TypeFor[uint64]())
	if err != nil {
		return nil, err
	}

	count, err := convertReflectContext(ctx, arguments[1], reflect.TypeFor[uint64]())
	if err != nil {
		return nil, err
	}

	return value.Uint() >> count.Uint(), nil
}
