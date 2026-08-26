package fasteval

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
)

const maxValueDepth = 1_024

func checkValueDepth(depth int) error {
	if depth <= maxValueDepth {
		return nil
	}

	return newDetailError("maximum value depth of %d exceeded", maxValueDepth)
}

type conversionVisit struct {
	source   reflect.Type
	target   reflect.Type
	identity uintptr
	length   int
}

type conversionState struct {
	active         map[conversionVisit]struct{}
	structFields   map[reflect.Type]map[string]int
	scanAssignable bool
}

func convertToWithAssignableScan[T any](ctx context.Context, value any, scanAssignable bool) (T, error) {
	var zero T

	target := reflect.TypeFor[T]()
	if scanAssignable && target.Kind() == reflect.Interface && isLiteralCollection(value) {
		var err error

		value, err = materializeEvaluationValue(contextOrBackground(ctx), value, emptySpan(), 0)
		if err != nil {
			return zero, err
		}

		scanAssignable = false
	}

	converted, err := convertReflectContextWithAssignableScan(ctx, value, target, scanAssignable)
	if err != nil {
		return zero, err
	}

	if converted.Kind() == reflect.Interface && converted.IsNil() {
		return zero, nil
	}

	result, ok := converted.Interface().(T)
	if !ok {
		return zero, newDetailError("converted value has type %T, want %v", converted.Interface(), target)
	}

	return result, nil
}

func convertReflectContext(ctx context.Context, input any, target reflect.Type) (reflect.Value, error) {
	return convertReflectContextWithAssignableScan(ctx, input, target, true)
}

func convertReflectContextWithAssignableScan(
	ctx context.Context,
	input any,
	target reflect.Type,
	scanAssignable bool,
) (reflect.Value, error) {
	state := conversionState{
		active: nil, structFields: nil, scanAssignable: scanAssignable,
	}
	ctx = contextOrBackground(ctx)

	return state.convertAtDepth(ctx, input, target, 0)
}

func (s *conversionState) convertAtDepth(
	ctx context.Context,
	input any,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return reflect.Value{}, err
	}

	err = contextCheckAt(ctx, "convert value", emptySpan())
	if err != nil {
		return reflect.Value{}, err
	}

	if target == nil {
		return reflect.ValueOf(input), nil
	}

	if target.Kind() == reflect.Interface && isLiteralCollection(input) {
		materialized, err := materializeEvaluationValue(ctx, input, emptySpan(), depth)
		if err != nil {
			return reflect.Value{}, err
		}

		input = materialized
	}

	source, converted, err := prepareConversion(input, target, depth)
	if err != nil || converted {
		return source, err
	}

	return s.convertPrepared(ctx, source, target, depth)
}

func (s *conversionState) convertValueTo(
	ctx context.Context,
	source, destination reflect.Value,
	depth int,
) error {
	err := checkValueDepth(depth)
	if err != nil {
		return err
	}

	err = contextCheckAt(ctx, "convert value", emptySpan())
	if err != nil {
		return err
	}

	prepared, converted, err := prepareValueConversion(source, destination.Type(), depth)
	if err != nil {
		return err
	}

	if converted {
		destination.Set(prepared)

		return nil
	}

	target := destination.Type()

	prepared, err = s.prepareInterfaceValue(ctx, prepared, target, depth)
	if err != nil {
		return err
	}

	assigned, err := s.setAssignableDestination(ctx, prepared, destination, depth)
	if err != nil || assigned {
		return err
	}

	if integerConversion(prepared.Type(), target) {
		return convertIntegerInto(prepared, destination)
	}

	convertedValue, err := s.convertPrepared(ctx, prepared, target, depth)
	if err != nil {
		return err
	}

	destination.Set(convertedValue)

	return nil
}

