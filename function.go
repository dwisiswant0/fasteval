package fasteval

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

type builtinFunc func(context.Context, []any) (any, error)

type callableArgumentMode uint8

const callableArguments callableArgumentMode = 0

const (
	secondArgumentFunctionReference callableArgumentMode = 1 << iota
	firstArgumentRaw
	allArgumentsRaw
)

type callable struct {
	name         string
	function     reflect.Value
	typeOf       reflect.Type
	contextual   bool
	variadic     bool
	builtin      builtinFunc
	argumentMode callableArgumentMode
}

type callableInvocation struct {
	callable *callable
	inputs   []reflect.Value
}

func (c *callable) newInvocation() callableInvocation {
	return callableInvocation{callable: c, inputs: nil}
}

func (i *callableInvocation) invoke(ctx context.Context, arguments []any, span Span) (any, error) {
	result, err := i.callable.invokeRawWithInputs(ctx, arguments, span, &i.inputs)
	if err != nil {
		return nil, err
	}

	return normalizeValue(result), nil
}

func (i *callableInvocation) invokeRaw(ctx context.Context, arguments []any, span Span) (any, error) {
	return i.callable.invokeRawWithInputs(ctx, arguments, span, &i.inputs)
}

func newCallable(name string, function any) (*callable, error) {
	if name == "" {
		return nil, newDetailError("function name cannot be empty")
	}

	value := reflect.ValueOf(function)
	if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
		return nil, newDetailError("function %q must be a non-nil Go function", name)
	}

	return newReflectCallable(name, value)
}

func newReflectCallable(name string, function reflect.Value) (*callable, error) {
	typeOf := function.Type()
	if typeOf.NumOut() != 1 && typeOf.NumOut() != 2 {
		return nil, newDetailError("function %q must return R or (R, error)", name)
	}

	if typeOf.NumOut() == 2 && typeOf.Out(1) != reflect.TypeFor[error]() {
		return nil, newDetailError("function %q second result must be error", name)
	}

	contextual := typeOf.NumIn() > 0 && typeOf.In(0) == reflect.TypeFor[context.Context]()

	return &callable{
		name: name, function: function, typeOf: typeOf,
		contextual: contextual, variadic: typeOf.IsVariadic(), builtin: nil,
		argumentMode: callableArguments,
	}, nil
}

func newBuiltin(name string, function builtinFunc, argumentMode callableArgumentMode) *callable {
	return &callable{
		name: name, function: reflect.Value{}, typeOf: nil,
		contextual: false, variadic: false, builtin: function,
		argumentMode: argumentMode,
	}
}

func (c *callable) invokeRaw(ctx context.Context, arguments []any, span Span) (any, error) {
	return c.invokeRawWithInputs(ctx, arguments, span, nil)
}

func (c *callable) invokeRawWithInputs(
	ctx context.Context,
	arguments []any,
	span Span,
	inputBuffer *[]reflect.Value,
) (any, error) {
	if c == nil {
		return nil, evalError(ErrFunction, "call function", span, "", newDetailError("function reference is nil"))
	}

	err := contextCheckAt(ctx, "call function", span)
	if err != nil {
		return nil, err
	}

	var result any

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = nil
				err = evalError(ErrPanic, "call function", span, c.name, newDetailError("panic: %v", recovered))
			}
		}()

		if c.builtin != nil {
			result, err = c.invokeBuiltin(ctx, arguments, span)

			return
		}

		result, err = c.invokeReflect(ctx, arguments, span, inputBuffer)
	}()

	if err == nil {
		cancellationErr := contextCheckAt(ctx, "call function", span)
		if cancellationErr != nil {
			return nil, cancellationErr
		}
	}

	return result, err
}

func (c *callable) invokeBuiltin(ctx context.Context, arguments []any, span Span) (any, error) {
	value, err := c.builtin(ctx, arguments)
	if err == nil {
		return value, nil
	}

	evaluationErr := new(EvalError)
	if errors.As(err, &evaluationErr) {
		if evaluationErr.span != emptySpan() {
			return nil, evaluationErr
		}

		withCallSpan := *evaluationErr
		withCallSpan.span = span

		return nil, &withCallSpan
	}

	return nil, evalError(ErrFunction, "call function", span, c.name, err)
}

func (c *callable) invokeReflect(
	ctx context.Context,
	arguments []any,
	span Span,
	inputBuffer *[]reflect.Value,
) (any, error) {
	firstParameter, err := c.validateArgumentCount(arguments, span)
	if err != nil {
		return nil, err
	}

	var reusable []reflect.Value
	if inputBuffer != nil {
		reusable = *inputBuffer
	}

	inputs, err := c.callInputs(ctx, arguments, firstParameter, span, reusable)
	if inputBuffer != nil {
		*inputBuffer = inputs
	}

	if err != nil {
		return nil, err
	}

	return c.callOutput(c.function.Call(inputs), span)
}

func (c *callable) validateArgumentCount(arguments []any, span Span) (int, error) {
	firstParameter := 0
	if c.contextual {
		firstParameter = 1
	}

	minimum := c.typeOf.NumIn() - firstParameter
	if c.variadic {
		minimum--
		if len(arguments) < minimum {
			return 0, evalError(ErrFunction, "call function", span, c.name,
				newDetailError("got %d arguments, want at least %d", len(arguments), minimum))
		}
	} else if len(arguments) != minimum {
		return 0, evalError(ErrFunction, "call function", span, c.name,
			newDetailError("got %d arguments, want %d", len(arguments), minimum))
	}

	return firstParameter, nil
}

func (c *callable) callInputs(
	ctx context.Context,
	arguments []any,
	firstParameter int,
	span Span,
	reusable []reflect.Value,
) ([]reflect.Value, error) {
	inputCount := len(arguments) + firstParameter

	inputs := reusable[:0]

	if cap(inputs) < inputCount {
		inputs = make([]reflect.Value, 0, inputCount)
	}

	if c.contextual {
		inputs = append(inputs, reflect.ValueOf(ctx))
	}

	for position, argument := range arguments {
		parameterIndex := position + firstParameter
		parameterType := c.parameterType(parameterIndex)

		converted, conversionErr := convertReflectContext(ctx, argument, parameterType)
		if conversionErr != nil {
			var evaluationErr *EvalError
			if errors.As(conversionErr, &evaluationErr) {
				return nil, withEvalErrorSpan(evaluationErr, span)
			}

			return nil, evalError(
				ErrType, "convert function argument", span, c.name,
				fmt.Errorf("argument %d: %w", position+1, conversionErr))
		}

		inputs = append(inputs, converted)
	}

	return inputs, nil
}

func (c *callable) parameterType(parameterIndex int) reflect.Type {
	if c.variadic && parameterIndex >= c.typeOf.NumIn()-1 {
		return c.typeOf.In(c.typeOf.NumIn() - 1).Elem()
	}

	return c.typeOf.In(parameterIndex)
}

func (c *callable) callOutput(outputs []reflect.Value, span Span) (any, error) {
	if len(outputs) == 2 && !outputs[1].IsNil() {
		callErr, ok := outputs[1].Interface().(error)
		if !ok {
			return nil, evalError(
				ErrFunction, "call function", span, c.name,
				newDetailError("second return value has type %v, want error", outputs[1].Type()),
			)
		}

		return nil, evalError(ErrFunction, "call function", span, c.name, callErr)
	}

	return outputs[0].Interface(), nil
}
