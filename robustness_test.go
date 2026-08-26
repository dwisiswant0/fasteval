package fasteval_test

import (
	"context"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"go.dw1.io/fasteval"
)

var (
	errCallbackFailed             = errors.New("callback failed")
	errUnexpectedExpressionResult = errors.New("unexpected expression result")
)

func TestUnsafePointerNilHandling(t *testing.T) {
	t.Parallel()

	converted, err := fasteval.EvalAs[unsafe.Pointer](context.Background(), "nil", nil)
	if err != nil || converted != nil {
		t.Fatalf("EvalAs[unsafe.Pointer]() = %v, %v; want nil, nil", converted, err)
	}

	variables := map[string]any{
		testValue:    unsafe.Pointer(nil), //nolint:gosec // The test verifies intentional unsafe.Pointer handling.
		testFallback: "used",
	}
	for _, testCase := range []struct {
		source string
		want   any
	}{
		{source: "value?.field", want: nil},
		{source: "value ?? fallback", want: "used"},
	} {
		got, evalErr := fasteval.Eval(context.Background(), testCase.source, variables)
		if evalErr != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, evalErr, testCase.want)
		}
	}
}

type cancelAfterChecks struct {
	remaining atomic.Int64
}

func newCancelAfterChecks(limit int64) *cancelAfterChecks {
	ctx := &cancelAfterChecks{remaining: atomic.Int64{}}
	ctx.remaining.Store(limit)

	return ctx
}

func (*cancelAfterChecks) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*cancelAfterChecks) Done() <-chan struct{} {
	return nil
}

func (*cancelAfterChecks) Value(any) any {
	return nil
}

func (c *cancelAfterChecks) Err() error {
	if c.remaining.Add(-1) <= 0 {
		return context.Canceled
	}

	return nil
}

func TestOperatorPrecedenceAndAssociativity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "2 ** 3 ** 2", want: 64},
		{source: "-2 ** 2", want: 4},
		{source: "1 | 2 & 4", want: 0},
		{source: "1 << 2 + 1", want: 8},
		{source: "true && false || true", want: true},
		{source: "nil ?? false ? 1 : 2", want: 2},
		{source: "true ? 7", want: 7},
		{source: "false ? 7", want: nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.source, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), testCase.source, nil)
			if err != nil || !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Eval() = %#v, %v; want %#v, nil", got, err, testCase.want)
			}
		})
	}
}

func TestLongBinaryChainPreservesEveryOperand(t *testing.T) {
	t.Parallel()

	const termCount = 40

	source := strings.Repeat(testValue+" + ", termCount-1) + testValue

	got, err := fasteval.Eval(context.Background(), source, map[string]any{testValue: 1})
	if err != nil || got != termCount {
		t.Fatalf("Eval() = %#v, %v; want %d, nil", got, err, termCount)
	}
}

func TestInvalidExpressionsReturnDiagnostics(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"", "1 +", "(", "{1}", "[1, 2", "'open", "name =~ '['", "[\xff]", "name\\\xff"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			_, err := fasteval.Compile(source)

			var diagnostics *fasteval.DiagnosticsError

			if !errors.As(err, &diagnostics) || diagnostics.Len() == 0 {
				t.Fatalf("Compile(%q) error = %v, want diagnostics", source, err)
			}
		})
	}
}

func TestConstantSizeDiagnosticsPreserveIndependentSpans(t *testing.T) {
	t.Parallel()

	const source = "(1 << 1048576) + (1 << 1048576)"

	_, err := fasteval.Compile(source)

	var diagnostics *fasteval.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("Compile() error = %v, want DiagnosticsError", err)
	}

	want := []fasteval.Span{
		{Start: 0, End: 14, Line: 1, Column: 1},
		{Start: 17, End: 31, Line: 1, Column: 18},
	}
	if diagnostics.Len() != len(want) {
		t.Fatalf("Compile() diagnostics = %#v, want %d diagnostics", diagnostics.All(), len(want))
	}

	for index := range want {
		if got := diagnostics.At(index).Span; got != want[index] {
			t.Fatalf("diagnostic %d span = %#v, want %#v", index, got, want[index])
		}
	}
}

