package fasteval

import (
	"math"
	"reflect"
	"time"
)

func accessValue(receiver any, name string, optional bool, span Span, mode evaluationResultMode) (any, error) {
	value, valid, err := indirectValue(receiver)
	if err != nil {
		return nil, evalError(ErrAccess, "access value", span, name, err)
	}

	if !valid {
		return nilAccessValue(optional, name, span)
	}

	if optional && nilable(value.Kind()) && reflectValueIsNil(value) {
		return nil, nil
	}

	if value.Kind() == reflect.Map {
		return accessMapValue(value, name, span, mode, isLiteralMap(receiver))
	}

	if value.Kind() == reflect.Struct {
		return accessStructValue(receiver, value, name, span, mode)
	}

	method, found, methodErr := accessMethodValue(receiver, value, name, span)
	if found || methodErr != nil {
		return method, methodErr
	}

	return nil, evalError(ErrAccess, "access field", span, name, newDetailError("receiver has type %T", receiver))
}

func nilAccessValue(optional bool, name string, span Span) (any, error) {
	if optional {
		return nil, nil
	}

	return nil, evalError(ErrAccess, "access value", span, name, newDetailError("receiver is nil"))
}

func indirectValue(input any) (reflect.Value, bool, error) {
	if isNilValue(input) {
		return reflect.Value{}, false, nil
	}

	value := reflect.ValueOf(input)

	var (
		firstPointer uintptr
		visited      map[uintptr]struct{}
	)

	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false, nil
		}

		if value.Kind() == reflect.Pointer {
			err := trackPointer(value.Pointer(), &firstPointer, &visited)
			if err != nil {
				return reflect.Value{}, false, err
			}
		}

		value = value.Elem()
	}

	return value, true, nil
}

func trackPointer(identity uintptr, firstPointer *uintptr, visited *map[uintptr]struct{}) error {
	if *firstPointer == 0 {
		*firstPointer = identity

		return nil
	}

	if identity == *firstPointer {
		return newDetailError("value contains a pointer cycle")
	}

	if *visited == nil {
		*visited = map[uintptr]struct{}{*firstPointer: {}}
	}

	if _, found := (*visited)[identity]; found {
		return newDetailError("value contains a pointer cycle")
	}

	(*visited)[identity] = struct{}{}

	return nil
}

func accessMapValue(
	value reflect.Value,
	name string,
	span Span,
	mode evaluationResultMode,
	literalFallback bool,
) (any, error) {
	keyType := value.Type().Key()
	if keyType.Kind() != reflect.String && keyType.Kind() != reflect.Interface {
		return nil, evalError(
			ErrAccess, "access map", span, name,
			newDetailError("dot access requires a string map key, got %v", keyType),
		)
	}

	key := reflect.ValueOf(name)
	if !key.Type().AssignableTo(keyType) {
		if !key.Type().ConvertibleTo(keyType) {
			return nil, evalError(
				ErrAccess, "access map", span, name,
				newDetailError("cannot construct key type %v", keyType),
			)
		}

		key = key.Convert(keyType)
	}

	entry := value.MapIndex(key)
	if !entry.IsValid() && literalFallback {
		entry, _, _ = lookupEquivalentEvaluatorMapKey(value, name)
	}

	if !entry.IsValid() {
		return nil, evalError(ErrAccess, "access map", span, name, newDetailError("key is not present"))
	}

	return accessResultValue(entry.Interface(), mode), nil
}

func accessStructValue(
	receiver any,
	value reflect.Value,
	name string,
	span Span,
	mode evaluationResultMode,
) (any, error) {
	if fieldInfo, found := value.Type().FieldByName(name); found {
		if fieldInfo.PkgPath != "" {
			return nil, evalError(ErrAccess, "access field", span, name, newDetailError("field is not exported"))
		}

		field, err := value.FieldByIndexErr(fieldInfo.Index)
		if err != nil {
			return nil, evalError(ErrAccess, "access field", span, name, err)
		}

		if !field.CanInterface() {
			return nil, evalError(ErrAccess, "access field", span, name, newDetailError("field cannot be accessed"))
		}

		return accessResultValue(field.Interface(), mode), nil
	}

	method, found, err := accessMethodValue(receiver, value, name, span)
	if found || err != nil {
		return method, err
	}

	return nil, evalError(
		ErrAccess, "access field or method", span, name,
		newDetailError("member is not present or exported"),
	)
}

