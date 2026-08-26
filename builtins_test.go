package fasteval_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.dw1.io/fasteval"
)

func TestCollectionBuiltinsUseNamedFunctionReferences(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("isEven", func(value int) bool { return value%2 == 0 }),
		fasteval.WithFunction("double", func(value int) int { return value * 2 }),
		fasteval.WithFunction("add", func(left, right int) int { return left + right }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	tests := []struct {
		source string
		want   any
	}{
		{source: "all([2, 4, 6], isEven)", want: true},
		{source: "any([1, 3, 4], isEven)", want: true},
		{source: "one([1, 2, 3], isEven)", want: true},
		{source: "none([1, 3], isEven)", want: true},
		{source: "map([1, 2, 3], double)", want: []any{2, 4, 6}},
		{source: "filter([1, 2, 3, 4], isEven)", want: []any{2, 4}},
		{source: "find([1, 3, 4], isEven)", want: 4},
		{source: "findIndex([1, 3, 4], isEven)", want: 2},
		{source: "findLast([2, 3, 4], isEven)", want: 4},
		{source: "findLastIndex([2, 3, 4], isEven)", want: 2},
		{
			source: "groupBy([1, 2, 3], isEven)",
			want:   map[any][]any{false: {1, 3}, true: {2}},
		},
		{source: "count([1, 2, 3, 4], isEven)", want: 2},
		{source: "reduce([1, 2, 3], add, 0)", want: 6},
		{source: "sortBy([3, 1, 2], double)", want: []any{1, 2, 3}},
	}
	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			expr, compileErr := compiler.Compile(testCase.source)
			if compileErr != nil {
				t.Fatalf("Compile() error = %v", compileErr)
			}

			got, evalErr := expr.Eval(context.Background(), map[string]any{
				"isEven": testShadow, testDouble: testShadow, "add": testShadow,
			})
			if evalErr != nil {
				t.Fatalf("Eval() error = %v", evalErr)
			}

			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestCollectionLiteralsAdaptToCallbackParameters(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("identityFloat32", func(value float32) float32 { return value }),
		fasteval.WithFunction("keepUint64", func(accumulator, _ uint64) uint64 { return accumulator }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	tests := []struct {
		source string
		want   any
	}{
		{source: "map([1.5,], identityFloat32)", want: []any{float32(1.5)}},
		{
			source: "reduce([0,], keepUint64, 18446744073709551615)",
			want:   uint64(math.MaxUint64),
		},
	}

	for _, testCase := range tests {
		got, evalErr := compiler.Compile(testCase.source)
		if evalErr != nil {
			t.Fatalf("Compile(%q) error = %v", testCase.source, evalErr)
		}

		value, evalErr := got.Eval(context.Background(), nil)
		if evalErr != nil || !reflect.DeepEqual(value, testCase.want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, value, evalErr, testCase.want)
		}
	}
}

func TestJoinMaterializesNumericLiteralElements(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"join([1, 2], ',')",
		"join(concat([1,], [2,]), ',')",
	} {
		got, err := fasteval.Eval(context.Background(), source, nil)
		if err != nil || got != "1,2" {
			t.Fatalf("Eval(%q) = %#v, %v; want 1,2, nil", source, got, err)
		}
	}
}

func TestEquivalentLiteralCollectionKeys(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"{1: 10, 01: 20}",
		"{1: 10, 1.0: 20}",
		"{1: 10, 1e0: 20}",
		"{1: 10, (1+0i): 20}",
		"fromPairs([[1, 10], [01, 20]])",
		"fromPairs([[1, 10], [1.0, 20]])",
	} {
		_, err := fasteval.Eval(context.Background(), source, nil)

		var evalErr *fasteval.EvalError
		if !errors.As(err, &evalErr) {
			t.Fatalf("Eval(%q) error = %v, want EvalError", source, err)
		}
	}

	compiler, err := fasteval.NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("groupBy([[1,], [1.0,]], first)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), nil)

	want := map[any][]any{1: {[]any{1}, []any{float64(1)}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Eval() = %#v, %v; want %#v, nil", got, err, want)
	}
}

func TestLiteralAndConcreteCollectionKeysAreEquivalent(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"len({1: 10, key: 20})",
		"len({key: 10, 1: 20})",
		"{1: 10, key: 20}[1]",
		"{key: 10, 1: 20}[key]",
		"fromPairs([[1, 10], [key, 20]])",
		"fromPairs([[key, 10], [1, 20]])",
	} {
		_, err := fasteval.Eval(context.Background(), source, map[string]any{testKey: int64(1)})

		var evalErr *fasteval.EvalError
		if !errors.As(err, &evalErr) {
			t.Fatalf("Eval(%q) error = %v, want duplicate-key EvalError", source, err)
		}
	}

	for _, testCase := range []struct {
		source string
		want   map[any][]any
	}{
		{
			source: "groupBy([[1,], [key,]], first)",
			want:   map[any][]any{1: {[]any{1}, []any{int64(1)}}},
		},
		{
			source: "groupBy([[key,], [1,]], first)",
			want:   map[any][]any{int64(1): {[]any{int64(1)}, []any{1}}},
		},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, map[string]any{testKey: int64(1)})
		if err != nil || !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestFindReturnsTheValueThatMatchedBeforePredicateMutation(t *testing.T) {
	t.Parallel()

	values := []int{1}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("matchAndMutate", func(value int) bool {
		values[0] = 2

		return value == 1
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	for _, name := range []string{"find", "findLast"} {
		values[0] = 1

		expression, compileErr := compiler.Compile(name + "(values, matchAndMutate)")
		if compileErr != nil {
			t.Fatalf("Compile() error = %v", compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), map[string]any{testValues: values})
		if evalErr != nil || got != 1 {
			t.Fatalf("%s() = %#v, %v; want 1, nil", name, got, evalErr)
		}
	}
}

func TestNumericLiteralsMaterializeAtBuiltinBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "type(1)", want: testInt},
		{source: "toJSON(1)", want: "1"},
		{source: "toJSON({'value': 1 + 1})", want: `{"value":2}`},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, nil)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestTypeBuiltinReportsPublicCollectionTypes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		want   string
	}{
		{source: "type([1,])", want: "[]interface {}"},
		{source: "type({1: 2})", want: "map[interface {}]interface {}"},
		{source: "type(groupBy([1,], type))", want: "map[interface {}][]interface {}"},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, nil)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %q, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestStringBuiltinFormatsDuration(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "string(duration('1s'))", nil)
	if err != nil || got != "1s" {
		t.Fatalf("Eval() = %#v, %v; want 1s, nil", got, err)
	}
}