func TestExpressionNestingLimitReturnsDiagnostics(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"parentheses": strings.Repeat("(", 1_000) + "0" + strings.Repeat(")", 1_000),
		"lists":       strings.Repeat("[", 1_000) + "0" + strings.Repeat(",]", 1_000),
	} {
		_, err := fasteval.Compile(source)
		if err != nil {
			t.Fatalf("Compile() at supported %s nesting depth error = %v", name, err)
		}
	}

	for name, source := range map[string]string{
		"parentheses": strings.Repeat("(", 2_000) + "0" + strings.Repeat(")", 2_000),
		"lists":       strings.Repeat("[", 2_000) + "0" + strings.Repeat(",]", 2_000),
		"prefix":      strings.Repeat("-", 2_000) + "1",
		"infix":       strings.Repeat("value + ", 2_000) + testValue,
		"postfix":     testValue + strings.Repeat(".field", 2_000),
	} {
		_, err := fasteval.Compile(source)

		var diagnostics *fasteval.DiagnosticsError

		if !errors.As(err, &diagnostics) || diagnostics.Len() == 0 {
			t.Fatalf("Compile() for excessive %s nesting error = %v, want diagnostics", name, err)
		}

		diagnostic := diagnostics.At(0)
		if diagnostic.Code != fasteval.ErrSyntax || !strings.Contains(diagnostic.Message, "maximum depth") {
			t.Fatalf("Compile() for excessive %s nesting diagnostic = %#v", name, diagnostic)
		}
	}
}

func TestZeroValueCompiledObjectsReturnErrors(t *testing.T) {
	t.Parallel()

	_, err := new(fasteval.Expression).Eval(context.Background(), nil)
	if err == nil {
		t.Fatal("zero-value Expression.Eval() error = nil")
	}

	_, err = new(fasteval.Template).Render(context.Background(), nil)
	if err == nil {
		t.Fatal("zero-value Template.Render() error = nil")
	}
}

func TestZeroValueCompilerRejectsExpressionAndTemplateCompilation(t *testing.T) {
	t.Parallel()

	compiler := new(fasteval.Compiler)

	_, err := compiler.Compile("1")
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("Compiler.Compile() error = %v, want not-initialized detail", err)
	}

	_, err = compiler.CompileTemplate("text")
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("Compiler.CompileTemplate() error = %v, want not-initialized detail", err)
	}
}