func (s *conversionState) prepareInterfaceValue(
	ctx context.Context,
	prepared reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	if !s.scanAssignable || target.Kind() != reflect.Interface {
		return prepared, nil
	}

	value := prepared.Interface()
	if isLiteralCollection(value) {
		materialized, err := materializeEvaluationValue(ctx, value, emptySpan(), depth)
		if err != nil {
			return reflect.Value{}, err
		}

		return reflect.ValueOf(materialized), nil
	}

	return reflect.ValueOf(normalizeValue(value)), nil
}

func (s *conversionState) setAssignableDestination(
	ctx context.Context,
	prepared, destination reflect.Value,
	depth int,
) (bool, error) {
	if !prepared.Type().AssignableTo(destination.Type()) {
		return false, nil
	}

	if !s.scanAssignable {
		destination.Set(prepared)

		return true, nil
	}

	containsLiteral, err := s.containsPreservedLiteral(ctx,
		prepared, make(map[conversionVisit]struct{}), depth,
	)
	if err != nil || containsLiteral {
		return false, err
	}

	destination.Set(prepared)

	return true, nil
}

func integerConversion(source, target reflect.Type) bool {
	return isNumericType(source) && isNumericType(target) &&
		(isSignedKind(source.Kind()) || isUnsignedKind(source.Kind())) &&
		(isSignedKind(target.Kind()) || isUnsignedKind(target.Kind()))
}

func prepareValueConversion(source reflect.Value, target reflect.Type, depth int) (reflect.Value, bool, error) {
	for source.IsValid() && source.Kind() == reflect.Interface {
		if source.IsNil() {
			return convertNil(target)
		}

		source = source.Elem()
	}

	if !source.IsValid() {
		return convertNil(target)
	}

	if source.Type() == reflect.TypeFor[numberLiteral]() {
		return prepareConversion(source.Interface(), target, depth)
	}

	if nilable(source.Kind()) && reflectValueIsNil(source) {
		return convertNil(target)
	}

	return source, false, nil
}

func (s *conversionState) containsPreservedLiteral(
	ctx context.Context,
	value reflect.Value,
	active map[conversionVisit]struct{},
	depth int,
) (bool, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return false, err
	}

	err = contextCheckAt(ctx, "convert value", emptySpan())
	if err != nil {
		return false, err
	}

	value, valid := unwrapReflectInterfaces(value)
	if !valid {
		return false, nil
	}

	literal, scan, err := preservedLiteralValueState(value, depth)
	if err != nil {
		return false, err
	}

	if literal {
		return true, nil
	}

	if !scan {
		return false, nil
	}

	visit, tracked, duplicate := trackConversionValue(value, active)
	if duplicate {
		return false, nil
	}

	if tracked {
		defer delete(active, visit)
	}

	return s.containsPreservedLiteralChildren(ctx, value, active, depth)
}

func unwrapReflectInterfaces(value reflect.Value) (reflect.Value, bool) {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Value{}, false
		}

		value = value.Elem()
	}

	return value, value.IsValid()
}

func preservedLiteralValueState(
	value reflect.Value,
	depth int,
) (bool, bool, error) {
	mayContain, err := typeMayContainNumberLiteral(
		value.Type(), make(map[reflect.Type]bool), depth,
	)
	if err != nil || !mayContain {
		return false, false, err
	}

	if value.Type() == reflect.TypeFor[numberLiteral]() {
		return true, false, nil
	}

	if nilable(value.Kind()) && reflectValueIsNil(value) {
		return false, false, nil
	}

	return false, true, nil
}

func trackConversionValue(
	value reflect.Value,
	active map[conversionVisit]struct{},
) (conversionVisit, bool, bool) {
	identity, length, tracked := conversionIdentity(value)
	if !tracked {
		return conversionVisit{source: nil, target: nil, identity: 0, length: 0}, false, false
	}

	visit := conversionVisit{source: value.Type(), target: nil, identity: identity, length: length}
	if _, found := active[visit]; found {
		return conversionVisit{source: nil, target: nil, identity: 0, length: 0}, false, true
	}

	active[visit] = struct{}{}

	return visit, true, false
}