func accessMethodValue(receiver any, value reflect.Value, name string, span Span) (any, bool, error) {
	method := reflect.ValueOf(receiver).MethodByName(name)
	if !method.IsValid() {
		method = value.MethodByName(name)
	}

	if !method.IsValid() && value.CanAddr() {
		method = value.Addr().MethodByName(name)
	}

	if !method.IsValid() {
		return nil, false, nil
	}

	function, err := newReflectCallable(name, method)
	if err != nil {
		return nil, true, evalError(ErrFunction, "prepare method", span, name, err)
	}

	return function, true, nil
}

func indexValue(receiver, index any, span Span, mode evaluationResultMode) (any, error) {
	value, valid, err := indirectValue(receiver)
	if err != nil {
		return nil, evalError(ErrAccess, "index value", span, "", err)
	}

	if !valid {
		return nil, evalError(ErrAccess, "index value", span, "", newDetailError("receiver is nil"))
	}

	switch value.Kind() {
	case reflect.String, reflect.Array, reflect.Slice:
		return indexSequenceValue(value, index, span, mode)
	case reflect.Map:
		return indexMapValue(value, index, span, mode, isLiteralMap(receiver))
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer, reflect.Struct, reflect.UnsafePointer:
		return nil, evalError(ErrType, "index value", span, "", newDetailError("value has type %T", receiver))
	}

	return nil, evalError(ErrType, "index value", span, "", newDetailError("unknown value kind %v", value.Kind()))
}

func indexSequenceValue(value reflect.Value, index any, span Span, mode evaluationResultMode) (any, error) {
	position, err := indexInteger(index)
	if err != nil {
		return nil, evalError(ErrType, "convert index", span, "", err)
	}

	if position < 0 || position >= value.Len() {
		return nil, evalError(
			ErrAccess, "index value", span, "",
			newDetailError("index %d is outside length %d", position, value.Len()),
		)
	}

	if value.Kind() == reflect.String {
		return value.String()[position], nil
	}

	return accessResultValue(value.Index(position).Interface(), mode), nil
}

func indexMapValue(
	value reflect.Value,
	index any,
	span Span,
	mode evaluationResultMode,
	literalFallback bool,
) (any, error) {
	entry, found, err := lookupMapEntry(value, index, literalFallback)
	if err != nil {
		return nil, evalError(ErrType, "convert map key", span, "", err)
	}

	if !found {
		return nil, evalError(ErrAccess, "index map", span, "", newDetailError("key %#v is not present", index))
	}

	return accessResultValue(entry.Interface(), mode), nil
}

func accessResultValue(value any, mode evaluationResultMode) any {
	if mode == normalizedResult {
		return normalizeValue(value)
	}

	return value
}

func lookupMapEntry(value reflect.Value, index any, literalFallback bool) (reflect.Value, bool, error) {
	key, err := exactMapKey(index, value.Type().Key())
	if err != nil {
		return reflect.Value{}, false, err
	}

	entry := value.MapIndex(key)
	if entry.IsValid() || value.Type().Key().Kind() != reflect.Interface {
		return entry, entry.IsValid(), nil
	}

	if !literalFallback {
		return reflect.Value{}, false, nil
	}

	return lookupEquivalentEvaluatorMapKey(value, index)
}