func TestVariadicFunctionAndResolverPanicBoundary(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("total", func(prefix int, values ...int) int {
		result := prefix
		for _, value := range values {
			result += value
		}

		return result
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("total(1, 2, 3, 4)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), nil)
	if err != nil || got != 10 {
		t.Fatalf("Eval() = %#v, %v; want 10, nil", got, err)
	}

	expression, err = compiler.Compile("lazy")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	resolver := fasteval.WithResolver(func(context.Context, string) (any, bool, error) {
		panic("resolver panic")
	})

	_, err = expression.Eval(context.Background(), nil, resolver)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrPanic {
		t.Fatalf("Eval() error = %v, want ErrPanic", err)
	}
}

func TestCancellationStopsEvaluation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fasteval.Eval(ctx, "1 + 1", nil)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("Eval() error = %v, want canceled EvalError", err)
	}
}

func TestCancellationStopsBuiltinLoops(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("cancelAfterFirst", func(value int) int {
		cancel()

		return value
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("sortBy([3, 2, 1], cancelAfterFirst)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = expression.Eval(ctx, nil)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("Eval() error = %v, want canceled EvalError", err)
	}
}

func TestCancellationPreventsLaterCallbacks(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var sideEffects atomic.Int64

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("cancelNow", func() int {
			cancel()

			return 1
		}),
		fasteval.WithFunction("sideEffect", func() int {
			sideEffects.Add(1)

			return 1
		}),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("cancelNow() + sideEffect()")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = expression.Eval(ctx, nil)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled {
		t.Fatalf("Eval() error = %v, want ErrCanceled", err)
	}

	if got := sideEffects.Load(); got != 0 {
		t.Fatalf("side-effect calls = %d, want 0", got)
	}
}

func TestCancellationStopsRecursiveResultConversion(t *testing.T) {
	t.Parallel()

	program, err := fasteval.CompileAs[[]int64](testValues)
	if err != nil {
		t.Fatalf("CompileAs() error = %v", err)
	}

	values := make([]int32, 100)
	_, err = program.Eval(newCancelAfterChecks(10), map[string]any{testValues: values})

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled {
		t.Fatalf("Program.Eval() error = %v, want ErrCanceled", err)
	}

	wantSpan := fasteval.Span{Start: 0, End: len(testValues), Line: 1, Column: 1}
	if evalErr.Span() != wantSpan {
		t.Fatalf("Program.Eval() span = %+v, want %+v", evalErr.Span(), wantSpan)
	}
}

func TestCancellationDuringFunctionArgumentConversionKeepsCallSpan(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("consume", func([]int64) int {
		return 0
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("consume(values)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = expression.Eval(newCancelAfterChecks(10), map[string]any{testValues: make([]int32, 100)})

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled {
		t.Fatalf("Eval() error = %v, want ErrCanceled", err)
	}

	wantSpan := fasteval.Span{Start: 0, End: len("consume(values)"), Line: 1, Column: 1}
	if evalErr.Span() != wantSpan {
		t.Fatalf("Eval() span = %+v, want %+v", evalErr.Span(), wantSpan)
	}
}

func TestKeysChecksContextDuringMapIteration(t *testing.T) {
	t.Parallel()

	values := make(map[int]int, 100)
	for index := range 100 {
		values[index] = index
	}

	_, err := fasteval.Eval(
		newCancelAfterChecks(10), "keys(values)", map[string]any{testValues: values},
	)

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled {
		t.Fatalf("Eval() error = %v, want ErrCanceled", err)
	}
}

func TestComplexInfinityEqualityUsesBothComponents(t *testing.T) {
	t.Parallel()

	left := complex64(complex(math.Inf(1), 0))
	right := complex(0, math.Inf(1))

	got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
		testLeft: left, testRight: right,
	})
	if err != nil || got != false {
		t.Fatalf("different complex infinities = %#v, %v; want false, nil", got, err)
	}

	got, err = fasteval.Eval(context.Background(), "left == same", map[string]any{
		testLeft: left, testSame: complex128(left),
	})
	if err != nil || got != true {
		t.Fatalf("equal complex infinities = %#v, %v; want true, nil", got, err)
	}
}

func TestCompiledExpressionIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	expression, err := fasteval.Compile("left + right == 42")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	var wait sync.WaitGroup

	errorsFound := make(chan error, 32)

	for range 32 {
		wait.Go(func() {
			value, evalErr := expression.Eval(context.Background(), map[string]any{testLeft: 40, testRight: 2})
			if evalErr != nil {
				errorsFound <- evalErr

				return
			}

			if value != true {
				errorsFound <- errUnexpectedExpressionResult
			}
		})
	}

	wait.Wait()
	close(errorsFound)

	for err := range errorsFound {
		t.Error(err)
	}
}

func TestCyclicCollectionsFailSafely(t *testing.T) {
	t.Parallel()

	cycle := []any{nil}

	cycle[0] = cycle

	for _, source := range []string{"flatten(value)", testToJSONValueSource} {
		_, err := fasteval.Eval(context.Background(), source, map[string]any{testValue: cycle})
		if err == nil {
			t.Fatalf("Eval(%q) error = nil, want cycle error", source)
		}
	}

	other := []any{nil}
	other[0] = other

	got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
		testLeft: cycle, testRight: other,
	})
	if err != nil || got != true {
		t.Fatalf("cyclic equality = %#v, %v; want true, nil", got, err)
	}
}

func TestEqualityLimitsAcyclicCollectionDepth(t *testing.T) {
	t.Parallel()

	left := nestedSingleElementSlices(256, 1)
	right := nestedSingleElementSlices(256, 1)

	got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
		testLeft: left, testRight: right,
	})
	if err != nil || got != true {
		t.Fatalf("supported-depth equality = %#v, %v; want true, nil", got, err)
	}

	left = nestedSingleElementSlices(1_100, 1)
	right = nestedSingleElementSlices(1_100, 1)

	_, err = fasteval.Eval(context.Background(), "left == right", map[string]any{
		testLeft: left, testRight: right,
	})
	if err == nil || !strings.Contains(err.Error(), "maximum value depth") {
		t.Fatalf("excessive-depth equality error = %v, want maximum-value-depth detail", err)
	}
}

