package fasteval_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"go.dw1.io/fasteval"
)

func TestTemplateExpressionsAndSubstitutions(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("Hello {{ name }}: {{score >= 80 ? 'pass' : 'fail'}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), map[string]any{testName: testAda, testScore: 90})
	if err != nil || got != "Hello Ada: pass" {
		t.Fatalf("Render() = %q, %v", got, err)
	}
}

func TestTemplateDelimiterInsideExpressionString(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate(`{{ "}}" + suffix }}`)
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), map[string]any{"suffix": "!"})
	if err != nil || got != "}}!" {
		t.Fatalf("Render() = %q, %v", got, err)
	}
}

func TestTemplateCustomDelimiters(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("Hello <% name %>", fasteval.WithDelimiters("<%", "%>"))
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), map[string]any{testName: testAda})
	if err != nil || got != "Hello Ada" {
		t.Fatalf("Render() = %q, %v", got, err)
	}
}

func TestTemplateQuotePrefixedEndDelimiters(t *testing.T) {
	t.Parallel()

	for _, end := range []string{`"%>`, `'%>`} {
		t.Run(end, func(t *testing.T) {
			t.Parallel()

			source := "Hello <%name" + end

			tpl, err := fasteval.CompileTemplate(source, fasteval.WithDelimiters("<%", end))
			if err != nil {
				t.Fatalf("CompileTemplate() error = %v", err)
			}

			got, err := tpl.Render(context.Background(), map[string]any{testName: testAda})
			if err != nil || got != "Hello Ada" {
				t.Fatalf("Render() = %q, %v; want Hello Ada, nil", got, err)
			}
		})
	}
}

func TestTemplateQuotePrefixedStartDelimiters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		start  string
		source string
	}{
		{name: "single quote", start: "'", source: "''x'}}"},
		{name: "double quote", start: `"`, source: `""x"}}`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tpl, err := fasteval.CompileTemplate(
				testCase.source, fasteval.WithDelimiters(testCase.start, "}}"),
			)
			if err != nil {
				t.Fatalf("CompileTemplate() error = %v", err)
			}

			got, err := tpl.Render(context.Background(), nil)
			if err != nil || got != "x" {
				t.Fatalf("Render() = %q, %v; want x, nil", got, err)
			}
		})
	}
}

func TestTemplateRendersExplicitDurationString(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("{{string(duration('1s'))}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), nil)
	if err != nil || got != "1s" {
		t.Fatalf("Render() = %q, %v; want 1s, nil", got, err)
	}
}

func TestTemplateBracketedIdentifierIgnoresDelimiterCharacters(t *testing.T) {
	t.Parallel()

	variables := map[string]any{"key(": 42}

	value, err := fasteval.Eval(context.Background(), "[key(]", variables)
	if err != nil || value != 42 {
		t.Fatalf("Eval() = %#v, %v; want 42, nil", value, err)
	}

	tpl, err := fasteval.CompileTemplate("{{ [key(] }}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), variables)
	if err != nil || got != "42" {
		t.Fatalf("Render() = %q, %v; want 42, nil", got, err)
	}
}

func TestTemplateBracketedIdentifierCanContainQuotes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"foo'bar", `foo"bar`} {
		source := "{{[" + name + "]}}"

		tpl, err := fasteval.CompileTemplate(source)
		if err != nil {
			t.Fatalf("CompileTemplate(%q) error = %v", source, err)
		}

		got, err := tpl.Render(context.Background(), map[string]any{name: 42})
		if err != nil || got != "42" {
			t.Fatalf("Render(%q) = %q, %v; want 42, nil", source, got, err)
		}
	}
}

func TestTemplateMalformedBracketEscapeReturnsDiagnostics(t *testing.T) {
	t.Parallel()

	source := string([]byte{'{', '{', '[', '\\', 0xff, ']', '}', '}'})
	_, err := fasteval.CompileTemplate(source)

	var diagnostics *fasteval.DiagnosticsError
	if !errors.As(err, &diagnostics) || diagnostics.Len() == 0 {
		t.Fatalf("CompileTemplate() error = %v, want diagnostics", err)
	}
}

