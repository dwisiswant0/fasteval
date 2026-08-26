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

const (
	testPowerSource     = "left ** right"
	testLeftShiftSource = "left << right"
)

func TestEvalLanguageCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		vars   map[string]any
		want   any
	}{
		{name: "precedence", source: "1 + 5 ** 3 % 2 * 5", vars: nil, want: 6},
		{name: "short circuit and", source: "false && missing", vars: nil, want: false},
		{name: "short circuit or", source: "true || missing", vars: nil, want: true},
		{name: "coalesce", source: "value ?? 7", vars: map[string]any{testValue: nil}, want: 7},
		{name: "ternary", source: "score >= 80 ? 'pass' : 'fail'", vars: map[string]any{testScore: 85}, want: "pass"},
		{name: "membership", source: "needle in ('a', 'b', 'c')", vars: map[string]any{testNeedle: "b"}, want: true},
		{name: "list", source: "[1, 2, 3][1]", vars: nil, want: 2},
		{name: testMap, source: "{'answer': 42}.answer", vars: nil, want: 42},
		{name: "byte index", source: "'é'[0]", vars: nil, want: byte(0xc3)},
		{name: "regex", source: "name =~ '^fast.*'", vars: map[string]any{testName: testFasteval}, want: true},
		{name: "complex", source: "(1 + 2i) * (2 + 0i)", vars: nil, want: complex(2, 4)},
		{name: "fractional literal power", source: "4.0 ** 0.5", vars: nil, want: float64(2)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v (%T), want %#v (%T)", got, got, testCase.want, testCase.want)
			}
		})
	}
}

func TestFloatToSignedIntegerRejectsExclusiveUpperBound(t *testing.T) {
	t.Parallel()

	value := float64(uint64(1) << 63)

	_, err := fasteval.EvalAs[int64](context.Background(), testValue, map[string]any{testValue: value})
	if err == nil {
		t.Fatal("EvalAs[int64]() error = nil, want range error")
	}

	if ^uint(0)>>63 != 0 {
		_, err = fasteval.EvalAs[int](context.Background(), testValue, map[string]any{testValue: value})
		if err == nil {
			t.Fatal("EvalAs[int]() error = nil, want range error")
		}
	}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("accept", func(int64) bool { return true }))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("accept(value)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = expression.Eval(context.Background(), map[string]any{testValue: value})
	if err == nil {
		t.Fatal("function argument conversion error = nil")
	}
}

func TestComplex64ConversionChecksFiniteCompanion(t *testing.T) {
	t.Parallel()

	value := complex(math.Inf(1), 1+math.Ldexp(1, -30))

	_, err := fasteval.Eval(context.Background(), "complex64(value)", map[string]any{testValue: value})
	if err == nil {
		t.Fatal("Eval() error = nil, want complex64 precision error")
	}
}

func TestFloat32LiteralConversionAvoidsDoubleRounding(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "float32(0x1.00000100000008p0)", nil)

	want := math.Float32frombits(0x3f800001)
	if err != nil || got != want {
		t.Fatalf("Eval() = %#v, %v; want %#v, nil", got, err, want)
	}
}

func TestTimeSubtractionHandlesMinimumDuration(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0).UTC()
	duration := time.Duration(math.MinInt64)

	got, err := fasteval.Eval(context.Background(), "start - duration", map[string]any{
		testStart: start, testDuration: duration,
	})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	want := start.Add(time.Duration(math.MaxInt64)).Add(time.Nanosecond)

	gotTime, ok := got.(time.Time)
	if !ok || !gotTime.Equal(want) {
		t.Fatalf("Eval() = %#v, want %v", got, want)
	}
}

func TestUnaryDurationOperations(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		value  time.Duration
		want   time.Duration
	}{
		{source: testPlusValueSource, value: time.Second, want: time.Second},
		{source: testNegateValueSource, value: time.Second, want: -time.Second},
	} {
		got, err := fasteval.Eval(
			context.Background(), testCase.source, map[string]any{testValue: testCase.value},
		)
		if err != nil {
			t.Fatalf("Eval(%q) error = %v", testCase.source, err)
		}

		if got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, want %#v", testCase.source, got, testCase.want)
		}
	}

	_, err := fasteval.Eval(
		context.Background(), testNegateValueSource, map[string]any{testValue: time.Duration(math.MinInt64)},
	)

	var evalErr *fasteval.EvalError
	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrOverflow {
		t.Fatalf("Eval() error = %v, want ErrOverflow", err)
	}
}