func (s *conversionState) containsPreservedLiteralChildren(
	ctx context.Context,
	value reflect.Value,
	active map[conversionVisit]struct{},
	depth int,
) (bool, error) {
	switch value.Kind() {
	case reflect.Array, reflect.Slice:
		return s.sequenceContainsPreservedLiteral(ctx, value, active, depth)
	case reflect.Map:
		return s.mapContainsPreservedLiteral(ctx, value, active, depth)
	case reflect.Pointer:
		return s.containsPreservedLiteral(ctx, value.Elem(), active, depth+1)
	case reflect.Struct:
		return s.structContainsPreservedLiteral(ctx, value, active, depth)
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.UnsafePointer:
	}

	return false, nil
}

func (s *conversionState) sequenceContainsPreservedLiteral(
	ctx context.Context,
	value reflect.Value,
	active map[conversionVisit]struct{},
	depth int,
) (bool, error) {
	for position := range value.Len() {
		found, err := s.containsPreservedLiteral(ctx, value.Index(position), active, depth+1)
		if err != nil || found {
			return found, err
		}
	}

	return false, nil
}

func (s *conversionState) mapContainsPreservedLiteral(
	ctx context.Context,
	value reflect.Value,
	active map[conversionVisit]struct{},
	depth int,
) (bool, error) {
	iterator := value.MapRange()
	for iterator.Next() {
		found, err := s.containsPreservedLiteral(ctx, iterator.Key(), active, depth+1)
		if err != nil || found {
			return found, err
		}

		found, err = s.containsPreservedLiteral(ctx, iterator.Value(), active, depth+1)
		if err != nil || found {
			return found, err
		}
	}

	return false, nil
}

func (s *conversionState) structContainsPreservedLiteral(
	ctx context.Context,
	value reflect.Value,
	active map[conversionVisit]struct{},
	depth int,
) (bool, error) {
	for _, field := range value.Fields() {
		found, err := s.containsPreservedLiteral(ctx, field, active, depth+1)
		if err != nil || found {
			return found, err
		}
	}

	return false, nil
}

func typeMayContainNumberLiteral(
	valueType reflect.Type,
	active map[reflect.Type]bool,
	depth int,
) (bool, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return false, err
	}

	if valueType == reflect.TypeFor[numberLiteral]() || valueType.Kind() == reflect.Interface {
		return true, nil
	}

	if active[valueType] {
		return false, nil
	}

	active[valueType] = true
	defer delete(active, valueType)

	return typeChildrenMayContainNumberLiteral(valueType, active, depth)
}

func typeChildrenMayContainNumberLiteral(
	valueType reflect.Type,
	active map[reflect.Type]bool,
	depth int,
) (bool, error) {
	switch valueType.Kind() {
	case reflect.Array, reflect.Pointer, reflect.Slice:
		return typeMayContainNumberLiteral(valueType.Elem(), active, depth+1)
	case reflect.Map:
		keyMayContain, err := typeMayContainNumberLiteral(valueType.Key(), active, depth+1)
		if err != nil || keyMayContain {
			return keyMayContain, err
		}

		return typeMayContainNumberLiteral(valueType.Elem(), active, depth+1)
	case reflect.Struct:
		for field := range valueType.Fields() {
			mayContain, err := typeMayContainNumberLiteral(field.Type, active, depth+1)
			if err != nil || mayContain {
				return mayContain, err
			}
		}
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.UnsafePointer:
	}

	return false, nil
}

func prepareConversion(input any, target reflect.Type, depth int) (reflect.Value, bool, error) {
	if literal, ok := input.(numberLiteral); ok {
		if target.Kind() == reflect.Interface {
			materialized, err := materializeLiteral(literal, emptySpan())
			if err != nil {
				return reflect.Value{}, true, err
			}

			input = materialized
		} else {
			converted, err := convertNumberLiteralTarget(literal, target, depth)

			return converted, true, err
		}
	}

	if input == nil || isNilValue(input) {
		return convertNil(target)
	}

	source, valid := unwrapInterface(input)
	if !valid {
		return convertNil(target)
	}

	return source, false, nil
}

