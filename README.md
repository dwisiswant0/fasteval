# fasteval

[![Go Reference](https://pkg.go.dev/badge/go.dw1.io/fasteval.svg)](https://pkg.go.dev/go.dw1.io/fasteval)

[`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) evaluates expressions and
renders raw string templates in Go. It supports native Go numeric types, typed
generic results, collection values, ordinary Go functions, and a fast path for
plain template identifiers.

The module requires Go 1.26.0 or newer.

```bash
go get go.dw1.io/fasteval
```

## Documentation

- [Language and template reference](docs/language.md)
- [Builtin function reference](docs/builtins.md)
- [Errors and AST inspection](docs/errors.md)
- [Performance and benchmark methodology](docs/performance.md)
- [Contributing](docs/contributing.md)

## Expressions

Call [`Compile`](https://pkg.go.dev/go.dw1.io/fasteval#Compile) once, then use
[`Expression.Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Expression.Eval)
concurrently:

```go
expression, err := fasteval.Compile("requests >= 100 && success / requests >= 0.9")
if err != nil {
	return err
}

value, err := expression.Eval(ctx, map[string]any{
	"requests": 100.0,
	"success":  95.0,
})
```

Use [`CompileAs[T]`](https://pkg.go.dev/go.dw1.io/fasteval#CompileAs) when the
result shape is known, then evaluate the returned program with
[`Program.Eval`](https://pkg.go.dev/go.dw1.io/fasteval#Program.Eval):

```go
type Result struct {
	Name   string `json:"name"`
	Scores []int8 `json:"scores"`
}

program, err := fasteval.CompileAs[Result](
	"{'name': name, 'scores': [1, 2, 3]}",
)
result, err := program.Eval(ctx, map[string]any{"name": "Ada"})
```

Typed conversion is recursive and lossless. It rejects integer truncation,
floating-point precision loss, and implicit string-to-number conversion.
Explicit numeric conversion functions can parse strings.

## Functions and lazy variables

Registered functions can be fixed or variadic. They return a value, optionally
followed by an [`error`](https://pkg.go.dev/builtin#error). A leading
[`context.Context`](https://pkg.go.dev/context#Context) is injected and does not
count toward the expression's arity.

Create a [`Compiler`](https://pkg.go.dev/go.dw1.io/fasteval#Compiler) with
[`NewCompiler`](https://pkg.go.dev/go.dw1.io/fasteval#NewCompiler) and register
functions with
[`WithFunction`](https://pkg.go.dev/go.dw1.io/fasteval#WithFunction):

```go
compiler, err := fasteval.NewCompiler(
	fasteval.WithFunction("isAdult", func(age int) bool { return age >= 18 }),
)
expression, err := compiler.Compile("filter(ages, isAdult)")
```

Expressions can also call exported methods and Go function values from the
variables map or resolver. Templates with custom functions must use
[`Compiler.CompileTemplate`](https://pkg.go.dev/go.dw1.io/fasteval#Compiler.CompileTemplate);
package-level
[`CompileTemplate`](https://pkg.go.dev/go.dw1.io/fasteval#CompileTemplate) uses
the default builtin-only compiler.

[`WithResolver`](https://pkg.go.dev/go.dw1.io/fasteval#WithResolver) supplies a
lazy fallback after direct map lookup. Its `found` result distinguishes an
explicit nil value from an unknown variable.

Builtin names are reserved and cannot be registered again. Function calls and
higher-order arguments resolve through a separate callable namespace, while a
data variable with the same name remains available elsewhere. Compilers and
compiled artifacts are immutable and safe for concurrent use. The package has
no hidden compile cache or mutable global function registry.

## Templates

Every template tag contains an expression. Plain identifiers use a direct
lookup and formatting path.

```go
template, err := fasteval.CompileTemplate(
	"Hello {{name}}: {{score >= 80 ? 'pass' : 'fail'}}",
)
rendered, err := template.Render(ctx, map[string]any{
	"name":  "Ada",
	"score": 90,
})
```

The default delimiters are `{{` and `}}`. Use
[`WithDelimiters`](https://pkg.go.dev/go.dw1.io/fasteval#WithDelimiters) to set
any distinct, non-empty pair. An end delimiter inside a quoted string or nested
expression construct does not close the tag.

[`Template.Execute`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Execute)
streams to an [`io.Writer`](https://pkg.go.dev/io#Writer).
[`Template.Render`](https://pkg.go.dev/go.dw1.io/fasteval#Template.Render)
returns a string. If an expression, formatter, or write fails, both methods
return the output completed before the error.

Template output is raw. The package does not know whether the destination is
HTML, SQL, a shell command, or another interpreter. Apply the correct
context-specific encoder before using untrusted values in those contexts.

## Language

The language provides these operator groups:

- arithmetic: `+`, `-`, `*`, `/`, `%`, and `**`
- bitwise: `&`, `|`, `^`, `~`, `<<`, and `>>`
- comparison: `==`, `!=`, `>`, `>=`, `<`, `<=`, `=~`, `!~`, and `in`
- logical and conditional: `!`, `&&`, `||`, `??`, and `? :`

Values follow these rules:

- Standard Go numeric types are preserved. Defined numeric types normalize to
  their underlying type at numeric-consumer and outward-result boundaries,
  except [`time.Duration`](https://pkg.go.dev/time#Duration). Transparent access
  and collection paths preserve the defined type while methods or exact map
  keys still need it.
- Untyped Go-like literals adapt to their operand, function parameter, map key,
  or typed-result context.
- Concrete mixed-type arithmetic requires explicit conversion. Integer
  arithmetic is checked for overflow and division errors.
- Numeric equality compares different numeric types only when the comparison
  is exact. NaN is never equal. Complex values cannot be ordered.
- Lists use `[a, b]`. Because `[escaped variable]` remains compatible, a
  singleton list requires a trailing comma: `[value,]`.
- Maps use `{key: value}` and accept any Go-comparable key. Map lookup uses
  exact Go key identity. Map iteration order is unspecified.
- Strings are indexed as UTF-8 bytes. Slicing and range syntax are not part of
  the language.
- Field access, map-dot access, exported methods, indexing, `?.`, and `?[` are
  supported. Optional access suppresses only a nil receiver; missing members
  and out-of-range indexes remain errors.
- Only `nil` is a null literal. Dates and durations use functions rather than
  guessed string-literal formats.

See the [language reference](docs/language.md) for literal syntax, exact
precedence, access and call behavior, result normalization, template formatting,
and fixed safety limits.

## Builtins

All shipped builtins are enabled:

- collections: `all`, `any`, `one`, `none`, `map`, `filter`, `find`,
  `findIndex`, `findLast`, `findLastIndex`, `groupBy`, `count`, `concat`,
  `flatten`, `uniq`, `join`, `reduce`, `sum`, `mean`, `median`, `first`,
  `last`, `take`, `reverse`, `sort`, and `sortBy`
- maps and pairs: `keys`, `values`, `toPairs`, and `fromPairs`
- strings: `trim`, `trimPrefix`, `trimSuffix`, `upper`, `lower`, `split`,
  `splitAfter`, `replace`, `repeat`, `indexOf`, `lastIndexOf`, `hasPrefix`,
  and `hasSuffix`
- numeric: `min`, `max`, `abs`, `ceil`, `floor`, `round`, every standard Go
  numeric conversion, `real`, `imag`, `complex`, and `conj`
- bitwise: `bitand`, `bitor`, `bitxor`, `bitnand`, `bitnot`, `bitshl`,
  `bitshr`, and `bitushr`
- time: `now`, `duration`, `date`, and `timezone`
- conversion and encoding: `type`, `string`, `toJSON`, `fromJSON`,
  `toBase64`, and `fromBase64`
- general access: `len` and `get`

Higher-order functions accept either a registered function name or a Go
function value. Inline predicates and closures are not supported.

See the [builtin reference](docs/builtins.md) for signatures, accepted inputs,
return values, and edge-case behavior.

## Benchmarks

This recorded run is generated in CI with the five-engine comparison set.

<details>
  <summary><code>benchstat</code></summary>

  ```
  goos: linux
  goarch: amd64
  pkg: benchmarks
  cpu: AMD EPYC 7763 64-Core Processor
                       │  fasteval   │               govaluate               │                  expr                  │                  cel                   │                 gval                  │
                       │   sec/op    │    sec/op     vs base                 │    sec/op     vs base                  │    sec/op     vs base                  │    sec/op     vs base                 │
  Compile/Arithmetic-4   2.641µ ± 4%    2.182µ ± 5%   -17.38% (p=0.000 n=10)   14.395µ ± 3%   +445.04% (p=0.000 n=10)   37.546µ ± 4%  +1321.66% (p=0.000 n=10)    2.281µ ± 1%   -13.65% (p=0.000 n=10)
  Compile/Variables-4    1.351µ ± 0%    2.099µ ± 1%   +55.39% (p=0.000 n=10)   15.877µ ± 2%  +1075.64% (p=0.000 n=10)   33.836µ ± 3%  +2405.44% (p=0.000 n=10)    2.937µ ± 3%  +117.44% (p=0.000 n=10)
  Compile/Boolean-4      2.387µ ± 3%    3.483µ ± 0%   +45.92% (p=0.000 n=10)   17.383µ ± 1%   +628.24% (p=0.000 n=10)   53.443µ ± 2%  +2138.92% (p=0.000 n=10)    4.485µ ± 1%   +87.89% (p=0.000 n=10)
  Compile/String-4       1.307µ ± 0%    2.709µ ± 1%  +107.23% (p=0.000 n=10)   16.235µ ± 1%  +1142.16% (p=0.000 n=10)   37.164µ ± 2%  +2743.46% (p=0.000 n=10)    4.142µ ± 1%  +216.91% (p=0.000 n=10)
  Eval/Arithmetic-4      5.971n ± 0%   18.075n ± 0%  +202.74% (p=0.000 n=10)   46.830n ± 1%   +684.36% (p=0.000 n=10)   95.535n ± 0%  +1500.12% (p=0.000 n=10)    2.822n ± 0%   -52.74% (p=0.000 n=10)
  Eval/Variables-4       91.01n ± 1%   130.65n ± 0%   +43.56% (p=0.000 n=10)   108.85n ± 1%    +19.60% (p=0.000 n=10)   197.65n ± 1%   +117.17% (p=0.000 n=10)   234.50n ± 0%  +157.66% (p=0.000 n=10)
  Eval/Boolean-4         142.3n ± 1%    212.3n ± 0%   +49.19% (p=0.000 n=10)    133.3n ± 3%     -6.32% (p=0.000 n=10)    263.6n ± 0%    +85.28% (p=0.000 n=10)    354.1n ± 1%  +148.84% (p=0.000 n=10)
  Eval/String-4          127.5n ± 1%    237.1n ± 0%   +85.89% (p=0.000 n=10)    130.1n ± 1%     +2.00% (p=0.000 n=10)    279.3n ± 2%   +118.97% (p=0.000 n=10)   1277.5n ± 1%  +901.57% (p=0.000 n=10)
  geomean                319.9n         517.4n        +61.72%                   1.243µ        +288.56%                   2.774µ        +767.15%                   662.8n       +107.17%

                       │    fasteval    │               govaluate                │                  expr                   │                    cel                    │                  gval                   │
                       │      B/op      │     B/op      vs base                  │     B/op       vs base                  │     B/op       vs base                    │     B/op      vs base                   │
  Compile/Arithmetic-4    1288.0 ± 0%       976.0 ± 0%  -24.22% (p=0.000 n=10)       9368.0 ± 0%   +627.33% (p=0.000 n=10)    16100.0 ± 0%  +1150.00% (p=0.000 n=10)      1960.0 ± 0%   +52.17% (p=0.000 n=10)
  Compile/Variables-4      696.0 ± 0%       768.0 ± 0%  +10.34% (p=0.000 n=10)      10448.0 ± 0%  +1401.15% (p=0.000 n=10)    15387.0 ± 0%  +2110.78% (p=0.000 n=10)      2224.0 ± 0%  +219.54% (p=0.000 n=10)
  Compile/Boolean-4      1.328Ki ± 0%     1.273Ki ± 0%   -4.12% (p=0.000 n=10)     11.023Ki ± 0%   +730.00% (p=0.000 n=10)   23.620Ki ± 0%  +1678.46% (p=0.000 n=10)     2.648Ki ± 0%   +99.41% (p=0.000 n=10)
  Compile/String-4         816.0 ± 0%      1048.0 ± 0%  +28.43% (p=0.000 n=10)      11192.0 ± 0%  +1271.57% (p=0.000 n=10)    18082.0 ± 0%  +2115.93% (p=0.000 n=10)      2800.0 ± 0%  +243.14% (p=0.000 n=10)
  Eval/Arithmetic-4         0.00 ± 0%        0.00 ± 0%        ~ (p=1.000 n=10) ¹      32.00 ± 0%          ? (p=0.000 n=10)       0.00 ± 0%          ~ (p=1.000 n=10) ¹      0.00 ± 0%         ~ (p=1.000 n=10) ¹
  Eval/Variables-4         8.000 ± 0%       8.000 ± 0%        ~ (p=1.000 n=10) ¹     40.000 ± 0%   +400.00% (p=0.000 n=10)     24.000 ± 0%   +200.00% (p=0.000 n=10)      56.000 ± 0%  +600.00% (p=0.000 n=10)
  Eval/Boolean-4           8.000 ± 0%       8.000 ± 0%        ~ (p=1.000 n=10) ¹     40.000 ± 0%   +400.00% (p=0.000 n=10)     32.000 ± 0%   +300.00% (p=0.000 n=10)      80.000 ± 0%  +900.00% (p=0.000 n=10)
  Eval/String-4             0.00 ± 0%        0.00 ± 0%        ~ (p=1.000 n=10) ¹      32.00 ± 0%          ? (p=0.000 n=10)      64.00 ± 0%          ? (p=0.000 n=10)      528.00 ± 0%         ? (p=0.000 n=10)
  geomean                             ²                  +0.37%                ²      614.2       ?                                         ?                        ²                 ?                       ²
  ¹ all samples are equal
  ² summaries must be >0 to compute geomean

                       │   fasteval   │               govaluate                │                 expr                  │                    cel                    │                  gval                   │
                       │  allocs/op   │  allocs/op   vs base                   │  allocs/op   vs base                  │  allocs/op    vs base                     │  allocs/op   vs base                    │
  Compile/Arithmetic-4   21.00 ± 0%      23.00 ± 0%    +9.52% (p=0.000 n=10)      47.00 ± 0%   +123.81% (p=0.000 n=10)    310.00 ± 0%   +1376.19% (p=0.000 n=10)      23.00 ± 0%     +9.52% (p=0.000 n=10)
  Compile/Variables-4    6.000 ± 0%     17.000 ± 0%  +183.33% (p=0.000 n=10)     63.000 ± 0%   +950.00% (p=0.000 n=10)   289.000 ± 0%   +4716.67% (p=0.000 n=10)     35.000 ± 0%   +483.33% (p=0.000 n=10)
  Compile/Boolean-4      10.00 ± 0%      29.00 ± 0%  +190.00% (p=0.000 n=10)      76.00 ± 0%   +660.00% (p=0.000 n=10)    452.00 ± 0%   +4420.00% (p=0.000 n=10)      57.00 ± 0%   +470.00% (p=0.000 n=10)
  Compile/String-4       3.000 ± 0%     23.000 ± 0%  +666.67% (p=0.000 n=10)     75.000 ± 0%  +2400.00% (p=0.000 n=10)   333.000 ± 0%  +11000.00% (p=0.000 n=10)     59.000 ± 0%  +1866.67% (p=0.000 n=10)
  Eval/Arithmetic-4      0.000 ± 0%      0.000 ± 0%         ~ (p=1.000 n=10) ¹    1.000 ± 0%          ? (p=0.000 n=10)     0.000 ± 0%           ~ (p=1.000 n=10) ¹    0.000 ± 0%          ~ (p=1.000 n=10) ¹
  Eval/Variables-4       1.000 ± 0%      1.000 ± 0%         ~ (p=1.000 n=10) ¹    2.000 ± 0%   +100.00% (p=0.000 n=10)     3.000 ± 0%    +200.00% (p=0.000 n=10)      5.000 ± 0%   +400.00% (p=0.000 n=10)
  Eval/Boolean-4         1.000 ± 0%      1.000 ± 0%         ~ (p=1.000 n=10) ¹    2.000 ± 0%   +100.00% (p=0.000 n=10)     4.000 ± 0%    +300.00% (p=0.000 n=10)      7.000 ± 0%   +600.00% (p=0.000 n=10)
  Eval/String-4          0.000 ± 0%      0.000 ± 0%         ~ (p=1.000 n=10) ¹    1.000 ± 0%          ? (p=0.000 n=10)     4.000 ± 0%           ? (p=0.000 n=10)     24.000 ± 0%          ? (p=0.000 n=10)
  geomean                           ²                 +69.77%                ²    9.521       ?                                        ?                         ²                ?                        ²
  ¹ all samples are equal
  ² summaries must be >0 to compute geomean
  ```
</details>

### Compile

* Execution Time (ns/op)

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/50b7398c-ddbe-44b2-89e0-5357ec379115" /></a>

* Memory Usage (B/op)

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/1f176b68-9f6f-403d-814f-df2faddd1bb0" /></a>

* Allocations/op

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/2b97c2f3-1baf-4a42-a47e-1596af25b6d8" /></a>

* Iterations

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/72688a4f-d158-49ec-8840-497104696f59" /></a>

### Eval

* Execution Time (ns/op)

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/2609e167-9387-4ce9-8a67-d98d367f2abd" /></a>

* Memory Usage (B/op)

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/6816762b-4a0c-44f9-9de6-5f38c8d292aa" /></a>

* Allocations/op

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/21e8b65b-bc9a-4e25-9d56-2aa1aa11c6ab" /></a>

* Iterations

  <a href="#"><img width="830" alt="Image" src="https://github.com/user-attachments/assets/1243fb8e-e571-4486-89b3-29a7035b35b3" /></a>

#### Highlights

- **Across these eight workloads:**
  [`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) has the lowest time
  geomean at 319.9 ns. The nearest result is
  [`govaluate`](https://pkg.go.dev/github.com/casbin/govaluate) at 517.4 ns,
  61.72% above the baseline.
- **Compilation:** [`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) leads the
  Variables, Boolean, and String workloads.
  [`govaluate`](https://pkg.go.dev/github.com/casbin/govaluate) and
  [`gval`](https://pkg.go.dev/github.com/PaesslerAG/gval) lead Arithmetic by
  17.38% and 13.65%, respectively.
- **Evaluation:** [`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) leads
  [`govaluate`](https://pkg.go.dev/github.com/casbin/govaluate) in all four
  workloads. [`gval`](https://pkg.go.dev/github.com/PaesslerAG/gval) leads
  Arithmetic, [`expr`](https://pkg.go.dev/github.com/expr-lang/expr) leads
  Boolean by 6.32%, and
  [`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) leads Variables and String.
- **Memory and allocations:**
  [`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) has the fewest compile
  allocations in every workload and matches
  [`govaluate`](https://pkg.go.dev/github.com/casbin/govaluate)'s evaluation
  allocation counts. It uses the least compile memory for Variables and String;
  [`govaluate`](https://pkg.go.dev/github.com/casbin/govaluate) uses less for
  Arithmetic and Boolean.

These figures are a machine-specific snapshot, not a performance guarantee. See
the [performance guide](docs/performance.md) for the benchmark method and
reporting limits.

Run benchmarks yourself:

```bash
make -C benchmarks bench
make -C benchmarks benchstat
```

## Errors and ASTs

Expression and template compilation failures return
[`DiagnosticsError`](https://pkg.go.dev/go.dw1.io/fasteval#DiagnosticsError).
Evaluation failures return
[`EvalError`](https://pkg.go.dev/go.dw1.io/fasteval#EvalError) with a stable
[`ErrorCode`](https://pkg.go.dev/go.dw1.io/fasteval#ErrorCode), operation,
source span, path, and wrapped cause when applicable.
[`Expression.AST`](https://pkg.go.dev/go.dw1.io/fasteval#Expression.AST) exposes
the immutable parsed tree.

See [Errors and AST inspection](docs/errors.md) for
[`errors.As`](https://pkg.go.dev/errors#As) examples, partial template failures,
error categories, and AST traversal.

## Trust and cancellation

Only trusted authors should write expressions. Exported methods, registered
functions, and Go function values from variables or resolvers can have side
effects. Panics from functions, methods, resolvers, builtins, and text
formatters become typed evaluation errors.

Expression nesting and recursively processed values have fixed depth limits of
1,024. Exact constant-folding operations have a 1,048,576-bit size limit. There
are no configurable step, collection-size, allocation, or workload budgets.
Evaluation and collection loops honor
[`context.Context`](https://pkg.go.dev/context#Context); caller functions must
also cooperate with cancellation. Context cancellation cannot reliably prevent
one large allocation from exhausting process memory.

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