func TestDurationEqualityIsSymmetricAndTypeSensitive(t *testing.T) {
	t.Parallel()

	variables := map[string]any{"integer": int64(1), "duration": time.Duration(1)}
	for _, source := range []string{"integer == duration", "duration == integer"} {
		got, err := fasteval.Eval(context.Background(), source, variables)
		if err != nil || got != false {
			t.Fatalf("Eval(%q) = %#v, %v; want false, nil", source, got, err)
		}
	}
}

func TestNestedTypedNilEqualityMatchesScalarEquality(t *testing.T) {
	t.Parallel()

	var (
		nilPointer *int
		nilMap     map[string]int
		nilSlice   []int
	)

	tests := []struct {
		name  string
		left  any
		right any
	}{
		{name: "pointer in slice", left: []any{nilPointer}, right: []any{nil}},
		{name: "map in slice", left: []any{nilMap}, right: []any{nil}},
		{name: "slice in map", left: map[string]any{testValue: nilSlice}, right: map[string]any{testValue: nil}},
	}
	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
			testLeft: testCase.left, testRight: testCase.right,
		})
		if err != nil || got != true {
			t.Fatalf("%s: Eval() = %#v, %v; want true, nil", testCase.name, got, err)
		}
	}

	got, err := fasteval.Eval(context.Background(), "[value?.missing,] == [nil,]", map[string]any{
		testValue: nilPointer,
	})
	if err != nil || got != true {
		t.Fatalf("optional access equality = %#v, %v; want true, nil", got, err)
	}
}

func TestDurationOperationsAdaptNumericLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "duration('1ns') + 1", want: 2 * time.Nanosecond},
		{source: "1 + duration('1ns')", want: 2 * time.Nanosecond},
		{source: "duration('2ns') - 1", want: time.Nanosecond},
		{source: "3 - duration('1ns')", want: 2 * time.Nanosecond},
		{source: "duration('2ns') > 1", want: true},
		{source: "1 < duration('2ns')", want: true},
	}
	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, nil)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}

	for _, source := range []string{"value + 1", "1 + value"} {
		_, err := fasteval.Eval(context.Background(), source, map[string]any{
			testValue: time.Duration(math.MaxInt64),
		})

		var evalErr *fasteval.EvalError
		if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrOverflow {
			t.Fatalf("Eval(%q) error = %v, want ErrOverflow", source, err)
		}
	}

	_, err := fasteval.Eval(context.Background(), "duration('1ns') + value", map[string]any{
		testValue: int64(1),
	})
	if err == nil {
		t.Fatal("duration plus concrete integer error = nil")
	}
}

func TestCompileAndEvaluateNativeNumbers(t *testing.T) {
	t.Parallel()

	expr, err := fasteval.Compile("left + 2")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expr.Eval(context.Background(), map[string]any{testLeft: int8(40)})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if got != int8(42) {
		t.Fatalf("Eval() = %#v (%T), want int8(42)", got, got)
	}
}

func TestParenthesizedUntypedArithmeticAdaptsToConcreteOperand(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "left + (1 + 1)", map[string]any{testLeft: int8(40)})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if got != int8(42) {
		t.Fatalf("Eval() = %#v (%T), want int8(42)", got, got)
	}
}

func TestMixedConcreteArithmeticRequiresConversion(t *testing.T) {
	t.Parallel()

	_, err := fasteval.Eval(context.Background(), "left + right", map[string]any{
		testLeft:  int8(1),
		testRight: int16(2),
	})

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrType {
		t.Fatalf("Eval() error = %v, want ErrType", err)
	}
}

func TestNumericEqualityIsLosslessAcrossTypes(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "a == b", map[string]any{
		"a": int64(1 << 53),
		"b": float64(1 << 53),
	})
	if err != nil || got != true {
		t.Fatalf("exact equality = %#v, %v; want true, nil", got, err)
	}

	got, err = fasteval.Eval(context.Background(), "a == b", map[string]any{
		"a": int64(1<<53 + 1),
		"b": float64(1<<53 + 1),
	})
	if err != nil || got != false {
		t.Fatalf("lossy equality = %#v, %v; want false, nil", got, err)
	}
}

