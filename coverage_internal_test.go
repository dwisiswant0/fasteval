package fasteval

import (
	"context"
	"encoding/json"
	"errors"
	"go/constant"
	gotoken "go/token"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

type coverageNamedInt int
type coverageNamedUint uint

const (
	coverageString = "string"
	coverageValue  = "value"
)

type coverageErrorCase struct {
	name string
	err  error
}

func TestSimpleDecimalConstantMatchesStandardParser(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"0.9", "100.0", ".5", "1.", "01.20", "0.0000000000000000001",
		"9007199254740992.0", "9007199254740993.0", "1844674407370955161.5",
	} {
		got, ok := simpleDecimalConstant(source)
		if !ok {
			t.Fatalf("simpleDecimalConstant(%q) ok = false", source)
		}

		want := constant.MakeFromLiteral(source, gotoken.FLOAT, 0)
		if got.Kind() != want.Kind() || got.ExactString() != want.ExactString() {
			t.Fatalf(
				"simpleDecimalConstant(%q) = %s (%s), want %s (%s)",
				source, got.ExactString(), got.Kind(), want.ExactString(), want.Kind(),
			)
		}
	}

	for _, source := range []string{
		"1", "1e2", "1_0.0", "1.2.3", "18446744073709551616.0", "0.00000000000000000001",
	} {
		if _, ok := simpleDecimalConstant(source); ok {
			t.Fatalf("simpleDecimalConstant(%q) ok = true", source)
		}
	}

	longLeadingZeroDecimal := strings.Repeat("0", 64) + ".9"
	if _, ok := makeSimpleDecimalLiteral(longLeadingZeroDecimal); ok {
		t.Fatalf("makeSimpleDecimalLiteral(%q) ok = true", longLeadingZeroDecimal)
	}
}

func TestSimpleDecimalLiteralEvaluation(t *testing.T) {
	t.Parallel()

	float32Value, err := EvalAs[float32](context.Background(), "0.9", nil)
	if err != nil || float32Value != float32(0.9) {
		t.Fatalf("EvalAs[float32]() = %v, %v; want %v, nil", float32Value, err, float32(0.9))
	}

	complex128Value, err := EvalAs[complex128](context.Background(), "1.5", nil)
	if err != nil || complex128Value != complex(1.5, 0) {
		t.Fatalf("EvalAs[complex128]() = %v, %v; want %v, nil", complex128Value, err, complex(1.5, 0))
	}

	comparison, err := Eval(context.Background(), "value >= 100.0", map[string]any{"value": 100})
	if err != nil || comparison != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", comparison, err)
	}

	longLeadingZeroValue, err := Eval(context.Background(), strings.Repeat("0", 64)+".9", nil)
	if err != nil || longLeadingZeroValue != 0.9 {
		t.Fatalf("Eval(long leading zero decimal) = %#v, %v; want 0.9, nil", longLeadingZeroValue, err)
	}
}

func assertCoverageErrors(t *testing.T, tests []coverageErrorCase) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.err == nil {
				t.Fatal("error = nil")
			}
		})
	}
}

func assertCoverageCanceled(t *testing.T, tests []coverageErrorCase) {
	t.Helper()

	for index := range tests {
		if !errors.Is(tests[index].err, context.Canceled) {
			t.Fatalf("%s error = %v, want context.Canceled", tests[index].name, tests[index].err)
		}
	}
}