func convertNumberLiteralTarget(literal numberLiteral, target reflect.Type, depth int) (reflect.Value, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return reflect.Value{}, err
	}

	if target.Kind() == reflect.Interface {
		materialized, err := materializeLiteral(literal, emptySpan())
		if err != nil {
			return reflect.Value{}, err
		}

		value := reflect.ValueOf(materialized)
		if value.IsValid() && value.Type().AssignableTo(target) {
			return value, nil
		}

		return reflect.Value{}, newDetailError("numeric literal cannot be converted to %v", target)
	}

	if target.Kind() != reflect.Pointer {
		return convertNumberLiteral(literal, target)
	}

	element, err := convertNumberLiteralTarget(literal, target.Elem(), depth+1)
	if err != nil {
		return reflect.Value{}, err
	}

	pointer := reflect.New(target.Elem())
	pointer.Elem().Set(element)

	if pointer.Type() == target {
		return pointer, nil
	}

	if pointer.Type().ConvertibleTo(target) {
		return pointer.Convert(target), nil
	}

	return reflect.Value{}, newDetailError("numeric literal pointer %v cannot be converted to %v", pointer.Type(), target)
}

func convertNil(target reflect.Type) (reflect.Value, bool, error) {
	if nilable(target.Kind()) {
		return reflect.Zero(target), true, nil
	}

	return reflect.Value{}, true, newDetailError("nil cannot be converted to %v", target)
}

func (s *conversionState) convertPrepared(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return reflect.Value{}, err
	}

	assignable, done, err := s.preparedAssignableValue(ctx, source, target, depth)
	if err != nil || done {
		return assignable, err
	}

	if isNumericType(source.Type()) && isNumericType(target) {
		return convertNumericReflect(source, target)
	}

	return s.convertTracked(ctx, source, target, depth)
}

func (s *conversionState) preparedAssignableValue(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, bool, error) {
	if !source.Type().AssignableTo(target) {
		return reflect.Value{}, false, nil
	}

	if !s.scanAssignable || !typeCanContainPreservedLiteral(source.Type()) {
		return assignableValue(source, target), true, nil
	}

	containsLiteral, err := s.containsPreservedLiteral(ctx,
		source, make(map[conversionVisit]struct{}), depth,
	)
	if err != nil {
		return reflect.Value{}, false, err
	}

	if !containsLiteral {
		return assignableValue(source, target), true, nil
	}

	if target.Kind() == reflect.Interface {
		value, err := s.convertPrepared(ctx, source, source.Type(), depth)

		return value, true, err
	}

	return reflect.Value{}, false, nil
}

func (s *conversionState) convertTracked(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	identity, length, tracked := conversionIdentity(source)
	if !tracked {
		return s.convertToTarget(ctx, source, target, depth)
	}

	visit := conversionVisit{source: source.Type(), target: target, identity: identity, length: length}
	if _, found := s.active[visit]; found {
		return reflect.Value{}, newDetailError("conversion from %v to %v contains a cycle", source.Type(), target)
	}

	if s.active == nil {
		s.active = make(map[conversionVisit]struct{})
	}

	s.active[visit] = struct{}{}
	defer delete(s.active, visit)

	return s.convertToTarget(ctx, source, target, depth)
}

func typeCanContainPreservedLiteral(valueType reflect.Type) bool {
	if valueType == reflect.TypeFor[numberLiteral]() {
		return true
	}

	switch valueType.Kind() {
	case reflect.Array, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.Struct:
		return true
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.String, reflect.UnsafePointer:
	}

	return false
}

func assignableValue(source reflect.Value, target reflect.Type) reflect.Value {
	if source.Type() == target || target.Kind() == reflect.Interface {
		return source
	}

	return source.Convert(target)
}