func TestOversizedLiteralEqualityIsExact(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		want   bool
	}{
		{source: "18446744073709551616 == 18446744073709551616", want: true},
		{source: "18446744073709551616 == 18446744073709551617", want: false},
		{source: "18446744073709551616 == nil", want: false},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, nil)
		if err != nil {
			t.Fatalf("Eval(%q) error = %v", testCase.source, err)
		}

		if got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, want %v", testCase.source, got, testCase.want)
		}
	}
}

func TestOversizedLiteralOrderingIsExact(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		want   bool
	}{
		{source: "18446744073709551616 < 18446744073709551617", want: true},
		{source: "18446744073709551616 >= 18446744073709551617", want: false},
		{source: "18446744073709551617 > 18446744073709551616", want: true},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, nil)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestEqualityDistinguishesSharedBackingSliceViews(t *testing.T) {
	t.Parallel()

	leftBacking := []int{1, 2}
	rightBacking := []int{1, 3}
	left := []any{leftBacking[:1], leftBacking[:2]}
	right := []any{rightBacking[:1], rightBacking[:2]}

	got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
		testLeft: left, testRight: right,
	})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if got != false {
		t.Fatalf("Eval() = %#v, want false", got)
	}
}

func TestIntegerOverflowIsAnEvaluationError(t *testing.T) {
	t.Parallel()

	_, err := fasteval.Eval(context.Background(), "value + 1", map[string]any{testValue: int8(math.MaxInt8)})

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrOverflow {
		t.Fatalf("Eval() error = %v, want ErrOverflow", err)
	}
}

func TestMinimumIntegerRemainderByMinusOne(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "value % divisor", map[string]any{
		testValue: int8(math.MinInt8), "divisor": int8(-1),
	})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if got != int8(0) {
		t.Fatalf("Eval() = %#v (%T), want int8(0)", got, got)
	}
}

func TestCheckedIntegerOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		vars   map[string]any
		want   any
	}{
		{name: "negate", source: testNegateValueSource, vars: map[string]any{testValue: int8(7)}, want: int8(-7)},
		{name: "absolute value", source: testAbsValueSource, vars: map[string]any{testValue: int8(-7)}, want: int8(7)},
		{
			name: "remainder", source: "left % right",
			vars: map[string]any{testLeft: int8(-7), testRight: int8(3)}, want: int8(-1),
		},
		{
			name: "power", source: testPowerSource,
			vars: map[string]any{testLeft: int8(2), testRight: int8(6)}, want: int8(64),
		},
		{
			name: "left shift", source: testLeftShiftSource,
			vars: map[string]any{testLeft: int8(1), testRight: int8(6)}, want: int8(64),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)
			if err != nil {
				t.Fatalf("Eval() error = %v", err)
			}

			if got != testCase.want {
				t.Fatalf("Eval() = %#v (%T), want %#v (%T)", got, got, testCase.want, testCase.want)
			}
		})
	}
}

func TestCheckedIntegerOperationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		vars     map[string]any
		wantCode fasteval.ErrorCode
	}{
		{
			name: "negate overflow", source: testNegateValueSource,
			vars: map[string]any{testValue: int8(math.MinInt8)}, wantCode: fasteval.ErrOverflow,
		},
		{
			name: "absolute value overflow", source: testAbsValueSource,
			vars: map[string]any{testValue: int8(math.MinInt8)}, wantCode: fasteval.ErrOverflow,
		},
		{
			name: "remainder by zero", source: "left % right",
			vars:     map[string]any{testLeft: int8(1), testRight: int8(0)},
			wantCode: fasteval.ErrDivisionByZero,
		},
		{
			name: "power overflow", source: testPowerSource,
			vars: map[string]any{testLeft: int8(2), testRight: int8(7)}, wantCode: fasteval.ErrOverflow,
		},
		{
			name: "left shift overflow", source: testLeftShiftSource,
			vars: map[string]any{testLeft: int8(1), testRight: int8(7)}, wantCode: fasteval.ErrOverflow,
		},
		{
			name: "wide power count", source: testPowerSource,
			vars:     map[string]any{testLeft: uint64(2), testRight: uint64(1) << 32},
			wantCode: fasteval.ErrOverflow,
		},
		{
			name: "wide left shift count", source: testLeftShiftSource,
			vars:     map[string]any{testLeft: uint64(1), testRight: uint64(1) << 32},
			wantCode: fasteval.ErrOverflow,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := fasteval.Eval(context.Background(), testCase.source, testCase.vars)

			var evalErr *fasteval.EvalError
			if !errors.As(err, &evalErr) || evalErr.Code() != testCase.wantCode {
				t.Fatalf("Eval() error = %v, want %s", err, testCase.wantCode)
			}
		})
	}
}