func TestInternalNumericDefensivePaths(t *testing.T) {
	t.Parallel()

	integerLiteral := makeNumberLiteral(constant.MakeInt64(2), "2")
	floatLiteral := makeNumberLiteral(constant.MakeFloat64(2.5), "2.5")
	_, notErr := unaryOperation("!", 1, emptySpan())
	_, literalPrefixErr := unaryLiteral("?", integerLiteral, emptySpan())
	_, floatBitwiseErr := unaryLiteral("~", floatLiteral, emptySpan())
	_, nilUnaryErr := unaryNormalized("+", nil, emptySpan())
	_, namedSignedErr := unarySigned("+", coverageNamedInt(1), emptySpan())
	_, namedUnsignedErr := unaryUnsigned("+", coverageNamedUint(1), emptySpan())
	_, signedPrefixErr := unarySignedInteger("?", 1, emptySpan())
	_, negativeUnsignedErr := unaryUnsignedInteger("-", uint(1), emptySpan())
	_, unsignedPrefixErr := unaryUnsignedInteger("?", uint(1), emptySpan())
	_, realPrefixErr := unaryPlusMinus("?", 1.0, emptySpan())
	_, nilShiftErr := numericShift("<<", 1, nil, emptySpan())
	_, durationShiftErr := numericShift("<<", 1, time.Second, emptySpan())
	_, floatShiftErr := numericShift("<<", 1, 1.0, emptySpan())
	_, shiftedFloatErr := numericShift("<<", 1.0, 1, emptySpan())
	_, namedSignedShiftErr := signedIndependentShift("<<", coverageNamedInt(1), 1, emptySpan())
	_, namedUnsignedShiftErr := unsignedIndependentShift("<<", coverageNamedUint(1), 1, emptySpan())
	_, _, mixedStringErr := concatenateStrings("+", "a", 1, emptySpan())
	_, integerOperatorErr := rawIntegerOperation("?", 1, 1)
	_, checkedOperatorErr := checkedIntegerOperation("?", 1, 1)
	_, exponentErr := integerPower(2, -1)
	_, floatOperatorErr := floatOperation("?", 1.0, 1.0, emptySpan())
	_, complexOperatorErr := complexOperation("%", 1+1i, 1+1i, emptySpan())

	assertCoverageErrors(t, []coverageErrorCase{
		{name: "not non-boolean", err: notErr},
		{name: "invalid literal prefix", err: literalPrefixErr},
		{name: "bitwise float literal", err: floatBitwiseErr},
		{name: "nil unary", err: nilUnaryErr},
		{name: "named signed dispatch", err: namedSignedErr},
		{name: "named unsigned dispatch", err: namedUnsignedErr},
		{name: "invalid signed prefix", err: signedPrefixErr},
		{name: "negative unsigned", err: negativeUnsignedErr},
		{name: "invalid unsigned prefix", err: unsignedPrefixErr},
		{name: "invalid real prefix", err: realPrefixErr},
		{name: "nil shift count", err: nilShiftErr},
		{name: "duration shift count", err: durationShiftErr},
		{name: "floating shift count", err: floatShiftErr},
		{name: "floating shifted value", err: shiftedFloatErr},
		{name: "named signed shift", err: namedSignedShiftErr},
		{name: "named unsigned shift", err: namedUnsignedShiftErr},
		{name: "mixed string addition", err: mixedStringErr},
		{name: "invalid integer operator", err: integerOperatorErr},
		{name: "invalid checked operator", err: checkedOperatorErr},
		{name: "negative exponent", err: exponentErr},
		{name: "invalid float operator", err: floatOperatorErr},
		{name: "invalid complex operator", err: complexOperatorErr},
	})

	value, handled, err := concatenateStrings("-", "a", "b", emptySpan())
	if value != nil || handled || err != nil {
		t.Fatalf("non-add string operation = %#v, %t, %v", value, handled, err)
	}

	if value := integerBitwise("?", 1, 1); value != 0 {
		t.Fatalf("invalid integerBitwise() = %d, want 0", value)
	}
}

func TestInternalComparisonDefensivePaths(t *testing.T) {
	t.Parallel()

	_, _, stringErr := compareSpecialValues("<", "a", 1, emptySpan())
	_, _, timeErr := compareSpecialValues("<", time.Now(), 1, emptySpan())
	_, _, durationErr := compareSpecialValues("<", time.Second, 1, emptySpan())
	_, complexErr := compareOperation("<", complex(1, 1), complex(2, 1), emptySpan())

	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "string mismatch", err: stringErr},
		{name: "time mismatch", err: timeErr},
		{name: "duration mismatch", err: durationErr},
		{name: "complex ordering", err: complexErr},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.err == nil {
				t.Fatal("error = nil")
			}
		})
	}
}

