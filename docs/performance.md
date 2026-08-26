# Performance and benchmark methodology

Treat performance in [`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) as a
measured design constraint. Results depend on the expression and machine, so
measure compile time, evaluation time, memory, and allocations separately on
equivalent workloads.

## Benchmark coverage

The root package contains focused and representative benchmarks for:

- expression compilation across tiny, arithmetic, access, Boolean, list, map,
  and regular-expression sources;
- literal, lookup, access, comparison, arithmetic, Boolean, collection, typed
  conversion, and template evaluation paths;
- large collection workloads and allocation-sensitive operations;
- the plain-identifier template substitution path.

Run the root suite with:

```bash
go test -run='^$' -bench=. -benchmem ./...
```

During development, narrow the benchmark expression to shorten each run:

```bash
go test -run='^$' \
  -bench='^BenchmarkEval(Lookup|Comparison|Arithmetic)$' \
  -benchmem -count=10 -benchtime=1s .
```

The `benchmarks` submodule compares
[`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) with four other expression
engines on a small common subset:

```bash
make -C benchmarks bench
make -C benchmarks benchstat
```

By default, the cross-engine suite collects ten two-second samples for each
compile and evaluation workload. Shorter screening runs can override both
values:

```bash
make -C benchmarks bench COUNT=5 BENCHTIME=500ms
```

Generated benchmark output is written below `benchmarks/out/` and is ignored by
Git.

## Cross-engine scope

Before timing begins, the harness runs every workload and checks each result.
Setup that does not belong to the measured compile operation stays outside the
timed loop, and evaluation benchmarks compile only once. Each benchmark reports
allocations and keeps its result alive to prevent compiler elimination.

Results apply only to the included sources and values. The harness does not
claim full semantic equivalence across engines with different type systems,
extensions, safety behavior, and optimization tradeoffs.

The shared workloads are:

- constant arithmetic;
- numeric variables and comparison;
- Boolean conditions;
- string equality and inequality.

Add a workload only when all compared engines can express equivalent behavior.
Keep engine-specific behavior in the root benchmark suite.

## Matched before-and-after comparisons

Base optimization claims on matched measurements:

1. Start from a passing, clean baseline and record the exact commit, Go version,
   architecture, CPU, and benchmark command.
2. Run the baseline and candidate with the same source workload, variables,
   process settings, sample count, and benchmark duration.
3. Keep setup outside timed loops in both versions.
4. Compare raw samples with
   [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat); do not
   compare only one displayed number from separate runs.
5. Re-run in reverse or alternating order when thermal state, background work,
   or frequency scaling can bias the result.
6. Inspect bytes and allocations together with time.
7. Profile the retained candidate and confirm that the expected hotspot moved.
8. Run correctness, race, architecture, lint, vulnerability, and fuzz gates
   before retaining a change.

For a lower-noise CPU comparison, keep the processor environment fixed and use
one Go scheduler thread when appropriate:

```bash
GOMAXPROCS=1 go test -run='^$' \
  -bench='^BenchmarkEvalComparison$' \
  -benchmem -count=10 -benchtime=1s .
```

Record any CPU affinity command separately because its availability and syntax
are host-specific.

## Profiling

Collect each profile separately to avoid one profiling mode distorting another:

```bash
go test -run='^$' -bench='^BenchmarkEvalArithmetic$' \
  -benchtime=10s -cpuprofile=/tmp/fasteval-cpu.pprof .

go test -run='^$' -bench='^BenchmarkEvalArithmetic$' \
  -benchtime=10s -memprofile=/tmp/fasteval-mem.pprof .

go tool pprof -top /tmp/fasteval-cpu.pprof
go tool pprof -top -alloc_space /tmp/fasteval-mem.pprof
```

Use `-inuse_space` when retained live memory is the concern. Allocation-space
profiles answer a different question. Use mutex and block profiles only when
there is evidence of contention or blocking.

Profiling can leave a package test binary in the worktree. Check `git status`
afterward, and remove only artifacts you can identify as generated.

## Retention rules

Keep a performance change only when:

- the benchmark represents a real compile or evaluation path;
- results are statistically supported or the allocation change is exact and
  materially useful;
- correctness and public behavior are unchanged;
- important adjacent workloads do not regress;
- the profile supports the proposed cause;
- added branches, state, or ownership complexity have a continuing measured
  benefit.

Revert neutral or noisy changes, unsupported workload-specific tradeoffs, and
changes that hurt a common smaller workload. A plausible fast path is not enough
evidence to keep it.

## Design constraints

The current implementation keeps these constraints:

- compiled objects and the builtin registry are immutable;
- callers own compiled-object caching;
- the public dynamic result remains [`any`](https://pkg.go.dev/builtin#any);
- checked integer arithmetic, lossless conversion, cancellation, and typed
  errors remain part of measured behavior;
- generic fallbacks preserve named types, typed nils, reflection-based access,
  and collection semantics;
- no [`unsafe`](https://pkg.go.dev/unsafe), pooling, mutable global registry, or
  hidden result cache is used.

Some type switches and scanner/parser ownership rules add detail to the
implementation. They remain because matched benchmarks and profiles showed
lower CPU or memory use without a behavior change.

## Interpreting results

Include these details with published results:

- the full benchmark name;
- toolchain, operating system, architecture, and CPU;
- sample count and benchmark duration;
- `ns/op`, `B/op`, and `allocs/op`;
- [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) deltas and
  significance when comparing versions;
- the exact baseline and candidate revisions;
- known workload or host limitations.

Keep compile and evaluation numbers separate unless the report explains their
weighting. A compile-once service and a compile-per-request tool have different
costs. If every engine moves in the same direction across sequential full runs,
the pattern usually indicates host drift rather than a causal improvement.

Historical figures are not a performance contract. Re-run the tracked harness
on the target hardware and workload before making capacity or engine-selection
decisions.

## Verification gates

Use checks that match the risk of the optimization. The full repository gate is:

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go mod tidy -diff
golangci-lint run --default=all .
govulncheck ./...
make -C benchmarks bench
make -C benchmarks benchstat
git diff --check
```

Run module checks in `benchmarks/` as well when its code or dependencies change.
Fuzz the compiler and evaluator when parser, literal, conversion, access, or
evaluation boundaries change.