func TestTypedConversionLimitsAcyclicCollectionDepth(t *testing.T) {
	t.Parallel()

	type recursiveSlice []recursiveSlice

	_, err := fasteval.EvalAs[recursiveSlice](context.Background(), testValue, map[string]any{
		testValue: nestedSingleElementSlices(256, []any{}),
	})
	if err != nil {
		t.Fatalf("supported-depth EvalAs[recursiveSlice]() error = %v", err)
	}

	_, err = fasteval.EvalAs[recursiveSlice](context.Background(), testValue, map[string]any{
		testValue: nestedSingleElementSlices(1_100, []any{}),
	})
	if err == nil || !strings.Contains(err.Error(), "maximum value depth") {
		t.Fatalf("excessive-depth EvalAs[recursiveSlice]() error = %v, want maximum-value-depth detail", err)
	}
}

func TestTypedSelectionDoesNotScanUnrelatedCallerCollection(t *testing.T) {
	t.Parallel()

	value := nestedSingleElementSlices(1_100, 1)

	got, err := fasteval.EvalAs[any](
		context.Background(), "len([1,]) == 1 ? value : nil", map[string]any{testValue: value},
	)
	if err != nil {
		t.Fatalf("EvalAs[any]() error = %v", err)
	}

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(value).Pointer() {
		t.Fatal("EvalAs[any]() did not return the selected caller collection")
	}
}

func TestFunctionResultsRemainBoundaryValues(t *testing.T) {
	t.Parallel()

	returned := nestedSingleElementSlices(1_100, 1)

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("deep", func(any) any {
		return returned
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("deep([1,])")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), nil)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(returned).Pointer() {
		t.Fatal("Eval() did not return the registered function's boundary value")
	}
}

func TestJSONLimitsAcyclicCollectionDepth(t *testing.T) {
	t.Parallel()

	for _, value := range []any{
		nestedSingleElementSlices(256, 1),
		nestedSingleEntryMaps(256, 1),
	} {
		_, err := fasteval.Eval(context.Background(), "toJSON(value)", map[string]any{testValue: value})
		if err != nil {
			t.Fatalf("supported-depth toJSON() error = %v", err)
		}
	}

	for _, value := range []any{
		nestedSingleElementSlices(1_100, 1),
		nestedSingleEntryMaps(1_100, 1),
	} {
		_, err := fasteval.Eval(context.Background(), "toJSON(value)", map[string]any{testValue: value})
		if err == nil || !strings.Contains(err.Error(), "maximum value depth") {
			t.Fatalf("excessive-depth toJSON() error = %v, want maximum-value-depth detail", err)
		}
	}
}

func nestedSingleElementSlices(depth int, leaf any) any {
	value := leaf
	for range depth {
		value = []any{value}
	}

	return value
}

func nestedSingleEntryMaps(depth int, leaf any) any {
	value := leaf
	for range depth {
		value = map[string]any{testValue: value}
	}

	return value
}

func TestFromPairsPreservesInnerSequenceErrors(t *testing.T) {
	t.Parallel()

	var cycle any

	cycle = &cycle

	_, err := fasteval.Eval(
		context.Background(), "fromPairs(values)", map[string]any{testValues: []any{cycle}},
	)
	if err == nil || !strings.Contains(err.Error(), "pointer cycle") {
		t.Fatalf("Eval() error = %v, want pointer-cycle detail", err)
	}
}

