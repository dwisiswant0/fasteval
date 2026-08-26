package fasteval

import (
	"context"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"
)

const medianPairSize = 2

type flattenStackItem struct {
	value   any
	leave   flattenIdentity
	leaving bool
}

type flattenIdentity struct {
	pointer uintptr
	length  int
}

func concatBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, -1)
	if err != nil {
		return nil, err
	}

	result := make(rawSequence, 0)

	for _, argument := range arguments {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		view, err := newSequenceView(argument)
		if err != nil {
			return nil, err
		}

		values, err := view.rawValues(ctx)
		if err != nil {
			return nil, err
		}

		result = append(result, values...)
	}

	return result, nil
}

func flattenBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
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

	stack := make([]flattenStackItem, 0, len(values))
	for _, v := range slices.Backward(values) {
		stack = append(stack, flattenStackItem{
			value: v, leave: flattenIdentity{pointer: 0, length: 0}, leaving: false,
		})
	}

	visiting := make(map[flattenIdentity]bool)

	result := make(rawSequence, 0)

	for len(stack) > 0 {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if item.leaving {
			delete(visiting, item.leave)

			continue
		}

		collection, err := pushFlattenCollection(item.value, &stack, visiting)
		if err != nil {
			return nil, err
		}

		if collection {
			continue
		}

		result = append(result, item.value)
	}

	return result, nil
}

func pushFlattenCollection(
	value any,
	stack *[]flattenStackItem,
	visiting map[flattenIdentity]bool,
) (bool, error) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.Array && reflected.Kind() != reflect.Slice {
		return false, nil
	}

	if reflected.Kind() == reflect.Slice && !reflected.IsNil() {
		identity := flattenIdentity{pointer: reflected.Pointer(), length: reflected.Len()}
		if visiting[identity] {
			return false, newDetailError("flatten input contains a collection cycle")
		}

		visiting[identity] = true

		*stack = append(*stack, flattenStackItem{value: nil, leave: identity, leaving: true})
	}

	for position := reflected.Len() - 1; position >= 0; position-- {
		*stack = append(*stack, flattenStackItem{
			value: reflected.Index(position).Interface(),
			leave: flattenIdentity{pointer: 0, length: 0}, leaving: false,
		})
	}

	return true, nil
}

func uniqBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
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

	state := uniqState{
		result: make(rawSequence, 0, len(values)),
		seen:   nil, fallback: nil, fullScan: nil, capacity: len(values),
	}

	for _, value := range values {
		err := state.append(ctx, value)
		if err != nil {
			return nil, err
		}
	}

	return state.result, nil
}

type uniqState struct {
	result   rawSequence
	seen     map[equalityHashKey]struct{}
	fallback []any
	fullScan []any
	capacity int
}

func (s *uniqState) append(ctx context.Context, value any) error {
	err := contextCheck(ctx)
	if err != nil {
		return err
	}

	key, state := makeEqualityHashKey(value)
	switch state {
	case equalityHashAlwaysDistinct:
		s.result = append(s.result, value)

		return nil
	case equalityHashAvailable:
		return s.appendHashable(ctx, key, value)
	case equalityHashRequiresFullScan:
		return s.appendScanned(ctx, &s.fullScan, s.result, value)
	case equalityHashUnavailable:
		return s.appendScanned(ctx, &s.fallback, s.fallback, value)
	}

	return nil
}

func (s *uniqState) appendHashable(ctx context.Context, key equalityHashKey, value any) error {
	if len(s.fullScan) != 0 {
		duplicate, err := containsValue(ctx, s.fullScan, value)
		if err != nil || duplicate {
			return err
		}
	}

	if _, duplicate := s.seen[key]; duplicate {
		return nil
	}

	if s.seen == nil {
		s.seen = make(map[equalityHashKey]struct{}, s.capacity)
	}

	s.seen[key] = struct{}{}
	s.result = append(s.result, value)

	return nil
}

func (s *uniqState) appendScanned(ctx context.Context, scanned *[]any, search []any, value any) error {
	duplicate, err := containsValue(ctx, search, value)
	if err != nil || duplicate {
		return err
	}

	*scanned = append(*scanned, value)
	s.result = append(s.result, value)

	return nil
}

func joinBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, twoArguments)
	if err != nil {
		return nil, err
	}

	values, err := sequenceValues(ctx, arguments[0])
	if err != nil {
		return nil, err
	}

	delimiter, ok := arguments[1].(string)
	if !ok {
		return nil, newDetailError("delimiter has type %T, want string", arguments[1])
	}

	parts := make([]string, len(values))

	for position, value := range values {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		value, err = materializeLiteral(value, emptySpan())
		if err != nil {
			return nil, err
		}

		parts[position], err = formatTemplateValue(value, emptySpan())
		if err != nil {
			return nil, err
		}
	}

	return strings.Join(parts, delimiter), nil
}

func sumBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	values, err := sequenceValues(ctx, arguments[0])
	if err != nil {
		return nil, err
	}

	if len(values) == 0 {
		return 0, nil
	}

	result := values[0]

	for _, value := range values[1:] {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		timeResult, handled, timeErr := timeOperation("+", result, value, emptySpan())
		if handled {
			if timeErr != nil {
				return nil, timeErr
			}

			result = timeResult

			continue
		}

		result, err = binaryNumericOrString("+", result, value, emptySpan())
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func meanBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	values, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	if values.len() == 0 {
		return nil, newDetailError("mean requires a non-empty sequence")
	}

	scan, err := scanMeanValues(ctx, values)
	if err != nil {
		return nil, err
	}

	divisor := float64(values.len())

	preliminary, done, err := scan.result(ctx, values, divisor)
	if done || err != nil {
		return preliminary, err
	}

	return mixedSignMean(ctx, values, divisor)
}

func (s *meanScan) result(
	ctx context.Context,
	values sequenceView,
	divisor float64,
) (float64, bool, error) {
	if s.notANumber || s.positiveInfinity && s.negativeInfinity {
		return math.NaN(), true, nil
	}

	if s.positiveInfinity {
		return math.Inf(1), true, nil
	}

	if s.negativeInfinity {
		return math.Inf(-1), true, nil
	}

	if s.positiveFinite && s.negativeFinite {
		return 0, false, nil
	}

	if !s.sameSignReliable {
		result, err := scaledSameSignMean(ctx, values, s.maximumMagnitude, divisor)

		return result, true, err
	}

	return s.sameSignMean, true, nil
}

func mixedSignMean(ctx context.Context, values sequenceView, divisor float64) (float64, error) {
	quotientTotal, quotientCorrection := 0.0, 0.0
	remainderTotal, remainderCorrection := 0.0, 0.0

	for position := range values.len() {
		err := contextCheck(ctx)
		if err != nil {
			return 0, err
		}

		number, conversionErr := float64FromReflect(ctx, values.value.Index(position))
		if conversionErr != nil {
			return 0, conversionErr
		}

		if math.IsInf(number, 0) {
			continue
		}

		// Split each term before summation. The quotients cannot overflow,
		// and compensated remainders preserve values lost by division.
		quotient := number / divisor
		remainder := math.FMA(-quotient, divisor, number)
		addCompensated(&quotientTotal, &quotientCorrection, quotient)
		addCompensated(&remainderTotal, &remainderCorrection, remainder)
	}

	return quotientTotal + quotientCorrection + (remainderTotal+remainderCorrection)/divisor, nil
}

type meanScan struct {
	positiveInfinity bool
	negativeInfinity bool
	positiveFinite   bool
	negativeFinite   bool
	notANumber       bool
	sameSignMean     float64
	sameSignReliable bool
	maximumMagnitude float64
	finiteCount      int
}

func scanMeanValues(ctx context.Context, values sequenceView) (meanScan, error) {
	result := meanScan{
		positiveInfinity: false, negativeInfinity: false,
		positiveFinite: false, negativeFinite: false, notANumber: false,
		sameSignMean: 0, sameSignReliable: true, maximumMagnitude: 0, finiteCount: 0,
	}

	for position := range values.len() {
		err := contextCheck(ctx)
		if err != nil {
			return result, err
		}

		number, err := float64FromReflect(ctx, values.value.Index(position))
		if err != nil {
			return result, err
		}

		result.add(number)

		if result.notANumber {
			return result, nil
		}
	}

	return result, nil
}

func (s *meanScan) add(number float64) {
	if math.IsNaN(number) {
		s.notANumber = true

		return
	}

	if math.IsInf(number, 1) {
		s.positiveInfinity = true

		return
	}

	if math.IsInf(number, -1) {
		s.negativeInfinity = true

		return
	}

	if number > 0 {
		s.positiveFinite = true
	} else if number < 0 {
		s.negativeFinite = true
	}

	s.maximumMagnitude = max(s.maximumMagnitude, math.Abs(number))
	s.finiteCount++

	if s.positiveFinite && s.negativeFinite {
		return
	}

	nextMean := s.sameSignMean + (number-s.sameSignMean)/float64(s.finiteCount)
	if nextMean == s.sameSignMean && number != s.sameSignMean {
		s.sameSignReliable = false
	}

	s.sameSignMean = nextMean
}