func TestShiftCountsUseIndependentIntegerTypes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		left   any
		right  any
		want   any
	}{
		{source: testRightShiftSource, left: uint8(1), right: uint16(256), want: uint8(0)},
		{source: "left >> 256", left: uint8(1), right: nil, want: uint8(0)},
		{source: testRightShiftSource, left: int8(-1), right: uint64(1_000), want: int8(-1)},
		{source: "left << right", left: uint16(1), right: uint8(8), want: uint16(256)},
		{source: "bitshr(left, right)", left: uint8(1), right: uint16(256), want: uint8(0)},
		{source: "bitshr(left, 256)", left: uint8(1), right: nil, want: uint8(0)},
		{source: "bitshl(left, right)", left: uint16(1), right: uint8(8), want: uint16(256)},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, map[string]any{
			testLeft: testCase.left, testRight: testCase.right,
		})
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestIndependentShiftCountsKeepValidationAndOverflowErrors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source   string
		left     any
		right    any
		wantCode fasteval.ErrorCode
	}{
		{source: testRightShiftSource, left: uint8(1), right: int16(-1), wantCode: fasteval.ErrType},
		{source: "left >> -1", left: uint8(1), right: nil, wantCode: fasteval.ErrType},
		{source: "bitshr(left, right)", left: uint8(1), right: int16(-1), wantCode: fasteval.ErrType},
		{source: "bitshr(left, -1)", left: uint8(1), right: nil, wantCode: fasteval.ErrType},
		{source: "left << right", left: uint8(1), right: uint16(8), wantCode: fasteval.ErrOverflow},
		{source: "bitshl(left, right)", left: uint8(1), right: uint16(8), wantCode: fasteval.ErrOverflow},
	} {
		_, err := fasteval.Eval(context.Background(), testCase.source, map[string]any{
			testLeft: testCase.left, testRight: testCase.right,
		})

		var evalErr *fasteval.EvalError
		if !errors.As(err, &evalErr) || evalErr.Code() != testCase.wantCode {
			t.Fatalf("Eval(%q) error = %v, want %s", testCase.source, err, testCase.wantCode)
		}
	}
}

func TestUnknownVariableAndResolver(t *testing.T) {
	t.Parallel()

	expr, err := fasteval.Compile("known + lazy + nullable")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expr.Eval(
		context.Background(),
		map[string]any{"known": 1},
		fasteval.WithResolver(func(_ context.Context, name string) (any, bool, error) {
			switch name {
			case "lazy":
				return 2, true, nil
			case "nullable":
				return nil, true, nil
			default:
				return nil, false, nil
			}
		}),
	)
	if err == nil || got != nil {
		t.Fatalf("Eval() = %#v, %v; want nil plus type error", got, err)
	}

	_, err = fasteval.Eval(context.Background(), "missing", nil)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrUnknownVariable {
		t.Fatalf("Eval() error = %v, want ErrUnknownVariable", err)
	}
}

func TestRegisteredFunctionsAndContext(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("add", func(a, b int8) (int8, error) { return a + b, nil }),
		fasteval.WithFunction("contextValue", func(ctx context.Context) string {
			value, _ := ctx.Value(contextKey{}).(string)

			return value
		}),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expr, err := compiler.Compile("add(40, 2) == 42 && contextValue() == 'ok'")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), contextKey{}, "ok")

	got, err := expr.Eval(ctx, nil)
	if err != nil || got != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
	}
}

