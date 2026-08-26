package fasteval

import (
	"context"
	"go/constant"
	gotoken "go/token"
	"maps"
	"math"
	"math/bits"
	"reflect"
	"time"
)

type equalityHashState uint8

type equalityMapDecision uint8

const (
	equalityHashUnavailable equalityHashState = iota
	equalityHashAlwaysDistinct
	equalityHashRequiresFullScan
	equalityHashAvailable
)

const (
	equalityMapInconclusive equalityMapDecision = iota
	equalityMapUnequal
	equalityMapEqual
)

type equalityHashKey struct {
	number  equalityNumericKey
	scalar  any
	numeric bool
}

type equalityNumericKey struct {
	realMagnitude      uint64
	imaginaryMagnitude uint64
	realExponent       equalityComponentCode
	imaginaryExponent  equalityComponentCode
}

func emptyEqualityNumericKey() equalityNumericKey {
	return equalityNumericKey{
		realMagnitude: 0, imaginaryMagnitude: 0,
		realExponent: 0, imaginaryExponent: 0,
	}
}

type literalMapKey struct {
	real      string
	imaginary string
}

type literalMapKeyIndex struct {
	exact            map[literalMapKey]any
	literals         []numberLiteral
	adaptedLiterals  map[any]any
	concreteKeys     map[any]any
	concreteKeyTypes map[reflect.Type]struct{}
}

type scalarMapValue struct {
	text      string
	primary   uint64
	secondary uint64
}

type equalityMapEntry struct {
	key   reflect.Value
	value reflect.Value
}

// equalityComponentCode stores finite exponents in [1, 2098]. The tag bits
// are outside that range and identify infinity and negative components.
type equalityComponentCode int16

const (
	equalityExponentOffset = 1075
	equalityInfinityCode   = equalityComponentCode(0x2000)
	equalityNegativeCode   = equalityComponentCode(0x4000)
	float64SignShift       = 63
	float64ExponentShift   = 52
	float64ExponentMask    = 0x7ff
	float64ExponentBias    = 1023
	float64SubnormalPower  = -1074
)

func equalValuesContext(ctx context.Context, left, right any) (bool, error) {
	if leftString, ok := left.(string); ok {
		if rightString, ok := right.(string); ok {
			cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
			if cancellationErr != nil {
				return false, cancellationErr
			}

			return leftString == rightString, nil
		}
	}

	return equalValuesSeen(ctx, left, right, make(map[equalityVisit]bool), 0)
}

func makeEqualityHashKey(value any) (equalityHashKey, equalityHashState) {
	if _, literal := value.(numberLiteral); literal {
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashRequiresFullScan
	}

	value = normalizeValue(value)
	if value == nil {
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashAvailable
	}

	if _, duration := value.(time.Duration); duration {
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: value, numeric: false}, equalityHashAvailable
	}

	reflected := reflect.ValueOf(value)
	if isNumericType(reflected.Type()) {
		number, _ := numericComplex128Reflect(reflected)
		if math.IsNaN(real(number)) || math.IsNaN(imag(number)) {
			return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashAlwaysDistinct
		}

		return equalityHashKey{
			number: makeEqualityNumericKey(reflected, number), scalar: nil, numeric: true,
		}, equalityHashAvailable
	}

	if !reflected.Comparable() {
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashUnavailable
	}

	switch reflected.Kind() {
	case reflect.Bool, reflect.Chan, reflect.String, reflect.UnsafePointer:
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: value, numeric: false}, equalityHashAvailable
	case reflect.Invalid,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.Struct:
	}

	return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashUnavailable
}

func makeEqualityHashKeyReflect(value reflect.Value) (equalityHashKey, equalityHashState) {
	value, valid := unwrapReflectInterfaces(value)
	if !valid {
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashAvailable
	}

	if value.Type() == reflect.TypeFor[time.Duration]() {
		return equalityHashKey{
			number: emptyEqualityNumericKey(), scalar: time.Duration(value.Int()), numeric: false,
		}, equalityHashAvailable
	}

	if isNumericType(value.Type()) {
		return makeNumericEqualityHashKey(value)
	}

	switch value.Kind() {
	case reflect.Bool:
		return equalityHashKey{
			number: emptyEqualityNumericKey(), scalar: value.Bool(), numeric: false,
		}, equalityHashAvailable
	case reflect.String:
		return equalityHashKey{
			number: emptyEqualityNumericKey(), scalar: value.String(), numeric: false,
		}, equalityHashAvailable
	case reflect.Invalid,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.Struct, reflect.UnsafePointer:
	}

	if !value.Comparable() {
		return equalityHashKey{number: emptyEqualityNumericKey(), scalar: nil, numeric: false}, equalityHashUnavailable
	}

	return makeEqualityHashKey(value.Interface())
}