func TestInternalComparisonDispatchDefaults(t *testing.T) {
	t.Parallel()

	if result, matched := compareSameType("<", nil, nil); result || matched {
		t.Fatalf("compareSameType(nil) = %t, %t", result, matched)
	}

	if result, matched := compareSameSigned("<", coverageNamedInt(1), coverageNamedInt(2)); result || matched {
		t.Fatalf("compareSameSigned(named) = %t, %t", result, matched)
	}

	if result, matched := compareSameUnsigned("<", coverageNamedUint(1), coverageNamedUint(2)); result || matched {
		t.Fatalf("compareSameUnsigned(named) = %t, %t", result, matched)
	}
}

func TestInternalCompareResultPaths(t *testing.T) {
	t.Parallel()

	for _, operator := range []string{">", ">=", "<", "<=", "?"} {
		t.Run(operator, func(t *testing.T) {
			t.Parallel()

			comparison := compareResult(operator, 1)
			want := operator == ">" || operator == ">="

			if comparison != want {
				t.Fatalf("compareResult(%q) = %t, want %t", operator, comparison, want)
			}
		})
	}
}

func TestInternalAccessAndReflectionHelpers(t *testing.T) {
	t.Parallel()

	first := uintptr(0)

	var visited map[uintptr]struct{}

	err := trackPointer(0, &first, &visited)
	if err != nil {
		t.Fatalf("trackPointer(0) error = %v", err)
	}

	err = trackPointer(1, &first, &visited)
	if err != nil {
		t.Fatalf("trackPointer(first) error = %v", err)
	}

	err = trackPointer(2, &first, &visited)
	if err != nil {
		t.Fatalf("trackPointer(second) error = %v", err)
	}

	err = trackPointer(2, &first, &visited)
	if err == nil {
		t.Fatal("trackPointer(cycle) error = nil")
	}

	got, err := checkedInt(1)
	if err != nil || got != 1 {
		t.Fatalf("checkedInt(1) = %d, %v", got, err)
	}

	_, err = checkedUintIndex(math.MaxUint64)
	if err == nil {
		t.Fatal("checkedUintIndex(MaxUint64) error = nil")
	}
}

func TestInternalNumericBaseTypes(t *testing.T) {
	t.Parallel()

	for kind, want := range map[reflect.Kind]reflect.Type{
		reflect.Int: reflect.TypeFor[int](), reflect.Int8: reflect.TypeFor[int8](),
		reflect.Int16: reflect.TypeFor[int16](), reflect.Int32: reflect.TypeFor[int32](),
		reflect.Int64: reflect.TypeFor[int64](), reflect.Uint: reflect.TypeFor[uint](),
		reflect.Uint8: reflect.TypeFor[uint8](), reflect.Uint16: reflect.TypeFor[uint16](),
		reflect.Uint32: reflect.TypeFor[uint32](), reflect.Uint64: reflect.TypeFor[uint64](),
		reflect.Uintptr: reflect.TypeFor[uintptr](), reflect.Float32: reflect.TypeFor[float32](),
		reflect.Float64: reflect.TypeFor[float64](), reflect.Complex64: reflect.TypeFor[complex64](),
		reflect.Complex128: reflect.TypeFor[complex128](),
	} {
		if got := numericBaseType(kind); got != want {
			t.Fatalf("numericBaseType(%v) = %v, want %v", kind, got, want)
		}
	}

	if got := numericBaseType(reflect.String); got != nil {
		t.Fatalf("numericBaseType(String) = %v, want nil", got)
	}
}

func TestInternalErrorAndArityHelpers(t *testing.T) {
	t.Parallel()

	err := checkArity(nil, 1, 1)
	if err == nil {
		t.Fatal("checkArity() too few error = nil")
	}

	err = checkArity([]any{1, 2}, 1, 1)
	if err == nil {
		t.Fatal("checkArity() too many error = nil")
	}

	err = checkArity([]any{1, 2}, 1, -1)
	if err != nil {
		t.Fatalf("checkArity() unbounded error = %v", err)
	}
}

