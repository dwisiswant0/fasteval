# Errors and AST inspection

[`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) returns typed errors for
compilation and evaluation, so callers can classify failures without parsing
error strings.

## Compile diagnostics

Expression and template syntax failures from
[`Compile`](https://pkg.go.dev/go.dw1.io/fasteval#Compile) and
[`CompileTemplate`](https://pkg.go.dev/go.dw1.io/fasteval#CompileTemplate)
return
[`*DiagnosticsError`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError).
One error may report several diagnostics, especially when more than one template
tag is invalid.

```go
expression, err := fasteval.Compile("price +")
if err != nil {
	var diagnostics *fasteval.DiagnosticsError
	if errors.As(err, &diagnostics) {
		for _, diagnostic := range diagnostics.All() {
			fmt.Printf("%s at %d:%d: %s\n",
				diagnostic.Code,
				diagnostic.Span.Line,
				diagnostic.Span.Column,
				diagnostic.Message,
			)
		}
	}
}
_ = expression
```

[`DiagnosticsError`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError)
has three accessors:

- [`DiagnosticsError.Len`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError.Len)
  returns the number of diagnostics.
- [`DiagnosticsError.At`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError.At)
  returns one diagnostic by index.
- [`DiagnosticsError.All`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError.All)
  returns a copy of the complete slice.

[`DiagnosticsError.At`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError.At)
and
[`DiagnosticsError.All`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError.All)
copy mutable note slices. Changes to the returned values do not affect the error
or compiled state.

Each [`Diagnostic`](https://pkg.go.dev/go.dw1.io/fasteval#Diagnostic) contains:

- [`Diagnostic.Code`](https://pkg.go.dev/go.dw1.io/fasteval#Diagnostic),
  currently [`ErrSyntax`](https://pkg.go.dev/go.dw1.io/fasteval#ErrSyntax) for
  parser and template syntax failures;
- [`Diagnostic.Severity`](https://pkg.go.dev/go.dw1.io/fasteval#Diagnostic);
- [`Diagnostic.Span`](https://pkg.go.dev/go.dw1.io/fasteval#Diagnostic);
- a human-readable
  [`Diagnostic.Message`](https://pkg.go.dev/go.dw1.io/fasteval#Diagnostic);
- optional [`Diagnostic.Notes`](https://pkg.go.dev/go.dw1.io/fasteval#Diagnostic).

Compiler configuration errors, invalid options, and invalid function
registrations may return ordinary wrapped errors instead of
[`DiagnosticsError`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError).
Handle the error even when
[`errors.As`](https://pkg.go.dev/errors#As) does not find a diagnostic list.

## Evaluation errors

[`Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Eval) and the other evaluation
methods usually return
[`*EvalError`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError):

```go
_, err := fasteval.Eval(ctx, "account.balance / divisor", variables)
if err != nil {
	var evaluationError *fasteval.EvalError
	if errors.As(err, &evaluationError) {
		fmt.Printf("%s during %s at %d:%d\n",
			evaluationError.Code(),
			evaluationError.Operation(),
			evaluationError.Span().Line,
			evaluationError.Span().Column,
		)
	}
}
```

An [`EvalError`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError) exposes:

- [`EvalError.Code`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError.Code) for
  stable programmatic classification;
- [`EvalError.Operation`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError.Operation)
  for the operation that failed;
- [`EvalError.Span`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError.Span) for
  the related source range;
- [`EvalError.Path`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError.Path) for a
  variable, member, or function name when available;
- [`EvalError.Unwrap`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError.Unwrap)
  for the underlying cause.