func TestExtremaPropagateNaN(t *testing.T) {
	t.Parallel()

	for _, function := range []string{testMin, testMax} {
		for _, values := range [][2]float64{{math.NaN(), 1}, {1, math.NaN()}} {
			got, err := fasteval.Eval(context.Background(), function+"(left, right)", map[string]any{
				testLeft: values[0], testRight: values[1],
			})

			floating, ok := got.(float64)
			if err != nil || !ok || !math.IsNaN(floating) {
				t.Fatalf("%s(%v, %v) = %#v, %v; want NaN, nil", function, values[0], values[1], got, err)
			}
		}
	}
}

func TestExtremaPropagatePointerCycleErrors(t *testing.T) {
	t.Parallel()

	var cycle any

	cycle = &cycle

	for _, function := range []string{"min", "max"} {
		_, err := fasteval.Eval(context.Background(), function+"(value)", map[string]any{
			testValue: cycle,
		})
		if err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("%s() error = %v, want cycle error", function, err)
		}
	}
}

func TestComplexBuiltinsPreserveNativeWidths(t *testing.T) {
	t.Parallel()

	variables := map[string]any{
		"smallComplex": complex64(1 + 2i), "smallReal": float32(1), "smallImag": float32(2),
		"wideReal": float64(1),
	}
	tests := []struct {
		source string
		want   any
	}{
		{source: "real(smallComplex)", want: float32(1)},
		{source: "imag(smallComplex)", want: float32(2)},
		{source: "conj(smallComplex)", want: complex64(1 - 2i)},
		{source: "complex(smallReal, smallImag)", want: complex64(1 + 2i)},
		{source: "complex(smallReal, 2)", want: complex64(1 + 2i)},
		{source: "complex(wideReal, 2)", want: complex128(1 + 2i)},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, variables)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v (%T), %v; want %#v (%T), nil", testCase.source, got, got, err, testCase.want, testCase.want)
		}
	}

	_, err := fasteval.Eval(
		context.Background(), "complex(smallReal, wideReal)", variables,
	)
	if err == nil {
		t.Fatal("mixed concrete float widths error = nil")
	}
}

