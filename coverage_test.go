package fasteval_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"go.dw1.io/fasteval"
)

func TestAdditionalPublicAPIBehavior(t *testing.T) {
	t.Parallel()

	functions := map[string]any{testDouble: func(value int) int { return value * 2 }}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunctions(functions))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	delete(functions, "double")

	program, err := fasteval.CompileAsWith[int](compiler, "double(value)")
	if err != nil {
		t.Fatalf("CompileAsWith() error = %v", err)
	}

	got, err := program.Eval(context.Background(), map[string]any{testValue: 21})
	if err != nil || got != 42 {
		t.Fatalf("Program.Eval() = %d, %v; want 42, nil", got, err)
	}

	got, err = fasteval.EvalAsWith[int](context.Background(), compiler, "double(value)", map[string]any{testValue: 4})
	if err != nil || got != 8 {
		t.Fatalf("EvalAsWith() = %d, %v; want 8, nil", got, err)
	}
}

func TestExpressionPublicAccessors(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction(testDouble, func(value int) int { return value * 2 }))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("double(value)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if gotSource := expression.Source(); gotSource != "double(value)" {
		t.Fatalf("Source() = %q, want double(value)", gotSource)
	}

	root := expression.AST()
	if root.Span() != (fasteval.Span{Start: 0, End: len("double(value)"), Line: 1, Column: 1}) {
		t.Fatalf("AST().Span() = %+v", root.Span())
	}

	if root.Text() != "call" || root.Kind() != fasteval.NodeCall {
		t.Fatalf("AST() = kind %v, text %q; want call, call", root.Kind(), root.Text())
	}
}

func TestNilPublicAPIAccessors(t *testing.T) {
	t.Parallel()

	var nilExpression *fasteval.Expression
	if nilExpression.Source() != "" || nilExpression.AST() != nil {
		t.Fatal("nil Expression accessors returned non-zero values")
	}

	var nilNode *fasteval.Node

	emptySpan := fasteval.Span{Start: 0, End: 0, Line: 0, Column: 0}
	if nilNode.Kind() != fasteval.NodeInvalid || nilNode.Span() != emptySpan ||
		nilNode.Text() != "" || nilNode.Children() != nil {
		t.Fatal("nil Node accessors returned non-zero values")
	}
}

func TestAdditionalOptionAndErrorBehavior(t *testing.T) {
	t.Parallel()

	_, err := fasteval.NewCompiler(
		fasteval.WithFunction(testSame, func() int { return 1 }),
		fasteval.WithFunctions(map[string]any{testSame: func() int { return 2 }}),
	)
	if err == nil {
		t.Fatal("NewCompiler() duplicate function error = nil")
	}

	_, err = fasteval.Eval(context.Background(), testValue, nil, nil)
	if err == nil {
		t.Fatal("Eval() nil option error = nil")
	}

	_, err = fasteval.Eval(context.Background(), testValue, nil, fasteval.WithResolver(nil))
	if err == nil {
		t.Fatal("Eval() nil resolver error = nil")
	}

	_, err = fasteval.Eval(context.Background(), "missing.path", nil)

	var evalErr *fasteval.EvalError
	if !errors.As(err, &evalErr) {
		t.Fatalf("Eval() error = %v, want EvalError", err)
	}

	emptySpan := fasteval.Span{Start: 0, End: 0, Line: 0, Column: 0}
	if evalErr.Operation() == "" || evalErr.Path() == "" || evalErr.Span() == emptySpan {
		t.Fatalf("EvalError accessors = operation %q, path %q, span %+v", evalErr.Operation(), evalErr.Path(), evalErr.Span())
	}
}

func TestNilDiagnosticsErrorAccessors(t *testing.T) {
	t.Parallel()

	var nilDiagnostics *fasteval.DiagnosticsError
	if nilDiagnostics.Error() != "expression compilation failed" ||
		nilDiagnostics.Len() != 0 || nilDiagnostics.All() != nil {
		t.Fatal("nil DiagnosticsError accessors returned unexpected values")
	}
}

func TestAdditionalBuiltinResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "trimPrefix('fasteval', 'fast')", want: "eval"},
		{source: "trimSuffix('fasteval', 'eval')", want: "fast"},
		{source: "lower('FAST')", want: "fast"},
		{source: "split('a,b', ',')", want: []any{"a", "b"}},
		{source: "splitAfter('a,b', ',')", want: []any{"a,", "b"}},
		{source: "replace('aaa', 'a', 'b', 2)", want: "bba"},
		{source: "repeat('ab', 2)", want: "abab"},
		{source: "indexOf('fasteval', 'eval')", want: 4},
		{source: "lastIndexOf('ababa', 'ba')", want: 3},
		{source: "hasSuffix('fasteval', 'eval')", want: true},
		{source: "int8('0x7f')", want: int8(127)},
		{source: "uint16('65535')", want: uint16(math.MaxUint16)},
		{source: "float32('1.5')", want: float32(1.5)},
		{source: "complex64('1+2i')", want: complex64(1 + 2i)},
		{source: "abs(value)", want: uint16(3)},
		{source: "imag(complex(2, 3))", want: float64(3)},
		{source: "conj(complex(2, 3))", want: complex(2, -3)},
		{source: "bitnot(1)", want: ^int(1)},
		{source: "bitushr(8, 2)", want: uint64(2)},
		{source: "date('2026-08-24')", want: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)},
		{source: "date('24/08/2026', '02/01/2006')", want: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)},
		{source: "date('2026-08-24 07:30', '2006-01-02 15:04', 'UTC')", want: time.Date(2026, 8, 24, 7, 30, 0, 0, time.UTC)},
		{source: "timezone('UTC')", want: time.UTC},
	}

	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			variables := map[string]any{testValue: uint16(3)}

			got, err := fasteval.Eval(context.Background(), testCase.source, variables)
			if err != nil || !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v (%T), %v; want %#v (%T), nil", got, got, err, testCase.want, testCase.want)
			}
		})
	}
}

func TestAdditionalBuiltinErrors(t *testing.T) {
	t.Parallel()

	sources := []string{
		"trim()",
		"trim(1)",
		"trimPrefix('a', 1)",
		"split('a', 1)",
		"replace('a', 'a', 1)",
		"replace('a', 'a', 'b', 'many')",
		"repeat(1, 2)",
		"repeat('a', -1)",
		"int8('128')",
		"uint8('-1')",
		"float32('not-a-number')",
		"complex64('not-a-number')",
		"min([])",
		"abs(nil)",
		"abs('x')",
		"ceil(nil)",
		"real('x')",
		"complex(value, double)",
		"bitnot('x')",
		"bitushr(-1, 1)",
		"duration(1)",
		"duration('invalid')",
		"date(1)",
		"date('2026', 1)",
		"date('invalid', '2006')",
		"date('2026', '2006', 1)",
		"date('2026', '2006', 'Invalid/Zone')",
		"date('invalid', '2006', 'UTC')",
		"timezone(1)",
		"timezone('Invalid/Zone')",
		"keys(1)",
		"values(1)",
		"toPairs(1)",
		"fromPairs([[1]])",
		"fromPairs([[[1], 2]])",
		"len(1)",
		"fromJSON(1)",
		"fromJSON('{')",
		"fromJSON('1 2')",
		"fromJSON('18446744073709551616')",
		"fromBase64(1)",
		"fromBase64('%%%')",
	}

	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			_, err := fasteval.Eval(context.Background(), source, map[string]any{
				testValue: float32(1), "double": float64(1),
			})
			if err == nil {
				t.Fatal("Eval() error = nil")
			}
		})
	}
}

func TestNativeIntegerNumericTypePaths(t *testing.T) {
	t.Parallel()

	signed := []any{int(3), int8(3), int16(3), int32(3), int64(3)}
	unsigned := []any{uint(3), uint8(3), uint16(3), uint32(3), uint64(3), uintptr(3)}

	for _, value := range append(signed, unsigned...) {
		name := reflect.TypeOf(value).String()
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			variables := map[string]any{testLeft: value, testRight: value, testValue: value}

			sources := []string{
				testPlusValueSource, "~value", "left + right", "left & right", "left << 1", "left > 1",
			}
			for _, source := range sources {
				got, err := fasteval.Eval(context.Background(), source, variables)
				if err != nil {
					t.Fatalf("Eval(%q) with %s error = %v", source, name, err)
				}

				if got == nil {
					t.Fatalf("Eval(%q) with %s returned nil", source, name)
				}
			}
		})
	}

	for _, source := range []string{testPlusValueSource, "~value"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), source, map[string]any{testValue: time.Duration(3)})
			if err != nil || got == nil {
				t.Fatalf("Eval(%q) with time.Duration = %#v, %v", source, got, err)
			}
		})
	}
}