func conversionIdentity(source reflect.Value) (uintptr, int, bool) {
	switch source.Kind() {
	case reflect.Slice:
		return source.Pointer(), source.Len(), true
	case reflect.Map, reflect.Pointer:
		return source.Pointer(), 0, true
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.Struct,
		reflect.UnsafePointer:
	}

	return 0, 0, false
}

func unwrapInterface(input any) (reflect.Value, bool) {
	source := reflect.ValueOf(input)
	for source.Kind() == reflect.Interface {
		if source.IsNil() {
			return reflect.Value{}, false
		}

		source = source.Elem()
	}

	return source, true
}

func (s *conversionState) convertToTarget(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	switch target.Kind() {
	case reflect.Pointer:
		return s.convertPointerReflect(ctx, source, target, depth)
	case reflect.Slice:
		return s.convertSequenceReflect(ctx, source, target, depth)
	case reflect.Array:
		return s.convertSequenceReflect(ctx, source, target, depth)
	case reflect.Map:
		return s.convertMapReflect(ctx, source, target, depth)
	case reflect.Struct:
		if source.Kind() != reflect.Map {
			return reflect.Value{}, newDetailError("%v cannot be converted to %v", source.Type(), target)
		}

		return s.convertMapToStruct(ctx, source, target, depth)
	case reflect.Interface:
		if target.NumMethod() == 0 {
			return source, nil
		}
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.String, reflect.UnsafePointer:
	}

	return reflect.Value{}, newDetailError("%v cannot be converted losslessly to %v", source.Type(), target)
}

func (s *conversionState) convertPointerReflect(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	element := source
	if source.Kind() == reflect.Pointer {
		element = source.Elem()
	}

	converted, err := s.convertAtDepth(ctx, element.Interface(), target.Elem(), depth+1)
	if err != nil {
		return reflect.Value{}, err
	}

	pointer := reflect.New(target.Elem())
	pointer.Elem().Set(converted)

	if pointer.Type() != target {
		pointer = pointer.Convert(target)
	}

	return pointer, nil
}

func (s *conversionState) convertSequenceReflect(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	if !isSequenceKind(source.Kind()) {
		return reflect.Value{}, newDetailError("%v cannot be converted to %v", source.Type(), target)
	}

	if target.Kind() == reflect.Array && source.Len() != target.Len() {
		return reflect.Value{}, newDetailError("array has length %d, want %d", source.Len(), target.Len())
	}

	result := reflect.New(target).Elem()
	if target.Kind() == reflect.Slice {
		result = reflect.MakeSlice(target, source.Len(), source.Len())
	}

	if source.Type().Elem().Kind() == reflect.Map && target.Elem().Kind() == reflect.Struct {
		return s.convertMapSequenceToStructs(ctx, source, result, depth)
	}

	for index := range source.Len() {
		err := s.convertValueTo(ctx, source.Index(index), result.Index(index), depth+1)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("element %d: %w", index, err)
		}
	}

	return result, nil
}

func (s *conversionState) convertMapSequenceToStructs(
	ctx context.Context,
	source, result reflect.Value,
	depth int,
) (reflect.Value, error) {
	keyValue := reflect.New(source.Type().Elem().Key()).Elem()
	mapValue := reflect.New(source.Type().Elem().Elem()).Elem()

	for index := range source.Len() {
		cancellationErr := contextCheckAt(ctx, "convert value", emptySpan())
		if cancellationErr != nil {
			return reflect.Value{}, cancellationErr
		}

		err := s.convertMapToStructInto(ctx,
			source.Index(index), result.Index(index), keyValue, mapValue, depth+1,
		)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("element %d: %w", index, err)
		}
	}

	return result, nil
}