func TestInternalEvalErrorHelpers(t *testing.T) {
	t.Parallel()

	original := evalError(ErrType, "test", emptySpan(), "path", io.EOF)

	withSpan := withEvalErrorSpan(original, Span{Start: 1, End: 2, Line: 1, Column: 2})
	if !errors.Is(withSpan, io.EOF) {
		t.Fatalf("withEvalErrorSpan() = %#v", withSpan)
	}

	got := withEvalErrorSpan(withSpan, Span{Start: 0, End: 0, Line: 2, Column: 0})

	var gotEval, withSpanEval *EvalError

	gotOK := errors.As(got, &gotEval)
	withSpanOK := errors.As(withSpan, &withSpanEval)

	if !gotOK || !withSpanOK || gotEval.Span() != withSpanEval.Span() {
		t.Fatal("withEvalErrorSpan() replaced an existing span")
	}

	var nilEvalError *EvalError
	if nilEvalError.Error() != "<nil>" {
		t.Fatalf("nil EvalError.Error() = %q", nilEvalError.Error())
	}

	withoutCause := evalError(ErrType, "test", emptySpan(), "", nil)
	if withoutCause.Error() != "test" {
		t.Fatalf("EvalError.Error() = %q, want test", withoutCause.Error())
	}

	err := contextCheck(context.Background())
	if err != nil {
		t.Fatalf("contextCheck() error = %v", err)
	}
}

func TestInternalNumericLiteralPaths(t *testing.T) {
	t.Parallel()

	integer := makeNumberLiteral(constant.MakeInt64(7), "7")
	floating := makeNumberLiteral(constant.MakeFloat64(1.5), "1.5")
	complexValue := constant.BinaryOp(
		constant.MakeInt64(1), gotoken.ADD, constant.MakeImag(constant.MakeInt64(2)),
	)
	complexLiteral := makeNumberLiteral(complexValue, "1+2i")

	for _, testCase := range []struct {
		literal numberLiteral
		want    any
	}{
		{literal: integer, want: int(7)},
		{literal: floating, want: float64(1.5)},
		{literal: complexLiteral, want: complex128(1 + 2i)},
	} {
		got, err := materializeLiteral(testCase.literal, emptySpan())
		if err != nil || got != testCase.want {
			t.Fatalf("materializeLiteral(%s) = %#v, %v; want %#v, nil", testCase.literal.text, got, err, testCase.want)
		}
	}

	unknown := makeNumberLiteral(constant.MakeUnknown(), "unknown")
	hugeInteger := makeNumberLiteral(constant.MakeFromLiteral("0x10000000000000000", gotoken.INT, 0), "huge")

	hugeFloat := makeNumberLiteral(constant.MakeFromLiteral("1e10000", gotoken.FLOAT, 0), "huge-float")
	for _, literal := range []numberLiteral{unknown, hugeInteger, hugeFloat} {
		_, err := materializeLiteral(literal, emptySpan())
		if err == nil {
			t.Fatalf("materializeLiteral(%s) error = nil", literal.text)
		}
	}
}

func TestInternalNumericLiteralOperations(t *testing.T) {
	t.Parallel()

	integer := makeNumberLiteral(constant.MakeInt64(7), "7")
	floating := makeNumberLiteral(constant.MakeFloat64(1.5), "1.5")

	negative := makeNumberLiteral(constant.MakeInt64(-1), "-1")

	_, handled, err := literalPower(integer, negative)
	if !handled || err == nil {
		t.Fatalf("literalPower(negative) = handled %t, error %v", handled, err)
	}

	_, handled, err = literalShift("<<", floating, integer)
	if !handled || err == nil {
		t.Fatalf("literalShift(float) = handled %t, error %v", handled, err)
	}

	got, handled, err := literalShift(">>", negative, integer)
	if !handled || err != nil || got.defaultValue != -1 {
		t.Fatalf("literalShift(saturated) = %#v, %t, %v", got, handled, err)
	}
}

func TestInternalNumericLiteralConversions(t *testing.T) {
	t.Parallel()

	integer := makeNumberLiteral(constant.MakeInt64(7), "7")

	for _, target := range []reflect.Type{
		reflect.TypeFor[int8](), reflect.TypeFor[uint8](), reflect.TypeFor[float32](),
		reflect.TypeFor[float64](), reflect.TypeFor[complex64](), reflect.TypeFor[complex128](),
	} {
		_, err := convertNumberLiteral(integer, target)
		if err != nil {
			t.Fatalf("convertNumberLiteral(int, %v) error = %v", target, err)
		}
	}
}