func TestNativeRealNumericTypePaths(t *testing.T) {
	t.Parallel()

	for _, value := range []any{float32(3), float64(3), complex64(3 + 2i), complex128(3 + 2i)} {
		name := reflect.TypeOf(value).String()
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			variables := map[string]any{testLeft: value, testRight: value, testValue: value}

			sources := []string{
				testPlusValueSource, "-value", "left + right", "left - right",
				"left * right", "left / right", "left ** right",
			}
			for _, source := range sources {
				got, err := fasteval.Eval(context.Background(), source, variables)
				if err != nil {
					t.Fatalf("Eval(%q) with %s error = %v", source, name, err)
				}

				if got == nil {
					t.Fatalf("Eval(%q) with %s returned nil", source, name)
				}
			}
		})
	}

	for _, value := range []any{float32(3), float64(3)} {
		t.Run(reflect.TypeOf(value).String()+" remainder", func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), "left % right", map[string]any{
				testLeft: value, testRight: value,
			})
			if err != nil || reflect.ValueOf(got).Float() != 0 {
				t.Fatalf("floating remainder with %T = %#v, %v; want zero, nil", value, got, err)
			}
		})
	}
}

func TestNativeNumericComparisonPaths(t *testing.T) {
	t.Parallel()

	signed := []any{int(3), int8(3), int16(3), int32(3), int64(3)}
	unsigned := []any{uint(3), uint8(3), uint16(3), uint32(3), uint64(3), uintptr(3)}

	for _, value := range append(signed, unsigned...) {
		for _, operator := range []string{">", ">=", "<", "<="} {
			got, err := fasteval.Eval(context.Background(), "left "+operator+" right", map[string]any{
				testLeft: value, testRight: value,
			})
			if err != nil || got != (operator == ">=" || operator == "<=") {
				t.Fatalf("%T %s comparison = %#v, %v", value, operator, got, err)
			}
		}
	}
}

func TestAdditionalSequenceAndHigherOrderResults(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("positive", func(value int) bool { return value > 0 }),
		fasteval.WithFunction("add", func(left, right int) int { return left + right }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	tests := []struct {
		source string
		want   any
	}{
		{source: "all([], positive)", want: true},
		{source: "any([], positive)", want: false},
		{source: "one([], positive)", want: false},
		{source: "none([], positive)", want: true},
		{source: "one([1, 2], positive)", want: false},
		{source: "find([-1, 0], positive)", want: nil},
		{source: "findIndex([-1, 0], positive)", want: -1},
		{source: "findLast([-1, 0], positive)", want: nil},
		{source: "findLastIndex([-1, 0], positive)", want: -1},
		{source: "count([1, 2, 3])", want: 3},
		{source: "count([])", want: 0},
		{source: "reduce([], add)", want: nil},
		{source: "reduce([1, 2, 3], add)", want: 6},
		{source: "sum([])", want: 0},
		{source: "first([])", want: nil},
		{source: "last([])", want: nil},
		{source: "first([1, 2])", want: 1},
		{source: "last([1, 2])", want: 2},
		{source: "take([1, 2], 20)", want: []any{1, 2}},
		{source: "take([1, 2], -20)", want: []any{1, 2}},
	}

	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			expression, compileErr := compiler.Compile(testCase.source)
			if compileErr != nil {
				t.Fatalf("Compile() error = %v", compileErr)
			}

			got, evalErr := expression.Eval(context.Background(), nil)
			if evalErr != nil || !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v, %v; want %#v, nil", got, evalErr, testCase.want)
			}
		})
	}
}

func TestAdditionalMeanResults(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		values []float64
		want   float64
	}{
		{name: "positive infinity", values: []float64{math.Inf(1), 1}, want: math.Inf(1)},
		{name: "negative infinity", values: []float64{math.Inf(-1), -1}, want: math.Inf(-1)},
		{name: "opposite infinities", values: []float64{math.Inf(1), math.Inf(-1)}, want: math.NaN()},
		{name: "not a number", values: []float64{math.NaN(), 1}, want: math.NaN()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, evalErr := fasteval.Eval(context.Background(), testMeanValuesSource, map[string]any{
				testValues: testCase.values,
			})

			gotFloat, ok := got.(float64)
			if evalErr != nil || !ok || !(gotFloat == testCase.want || math.IsNaN(gotFloat) && math.IsNaN(testCase.want)) {
				t.Fatalf("mean(%v) = %#v, %v; want %v, nil", testCase.values, got, evalErr, testCase.want)
			}
		})
	}
}

