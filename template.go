package fasteval

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	defaultStartDelimiter = "{{"
	defaultEndDelimiter   = "}}"
	complex64BitSize      = 64
	complex128BitSize     = 128
)

type templateOptions struct {
	start string
	end   string
}

// TemplateOption changes how a template is compiled.
type TemplateOption func(*templateOptions) error

// WithDelimiters replaces the default delimiters with a distinct, non-empty pair.
func WithDelimiters(start, end string) TemplateOption {
	return func(options *templateOptions) error {
		if start == "" || end == "" {
			return newDetailError("template delimiters cannot be empty")
		}

		if start == end {
			return newDetailError("template delimiters must be distinct")
		}

		options.start = start
		options.end = end

		return nil
	}
}

type templateSegment struct {
	text string
	root *Node
}

type templateCompiler struct {
	source      string
	compiler    *Compiler
	options     templateOptions
	segments    []templateSegment
	diagnostics []Diagnostic
	position    int
	lineOffsets []int
}

// Template is an immutable compiled template safe for concurrent use.
type Template struct {
	source   string
	compiler *Compiler
	segments []templateSegment
}

// CompileTemplate builds a [Template] from source with the default builtin-only compiler.
func CompileTemplate(source string, options ...TemplateOption) (*Template, error) {
	return defaultCompiler().CompileTemplate(source, options...)
}

// CompileTemplate builds a [Template] from source with c's function registry.
func (c *Compiler) CompileTemplate(source string, options ...TemplateOption) (*Template, error) {
	if c == nil || c.functions == nil {
		return nil, newDetailError("compiler is not initialized")
	}

	config, err := applyTemplateOptions(options)
	if err != nil {
		return nil, err
	}

	parser := templateCompiler{
		source: source, compiler: c, options: config,
		segments: nil, diagnostics: nil, position: 0, lineOffsets: sourceLineOffsets(source),
	}

	err = parser.compile()
	if err != nil {
		return nil, err
	}

	return &Template{source: source, compiler: c, segments: parser.segments}, nil
}

func applyTemplateOptions(options []TemplateOption) (templateOptions, error) {
	config := templateOptions{start: defaultStartDelimiter, end: defaultEndDelimiter}

	for _, option := range options {
		if option == nil {
			return config, newDetailError("template option cannot be nil")
		}

		err := option(&config)
		if err != nil {
			return config, fmt.Errorf("configure template: %w", err)
		}
	}

	return config, nil
}

func (p *templateCompiler) compile() error {
	for p.position < len(p.source) {
		err := p.compileNext()
		if err != nil {
			return err
		}
	}

	if len(p.source) == 0 {
		p.appendText("")
	}

	if len(p.diagnostics) != 0 {
		return newDiagnostics(p.diagnostics...)
	}

	return nil
}

func (p *templateCompiler) compileNext() error {
	relativeStart := strings.Index(p.source[p.position:], p.options.start)
	if relativeStart < 0 {
		p.appendText(p.source[p.position:])
		p.position = len(p.source)

		return nil
	}

	start := p.position + relativeStart
	if start > p.position {
		p.appendText(p.source[p.position:start])
	}

	expressionStart := start + len(p.options.start)

	end, err := findTemplateEnd(p.source, expressionStart, p.options.start, p.options.end)
	if err != nil {
		span := spanFromLineOffsets(p.lineOffsets, start, len(p.source))
		p.diagnostics = append(p.diagnostics, newErrorDiagnostic(span, err.Error()))

		return newDiagnostics(p.diagnostics...)
	}

	err = p.compileExpression(expressionStart, end)
	p.position = end + len(p.options.end)

	return err
}

func (p *templateCompiler) compileExpression(start, end int) error {
	expression, err := p.compiler.Compile(p.source[start:end])
	if err != nil {
		diagnostics := new(DiagnosticsError)
		if errors.As(err, &diagnostics) {
			shifted := shiftDiagnostics(p.lineOffsets, start, diagnostics)
			p.diagnostics = append(p.diagnostics, shifted.All()...)

			return nil
		}

		return err
	}

	shiftNodeSpans(expression.root, p.lineOffsets, start)

	p.segments = append(p.segments, templateSegment{text: "", root: expression.root})

	return nil
}

func (p *templateCompiler) appendText(text string) {
	p.segments = append(p.segments, templateSegment{text: text, root: nil})
}

// Source returns the template text supplied at compile time.
func (t *Template) Source() string {
	if t == nil {
		return ""
	}

	return t.source
}

// Execute evaluates t and writes raw output to writer. If a later expression or
// write fails, writer retains the bytes written before the error.
func (t *Template) Execute(
	ctx context.Context,
	writer io.Writer,
	variables map[string]any,
	options ...EvalOption,
) (int64, error) {
	if t == nil || t.compiler == nil {
		return 0, newDetailError("template is not initialized")
	}

	if writer == nil {
		return 0, newDetailError("template writer is nil")
	}

	var config evalOptions

	if len(options) != 0 {
		var err error

		config, err = applyEvalOptions(options)
		if err != nil {
			return 0, err
		}
	}

	ctx = contextOrBackground(ctx)
	state := evalState{
		variables: variables, resolver: config.resolver, functions: t.compiler.functions,
		preserveLiterals: false, stepsUntilContextCheck: 0,
	}

	var written int64

	for _, segment := range t.segments {
		err := ctx.Err()
		if err != nil {
			return written, evalError(ErrCanceled, "evaluate template", emptySpan(), "", err)
		}

		state.stepsUntilContextCheck = evaluationContextCheckInterval - 1

		count, err := executeTemplateSegment(ctx, writer, &state, segment)
		written += int64(count)

		if err != nil {
			return written, err
		}
	}

	return written, nil
}