func TestInternalNumericLiteralConversionErrors(t *testing.T) {
	t.Parallel()

	integer := makeNumberLiteral(constant.MakeInt64(7), "7")
	floating := makeNumberLiteral(constant.MakeFloat64(1.5), "1.5")
	complexValue := constant.BinaryOp(
		constant.MakeInt64(1), gotoken.ADD, constant.MakeImag(constant.MakeInt64(2)),
	)
	complexLiteral := makeNumberLiteral(complexValue, "1+2i")
	hugeFloat := makeNumberLiteral(constant.MakeFromLiteral("1e10000", gotoken.FLOAT, 0), "huge-float")

	for _, testCase := range []struct {
		literal numberLiteral
		target  reflect.Type
	}{
		{literal: floating, target: reflect.TypeFor[int]()},
		{literal: makeNumberLiteral(constant.MakeInt64(-1), "-1"), target: reflect.TypeFor[uint]()},
		{literal: complexLiteral, target: reflect.TypeFor[float64]()},
		{literal: integer, target: reflect.TypeFor[bool]()},
		{literal: hugeFloat, target: reflect.TypeFor[float32]()},
		{literal: hugeFloat, target: reflect.TypeFor[complex64]()},
	} {
		_, err := convertNumberLiteral(testCase.literal, testCase.target)
		if err == nil {
			t.Fatalf("convertNumberLiteral(%s, %v) error = nil", testCase.literal.text, testCase.target)
		}
	}
}

func TestInternalUnsupportedEvaluatorNodes(t *testing.T) {
	t.Parallel()

	state := evalState{
		variables: nil, resolver: nil, functions: nil, preserveLiterals: false,
		stepsUntilContextCheck: evaluationContextCheckInterval,
	}
	invalidKind := NodeKind(255)
	invalidNode := &Node{
		kind: invalidKind, depth: 0, span: emptySpan(), text: "", value: nil, children: nil, optional: false,
	}

	_, err := state.evaluate(context.Background(), invalidNode)
	if err == nil {
		t.Fatal("evaluate(unsupported node) error = nil")
	}

	callNode := &Node{
		kind: NodeCall, depth: 0, span: emptySpan(), text: "", value: nil, children: nil, optional: false,
	}

	_, err = state.evaluateBasicNode(context.Background(), callNode, normalizedResult, true)
	if err == nil {
		t.Fatal("evaluateBasicNode(compound node) error = nil")
	}

	literalNode := &Node{
		kind: NodeLiteral, depth: 0, span: emptySpan(), text: "", value: nil, children: nil, optional: false,
	}

	_, err = state.evaluateCompoundNode(context.Background(), literalNode, normalizedResult)
	if err == nil {
		t.Fatal("evaluateCompoundNode(basic node) error = nil")
	}

	if basicNodeKind(invalidKind) {
		t.Fatal("basicNodeKind(unsupported) = true")
	}
}

func TestInternalEqualityHashClassifications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		state equalityHashState
	}{
		{name: "nil", value: nil, state: equalityHashAvailable},
		{name: "duration", value: time.Second, state: equalityHashAvailable},
		{name: "integer", value: int8(-1), state: equalityHashAvailable},
		{name: "unsigned", value: uint16(1), state: equalityHashAvailable},
		{name: "float", value: float32(1.5), state: equalityHashAvailable},
		{name: "complex", value: complex64(1 + 2i), state: equalityHashAvailable},
		{name: "NaN", value: math.NaN(), state: equalityHashAlwaysDistinct},
		{name: "bool", value: true, state: equalityHashAvailable},
		{name: coverageString, value: coverageValue, state: equalityHashAvailable},
		{name: "slice", value: []int{1}, state: equalityHashUnavailable},
		{name: "array", value: [1]int{1}, state: equalityHashUnavailable},
		{name: "literal", value: makeNumberLiteral(constant.MakeInt64(1), "1"), state: equalityHashRequiresFullScan},
	}
	for _, testCase := range tests {
		_, state := makeEqualityHashKey(testCase.value)
		if state != testCase.state {
			t.Fatalf("makeEqualityHashKey(%s) state = %v, want %v", testCase.name, state, testCase.state)
		}
	}

	for _, value := range []any{true, coverageValue, int8(1), uint16(1), float32(1), complex64(1)} {
		_, state := makeEqualityHashKeyReflect(reflect.ValueOf(value))
		if state != equalityHashAvailable {
			t.Fatalf("makeEqualityHashKeyReflect(%T) state = %v", value, state)
		}
	}

	if _, state := makeEqualityHashKeyReflect(reflect.ValueOf([]int{1})); state != equalityHashUnavailable {
		t.Fatalf("makeEqualityHashKeyReflect(slice) state = %v", state)
	}
}

