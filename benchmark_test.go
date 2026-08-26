package fasteval_test

import (
	"context"
	"io"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"go.dw1.io/fasteval"
)

type benchmarkConversionStruct struct {
	A int
	B int
	C int
	D int
	E int
	F int
	G int
	H int
}

func BenchmarkCompile(b *testing.B) {
	benchmarkCompile(b, "(requests_made * requests_succeeded / 100) >= 90")
}

func BenchmarkCompileWorkloads(b *testing.B) {
	longIdentifier := "this_is_a_deliberately_long_identifier_that_models_" +
		"namespaced_generated_or_machine_produced_variable_names_with_many_segments"
	booleanChain := strings.Repeat("value && ", 31) + testValue
	largeList := "[" + strings.Repeat("value, ", 31) + "value]"
	largeMap := "{'a': value, 'b': value, 'c': value, 'd': value, " +
		"'e': value, 'f': value, 'g': value, 'h': value, " +
		"'i': value, 'j': value, 'k': value, 'l': value, " +
		"'m': value, 'n': value, 'o': value, 'p': value}"

	tests := []struct {
		name   string
		source string
	}{
		{name: "tiny_identifier", source: testValue},
		{name: "small_arithmetic", source: "first + second + third"},
		{name: "constant_expression", source: "((1 + 2) * (3 + 4) - 5) ** 2"},
		{name: "medium_arithmetic", source: "(requests_made * requests_succeeded / 100) >= 90"},
		{name: "long_identifier", source: longIdentifier},
		{
			name:   "access_conditional",
			source: "session?.user.roles[0] ?? (score >= threshold ? 'member' : 'guest')",
		},
		{name: "boolean_chain_32", source: booleanChain},
		{name: "list_32", source: largeList},
		{name: "map_16", source: largeMap},
		{name: "regex_literal", source: "name =~ '^(fast|safe)[a-z0-9_-]{3,32}$'"},
	}

	for _, test := range tests {
		_, err := fasteval.Compile(test.source)
		if err != nil {
			b.Fatalf("compile %s workload: %v", test.name, err)
		}

		b.Run(test.name, func(b *testing.B) {
			benchmarkCompile(b, test.source)
		})
	}
}

func BenchmarkCompileNestedLists(b *testing.B) {
	for _, depth := range []int{100, 1_000} {
		source := strings.Repeat("[", depth) + "0" + strings.Repeat(",]", depth)
		b.Run(strconv.Itoa(depth), func(b *testing.B) {
			benchmarkCompile(b, source)
		})
	}
}

func benchmarkCompile(b *testing.B, source string) {
	b.Helper()
	b.ReportAllocs()

	var result *fasteval.Expression

	for b.Loop() {
		expression, err := fasteval.Compile(source)
		if err != nil {
			b.Fatal(err)
		}

		result = expression
	}

	runtime.KeepAlive(result)
}

func BenchmarkASTChildren(b *testing.B) {
	expression := mustCompileBenchmark(b, "left + right")
	root := expression.AST()

	b.ReportAllocs()

	var children any

	for b.Loop() {
		children = root.Children()
	}

	runtime.KeepAlive(children)
}