func TestFromPairsCanonicalizesTypedNilKeys(t *testing.T) {
	t.Parallel()

	var key *int

	variables := map[string]any{testPairs: []any{[]any{nil, 1}, []any{key, 2}}}
	for _, source := range []string{"fromPairs(pairs)", "len(fromPairs(pairs))"} {
		_, err := fasteval.Eval(context.Background(), source, variables)
		if err == nil {
			t.Fatalf("Eval(%q) error = nil, want duplicate logical-nil key error", source)
		}
	}
}

func TestRecursiveTypedConversionRejectsSourceCycle(t *testing.T) {
	t.Parallel()

	type recursiveValue struct {
		Next *recursiveValue `json:"next"`
	}

	value := map[string]any{}
	value["next"] = value

	_, err := fasteval.EvalAs[recursiveValue](context.Background(), testValue, map[string]any{
		testValue: value,
	})
	if err == nil {
		t.Fatal("EvalAs() error = nil, want conversion cycle error")
	}
}

func TestAcyclicSliceViewsDoNotReportCycles(t *testing.T) {
	t.Parallel()

	value := []any{1, nil}
	value[1] = value[:1]

	flattened, err := fasteval.Eval(context.Background(), "flatten(value)", map[string]any{
		testValue: value,
	})
	if err != nil || !reflect.DeepEqual(flattened, []any{1, 1}) {
		t.Fatalf("flatten() = %#v, %v; want [1 1], nil", flattened, err)
	}

	encoded, err := fasteval.Eval(context.Background(), "toJSON(value)", map[string]any{
		testValue: value,
	})
	if err != nil || encoded != `[1,[1]]` {
		t.Fatalf("toJSON() = %#v, %v; want [1,[1]], nil", encoded, err)
	}

	type recursiveSlice []recursiveSlice

	source := []any{nil}
	source[0] = source[:0]

	converted, err := fasteval.EvalAs[recursiveSlice](context.Background(), testValue, map[string]any{
		testValue: source,
	})
	if err != nil || len(converted) != 1 || converted[0] == nil || len(converted[0]) != 0 {
		t.Fatalf("EvalAs[recursiveSlice]() = %#v, %v; want one empty nested slice", converted, err)
	}
}

func TestEvalAsSupportsNilInterfaceTargets(t *testing.T) {
	t.Parallel()

	value, err := fasteval.EvalAs[any](context.Background(), "nil", nil)
	if err != nil || value != nil {
		t.Fatalf("EvalAs[any]() = %#v, %v; want nil, nil", value, err)
	}

	type stringer interface {
		String() string
	}

	stringValue, err := fasteval.EvalAs[stringer](context.Background(), "nil", nil)
	if err != nil || stringValue != nil {
		t.Fatalf("EvalAs[stringer]() = %#v, %v; want nil, nil", stringValue, err)
	}
}

func TestOptionalAccessProducesPlainNilInsideCompositeResults(t *testing.T) {
	t.Parallel()

	variables := map[string]any{testValue: nil}

	got, err := fasteval.Eval(context.Background(), "[value?.missing,]", variables)
	if err != nil || !reflect.DeepEqual(got, []any{nil}) {
		t.Fatalf("Eval() = %#v, %v; want []any{nil}, nil", got, err)
	}

	typed, err := fasteval.EvalAs[[]any](context.Background(), "[value?.missing,]", variables)
	if err != nil || !reflect.DeepEqual(typed, []any{nil}) {
		t.Fatalf("EvalAs[[]any]() = %#v, %v; want []any{nil}, nil", typed, err)
	}
}

func TestOptionalIndexSuppressesIndirectNilReceiver(t *testing.T) {
	t.Parallel()

	var values []int

	got, err := fasteval.Eval(context.Background(), "value?[undefined]", map[string]any{
		testValue: &values,
	})
	if err != nil || got != nil {
		t.Fatalf("Eval() = %#v, %v; want nil, nil", got, err)
	}
}