func TestComplexBuiltinRejectsLossyConcreteIntegers(t *testing.T) {
	t.Parallel()

	for _, value := range []any{int64(1<<53 + 1), uint64(1<<53 + 1)} {
		_, err := fasteval.Eval(context.Background(), "complex(value, 0)", map[string]any{
			testValue: value,
		})
		if err == nil {
			t.Fatalf("complex(%T(%v), 0) error = nil", value, value)
		}
	}

	for _, value := range []any{int64(1 << 53), uint64(1 << 53)} {
		got, err := fasteval.Eval(context.Background(), "complex(value, 0)", map[string]any{
			testValue: value,
		})

		want := complex(float64(1<<53), 0)
		if err != nil || got != want {
			t.Fatalf("complex(%T(%v), 0) = %#v, %v; want %#v, nil", value, value, got, err, want)
		}
	}
}

func TestUnsignedShiftBuiltinAcceptsWideCounts(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "bitushr(value, count)", map[string]any{
		testValue: uint64(1), testCount: uint64(math.MaxUint64),
	})
	if err != nil || got != uint64(0) {
		t.Fatalf("bitushr() = %#v, %v; want uint64(0), nil", got, err)
	}

	_, err = fasteval.Eval(context.Background(), "bitushr(value, -1)", map[string]any{
		testValue: uint64(1),
	})
	if err == nil {
		t.Fatal("negative bitushr count error = nil")
	}
}

func TestExtremaAdaptWinningLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		value  int8
	}{
		{source: "type(min(value, 1))", value: 2},
		{source: "type(min(1, value))", value: 2},
		{source: "type(max(value, 3))", value: 2},
		{source: "type(max(3, value))", value: 2},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, map[string]any{
			testValue: testCase.value,
		})
		if err != nil || got != testInt8 {
			t.Fatalf("Eval(%q) = %#v, %v; want int8, nil", testCase.source, got, err)
		}
	}
}

func TestMeanAndMedianAvoidFiniteOverflow(t *testing.T) {
	t.Parallel()

	for _, source := range []string{testMeanValuesSource, testMedianValuesSource} {
		assertFloatSequenceEval(t, source, []float64{math.MaxFloat64, math.MaxFloat64}, math.MaxFloat64)
	}

	for _, source := range []string{"mean(values)", "median(values)"} {
		assertFloatSequenceEval(
			t, source,
			[]float64{math.SmallestNonzeroFloat64, 2 * math.SmallestNonzeroFloat64},
			2*math.SmallestNonzeroFloat64,
		)
	}

	assertFloatSequenceEval(
		t, "mean(values)",
		[]float64{
			math.MaxFloat64, -math.MaxFloat64, 3 * math.SmallestNonzeroFloat64,
		},
		math.SmallestNonzeroFloat64,
	)
	assertFloatSequenceEval(
		t, "mean(values)",
		[]float64{math.MaxFloat64, math.MaxFloat64, math.MaxFloat64}, math.MaxFloat64,
	)
}

func assertFloatSequenceEval(t *testing.T, source string, values []float64, want float64) {
	t.Helper()

	got, err := fasteval.Eval(context.Background(), source, map[string]any{testValues: values})
	if err != nil || got != want {
		t.Fatalf("Eval(%q) = %#v, %v; want %v, nil", source, got, err, want)
	}
}