// lookupEquivalentEvaluatorMapKey repairs only the evaluator's preserved-literal
// representation after exact Go-key lookup has failed.
func lookupEquivalentEvaluatorMapKey(value reflect.Value, index any) (reflect.Value, bool, error) {
	if literal, ok := index.(numberLiteral); ok {
		return lookupEquivalentLiteralMapKey(value, literal)
	}

	normalized := normalizeValue(index)
	target := reflect.TypeOf(index)

	normalizedType := reflect.TypeOf(normalized)
	if normalizedType != nil && (normalizedType.Kind() == reflect.Bool || normalizedType.Kind() == reflect.String) {
		return lookupEquivalentScalarMapKey(value, normalized, normalizedType)
	}

	if !isNumericType(target) {
		return reflect.Value{}, false, nil
	}

	iterator := value.MapRange()
	for iterator.Next() {
		literal, ok := iterator.Key().Interface().(numberLiteral)
		if !ok {
			continue
		}

		adapted, conversionErr := convertNumberLiteral(literal, target)
		if conversionErr == nil && adapted.Interface() == index {
			return iterator.Value(), true, nil
		}
	}

	return reflect.Value{}, false, nil
}

func lookupEquivalentLiteralMapKey(value reflect.Value, literal numberLiteral) (reflect.Value, bool, error) {
	literalKey := reflect.ValueOf(literal)
	if literalKey.Type().AssignableTo(value.Type().Key()) {
		entry := value.MapIndex(literalKey)
		if entry.IsValid() {
			return entry, true, nil
		}
	}

	iterator := value.MapRange()
	for iterator.Next() {
		candidate := iterator.Key()
		for candidate.Kind() == reflect.Interface && !candidate.IsNil() {
			candidate = candidate.Elem()
		}

		if !candidate.IsValid() || !isNumericType(candidate.Type()) {
			continue
		}

		adapted, conversionErr := convertNumberLiteral(literal, candidate.Type())
		if conversionErr == nil && adapted.Interface() == candidate.Interface() {
			return iterator.Value(), true, nil
		}
	}

	return reflect.Value{}, false, nil
}

func lookupEquivalentScalarMapKey(
	value reflect.Value,
	normalized any,
	normalizedType reflect.Type,
) (reflect.Value, bool, error) {
	iterator := value.MapRange()
	for iterator.Next() {
		candidate := normalizeValue(iterator.Key().Interface())
		if reflect.TypeOf(candidate) == normalizedType && candidate == normalized {
			return iterator.Value(), true, nil
		}
	}

	return reflect.Value{}, false, nil
}

func exactMapKey(index any, target reflect.Type) (reflect.Value, error) {
	if index == nil {
		if nilable(target.Kind()) {
			return reflect.Zero(target), nil
		}

		return reflect.Value{}, newDetailError("nil is not a valid %v map key", target)
	}

	if target.Kind() == reflect.Interface {
		return exactInterfaceMapKey(index, target)
	}

	if literal, ok := index.(numberLiteral); ok {
		return convertNumberLiteral(literal, target)
	}

	value := reflect.ValueOf(index)
	if !value.IsValid() {
		return reflect.Value{}, newDetailError("nil is not a valid map key")
	}

	if value.Type() != target {
		return reflect.Value{}, newDetailError("key has type %v, want exact type %v", value.Type(), target)
	}

	return value, nil
}

func exactInterfaceMapKey(index any, target reflect.Type) (reflect.Value, error) {
	if literal, ok := index.(numberLiteral); ok {
		materialized, err := materializeLiteral(literal, emptySpan())
		if err != nil {
			return reflect.Value{}, err
		}

		index = materialized
	}

	value := reflect.ValueOf(index)
	if !value.IsValid() || !value.Comparable() || !value.Type().AssignableTo(target) {
		return reflect.Value{}, newDetailError("key has non-comparable type %T", index)
	}

	return value, nil
}

func comparableMapKey(value any) bool {
	reflected := reflect.ValueOf(value)

	return !reflected.IsValid() || reflected.Comparable()
}