func TestInternalScalarMapClassifications(t *testing.T) {
	t.Parallel()

	for _, value := range []any{true, "value", int8(-1), uint16(1), float32(1), complex64(1 + 2i)} {
		reflected := reflect.ValueOf(value)
		if _, valid := makeScalarMapValue(reflected, reflected.Kind()); !valid {
			t.Fatalf("makeScalarMapValue(%T) valid = false", value)
		}
	}

	if _, valid := floatScalarMapValue(math.NaN()); valid {
		t.Fatal("floatScalarMapValue(NaN) valid = true")
	}

	if _, valid := complexScalarMapValue(complex(math.NaN(), 0)); valid {
		t.Fatal("complexScalarMapValue(NaN) valid = true")
	}

	if _, valid := numericComplex128Reflect(reflect.ValueOf("not numeric")); valid {
		t.Fatal("numericComplex128Reflect(string) valid = true")
	}
}

func TestInternalConversionClassifications(t *testing.T) {
	t.Parallel()

	if !integerConversion(reflect.TypeFor[int8](), reflect.TypeFor[uint64]()) {
		t.Fatal("integerConversion(int8, uint64) = false")
	}

	if integerConversion(reflect.TypeFor[float64](), reflect.TypeFor[int]()) {
		t.Fatal("integerConversion(float64, int) = true")
	}
}

func TestInternalTypesMayContainNumberLiteral(t *testing.T) {
	t.Parallel()

	typesContainingLiteral := []reflect.Type{
		reflect.TypeFor[numberLiteral](), reflect.TypeFor[any](), reflect.TypeFor[[]any](),
		reflect.TypeFor[map[string]any](), reflect.TypeFor[*any](),
		reflect.TypeFor[struct{ Value any }](),
	}
	for _, valueType := range typesContainingLiteral {
		got, err := typeMayContainNumberLiteral(valueType, make(map[reflect.Type]bool), 0)
		if err != nil || !got {
			t.Fatalf("typeMayContainNumberLiteral(%v) = %t, %v", valueType, got, err)
		}
	}

	typesWithoutLiteral := []reflect.Type{
		reflect.TypeFor[int](), reflect.TypeFor[string](), reflect.TypeFor[chan int](),
	}
	for _, valueType := range typesWithoutLiteral {
		got, err := typeMayContainNumberLiteral(valueType, make(map[reflect.Type]bool), 0)
		if err != nil || got {
			t.Fatalf("typeMayContainNumberLiteral(%v) = %t, %v", valueType, got, err)
		}
	}
}

func TestInternalTypesCanContainPreservedLiteral(t *testing.T) {
	t.Parallel()

	for _, valueType := range []reflect.Type{
		reflect.TypeFor[numberLiteral](), reflect.TypeFor[any](), reflect.TypeFor[[]int](),
		reflect.TypeFor[map[string]int](), reflect.TypeFor[*int](), reflect.TypeFor[struct{}](),
	} {
		if !typeCanContainPreservedLiteral(valueType) {
			t.Fatalf("typeCanContainPreservedLiteral(%v) = false", valueType)
		}
	}

	if typeCanContainPreservedLiteral(reflect.TypeFor[int]()) {
		t.Fatal("typeCanContainPreservedLiteral(int) = true")
	}
}