Use [`errors.Is`](https://pkg.go.dev/errors#Is) when the wrapped cause has its
own identity. A canceled evaluation, for example, wraps
[`context.Canceled`](https://pkg.go.dev/context#Canceled) or
[`context.DeadlineExceeded`](https://pkg.go.dev/context#DeadlineExceeded):

```go
if errors.Is(err, context.DeadlineExceeded) {
	// The caller's deadline ended evaluation.
}
```

Output writers and some option or zero-value checks may return ordinary wrapped
errors instead of
[`EvalError`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError).

## Error codes

| Code | Meaning |
| --- | --- |
| [`ErrSyntax`](https://pkg.go.dev/go.dw1.io/fasteval#ErrSyntax) | Malformed expression or template syntax. |
| [`ErrUnknownVariable`](https://pkg.go.dev/go.dw1.io/fasteval#ErrUnknownVariable) | Neither the direct variables map nor the resolver supplied a name. |
| [`ErrType`](https://pkg.go.dev/go.dw1.io/fasteval#ErrType) | An operator, argument, conversion, or result used an incompatible type. |
| [`ErrOverflow`](https://pkg.go.dev/go.dw1.io/fasteval#ErrOverflow) | Checked arithmetic or literal materialization exceeded its result type. |
| [`ErrDivisionByZero`](https://pkg.go.dev/go.dw1.io/fasteval#ErrDivisionByZero) | Integer division or remainder used zero. |
| [`ErrAccess`](https://pkg.go.dev/go.dw1.io/fasteval#ErrAccess) | Field, method, map, or index access failed. |
| [`ErrFunction`](https://pkg.go.dev/go.dw1.io/fasteval#ErrFunction) | A function could not be prepared or invoked, had invalid arity, or returned an error. |
| [`ErrPanic`](https://pkg.go.dev/go.dw1.io/fasteval#ErrPanic) | A caller-controlled extension point panicked and the evaluator recovered it. |
| [`ErrCanceled`](https://pkg.go.dev/go.dw1.io/fasteval#ErrCanceled) | The evaluation context was canceled or reached its deadline. |
| [`ErrFormat`](https://pkg.go.dev/go.dw1.io/fasteval#ErrFormat) | A template value could not be formatted without explicit conversion. |

The code is the stable classification API. Full error text includes operational
detail and is not a wire-format contract.

## Source spans

[`Span`](https://pkg.go.dev/go.dw1.io/fasteval#Span) identifies a half-open byte
range
[`[Span.Start, Span.End)`](https://pkg.go.dev/go.dw1.io/fasteval#Span) in the
original source. [`Span.Line`](https://pkg.go.dev/go.dw1.io/fasteval#Span) and
[`Span.Column`](https://pkg.go.dev/go.dw1.io/fasteval#Span) are one-based.
Columns count bytes from the start of the line, matching the parser's byte
offsets.

An empty or unavailable span has zero values. Template compilation and
evaluation shift expression-local spans so they refer to the complete template
source.

## Panics and caller code

The evaluator recovers panics from:

- registered functions;
- Go function values supplied through variables or a resolver;
- exported methods;
- resolvers;
- builtins;
- [`encoding.TextMarshaler`](https://pkg.go.dev/encoding#TextMarshaler)
  implementations used by templates.

Recovered panics become
[`ErrPanic`](https://pkg.go.dev/go.dw1.io/fasteval#ErrPanic). The panic does not
cross the evaluation API, but any earlier side effects remain. Expression
authors must still be trusted.

## Partial template output

Template rendering is not atomic.
[`Template.Execute`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Execute)
writes segments as they succeed.
[`Template.Render`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Render)
collects them in a string builder. If a later segment fails, both methods return
the completed prefix with the error.

```go
template, err := fasteval.CompileTemplate("Hello {{name}}! {{1 / zero}}")
if err != nil {
	return err
}

rendered, err := template.Render(ctx, map[string]any{
	"name": "Ada",
	"zero": 0,
})
// rendered is "Hello Ada! " and err has code ErrDivisionByZero.
```

For [`Template.Execute`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Execute),
the returned byte count includes bytes accepted by the writer before its error.
A writer that reports an invalid count or makes no progress causes a write
error.

## Read-only AST

[`Expression.AST`](https://pkg.go.dev/go.dw1.io/fasteval#Expression.AST) returns
the immutable parsed tree. This tree reflects the source, while the evaluator
uses a separate prepared plan. Constant folding and other preparation never
rewrite the public tree.

```go
expression, err := fasteval.Compile("price * quantity")
if err != nil {
	return err
}

root := expression.AST()
fmt.Println(root.Kind(), root.Text(), root.Span())
for _, child := range root.Children() {
	fmt.Println(child.Kind(), child.Text(), child.Span())
}
```

[`Node`](https://pkg.go.dev/go.dw1.io/fasteval#Node) exposes:

- [`Node.Kind`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Kind)
- [`Node.Text`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Text)
- [`Node.Span`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Span)
- [`Node.Children`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Children)

[`Node.Children`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Children) returns a
copy of the slice. Its child nodes are shared but immutable. Literal runtime
values and optional-access markers are internal and do not appear in the public
AST.

## Node kinds

| Kind | Source concept |
| --- | --- |
| [`NodeInvalid`](https://pkg.go.dev/go.dw1.io/fasteval#NodeInvalid) | A nil or unavailable node. |
| [`NodeLiteral`](https://pkg.go.dev/go.dw1.io/fasteval#NodeLiteral) | Numeric, string, Boolean, or nil literal. |
| [`NodeIdentifier`](https://pkg.go.dev/go.dw1.io/fasteval#NodeIdentifier) | Variable or function name. |
| [`NodeUnary`](https://pkg.go.dev/go.dw1.io/fasteval#NodeUnary) | Prefix operator. |
| [`NodeBinary`](https://pkg.go.dev/go.dw1.io/fasteval#NodeBinary) | Binary operator. |
| [`NodeConditional`](https://pkg.go.dev/go.dw1.io/fasteval#NodeConditional) | Conditional expression. |
| [`NodeCall`](https://pkg.go.dev/go.dw1.io/fasteval#NodeCall) | Function or method call. |
| [`NodeAccess`](https://pkg.go.dev/go.dw1.io/fasteval#NodeAccess) | Field or map-dot access. |
| [`NodeIndex`](https://pkg.go.dev/go.dw1.io/fasteval#NodeIndex) | Array, slice, string, or map index. |
| [`NodeList`](https://pkg.go.dev/go.dw1.io/fasteval#NodeList) | Bracket or parenthesized list. |
| [`NodeMap`](https://pkg.go.dev/go.dw1.io/fasteval#NodeMap) | Map literal. |

[`Node.Text`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Text) contains the
source-associated operator, identifier, literal text, or internal category text
for the node. Use
[`Node.Kind`](https://pkg.go.dev/go.dw1.io/fasteval#Node.Kind) to interpret it
instead of depending on undocumented text.

[`Program[T]`](https://pkg.go.dev/go.dw1.io/fasteval#Program) and
[`Template`](https://pkg.go.dev/go.dw1.io/fasteval#Template) do not expose AST
accessors. Compile an
[`Expression`](https://pkg.go.dev/go.dw1.io/fasteval#Expression) when tooling
needs to inspect expression syntax before evaluation.
