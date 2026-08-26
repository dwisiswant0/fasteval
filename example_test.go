package fasteval_test

import (
	"context"
	"errors"
	"fmt"

	"go.dw1.io/fasteval"
)

func ExampleCompile() {
	expression, err := fasteval.Compile("price * quantity >= 100")
	if err != nil {
		panic(err)
	}

	value, err := expression.Eval(context.Background(), map[string]any{
		"price":    25,
		"quantity": 4,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: true
}

func ExampleCompileAs() {
	type result struct {
		Name  string `json:"name"`
		Valid bool   `json:"valid"`
	}

	program, err := fasteval.CompileAs[result]("{'name': name, 'valid': score >= 80}")
	if err != nil {
		panic(err)
	}

	value, err := program.Eval(context.Background(), map[string]any{testName: testAda, testScore: 90})
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s: %t\n", value.Name, value.Valid)
	// Output: Ada: true
}

func ExampleCompileTemplate() {
	template, err := fasteval.CompileTemplate("Hello {{name}}: {{score >= 80 ? 'pass' : 'fail'}}")
	if err != nil {
		panic(err)
	}

	value, err := template.Render(context.Background(), map[string]any{testName: testAda, testScore: 90})
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: Hello Ada: pass
}

func ExampleWithResolver() {
	expression, err := fasteval.Compile("known + lazy")
	if err != nil {
		panic(err)
	}

	value, err := expression.Eval(
		context.Background(),
		map[string]any{"known": 40},
		fasteval.WithResolver(func(_ context.Context, name string) (any, bool, error) {
			if name == "lazy" {
				return 2, true, nil
			}

			return nil, false, nil
		}),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: 42
}

func ExampleCompiler_CompileTemplate() {
	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("greet", func(name string) string {
			return "Hello " + name
		}),
	)
	if err != nil {
		panic(err)
	}

	template, err := compiler.CompileTemplate("{{greet(name)}}")
	if err != nil {
		panic(err)
	}

	value, err := template.Render(context.Background(), map[string]any{testName: testAda})
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: Hello Ada
}

func ExampleDiagnosticsError() {
	_, err := fasteval.Compile("price +")

	var diagnostics *fasteval.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		panic(err)
	}

	diagnostic := diagnostics.At(0)
	fmt.Println(diagnostics.Len(), diagnostic.Code, diagnostic.Severity)
	// Output: 1 syntax error
}

func ExampleEvalError() {
	_, err := fasteval.Eval(context.Background(), "missing", nil)

	var evaluationError *fasteval.EvalError
	if !errors.As(err, &evaluationError) {
		panic(err)
	}

	fmt.Println(evaluationError.Code(), evaluationError.Operation(), evaluationError.Path())
	// Output: unknown_variable resolve variable missing
}

func ExampleTemplate_Render_partialOutput() {
	template, err := fasteval.CompileTemplate("Hello {{name}}! {{1 / zero}}")
	if err != nil {
		panic(err)
	}

	value, err := template.Render(context.Background(), map[string]any{
		testName: testAda,
		"zero":   0,
	})

	var evaluationError *fasteval.EvalError
	if !errors.As(err, &evaluationError) {
		panic(err)
	}

	fmt.Printf("%q %s\n", value, evaluationError.Code())
	// Output: "Hello Ada! " division_by_zero
}

func ExampleExpression_AST() {
	expression, err := fasteval.Compile("price * quantity")
	if err != nil {
		panic(err)
	}

	root := expression.AST()
	fmt.Println(root.Kind() == fasteval.NodeBinary, root.Text(), len(root.Children()))

	for _, child := range root.Children() {
		fmt.Println(child.Kind() == fasteval.NodeIdentifier, child.Text())
	}
	// Output:
	// true * 2
	// true price
	// true quantity
}
