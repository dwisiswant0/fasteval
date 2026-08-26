package fasteval_test

import (
	"context"
	"testing"

	"go.dw1.io/fasteval"
)

func FuzzCompileAndEvalNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"1 + 2 * 3", "name =~ '^fast.*'", "{'a': [1, 2]}?.a[0]", "[escaped name]", "\xff",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, source string) {
		expression, err := fasteval.Compile(source)
		if err != nil {
			return
		}

		_, _ = expression.Eval(context.Background(), map[string]any{
			testName: testFasteval, "escaped name": 1,
		})
	})
}

func FuzzCompileTemplateNeverPanics(f *testing.F) {
	for _, seed := range []string{testText, "{{name}}", `{{ "}}" }}`, "{{ {'a': 1} }}", "{{"} {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, source string) {
		template, err := fasteval.CompileTemplate(source)
		if err != nil {
			return
		}

		_, _ = template.Render(context.Background(), map[string]any{testName: testAda})
	})
}
