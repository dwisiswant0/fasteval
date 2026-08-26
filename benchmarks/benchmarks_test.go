package benchmarks_test

import (
	"context"
	"runtime"
	"testing"

	"cel.dev/cel-go/cel"
	"github.com/PaesslerAG/gval"
	"github.com/casbin/govaluate"
	"github.com/expr-lang/expr"
	"go.dw1.io/fasteval"
)

type benchmarkWorkload struct {
	name       string
	expression string
	variables  map[string]any
	celOptions []cel.EnvOption
	want       any
}

func benchmarkWorkloads() []benchmarkWorkload {
	return []benchmarkWorkload{
		{
			name:       "Arithmetic",
			expression: "1 + 2 * 3",
			variables:  nil,
			celOptions: nil,
			want:       float64(7),
		},
		{
			name:       "Variables",
			expression: "price * quantity >= 100.0",
			variables:  map[string]any{"price": 25.0, "quantity": 4.0},
			celOptions: []cel.EnvOption{
				cel.Variable("price", cel.DoubleType),
				cel.Variable("quantity", cel.DoubleType),
			},
			want: true,
		},
		{
			name:       "Boolean",
			expression: "requests >= 100.0 && success / requests >= 0.9",
			variables:  map[string]any{"requests": 120.0, "success": 114.0},
			celOptions: []cel.EnvOption{
				cel.Variable("requests", cel.DoubleType),
				cel.Variable("success", cel.DoubleType),
			},
			want: true,
		},
		{
			name:       "String",
			expression: "name == expected && role != excluded",
			variables: map[string]any{
				"name":     "alice",
				"expected": "alice",
				"role":     "admin",
				"excluded": "guest",
			},
			celOptions: []cel.EnvOption{
				cel.Variable("name", cel.StringType),
				cel.Variable("expected", cel.StringType),
				cel.Variable("role", cel.StringType),
				cel.Variable("excluded", cel.StringType),
			},
			want: true,
		},
	}
}

func BenchmarkCompile(b *testing.B) {
	for _, workload := range benchmarkWorkloads() {
		validateWorkload(b, workload)

		b.Run(workload.name, func(b *testing.B) {
			b.Run("fasteval", func(b *testing.B) {
				benchmarkCompileFasteval(b, workload)
			})
			b.Run("govaluate", func(b *testing.B) {
				benchmarkCompileGovaluate(b, workload)
			})
			b.Run("expr", func(b *testing.B) {
				benchmarkCompileExpr(b, workload)
			})
			b.Run("cel", func(b *testing.B) {
				benchmarkCompileCEL(b, workload)
			})
			b.Run("gval", func(b *testing.B) {
				benchmarkCompileGval(b, workload)
			})
		})
	}
}

func benchmarkCompileFasteval(b *testing.B, workload benchmarkWorkload) {
	b.Helper()
	b.ReportAllocs()

	for b.Loop() {
		program, err := fasteval.Compile(workload.expression)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(program)
	}
}

func benchmarkCompileGovaluate(b *testing.B, workload benchmarkWorkload) {
	b.Helper()
	b.ReportAllocs()

	for b.Loop() {
		program, err := govaluate.NewEvaluableExpression(workload.expression)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(program)
	}
}

func benchmarkCompileExpr(b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	options := []expr.Option{expr.Env(workload.variables)}

	b.ReportAllocs()

	for b.Loop() {
		program, err := expr.Compile(workload.expression, options...)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(program)
	}
}

func benchmarkCompileCEL(b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	environment, err := cel.NewEnv(workload.celOptions...)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		ast, issues := environment.Compile(workload.expression)
		if issues != nil && issues.Err() != nil {
			b.Fatal(issues.Err())
		}

		program, err := environment.Program(ast)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(program)
	}
}

func benchmarkCompileGval(b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	language := gval.Full()

	b.ReportAllocs()

	for b.Loop() {
		program, err := language.NewEvaluable(workload.expression)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(program)
	}
}

func BenchmarkEval(b *testing.B) {
	ctx := context.Background()

	for _, workload := range benchmarkWorkloads() {
		validateWorkload(b, workload)

		b.Run(workload.name, func(b *testing.B) {
			b.Run("fasteval", func(b *testing.B) {
				benchmarkEvalFasteval(ctx, b, workload)
			})
			b.Run("govaluate", func(b *testing.B) {
				benchmarkEvalGovaluate(b, workload)
			})
			b.Run("expr", func(b *testing.B) {
				benchmarkEvalExpr(b, workload)
			})
			b.Run("cel", func(b *testing.B) {
				benchmarkEvalCEL(b, workload)
			})
			b.Run("gval", func(b *testing.B) {
				benchmarkEvalGval(ctx, b, workload)
			})
		})
	}
}

func benchmarkEvalFasteval(ctx context.Context, b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	program, err := fasteval.Compile(workload.expression)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		result, err := program.Eval(ctx, workload.variables)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(result)
	}
}

func benchmarkEvalGovaluate(b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	program, err := govaluate.NewEvaluableExpression(workload.expression)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		result, err := program.Evaluate(workload.variables)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(result)
	}
}