func TestExcessiveLiteralOperationsReturnErrors(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"1 << 18446744073709551615",
		"2 ** 18446744073709551615",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			_, err := fasteval.Compile(source)
			if err == nil {
				t.Fatalf("Compile(%q) error = nil, want constant-size error", source)
			}
		})
	}
}

func TestNonGrowingLiteralOperationsIgnoreGrowthLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "1 ** 1048577", want: 1},
		{source: "1 >> 1048577", want: 0},
		{source: "1 >> 18446744073709551615", want: 0},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, nil)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestLiteralArgumentsMaterializeForInterfaceParameters(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("identity", func(value any) any { return value }),
		fasteval.WithFunction("lastArg", func(values ...any) any { return values[len(values)-1] }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	for _, source := range []string{"identity(1)", "lastArg(0, 1)"} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), nil)
		if evalErr != nil || got != 1 {
			t.Fatalf("Eval(%q) = %#v, %v; want 1, nil", source, got, evalErr)
		}
	}
}

func TestLiteralCollectionsMaterializeForInterfaceParameters(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("listLen", func(value any) int {
			values, ok := value.([]any)
			if !ok {
				return -1
			}

			return len(values)
		}),
		fasteval.WithFunction("mapLen", func(value any) int {
			values, ok := value.(map[any]any)
			if !ok {
				return -1
			}

			return len(values)
		}),
		fasteval.WithFunction("lastListLen", func(values ...any) int {
			last, ok := values[len(values)-1].([]any)
			if !ok {
				return -1
			}

			return len(last)
		}),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	for _, source := range []string{
		"listLen([1, 2]) == 2",
		"mapLen({'key': 1}) == 1",
		"lastListLen(0, [1, 2]) == 2",
	} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), nil)
		if evalErr != nil || got != true {
			t.Fatalf("Eval(%q) = %#v, %v; want true, nil", source, got, evalErr)
		}
	}
}

func TestCompiledConstantCollectionsAreFresh(t *testing.T) {
	t.Parallel()

	expression, err := fasteval.Compile("[1, {'answer': 42}]")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	first, firstMap := evaluatedConstantCollection(t, expression)
	first[0] = 99
	firstMap[testAnswer] = 99

	second, secondMap := evaluatedConstantCollection(t, expression)
	if second[0] != 1 || secondMap[testAnswer] != 42 {
		t.Fatalf("second Eval() = %#v, want fresh collection", second)
	}

	mutateConstantCollectionConcurrently(t, expression)
}

func evaluatedConstantCollection(
	t *testing.T,
	expression *fasteval.Expression,
) ([]any, map[any]any) {
	t.Helper()

	value, err := expression.Eval(context.Background(), nil)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	items, itemsOK := value.([]any)
	if !itemsOK {
		t.Fatalf("Eval() result has type %T, want []any", value)
	}

	itemsMap, ok := items[1].(map[any]any)
	if !ok {
		t.Fatalf("Eval() nested result has type %T, want map[any]any", items[1])
	}

	return items, itemsMap
}

func mutateConstantCollectionConcurrently(t *testing.T, expression *fasteval.Expression) {
	t.Helper()

	var wait sync.WaitGroup

	errorsFound := make(chan error, 16)

	for worker := range 16 {
		wait.Go(func() {
			value, evalErr := expression.Eval(context.Background(), nil)
			if evalErr != nil {
				errorsFound <- evalErr

				return
			}

			items, itemsOK := value.([]any)
			if !itemsOK {
				errorsFound <- errUnexpectedExpressionResult

				return
			}

			items[0] = worker

			itemsMap, ok := items[1].(map[any]any)
			if !ok {
				errorsFound <- errUnexpectedExpressionResult

				return
			}

			itemsMap[testAnswer] = worker
		})
	}

	wait.Wait()
	close(errorsFound)

	for evalErr := range errorsFound {
		t.Error(evalErr)
	}
}