func TestInternalUnwrapInterface(t *testing.T) {
	t.Parallel()

	if value, valid := unwrapInterface(nil); !valid || value.IsValid() {
		t.Fatalf("unwrapInterface(nil) = %v, %t", value, valid)
	}

	if value, valid := unwrapInterface(1); !valid || value.Int() != 1 {
		t.Fatalf("unwrapInterface(1) = %v, %t", value, valid)
	}
}

func TestInternalJSONConversionHelpers(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		number json.Number
		want   any
	}{
		{number: json.Number("1"), want: float64(1)},
		{number: json.Number("-9007199254740993"), want: int64(-9_007_199_254_740_993)},
		{number: json.Number("18446744073709551615"), want: uint64(math.MaxUint64)},
		{number: json.Number("1.5"), want: float64(1.5)},
	} {
		got, err := jsonNumberValue(testCase.number)
		if err != nil || got != testCase.want {
			t.Fatalf("jsonNumberValue(%q) = %#v, %v; want %#v, nil", testCase.number, got, err, testCase.want)
		}
	}

	_, err := jsonNumberValue(json.Number("invalid"))
	if err == nil {
		t.Fatal("jsonNumberValue(invalid) error = nil")
	}
}

func TestInternalJSONSequenceHelpers(t *testing.T) {
	t.Parallel()

	array := reflect.ValueOf([1]int{1})

	identity, tracked, err := trackJSONSequence(array, make(map[collectionIdentity]bool))
	if err != nil || tracked || identity.kind != reflect.Array {
		t.Fatalf("trackJSONSequence(array) = %+v, %t, %v", identity, tracked, err)
	}

	nilSlice := reflect.ValueOf([]int(nil))

	_, tracked, err = trackJSONSequence(nilSlice, make(map[collectionIdentity]bool))
	if err != nil || tracked {
		t.Fatalf("trackJSONSequence(nil slice) = tracked %t, %v", tracked, err)
	}

	var nilKey any

	_, err = jsonMapKey(reflect.ValueOf(&nilKey).Elem())
	if err == nil {
		t.Fatal("jsonMapKey(nil interface) error = nil")
	}
}

func TestInternalCallableAndSequenceHelpers(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		fn   any
	}{
		{name: "", fn: func() int { return 1 }},
		{name: "nil", fn: nil},
		{name: "not-function", fn: 1},
		{name: "typed-nil", fn: (func() int)(nil)},
		{name: "no-result", fn: func() {}},
		{name: "bad-error-result", fn: func() (int, int) { return 1, 2 }},
	} {
		_, err := newCallable(testCase.name, testCase.fn)
		if err == nil {
			t.Fatalf("newCallable(%q, %T) error = nil", testCase.name, testCase.fn)
		}
	}

	var nilCallable *callable

	_, err := nilCallable.invokeRaw(context.Background(), nil, emptySpan())
	if err == nil {
		t.Fatal("nil callable invoke error = nil")
	}

	resolved, err := resolveCallable(func(value int) bool { return value > 0 })
	if err != nil || resolved == nil {
		t.Fatalf("resolveCallable(function) = %#v, %v", resolved, err)
	}

	_, err = resolveCallable(1)
	if err == nil {
		t.Fatal("resolveCallable(non-function) error = nil")
	}
}

func TestInternalSequenceHelpers(t *testing.T) {
	t.Parallel()

	values, err := sequenceValues(context.Background(), [2]int{1, 2})
	if err != nil || !reflect.DeepEqual(values, []any{1, 2}) {
		t.Fatalf("sequenceValues(array) = %#v, %v", values, err)
	}

	for _, value := range []any{nil, 1, (*[]int)(nil)} {
		_, err := newSequenceView(value)
		if err == nil {
			t.Fatalf("newSequenceView(%T) error = nil", value)
		}
	}

	if !isNaN(float32(math.NaN())) || isNaN("not numeric") {
		t.Fatal("isNaN() returned an incorrect classification")
	}
}

