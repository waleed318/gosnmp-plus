# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/) once it
reaches v1.0.0. Until then, minor versions may include breaking changes.

## Unreleased

### Added
- `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`

## v0.1.0-beta.1 — 2026-06-22

Invite early feedback. M4 (desired-state reconciliation) and M5 (rollback)
land, along with the `client.Client` wrapper that ties every package
together, and runnable examples.

### Added
- `state` package: `DesiredState`, `DesiredStates`, and a `Reconciler` that
  fetches current values, classifies each OID as unchanged or drifted
  (respecting per-state `Tolerance` for numeric types), and issues a single
  `Set` for the drifted batch
- `rollback` package: `Tx.Apply` snapshots every OID before `Set`, restores
  the full snapshot if `Set` fails, and returns a compound error wrapping
  both the original and (if it also fails) the restore error — never
  silently dropping either
- `client.Client`: the top-level wrapper — `NewClient`, `Get`, `Set`,
  `Walk`, `Reconcile`, `Raw`, `Close` — wiring `retry`, `client.Pool`,
  `rollback`, and `state` behind a single target. `Get`/`Set` apply the
  configured retry policy automatically; `Set` (including the `Set`
  `Reconcile` issues internally) always goes through `rollback.Tx`
- `client.WithCredentials` option (SNMP community + version)
- `examples/basic_get`, `examples/reconcile`, `examples/rollback` — runnable
  commands demonstrating each capability against a real target

### Fixed
- `depguard`'s "retry must not import state" rule had no `files:` scope
  (lost during the v1→v2 golangci-lint config migration), so it was
  blocking every package — including `client` — from importing `state`.
  Re-scoped to `retry/**` only
- Coverage gate: `go test ./...` instruments coverage for every matched
  package, including `examples/`'s untested `main()` commands, which
  dragged the blended total below the 80% gate. Excluded `examples/` from
  the coverage profile; they're still verified by `go build`/`go vet`/lint

## v0.1.0-alpha.1 — 2026-06-20

Internal testing only. Scaffold through the connection pool.

### Added
- Module scaffold: `go.mod`, `.golangci.yml`, CI pipeline (lint → race
  tests with 80%-per-package coverage gate → build)
- `errors` package: five sentinel errors (`ErrTimeout`, `ErrAuthFailed`,
  `ErrNoSuchOID`, `ErrPartialSet`, `ErrPoolClosed`) that every other
  package wraps its errors around
- `testdata/agent`: an in-process UDP SNMP v2c agent (GetRequest/SetRequest)
  used by integration tests so nothing touches the real network
- `retry` package: `Policy.Do` with pluggable `BackoffStrategy` —
  `Fixed`, `Exponential`, and `Jitter` (±25%); respects context
  cancellation between attempts
- `client.Pool`: a per-target idle-connection pool, safe for concurrent
  use, with a background goroutine that evicts connections past a
  configurable TTL and exits cleanly on `Close`
- `client.Option` functional options: `WithRetry`, `WithPool`, `WithLogger`
- Repository hygiene: `.gitignore`, Dependabot config (gomod + GitHub
  Actions, monthly)