func (s *conversionState) convertMapReflect(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	if source.Kind() != reflect.Map {
		return reflect.Value{}, newDetailError("%v cannot be converted to %v", source.Type(), target)
	}

	result := reflect.MakeMapWithSize(target, source.Len())
	iterator := source.MapRange()
	sourceKey := reflect.New(source.Type().Key()).Elem()
	sourceValue := reflect.New(source.Type().Elem()).Elem()
	targetKey := reflect.New(target.Key()).Elem()
	targetValue := reflect.New(target.Elem()).Elem()

	for iterator.Next() {
		sourceKey.SetIterKey(iterator)
		sourceValue.SetIterValue(iterator)

		err := s.convertValueTo(ctx, sourceKey, targetKey, depth+1)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("map key: %w", err)
		}

		err = s.convertValueTo(ctx, sourceValue, targetValue, depth+1)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("map value for %v: %w", sourceKey.Interface(), err)
		}

		if !injectiveMapKeyConversion(source.Type().Key(), target.Key()) && result.MapIndex(targetKey).IsValid() {
			return reflect.Value{}, newDetailError("converted map contains duplicate key %v", targetKey.Interface())
		}

		result.SetMapIndex(targetKey, targetValue)
	}

	return result, nil
}

func injectiveMapKeyConversion(source, target reflect.Type) bool {
	return (isSignedKind(source.Kind()) || isUnsignedKind(source.Kind())) &&
		(isSignedKind(target.Kind()) || isUnsignedKind(target.Kind()))
}

func (s *conversionState) convertMapToStruct(
	ctx context.Context,
	source reflect.Value,
	target reflect.Type,
	depth int,
) (reflect.Value, error) {
	result := reflect.New(target).Elem()
	keyValue := reflect.New(source.Type().Key()).Elem()
	mapValue := reflect.New(source.Type().Elem()).Elem()

	err := s.fillMapToStruct(ctx, source, result, keyValue, mapValue, depth)
	if err != nil {
		return reflect.Value{}, err
	}

	return result, nil
}

func (s *conversionState) convertMapToStructInto(
	ctx context.Context,
	source, result, keyValue, mapValue reflect.Value,
	depth int,
) error {
	err := checkValueDepth(depth)
	if err != nil {
		return err
	}

	if source.IsNil() {
		_, _, err := convertNil(result.Type())

		return err
	}

	identity := source.Pointer()

	visit := conversionVisit{source: source.Type(), target: result.Type(), identity: identity, length: 0}
	if _, found := s.active[visit]; found {
		return newDetailError("conversion from %v to %v contains a cycle", source.Type(), result.Type())
	}

	if s.active == nil {
		s.active = make(map[conversionVisit]struct{})
	}

	s.active[visit] = struct{}{}
	defer delete(s.active, visit)

	return s.fillMapToStruct(ctx, source, result, keyValue, mapValue, depth)
}

func (s *conversionState) fillMapToStruct(
	ctx context.Context,
	source, result, keyValue, mapValue reflect.Value,
	depth int,
) error {
	target := result.Type()
	fields := s.cachedStructFieldAliases(target)
	assigned := make(map[int]string)

	iterator := source.MapRange()
	for iterator.Next() {
		keyValue.SetIterKey(iterator)
		mapValue.SetIterValue(iterator)

		key, ok := reflectStringValue(keyValue)
		if !ok {
			return newDetailError("struct key has type %T, want string", keyValue.Interface())
		}

		fieldIndex, err := structFieldIndex(fields, target, key)
		if err != nil {
			return err
		}

		if previous, exists := assigned[fieldIndex]; exists {
			return newDetailError(
				"keys %q and %q both select field %s", previous, key, target.Field(fieldIndex).Name,
			)
		}

		assigned[fieldIndex] = key

		err = s.setStructField(ctx, result, target, fieldIndex, mapValue, depth+1)
		if err != nil {
			return err
		}
	}

	return nil
}

func reflectStringValue(value reflect.Value) (string, bool) {
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.String {
		return "", false
	}

	return value.String(), true
}

func (s *conversionState) cachedStructFieldAliases(target reflect.Type) map[string]int {
	if fields, found := s.structFields[target]; found {
		return fields
	}

	fields := structFieldAliases(target)

	if s.structFields == nil {
		s.structFields = make(map[reflect.Type]map[string]int)
	}

	s.structFields[target] = fields

	return fields
}

