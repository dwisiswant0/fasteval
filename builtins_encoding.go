package fasteval

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const jsonIntegerBits = 64

func registerEncodingBuiltins(functions map[string]*callable) {
	registerBuiltin(functions, "type", typeBuiltin)
	registerBuiltin(functions, "string", stringBuiltin)
	registerBuiltin(functions, "toJSON", toJSONBuiltin)
	registerBuiltin(functions, "fromJSON", fromJSONBuiltin)
	registerBuiltin(functions, "toBase64", toBase64Builtin)
	registerBuiltin(functions, "fromBase64", fromBase64Builtin)
}

func typeBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value := arguments[0]
	switch value.(type) {
	case rawSequence:
		value = []any(nil)
	case rawMap:
		value = map[any]any(nil)
	case rawGroupedMap:
		value = map[any][]any(nil)
	case numberLiteral:
		var materializeErr error

		value, materializeErr = materializeLiteral(value, emptySpan())
		if materializeErr != nil {
			return nil, materializeErr
		}
	}

	if value == nil {
		return nilKeyword, nil
	}

	return reflect.TypeOf(value).String(), nil
}

func stringBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value, err := materializeLiteral(arguments[0], emptySpan())
	if err != nil {
		return nil, err
	}

	if duration, ok := value.(time.Duration); ok {
		return duration.String(), nil
	}

	if reflected := reflect.ValueOf(value); reflected.IsValid() && reflected.Kind() == reflect.String {
		return reflected.String(), nil
	}

	return formatTemplateValue(value, emptySpan())
}

func toJSONBuiltin(ctx context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	value, err := jsonCompatible(ctx, arguments[0], make(map[collectionIdentity]bool), 0)
	if err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return string(encoded), nil
}

func fromJSONBuiltin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	text, ok := arguments[0].(string)
	if !ok {
		return nil, newDetailError("fromJSON argument has type %T, want string", arguments[0])
	}

	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()

	var result any

	err = decoder.Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}

	var trailing any

	trailingErr := decoder.Decode(&trailing)
	if trailingErr != io.EOF {
		if trailingErr == nil {
			return nil, newDetailError("unmarshal JSON: multiple JSON values")
		}

		return nil, fmt.Errorf("unmarshal JSON: %w", trailingErr)
	}

	result, err = normalizeJSONNumbers(result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func normalizeJSONNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		return jsonNumberValue(typed)
	case []any:
		for position, element := range typed {
			converted, err := normalizeJSONNumbers(element)
			if err != nil {
				return nil, err
			}

			typed[position] = converted
		}
	case map[string]any:
		for key, element := range typed {
			converted, err := normalizeJSONNumbers(element)
			if err != nil {
				return nil, err
			}

			typed[key] = converted
		}
	}

	return value, nil
}

func jsonNumberValue(number json.Number) (any, error) {
	text := number.String()

	floating, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf("decode JSON number %q: %w", text, err)
	}

	if strings.ContainsAny(text, ".eE") {
		return floating, nil
	}

	return jsonIntegerValue(text, floating)
}

func jsonIntegerValue(text string, floating float64) (any, error) {
	integer, signedErr := strconv.ParseInt(text, 10, 64)
	if signedErr == nil {
		return exactSignedJSONNumber(floating, integer), nil
	}

	unsigned, unsignedErr := strconv.ParseUint(text, 10, jsonIntegerBits)
	if unsignedErr == nil {
		return exactUnsignedJSONNumber(floating, unsigned), nil
	}

	return nil, newDetailError("JSON integer %q is outside the supported 64-bit range", text)
}

func exactSignedJSONNumber(floating float64, integer int64) any {
	const signedLimit = float64(1 << 63)

	if floating >= -signedLimit && floating < signedLimit && int64(floating) == integer {
		return floating
	}

	return integer
}

func exactUnsignedJSONNumber(floating float64, unsigned uint64) any {
	unsignedLimit := math.Ldexp(1, jsonIntegerBits)
	if floating >= 0 && floating < unsignedLimit && uint64(floating) == unsigned {
		return floating
	}

	return unsigned
}

func toBase64Builtin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	var value []byte

	switch input := arguments[0].(type) {
	case string:
		value = []byte(input)
	case []byte:
		value = input
	default:
		return nil, newDetailError("toBase64 argument has type %T, want string or []byte", arguments[0])
	}

	return base64.StdEncoding.EncodeToString(value), nil
}