func benchmarkEvalExpr(b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	program, err := expr.Compile(workload.expression, expr.Env(workload.variables))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		result, err := expr.Run(program, workload.variables)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(result)
	}
}

func benchmarkEvalCEL(b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	environment, err := cel.NewEnv(workload.celOptions...)
	if err != nil {
		b.Fatal(err)
	}

	ast, issues := environment.Compile(workload.expression)
	if issues != nil && issues.Err() != nil {
		b.Fatal(issues.Err())
	}

	program, err := environment.Program(ast)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		result, _, err := program.Eval(workload.variables)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(result)
	}
}

func benchmarkEvalGval(ctx context.Context, b *testing.B, workload benchmarkWorkload) {
	b.Helper()

	program, err := gval.Full().NewEvaluable(workload.expression)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		result, err := program(ctx, workload.variables)
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(result)
	}
}

func TestBenchmarkWorkloads(t *testing.T) {
	t.Parallel()

	for _, workload := range benchmarkWorkloads() {
		t.Run(workload.name, func(t *testing.T) {
			t.Parallel()

			validateWorkload(t, workload)
		})
	}
}

func validateWorkload(tb testing.TB, workload benchmarkWorkload) {
	tb.Helper()

	results := []struct {
		engine string
		value  any
	}{
		{engine: "fasteval", value: evaluateFasteval(tb, workload)},
		{engine: "govaluate", value: evaluateGovaluate(tb, workload)},
		{engine: "expr", value: evaluateExpr(tb, workload)},
		{engine: "cel", value: evaluateCEL(tb, workload)},
		{engine: "gval", value: evaluateGval(tb, workload)},
	}
	for _, result := range results {
		checkBenchmarkResult(tb, result.engine, result.value, workload.want)
	}
}

func evaluateFasteval(tb testing.TB, workload benchmarkWorkload) any {
	tb.Helper()

	program, err := fasteval.Compile(workload.expression)
	if err != nil {
		tb.Fatalf("fasteval compile %q: %v", workload.expression, err)
	}

	result, err := program.Eval(context.Background(), workload.variables)
	if err != nil {
		tb.Fatalf("fasteval evaluate %q: %v", workload.expression, err)
	}

	return result
}

func evaluateGovaluate(tb testing.TB, workload benchmarkWorkload) any {
	tb.Helper()

	program, err := govaluate.NewEvaluableExpression(workload.expression)
	if err != nil {
		tb.Fatalf("govaluate compile %q: %v", workload.expression, err)
	}

	result, err := program.Evaluate(workload.variables)
	if err != nil {
		tb.Fatalf("govaluate evaluate %q: %v", workload.expression, err)
	}

	return result
}

func evaluateExpr(tb testing.TB, workload benchmarkWorkload) any {
	tb.Helper()

	program, err := expr.Compile(workload.expression, expr.Env(workload.variables))
	if err != nil {
		tb.Fatalf("expr compile %q: %v", workload.expression, err)
	}

	result, err := expr.Run(program, workload.variables)
	if err != nil {
		tb.Fatalf("expr evaluate %q: %v", workload.expression, err)
	}

	return result
}

func evaluateCEL(tb testing.TB, workload benchmarkWorkload) any {
	tb.Helper()

	environment, err := cel.NewEnv(workload.celOptions...)
	if err != nil {
		tb.Fatalf("cel create environment for %q: %v", workload.expression, err)
	}

	ast, issues := environment.Compile(workload.expression)
	if issues != nil && issues.Err() != nil {
		tb.Fatalf("cel compile %q: %v", workload.expression, issues.Err())
	}

	program, err := environment.Program(ast)
	if err != nil {
		tb.Fatalf("cel create program for %q: %v", workload.expression, err)
	}

	result, _, err := program.Eval(workload.variables)
	if err != nil {
		tb.Fatalf("cel evaluate %q: %v", workload.expression, err)
	}

	return result.Value()
}

func evaluateGval(tb testing.TB, workload benchmarkWorkload) any {
	tb.Helper()

	program, err := gval.Full().NewEvaluable(workload.expression)
	if err != nil {
		tb.Fatalf("gval compile %q: %v", workload.expression, err)
	}

	result, err := program(context.Background(), workload.variables)
	if err != nil {
		tb.Fatalf("gval evaluate %q: %v", workload.expression, err)
	}

	return result
}

func checkBenchmarkResult(tb testing.TB, engine string, got, want any) {
	tb.Helper()

	wantNumber, wantIsNumber := want.(float64)
	if wantIsNumber {
		gotNumber, ok := benchmarkNumber(got)
		if !ok || gotNumber != wantNumber {
			tb.Fatalf("%s result = %v (%T), want %v", engine, got, got, want)
		}

		return
	}

	if got != want {
		tb.Fatalf("%s result = %v (%T), want %v (%T)", engine, got, got, want, want)
	}
}

func benchmarkNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float64:
		return value, true
	default:
		return 0, false
	}
}