func makeNumericEqualityHashKey(value reflect.Value) (equalityHashKey, equalityHashState) {
	number, _ := numericComplex128Reflect(value)
	if math.IsNaN(real(number)) || math.IsNaN(imag(number)) {
		return equalityHashKey{
			number: emptyEqualityNumericKey(), scalar: nil, numeric: false,
		}, equalityHashAlwaysDistinct
	}

	return equalityHashKey{
		number: makeEqualityNumericKey(value, number), scalar: nil, numeric: true,
	}, equalityHashAvailable
}

func makeEqualityNumericKey(reflected reflect.Value, number complex128) equalityNumericKey {
	var key equalityNumericKey

	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		key.realMagnitude, key.realExponent = signedEqualityComponent(reflected.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		key.realMagnitude, key.realExponent = normalizeEqualityComponent(reflected.Uint(), 0, false)
	case reflect.Float32, reflect.Float64:
		key.realMagnitude, key.realExponent = floatEqualityComponent(reflected.Float())
	case reflect.Complex64, reflect.Complex128:
		key.realMagnitude, key.realExponent = floatEqualityComponent(real(number))
		key.imaginaryMagnitude, key.imaginaryExponent = floatEqualityComponent(imag(number))
	case reflect.Invalid, reflect.Bool, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return key
}

func signedEqualityComponent(value int64) (uint64, equalityComponentCode) {
	if value >= 0 {
		return normalizeEqualityComponent(uint64(value), 0, false)
	}

	// value is negative, so -(value + 1) is non-negative and fits in uint64.
	magnitude := uint64(-(value + 1)) + 1 //nolint:gosec // This handles MinInt64 without signed overflow.

	return normalizeEqualityComponent(magnitude, 0, true)
}

func floatEqualityComponent(value float64) (uint64, equalityComponentCode) {
	if math.IsInf(value, 0) {
		code := equalityInfinityCode
		if math.Signbit(value) {
			code |= equalityNegativeCode
		}

		return 0, code
	}

	representation := math.Float64bits(value)
	negative := representation>>float64SignShift != 0
	exponentBits := int((representation >> float64ExponentShift) & float64ExponentMask)
	magnitude := representation & (uint64(1)<<float64ExponentShift - 1)
	exponent := float64SubnormalPower

	if exponentBits != 0 {
		magnitude |= uint64(1) << float64ExponentShift
		exponent = exponentBits - float64ExponentBias - float64ExponentShift
	}

	return normalizeEqualityComponent(magnitude, exponent, negative)
}

func normalizeEqualityComponent(magnitude uint64, exponent int, negative bool) (uint64, equalityComponentCode) {
	if magnitude == 0 {
		return 0, 0
	}

	shift := bits.TrailingZeros64(magnitude)
	// Finite float64 exponents map to [1, 2098], which fits in equalityComponentCode.
	component := exponent + shift + equalityExponentOffset

	code := equalityComponentCode(component) //nolint:gosec // The float64 exponent range bounds the conversion.
	if negative {
		code |= equalityNegativeCode
	}

	return magnitude >> shift, code
}

type equalityVisit struct {
	left, right             uintptr
	leftLength, rightLength int
	kind                    reflect.Kind
}

func equalValuesSeen(ctx context.Context, left, right any, seen map[equalityVisit]bool, depth int) (bool, error) {
	depthErr := checkValueDepth(depth)
	if depthErr != nil {
		return false, depthErr
	}

	cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
	if cancellationErr != nil {
		return false, cancellationErr
	}

	if leftString, ok := left.(string); ok {
		if rightString, ok := right.(string); ok {
			return leftString == rightString, nil
		}
	}

	if equal, bothLiterals := equalNumberLiterals(left, right); bothLiterals {
		return equal, nil
	}

	return equalNormalizedValues(ctx, left, right, seen, depth)
}

func equalNormalizedValues(
	ctx context.Context,
	left, right any,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	left = normalizeValue(equalityValue(left, right))
	right = normalizeValue(equalityValue(right, left))

	if result, handled := equalScalarValues(left, right); handled {
		return result, nil
	}

	if left == nil || right == nil {
		return left == nil && right == nil, nil
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)

	if isSequenceKind(leftValue.Kind()) && isSequenceKind(rightValue.Kind()) {
		return equalSequenceValues(ctx, leftValue, rightValue, seen, depth)
	}

	if leftValue.Kind() == reflect.Map && rightValue.Kind() == reflect.Map {
		return equalMapValues(ctx, leftValue, rightValue, seen, depth)
	}

	return reflect.DeepEqual(left, right), nil
}

func equalNumberLiterals(left, right any) (bool, bool) {
	leftLiteral, leftOK := left.(numberLiteral)

	rightLiteral, rightOK := right.(numberLiteral)

	if !leftOK || !rightOK {
		return false, false
	}

	return constant.Compare(leftLiteral.exactValue(), gotoken.EQL, rightLiteral.exactValue()), true
}

func existingEquivalentMapKey[T any](values map[any]T, literals *literalMapKeyIndex, key any) (any, bool) {
	if _, found := values[key]; found {
		return key, true
	}

	literal, ok := key.(numberLiteral)
	if ok {
		return literals.findOrAddLiteral(literal)
	}

	normalized := normalizeValue(key)

	target := reflect.TypeOf(normalized)
	if target == nil {
		return nil, false
	}

	if !isNumericType(target) && target.Kind() != reflect.Bool && target.Kind() != reflect.String {
		return nil, false
	}

	return literals.findOrAddConcreteKey(key, normalized, target)
}

func (i *literalMapKeyIndex) findOrAddLiteral(literal numberLiteral) (any, bool) {
	exactValue := literal.exactValue()

	canonical := literalMapKey{
		real:      constant.Real(exactValue).ExactString(),
		imaginary: constant.Imag(exactValue).ExactString(),
	}
	if existing, found := i.exact[canonical]; found {
		return existing, true
	}

	for target := range i.concreteKeyTypes {
		adapted, ok := adaptedLiteralMapKey(literal, target)
		if !ok {
			continue
		}

		if existing, found := i.concreteKeys[adapted]; found {
			return existing, true
		}
	}

	if i.exact == nil {
		i.exact = make(map[literalMapKey]any)
	}

	i.exact[canonical] = literal

	i.literals = append(i.literals, literal)

	for target := range i.concreteKeyTypes {
		i.addAdaptedLiteral(literal, target)
	}

	return nil, false
}

func (i *literalMapKeyIndex) findOrAddConcreteKey(key, normalized any, target reflect.Type) (any, bool) {
	if i.concreteKeyTypes == nil {
		i.concreteKeyTypes = make(map[reflect.Type]struct{})
	}

	if _, known := i.concreteKeyTypes[target]; !known {
		i.concreteKeyTypes[target] = struct{}{}
		for _, literal := range i.literals {
			i.addAdaptedLiteral(literal, target)
		}
	}

	if existing, found := i.adaptedLiterals[normalized]; found {
		return existing, true
	}

	if i.concreteKeys == nil {
		i.concreteKeys = make(map[any]any)
	}

	if existing, found := i.concreteKeys[normalized]; found {
		return existing, true
	}

	i.concreteKeys[normalized] = key

	return nil, false
}

func (i *literalMapKeyIndex) addAdaptedLiteral(literal numberLiteral, target reflect.Type) {
	adapted, ok := adaptedLiteralMapKey(literal, target)
	if !ok {
		return
	}

	if i.adaptedLiterals == nil {
		i.adaptedLiterals = make(map[any]any)
	}

	if _, found := i.adaptedLiterals[adapted]; !found {
		i.adaptedLiterals[adapted] = literal
	}
}

func adaptedLiteralMapKey(literal numberLiteral, target reflect.Type) (any, bool) {
	converted, err := convertNumberLiteral(literal, target)
	if err != nil {
		return nil, false
	}

	return converted.Interface(), true
}

func equalityValue(value, other any) any {
	literal, ok := value.(numberLiteral)
	if !ok {
		return value
	}

	target := reflect.TypeOf(normalizeValue(other))
	if isNumericType(target) {
		converted, err := convertNumberLiteral(literal, target)
		if err == nil {
			return converted.Interface()
		}
	}

	materialized, err := materializeLiteral(literal, emptySpan())
	if err != nil {
		return value
	}

	return materialized
}

func equalScalarValues(left, right any) (bool, bool) {
	if leftTime, ok := left.(time.Time); ok {
		rightTime, rightOK := right.(time.Time)

		return rightOK && leftTime.Equal(rightTime), true
	}

	if equal, handled := equalDurationValues(left, right); handled {
		return equal, true
	}

	leftType, rightType := reflect.TypeOf(left), reflect.TypeOf(right)
	if leftType == rightType && isNumericType(leftType) {
		return left == right, true
	}

	if isNumericType(leftType) && isNumericType(rightType) {
		return equalNumeric(left, right), true
	}

	return false, false
}

func equalDurationValues(left, right any) (bool, bool) {
	leftDuration, leftIsDuration := left.(time.Duration)
	rightDuration, rightIsDuration := right.(time.Duration)

	if !leftIsDuration && !rightIsDuration {
		return false, false
	}

	return leftIsDuration && rightIsDuration && leftDuration == rightDuration, true
}

func isSequenceKind(kind reflect.Kind) bool {
	return kind == reflect.Array || kind == reflect.Slice
}

func equalSequenceValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	if left.Len() != right.Len() {
		return false, nil
	}

	if left.Kind() == reflect.Slice && right.Kind() == reflect.Slice && equalitySeen(left, right, reflect.Slice, seen) {
		return true, nil
	}

	for position := range left.Len() {
		equal, err := equalReflectValues(ctx, left.Index(position), right.Index(position), seen, depth+1)
		if err != nil || !equal {
			return false, err
		}
	}

	return true, nil
}

func equalMapValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	if left.Len() != right.Len() {
		return false, nil
	}

	if equalitySeen(left, right, reflect.Map, seen) {
		return true, nil
	}

	equal, handled, err := equalSameKeyMapValues(ctx, left, right, seen, depth)
	if err != nil || handled {
		return equal, err
	}

	if bothScalarMapKeys(left, right) {
		decision, err := equalHashedMapValues(ctx, left, right, seen, depth)
		if err != nil {
			return false, err
		}

		switch decision {
		case equalityMapEqual:
			return true, nil
		case equalityMapUnequal:
			return false, nil
		case equalityMapInconclusive:
		}
	}

	return equalSemanticMapValues(ctx, left, right, seen, depth)
}

func bothScalarMapKeys(left, right reflect.Value) bool {
	return scalarMapKind(left.Type().Key().Kind()) && scalarMapKind(right.Type().Key().Kind())
}

func equalSameKeyMapValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, bool, error) {
	if left.Type().Key() != right.Type().Key() {
		return false, false, nil
	}

	keyKind := left.Type().Key().Kind()
	if left.Type().Elem() == right.Type().Elem() &&
		scalarMapKind(keyKind) && scalarMapKind(left.Type().Elem().Kind()) {
		equal, err := equalScalarMapValues(ctx, left, right)

		return equal, true, err
	}

	if scalarMapKind(keyKind) {
		equal, err := equalExactMapValues(ctx, left, right, seen, depth)

		return equal, true, err
	}

	equal, err := equalExactMapValues(ctx, left, right, copyEqualitySeen(seen), depth)

	return equal, equal || err != nil, err
}

func equalHashedMapValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (equalityMapDecision, error) {
	if left.Type().Elem() == right.Type().Elem() && scalarMapKind(left.Type().Elem().Kind()) {
		return equalHashedScalarMapValues(ctx, left, right)
	}

	rightEntries, decision, err := indexHashedMapValues(ctx, right)
	if err != nil || decision != equalityMapEqual {
		return decision, err
	}

	return compareHashedMapValues(ctx, left, rightEntries, seen, depth)
}

func indexHashedMapValues(
	ctx context.Context,
	right reflect.Value,
) (map[equalityHashKey]reflect.Value, equalityMapDecision, error) {
	rightEntries := make(map[equalityHashKey]reflect.Value, right.Len())
	rightIterator := right.MapRange()
	rightKey := reflect.New(right.Type().Key()).Elem()

	for rightIterator.Next() {
		cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
		if cancellationErr != nil {
			return nil, equalityMapUnequal, cancellationErr
		}

		rightKey.SetIterKey(rightIterator)

		key, state := makeEqualityHashKeyReflect(rightKey)
		switch state {
		case equalityHashAlwaysDistinct:
			return nil, equalityMapUnequal, nil
		case equalityHashAvailable:
			if _, duplicate := rightEntries[key]; duplicate {
				return nil, equalityMapInconclusive, nil
			}

			rightEntries[key] = rightIterator.Value()
		case equalityHashUnavailable, equalityHashRequiresFullScan:
			return nil, equalityMapInconclusive, nil
		}
	}

	return rightEntries, equalityMapEqual, nil
}

