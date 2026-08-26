package fasteval

import (
	"context"
	"sort"
)

type quantifyMode uint8
type findMode uint8

type keyedValue struct {
	value any
	key   any
}

const (
	quantifyAll quantifyMode = iota
	quantifyAny
	quantifyOne
	quantifyNone
)

const (
	findFirstValue findMode = iota
	findFirstIndex
	findLastValue
	findLastIndex
)

func quantifyBuiltin(mode quantifyMode) builtinFunc {
	return func(ctx context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, twoArguments, twoArguments)
		if err != nil {
			return nil, err
		}

		values, err := newSequenceView(arguments[0])
		if err != nil {
			return nil, err
		}

		function, err := resolveCallable(arguments[1])
		if err != nil {
			return nil, err
		}

		return quantifyValues(ctx, values, function, mode)
	}
}

func quantifyValues(
	ctx context.Context,
	values sequenceView,
	function *callable,
	mode quantifyMode,
) (bool, error) {
	matches := 0
	callArguments := []any{nil}
	invocation := function.newInvocation()

	for position := range values.len() {
		value, err := values.valueAt(ctx, position)
		if err != nil {
			return false, err
		}

		callArguments[0] = value

		matched, err := invokePredicate(ctx, &invocation, callArguments)
		if err != nil {
			return false, err
		}

		if matched {
			matches++
		}

		done, result := quantifyIntermediateResult(mode, matched, matches)
		if done {
			return result, nil
		}
	}

	switch mode {
	case quantifyAll, quantifyNone:
		return true, nil
	case quantifyOne:
		return matches == 1, nil
	case quantifyAny:
		return false, nil
	}

	return false, nil
}

func quantifyIntermediateResult(mode quantifyMode, matched bool, matches int) (bool, bool) {
	if !matched {
		return mode == quantifyAll, false
	}

	switch mode {
	case quantifyAny:
		return true, true
	case quantifyNone:
		return true, false
	case quantifyOne:
		return matches > 1, false
	case quantifyAll:
		return false, false
	}

	return false, false
}

func mapBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	values, err := sequenceValues(ctx, arguments[0])
	if err != nil {
		return nil, err
	}

	function, err := resolveCallable(arguments[1])
	if err != nil {
		return nil, err
	}

	result := make(rawSequence, len(values))
	callArguments := []any{nil}
	invocation := function.newInvocation()

	for position, value := range values {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		callArguments[0] = value

		result[position], err = invocation.invokeRaw(ctx, callArguments, emptySpan())
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func filterBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	values, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	function, err := resolveCallable(arguments[1])
	if err != nil {
		return nil, err
	}

	result := make(rawSequence, 0, values.len())
	callArguments := []any{nil}
	invocation := function.newInvocation()

	for position := range values.len() {
		value, err := values.rawValueAt(ctx, position)
		if err != nil {
			return nil, err
		}

		callArguments[0] = normalizeValue(value)

		matched, predicateErr := invokePredicate(ctx, &invocation, callArguments)
		if predicateErr != nil {
			return nil, predicateErr
		}

		if matched {
			result = append(result, value)
		}
	}

	return result, nil
}

func findBuiltin(mode findMode) builtinFunc {
	return func(ctx context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, twoArguments, twoArguments)
		if err != nil {
			return nil, err
		}

		values, err := newSequenceView(arguments[0])
		if err != nil {
			return nil, err
		}

		function, err := resolveCallable(arguments[1])
		if err != nil {
			return nil, err
		}

		position, value, err := findMatch(ctx, values, function, mode)
		if err != nil {
			return nil, err
		}

		if mode == findFirstIndex || mode == findLastIndex {
			return position, nil
		}

		if position < 0 {
			return nil, nil
		}

		return value, nil
	}
}

func findMatch(
	ctx context.Context,
	values sequenceView,
	function *callable,
	mode findMode,
) (int, any, error) {
	start, end, step := 0, values.len(), 1
	if mode == findLastValue || mode == findLastIndex {
		start, end, step = values.len()-1, -1, -1
	}

	callArguments := []any{nil}
	invocation := function.newInvocation()

	for position := start; position != end; position += step {
		value, err := values.rawValueAt(ctx, position)
		if err != nil {
			return -1, nil, err
		}

		callArguments[0] = normalizeValue(value)

		matched, err := invokePredicate(ctx, &invocation, callArguments)
		if err != nil {
			return -1, nil, err
		}

		if matched {
			return position, value, nil
		}
	}

	return -1, nil, nil
}

func groupByBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	view, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	values, err := view.rawValues(ctx)
	if err != nil {
		return nil, err
	}

	function, err := resolveCallable(arguments[1])
	if err != nil {
		return nil, err
	}

	result := make(rawGroupedMap)

	var literalKeys literalMapKeyIndex

	callArguments := []any{nil}
	invocation := function.newInvocation()

	for _, value := range values {
		key, err := groupedValueKey(ctx, &invocation, callArguments, value)
		if err != nil {
			return nil, err
		}

		bucketKey, found := existingEquivalentMapKey(result, &literalKeys, key)
		if !found {
			bucketKey = key
		}

		result[bucketKey] = append(result[bucketKey], value)
	}

	return result, nil
}

func groupedValueKey(
	ctx context.Context,
	invocation *callableInvocation,
	callArguments []any,
	value any,
) (any, error) {
	err := contextCheck(ctx)
	if err != nil {
		return nil, err
	}

	callArguments[0] = normalizeValue(value)

	key, err := invocation.invokeRaw(ctx, callArguments, emptySpan())
	if err != nil {
		return nil, err
	}

	if isNilValue(key) {
		key = nil
	}

	if !comparableMapKey(key) {
		return nil, newDetailError("group key has non-comparable type %T", key)
	}

	return key, nil
}

func countBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, twoArguments)
	if err != nil {
		return nil, err
	}

	values, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	if len(arguments) == 1 {
		return sequenceLength(ctx, values)
	}

	function, err := resolveCallable(arguments[1])
	if err != nil {
		return nil, err
	}

	count := 0
	callArguments := []any{nil}
	invocation := function.newInvocation()

	for position := range values.len() {
		value, valueErr := values.valueAt(ctx, position)
		if valueErr != nil {
			return nil, valueErr
		}

		callArguments[0] = value

		matched, predicateErr := invokePredicate(ctx, &invocation, callArguments)
		if predicateErr != nil {
			return nil, predicateErr
		}

		if matched {
			count++
		}
	}

	return count, nil
}

func sequenceLength(ctx context.Context, values sequenceView) (any, error) {
	if values.len() == 0 {
		return 0, nil
	}

	err := contextCheck(ctx)

	return values.len(), err
}

func reduceBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, threeArguments)
	if err != nil {
		return nil, err
	}

	view, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	values, err := view.rawValues(ctx)
	if err != nil {
		return nil, err
	}

	function, err := resolveCallable(arguments[1])
	if err != nil {
		return nil, err
	}

	var accumulator any

	start := 0

	if len(arguments) == threeArguments {
		accumulator = arguments[len(arguments)-1]
	} else {
		if len(values) == 0 {
			return nil, nil
		}

		accumulator = values[0]
		start = 1
	}

	callArguments := []any{nil, nil}
	invocation := function.newInvocation()

	for _, value := range values[start:] {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		callArguments[0], callArguments[1] = normalizeValue(accumulator), normalizeValue(value)

		accumulator, err = invocation.invokeRaw(ctx, callArguments, emptySpan())
		if err != nil {
			return nil, err
		}
	}

	return accumulator, nil
}

func sortByBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	view, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	values, err := view.rawValues(ctx)
	if err != nil {
		return nil, err
	}

	function, err := resolveCallable(arguments[1])
	if err != nil {
		return nil, err
	}

	items, err := buildKeyedValues(ctx, values, function)
	if err != nil {
		return nil, err
	}

	err = sortKeyedValues(ctx, items)
	if err != nil {
		return nil, err
	}

	return extractSortedValues(ctx, values, items)
}

func buildKeyedValues(ctx context.Context, values rawSequence, function *callable) ([]keyedValue, error) {
	items := make([]keyedValue, len(values))
	callArguments := []any{nil}
	invocation := function.newInvocation()

	for position, value := range values {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		callArguments[0] = normalizeValue(value)

		key, err := invocation.invoke(ctx, callArguments, emptySpan())
		if err != nil {
			return nil, err
		}

		items[position] = keyedValue{value: value, key: key}
	}

	return items, nil
}

func sortKeyedValues(ctx context.Context, items []keyedValue) error {
	var comparisonErr error

	sort.SliceStable(items, func(position, otherPosition int) bool {
		if comparisonErr != nil {
			return false
		}

		comparisonErr = contextCheck(ctx)
		if comparisonErr != nil {
			return false
		}

		var less bool

		less, comparisonErr = lessForSort(items[position].key, items[otherPosition].key)

		return less
	})

	return comparisonErr
}

func extractSortedValues(ctx context.Context, values rawSequence, items []keyedValue) (rawSequence, error) {
	for position := range items {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		values[position] = items[position].value
	}

	return values, nil
}