func TestTemplateListStringCanContainClosingBracket(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("{{toJSON([']',])}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), nil)
	if err != nil || got != `["]"]` {
		t.Fatalf("Render() = %q, %v; want %q, nil", got, err, `["]"]`)
	}
}

func TestTemplateQuotedIndexCanContainEndDelimiter(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate(`{{ values["]}}"] }}`)
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), map[string]any{
		testValues: map[string]string{"]}}": "ok"},
	})
	if err != nil || got != "ok" {
		t.Fatalf("Render() = %q, %v; want ok, nil", got, err)
	}
}

func TestTemplateEscapedIdentifierQuoteCanContainClosingBracket(t *testing.T) {
	t.Parallel()

	template, err := fasteval.CompileTemplate("{{[']']}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := template.Render(context.Background(), map[string]any{"']'": 42})
	if err != nil || got != "42" {
		t.Fatalf("Render() = %q, %v; want 42, nil", got, err)
	}
}

func TestTemplateNestedBracketDisambiguation(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("{{toJSON([[foo],])}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	got, err := tpl.Render(context.Background(), map[string]any{testFoo: 1})
	if err != nil || got != "[1]" {
		t.Fatalf("Render() = %q, %v; want %q, nil", got, err, "[1]")
	}
}

func TestTemplateResolverCancellationWinsOverReturnedIdentifier(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("{{resolved}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	_, err = tpl.Render(ctx, nil, fasteval.WithResolver(func(context.Context, string) (any, bool, error) {
		cancel()

		return 42, true, nil
	}))

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrCanceled ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("Render() error = %v, want ErrCanceled", err)
	}
}

func TestTemplateReturnsPartialResultsOnEvaluationError(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("Hello {{name}}! {{1 / zero}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	vars := map[string]any{testName: testAda, testZero: 0}

	var dst bytes.Buffer

	n, err := tpl.Execute(context.Background(), &dst, vars)
	if err == nil || n != 11 || dst.String() != "Hello Ada! " {
		t.Fatalf("Execute() = %d, %q, %v; want 11, partial output, error", n, dst.String(), err)
	}

	got, err := tpl.Render(context.Background(), vars)
	if err == nil || got != "Hello Ada! " {
		t.Fatalf("Render() = %q, %v; want partial output and error", got, err)
	}
}

func TestTemplateWriterFailureReportsWrittenBytes(t *testing.T) {
	t.Parallel()

	tpl, err := fasteval.CompileTemplate("abc{{name}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	w := &limitedWriter{remaining: 4}

	n, err := tpl.Execute(context.Background(), w, map[string]any{testName: testAda})
	if !errors.Is(err, errWriterFull) || n != 4 {
		t.Fatalf("Execute() = %d, %v; want 4, errWriterFull", n, err)
	}
}

func TestTemplateCompileCollectsIndependentDiagnostics(t *testing.T) {
	t.Parallel()

	_, err := fasteval.CompileTemplate("{{ 1 + }} between {{ name =~ '[' }}")

	var diagnostics *fasteval.DiagnosticsError

	if !errors.As(err, &diagnostics) {
		t.Fatalf("CompileTemplate() error = %v, want DiagnosticsError", err)
	}

	if diagnostics.Len() != 2 {
		t.Fatalf("CompileTemplate() diagnostics = %d, want 2", diagnostics.Len())
	}
}

var errWriterFull = errors.New("writer full")

type limitedWriter struct{ remaining int }

func (w *limitedWriter) Write(p []byte) (int, error) {
	if len(p) <= w.remaining {
		w.remaining -= len(p)

		return len(p), nil
	}

	n := w.remaining
	w.remaining = 0

	return n, errWriterFull
}