func compareHashedMapValues(
	ctx context.Context,
	left reflect.Value,
	rightEntries map[equalityHashKey]reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (equalityMapDecision, error) {
	leftIterator := left.MapRange()
	leftKey := reflect.New(left.Type().Key()).Elem()

	for leftIterator.Next() {
		cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
		if cancellationErr != nil {
			return equalityMapUnequal, cancellationErr
		}

		leftKey.SetIterKey(leftIterator)

		key, state := makeEqualityHashKeyReflect(leftKey)
		if state == equalityHashAlwaysDistinct {
			return equalityMapUnequal, nil
		}

		if state != equalityHashAvailable {
			return equalityMapInconclusive, nil
		}

		rightValue, found := rightEntries[key]
		if !found {
			return equalityMapUnequal, nil
		}

		equal, err := equalReflectValues(ctx, leftIterator.Value(), rightValue, seen, depth+1)
		if err != nil || !equal {
			return equalityMapUnequal, err
		}
	}

	return equalityMapEqual, nil
}

func equalHashedScalarMapValues(ctx context.Context, left, right reflect.Value) (equalityMapDecision, error) {
	valueKind := right.Type().Elem().Kind()

	rightEntries, decision, err := indexHashedScalarMapValues(ctx, right, valueKind)
	if err != nil || decision != equalityMapEqual {
		return decision, err
	}

	return compareHashedScalarMapValues(ctx, left, rightEntries, valueKind)
}

func indexHashedScalarMapValues(
	ctx context.Context,
	right reflect.Value,
	valueKind reflect.Kind,
) (map[equalityHashKey]scalarMapValue, equalityMapDecision, error) {
	rightEntries := make(map[equalityHashKey]scalarMapValue, right.Len())
	rightIterator := right.MapRange()
	rightKey := reflect.New(right.Type().Key()).Elem()
	rightValue := reflect.New(right.Type().Elem()).Elem()

	for rightIterator.Next() {
		cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
		if cancellationErr != nil {
			return nil, equalityMapUnequal, cancellationErr
		}

		rightKey.SetIterKey(rightIterator)
		rightValue.SetIterValue(rightIterator)

		key, keyState := makeEqualityHashKeyReflect(rightKey)

		value, valueComparable := makeScalarMapValue(rightValue, valueKind)

		if keyState == equalityHashAlwaysDistinct || !valueComparable {
			return nil, equalityMapUnequal, nil
		}

		if keyState != equalityHashAvailable {
			return nil, equalityMapInconclusive, nil
		}

		if _, duplicate := rightEntries[key]; duplicate {
			return nil, equalityMapInconclusive, nil
		}

		rightEntries[key] = value
	}

	return rightEntries, equalityMapEqual, nil
}

func compareHashedScalarMapValues(
	ctx context.Context,
	left reflect.Value,
	rightEntries map[equalityHashKey]scalarMapValue,
	valueKind reflect.Kind,
) (equalityMapDecision, error) {
	leftIterator := left.MapRange()
	leftKey := reflect.New(left.Type().Key()).Elem()
	leftValue := reflect.New(left.Type().Elem()).Elem()

	for leftIterator.Next() {
		cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
		if cancellationErr != nil {
			return equalityMapUnequal, cancellationErr
		}

		leftKey.SetIterKey(leftIterator)
		leftValue.SetIterValue(leftIterator)

		key, keyState := makeEqualityHashKeyReflect(leftKey)

		value, valueComparable := makeScalarMapValue(leftValue, valueKind)

		if keyState == equalityHashAlwaysDistinct || !valueComparable {
			return equalityMapUnequal, nil
		}

		if keyState != equalityHashAvailable {
			return equalityMapInconclusive, nil
		}

		rightValue, found := rightEntries[key]
		if !found || rightValue != value {
			return equalityMapUnequal, nil
		}
	}

	return equalityMapEqual, nil
}

func equalExactMapValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	iterator := left.MapRange()
	key := reflect.New(left.Type().Key()).Elem()
	leftEntry := reflect.New(left.Type().Elem()).Elem()
	rightEntry := reflect.New(right.Type().Elem()).Elem()

	for iterator.Next() {
		key.SetIterKey(iterator)
		leftEntry.SetIterValue(iterator)

		entry := right.MapIndex(key)
		if !entry.IsValid() {
			return false, nil
		}

		rightEntry.Set(entry)

		equal, err := equalReflectValues(ctx, leftEntry, rightEntry, seen, depth+1)
		if err != nil || !equal {
			return false, err
		}
	}

	return true, nil
}

func equalSemanticMapValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	leftEntries := equalityMapEntries(left)
	rightEntries := equalityMapEntries(right)

	matchedRight := make([]int, len(rightEntries))
	for index := range matchedRight {
		matchedRight[index] = -1
	}

	for leftIndex := range leftEntries {
		matched, err := matchSemanticMapEntry(
			ctx, leftIndex, leftEntries, rightEntries, matchedRight, make([]bool, len(rightEntries)), seen, depth,
		)
		if err != nil || !matched {
			return false, err
		}
	}

	return true, nil
}

func equalityMapEntries(value reflect.Value) []equalityMapEntry {
	entries := make([]equalityMapEntry, 0, value.Len())

	iterator := value.MapRange()

	for iterator.Next() {
		entries = append(entries, equalityMapEntry{key: iterator.Key(), value: iterator.Value()})
	}

	return entries
}

func matchSemanticMapEntry(
	ctx context.Context,
	leftIndex int,
	leftEntries, rightEntries []equalityMapEntry,
	matchedRight []int,
	visitedRight []bool,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	leftEntry := leftEntries[leftIndex]

	for rightIndex, rightEntry := range rightEntries {
		if visitedRight[rightIndex] {
			continue
		}

		candidateSeen := copyEqualitySeen(seen)

		keysEqual, err := equalReflectValues(ctx, leftEntry.key, rightEntry.key, candidateSeen, depth+1)
		if err != nil {
			return false, err
		}

		if !keysEqual {
			continue
		}

		valuesEqual, err := equalReflectValues(ctx, leftEntry.value, rightEntry.value, candidateSeen, depth+1)
		if err != nil {
			return false, err
		}

		if !valuesEqual {
			continue
		}

		visitedRight[rightIndex] = true

		previousLeft := matchedRight[rightIndex]

		if previousLeft >= 0 {
			rematched, rematchErr := matchSemanticMapEntry(
				ctx, previousLeft, leftEntries, rightEntries, matchedRight, visitedRight, seen, depth,
			)
			if rematchErr != nil {
				return false, rematchErr
			}

			if !rematched {
				continue
			}
		}

		matchedRight[rightIndex] = leftIndex

		return true, nil
	}

	return false, nil
}

func copyEqualitySeen(seen map[equalityVisit]bool) map[equalityVisit]bool {
	result := make(map[equalityVisit]bool, len(seen))
	maps.Copy(result, seen)

	return result
}

func scalarMapKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	case reflect.Invalid, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.Struct, reflect.UnsafePointer:
	}

	return false
}

func equalScalarMapValues(ctx context.Context, left, right reflect.Value) (bool, error) {
	keyKind := left.Type().Key().Kind()

	valueKind := left.Type().Elem().Kind()

	if singleWordScalarKind(keyKind) && singleWordScalarKind(valueKind) {
		return equalIndexedScalarMapValues(
			ctx, left, right, keyKind, valueKind, makeScalarMapWord,
		)
	}

	return equalIndexedScalarMapValues(
		ctx, left, right, keyKind, valueKind, makeScalarMapValue,
	)
}

func singleWordScalarKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Invalid, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return false
}

func equalIndexedScalarMapValues[T comparable](
	ctx context.Context,
	left, right reflect.Value,
	keyKind, valueKind reflect.Kind,
	makeScalar func(reflect.Value, reflect.Kind) (T, bool),
) (bool, error) {
	rightEntries, ok, err := indexScalarMap(ctx, right, keyKind, valueKind, makeScalar)
	if err != nil || !ok {
		return false, err
	}

	return compareIndexedScalarMap(ctx, left, rightEntries, keyKind, valueKind, makeScalar)
}

func indexScalarMap[T comparable](
	ctx context.Context,
	right reflect.Value,
	keyKind, valueKind reflect.Kind,
	makeScalar func(reflect.Value, reflect.Kind) (T, bool),
) (map[T]T, bool, error) {
	rightEntries := make(map[T]T, right.Len())
	rightIterator := right.MapRange()
	rightKey := reflect.New(right.Type().Key()).Elem()
	rightValue := reflect.New(right.Type().Elem()).Elem()

	for rightIterator.Next() {
		cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
		if cancellationErr != nil {
			return nil, false, cancellationErr
		}

		rightKey.SetIterKey(rightIterator)
		rightValue.SetIterValue(rightIterator)

		key, keyComparable := makeScalar(rightKey, keyKind)

		value, valueComparable := makeScalar(rightValue, valueKind)

		if !keyComparable || !valueComparable {
			return nil, false, nil
		}

		rightEntries[key] = value
	}

	return rightEntries, true, nil
}