func indexInteger(index any) (int, error) {
	if literal, ok := index.(numberLiteral); ok {
		value, err := convertNumberLiteral(literal, reflect.TypeFor[int]())
		if err != nil {
			return 0, err
		}

		return int(value.Int()), nil
	}

	value := reflect.ValueOf(normalizeValue(index))
	if !value.IsValid() {
		return 0, newDetailError("index has type %T, want integer", index)
	}

	if isSignedKind(value.Kind()) {
		return checkedInt(value.Int())
	}

	if isUnsignedKind(value.Kind()) {
		return checkedUintIndex(value.Uint())
	}

	return 0, newDetailError("index has type %T, want integer", index)
}

func checkedInt(value int64) (int, error) {
	if value < math.MinInt || value > math.MaxInt {
		return 0, newDetailError("index %d cannot fit in int", value)
	}

	return int(value), nil
}

func checkedUintIndex(value uint64) (int, error) {
	if value > math.MaxInt {
		return 0, newDetailError("index %d cannot fit in int", value)
	}

	return int(value), nil
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}

	return reflectValueIsNil(reflect.ValueOf(value))
}

func reflectValueIsNil(reflected reflect.Value) bool {
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	case reflect.UnsafePointer:
		return reflected.Pointer() == 0
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.String, reflect.Struct:
	}

	return false
}

func normalizeValue(value any) any {
	switch value.(type) {
	case bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64, complex64, complex128,
		time.Duration, time.Time:
		return value
	}

	if isNilValue(value) {
		return nil
	}

	if values, ok := value.(rawSequence); ok {
		for index, item := range values {
			values[index] = normalizeValue(item)
		}

		return []any(values)
	}

	reflected := reflect.ValueOf(value)

	typeOf := reflected.Type()
	if typeOf.PkgPath() == "" {
		return value
	}

	switch reflected.Kind() {
	case reflect.Bool:
		return reflected.Bool()
	case reflect.String:
		return reflected.String()
	case reflect.Invalid,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.Struct, reflect.UnsafePointer:
	}

	baseType := numericBaseType(reflected.Kind())
	if baseType == nil {
		return value
	}

	return reflected.Convert(baseType).Interface()
}

func numericBaseType(kind reflect.Kind) reflect.Type {
	switch {
	case isSignedKind(kind):
		return signedBaseType(kind)
	case isUnsignedKind(kind):
		return unsignedBaseType(kind)
	case isFloatKind(kind):
		return floatBaseType(kind)
	case isComplexKind(kind):
		return complexBaseType(kind)
	default:
		return nil
	}
}

func signedBaseType(kind reflect.Kind) reflect.Type {
	switch int(kind) {
	case int(reflect.Int):
		return reflect.TypeFor[int]()
	case int(reflect.Int8):
		return reflect.TypeFor[int8]()
	case int(reflect.Int16):
		return reflect.TypeFor[int16]()
	case int(reflect.Int32):
		return reflect.TypeFor[int32]()
	default:
		return reflect.TypeFor[int64]()
	}
}

func unsignedBaseType(kind reflect.Kind) reflect.Type {
	switch int(kind) {
	case int(reflect.Uint):
		return reflect.TypeFor[uint]()
	case int(reflect.Uint8):
		return reflect.TypeFor[uint8]()
	case int(reflect.Uint16):
		return reflect.TypeFor[uint16]()
	case int(reflect.Uint32):
		return reflect.TypeFor[uint32]()
	case int(reflect.Uint64):
		return reflect.TypeFor[uint64]()
	default:
		return reflect.TypeFor[uintptr]()
	}
}

func floatBaseType(kind reflect.Kind) reflect.Type {
	if kind == reflect.Float32 {
		return reflect.TypeFor[float32]()
	}

	return reflect.TypeFor[float64]()
}

func complexBaseType(kind reflect.Kind) reflect.Type {
	if kind == reflect.Complex64 {
		return reflect.TypeFor[complex64]()
	}

	return reflect.TypeFor[complex128]()
}