func TestAdditionalSequenceAndHigherOrderErrors(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("notPredicate", func(int) int { return 1 }),
		fasteval.WithFunction("sliceKey", func(int) []int { return []int{1} }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	sources := []string{
		"concat(1)",
		"flatten(1)",
		"uniq(1)",
		"join(1, ',')",
		"join([], 1)",
		"sum(1)",
		"mean([])",
		"mean(1)",
		testMeanValuesSource,
		"median([])",
		"median(1)",
		"first(1)",
		"take(1, 1)",
		"take([], 'one')",
		"reverse(1)",
		"sort(1)",
		"sort([1, 'a'])",
		"all(1, notPredicate)",
		"all([1], notPredicate)",
		"all([1], missing)",
		"map(1, notPredicate)",
		"filter(1, notPredicate)",
		"find(1, notPredicate)",
		"groupBy([1], sliceKey)",
		"count(1)",
		"reduce(1, notPredicate)",
		"sortBy(1, notPredicate)",
	}

	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			expression, compileErr := compiler.Compile(source)
			if compileErr != nil {
				t.Fatalf("Compile() error = %v", compileErr)
			}

			_, evalErr := expression.Eval(context.Background(), map[string]any{
				testValues: []uint64{1<<53 + 1},
			})
			if evalErr == nil {
				t.Fatal("Eval() error = nil")
			}
		})
	}
}

type coverageRecord struct {
	Name  string `json:"name"`
	Count int
}

func TestTypedNumericConversionPaths(t *testing.T) {
	t.Parallel()

	assertCoverageEvalAsValue[int](t, int8(7), 7)
	assertCoverageEvalAsValue[int8](t, int16(7), 7)
	assertCoverageEvalAsValue[int16](t, int32(7), 7)
	assertCoverageEvalAsValue[int32](t, int64(7), 7)
	assertCoverageEvalAsValue[int64](t, uint64(7), 7)
	assertCoverageEvalAsValue[uint](t, int64(7), 7)
	assertCoverageEvalAsValue[uint8](t, uint16(7), 7)
	assertCoverageEvalAsValue[uint16](t, uint32(7), 7)
	assertCoverageEvalAsValue[uint32](t, uint64(7), 7)
	assertCoverageEvalAsValue[uint64](t, int64(7), 7)
	assertCoverageEvalAsValue[uintptr](t, uint64(7), 7)
	assertCoverageEvalAsValue[float32](t, int16(7), 7)
	assertCoverageEvalAsValue[float64](t, uint16(7), 7)
	assertCoverageEvalAsValue[int](t, float64(7), 7)
	assertCoverageEvalAsValue[uint](t, float32(7), 7)
	assertCoverageEvalAsValue[complex64](t, float32(7), 7)
	assertCoverageEvalAsValue[complex128](t, int16(7), 7)
	assertCoverageEvalAsValue[float64](t, complex128(7), 7)
	assertCoverageEvalAsValue[complex128](t, complex64(7+2i), 7+2i)
}

func TestTypedCompositeConversionPaths(t *testing.T) {
	t.Parallel()

	record, err := fasteval.EvalAs[coverageRecord](context.Background(), testValue, map[string]any{
		testValue: map[string]any{"name": testAda, "Count": int8(2)},
	})
	if err != nil || record != (coverageRecord{Name: "Ada", Count: 2}) {
		t.Fatalf("EvalAs[coverageRecord]() = %#v, %v", record, err)
	}

	records, err := fasteval.EvalAs[[]coverageRecord](context.Background(), testValue, map[string]any{
		testValue: []map[string]any{{"name": testAda, "Count": int8(2)}},
	})
	if err != nil || !reflect.DeepEqual(records, []coverageRecord{{Name: "Ada", Count: 2}}) {
		t.Fatalf("EvalAs[[]coverageRecord]() = %#v, %v", records, err)
	}
}

func TestTypedCollectionAndPointerConversionPaths(t *testing.T) {
	t.Parallel()

	array, err := fasteval.EvalAs[[2]int](context.Background(), testValue, map[string]any{
		testValue: []int8{1, 2},
	})
	if err != nil || array != [2]int{1, 2} {
		t.Fatalf("EvalAs[[2]int]() = %#v, %v", array, err)
	}

	mapped, err := fasteval.EvalAs[map[int]int](context.Background(), testValue, map[string]any{
		testValue: map[int8]int16{1: 2},
	})
	if err != nil || !reflect.DeepEqual(mapped, map[int]int{1: 2}) {
		t.Fatalf("EvalAs[map[int]int]() = %#v, %v", mapped, err)
	}

	pointer, err := fasteval.EvalAs[*int](context.Background(), testValue, map[string]any{
		testValue: int8(7),
	})
	if err != nil || pointer == nil || *pointer != 7 {
		t.Fatalf("EvalAs[*int]() = %#v, %v", pointer, err)
	}
}