func TestFunctionPanicBecomesEvaluationError(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("explode", func() int {
		panic("boom")
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expr, err := compiler.Compile("explode()")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = expr.Eval(context.Background(), nil)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrPanic {
		t.Fatalf("Eval() error = %v, want ErrPanic", err)
	}
}

func TestTypedProgramRecursivelyConverts(t *testing.T) {
	t.Parallel()

	type output struct {
		Name   string `json:"name"`
		Scores []int8 `json:"scores"`
	}

	program, err := fasteval.CompileAs[output]("{'name': name, 'scores': [1, 2, 3]}")
	if err != nil {
		t.Fatalf("CompileAs() error = %v", err)
	}

	got, err := program.Eval(context.Background(), map[string]any{testName: testAda})
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if got.Name != testAda || len(got.Scores) != 3 || got.Scores[2] != 3 {
		t.Fatalf("Eval() = %#v", got)
	}
}

func TestEvalAsConvertsNamedStringMapKeysToStruct(t *testing.T) {
	t.Parallel()

	type namedString string

	type output struct {
		Name string `json:"name"`
	}

	got, err := fasteval.EvalAs[output](context.Background(), testValue, map[string]any{
		testValue: map[namedString]any{testName: testAda},
	})
	if err != nil || got.Name != testAda {
		t.Fatalf("EvalAs[output]() = %#v, %v; want name %q, nil", got, err, testAda)
	}
}

func TestEvalAsConvertsAssignableValuesToNamedTargets(t *testing.T) {
	t.Parallel()

	type namedSlice []int

	type namedMap map[string]int

	type namedStruct struct{ Value int }

	type namedPointer *int

	assertEvalAsValue(t, testValue, map[string]any{testValue: []int{1, 2}}, namedSlice{1, 2})
	assertEvalAsValue(
		t, testValue, map[string]any{testValue: map[string]int{testAnswer: 42}},
		namedMap{testAnswer: 42},
	)
	assertEvalAsValue(
		t, testValue, map[string]any{testValue: struct{ Value int }{Value: 42}},
		namedStruct{Value: 42},
	)

	integer := 42

	assertEvalAsValue(t, testValue, map[string]any{testValue: &integer}, namedPointer(&integer))
	assertEvalAsValue[any](t, testValue, map[string]any{testValue: []int{1, 2}}, []int{1, 2})
}

func assertEvalAsValue[T any](t *testing.T, source string, variables map[string]any, want T) {
	t.Helper()

	got, err := fasteval.EvalAs[T](context.Background(), source, variables)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("EvalAs(%q) = %#v, %v; want %#v, nil", source, got, err, want)
	}
}

func TestResolverCancellationWinsOverReturnedValue(t *testing.T) {
	t.Parallel()

	for _, found := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		option := fasteval.WithResolver(func(context.Context, string) (any, bool, error) {
			cancel()

			return 42, found, nil
		})

		_, err := fasteval.Eval(ctx, "resolved", nil, option)

		var evalErr *fasteval.EvalError
		if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled ||
			!errors.Is(err, context.Canceled) {
			t.Fatalf("found=%v: Eval() error = %v, want ErrCanceled", found, err)
		}
	}
}

func TestTypedProgramUsesResultTypeForLiterals(t *testing.T) {
	t.Parallel()

	integer, err := fasteval.EvalAs[uint64](context.Background(), "18446744073709551615", nil)
	if err != nil || integer != math.MaxUint64 {
		t.Fatalf("EvalAs[uint64]() = %d, %v; want %d, nil", integer, err, uint64(math.MaxUint64))
	}

	floating, err := fasteval.EvalAs[float32](context.Background(), "0x1.00000100000008p0", nil)

	wantFloat := math.Float32frombits(0x3f800001)
	if err != nil || floating != wantFloat {
		t.Fatalf("EvalAs[float32]() = %#v, %v; want %#v, nil", floating, err, wantFloat)
	}

	sequence, err := fasteval.EvalAs[[]uint64](
		context.Background(), "[18446744073709551615,]", nil,
	)
	if err != nil || !reflect.DeepEqual(sequence, []uint64{math.MaxUint64}) {
		t.Fatalf("EvalAs[[]uint64]() = %#v, %v; want max uint64, nil", sequence, err)
	}

	mapping, err := fasteval.EvalAs[map[uint64]float32](
		context.Background(), "{18446744073709551615: 0x1.00000100000008p0}", nil,
	)
	if err != nil || mapping[math.MaxUint64] != wantFloat {
		t.Fatalf("EvalAs[map[uint64]float32]() = %#v, %v; want exact key and value", mapping, err)
	}
}