func TestMeanRejectsLossyConcreteIntegers(t *testing.T) {
	t.Parallel()

	for _, values := range []any{
		[]int64{1<<53 + 1, 1<<53 + 2},
		[]uint64{1<<53 + 1, 1<<53 + 2},
		[]any{int64(1<<53 + 1), int64(1<<53 + 2)},
		[]any{uint64(1<<53 + 1), uint64(1<<53 + 2)},
	} {
		_, err := fasteval.Eval(context.Background(), "mean(values)", map[string]any{testValues: values})
		if err == nil {
			t.Fatalf("mean(%T) error = nil, want exact-conversion error", values)
		}
	}
}

func TestMedianPropagatesNaN(t *testing.T) {
	t.Parallel()

	for _, values := range [][]float64{
		{math.NaN(), 1, 2},
		{math.NaN(), 1, 2, 3},
	} {
		got, err := fasteval.Eval(context.Background(), "median(values)", map[string]any{
			testValues: values,
		})
		if err != nil {
			t.Fatalf("Eval() error = %v", err)
		}

		median, ok := got.(float64)
		if !ok || !math.IsNaN(median) {
			t.Fatalf("Eval() = %#v, want NaN", got)
		}
	}
}

func TestUniqPreservesNumericAndCompositeEquality(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "uniq(values)", map[string]any{
		testValues: []any{
			int8(1), int64(1), float64(1), []int{2}, []int{2}, time.Duration(1), time.Duration(1),
		},
	})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	want := []any{int8(1), []int{2}, time.Duration(1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Eval() = %#v, want %#v", got, want)
	}
}

func TestTypedUniqMatchesLiteralsAndConcreteNumbers(t *testing.T) {
	t.Parallel()

	assertTypedUniq(t, "uniq([1, value])", int64(1), []int64{1})

	for _, source := range []string{
		"uniq([18446744073709551615, value])",
		"uniq([value, 18446744073709551615])",
	} {
		assertTypedUniq(t, source, uint64(math.MaxUint64), []uint64{math.MaxUint64})
	}

	for _, source := range []string{"uniq([0.1, value])", "uniq([value, 0.1])"} {
		assertTypedUniq(t, source, float32(0.1), []float32{0.1})
	}

	for _, source := range []string{"uniq([0.1+0.2i, value])", "uniq([value, 0.1+0.2i])"} {
		assertTypedUniq(t, source, complex64(0.1+0.2i), []complex64{0.1 + 0.2i})
	}

	for _, source := range []string{"uniq([1, value])", "uniq([value, 1])"} {
		assertTypedUniq(t, source, time.Nanosecond, []time.Duration{time.Nanosecond})
	}
}

func assertTypedUniq[T any](t *testing.T, source string, value T, want []T) {
	t.Helper()

	got, err := fasteval.EvalAs[[]T](context.Background(), source, map[string]any{testValue: value})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("EvalAs(%q) = %#v, %v; want %#v, nil", source, got, err, want)
	}
}

func TestJSONHandlesTypedNilAndRejectsNormalizedKeyCollisions(t *testing.T) {
	t.Parallel()

	var (
		nilMap   map[string]any
		nilSlice []any
	)

	got, err := fasteval.Eval(context.Background(), "toJSON(value)", map[string]any{
		testValue: map[string]any{testMap: nilMap, testSlice: nilSlice},
	})
	if err != nil || got != `{"map":null,"slice":null}` {
		t.Fatalf("typed nil JSON = %#v, %v", got, err)
	}

	type namedString string

	_, err = fasteval.Eval(context.Background(), "toJSON(value)", map[string]any{
		testValue: map[any]any{string("x"): 1, namedString("x"): 2},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate normalized key") {
		t.Fatalf("normalized JSON key collision error = %v", err)
	}
}

func TestFromJSONPreservesLargeIntegers(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "fromJSON('9007199254740993')", nil)
	if err != nil || got != int64(9_007_199_254_740_993) {
		t.Fatalf("fromJSON() = %#v (%T), %v; want exact int64", got, got, err)
	}

	got, err = fasteval.Eval(
		context.Background(),
		`toJSON(fromJSON('{"value":18446744073709551615}'))`,
		nil,
	)
	if err != nil || got != `{"value":18446744073709551615}` {
		t.Fatalf("nested JSON round trip = %#v, %v", got, err)
	}

	converted, err := fasteval.EvalAs[uint64](
		context.Background(), "fromJSON('18446744073709551615')", nil,
	)
	if err != nil || converted != math.MaxUint64 {
		t.Fatalf("EvalAs[uint64]() = %d, %v; want %d, nil", converted, err, uint64(math.MaxUint64))
	}
}

