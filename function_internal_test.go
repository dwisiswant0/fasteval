package fasteval

import (
	"context"
	"testing"
)

type namedIntegerResult int

func (value namedIntegerResult) Double() int { return int(value) * 2 }

func TestBuiltinCallResultsPreserveNamedValuesForAccessAndIndex(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiler.functions["namedBuiltin"] = newBuiltin(
		"namedBuiltin", func(context.Context, []any) (any, error) { return namedIntegerResult(21), nil }, callableArguments,
	)

	for _, testCase := range []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "namedBuiltin().Double()", vars: nil, want: 42},
		{
			source: "values[namedBuiltin()]",
			vars:   map[string]any{"values": map[namedIntegerResult]string{21: "found"}},
			want:   "found",
		},
		{source: "namedBuiltin()", vars: nil, want: int(21)},
	} {
		expression, compileErr := compiler.Compile(testCase.source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", testCase.source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), testCase.vars)
		if evalErr != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, evalErr, testCase.want)
		}
	}
}