func TestTypedInterfaceCompositesMaterializeLiterals(t *testing.T) {
	t.Parallel()

	sequence, err := fasteval.EvalAs[[]any](context.Background(), "[1,]", nil)
	if err != nil || !reflect.DeepEqual(sequence, []any{1}) {
		t.Fatalf("EvalAs[[]any]() = %#v, %v; want []any{1}, nil", sequence, err)
	}

	interfaceValue, err := fasteval.EvalAs[any](context.Background(), "[[1,],]", nil)

	want := []any{[]any{1}}
	if err != nil || !reflect.DeepEqual(interfaceValue, want) {
		t.Fatalf("EvalAs[any]() = %#v, %v; want %#v, nil", interfaceValue, err, want)
	}
}

func TestTypedNumericLiteralsConvertThroughPointers(t *testing.T) {
	t.Parallel()

	type namedPointer *int

	one := 1

	assertEvalAsValue(t, "1", nil, &one)

	two := 2
	twoPointer := &two
	assertEvalAsValue(t, "2", nil, &twoPointer)

	three := 3
	assertEvalAsValue(t, testValue, map[string]any{testValue: 3}, namedPointer(&three))

	type output struct {
		Value *uint64 `json:"value"`
	}

	maximum := uint64(math.MaxUint64)
	assertEvalAsValue(t, "{'value': 18446744073709551615}", nil, output{Value: &maximum})
}

func TestTypedNumericValuesConvertThroughPointers(t *testing.T) {
	t.Parallel()

	value := int32(7)

	converted, err := fasteval.EvalAs[*int64](
		context.Background(), testValue, map[string]any{testValue: &value},
	)
	if err != nil || converted == nil || *converted != 7 {
		t.Fatalf("EvalAs[*int64]() = %#v, %v; want pointer to 7", converted, err)
	}

	type output struct {
		Value *int64 `json:"value"`
	}

	nested, err := fasteval.EvalAs[output](
		context.Background(), "{'value': value}", map[string]any{testValue: &value},
	)
	if err != nil || nested.Value == nil || *nested.Value != 7 {
		t.Fatalf("EvalAs[output]() = %#v, %v; want pointer to 7", nested, err)
	}
}

func TestFloatingPointLiteralRemainder(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "5.5 % 2.0", nil)
	if err != nil || got != 1.5 {
		t.Fatalf("Eval() = %#v, %v; want 1.5, nil", got, err)
	}
}

func TestNonRealComplexLiteralsRejectRealConversion(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(
		context.Background(), "1 + 2i == value", map[string]any{testValue: float64(0)},
	)
	if err != nil || got != false {
		t.Fatalf("complex equality = %#v, %v; want false, nil", got, err)
	}

	for _, source := range []string{
		"float32(1 + 2i)",
		"float64(1 + 2i)",
		"1 + 2i + value",
	} {
		_, err := fasteval.Eval(
			context.Background(), source, map[string]any{testValue: float64(1)},
		)
		if err == nil {
			t.Fatalf("Eval(%q) error = nil, want conversion error", source)
		}
	}

	_, err = fasteval.EvalAs[float32](context.Background(), "1 + 2i", nil)
	if err == nil {
		t.Fatal("EvalAs[float32]() error = nil, want conversion error")
	}

	_, err = fasteval.EvalAs[float64](context.Background(), "1 + 2i", nil)
	if err == nil {
		t.Fatal("EvalAs[float64]() error = nil, want conversion error")
	}
}