func TestTypedConversionErrors(t *testing.T) {
	t.Parallel()

	_, negativeToUnsignedErr := fasteval.EvalAs[uint8](
		context.Background(), testValue, map[string]any{testValue: int8(-1)},
	)
	_, unsignedOverflowErr := fasteval.EvalAs[int8](
		context.Background(), testValue, map[string]any{testValue: uint16(256)},
	)
	_, fractionErr := fasteval.EvalAs[int](context.Background(), testValue, map[string]any{testValue: 1.5})
	_, nanErr := fasteval.EvalAs[int](context.Background(), testValue, map[string]any{testValue: math.NaN()})
	_, negativeFloatErr := fasteval.EvalAs[uint](context.Background(), testValue, map[string]any{testValue: -1.0})
	_, narrowFloatErr := fasteval.EvalAs[float32](context.Background(), testValue, map[string]any{testValue: 1.1})
	_, imaginaryErr := fasteval.EvalAs[float64](
		context.Background(), testValue, map[string]any{testValue: complex(1, 1)},
	)
	_, narrowComplexErr := fasteval.EvalAs[complex64](
		context.Background(), testValue, map[string]any{testValue: complex(1e100, 0)},
	)
	_, arrayLengthErr := fasteval.EvalAs[[2]int](
		context.Background(), testValue, map[string]any{testValue: []int{1}},
	)
	_, sliceTypeErr := fasteval.EvalAs[[]int](context.Background(), testValue, map[string]any{testValue: 1})
	_, mapTypeErr := fasteval.EvalAs[map[string]int](
		context.Background(), testValue, map[string]any{testValue: []int{1}},
	)
	_, structFieldErr := fasteval.EvalAs[coverageRecord](
		context.Background(), testValue, map[string]any{testValue: map[string]any{"unknown": 1}},
	)

	tests := []error{
		negativeToUnsignedErr, unsignedOverflowErr, fractionErr, nanErr, negativeFloatErr, narrowFloatErr,
		imaginaryErr, narrowComplexErr, arrayLengthErr, sliceTypeErr, mapTypeErr, structFieldErr,
	}

	for index, err := range tests {
		if err == nil {
			t.Fatalf("conversion case %d error = nil", index)
		}
	}
}

func TestIndexAcceptsAllIntegerWidths(t *testing.T) {
	t.Parallel()

	indices := []any{
		int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1), uintptr(1),
	}
	for _, index := range indices {
		got, err := fasteval.Eval(context.Background(), "values[index]", map[string]any{
			testValues: []string{testZero, testOne}, "index": index,
		})
		if err != nil || got != testOne {
			t.Fatalf("index %T = %#v, %v; want one, nil", index, got, err)
		}
	}

	for _, index := range []any{int64(-1), uint64(math.MaxUint64), 1.5, nil} {
		_, err := fasteval.Eval(context.Background(), "values[index]", map[string]any{
			testValues: []string{testZero, testOne}, "index": index,
		})
		if err == nil {
			t.Fatalf("index %T(%v) error = nil", index, index)
		}
	}
}

func TestEqualityAcrossConcreteKinds(t *testing.T) {
	t.Parallel()

	now := time.Now()
	channel := make(chan int)
	function := func() {}

	var nilFunction func()

	tests := []struct {
		name  string
		left  any
		right any
		want  bool
	}{
		{name: "bool", left: true, right: true, want: true},
		{name: testString, left: testSame, right: testSame, want: true},
		{name: "different array types", left: [2]int{1, 2}, right: [2]int8{1, 2}, want: true},
		{name: "different slice lengths", left: []int{1}, right: []int{1, 2}, want: false},
		{name: "different slice values", left: []int{1, 2}, right: []int{1, 3}, want: false},
		{name: "same instant", left: now, right: now.In(time.FixedZone("other", 3600)), want: true},
		{name: "time and struct", left: now, right: struct{}{}, want: false},
		{name: "same channel", left: channel, right: channel, want: true},
		{name: "different channels", left: channel, right: make(chan int), want: false},
		{name: "same function", left: function, right: function, want: false},
		{name: "nil functions", left: nilFunction, right: nilFunction, want: true},
		{
			name: "different structs",
			left: coverageRecord{Name: "a", Count: 0}, right: coverageRecord{Name: "b", Count: 0}, want: false,
		},
		{name: "signed and unsigned", left: int64(7), right: uint8(7), want: true},
		{name: "float and complex", left: float32(7), right: complex128(7), want: true},
		{name: "NaN", left: math.NaN(), right: math.NaN(), want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
				testLeft: testCase.left, testRight: testCase.right,
			})
			if err != nil || got != testCase.want {
				t.Fatalf("Eval() = %#v, %v; want %t, nil", got, err, testCase.want)
			}
		})
	}
}

func TestMapEqualityAcrossScalarKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  any
		right any
		want  bool
	}{
		{name: "bool", left: map[bool]bool{true: true}, right: map[bool]bool{true: true}, want: true},
		{name: "string", left: map[string]string{"a": "b"}, right: map[string]string{"a": "b"}, want: true},
		{name: testInt, left: map[int]int{1: 2}, right: map[int]int{1: 2}, want: true},
		{name: testInt8, left: map[int8]int8{1: 2}, right: map[int8]int8{1: 2}, want: true},
		{name: "int16", left: map[int16]int16{1: 2}, right: map[int16]int16{1: 2}, want: true},
		{name: "int32", left: map[int32]int32{1: 2}, right: map[int32]int32{1: 2}, want: true},
		{name: "int64", left: map[int64]int64{1: 2}, right: map[int64]int64{1: 2}, want: true},
		{name: "uint", left: map[uint]uint{1: 2}, right: map[uint]uint{1: 2}, want: true},
		{name: "uint8", left: map[uint8]uint8{1: 2}, right: map[uint8]uint8{1: 2}, want: true},
		{name: "uint16", left: map[uint16]uint16{1: 2}, right: map[uint16]uint16{1: 2}, want: true},
		{name: "uint32", left: map[uint32]uint32{1: 2}, right: map[uint32]uint32{1: 2}, want: true},
		{name: testUint64, left: map[uint64]uint64{1: 2}, right: map[uint64]uint64{1: 2}, want: true},
		{name: "uintptr", left: map[uintptr]uintptr{1: 2}, right: map[uintptr]uintptr{1: 2}, want: true},
		{name: "float32", left: map[float32]float32{1: 2}, right: map[float32]float32{1: 2}, want: true},
		{name: "float64", left: map[float64]float64{1: 2}, right: map[float64]float64{1: 2}, want: true},
		{name: "complex64", left: map[complex64]complex64{1: 2}, right: map[complex64]complex64{1: 2}, want: true},
		{name: "complex128", left: map[complex128]complex128{1: 2}, right: map[complex128]complex128{1: 2}, want: true},
		{name: "cross numeric key", left: map[int8]string{1: "a"}, right: map[uint64]string{1: "a"}, want: true},
		{name: "different length", left: map[int]int{1: 2}, right: map[int]int{}, want: false},
		{name: "different value", left: map[int]int{1: 2}, right: map[int]int{1: 3}, want: false},
		{name: "composite value", left: map[int][]int{1: {2}}, right: map[int][]int{1: {2}}, want: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
				testLeft: testCase.left, testRight: testCase.right,
			})
			if err != nil || got != testCase.want {
				t.Fatalf("Eval() = %#v, %v; want %t, nil", got, err, testCase.want)
			}
		})
	}
}

func TestTemplateFormatsConcreteScalarKinds(t *testing.T) {
	t.Parallel()

	template, err := fasteval.CompileTemplate("{{value}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	if template.Source() != "{{value}}" {
		t.Fatalf("Template.Source() = %q", template.Source())
	}

	tests := []struct {
		value any
		want  string
	}{
		{value: nil, want: ""},
		{value: testText, want: testText},
		{value: []byte("bytes"), want: "bytes"},
		{value: true, want: "true"},
		{value: int(1), want: "1"},
		{value: int8(1), want: "1"},
		{value: int16(1), want: "1"},
		{value: int32(1), want: "1"},
		{value: int64(1), want: "1"},
		{value: uint(1), want: "1"},
		{value: uint8(1), want: "1"},
		{value: uint16(1), want: "1"},
		{value: uint32(1), want: "1"},
		{value: uint64(1), want: "1"},
		{value: uintptr(1), want: "1"},
		{value: float32(1.5), want: "1.5"},
		{value: float64(1.5), want: "1.5"},
		{value: complex64(1 + 2i), want: "(1+2i)"},
		{value: complex128(1 + 2i), want: "(1+2i)"},
		{value: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC), want: "2026-08-24T00:00:00Z"},
	}

	for _, testCase := range tests {
		got, renderErr := template.Render(context.Background(), map[string]any{testValue: testCase.value})
		if renderErr != nil || got != testCase.want {
			t.Fatalf("Render(%T) = %q, %v; want %q, nil", testCase.value, got, renderErr, testCase.want)
		}
	}

	var nilTemplate *fasteval.Template
	if nilTemplate.Source() != "" {
		t.Fatalf("nil Template.Source() = %q", nilTemplate.Source())
	}
}