func scaledSameSignMean(
	ctx context.Context,
	values sequenceView,
	maximumMagnitude, divisor float64,
) (float64, error) {
	total, correction := 0.0, 0.0

	for position := range values.len() {
		err := contextCheck(ctx)
		if err != nil {
			return 0, err
		}

		number, err := float64FromReflect(ctx, values.value.Index(position))
		if err != nil {
			return 0, err
		}

		addCompensated(&total, &correction, number/maximumMagnitude)
	}

	return ((total + correction) / divisor) * maximumMagnitude, nil
}

func addCompensated(total, correction *float64, value float64) {
	next := *total + value
	if math.Abs(*total) >= math.Abs(value) {
		*correction += (*total - next) + value
	} else {
		*correction += (value - next) + *total
	}

	*total = next
}

func medianBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	values, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	if values.len() == 0 {
		return nil, newDetailError("median requires a non-empty sequence")
	}

	numbers := make([]float64, values.len())

	for position := range values.len() {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		numbers[position], err = float64FromReflect(ctx, values.value.Index(position))
		if err != nil {
			return nil, err
		}

		if math.IsNaN(numbers[position]) {
			return math.NaN(), nil
		}
	}

	sort.Float64s(numbers)

	middle := len(numbers) / medianPairSize
	if len(numbers)%2 == 1 {
		return numbers[middle], nil
	}

	return averagePair(numbers[middle-1], numbers[middle]), nil
}

func float64FromReflect(ctx context.Context, value reflect.Value) (float64, error) {
	switch {
	case isSignedKind(value.Kind()):
		integer := value.Int()

		number := float64(integer)
		if int64(number) != integer {
			return 0, newDetailError("%v cannot be represented exactly as float64", value.Interface())
		}

		return number, nil
	case isUnsignedKind(value.Kind()):
		integer := value.Uint()

		number := float64(integer)
		if uint64(number) != integer {
			return 0, newDetailError("%v cannot be represented exactly as float64", value.Interface())
		}

		return number, nil
	case isFloatKind(value.Kind()):
		return value.Float(), nil
	default:
		return float64Value(ctx, value.Interface())
	}
}

func averagePair(left, right float64) float64 {
	if left == right {
		return left
	}

	if math.IsInf(left, 0) || math.IsInf(right, 0) {
		return (left + right) / medianPairSize
	}

	sum := left + right
	if !math.IsInf(sum, 0) {
		return sum / medianPairSize
	}

	return left/medianPairSize + right/medianPairSize
}

func edgeBuiltin(last bool) builtinFunc {
	return func(ctx context.Context, arguments []any) (any, error) {
		err := checkArity(arguments, 1, 1)
		if err != nil {
			return nil, err
		}

		values, err := newSequenceView(arguments[0])
		if err != nil {
			return nil, err
		}

		if values.len() == 0 {
			return nil, nil
		}

		if last {
			return values.rawValueAt(ctx, values.len()-1)
		}

		return values.rawValueAt(ctx, 0)
	}
}

func takeBuiltin(ctx context.Context, arguments []any) (any, error) {
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

	count, err := integerArgument(ctx, arguments[1])
	if err != nil {
		return nil, err
	}

	if count >= 0 {
		if count > len(values) {
			count = len(values)
		}

		return append(rawSequence{}, values[:count]...), nil
	}

	count = -count
	if count < 0 {
		return nil, newDetailError("take count %d is outside supported range", count)
	}

	if count > len(values) {
		count = len(values)
	}

	return append(rawSequence{}, values[len(values)-count:]...), nil
}

func reverseBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
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

	for position := range len(values) / 2 {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		opposite := len(values) - 1 - position
		values[position], values[opposite] = values[opposite], values[position]
	}

	return values, nil
}

func sortBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
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

	items := normalizedSortItems(values)

	if items != nil {
		err = sortKeyedValues(ctx, items)
		if err != nil {
			return nil, err
		}

		return extractSortedValues(ctx, values, items)
	}

	err = sortRawValues(ctx, values)
	if err != nil {
		return nil, err
	}

	return values, nil
}

func normalizedSortItems(values []any) []keyedValue {
	var items []keyedValue

	for position, value := range values {
		key := normalizeValue(value)
		if items == nil && reflect.TypeOf(key) != reflect.TypeOf(value) {
			items = make([]keyedValue, len(values))
			for previous := range position {
				items[previous] = keyedValue{value: values[previous], key: values[previous]}
			}
		}

		if items != nil {
			items[position] = keyedValue{value: value, key: key}
		}
	}

	return items
}

func sortRawValues(ctx context.Context, values []any) error {
	var comparisonErr error

	sort.SliceStable(values, func(position, otherPosition int) bool {
		if comparisonErr != nil {
			return false
		}

		comparisonErr = contextCheck(ctx)
		if comparisonErr != nil {
			return false
		}

		var less bool

		less, comparisonErr = lessForSort(values[position], values[otherPosition])

		return less
	})

	return comparisonErr
}