func TestCollectionAndEncodingBuiltins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "concat([1, 2], [3,])", want: []any{1, 2, 3}},
		{source: "flatten([1, [2, [3,]]])", want: []any{1, 2, 3}},
		{source: "uniq([1, 1, 2, 1])", want: []any{1, 2}},
		{source: "join(['a', 'b'], '-')", want: "a-b"},
		{source: "sum([1, 2, 3])", want: 6},
		{source: "mean([1, 2, 3])", want: float64(2)},
		{source: "median([3, 1, 2])", want: float64(2)},
		{source: "reverse([1, 2, 3])", want: []any{3, 2, 1}},
		{source: "sort([3, 1, 2])", want: []any{1, 2, 3}},
		{source: "take([1, 2, 3], -2)", want: []any{2, 3}},
		{source: "get({'a': 1}, 'missing')", want: nil},
		{source: "fromBase64(toBase64('hello'))", want: "hello"},
		{source: "fromJSON(toJSON({'a': 1})).a", want: float64(1)},
	}
	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, nil)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v (%T), want %#v (%T)", got, got, testCase.want, testCase.want)
			}
		})
	}
}

func TestStringNumericAndBitwiseBuiltins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "upper(trim('  fasteval  '))", want: "FASTEVAL"},
		{source: "replace('a-b-a', 'a', 'x')", want: "x-b-x"},
		{source: "hasPrefix('fasteval', 'fast')", want: true},
		{source: "int8(127)", want: int8(127)},
		{source: "float32(1.5)", want: float32(1.5)},
		{source: "abs(-5)", want: 5},
		{source: "round(2.6)", want: float64(3)},
		{source: "bitand(7, 3)", want: 3},
		{source: "bitshl(2, 3)", want: 16},
		{source: "real(complex(2, 3))", want: float64(2)},
	}
	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, nil)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v (%T), want %#v (%T)", got, got, testCase.want, testCase.want)
			}
		})
	}
}

func TestStringBuiltinAcceptsNamedStrings(t *testing.T) {
	t.Parallel()

	type label string

	got, err := fasteval.Eval(context.Background(), "string(value)", map[string]any{testValue: label("Ada")})
	if err != nil || got != testAda {
		t.Fatalf("Eval() = %#v, %v; want Ada, nil", got, err)
	}
}

func TestFloatMathBuiltinsRejectLossyIntegerConversion(t *testing.T) {
	t.Parallel()

	for _, function := range []string{"ceil", "floor", "round"} {
		for _, value := range []any{int64(1<<53 + 1), uint64(1<<53 + 1)} {
			_, err := fasteval.Eval(
				context.Background(), function+"(value)", map[string]any{testValue: value},
			)
			if err == nil {
				t.Fatalf("Eval(%s(value)) with %T error = nil, want exact-conversion error", function, value)
			}
		}
	}
}