func BenchmarkEvalLiteral(b *testing.B) {
	expression := mustCompileBenchmark(b, "1")
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalAsSimpleDecimalFloat32(b *testing.B) {
	program, err := fasteval.CompileAs[float32]("0.9")
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ReportAllocs()

	var result float32

	for b.Loop() {
		value, evalErr := program.Eval(ctx, nil)
		if evalErr != nil {
			b.Fatal(evalErr)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalAsSimpleRealComplex128(b *testing.B) {
	program, err := fasteval.CompileAs[complex128]("1.5")
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ReportAllocs()

	var result complex128

	for b.Loop() {
		value, evalErr := program.Eval(ctx, nil)
		if evalErr != nil {
			b.Fatal(evalErr)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalSimpleDecimalAgainstInteger(b *testing.B) {
	expression := mustCompileBenchmark(b, "value >= 100.0")
	ctx := context.Background()
	variables := map[string]any{testValue: 100}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalLookup(b *testing.B) {
	expression := mustCompileBenchmark(b, testValue)
	ctx := context.Background()
	variables := map[string]any{testValue: 42}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalPointerAccess(b *testing.B) {
	expression := mustCompileBenchmark(b, "value.Field")
	variables := map[string]any{testValue: &struct{ Field int }{Field: 42}}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalComparison(b *testing.B) {
	expression := mustCompileBenchmark(b, "left > right")
	ctx := context.Background()
	variables := map[string]any{testLeft: 42, testRight: 10}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalBoolean(b *testing.B) {
	expression := mustCompileBenchmark(b, "left && right")
	ctx := context.Background()
	variables := map[string]any{testLeft: true, testRight: true}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalArithmetic(b *testing.B) {
	expression := mustCompileBenchmark(b, "(requests_made * requests_succeeded / 100) >= 90")
	ctx := context.Background()
	variables := map[string]any{"requests_made": 99.0, "requests_succeeded": 90.0}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkTemplateSubstitution(b *testing.B) {
	template, err := fasteval.CompileTemplate("Hello {{name}}!")
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	variables := map[string]any{testName: testAda}

	b.ReportAllocs()

	for b.Loop() {
		_, err := template.Execute(ctx, io.Discard, variables)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalUniqUniqueIntegers(b *testing.B) {
	expression := mustCompileBenchmark(b, "uniq(values)")

	values := make([]int, 1_000)
	for index := range values {
		values[index] = index
	}

	ctx := context.Background()
	variables := map[string]any{testValues: values}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalMissingMapMembership(b *testing.B) {
	expression := mustCompileBenchmark(b, "needle in values")

	values := make(map[string]int, 10_000)
	for index := range 10_000 {
		values[strconv.Itoa(index)] = index
	}

	ctx := context.Background()
	variables := map[string]any{testNeedle: testMissing, testValues: values}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalMissingInterfaceMapIndex(b *testing.B) {
	expression := mustCompileBenchmark(b, "values[needle]")

	values := make(map[any]int, 10_000)
	for index := range 10_000 {
		values[index] = index
	}

	ctx := context.Background()
	variables := map[string]any{testNeedle: int64(-1), testValues: values}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = expression.Eval(ctx, variables)
	}
}

func BenchmarkKeysLargeMap(b *testing.B) {
	expression := mustCompileBenchmark(b, "keys(values)")

	values := make(map[int]int, 100_000)
	for index := range 100_000 {
		values[index] = index
	}

	ctx := context.Background()
	variables := map[string]any{testValues: values}

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkCompileTemplateManySegments(b *testing.B) {
	source := strings.Repeat("x{{value}}", 1_000)

	b.ReportAllocs()

	var result *fasteval.Template

	for b.Loop() {
		template, err := fasteval.CompileTemplate(source)
		if err != nil {
			b.Fatal(err)
		}

		result = template
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalFirstLargeSequence(b *testing.B) {
	expression := mustCompileBenchmark(b, "first(values)")
	values := make([]int, 100_000)
	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalAsIntegerSlice(b *testing.B) {
	program, err := fasteval.CompileAs[[]int64](testValues)
	if err != nil {
		b.Fatal(err)
	}

	values := make([]int32, 100_000)
	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result []int64

	for b.Loop() {
		value, evalErr := program.Eval(ctx, variables)
		if evalErr != nil {
			b.Fatal(evalErr)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalAsStructSlice(b *testing.B) {
	program, err := fasteval.CompileAs[[]benchmarkConversionStruct](testValues)
	if err != nil {
		b.Fatal(err)
	}

	entry := map[string]any{
		"A": 1, "B": 2, "C": 3, "D": 4,
		"E": 5, "F": 6, "G": 7, "H": 8,
	}

	values := make([]map[string]any, 10_000)

	for index := range values {
		values[index] = entry
	}

	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result []benchmarkConversionStruct

	for b.Loop() {
		value, evalErr := program.Eval(ctx, variables)
		if evalErr != nil {
			b.Fatal(evalErr)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalAsInterfaceSlice(b *testing.B) {
	program, err := fasteval.CompileAs[[]any](testValues)
	if err != nil {
		b.Fatal(err)
	}

	values := make([]any, 100_000)
	for index := range values {
		values[index] = index
	}

	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result []any

	for b.Loop() {
		value, evalErr := program.Eval(ctx, variables)
		if evalErr != nil {
			b.Fatal(evalErr)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalEqualIntegerCollections(b *testing.B) {
	leftMap := make(map[int]int, 10_000)
	rightMap := make(map[int]int, 10_000)

	benchmarks := []struct {
		name  string
		left  any
		right any
	}{
		{name: testSlice, left: make([]int, 100_000), right: make([]int, 100_000)},
		{name: testMap, left: leftMap, right: rightMap},
	}

	for index := range 10_000 {
		leftMap[index] = index
		rightMap[index] = index
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			expression := mustCompileBenchmark(b, "left == right")
			variables := map[string]any{testLeft: benchmark.left, testRight: benchmark.right}
			ctx := context.Background()

			b.ReportAllocs()

			var result any

			for b.Loop() {
				value, err := expression.Eval(ctx, variables)
				if err != nil {
					b.Fatal(err)
				}

				result = value
			}

			runtime.KeepAlive(result)
		})
	}
}

func BenchmarkEvalFromPairsUniqueStrings(b *testing.B) {
	pairs := make([][]any, 10_000)
	for index := range pairs {
		pairs[index] = []any{strconv.Itoa(index), index}
	}

	expression := mustCompileBenchmark(b, "fromPairs(values)")
	variables := map[string]any{testValues: pairs}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalOrderingLargeSequence(b *testing.B) {
	values := make([]int, 100_000)
	for index := range values {
		values[index] = len(values) - index
	}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("identity", func(value int) int {
		return value
	}))
	if err != nil {
		b.Fatal(err)
	}

	for _, source := range []string{"reverse(values)", testSortValuesSource, "sortBy(values, identity)"} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			b.Fatal(compileErr)
		}

		b.Run(source, func(b *testing.B) {
			variables := map[string]any{testValues: values}
			ctx := context.Background()

			b.ReportAllocs()

			var result any

			for b.Loop() {
				value, evalErr := expression.Eval(ctx, variables)
				if evalErr != nil {
					b.Fatal(evalErr)
				}

				result = value
			}

			runtime.KeepAlive(result)
		})
	}
}

func BenchmarkEvalMeanLargeSequence(b *testing.B) {
	expression := mustCompileBenchmark(b, "mean(values)")

	values := make([]int, 100_000)
	for index := range values {
		values[index] = index
	}

	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalCrossTypeNumericEquality(b *testing.B) {
	expression := mustCompileBenchmark(b, "left == right")
	variables := map[string]any{
		testLeft: int64(9_007_199_254_740_993), testRight: uint64(9_007_199_254_740_993),
	}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalCrossTypeMapEquality(b *testing.B) {
	expression := mustCompileBenchmark(b, "left == right")
	left := make(map[int]string, 1_000)
	right := make(map[int64]string, 1_000)

	for index := range 1_000 {
		value := strconv.Itoa(index)
		left[index] = value
		right[int64(index)] = value
	}

	variables := map[string]any{testLeft: left, testRight: right}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalAsNumericMap(b *testing.B) {
	program, err := fasteval.CompileAs[map[int64]int64](testValues)
	if err != nil {
		b.Fatal(err)
	}

	values := make(map[int32]int32, 100_000)
	for index := range 100_000 {
		values[int32(index)] = int32(index)
	}

	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result map[int64]int64

	for b.Loop() {
		value, evalErr := program.Eval(ctx, variables)
		if evalErr != nil {
			b.Fatal(evalErr)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func BenchmarkEvalMedianLargeSequence(b *testing.B) {
	expression := mustCompileBenchmark(b, "median(values)")

	values := make([]int, 100_000)
	for index := range values {
		values[index] = index
	}

	variables := map[string]any{testValues: values}
	ctx := context.Background()

	b.ReportAllocs()

	var result any

	for b.Loop() {
		value, err := expression.Eval(ctx, variables)
		if err != nil {
			b.Fatal(err)
		}

		result = value
	}

	runtime.KeepAlive(result)
}

func mustCompileBenchmark(b *testing.B, source string) *fasteval.Expression {
	b.Helper()

	expression, err := fasteval.Compile(source)
	if err != nil {
		b.Fatal(err)
	}

	return expression
}
