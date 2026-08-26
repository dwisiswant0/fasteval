package fasteval

import (
	"errors"
	"fmt"
	"strings"
)

func newDetailError(format string, arguments ...any) error {
	return fmt.Errorf(format, arguments...) //nolint:err113 // Detail errors intentionally include evaluator context.
}

// ErrorCode classifies compile-time and evaluation failures. Its values are stable.
type ErrorCode string

const (
	// ErrSyntax reports malformed expression or template syntax.
	ErrSyntax ErrorCode = "syntax"
	// ErrUnknownVariable reports an unresolved variable name.
	ErrUnknownVariable ErrorCode = "unknown_variable"
	// ErrType reports an operation or conversion applied to an invalid type.
	ErrType ErrorCode = "type"
	// ErrOverflow reports checked integer overflow or underflow.
	ErrOverflow ErrorCode = "overflow"
	// ErrDivisionByZero reports integer division or remainder by zero.
	ErrDivisionByZero ErrorCode = "division_by_zero"
	// ErrAccess reports invalid field, method, map, or index access.
	ErrAccess ErrorCode = "access"
	// ErrFunction reports invalid function registration or invocation.
	ErrFunction ErrorCode = "function"
	// ErrPanic reports a recovered panic from caller-controlled code.
	ErrPanic ErrorCode = "panic"
	// ErrCanceled reports evaluation canceled through context.Context.
	ErrCanceled ErrorCode = "canceled"
	// ErrFormat reports a value that a template cannot render.
	ErrFormat ErrorCode = "format"
)

// Severity classifies the effect of a diagnostic.
type Severity string

const (
	// SeverityError marks a diagnostic that prevents compilation.
	SeverityError Severity = "error"
	// SeverityWarning marks a non-fatal diagnostic.
	SeverityWarning Severity = "warning"
)

// Span locates a half-open byte range at a one-based source position.
type Span struct {
	Start  int
	End    int
	Line   int
	Column int
}

func emptySpan() Span {
	return Span{Start: 0, End: 0, Line: 0, Column: 0}
}

func withEvalErrorSpan(err error, span Span) error {
	evaluationErr := new(EvalError)
	if !errors.As(err, &evaluationErr) || evaluationErr.span != emptySpan() {
		return err
	}

	withSpan := *evaluationErr
	withSpan.span = span

	return &withSpan
}

// Diagnostic describes one compilation issue.
type Diagnostic struct {
	Code     ErrorCode
	Severity Severity
	Span     Span
	Message  string
	Notes    []string
}

func newErrorDiagnostic(span Span, message string) Diagnostic {
	return Diagnostic{
		Code: ErrSyntax, Severity: SeverityError, Span: span, Message: message, Notes: nil,
	}
}

// DiagnosticsError contains one or more compilation diagnostics. Its accessors
// return copies instead of exposing mutable backing storage.
type DiagnosticsError struct {
	items []Diagnostic
}

func newDiagnostics(items ...Diagnostic) *DiagnosticsError {
	return &DiagnosticsError{items: append([]Diagnostic(nil), items...)}
}

// Error formats all diagnostics as a compact string.
func (d *DiagnosticsError) Error() string {
	if d == nil || len(d.items) == 0 {
		return "expression compilation failed"
	}

	var builder strings.Builder

	for index, item := range d.items {
		if index > 0 {
			builder.WriteString("; ")
		}

		fmt.Fprintf(&builder, "%d:%d: %s", item.Span.Line, item.Span.Column, item.Message)
	}

	return builder.String()
}

// Len returns the number of diagnostics in d.
func (d *DiagnosticsError) Len() int {
	if d == nil {
		return 0
	}

	return len(d.items)
}

// At returns the diagnostic at index.
func (d *DiagnosticsError) At(index int) Diagnostic {
	item := d.items[index]
	item.Notes = append([]string(nil), item.Notes...)

	return item
}

// All returns a copy of every diagnostic in d.
func (d *DiagnosticsError) All() []Diagnostic {
	if d == nil {
		return nil
	}

	items := make([]Diagnostic, len(d.items))
	for index := range d.items {
		items[index] = d.At(index)
	}

	return items
}

// EvalError describes a failure during evaluation.
type EvalError struct {
	code      ErrorCode
	operation string
	span      Span
	path      string
	cause     error
}

func evalError(code ErrorCode, operation string, span Span, path string, cause error) *EvalError {
	return &EvalError{code: code, operation: operation, span: span, path: path, cause: cause}
}

// Error formats the evaluation failure.
func (e *EvalError) Error() string {
	if e == nil {
		return "<nil>"
	}

	where := ""
	if e.span.Line > 0 {
		where = fmt.Sprintf(" at %d:%d", e.span.Line, e.span.Column)
	}

	path := ""
	if e.path != "" {
		path = " " + e.path
	}

	if e.cause == nil {
		return fmt.Sprintf("%s%s%s", e.operation, path, where)
	}

	return fmt.Sprintf("%s%s%s: %v", e.operation, path, where, e.cause)
}

// Unwrap returns the underlying cause.
func (e *EvalError) Unwrap() error { return e.cause }

// Code returns the stable error category.
func (e *EvalError) Code() ErrorCode { return e.code }

// Operation returns the operation that failed.
func (e *EvalError) Operation() string { return e.operation }

// Span returns the source location associated with the failure.
func (e *EvalError) Span() Span { return e.span }

// Path returns the related variable, accessor, or function path.
func (e *EvalError) Path() string { return e.path }