func structFieldAliases(target reflect.Type) map[string]int {
	fields := make(map[string]int)

	for fieldIndex := range target.NumField() {
		field := target.Field(fieldIndex)
		if field.PkgPath != "" {
			continue
		}

		addStructFieldAlias(fields, field.Name, fieldIndex)

		if tag := strings.Split(field.Tag.Get("json"), ",")[0]; tag != "" && tag != "-" {
			addStructFieldAlias(fields, tag, fieldIndex)
		}
	}

	return fields
}

func addStructFieldAlias(fields map[string]int, alias string, fieldIndex int) {
	if previous, exists := fields[alias]; exists && previous != fieldIndex {
		fields[alias] = -1

		return
	}

	fields[alias] = fieldIndex
}

func structFieldIndex(fields map[string]int, target reflect.Type, key string) (int, error) {
	fieldIndex, found := fields[key]
	if !found {
		return 0, newDetailError("struct %v has no exported field for key %q", target, key)
	}

	if fieldIndex < 0 {
		return 0, newDetailError("struct %v has an ambiguous field alias %q", target, key)
	}

	return fieldIndex, nil
}

func (s *conversionState) setStructField(
	ctx context.Context,
	result reflect.Value,
	target reflect.Type,
	fieldIndex int,
	value reflect.Value,
	depth int,
) error {
	field := result.Field(fieldIndex)

	err := s.convertValueTo(ctx, value, field, depth)
	if err != nil {
		return fmt.Errorf("field %s: %w", target.Field(fieldIndex).Name, err)
	}

	return nil
}

func convertNumericReflect(source reflect.Value, target reflect.Type) (reflect.Value, error) {
	if isComplexKind(source.Kind()) || isComplexKind(target.Kind()) {
		return convertComplexReflect(source, target)
	}

	if isFloatKind(source.Kind()) || isFloatKind(target.Kind()) {
		return convertRealReflect(source, target)
	}

	return convertIntegerReflect(source, target)
}

func convertIntegerReflect(source reflect.Value, target reflect.Type) (reflect.Value, error) {
	result := reflect.New(target).Elem()

	err := convertIntegerInto(source, result)
	if err != nil {
		return reflect.Value{}, err
	}

	return result, nil
}

func convertIntegerInto(source, result reflect.Value) error {
	target := result.Type()

	if isSignedKind(source.Kind()) {
		value := source.Int()
		if isSignedKind(target.Kind()) {
			if result.OverflowInt(value) {
				return newDetailError("%v does not fit in %v", source.Interface(), target)
			}

			result.SetInt(value)

			return nil
		}

		if value < 0 || result.OverflowUint(uint64(value)) {
			return newDetailError("%v does not fit in %v", source.Interface(), target)
		}

		result.SetUint(uint64(value))

		return nil
	}

	value := source.Uint()
	if isSignedKind(target.Kind()) {
		if value > math.MaxInt64 || result.OverflowInt(int64(value)) {
			return newDetailError("%v does not fit in %v", source.Interface(), target)
		}

		result.SetInt(int64(value))

		return nil
	}

	if result.OverflowUint(value) {
		return newDetailError("%v does not fit in %v", source.Interface(), target)
	}

	result.SetUint(value)

	return nil
}

func convertRealReflect(source reflect.Value, target reflect.Type) (reflect.Value, error) {
	value, err := realReflectValue(source, target)
	if err != nil {
		return reflect.Value{}, err
	}

	if isFloatKind(target.Kind()) {
		return convertFloatReflect(source, target, value)
	}

	return convertRealToInteger(source, target, value)
}

func realReflectValue(source reflect.Value, target reflect.Type) (float64, error) {
	switch {
	case isFloatKind(source.Kind()):
		return source.Float(), nil
	case isSignedKind(source.Kind()):
		value := float64(source.Int())
		if int64(value) != source.Int() {
			return 0, newDetailError("%v cannot be represented exactly as %v", source.Interface(), target)
		}

		return value, nil
	default:
		value := float64(source.Uint())
		if uint64(value) != source.Uint() {
			return 0, newDetailError("%v cannot be represented exactly as %v", source.Interface(), target)
		}

		return value, nil
	}
}

