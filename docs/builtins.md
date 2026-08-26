# Builtin function reference

Every compiler includes the full builtin set. Builtin names are reserved and
cannot be replaced through
[`WithFunction`](https://pkg.go.dev/go.dw1.io/fasteval#WithFunction) or
[`WithFunctions`](https://pkg.go.dev/go.dw1.io/fasteval#WithFunctions).

## Conventions

The tables use this notation:

- [`any`](https://pkg.go.dev/builtin#any) accepts any expression value.
- `sequence` accepts a Go array or slice, including evaluator list results.
- `map` accepts a Go map, including evaluator map results.
- `fn` accepts a registered function reference or a Go function value.
- `predicate` is an `fn` that accepts one element and returns
  [`bool`](https://pkg.go.dev/builtin#bool).
- Brackets in a signature mark an optional argument.
- `...` marks one or more values where the table says so.

Function arguments are converted with range and precision checks. Each callback
argument must fit its Go parameter type exactly. Collection loops and callback
calls honor [`context.Context`](https://pkg.go.dev/context#Context)
cancellation.

Higher-order callbacks receive normalized values. Selectors and collection
transforms keep the original Go value while the surrounding expression may
still need a defined-type method or exact map key. Final dynamic results are
normalized at the package boundary.

## Predicates and transforms

| Function | Result | Behavior |
| --- | --- | --- |
| `all(sequence, predicate)` | [`bool`](https://pkg.go.dev/builtin#bool) | True when every element matches. True for an empty sequence. Stops at the first false result. |
| `any(sequence, predicate)` | [`bool`](https://pkg.go.dev/builtin#bool) | True when at least one element matches. False for an empty sequence. Stops at the first match. |
| `one(sequence, predicate)` | [`bool`](https://pkg.go.dev/builtin#bool) | True when exactly one element matches. Stops after the second match. |
| `none(sequence, predicate)` | [`bool`](https://pkg.go.dev/builtin#bool) | True when no element matches. True for an empty sequence. |
| `map(sequence, fn)` | list | Calls `fn(element)` for each element and returns the results. |
| `filter(sequence, predicate)` | list | Returns the original elements that match. |
| `find(sequence, predicate)` | [`any`](https://pkg.go.dev/builtin#any) or `nil` | Returns the first matching element, or `nil`. |
| `findIndex(sequence, predicate)` | [`int`](https://pkg.go.dev/builtin#int) | Returns the first matching zero-based index, or `-1`. |
| `findLast(sequence, predicate)` | [`any`](https://pkg.go.dev/builtin#any) or `nil` | Returns the last matching element, or `nil`. |
| `findLastIndex(sequence, predicate)` | [`int`](https://pkg.go.dev/builtin#int) | Returns the last matching zero-based index, or `-1`. |
| `groupBy(sequence, fn)` | map | Calls `fn(element)` for each group key. Keys must be Go-comparable; logical nil becomes `nil`. Values in each group keep input order. |
| `count(sequence[, predicate])` | [`int`](https://pkg.go.dev/builtin#int) | Returns the sequence length, or the number of matching elements when a predicate is supplied. |
| `reduce(sequence, fn[, initial])` | [`any`](https://pkg.go.dev/builtin#any) or `nil` | Calls `fn(accumulator, element)`. Without `initial`, the first element is the accumulator. An empty sequence without `initial` returns `nil`. |
| `sortBy(sequence, fn)` | list | Stably sorts the original elements by `fn(element)`. Keys use the same ordering rules as comparison operators. |

Higher-order functions do not support inline lambdas. Register a Go function,
then pass its name without parentheses:

```go
compiler, err := fasteval.NewCompiler(
	fasteval.WithFunction("active", func(user User) bool { return user.Active }),
)
expression, err := compiler.Compile("filter(users, active)")
```

The returned [`Compiler`](https://pkg.go.dev/go.dw1.io/fasteval#Compiler) uses
[`Compiler.Compile`](https://pkg.go.dev/go.dw1.io/fasteval#Compiler.Compile) for
expressions and
[`Compiler.CompileTemplate`](https://pkg.go.dev/go.dw1.io/fasteval#Compiler.CompileTemplate)
for templates.

In a higher-order function position, a registered name takes precedence over a
variable with the same name.

## Sequence operations

| Function | Result | Behavior |
| --- | --- | --- |
| `concat(sequence, ...)` | list | Concatenates one or more arrays or slices. |
| `flatten(sequence)` | list | Recursively flattens nested arrays and slices. Cyclic slice input is an error. |
| `uniq(sequence)` | list | Keeps the first value from each expression-equality class and preserves order. |
| `join(sequence, delimiter)` | [`string`](https://pkg.go.dev/builtin#string) | Formats each element with template formatting and joins with a string delimiter. Values that require explicit conversion return [`ErrFormat`](https://pkg.go.dev/go.dw1.io/fasteval#ErrFormat). |
| `sum(sequence)` | numeric, string, or time value | Adds elements with normal `+` semantics. An empty sequence returns [`int(0)`](https://pkg.go.dev/builtin#int). |
| `mean(sequence)` | [`float64`](https://pkg.go.dev/builtin#float64) | Returns a compensated arithmetic mean. Input must be non-empty and each number must be exactly representable as [`float64`](https://pkg.go.dev/builtin#float64). |
| `median(sequence)` | [`float64`](https://pkg.go.dev/builtin#float64) | Sorts converted numbers and returns the middle value or mean of the middle pair. Input must be non-empty and exactly convertible to [`float64`](https://pkg.go.dev/builtin#float64). NaN produces NaN. |
| `first(sequence)` | any or `nil` | Returns the first element, or `nil` for an empty sequence. |
| `last(sequence)` | any or `nil` | Returns the last element, or `nil` for an empty sequence. |
| `take(sequence, count)` | list | A non-negative count takes from the start. A negative count takes its absolute value from the end. Counts are clamped to the sequence length. |
| `reverse(sequence)` | list | Returns a reversed copy. |
| `sort(sequence)` | list | Stably sorts real numbers, strings, [`time.Time`](https://pkg.go.dev/time#Time), or [`time.Duration`](https://pkg.go.dev/time#Duration). NaN values sort before non-NaN values. Incompatible values are errors. |

Collection functions return new evaluator-owned values and leave the caller's
array, slice, or map unchanged.

## Map, pair, length, and fallback access

| Function | Result | Behavior |
| --- | --- | --- |
| `keys(map)` | list | Returns map keys in unspecified order. |
| `values(map)` | list | Returns map values in the same unspecified iteration order used for that call. |
| `toPairs(map)` | list | Returns `[key, value]` pairs in unspecified order. |
| `fromPairs(sequence)` | map | Requires each item to contain exactly two values. Keys must be Go-comparable. Duplicate evaluated keys are errors. |
| `len(value)` | [`int`](https://pkg.go.dev/builtin#int) | Supports arrays, channels, maps, slices, and strings. Nil returns zero. Strings are measured in bytes. |
| `get(value, key[, fallback])` | [`any`](https://pkg.go.dev/builtin#any) | Reads a map key, sequence or string index, or exported struct field. Missing or invalid access returns `fallback`, which defaults to `nil`. |

Normal `[]`, `.`, and `?.` access reports errors. `get` returns its fallback
instead. String indexing returns a byte value.

## Strings

String functions require concrete string arguments.

| Function | Result | Behavior |
| --- | --- | --- |
| `trim(text)` | [`string`](https://pkg.go.dev/builtin#string) | Removes leading and trailing Unicode whitespace. |
| `trimPrefix(text, prefix)` | [`string`](https://pkg.go.dev/builtin#string) | Removes one matching prefix. |
| `trimSuffix(text, suffix)` | [`string`](https://pkg.go.dev/builtin#string) | Removes one matching suffix. |
| `upper(text)` | [`string`](https://pkg.go.dev/builtin#string) | Applies Unicode upper-case mapping. |
| `lower(text)` | [`string`](https://pkg.go.dev/builtin#string) | Applies Unicode lower-case mapping. |
| `split(text, separator)` | list | Splits before each separator and returns string elements. |
| `splitAfter(text, separator)` | list | Splits after each separator and returns string elements. |
| `replace(text, old, replacement[, count])` | [`string`](https://pkg.go.dev/builtin#string) | Replaces all matches when `count` is omitted. Otherwise it uses the supplied integer count. |
| `repeat(text, count)` | [`string`](https://pkg.go.dev/builtin#string) | Repeats text a non-negative number of times. Negative or overflowing output sizes are errors. |
| `indexOf(text, substring)` | [`int`](https://pkg.go.dev/builtin#int) | Returns the first byte index, or `-1`. |
| `lastIndexOf(text, substring)` | [`int`](https://pkg.go.dev/builtin#int) | Returns the last byte index, or `-1`. |
| `hasPrefix(text, prefix)` | [`bool`](https://pkg.go.dev/builtin#bool) | Reports whether text starts with prefix. |
| `hasSuffix(text, suffix)` | [`bool`](https://pkg.go.dev/builtin#bool) | Reports whether text ends with suffix. |

`split`, `splitAfter`, `replace`, `indexOf`, and related functions follow Go
string byte-index behavior. They do not segment grapheme clusters.

## Numeric conversion and arithmetic helpers

The numeric conversion names are:

```text
int int8 int16 int32 int64
uint uint8 uint16 uint32 uint64 uintptr
float float32 float64
complex64 complex128
```

Each conversion takes one value. Strings use the corresponding
[`strconv`](https://pkg.go.dev/strconv) parser, and integer strings may include
Go base prefixes. Other values must convert exactly. `float` is an alias for
[`float64`](https://pkg.go.dev/builtin#float64).

| Function | Result | Behavior |
| --- | --- | --- |
| `min(value, ...)` or `min(sequence)` | any | Returns the least compatible value. Accepts one or more arguments; a sole array or slice is expanded. NaN is returned when encountered. |
| `max(value, ...)` or `max(sequence)` | any | Returns the greatest compatible value. Accepts one or more arguments; a sole array or slice is expanded. NaN is returned when encountered. |
| `abs(number)` | numeric | Checked absolute value for signed integers and durations; identity for unsigned integers; magnitude for real and complex numbers. Complex input returns a real result of the corresponding width. |
| `ceil(number)` | [`float64`](https://pkg.go.dev/builtin#float64) | Converts exactly to [`float64`](https://pkg.go.dev/builtin#float64), then applies [`math.Ceil`](https://pkg.go.dev/math#Ceil). |
| `floor(number)` | [`float64`](https://pkg.go.dev/builtin#float64) | Converts exactly to [`float64`](https://pkg.go.dev/builtin#float64), then applies [`math.Floor`](https://pkg.go.dev/math#Floor). |
| `round(number)` | [`float64`](https://pkg.go.dev/builtin#float64) | Converts exactly to [`float64`](https://pkg.go.dev/builtin#float64), then applies [`math.Round`](https://pkg.go.dev/math#Round). |
| `real(number)` | [`float32`](https://pkg.go.dev/builtin#float32) or [`float64`](https://pkg.go.dev/builtin#float64) | Returns the real component of [`complex64`](https://pkg.go.dev/builtin#complex64) or [`complex128`](https://pkg.go.dev/builtin#complex128). Real inputs convert to [`complex128`](https://pkg.go.dev/builtin#complex128) first. |
| `imag(number)` | [`float32`](https://pkg.go.dev/builtin#float32) or [`float64`](https://pkg.go.dev/builtin#float64) | Returns the imaginary component. |
| `complex(real, imaginary)` | [`complex64`](https://pkg.go.dev/builtin#complex64) or [`complex128`](https://pkg.go.dev/builtin#complex128) | Produces [`complex64`](https://pkg.go.dev/builtin#complex64) for compatible [`float32`](https://pkg.go.dev/builtin#float32) operands and [`complex128`](https://pkg.go.dev/builtin#complex128) otherwise. Mixing fixed [`float32`](https://pkg.go.dev/builtin#float32) and wider operands is an error. |
| `conj(number)` | complex | Returns the complex conjugate, preserving complex width. |

`min`, `max`, collection selectors, and transforms keep defined Go values intact
inside a larger expression. A plain dynamic result is normalized before it
leaves the evaluator.

## Bitwise helpers

| Function | Equivalent operation |
| --- | --- |
| `bitand(left, right)` | `left & right` |
| `bitor(left, right)` | `left | right` |
| `bitxor(left, right)` | `left ^ right` |
| `bitnand(left, right)` | `~(left & right)` |
| `bitnot(value)` | `~value` |
| `bitshl(value, count)` | `value << count` |
| `bitshr(value, count)` | `value >> count` |
| `bitushr(value, count)` | Convert both operands to [`uint64`](https://pkg.go.dev/builtin#uint64), then shift right; returns [`uint64`](https://pkg.go.dev/builtin#uint64). |

The normal integer compatibility, shift-count, and overflow rules apply.

## Time

| Function | Result | Behavior |
| --- | --- | --- |
| `now()` | [`time.Time`](https://pkg.go.dev/time#Time) | Returns [`time.Now()`](https://pkg.go.dev/time#Now) at evaluation time. |
| `duration(text)` | [`time.Duration`](https://pkg.go.dev/time#Duration) | Parses a Go duration string with [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration). |
| `date(text)` | [`time.Time`](https://pkg.go.dev/time#Time) | Accepts [`time.RFC3339Nano`](https://pkg.go.dev/time#RFC3339Nano), [`time.RFC3339`](https://pkg.go.dev/time#RFC3339), or `YYYY-MM-DD`. |
| `date(text, layout)` | [`time.Time`](https://pkg.go.dev/time#Time) | Parses with a Go time layout and [`time.Parse`](https://pkg.go.dev/time#Parse). |
| `date(text, layout, zone)` | [`time.Time`](https://pkg.go.dev/time#Time) | Loads the named location and parses with [`time.ParseInLocation`](https://pkg.go.dev/time#ParseInLocation). |
| `timezone(name)` | [`*time.Location`](https://pkg.go.dev/time#Location) | Loads an IANA location with [`time.LoadLocation`](https://pkg.go.dev/time#LoadLocation). |

Named locations depend on zone data available to the host. Use
[`time.UTC`](https://pkg.go.dev/time#UTC) when a portable fixed location is
sufficient.

## Conversion and encoding

| Function | Result | Behavior |
| --- | --- | --- |
| `type(value)` | [`string`](https://pkg.go.dev/builtin#string) | Returns the Go dynamic type name. Nil returns `"nil"`; evaluator lists and maps report their public collection types. |
| `string(value)` | [`string`](https://pkg.go.dev/builtin#string) | Formats [`time.Duration`](https://pkg.go.dev/time#Duration), strings, scalars, complex values, and [`encoding.TextMarshaler`](https://pkg.go.dev/encoding#TextMarshaler) values. Other values require `toJSON` or another explicit conversion. |
| `toJSON(value)` | [`string`](https://pkg.go.dev/builtin#string) | Encodes JSON with [`encoding/json`](https://pkg.go.dev/encoding/json). Maps must have string keys after interface unwrapping. Cycles, duplicate normalized keys, unsupported values, and excessive depth are errors. |
| `fromJSON(text)` | [`any`](https://pkg.go.dev/builtin#any) | Decodes exactly one JSON value. Decimal and exponent numbers become [`float64`](https://pkg.go.dev/builtin#float64); integer tokens become [`float64`](https://pkg.go.dev/builtin#float64) when exactly representable, otherwise [`int64`](https://pkg.go.dev/builtin#int64) or [`uint64`](https://pkg.go.dev/builtin#uint64). Integers outside 64-bit range are errors. |
| `toBase64(value)` | [`string`](https://pkg.go.dev/builtin#string) | Standard-base64 encodes a string or [`[]byte`](https://pkg.go.dev/builtin#byte). |
| `fromBase64(text)` | [`string`](https://pkg.go.dev/builtin#string) | Standard-base64 decodes text and returns the decoded bytes as a string. |

JSON key order belongs to the encoder, not the expression language's map
contract. Application logic must not depend on serialized key order.

## Errors, cancellation, and trust

Wrong argument counts and types become evaluation errors at the call span. A
callback error is wrapped as
[`ErrFunction`](https://pkg.go.dev/go.dw1.io/fasteval#ErrFunction); a callback
used as a predicate must return [`bool`](https://pkg.go.dev/builtin#bool).
Panics are recovered as
[`ErrPanic`](https://pkg.go.dev/go.dw1.io/fasteval#ErrPanic).

Only trusted authors should write expressions. Builtins such as `repeat`, `map`,
`concat`, and `toJSON` allocate in proportion to their inputs. There is no
collection-size or allocation budget. Bound input sizes and use a cancellable
context, but remember that cancellation cannot stop an allocation already in
progress.
