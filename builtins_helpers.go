package fasteval

import (
	"context"
	"math"
	"reflect"
)

const (
	twoArguments   = 2
	threeArguments = 3
	fourArguments  = 4
)

type sequenceView struct {
	value reflect.Value
}

func lessForSort(left, right any) (bool, error) {
	leftNaN := isNaN(left)

	rightNaN := isNaN(right)

	if leftNaN || rightNaN {
		return leftNaN && !rightNaN, nil
	}

	return compareOperation("<", left, right, emptySpan())
}

func isNaN(value any) bool {
	switch number := value.(type) {
	case float32:
		return math.IsNaN(float64(number))
	case float64:
		return math.IsNaN(number)
	default:
		return false
	}
}

func checkArity(arguments []any, minimum, maximum int) error {
	if len(arguments) < minimum || maximum >= 0 && len(arguments) > maximum {
		if minimum == maximum {
			return newDetailError("got %d arguments, want %d", len(arguments), minimum)
		}

		if maximum < 0 {
			return newDetailError("got %d arguments, want at least %d", len(arguments), minimum)
		}

		return newDetailError("got %d arguments, want %d to %d", len(arguments), minimum, maximum)
	}

	return nil
}

func sequenceValues(ctx context.Context, value any) ([]any, error) {
	view, err := newSequenceView(value)
	if err != nil {
		return nil, err
	}

	return view.values(ctx)
}

func newSequenceView(value any) (sequenceView, error) {
	if value == nil {
		return sequenceView{}, newDetailError("value is nil, want array or slice")
	}

	reflected, valid, err := indirectValue(value)
	if err != nil {
		return sequenceView{}, err
	}

	if !valid {
		return sequenceView{}, newDetailError("value is nil, want array or slice")
	}

	if reflected.Kind() != reflect.Array && reflected.Kind() != reflect.Slice {
		return sequenceView{}, newDetailError("value has type %T, want array or slice", value)
	}

	return sequenceView{value: reflected}, nil
}

func (v sequenceView) len() int {
	return v.value.Len()
}

func (v sequenceView) valueAt(ctx context.Context, position int) (any, error) {
	value, err := v.rawValueAt(ctx, position)
	if err != nil {
		return nil, err
	}

	return normalizeValue(value), nil
}

func (v sequenceView) rawValueAt(ctx context.Context, position int) (any, error) {
	err := contextCheck(ctx)
	if err != nil {
		return nil, err
	}

	return v.value.Index(position).Interface(), nil
}

func (v sequenceView) values(ctx context.Context) ([]any, error) {
	result := make([]any, v.len())
	for position := range result {
		value, err := v.valueAt(ctx, position)
		if err != nil {
			return nil, err
		}

		result[position] = value
	}

	return result, nil
}

func (v sequenceView) rawValues(ctx context.Context) (rawSequence, error) {
	result := make(rawSequence, v.len())
	for position := range result {
		value, err := v.rawValueAt(ctx, position)
		if err != nil {
			return nil, err
		}

		result[position] = value
	}

	return result, nil
}

func resolveCallable(value any) (*callable, error) {
	if function, ok := value.(*callable); ok {
		return function, nil
	}

	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && reflected.Kind() == reflect.Func {
		return newReflectCallable("function reference", reflected)
	}

	return nil, newDetailError("value has type %T, want a registered function reference", value)
}

func invokePredicate(ctx context.Context, invocation *callableInvocation, arguments []any) (bool, error) {
	value, err := invocation.invoke(ctx, arguments, emptySpan())
	if err != nil {
		return false, err
	}

	boolean, ok := value.(bool)
	if !ok {
		return false, newDetailError("predicate %q returned %T, want bool", invocation.callable.name, value)
	}

	return boolean, nil
}

func integerArgument(ctx context.Context, value any) (int, error) {
	converted, err := convertReflectContext(ctx, value, reflect.TypeFor[int]())
	if err != nil {
		return 0, err
	}

	return int(converted.Int()), nil
}

func contextCheck(ctx context.Context) error {
	return contextCheckAt(ctx, "evaluate builtin", emptySpan())
}

func contextCheckAt(ctx context.Context, operation string, span Span) error {
	err := ctx.Err()
	if err != nil {
		return evalError(ErrCanceled, operation, span, "", err)
	}

	return nil
}