func TestInterfaceBackedMapKeysAreValidatedBeforeUse(t *testing.T) {
	t.Parallel()

	type interfaceKey struct {
		Value any
	}

	key := interfaceKey{Value: []int{1}}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("badKey", func(any) any { return key }))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	tests := []struct {
		name   string
		source string
		vars   map[string]any
	}{
		{name: "map literal", source: "{key: 1}", vars: map[string]any{testKey: key}},
		{name: "map index", source: "values[key]", vars: map[string]any{testValues: map[any]any{"x": 1}, testKey: key}},
		{name: "from pairs", source: "fromPairs([[key, 1]])", vars: map[string]any{testKey: key}},
		{name: "group by", source: "groupBy([1], badKey)", vars: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			expression, compileErr := compiler.Compile(testCase.source)
			if compileErr != nil {
				t.Fatalf("Compile() error = %v", compileErr)
			}

			_, evalErr := expression.Eval(context.Background(), testCase.vars)
			if evalErr == nil {
				t.Fatal("Eval() error = nil, want non-comparable key error")
			}
		})
	}
}

func TestTypedStructConversionRejectsDuplicateFieldAliases(t *testing.T) {
	t.Parallel()

	type output struct {
		Name string `json:"name"`
	}

	program, err := fasteval.CompileAs[output]("{'Name': 'Ada', 'name': 'Grace'}")
	if err != nil {
		t.Fatalf("CompileAs() error = %v", err)
	}

	_, err = program.Eval(context.Background(), nil)
	if err == nil {
		t.Fatal("Eval() error = nil, want duplicate-field error")
	}
}

func TestTemplateSyntaxFormattingAndShortWriteErrors(t *testing.T) {
	t.Parallel()

	assertInvalidTemplateSyntax(t)
	assertInvalidTemplateDelimiters(t)
	assertTemplateFormatError(t)
	assertTemplateWriteErrors(t)
}

func assertInvalidTemplateSyntax(t *testing.T) {
	t.Helper()

	for _, source := range []string{"{{ 1", "{{ 1 {{ 2 }}"} {
		_, err := fasteval.CompileTemplate(source)
		if err == nil {
			t.Fatalf("CompileTemplate(%q) error = nil", source)
		}
	}
}

func assertInvalidTemplateDelimiters(t *testing.T) {
	t.Helper()

	_, err := fasteval.CompileTemplate("x", fasteval.WithDelimiters("", "}}"))
	if err == nil {
		t.Fatal("empty delimiter error = nil")
	}

	_, err = fasteval.CompileTemplate("x", fasteval.WithDelimiters("%%", "%%"))
	if err == nil {
		t.Fatal("identical delimiter error = nil")
	}
}

func assertTemplateFormatError(t *testing.T) {
	t.Helper()

	template, err := fasteval.CompileTemplate("prefix {{[1, 2]}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := template.Render(context.Background(), nil)
	if err == nil || got != "prefix " {
		t.Fatalf("Render() = %q, %v; want partial prefix and format error", got, err)
	}
}

func assertTemplateWriteErrors(t *testing.T) {
	t.Helper()

	template, err := fasteval.CompileTemplate("text")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	_, err = template.Execute(context.Background(), zeroWriter{}, nil)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Execute() error = %v, want io.ErrShortWrite", err)
	}

	for _, count := range []int{-1, len("text") + 1} {
		written, invalidErr := template.Execute(
			context.Background(), invalidCountWriter{count: count}, nil,
		)
		if invalidErr == nil || !strings.Contains(invalidErr.Error(), "invalid write count") {
			t.Fatalf("Execute() with count %d error = %v, want invalid-write-count detail", count, invalidErr)
		}

		if written != 0 {
			t.Fatalf("Execute() with count %d wrote %d bytes, want 0", count, written)
		}
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type invalidCountWriter struct{ count int }

func (w invalidCountWriter) Write([]byte) (int, error) { return w.count, nil }