func TestNumericLiteralsConvertThroughPointersToInterfaces(t *testing.T) {
	t.Parallel()

	var one any = 1

	assertEvalAsValue(t, "1", nil, &one)

	type output struct {
		Value *any `json:"value"`
	}

	assertEvalAsValue(t, "{'value': 1}", nil, output{Value: &one})

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("accept", func(value *any) bool {
		return value != nil && *value == 1
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("accept(1)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	accepted, err := expression.Eval(context.Background(), nil)
	if err != nil || accepted != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", accepted, err)
	}
}

func TestComplex64LiteralConversionAvoidsDoubleRounding(t *testing.T) {
	t.Parallel()

	type namedComplex64 complex64

	source := "0x1.00000100000008p0 + 0x1.00000100000008p0i"
	component := math.Float32frombits(0x3f800001)
	want := complex(component, component)

	assertEvalAsValue(t, source, nil, want)
	assertEvalAsValue(t, source, nil, namedComplex64(want))

	explicit, err := fasteval.Eval(context.Background(), "complex64("+source+")", nil)
	if err != nil || explicit != want {
		t.Fatalf("complex64() = %#v, %v; want %#v, nil", explicit, err, want)
	}

	arithmetic, err := fasteval.Eval(context.Background(), "value + ("+source+")", map[string]any{
		testValue: complex64(0),
	})
	if err != nil || arithmetic != want {
		t.Fatalf("complex64 arithmetic = %#v, %v; want %#v, nil", arithmetic, err, want)
	}

	equal, err := fasteval.Eval(context.Background(), "("+source+") == value", map[string]any{
		testValue: want,
	})
	if err != nil || equal != true {
		t.Fatalf("complex64 equality = %#v, %v; want true, nil", equal, err)
	}
}

func TestNestedNilCollectionsRemainDistinctFromEmptyCollections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  any
		right any
	}{
		{name: testSlice, left: []int(nil), right: []int{}},
		{name: testMap, left: map[string]int(nil), right: map[string]int{}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, nested := range []bool{false, true} {
				left, right := testCase.left, testCase.right
				if nested {
					left, right = []any{left}, []any{right}
				}

				got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
					testLeft: left, testRight: right,
				})
				if err != nil || got != false {
					t.Fatalf("nested=%v: Eval() = %#v, %v; want false, nil", nested, got, err)
				}
			}
		})
	}
}

func TestBulkMapToStructConversionRejectsNilMap(t *testing.T) {
	t.Parallel()

	type output struct {
		Value int
	}

	_, err := fasteval.EvalAs[[]output](context.Background(), testValues, map[string]any{
		testValues: []map[string]any{nil},
	})
	if err == nil {
		t.Fatal("EvalAs[[]output]() error = nil, want nil-map conversion error")
	}
}

func TestDiagnosticsAndAST(t *testing.T) {
	t.Parallel()

	_, err := fasteval.Compile("1 + )")

	var diagnostics *fasteval.DiagnosticsError

	if !errors.As(err, &diagnostics) || diagnostics.Len() == 0 {
		t.Fatalf("Compile() error = %v, want diagnostics", err)
	}

	if diagnostics.At(0).Span.Start < 0 || diagnostics.At(0).Code == "" {
		t.Fatalf("diagnostic = %#v", diagnostics.At(0))
	}

	expr, err := fasteval.Compile("user.score >= 10")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	root := concreteAST(t, expr)
	if root.Kind() != fasteval.NodeBinary || len(root.Children()) != 2 {
		t.Fatalf("AST root = %v with %d children", root.Kind(), len(root.Children()))
	}
}

func TestASTPreservesParsedConstantExpression(t *testing.T) {
	t.Parallel()

	expr, err := fasteval.Compile("1 + 2")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	root := concreteAST(t, expr)
	if root.Kind() != fasteval.NodeBinary || len(root.Children()) != 2 {
		t.Fatalf("AST root = %v with %d children", root.Kind(), len(root.Children()))
	}

	got, err := expr.Eval(context.Background(), nil)
	if err != nil || got != 3 {
		t.Fatalf("Eval() = %#v, %v", got, err)
	}
}

func TestASTChildrenReturnsCopy(t *testing.T) {
	t.Parallel()

	expression, err := fasteval.Compile("left + right")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	root := concreteAST(t, expression)
	children := root.Children()
	first := children[0]
	children[0] = nil

	if root.Children()[0] != first {
		t.Fatal("mutating Children() result changed the compiled AST")
	}
}

func concreteAST(t *testing.T, expression *fasteval.Expression) *fasteval.Node {
	t.Helper()

	return expression.AST()
}

func TestLargeIntegerLiteralFloatConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		value  float64
	}{
		{name: testInt64, source: "value == 9223372036854775807", value: float64(9223372036854775807)},
		{name: testUint64, source: "value == 18446744073709551615", value: float64(18446744073709551615)},
		{name: "arbitrary precision", source: "value == 18446744073709551616", value: float64(18446744073709551616)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), test.source, map[string]any{testValue: test.value})
			if err != nil || got != true {
				t.Fatalf("Eval() = %#v, %v", got, err)
			}
		})
	}
}