func TestBinaryBitwiseBuiltinsAdaptEitherLiteral(t *testing.T) {
	t.Parallel()

	variable := uint64(3)
	tests := []struct {
		source string
		want   uint64
	}{
		{source: "bitand(6, value)", want: 2},
		{source: "bitand(value, 6)", want: 2},
		{source: "bitor(6, value)", want: 7},
		{source: "bitor(value, 6)", want: 7},
		{source: "bitxor(6, value)", want: 5},
		{source: "bitxor(value, 6)", want: 5},
		{source: "bitnand(6, value)", want: ^uint64(2)},
		{source: "bitnand(value, 6)", want: ^uint64(2)},
		{source: "bitshl(6, value)", want: 48},
		{source: "bitshl(value, 6)", want: 192},
		{source: "bitshr(6, value)", want: 0},
		{source: "bitshr(value, 1)", want: 1},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, map[string]any{
			testValue: variable,
		})
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestTimeBuiltins(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "date('2026-08-10T12:00:00Z') + duration('2h')", nil)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	want := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)

	gotTime, ok := got.(time.Time)

	if !ok || !gotTime.Equal(want) {
		t.Fatalf("Eval() = %v, want %v", got, want)
	}

	got, err = fasteval.Eval(context.Background(), "abs(duration('-2s'))", nil)
	if err != nil {
		t.Fatalf("Eval() abs(duration) error = %v", err)
	}

	if got != 2*time.Second {
		t.Fatalf("Eval() abs(duration) = %#v (%T), want %v", got, got, 2*time.Second)
	}
}

func TestAllBuiltinsAreReserved(t *testing.T) {
	t.Parallel()

	_, err := fasteval.NewCompiler(fasteval.WithFunction("len", func() int { return 0 }))
	if err == nil {
		t.Fatal("NewCompiler() error = nil, want reserved-name error")
	}

	expression, err := fasteval.Compile("len([1, 2])")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), map[string]any{testLen: 99})
	if err != nil || got != 2 {
		t.Fatalf("Eval() = %#v, %v; want reserved len function result 2, nil", got, err)
	}
}

func TestCallableNamesDoNotResolveAsValues(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"len", "toJSON(len)"} {
		_, err := fasteval.Eval(context.Background(), source, nil)

		var evalErr *fasteval.EvalError
		if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrUnknownVariable {
			t.Fatalf("Eval(%q) error = %v, want ErrUnknownVariable", source, err)
		}
	}
}

func TestTakeRejectsMinimumIntegerCountWithoutPanicking(t *testing.T) {
	t.Parallel()

	minimumInt := -int(^uint(0)>>1) - 1
	_, err := fasteval.Eval(context.Background(), "take([1, 2], count)", map[string]any{testCount: minimumInt})

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) {
		t.Fatalf("Eval() error = %v, want EvalError", err)
	}

	if evalErr.Code() == fasteval.ErrPanic {
		t.Fatalf("Eval() error = %v, want a validation error", err)
	}
}

func TestRepeatRejectsOutputLengthOverflowWithoutPanicking(t *testing.T) {
	t.Parallel()

	count := int(^uint(0) >> 1)
	_, err := fasteval.Eval(
		context.Background(), "repeat('xx', count)", map[string]any{testCount: count},
	)

	var evalErr *fasteval.EvalError
	if !errors.As(err, &evalErr) {
		t.Fatalf("Eval() error = %v, want EvalError", err)
	}

	if evalErr.Code() == fasteval.ErrPanic {
		t.Fatalf("Eval() error = %v, want a validation error", err)
	}
}

func TestBuiltinErrorsPreserveCallSpan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		options []fasteval.CompilerOption
		vars    map[string]any
	}{
		{
			name:    "builtin",
			source:  testAbsValueSource,
			options: nil,
			vars:    map[string]any{testValue: int8(math.MinInt8)},
		},
		{
			name:   "higher-order callback",
			source: "all(values, fail)",
			options: []fasteval.CompilerOption{fasteval.WithFunction(
				"fail",
				func(int) (bool, error) { return false, errCallbackFailed },
			)},
			vars: map[string]any{testValues: []int{1}},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := fasteval.NewCompiler(testCase.options...)
			if err != nil {
				t.Fatalf("NewCompiler() error = %v", err)
			}

			expression, err := compiler.Compile(testCase.source)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}

			_, err = expression.Eval(context.Background(), testCase.vars)

			var evalErr *fasteval.EvalError
			if !errors.As(err, &evalErr) {
				t.Fatalf("Eval() error = %v, want EvalError", err)
			}

			want := fasteval.Span{Start: 0, End: len(testCase.source), Line: 1, Column: 1}
			if got := evalErr.Span(); got != want {
				t.Fatalf("EvalError.Span() = %+v, want %+v; error = %v", got, want, err)
			}
		})
	}
}

