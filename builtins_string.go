package fasteval

import (
	"context"
	"math"
	"strings"
)

func registerStringBuiltins(functions map[string]*callable) {
	registerBuiltin(functions, "trim", unaryStringBuiltin(strings.TrimSpace))
	registerBuiltin(functions, "trimPrefix", binaryStringBuiltin(strings.TrimPrefix))
	registerBuiltin(functions, "trimSuffix", binaryStringBuiltin(strings.TrimSuffix))
	registerBuiltin(functions, "upper", unaryStringBuiltin(strings.ToUpper))
	registerBuiltin(functions, "lower", unaryStringBuiltin(strings.ToLower))
	registerBuiltin(functions, "split", splitBuiltin(false))
	registerBuiltin(functions, "splitAfter", splitBuiltin(true))
	registerBuiltin(functions, "replace", replaceBuiltin)
	registerBuiltin(functions, "repeat", repeatBuiltin)
	registerBuiltin(functions, "indexOf", binaryStringIntBuiltin(strings.Index))
	registerBuiltin(functions, "lastIndexOf", binaryStringIntBuiltin(strings.LastIndex))
	registerBuiltin(functions, "hasPrefix", binaryStringBoolBuiltin(strings.HasPrefix))
	registerBuiltin(functions, "hasSuffix", binaryStringBoolBuiltin(strings.HasSuffix))
}

func unaryStringBuiltin(function func(string) string) builtinFunc {
	return func(_ context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, 1, 1)
		if err != nil {
			return nil, err
		}

		value, ok := arguments[0].(string)
		if !ok {
			return nil, newDetailError("argument has type %T, want string", arguments[0])
		}

		return function(value), nil
	}
}

func binaryStringBuiltin(function func(string, string) string) builtinFunc {
	return func(_ context.Context, arguments []any) (any, error) {
		left, right, err := twoStrings(arguments)
		if err != nil {
			return nil, err
		}

		return function(left, right), nil
	}
}

func binaryStringIntBuiltin(function func(string, string) int) builtinFunc {
	return func(_ context.Context, arguments []any) (any, error) {
		left, right, err := twoStrings(arguments)
		if err != nil {
			return nil, err
		}

		return function(left, right), nil
	}
}

func binaryStringBoolBuiltin(function func(string, string) bool) builtinFunc {
	return func(_ context.Context, arguments []any) (any, error) {
		left, right, err := twoStrings(arguments)
		if err != nil {
			return nil, err
		}

		return function(left, right), nil
	}
}

func splitBuiltin(after bool) builtinFunc {
	return func(_ context.Context, arguments []any) (any, error) {
		left, right, err := twoStrings(arguments)
		if err != nil {
			return nil, err
		}

		var values []string
		if after {
			values = strings.SplitAfter(left, right)
		} else {
			values = strings.Split(left, right)
		}

		result := make([]any, len(values))
		for i := range values {
			result[i] = values[i]
		}

		return result, nil
	}
}

func replaceBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, threeArguments, fourArguments)
	if err != nil {
		return nil, err
	}

	value, valueOK := arguments[0].(string)
	old, oldOK := arguments[1].(string)

	replacement, replacementOK := arguments[2].(string)

	if !valueOK || !oldOK || !replacementOK {
		return nil, newDetailError("replace arguments must be strings")
	}

	count := -1

	if len(arguments) == fourArguments {
		var err error

		count, err = integerArgument(ctx, arguments[3])
		if err != nil {
			return nil, err
		}
	}

	return strings.Replace(value, old, replacement, count), nil
}

func repeatBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	value, ok := arguments[0].(string)
	if !ok {
		return nil, newDetailError("argument 1 has type %T, want string", arguments[0])
	}

	count, err := integerArgument(ctx, arguments[1])
	if err != nil {
		return nil, err
	}

	if count < 0 {
		return nil, newDetailError("repeat count cannot be negative")
	}

	if len(value) != 0 && count > math.MaxInt/len(value) {
		return nil, newDetailError("repeated string length overflows int")
	}

	return strings.Repeat(value, count), nil
}

func twoStrings(arguments []any) (string, string, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return "", "", err
	}

	left, leftOK := arguments[0].(string)

	right, rightOK := arguments[1].(string)

	if !leftOK || !rightOK {
		return "", "", newDetailError("arguments have types %T and %T, want strings", arguments[0], arguments[1])
	}

	return left, right, nil
}