func TestInternalBuiltinCancellationBranches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	predicate, err := newCallable("positive", func(value int) bool { return value > 0 })
	if err != nil {
		t.Fatalf("newCallable(predicate) error = %v", err)
	}

	identity, err := newCallable("identity", func(value int) int { return value })
	if err != nil {
		t.Fatalf("newCallable(identity) error = %v", err)
	}

	add, err := newCallable("add", func(left, right int) int { return left + right })
	if err != nil {
		t.Fatalf("newCallable(add) error = %v", err)
	}

	_, concatErr := concatBuiltin(ctx, []any{[]int{1}})
	_, flattenErr := flattenBuiltin(ctx, []any{[]int{1}})
	_, uniqErr := uniqBuiltin(ctx, []any{[]int{1}})
	_, joinErr := joinBuiltin(ctx, []any{[]string{"a"}, ","})
	_, sumErr := sumBuiltin(ctx, []any{[]int{1, 2}})
	_, meanErr := meanBuiltin(ctx, []any{[]float64{1}})
	_, medianErr := medianBuiltin(ctx, []any{[]float64{1}})
	_, reverseErr := reverseBuiltin(ctx, []any{[]int{1, 2}})
	_, sortErr := sortBuiltin(ctx, []any{[]int{2, 1}})
	_, allErr := quantifyBuiltin(quantifyAll)(ctx, []any{[]int{1}, predicate})
	_, mapErr := mapBuiltin(ctx, []any{[]int{1}, identity})
	_, filterErr := filterBuiltin(ctx, []any{[]int{1}, predicate})
	_, findErr := findBuiltin(findFirstValue)(ctx, []any{[]int{1}, predicate})
	_, groupByErr := groupByBuiltin(ctx, []any{[]int{1}, identity})
	_, countErr := countBuiltin(ctx, []any{[]int{1}, predicate})
	_, reduceErr := reduceBuiltin(ctx, []any{[]int{1, 2}, add})
	_, sortByErr := sortByBuiltin(ctx, []any{[]int{1}, identity})

	tests := []coverageErrorCase{
		{name: "concat", err: concatErr},
		{name: "flatten", err: flattenErr},
		{name: "uniq", err: uniqErr},
		{name: "join", err: joinErr},
		{name: "sum", err: sumErr},
		{name: "mean", err: meanErr},
		{name: "median", err: medianErr},
		{name: "reverse", err: reverseErr},
		{name: "sort", err: sortErr},
		{name: "all", err: allErr},
		{name: "map", err: mapErr},
		{name: "filter", err: filterErr},
		{name: "find", err: findErr},
		{name: "groupBy", err: groupByErr},
		{name: "count", err: countErr},
		{name: "reduce", err: reduceErr},
		{name: "sortBy", err: sortByErr},
	}

	assertCoverageCanceled(t, tests)
}

func TestInternalEqualityCancellationBranches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seen := make(map[equalityVisit]bool)

	sequence := reflect.ValueOf([]int{1})

	_, err := equalSequenceValues(ctx, sequence, sequence, seen, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("equalSequenceValues() error = %v", err)
	}

	compositeMap := reflect.ValueOf(map[int][]int{1: {2}})

	_, _, err = indexHashedMapValues(ctx, compositeMap)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("indexHashedMapValues() error = %v", err)
	}

	_, err = compareHashedMapValues(ctx, compositeMap, nil, seen, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("compareHashedMapValues() error = %v", err)
	}

	_, err = equalExactMapValues(ctx, compositeMap, compositeMap, seen, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("equalExactMapValues() error = %v", err)
	}

	_, err = equalSemanticMapValues(ctx, compositeMap, compositeMap, seen, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("equalSemanticMapValues() error = %v", err)
	}

	scalarMap := reflect.ValueOf(map[int]int{1: 2})

	_, _, err = indexHashedScalarMapValues(ctx, scalarMap, reflect.Int)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("indexHashedScalarMapValues() error = %v", err)
	}

	_, err = compareHashedScalarMapValues(ctx, scalarMap, nil, reflect.Int)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("compareHashedScalarMapValues() error = %v", err)
	}

	_, _, err = indexScalarMap(ctx, scalarMap, reflect.Int, reflect.Int, makeScalarMapWord)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("indexScalarMap() error = %v", err)
	}

	_, err = compareIndexedScalarMap(ctx, scalarMap, nil, reflect.Int, reflect.Int, makeScalarMapWord)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("compareIndexedScalarMap() error = %v", err)
	}
}