func TestAdditionalEvaluatorOperatorPaths(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "name !~ pattern", vars: map[string]any{testName: testFasteval, testPattern: "^slow"}, want: true},
		{source: "start - end", vars: map[string]any{testStart: now.Add(time.Hour), "end": now}, want: time.Hour},
		{source: "duration + start", vars: map[string]any{testDuration: time.Hour, testStart: now}, want: now.Add(time.Hour)},
		{source: "needle in value", vars: map[string]any{testNeedle: 1, testValue: 1}, want: true},
		{source: "needle in value", vars: map[string]any{testNeedle: nil, testValue: nil}, want: true},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
		if err != nil || !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestAdditionalEvaluatorOperatorErrors(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		source string
		vars   map[string]any
	}{
		{source: "1 ? 2 : 3", vars: nil},
		{source: "1 && true", vars: nil},
		{source: "true && 1", vars: nil},
		{source: "1 || false", vars: nil},
		{source: "false || 1", vars: nil},
		{source: "value =~ 'x'", vars: map[string]any{testValue: 1}},
		{source: "'x' =~ pattern", vars: map[string]any{testPattern: 1}},
		{source: "'x' =~ pattern", vars: map[string]any{testPattern: "["}},
		{source: "start * duration", vars: map[string]any{testStart: now, testDuration: time.Hour}},
		{source: "duration * 2", vars: map[string]any{testDuration: time.Hour}},
		{source: "value()", vars: map[string]any{testValue: 1}},
		{source: "value()", vars: map[string]any{testValue: func(chan int) {}}},
	}

	for _, testCase := range tests {
		_, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
		if err == nil {
			t.Fatalf("Eval(%q) error = nil", testCase.source)
		}
	}
}

func TestEveryBuiltinRejectsInvalidArity(t *testing.T) {
	t.Parallel()

	zeroArgumentBuiltins := []string{
		"all", "any", "one", "none", "map", "filter", "find", "findIndex", "findLast", "findLastIndex",
		"groupBy", "count", "concat", "flatten", "uniq", "join", "reduce", "sum", "mean", "median", testFirst,
		"last", "take", "reverse", "sort", "sortBy", testKeys, "values", "toPairs", "fromPairs", testLen, "get",
		"trim", "trimPrefix", "trimSuffix", "upper", "lower", "split", "splitAfter", "replace", "repeat", "indexOf",
		"lastIndexOf", "hasPrefix", "hasSuffix", "int", "int8", "int16", "int32", "int64", "uint", "uint8",
		"uint16", "uint32", testUint64, "uintptr", "float", "float32", "float64", "complex64", "complex128", testMin,
		testMax, "abs", "ceil", "floor", "round", "real", "imag", "complex", "conj", "bitand", "bitor", "bitxor",
		"bitnand", "bitnot", "bitshl", "bitshr", "bitushr", testDuration, "date", "timezone", "type", testString,
		"toJSON", "fromJSON", "toBase64", "fromBase64",
	}

	for _, name := range zeroArgumentBuiltins {
		_, err := fasteval.Eval(context.Background(), name+"()", nil)
		if err == nil {
			t.Fatalf("%s() error = nil", name)
		}
	}

	_, err := fasteval.Eval(context.Background(), "now(1)", nil)
	if err == nil {
		t.Fatal("now(1) error = nil")
	}
}

func TestAdditionalAccessBuiltinResults(t *testing.T) {
	t.Parallel()

	user := account{Name: testAda, note: ""}
	tests := []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "get(user, 'Name', 'missing')", vars: map[string]any{testUser: user}, want: testAda},
		{source: "get(user, 'Missing', 'fallback')", vars: map[string]any{testUser: user}, want: testFallback},
		{source: "get('abc', 1, 'missing')", vars: nil, want: byte('b')},
		{source: "get('abc', 20, 'missing')", vars: nil, want: testMissing},
		{source: "get(nil, 'anything', 'fallback')", vars: nil, want: testFallback},
		{source: "len(channel)", vars: map[string]any{"channel": make(chan int, 2)}, want: 0},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
		if err != nil || !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestAdditionalEncodingBuiltinPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "toBase64(value)", vars: map[string]any{testValue: []byte("hello")}, want: "aGVsbG8="},
		{source: "fromJSON('-9007199254740993')", vars: nil, want: int64(-9_007_199_254_740_993)},
		{source: "fromJSON('1.25')", vars: nil, want: float64(1.25)},
		{
			source: "fromJSON('[1,{\"value\":2}]')", vars: nil,
			want: []any{float64(1), map[string]any{testValue: float64(2)}},
		},
		{source: testToJSONValueSource, vars: map[string]any{testValue: []int{1, 2}}, want: "[1,2]"},
		{source: testToJSONValueSource, vars: map[string]any{testValue: [2]int{1, 2}}, want: "[1,2]"},
		{source: testToJSONValueSource, vars: map[string]any{testValue: map[string]int{"a": 1}}, want: `{"a":1}`},
		{source: testToJSONValueSource, vars: map[string]any{testValue: map[string]int(nil)}, want: "null"},
		{source: testToJSONValueSource, vars: map[string]any{testValue: []int(nil)}, want: "null"},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
		if err != nil || !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}

	for _, value := range []any{complex(1, 1), make(chan int), map[int]string{1: "one"}, map[any]string{nil: "nil"}} {
		_, err := fasteval.Eval(context.Background(), testToJSONValueSource, map[string]any{testValue: value})
		if err == nil {
			t.Fatalf("toJSON(%T) error = nil", value)
		}
	}
}

