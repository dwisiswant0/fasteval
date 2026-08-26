package fasteval

import (
	"context"
	"reflect"
)

type rawSequence []any
type rawMap map[any]any
type rawGroupedMap map[any]rawSequence

func keysBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value := reflect.ValueOf(arguments[0])
	if !value.IsValid() || value.Kind() != reflect.Map {
		return nil, newDetailError("value has type %T, want map", arguments[0])
	}

	result := make(rawSequence, value.Len())
	position := 0

	iterator := value.MapRange()
	for iterator.Next() {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		result[position] = iterator.Key().Interface()
		position++
	}

	return result, nil
}

func valuesBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value := reflect.ValueOf(arguments[0])
	if !value.IsValid() || value.Kind() != reflect.Map {
		return nil, newDetailError("value has type %T, want map", arguments[0])
	}

	result := make(rawSequence, 0, value.Len())

	iterator := value.MapRange()
	for iterator.Next() {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		result = append(result, iterator.Value().Interface())
	}

	return result, nil
}

func toPairsBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value := reflect.ValueOf(arguments[0])
	if !value.IsValid() || value.Kind() != reflect.Map {
		return nil, newDetailError("value has type %T, want map", arguments[0])
	}

	result := make(rawSequence, 0, value.Len())

	iterator := value.MapRange()
	for iterator.Next() {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		result = append(result, rawSequence{
			iterator.Key().Interface(),
			iterator.Value().Interface(),
		})
	}

	return result, nil
}

func fromPairsBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	pairs, err := newSequenceView(arguments[0])
	if err != nil {
		return nil, err
	}

	result := make(rawMap, pairs.len())

	var literalKeys literalMapKeyIndex

	for position := range pairs.len() {
		key, value, err := pairValues(ctx, pairs, position)
		if err != nil {
			return nil, err
		}

		if !comparableMapKey(key) {
			return nil, newDetailError("pair %d has non-comparable key type %T", position, key)
		}

		if _, duplicate := existingEquivalentMapKey(result, &literalKeys, key); duplicate {
			return nil, newDetailError("pair %d duplicates key %#v", position, key)
		}

		result[key] = value
	}

	return result, nil
}

func pairValues(ctx context.Context, pairs sequenceView, position int) (any, any, error) {
	pair, err := pairs.rawValueAt(ctx, position)
	if err != nil {
		return nil, nil, err
	}

	values, err := newSequenceView(pair)
	if err != nil {
		return nil, nil, err
	}

	if values.len() != twoArguments {
		return nil, nil, newDetailError("pair %d must contain exactly two values", position)
	}

	key, err := values.rawValueAt(ctx, 0)
	if err != nil {
		return nil, nil, err
	}

	if isNilValue(key) {
		key = nil
	}

	value, err := values.rawValueAt(ctx, 1)

	return key, value, err
}

func lengthBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value := reflect.ValueOf(arguments[0])
	if !value.IsValid() {
		return 0, nil
	}

	switch value.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return value.Len(), nil
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Func, reflect.Interface, reflect.Pointer, reflect.Struct, reflect.UnsafePointer:
		return nil, newDetailError("value has type %T, which has no length", arguments[0])
	}

	return nil, newDetailError("value has unknown reflection kind %v", value.Kind())
}

func getBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, twoArguments, threeArguments)
	if err != nil {
		return nil, err
	}

	fallback := any(nil)
	if len(arguments) == threeArguments {
		fallback = arguments[2]
	}

	rawMapValue := arguments[0]
	literalFallback := isLiteralMap(rawMapValue)

	value, valid, indirectErr := indirectValue(rawMapValue)
	if indirectErr != nil {
		return nil, indirectErr
	}

	if !valid {
		return fallback, nil
	}

	switch value.Kind() {
	case reflect.Map:
		return getMapValue(value, arguments[1], fallback, literalFallback), nil
	case reflect.Array, reflect.Slice, reflect.String:
		return getSequenceValue(value, arguments[1], fallback), nil
	case reflect.Struct:
		return getStructValue(arguments[0], arguments[1], fallback), nil
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer, reflect.UnsafePointer:
	}

	return fallback, nil
}

func getMapValue(value reflect.Value, rawKey, fallback any, literalFallback bool) any {
	entry, found, err := lookupMapEntry(value, rawKey, literalFallback)
	if err != nil || !found {
		return fallback
	}

	return entry.Interface()
}

func getSequenceValue(value reflect.Value, rawIndex, fallback any) any {
	index, err := indexInteger(rawIndex)
	if err != nil || index < 0 || index >= value.Len() {
		return fallback
	}

	if value.Kind() == reflect.String {
		return value.String()[index]
	}

	return value.Index(index).Interface()
}

func getStructValue(receiver, rawName, fallback any) any {
	name, valid := normalizeValue(rawName).(string)
	if !valid {
		return fallback
	}

	accessed, err := accessValue(receiver, name, false, emptySpan(), rawResult)
	if err != nil {
		return fallback
	}

	return accessed
}