func TestNilMapKeys(t *testing.T) {
	t.Parallel()

	integer := 1
	tests := []struct {
		name   string
		source string
		vars   map[string]any
		want   any
	}{
		{
			name: "map literal", source: "{nil: 1}[nil]", vars: nil, want: 1,
		},
		{
			name: "interface map lookup", source: "values[nil]",
			vars: map[string]any{testValues: map[any]string{nil: testFound}}, want: testFound,
		},
		{
			name: "pointer map lookup", source: "values[nil]",
			vars: map[string]any{testValues: map[*int]string{nil: testFound, &integer: "other"}}, want: testFound,
		},
		{
			name: "from pairs", source: "fromPairs(values)[nil]",
			vars: map[string]any{testValues: [][]any{{nil, 1}}}, want: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v, want %#v", got, testCase.want)
			}
		})
	}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("nilKey", func(int) any { return nil }))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("groupBy(values, nilKey)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), map[string]any{testValues: []int{1, 2}})
	if err != nil {
		t.Fatalf("Eval() groupBy error = %v", err)
	}

	want := map[any][]any{nil: {1, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Eval() groupBy = %#v, want %#v", got, want)
	}
}

func TestOptionalNilBuiltinArguments(t *testing.T) {
	t.Parallel()

	variables := map[string]any{testValue: nil}
	tests := []struct {
		source string
		want   any
	}{
		{source: "type(value?.field)", want: "nil"},
		{source: "len(value?.field)", want: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, variables)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if got != testCase.want {
				t.Fatalf("Eval() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestEmptySequenceBuiltinsReturnEmptyLists(t *testing.T) {
	t.Parallel()

	variables := map[string]any{testValues: []int{}}

	for _, source := range []string{
		"concat(values)",
		"flatten(values)",
		"take(values, 0)",
		testSortValuesSource,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), source, variables)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if !reflect.DeepEqual(got, []any{}) {
				t.Fatalf("Eval() = %#v, want non-nil empty list", got)
			}
		})
	}
}

func TestSortOrdersNaNDeterministically(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction(testKey, func(value float64) float64 {
		return value
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	values := []float64{2, math.NaN(), 1, math.NaN()}

	for _, source := range []string{testSortValuesSource, "sortBy(values, key)"} {
		assertNaNSortOrder(t, compiler, source, values)
	}
}

func assertNaNSortOrder(t *testing.T, compiler *fasteval.Compiler, source string, values []float64) {
	t.Helper()

	const wantLength = 4

	expression, err := compiler.Compile(source)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", source, err)
	}

	got, err := expression.Eval(context.Background(), map[string]any{testValues: values})
	if err != nil {
		t.Fatalf("Eval(%q) error = %v", source, err)
	}

	sorted, ok := got.([]any)
	if !ok || len(sorted) != wantLength {
		t.Fatalf("Eval(%q) = %#v, want four sorted values", source, got)
	}

	if !validNaNSort(sorted) {
		t.Fatalf("Eval(%q) = %#v, want NaNs first then [1 2]", source, got)
	}
}

func validNaNSort(sorted []any) bool {
	first, firstOK := sorted[0].(float64)
	second, secondOK := sorted[1].(float64)

	return firstOK && secondOK && math.IsNaN(first) && math.IsNaN(second) &&
		sorted[2] == float64(1) && sorted[3] == float64(2)
}

func TestSumDurations(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "sum(values)", map[string]any{
		testValues: []time.Duration{time.Second, 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if got != 3*time.Second {
		t.Fatalf("Eval() = %#v, want %v", got, 3*time.Second)
	}

	_, err = fasteval.Eval(context.Background(), "sum(values)", map[string]any{
		testValues: []time.Duration{time.Duration(math.MaxInt64), time.Nanosecond},
	})

	var evalErr *fasteval.EvalError
	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrOverflow {
		t.Fatalf("Eval() error = %v, want ErrOverflow", err)
	}
}