func TestCompilerConvenienceFunctionsPropagateErrors(t *testing.T) {
	t.Parallel()

	for _, option := range []fasteval.CompilerOption{
		fasteval.WithFunction("", func() int { return 1 }),
		fasteval.WithFunction("nil", nil),
		fasteval.WithFunction("noResult", func() {}),
		fasteval.WithFunction("badSecondResult", func() (int, int) { return 1, 2 }),
	} {
		_, err := fasteval.NewCompiler(option)
		if err == nil {
			t.Fatal("NewCompiler() error = nil")
		}
	}

	var nilCompiler *fasteval.Compiler

	_, err := fasteval.CompileAsWith[int](nilCompiler, "1")
	if err == nil {
		t.Fatal("CompileAsWith(nil) error = nil")
	}

	_, err = fasteval.EvalAsWith[int](context.Background(), nilCompiler, "1", nil)
	if err == nil {
		t.Fatal("EvalAsWith(nil) error = nil")
	}

	_, err = fasteval.CompileAs[int]("[")
	if err == nil {
		t.Fatal("CompileAs(invalid) error = nil")
	}

	_, err = fasteval.EvalAs[int](context.Background(), "[", nil)
	if err == nil {
		t.Fatal("EvalAs(invalid) error = nil")
	}
}

func TestAdditionalSyntaxDiagnosticPaths(t *testing.T) {
	t.Parallel()

	sources := []string{
		"1 2",
		"true ? :",
		"true ? 1 :",
		"+",
		"* 1",
		"()",
		"(1",
		"(1, 2",
		"user.",
		"values[1",
		"{",
		"{1 2}",
		"{1:",
		"{1: 2",
		"call(1",
		"@",
		"1e+",
		"'unclosed",
		`'\q'`,
		"name\\",
		string([]byte{0xff}),
	}

	for _, source := range sources {
		_, err := fasteval.Compile(source)

		var diagnostics *fasteval.DiagnosticsError
		if !errors.As(err, &diagnostics) || diagnostics.Len() == 0 {
			t.Fatalf("Compile(%q) error = %v, want DiagnosticsError", source, err)
		}
	}
}

func TestAdditionalNumericAndStatisticBuiltinPaths(t *testing.T) {
	t.Parallel()

	absTests := []struct {
		value any
		want  any
	}{
		{value: int(-3), want: int(3)},
		{value: int16(-3), want: int16(3)},
		{value: int32(-3), want: int32(3)},
		{value: int64(-3), want: int64(3)},
		{value: uint(3), want: uint(3)},
		{value: float32(-3.5), want: float32(3.5)},
		{value: float64(-3.5), want: float64(3.5)},
		{value: complex64(3 + 4i), want: float32(5)},
		{value: complex128(3 + 4i), want: float64(5)},
	}
	for _, testCase := range absTests {
		got, err := fasteval.Eval(context.Background(), "abs(value)", map[string]any{testValue: testCase.value})
		if err != nil || got != testCase.want {
			t.Fatalf("abs(%T) = %#v, %v; want %#v, nil", testCase.value, got, err, testCase.want)
		}
	}
}

func TestAdditionalStatisticBuiltinPaths(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "sum(values)", vars: map[string]any{testValues: []string{"a", "b"}}, want: "ab"},
		{source: testMedianValuesSource, vars: map[string]any{testValues: []float64{1, 1}}, want: float64(1)},
		{
			source: testMedianValuesSource,
			vars:   map[string]any{testValues: []float64{math.Inf(-1), math.Inf(1)}}, want: math.NaN(),
		},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
		gotFloat, gotIsFloat := got.(float64)
		wantFloat, wantIsFloat := testCase.want.(float64)

		equal := reflect.DeepEqual(got, testCase.want) ||
			gotIsFloat && wantIsFloat && math.IsNaN(gotFloat) && math.IsNaN(wantFloat)
		if err != nil || !equal {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func assertCoverageEvalAsValue[T comparable](t *testing.T, value any, want T) {
	t.Helper()

	got, err := fasteval.EvalAs[T](context.Background(), testValue, map[string]any{testValue: value})
	if err != nil || got != want {
		t.Fatalf("EvalAs[%T](%T) = %#v, %v; want %#v, nil", want, value, got, err, want)
	}
}
