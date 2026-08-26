# Contributing

[`fasteval`](https://pkg.go.dev/go.dw1.io/fasteval) requires Go 1.26.0 or newer.
Keep changes focused, preserve existing behavior unless the change requires
otherwise, and add tests for new or corrected behavior.

## Root module checks

Run the focused tests for the code you changed, then run the full root checks:

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go mod tidy -diff
golangci-lint run --default=all .
govulncheck ./...
git diff --check
```

Do not resolve addressable lint findings by changing `.golangci.yml` or adding
`nolint` directives. Fix the code or tests instead.

## Benchmark module

The `benchmarks` directory is a separate Go module. If a change touches its code
or dependencies, check that module separately:

```bash
cd benchmarks
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go mod tidy -diff
```

Run the cross-engine suite from the repository root:

```bash
make -C benchmarks bench
make -C benchmarks benchstat
```

Read the [performance guide](performance.md) before changing benchmark workloads
or interpreting results. Performance claims need matched measurements from
equivalent workloads; separate sequential runs can reflect host drift.

## Documentation

Update README and the relevant file under `docs/` when public behavior changes.
Keep exported Go documentation current and link prose references to Go symbols
on `pkg.go.dev`. Check rendered package documentation with:

```bash
go doc -all .
```

## Implementation notes

The parser and evaluator use the standard library. Checked generic integer
operations come from
[`go.dw1.io/safemath`](https://pkg.go.dev/go.dw1.io/safemath).
