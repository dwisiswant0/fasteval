# Language and template reference

[`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) uses the
[`govaluate`](https://pkg.go.dev/github.com/casbin/govaluate) operator surface,
then adds native Go values and the extensions documented here.

## Compile and evaluate

[`Compile`](https://pkg.go.dev/go.dw1.io/fasteval#Compile) uses the default
builtin-only compiler and returns an
[`Expression`](https://pkg.go.dev/go.dw1.io/fasteval#Expression). Evaluate it
with [`Expression.Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Expression.Eval):

```go
expression, err := fasteval.Compile("price * quantity >= 100")
value, err := expression.Eval(ctx, map[string]any{
	"price":    25,
	"quantity": 4,
})
```

Use [`NewCompiler`](https://pkg.go.dev/go.dw1.io/fasteval#NewCompiler) when an
expression or template needs custom functions. A compiler, expression, typed
program, or template becomes immutable after construction and is safe for
concurrent use. The caller still owns variable maps, resolver state, function
state, and output writers, and must synchronize mutable values.

[`Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Eval) and
[`EvalAs`](https://pkg.go.dev/go.dw1.io/fasteval#EvalAs) compile on every call.
They suit one-off evaluations. For repeated work, compile once and keep the
returned object. The package does not keep a caller-visible compile cache.

[`CompileAs[T]`](https://pkg.go.dev/go.dw1.io/fasteval#CompileAs) returns a
[`Program[T]`](https://pkg.go.dev/go.dw1.io/fasteval#Program), which evaluates
through [`Program.Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Program.Eval).

Unknown variables are errors by default.
[`WithResolver`](https://pkg.go.dev/go.dw1.io/fasteval#WithResolver) runs only
after a direct variables-map miss. Its `found` result distinguishes an explicit
`nil` value from an unresolved name:

```go
value, err := expression.Eval(ctx, variables,
	fasteval.WithResolver(func(ctx context.Context, name string) (any, bool, error) {
		value, found := loadValue(ctx, name)
		return value, found, nil
	}),
)
```

A nil [`context.Context`](https://pkg.go.dev/context#Context) is treated as
[`context.Background()`](https://pkg.go.dev/context#Background).

## Lexical syntax

Whitespace is allowed between tokens.

Identifiers start with a Unicode letter, `_`, or an escaped rune. Later runes
may also be Unicode digits. A backslash includes the next rune in the name, so
`response\-time` refers to `response-time`. The compatible bracket form
`[response-time]` does the same when the brackets do not form a list.

Reserved words are:

- `true` and `false`
- `nil`
- `in` and `IN`, which both produce the `in` operator

Strings may use single or double quotes, with Go-style character escapes.
Numeric literals use the integer, floating-point, hexadecimal, binary, octal,
exponent, underscore, and imaginary forms supported by
[`go/constant`](https://pkg.go.dev/go/constant).

Examples:

```text
42
0xff
1_000_000
.5
0x1.8p+1
2i
'line\ntext'
"quoted"
```

## Precedence and associativity

Precedence runs from highest to lowest:

| Level | Forms |
| --- | --- |
| Postfix | calls `f(...)`, access `a.b`, optional access `a?.b`, index `a[i]`, optional index `a?[i]` |
| Prefix | `+a`, `-a`, `!a`, `~a` |
| Exponent | `**` |
| Multiplicative | `*`, `/`, `%` |
| Additive | `+`, `-` |
| Shift | `<<`, `>>` |
| Bitwise | `&`, `|`, `^` |
| Comparison | `==`, `!=`, `>`, `>=`, `<`, `<=`, `=~`, `!~`, `in` |
| Logical AND | `&&` |
| Logical OR | `||` |
| Coalescing and conditional | `??`, `condition ? whenTrue : whenFalse` |

Binary operators associate to the left. Parentheses override precedence. The
false branch of a conditional is optional; `condition ? value` returns `nil`
when the condition is false.

`&&` and `||` require Boolean operands and short-circuit. `??` evaluates the
right operand only when the left operand is nil or a typed nil. Conditional
conditions must be Boolean; other values are not coerced to truth values.

## Numeric values

All standard Go integer, unsigned integer, floating-point, and complex types are
supported. `float` is an alias for
[`float64`](https://pkg.go.dev/builtin#float64) in the builtin conversion set.
[`time.Duration`](https://pkg.go.dev/time#Duration) keeps its distinct time
semantics.

Source literals stay exact until an operation, function parameter, map key, or
result boundary assigns a concrete type. An untyped integer result must fit the
platform [`int`](https://pkg.go.dev/builtin#int), a floating-point result must
fit [`float64`](https://pkg.go.dev/builtin#float64), and an imaginary or complex
result must fit [`complex128`](https://pkg.go.dev/builtin#complex128).

Arithmetic does not widen mixed concrete types automatically. Convert one
operand explicitly instead. The value must be exactly representable in the
destination type.

Integer arithmetic checks overflow, invalid shifts, negative integer powers,
and division or remainder by zero. Floating-point and complex operations use Go
semantics. Complex values support equality and arithmetic but cannot be ordered.

Defined numeric types keep their identity while an expression still needs a
method or exact map key. Access, indexing, selection, and collection transforms
carry the defined value through unchanged. Numeric consumers and untyped final
results normalize it to the underlying built-in type. Both expressions below
are therefore valid:

```text
values[userID].Name()
filter(ids, active)[0].String()
```

The final result of a plain [`Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Eval)
call contains normalized collection elements. Use
[`CompileAs[T]`](https://pkg.go.dev/go.dw1.io/fasteval#CompileAs) or
[`EvalAs[T]`](https://pkg.go.dev/go.dw1.io/fasteval#EvalAs) when the caller
requires a specific result shape.

## Equality, ordering, and membership

`==` and `!=` work with scalars, arrays, slices, maps, structs, pointers,
interfaces, and cyclic collections. Collections use deep structural equality.

Numeric values with different concrete types compare equal only when their
values are exactly equal. NaN is never equal to any value, including itself.
[`time.Duration`](https://pkg.go.dev/time#Duration) is not equal to a plain
integer of the same magnitude. Nil and typed-nil values compare as logical nil.

Ordering supports real numeric values, strings,
[`time.Time`](https://pkg.go.dev/time#Time), and
[`time.Duration`](https://pkg.go.dev/time#Duration). Operands must be
compatible. Complex values cannot be ordered.

With an array or slice on the right, `in` searches the elements using expression
equality. With a map, it searches the keys. Direct map indexing is stricter and
uses exact Go key identity. An `int8(1)` can therefore be `in` a map with an
`int64(1)` key without being able to index that map directly.

`=~` and `!~` require a string on the left and a Go regular-expression pattern
string on the right. A constant pattern is compiled with the expression. A
dynamic pattern is compiled during evaluation.

## Lists and maps

Lists use brackets:

```text
[]
[1, 2, 3]
[value,]
```

Because `[name]` remains compatible with an escaped variable name, a one-item
list needs a trailing comma. A parenthesized comma list such as `(1, 2)` also
produces a list.

Maps use evaluated keys and values:

```text
{}
{'name': user.name, user.id: user}
```

Map keys may use any Go-comparable type. Logical nil becomes `nil`, and duplicate
evaluated keys are errors. Iteration order is unspecified, including for
`keys`, `values`, `toPairs`, and `groupBy`.

Outward dynamic list and map literals use
[`[]any`](https://pkg.go.dev/builtin#any) and
[`map[any]any`](https://pkg.go.dev/builtin#any). Typed programs recursively
convert them to compatible arrays, slices, maps, structs, pointers, and
interfaces without lossy numeric conversion.

## Access and calls

The language supports:

- exported struct fields and exported methods;
- map access by index and string-key dot access;
- array and slice indexing;
- UTF-8 byte indexing for strings;
- optional field or map-dot access with `?.`;
- optional indexing with `?[`.

Normal access reports missing fields, keys, invalid indexes, and nil receivers.
Optional access handles only a nil receiver; missing members and out-of-range
indexes remain errors. Use `get(value, key, fallback)` when either case should
return a fallback.

A call target can be:

- a shipped builtin;
- a function registered with
  [`WithFunction`](https://pkg.go.dev/go.dw1.io/fasteval#WithFunction) or
  [`WithFunctions`](https://pkg.go.dev/go.dw1.io/fasteval#WithFunctions);
- an exported Go method reached through access;
- a Go function value supplied by the variables map or resolver.

An ordinary Go function may be fixed or variadic. It returns a value, optionally
followed by an [`error`](https://pkg.go.dev/builtin#error). A leading
[`context.Context`](https://pkg.go.dev/context#Context) is injected and does not
count toward expression arity. Arguments are converted recursively to the Go
parameter types without loss. A non-nil returned error becomes an
[`ErrFunction`](https://pkg.go.dev/go.dw1.io/fasteval#ErrFunction) evaluation
error. Panics are recovered as
[`ErrPanic`](https://pkg.go.dev/go.dw1.io/fasteval#ErrPanic).

Builtin names are reserved during compiler construction. In a call or direct
higher-order argument, a registered function name takes precedence over a data
variable with the same name. The variable remains available elsewhere.

Higher-order builtins accept registered function references or Go function
values. Predicates must return [`bool`](https://pkg.go.dev/builtin#bool).
Lambdas, closures, assignment, loops, and custom operator syntax are not part of
the language.

## Time values

Supply [`time.Time`](https://pkg.go.dev/time#Time) and
[`time.Duration`](https://pkg.go.dev/time#Duration) as Go values, or create them
with the `date`, `duration`, `timezone`, and `now` builtins.

Supported time arithmetic is:

- [`time.Time`](https://pkg.go.dev/time#Time) +
  [`time.Duration`](https://pkg.go.dev/time#Duration)
- [`time.Duration`](https://pkg.go.dev/time#Duration) +
  [`time.Time`](https://pkg.go.dev/time#Time)
- [`time.Time`](https://pkg.go.dev/time#Time) -
  [`time.Duration`](https://pkg.go.dev/time#Duration)
- [`time.Time`](https://pkg.go.dev/time#Time) -
  [`time.Time`](https://pkg.go.dev/time#Time), which returns
  [`time.Duration`](https://pkg.go.dev/time#Duration)
- checked [`time.Duration`](https://pkg.go.dev/time#Duration) +
  [`time.Duration`](https://pkg.go.dev/time#Duration)
- checked [`time.Duration`](https://pkg.go.dev/time#Duration) -
  [`time.Duration`](https://pkg.go.dev/time#Duration)

Time values of the same type support equality and ordering.

## Templates

Every template tag is an expression. Plain identifiers use a specialized lookup
and formatting path.

```go
template, err := compiler.CompileTemplate(
	"Hello {{greet(name)}}",
)
```

The package-level
[`CompileTemplate`](https://pkg.go.dev/go.dw1.io/fasteval#CompileTemplate) uses
the default builtin-only compiler. Use
[`Compiler.CompileTemplate`](https://pkg.go.dev/go.dw1.io/fasteval#Compiler.CompileTemplate)
for registered custom functions. Templates may also call Go function values
from variables and resolvers, so template authors must be trusted.

The default delimiters are `{{` and `}}`.
[`WithDelimiters`](https://pkg.go.dev/go.dw1.io/fasteval#WithDelimiters) accepts
any distinct, non-empty pair. A delimiter inside a quoted string or nested
expression does not close the tag. Nested template tags are not supported.

Template formatting is deterministic:

- `nil` renders as an empty string;
- strings and [`[]byte`](https://pkg.go.dev/builtin#byte) render as text;
- Boolean, integer, floating-point, and complex values use
  [`strconv`](https://pkg.go.dev/strconv) formatting;
- [`encoding.TextMarshaler`](https://pkg.go.dev/encoding#TextMarshaler) values
  use
  [`encoding.TextMarshaler.MarshalText`](https://pkg.go.dev/encoding#TextMarshaler);
- other values return
  [`ErrFormat`](https://pkg.go.dev/go.dw1.io/fasteval#ErrFormat) and require
  explicit conversion such as
  `string(value)` or `toJSON(value)`.

Output is raw. The package does not encode HTML, SQL, shell, URL, or any other
output context. Encode untrusted values for the destination before rendering or
before using the rendered text.

[`Template.Execute`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Execute)
writes incrementally and returns the number of successfully written bytes.
[`Template.Render`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Render)
accumulates a string. If evaluation, formatting, or writing fails later, both
methods return the completed prefix with the error.

## Cancellation and fixed limits

Evaluation checks [`context.Context`](https://pkg.go.dev/context#Context) at
expression and operation boundaries and while processing collections.
Registered functions and methods that block or run for a long time must honor
the injected context themselves.

The evaluator enforces three fixed structural limits:

- expression nesting: 1,024 levels;
- recursively processed values: 1,024 levels for conversion, equality, and JSON
  traversal;
- exact constant-folding results: 1,048,576 bits.

There are no configurable budgets for steps, collection size, allocation, or
total work. Cancellation cannot reliably interrupt an allocation already in
progress. Trust expression authors and bound input sizes where memory exhaustion
is a concern.