func executeTemplateSegment(
	ctx context.Context,
	writer io.Writer,
	state *evalState,
	segment templateSegment,
) (int, error) {
	if segment.root == nil {
		return writeString(writer, segment.text)
	}

	value, err := state.evaluate(ctx, segment.root)
	if err != nil {
		return 0, err
	}

	value, err = materializeLiteral(value, segment.root.span)
	if err != nil {
		return 0, err
	}

	formatted, err := formatTemplateValue(value, segment.root.span)
	if err != nil {
		return 0, err
	}

	return writeString(writer, formatted)
}

// Render evaluates t and returns raw output. On error, it also returns the text
// completed before the failure.
func (t *Template) Render(ctx context.Context, variables map[string]any, options ...EvalOption) (string, error) {
	var output strings.Builder
	if t != nil {
		output.Grow(len(t.source))
	}

	_, err := t.Execute(ctx, &output, variables, options...)

	return output.String(), err
}

func findTemplateEnd(source string, start int, startDelimiter, endDelimiter string) (int, error) {
	firstEnd := strings.Index(source[start:], endDelimiter)

	windowEnd := len(source)

	if firstEnd >= 0 {
		windowEnd = start + firstEnd + len(endDelimiter)
	}

	for {
		scanner := lexer{
			source: source[start:windowEnd], offset: 0, lineOffsets: nil,
			listBracketOffsets: nil, token: emptyLexToken(), classifyBrackets: false, canEnd: false,
		}

		end, found, uncertain, err := scanner.findTemplateEnd(startDelimiter, endDelimiter)
		if uncertain && windowEnd != len(source) {
			found, err = false, nil
		}

		if err != nil {
			return 0, err
		}

		if found {
			return start + end, nil
		}

		if windowEnd == len(source) {
			break
		}

		windowLength := windowEnd - start
		windowEnd = min(len(source), start+windowLength*2)
	}

	return 0, newDetailError("cannot find template end delimiter %q", endDelimiter)
}

func formatTemplateValue(value any, span Span) (string, error) {
	var result string

	var err error

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = ""
				err = evalError(
					ErrPanic, "format template value", span, "",
					newDetailError("panic: %v", recovered),
				)
			}
		}()

		result, err = formatTemplateValueUnsafe(value, span)
	}()

	return result, err
}

func formatTemplateValueUnsafe(value any, span Span) (string, error) {
	if value == nil {
		return "", nil
	}

	if result, ok := formatSimpleValue(value); ok {
		return result, nil
	}

	if result, ok := formatSignedValue(value); ok {
		return result, nil
	}

	if result, ok := formatUnsignedValue(value); ok {
		return result, nil
	}

	if result, ok := formatRealValue(value); ok {
		return result, nil
	}

	marshaler, ok := value.(encoding.TextMarshaler)
	if !ok {
		return "", evalError(
			ErrFormat, "format template value", span, "",
			newDetailError("type %T requires explicit conversion", value),
		)
	}

	text, err := marshaler.MarshalText()
	if err != nil {
		return "", evalError(ErrFormat, "marshal template value", span, "", err)
	}

	return string(text), nil
}

func formatSimpleValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	case bool:
		return strconv.FormatBool(typed), true
	default:
		return "", false
	}
}

func formatSignedValue(value any) (string, bool) {
	switch typed := value.(type) {
	case int:
		return strconv.Itoa(typed), true
	case int8:
		return strconv.FormatInt(int64(typed), 10), true
	case int16:
		return strconv.FormatInt(int64(typed), 10), true
	case int32:
		return strconv.FormatInt(int64(typed), 10), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	default:
		return "", false
	}
}

func formatUnsignedValue(value any) (string, bool) {
	switch typed := value.(type) {
	case uint:
		return strconv.FormatUint(uint64(typed), 10), true
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), true
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), true
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), true
	case uint64:
		return strconv.FormatUint(typed, 10), true
	case uintptr:
		return strconv.FormatUint(uint64(typed), 10), true
	default:
		return "", false
	}
}

func formatRealValue(value any) (string, bool) {
	switch typed := value.(type) {
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), true
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), true
	case complex64:
		return strconv.FormatComplex(complex128(typed), 'g', -1, complex64BitSize), true
	case complex128:
		return strconv.FormatComplex(typed, 'g', -1, complex128BitSize), true
	default:
		return "", false
	}
}

func writeString(w io.Writer, value string) (int, error) {
	written := 0
	for written < len(value) {
		count, err := io.WriteString(w, value[written:])
		if count < 0 || count > len(value)-written {
			return written, newDetailError(
				"invalid write count %d for %d-byte buffer", count, len(value)-written,
			)
		}

		written += count
		if err != nil {
			return written, fmt.Errorf("write template output: %w", err)
		}

		if count == 0 {
			return written, io.ErrShortWrite
		}
	}

	return written, nil
}

func shiftDiagnostics(lineOffsets []int, offset int, diagnostics *DiagnosticsError) *DiagnosticsError {
	items := diagnostics.All()
	for cursor := range items {
		items[cursor].Span = spanFromLineOffsets(
			lineOffsets, items[cursor].Span.Start+offset, items[cursor].Span.End+offset,
		)
	}

	return newDiagnostics(items...)
}

func shiftNodeSpans(currentNode *Node, lineOffsets []int, offset int) {
	if currentNode == nil {
		return
	}

	currentNode.span = spanFromLineOffsets(
		lineOffsets, currentNode.span.Start+offset, currentNode.span.End+offset,
	)
	for _, child := range currentNode.children {
		shiftNodeSpans(child, lineOffsets, offset)
	}
}
