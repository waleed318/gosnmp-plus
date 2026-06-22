# Contributing to gosnmp-plus

Thanks for considering a contribution. This project is in active development
(pre-v1.0), so expect some API churn until v0.1.0 ships.

## Development setup

```bash
git clone https://github.com/waleed318/gosnmp-plus.git
cd gosnmp-plus
go build ./...
```

Requires the Go version declared in `go.mod`.

## Common commands

```bash
go build ./...                                            # build everything
go vet ./...                                               # static checks
go test -race ./...                                        # full test suite (race required)
go test -race -run TestName ./retry/...                    # single test
go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out  # coverage report
golangci-lint run ./...                                    # lint (must be 0 issues)
```

All four of the first commands (plus lint) must pass before opening a PR —
CI enforces the same.

## Project structure

See [AGENTS.md](AGENTS.md) for the full repository layout, package
dependency graph, and coding standards. The short version:

- `errors` has no internal imports; every other package wraps its errors
  around one of its five sentinels.
- `retry` may depend on `errors` only.
- `rollback` and `state` each define their own minimal interface for what
  they need from a connection (`Get`/`Set`), rather than importing
  `client` — this avoids an import cycle, since `client` imports both of
  them.
- `client` is the only package that wires everything together.
- `testdata/` and `examples/` are not imported by library code.

Don't add a new top-level package or move files between packages without
opening an issue first to confirm it fits the layout.

## Coding standards

- Every exported symbol has a godoc comment starting with its name.
- No `panic` in library code — return errors.
- No global mutable state; no `init()` functions.
- `context.Context` is the first argument on every public method that does I/O.
- Errors are wrapped with `fmt.Errorf("gosnmp-plus/<pkg>: <action>: %w", err)`
  around one of the sentinels in `errors/errors.go`. Never match on
  `err.Error()` strings — use `errors.Is`/`errors.As`.
- New constructors use the functional-options pattern (`New<Type>(required, opts ...Option)`).
- Format with `gofmt`; import order is stdlib → external → internal.

## Tests

- Every exported function needs at least one test.
- Table-driven tests for anything with more than two code paths.
- Tests must not make real network calls — use `testdata/agent` (a
  real in-process UDP SNMP v2c agent) or a hand-rolled mock in the test
  file itself.
- Minimum 80% coverage per package, enforced in CI.
- Everything must pass `go test -race`.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/):

```
feat(retry): add exponential backoff with jitter
fix(pool): idle timeout not enforced under concurrent load
test(state): add tolerance edge cases for float comparison
docs(readme): add reconciler quickstart example
chore(ci): add depguard to golangci-lint config
```

Breaking changes append `!` after the type: `feat(client)!: rename Reconcile to Apply`.

## Branching and PRs

- Never commit directly to `main` — every change goes through a PR, even
  for small fixes.
- One branch per feature/fix, branched from the latest `main`.
- Don't modify `.github/workflows/ci.yml` as a side effect of an unrelated
  PR; call it out explicitly if a change genuinely needs it.

## Reporting bugs / proposing features

Open a GitHub issue. For anything that would add scope explicitly listed
as out-of-scope for v0.1 in [AGENTS.md §12](AGENTS.md) (OID registry, MIB
parsing, trap handling, OpenTelemetry/Prometheus instrumentation, SNMPv3
engine ID discovery, CLI tooling), please open an issue to discuss before
sending a PR — it'll likely be deferred to v0.2.

## Security issues

Do not open a public issue for a security vulnerability — see
[SECURITY.md](SECURITY.md).