func convertFloatReflect(source reflect.Value, target reflect.Type, value float64) (reflect.Value, error) {
	result := reflect.New(target).Elem()
	if target.Kind() != reflect.Float32 {
		result.SetFloat(value)

		return result, nil
	}

	narrowed := float32(value)
	if !math.IsNaN(value) && !math.IsInf(value, 0) && float64(narrowed) != value {
		return reflect.Value{}, newDetailError("%v cannot be represented exactly as float32", source.Interface())
	}

	result.SetFloat(float64(narrowed))

	return result, nil
}

func convertRealToInteger(source reflect.Value, target reflect.Type, value float64) (reflect.Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
		return reflect.Value{}, newDetailError("%v is not an exact integer", source.Interface())
	}

	if isSignedKind(target.Kind()) {
		const signedIntegerLimit = float64(1 << 63)

		if value < -signedIntegerLimit || value >= signedIntegerLimit {
			return reflect.Value{}, newDetailError("%v does not fit in %v", source.Interface(), target)
		}

		temporary := reflect.ValueOf(int64(value))

		return convertIntegerReflect(temporary, target)
	}

	if value < 0 || value >= math.MaxUint64 {
		return reflect.Value{}, newDetailError("%v does not fit in %v", source.Interface(), target)
	}

	temporary := reflect.ValueOf(uint64(value))

	return convertIntegerReflect(temporary, target)
}

func convertComplexReflect(source reflect.Value, target reflect.Type) (reflect.Value, error) {
	value, err := complexReflectValue(source)
	if err != nil {
		return reflect.Value{}, err
	}

	if isComplexKind(target.Kind()) {
		return convertComplexTarget(source, target, value)
	}

	if imag(value) != 0 {
		return reflect.Value{}, newDetailError("complex value %v has a non-zero imaginary component", value)
	}

	return convertRealReflect(reflect.ValueOf(real(value)), target)
}

func complexReflectValue(source reflect.Value) (complex128, error) {
	switch {
	case isComplexKind(source.Kind()):
		return source.Complex(), nil
	case isFloatKind(source.Kind()):
		return complex(source.Float(), 0), nil
	case isSignedKind(source.Kind()):
		value := complex(float64(source.Int()), 0)
		if int64(real(value)) != source.Int() {
			return 0, newDetailError("%v cannot be represented exactly as complex", source.Interface())
		}

		return value, nil
	default:
		value := complex(float64(source.Uint()), 0)
		if uint64(real(value)) != source.Uint() {
			return 0, newDetailError("%v cannot be represented exactly as complex", source.Interface())
		}

		return value, nil
	}
}

func convertComplexTarget(source reflect.Value, target reflect.Type, value complex128) (reflect.Value, error) {
	result := reflect.New(target).Elem()
	if target.Kind() != reflect.Complex64 {
		result.SetComplex(value)

		return result, nil
	}

	narrowed := complex64(value)

	if !float64FitsFloat32(real(value)) || !float64FitsFloat32(imag(value)) {
		return reflect.Value{}, newDetailError("%v cannot be represented exactly as complex64", source.Interface())
	}

	result.SetComplex(complex128(narrowed))

	return result, nil
}

func nilable(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice,
		reflect.UnsafePointer:
		return true
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.String, reflect.Struct:
	}

	return false
}

func isSignedKind(kind reflect.Kind) bool {
	return kind >= reflect.Int && kind <= reflect.Int64
}

func isUnsignedKind(kind reflect.Kind) bool {
	return kind >= reflect.Uint && kind <= reflect.Uintptr
}

func isFloatKind(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}

func isComplexKind(kind reflect.Kind) bool {
	return kind == reflect.Complex64 || kind == reflect.Complex128
}

func float64FitsFloat32(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || float64(float32(value)) == value
}