func fromBase64Builtin(_ context.Context, arguments []any) (any, error) {
	err := checkArity(arguments, 1, 1)
	if err != nil {
		return nil, err
	}

	text, ok := arguments[0].(string)
	if !ok {
		return nil, newDetailError("fromBase64 argument has type %T, want string", arguments[0])
	}

	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	return string(decoded), nil
}

type collectionIdentity struct {
	kind    reflect.Kind
	pointer uintptr
	length  int
}

func jsonCompatible(
	ctx context.Context,
	value any,
	visiting map[collectionIdentity]bool,
	depth int,
) (any, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return nil, err
	}

	err = contextCheck(ctx)
	if err != nil {
		return nil, err
	}

	if value == nil {
		return nil, nil
	}

	value, err = materializeJSONLiteral(value)
	if err != nil {
		return nil, err
	}

	reflected := reflect.ValueOf(value)
	if (reflected.Kind() == reflect.Map || reflected.Kind() == reflect.Slice) && reflected.IsNil() {
		return nil, nil
	}

	return jsonCompatibleReflect(ctx, reflected, value, visiting, depth)
}

func jsonCompatibleReflect(
	ctx context.Context,
	reflected reflect.Value,
	value any,
	visiting map[collectionIdentity]bool,
	depth int,
) (any, error) {
	switch reflected.Kind() {
	case reflect.Map:
		return jsonMap(ctx, reflected, visiting, depth)
	case reflect.Array, reflect.Slice:
		return jsonSequence(ctx, reflected, visiting, depth)
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer, reflect.String, reflect.Struct,
		reflect.UnsafePointer:
	}

	return value, nil
}

func materializeJSONLiteral(value any) (any, error) {
	if _, literal := value.(numberLiteral); !literal {
		return value, nil
	}

	return materializeLiteral(value, emptySpan())
}

func jsonMap(
	ctx context.Context,
	reflected reflect.Value,
	visiting map[collectionIdentity]bool,
	depth int,
) (any, error) {
	identity := collectionIdentity{kind: reflected.Kind(), pointer: reflected.Pointer(), length: 0}
	if visiting[identity] {
		return nil, newDetailError("JSON value contains a map cycle")
	}

	visiting[identity] = true
	defer delete(visiting, identity)

	result := make(map[string]any, reflected.Len())
	iterator := reflected.MapRange()

	for iterator.Next() {
		err := contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		key, err := jsonMapKey(iterator.Key())
		if err != nil {
			return nil, err
		}

		if _, duplicate := result[key]; duplicate {
			return nil, newDetailError("JSON map contains duplicate normalized key %q", key)
		}

		converted, err := jsonCompatible(ctx, iterator.Value().Interface(), visiting, depth+1)
		if err != nil {
			return nil, err
		}

		result[key] = converted
	}

	return result, nil
}

func jsonMapKey(value reflect.Value) (string, error) {
	original := value

	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", newDetailError("JSON map key has type %T, want string", original.Interface())
		}

		value = value.Elem()
	}

	if value.Kind() == reflect.String {
		return value.String(), nil
	}

	return "", newDetailError("JSON map key has type %T, want string", original.Interface())
}

func jsonSequence(
	ctx context.Context,
	reflected reflect.Value,
	visiting map[collectionIdentity]bool,
	depth int,
) (any, error) {
	identity, tracked, err := trackJSONSequence(reflected, visiting)
	if err != nil {
		return nil, err
	}

	if tracked {
		defer delete(visiting, identity)
	}

	result := make([]any, reflected.Len())
	for position := range result {
		err = contextCheck(ctx)
		if err != nil {
			return nil, err
		}

		result[position], err = jsonCompatible(ctx, reflected.Index(position).Interface(), visiting, depth+1)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func trackJSONSequence(
	reflected reflect.Value,
	visiting map[collectionIdentity]bool,
) (collectionIdentity, bool, error) {
	identity := collectionIdentity{kind: reflected.Kind(), pointer: 0, length: 0}
	if reflected.Kind() != reflect.Slice || reflected.IsNil() {
		return identity, false, nil
	}

	identity.pointer = reflected.Pointer()

	identity.length = reflected.Len()

	if visiting[identity] {
		return identity, false, newDetailError("JSON value contains a slice cycle")
	}

	visiting[identity] = true

	return identity, true, nil
}