func compareIndexedScalarMap[T comparable](
	ctx context.Context,
	left reflect.Value,
	rightEntries map[T]T,
	keyKind, valueKind reflect.Kind,
	makeScalar func(reflect.Value, reflect.Kind) (T, bool),
) (bool, error) {
	leftIterator := left.MapRange()
	leftKey := reflect.New(left.Type().Key()).Elem()
	leftValue := reflect.New(left.Type().Elem()).Elem()

	for leftIterator.Next() {
		cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
		if cancellationErr != nil {
			return false, cancellationErr
		}

		leftKey.SetIterKey(leftIterator)
		leftValue.SetIterValue(leftIterator)

		key, keyComparable := makeScalar(leftKey, keyKind)

		value, valueComparable := makeScalar(leftValue, valueKind)

		if !keyComparable || !valueComparable {
			return false, nil
		}

		rightValue, found := rightEntries[key]
		if !found || rightValue != value {
			return false, nil
		}
	}

	return true, nil
}

func makeScalarMapWord(value reflect.Value, kind reflect.Kind) (uint64, bool) {
	switch kind {
	case reflect.Bool:
		if value.Bool() {
			return 1, true
		}

		return 0, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(value.Int()), true //nolint:gosec // The bit pattern is the equality hash representation.
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint(), true
	case reflect.Float32, reflect.Float64:
		floating := value.Float()
		if math.IsNaN(floating) {
			return 0, false
		}

		if floating == 0 {
			floating = 0
		}

		return math.Float64bits(floating), true
	case reflect.Invalid, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return 0, false
}

func makeScalarMapValue(value reflect.Value, kind reflect.Kind) (scalarMapValue, bool) {
	switch kind {
	case reflect.Bool:
		if value.Bool() {
			return scalarMapValue{text: "", primary: 1, secondary: 0}, true
		}

		return scalarMapValue{text: "", primary: 0, secondary: 0}, true
	case reflect.String:
		return scalarMapValue{text: value.String(), primary: 0, secondary: 0}, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return scalarMapValue{
			text:      "",
			primary:   uint64(value.Int()), //nolint:gosec // The map comparison key uses the bit pattern.
			secondary: 0,
		}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return scalarMapValue{text: "", primary: value.Uint(), secondary: 0}, true
	case reflect.Float32, reflect.Float64:
		return floatScalarMapValue(value.Float())
	case reflect.Complex64, reflect.Complex128:
		return complexScalarMapValue(value.Complex())
	case reflect.Invalid, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.Struct, reflect.UnsafePointer:
	}

	return scalarMapValue{text: "", primary: 0, secondary: 0}, false
}

func floatScalarMapValue(value float64) (scalarMapValue, bool) {
	if math.IsNaN(value) {
		return scalarMapValue{text: "", primary: 0, secondary: 0}, false
	}

	if value == 0 {
		value = 0
	}

	return scalarMapValue{text: "", primary: math.Float64bits(value), secondary: 0}, true
}

func complexScalarMapValue(value complex128) (scalarMapValue, bool) {
	realPart := real(value)
	imaginaryPart := imag(value)

	if math.IsNaN(realPart) || math.IsNaN(imaginaryPart) {
		return scalarMapValue{text: "", primary: 0, secondary: 0}, false
	}

	if realPart == 0 {
		realPart = 0
	}

	if imaginaryPart == 0 {
		imaginaryPart = 0
	}

	return scalarMapValue{
		text: "", primary: math.Float64bits(realPart), secondary: math.Float64bits(imaginaryPart),
	}, true
}

func equalReflectValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	depthErr := checkValueDepth(depth)
	if depthErr != nil {
		return false, depthErr
	}

	cancellationErr := contextCheckAt(ctx, "compare values", emptySpan())
	if cancellationErr != nil {
		return false, cancellationErr
	}

	left, _ = unwrapReflectInterfaces(left)
	right, _ = unwrapReflectInterfaces(right)

	result, handled, err := equalSpecialReflectValues(ctx, left, right, seen, depth)
	if err != nil || handled {
		return result, err
	}

	return equalConcreteReflectValues(ctx, left, right, seen, depth)
}

func equalSpecialReflectValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, bool, error) {
	leftNil := reflectValueNil(left)
	rightNil := reflectValueNil(right)

	if leftNil || rightNil {
		return leftNil && rightNil, true, nil
	}

	literalType := reflect.TypeFor[numberLiteral]()
	if left.Type() == literalType || right.Type() == literalType {
		equal, err := equalValuesSeen(ctx, left.Interface(), right.Interface(), seen, depth)

		return equal, true, err
	}

	durationType := reflect.TypeFor[time.Duration]()
	if left.Type() == durationType || right.Type() == durationType {
		equal := left.Type() == durationType && right.Type() == durationType && left.Int() == right.Int()

		return equal, true, nil
	}

	return equalTimeReflectValues(left, right)
}

func reflectValueNil(value reflect.Value) bool {
	return !value.IsValid() || nilable(value.Kind()) && reflectValueIsNil(value)
}

func equalTimeReflectValues(left, right reflect.Value) (bool, bool, error) {
	timeType := reflect.TypeFor[time.Time]()
	if left.Type() != timeType && right.Type() != timeType {
		return false, false, nil
	}

	if left.Type() != timeType || right.Type() != timeType {
		return false, true, nil
	}

	leftTime, leftOK := left.Interface().(time.Time)

	rightTime, rightOK := right.Interface().(time.Time)
	if !leftOK || !rightOK {
		return false, true, nil
	}

	return leftTime.Equal(rightTime), true, nil
}

func equalConcreteReflectValues(
	ctx context.Context,
	left, right reflect.Value,
	seen map[equalityVisit]bool,
	depth int,
) (bool, error) {
	if isNumericType(left.Type()) && isNumericType(right.Type()) {
		return equalNumericReflect(left, right), nil
	}

	if isSequenceKind(left.Kind()) && isSequenceKind(right.Kind()) {
		return equalSequenceValues(ctx, left, right, seen, depth)
	}

	if left.Kind() == reflect.Map && right.Kind() == reflect.Map {
		return equalMapValues(ctx, left, right, seen, depth)
	}

	if left.Kind() == right.Kind() {
		if equal, handled := equalSameKindReflectValues(left, right); handled {
			return equal, nil
		}
	}

	if left.Type() != right.Type() {
		return false, nil
	}

	return reflect.DeepEqual(left.Interface(), right.Interface()), nil
}

func equalSameKindReflectValues(left, right reflect.Value) (bool, bool) {
	switch left.Kind() {
	case reflect.Bool:
		return left.Bool() == right.Bool(), true
	case reflect.String:
		return left.String() == right.String(), true
	case reflect.Invalid,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.Struct, reflect.UnsafePointer:
	}

	return false, false
}

func equalNumericReflect(left, right reflect.Value) bool {
	leftNumber, leftOK := numericComplex128Reflect(left)

	rightNumber, rightOK := numericComplex128Reflect(right)

	if !leftOK || !rightOK ||
		math.IsNaN(real(leftNumber)) || math.IsNaN(imag(leftNumber)) ||
		math.IsNaN(real(rightNumber)) || math.IsNaN(imag(rightNumber)) {
		return false
	}

	return makeEqualityNumericKey(left, leftNumber) == makeEqualityNumericKey(right, rightNumber)
}

func numericComplex128Reflect(value reflect.Value) (complex128, bool) {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return complex(float64(value.Int()), 0), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return complex(float64(value.Uint()), 0), true
	case reflect.Float32, reflect.Float64:
		return complex(value.Float(), 0), true
	case reflect.Complex64, reflect.Complex128:
		return value.Complex(), true
	case reflect.Invalid, reflect.Bool, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return 0, false
}

func equalitySeen(left, right reflect.Value, kind reflect.Kind, seen map[equalityVisit]bool) bool {
	visit := equalityVisit{
		left: left.Pointer(), right: right.Pointer(), leftLength: 0, rightLength: 0, kind: kind,
	}
	if kind == reflect.Slice {
		visit.leftLength = left.Len()
		visit.rightLength = right.Len()
	}

	if visit.left == 0 || visit.right == 0 {
		return false
	}

	if seen[visit] {
		return true
	}

	seen[visit] = true

	return false
}

func equalNumeric(left, right any) bool {
	leftKey, leftState := makeEqualityHashKey(left)
	rightKey, rightState := makeEqualityHashKey(right)

	return leftState == equalityHashAvailable && rightState == equalityHashAvailable && leftKey == rightKey
}
