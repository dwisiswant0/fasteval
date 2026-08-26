package fasteval

import (
	"go/constant"
	gotoken "go/token"
	"math"
	"reflect"
	"time"
)

func compareOperation(operator string, left, right any, span Span) (bool, error) {
	switch leftValue := left.(type) {
	case int:
		if rightValue, ok := right.(int); ok {
			return compareOrdered(operator, leftValue, rightValue), nil
		}

		if result, matched := compareIntDecimal(operator, leftValue, right); matched {
			return result, nil
		}
	case float64:
		if rightValue, ok := right.(float64); ok {
			return compareOrdered(operator, leftValue, rightValue), nil
		}

		rightLiteral, ok := right.(numberLiteral)
		if ok && rightLiteral.float64OK {
			return compareOrdered(operator, leftValue, rightLiteral.float64Value), nil
		}
	}

	return genericCompareOperation(operator, left, right, span)
}

func compareIntDecimal(operator string, left int, right any) (bool, bool) {
	rightLiteral, ok := right.(numberLiteral)
	if !ok {
		return false, false
	}

	rightInteger, integerOK := rightLiteral.simpleDecimalInteger()
	if !integerOK || rightInteger > uint64(math.MaxInt) {
		return false, false
	}

	return compareOrdered(operator, left, int(rightInteger)), true
}

func genericCompareOperation(operator string, left, right any, span Span) (bool, error) {
	if isTimeOperand(left) || isTimeOperand(right) {
		var err error

		left, right, err = adaptDurationLiterals(left, right)
		if err != nil {
			return false, evalError(ErrType, "evaluate "+operator, span, "", err)
		}
	}

	result, handled, err := compareSpecialValues(operator, left, right, span)
	if err != nil || handled {
		return result, err
	}

	if result, matched := compareSameType(operator, left, right); matched {
		return result, nil
	}

	if result, matched := compareLiteralConstants(operator, left, right); matched {
		return result, nil
	}

	left, right, err = coerceNumbers(left, right)
	if err != nil {
		return false, evalError(ErrType, "evaluate "+operator, span, "", err)
	}

	result, matched := compareSameType(operator, left, right)
	if !matched {
		return false, evalError(ErrType, "evaluate "+operator, span, "", newDetailError("complex values cannot be ordered"))
	}

	return result, nil
}

func compareSpecialValues(operator string, left, right any, span Span) (bool, bool, error) {
	if leftString, ok := left.(string); ok {
		rightString, rightOK := right.(string)
		if !rightOK {
			return false, true, comparisonTypeError(operator, span, "string", right)
		}

		return compareOrdered(operator, leftString, rightString), true, nil
	}

	if leftTime, ok := left.(time.Time); ok {
		rightTime, rightOK := right.(time.Time)
		if !rightOK {
			return false, true, comparisonTypeError(operator, span, "time.Time", right)
		}

		comparison := leftTime.Compare(rightTime)

		return compareResult(operator, comparison), true, nil
	}

	if leftDuration, ok := left.(time.Duration); ok {
		rightDuration, rightOK := right.(time.Duration)
		if !rightOK {
			return false, true, comparisonTypeError(operator, span, "time.Duration", right)
		}

		return compareOrdered(operator, leftDuration, rightDuration), true, nil
	}

	return false, false, nil
}

func compareLiteralConstants(operator string, left, right any) (bool, bool) {
	leftLiteral, leftOK := left.(numberLiteral)

	rightLiteral, rightOK := right.(numberLiteral)

	if !leftOK || !rightOK ||
		leftLiteral.constantKind() == constant.Complex || rightLiteral.constantKind() == constant.Complex {
		return false, false
	}

	var token gotoken.Token

	switch operator {
	case ">":
		token = gotoken.GTR
	case ">=":
		token = gotoken.GEQ
	case "<":
		token = gotoken.LSS
	case "<=":
		token = gotoken.LEQ
	default:
		return false, false
	}

	return constant.Compare(leftLiteral.exactValue(), token, rightLiteral.exactValue()), true
}

func comparisonTypeError(operator string, span Span, leftType string, right any) error {
	return evalError(
		ErrType, "evaluate "+operator, span, "",
		newDetailError("operands have types %s and %T", leftType, right),
	)
}

func compareSameType(operator string, left, right any) (bool, bool) {
	reflected := reflect.ValueOf(left)
	if !reflected.IsValid() {
		return false, false
	}

	switch {
	case isSignedKind(reflected.Kind()):
		return compareSameSigned(operator, left, right)
	case isUnsignedKind(reflected.Kind()):
		return compareSameUnsigned(operator, left, right)
	default:
		return compareSameFloat(operator, left, right)
	}
}

func compareSameSigned(operator string, left, right any) (bool, bool) {
	switch leftValue := left.(type) {
	case int:
		rightValue, ok := right.(int)

		return compareOrdered(operator, leftValue, rightValue), ok
	case int8:
		rightValue, ok := right.(int8)

		return compareOrdered(operator, leftValue, rightValue), ok
	case int16:
		rightValue, ok := right.(int16)

		return compareOrdered(operator, leftValue, rightValue), ok
	case int32:
		rightValue, ok := right.(int32)

		return compareOrdered(operator, leftValue, rightValue), ok
	case int64:
		rightValue, ok := right.(int64)

		return compareOrdered(operator, leftValue, rightValue), ok
	default:
		return false, false
	}
}

func compareSameUnsigned(operator string, left, right any) (bool, bool) {
	switch leftValue := left.(type) {
	case uint:
		rightValue, ok := right.(uint)

		return compareOrdered(operator, leftValue, rightValue), ok
	case uint8:
		rightValue, ok := right.(uint8)

		return compareOrdered(operator, leftValue, rightValue), ok
	case uint16:
		rightValue, ok := right.(uint16)

		return compareOrdered(operator, leftValue, rightValue), ok
	case uint32:
		rightValue, ok := right.(uint32)

		return compareOrdered(operator, leftValue, rightValue), ok
	case uint64:
		rightValue, ok := right.(uint64)

		return compareOrdered(operator, leftValue, rightValue), ok
	case uintptr:
		rightValue, ok := right.(uintptr)

		return compareOrdered(operator, leftValue, rightValue), ok
	default:
		return false, false
	}
}

func compareSameFloat(operator string, left, right any) (bool, bool) {
	switch leftValue := left.(type) {
	case float32:
		rightValue, ok := right.(float32)

		return compareOrdered(operator, leftValue, rightValue), ok
	case float64:
		rightValue, ok := right.(float64)

		return compareOrdered(operator, leftValue, rightValue), ok
	default:
		return false, false
	}
}

type ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 | ~string
}

func compareOrdered[T ordered](operator string, left, right T) bool {
	switch operator {
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<":
		return left < right
	case "<=":
		return left <= right
	default:
		return false
	}
}

func compareResult(operator string, comparison int) bool {
	switch operator {
	case ">":
		return comparison > 0
	case ">=":
		return comparison >= 0
	case "<":
		return comparison < 0
	case "<=":
		return comparison <= 0
	default:
		return false
	}
}
