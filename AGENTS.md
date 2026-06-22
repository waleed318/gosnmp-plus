# AGENT.md — gosnmp-plus

> **For GitHub Copilot Workspace.**
> This file is the single source of truth for autonomous coding tasks on this repository.
> Read it fully before generating any file, test, or edit.

---

## 1. Project Identity

| Field         | Value                                          |
|---------------|------------------------------------------------|
| Module        | `github.com/waleed318/gosnmp-plus`              |
| Language      | Go 1.22+                                       |
| Core dep      | `github.com/gosnmp/gosnmp` (only external dep) |
| License       | Apache 2.0                                     |
| Go version    | Declared in `go.mod`; never exceed it          |

**Purpose:** A production-grade SNMP client SDK that layers retry, connection pooling, desired-state reconciliation, and rollback on top of `gosnmp`. It is a resilience library — not a protocol implementation, not an NMS.

---

## 1.1 Quick Commands

```bash
go build ./...                                          # build everything
go vet ./...                                             # static checks
go test -race ./...                                      # full test suite (race required)
go test -race -run TestName ./retry/...                  # single test
go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out  # coverage report
golangci-lint run ./...                                  # lint (must be 0 issues; config is v2 format)
go mod verify                                             # checksum verification (run in CI)
```

---

## 2. Repository Layout

```
gosnmp-plus/
├── AGENT.md                  ← this file
├── README.md
├── CHANGELOG.md
├── CONTRIBUTING.md
├── SECURITY.md
├── LICENSE
├── go.mod
├── go.sum
│
├── client/
│   ├── client.go             ← core wrapper; implements SNMPClient interface
│   ├── client_test.go
│   ├── pool.go               ← connection pool (per-target, configurable)
│   ├── pool_test.go
│   └── options.go            ← functional options (WithRetry, WithPool, WithLogger)
│
├── retry/
│   ├── policy.go             ← RetryPolicy struct
│   ├── backoff.go            ← Fixed / Exponential / Jitter strategies
│   └── retry_test.go
│
├── state/
│   ├── desired.go            ← DesiredState struct, StateSet type
│   ├── reconciler.go         ← Reconciler interface + default implementation
│   ├── diff.go               ← typed value comparison (int, float, string, []byte)
│   └── state_test.go
│
├── rollback/
│   ├── tx.go                 ← pre-read snapshot, apply, restore on failure
│   └── tx_test.go
│
├── errors/
│   └── errors.go             ← typed sentinel errors
│
├── testdata/
│   └── agent/
│       └── agent.go          ← lightweight UDP SNMP echo agent for tests
│
├── examples/
│   ├── basic_get/main.go
│   ├── reconcile/main.go
│   └── rollback/main.go
│
└── .github/
    └── workflows/
        └── ci.yml
```

**Rules:**
- Never create files outside this layout without updating this section first.
- Never merge packages. `retry` must never import `state`. `state` may import `errors`. See §7 for the full dependency graph.
- `testdata/` is test-only; never imported by production code.
- Caveat: the Go tool excludes any directory named `testdata` from `./...` package patterns and from `go mod tidy`'s module graph resolution. `testdata/agent`'s `gosnmp` import is therefore invisible to `go mod tidy` and gets pruned/marked indirect until a non-`testdata` package (`client`, from M3) imports `gosnmp` directly. Do not add a `go mod tidy && git diff --exit-code go.mod go.sum` CI check before M3 lands — it will fail spuriously.

---

## 3. Coding Standards

### 3.1 General

- All exported symbols must have a `godoc` comment beginning with the symbol name.
- No `panic` in library code. Return errors.
- No `init()` functions.
- No global mutable state. All state lives in structs.
- Use `context.Context` as the first argument on every public method that performs I/O.
- Format with `gofmt`. Imports ordered: stdlib → external → internal (separated by blank lines).

### 3.2 Error Handling

- Always return typed errors from `errors/errors.go`. Never return raw `gosnmp` errors directly — wrap them.
- Wrap with `fmt.Errorf("gosnmp-plus/client: get %s: %w", oid, err)` — include package prefix in the message.
- Use `errors.Is` / `errors.As` for error inspection. Never string-match error messages.

```go
// CORRECT
if errors.Is(err, snmpplus.ErrTimeout) { ... }

// WRONG
if strings.Contains(err.Error(), "timeout") { ... }
```

### 3.3 Interfaces

Define interfaces at the point of use (consumer side), not at the point of definition.

```go
// retry/policy.go — defines the strategy
type BackoffStrategy interface {
    Next(attempt int) time.Duration
}

// client/client.go — depends on the interface, not the concrete type
type SNMPClient interface {
    Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
    Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error
    Walk(ctx context.Context, oid string, fn gosnmp.WalkFunc) error
    Close() error
}
```

### 3.4 Functional Options

All constructors use the functional options pattern:

```go
type Option func(*config)

func WithRetry(p retry.Policy) Option {
    return func(c *config) { c.retry = p }
}

func NewClient(target string, opts ...Option) (*Client, error) { ... }
```

Never add required parameters after `target`. Future additions are always `Option` values.

### 3.5 Concurrency

- The connection pool must be safe for concurrent use. Use `sync.Mutex` or `sync/atomic`; document which.
- Never start a goroutine without documenting its lifecycle and stop condition.
- All goroutines started by `pool.go` must exit when `Close()` is called.
- Tests must pass with `go test -race`.

---

## 4. Package Contracts

### 4.1 `errors`

```go
var (
    ErrTimeout    = errors.New("gosnmp-plus: request timed out")
    ErrAuthFailed = errors.New("gosnmp-plus: authentication failed")
    ErrNoSuchOID  = errors.New("gosnmp-plus: OID not present on device")
    ErrPartialSet = errors.New("gosnmp-plus: set partially applied; rollback triggered")
    ErrPoolClosed = errors.New("gosnmp-plus: connection pool is closed")
)
```

- Add new sentinels here only. Never define errors in other packages.
- All errors from other packages must wrap one of these sentinels.

### 4.2 `retry`

```go
type Policy struct {
    MaxAttempts int           // 0 = no retry (single attempt)
    Backoff     BackoffStrategy
    RetryOn     func(error) bool  // nil = retry on ErrTimeout only
}
```

- `Exponential(base, multiplier, max)` — base × multiplierᵃᵗᵗᵉᵐᵖᵗ, capped at max.
- `Jitter(strategy)` — wraps any strategy and adds ±25% random jitter.
- `Fixed(d)` — constant delay.
- The retry loop must respect `ctx.Done()` between attempts.

### 4.3 `client`

```go
type Client struct { /* unexported */ }

func NewClient(target string, opts ...Option) (*Client, error)
func (c *Client) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
func (c *Client) Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error
func (c *Client) Walk(ctx context.Context, oid string, fn gosnmp.WalkFunc) error
func (c *Client) Reconcile(ctx context.Context, desired []state.DesiredState) (state.ReconcileResult, error)
func (c *Client) Raw() *gosnmp.GoSNMP   // escape hatch; documented as unsupported
func (c *Client) Close() error
```

- `Get` and `Set` apply the retry policy automatically.
- `Set` always calls `rollback.Apply` internally — it is always atomic.

### 4.4 `state`

```go
type DesiredState struct {
    OID       string
    Value     interface{}
    Type      gosnmp.Asn1BER
    Tolerance float64   // 0 = exact match; >0 = allowed deviation for numeric types
}

type ReconcileResult struct {
    Applied    []string          // OIDs successfully set
    Drifted    []string          // OIDs that were out of spec before apply
    Unchanged  []string          // OIDs already at desired value
    RolledBack []string          // OIDs restored after partial failure
    Errors     map[string]error  // per-OID errors
}

type Reconciler interface {
    Apply(ctx context.Context, states []DesiredState) (ReconcileResult, error)
}
```

- `diff.go` must handle: `gosnmp.Integer`, `gosnmp.Gauge32`, `gosnmp.Counter32`, `gosnmp.OctetString`, `gosnmp.ObjectIdentifier`, `gosnmp.IPAddress`.
- Float tolerance comparison: `abs(actual - desired) <= tolerance`.
- Unknown types: return `ErrNoSuchOID` wrapped with OID context.

### 4.5 `rollback`

```go
type Tx struct { /* unexported */ }

func NewTx(client SNMPClient) *Tx
func (t *Tx) Apply(ctx context.Context, pdus []gosnmp.SnmpPDU) error
// Apply: 1) Get current values for all OIDs  2) Set all PDUs  3) On any error: restore snapshot
```

- Snapshot must be taken **before** any Set is attempted.
- If restore itself fails, return a compound error wrapping both the original and restore errors.
- Never silently swallow restore errors.

---

## 5. Test Requirements

### 5.1 Rules

- Every exported function must have at least one test.
- Table-driven tests are mandatory for functions with more than two code paths.
- All tests must pass with `go test -race ./...`.
- Minimum coverage: **80%** per package (enforced in CI).
- Tests must not make real network calls. Use `testdata/agent` or interface mocks.

### 5.2 Mock Pattern

Define mock types in `_test.go` files, not in separate mock packages:

```go
type mockSNMPClient struct {
    getFunc func(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
    setFunc func(ctx context.Context, pdus []gosnmp.SnmpPDU) error
}

func (m *mockSNMPClient) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error) {
    return m.getFunc(ctx, oids)
}
// ... implement full interface
```

### 5.3 Test Agent (`testdata/agent/agent.go`)

The agent is a real UDP listener that responds to SNMP v2c Get/Set. It must:

- Start on a random free port (use `:0`).
- Expose `Addr() string` so tests can connect to it.
- Support `SetOID(oid string, value interface{})` for fixture setup.
- Shut down cleanly when `Close()` is called.

### 5.4 Required Test Cases per Package

**`retry/`**
- Succeeds on first attempt — no delay applied.
- Retries exactly `MaxAttempts` times then returns the last error.
- Respects context cancellation between attempts.
- Jitter stays within ±25% of base delay.

**`client/`**
- `Get` with retry on simulated timeout.
- `Set` triggers rollback on mid-batch failure.
- Pool returns connection to pool after use (check pool size is unchanged).
- `Close` drains all pool connections with no goroutine leak (`goleak` or manual goroutine count).

**`state/`**
- Reconciler calls `Set` only for drifted OIDs, not unchanged ones.
- Tolerance: value within tolerance → unchanged; outside → drifted.
- `ReconcileResult.Errors` is populated per-OID on partial failure.

**`rollback/`**
- Happy path: all PDUs set, no snapshot restore.
- Partial failure: snapshot restored for all OIDs, including those that succeeded.
- Restore failure: compound error returned, neither error dropped.

---

## 6. Scaffolding Instructions

When asked to scaffold a new file or package, follow this checklist in order:

1. **Check layout** — confirm the path matches §2. If not, raise it before creating.
2. **Add package doc** — first line of every new `.go` file is `// Package <name> <one-line description>.`
3. **Define interfaces first** — before any struct implementation.
4. **Add constructor** — every exported struct has a `New<Type>(...)` constructor.
5. **Add `Close() error`** — if the type holds resources (connections, goroutines, channels).
6. **Create `_test.go`** — always create the test file alongside the implementation file.
7. **Add to `examples/`** — if the new surface is user-facing, add a runnable example.

---

## 7. Dependency Graph

Strict. Violations are build errors (enforced via `depguard` in lint config).

```
errors      ← no internal imports
retry       ← errors
rollback    ← errors, client (interface only)
state       ← errors, client (interface only)
client      ← errors, retry, rollback, state
examples/*  ← client (only)
testdata/*  ← stdlib, gosnmp (only)
```

- `retry` must never import `state` or `rollback`.
- `state` must never import `retry` or `rollback` directly.
- `client` is the only package that wires everything together.

---

## 8. Linting

CI runs `golangci-lint run ./...` with this minimal config (`.golangci.yml` at repo root):

```yaml
linters:
  enable:
    - errcheck       # all errors must be checked
    - gosec          # security issues
    - staticcheck    # correctness and performance
    - revive         # style (replaces golint)
    - depguard       # enforce §7 dependency graph
    - govet          # suspicious constructs
    - unused         # dead code

linters-settings:
  revive:
    rules:
      - name: exported          # all exported symbols need godoc
      - name: error-return      # error must be last return value
      - name: var-naming        # Go naming conventions
  depguard:
    rules:
      main:
        deny:
          - pkg: "github.com/waleed318/gosnmp-plus/state"
            desc: "retry must not import state"
            files: ["**/retry/**"]
```

**Before committing any file, the agent must mentally verify:**
- No unhandled errors (`err` assigned but not checked).
- No exported symbol without a godoc comment.
- No import that violates §7.

---

## 9. CI Pipeline  (`.github/workflows/ci.yml`)

```yaml
name: CI
on: [push, pull_request]
jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      # go mod tidy drift-check is deferred until M3: see §2 testdata/ caveat.
      - name: Verify modules
        run: go mod verify
      - name: Lint
        uses: golangci/golangci-lint-action@v7
        with: { version: v2.12.2 }
      - name: Test
        run: go test -race -coverprofile=coverage.out ./...
      - name: Coverage gate
        run: |
          go tool cover -func=coverage.out | \
          awk '/total/{if ($3+0 < 80) {print "Coverage "$3" < 80%"; exit 1}}'
      - name: Build
        run: go build ./...
```

The agent must not modify this file without explicit instruction.

---

## 10. Milestone Feature Map

Use this table to map a milestone instruction to the files that must be created or modified.

| Milestone | Instruction keyword    | Files to create/modify                                      |
|-----------|------------------------|-------------------------------------------------------------|
| M1        | `scaffold`, `init`     | `go.mod`, `errors/errors.go`, `testdata/agent/agent.go`, `ci.yml` |
| M2        | `retry`, `backoff`     | `retry/policy.go`, `retry/backoff.go`, `retry/retry_test.go` |
| M3        | `pool`, `connection`   | `client/pool.go`, `client/pool_test.go`, `client/options.go` |
| M4        | `reconcile`, `desired` | `state/desired.go`, `state/reconciler.go`, `state/diff.go`, `state/state_test.go` |
| M5        | `rollback`, `atomic`   | `rollback/tx.go`, `rollback/tx_test.go`                     |
| M6        | `release`, `oss`       | `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `SECURITY.md`, `examples/` |

---

## 11. Commit Convention

All commits generated by the agent must follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(retry): add exponential backoff with jitter
fix(pool): idle timeout not enforced under concurrent load
test(state): add tolerance edge cases for float comparison
docs(readme): add reconciler quickstart example
chore(ci): add depguard to golangci-lint config
```

- `feat` — new behaviour
- `fix` — bug correction
- `test` — test-only changes
- `docs` — documentation only
- `chore` — tooling, CI, config
- `refactor` — no behaviour change
- Breaking changes: append `!` after type: `feat(client)!: rename Reconcile to Apply`

---

## 12. Out of Scope for v0.1

The agent must **not** implement the following unless explicitly instructed:

- OID registry or multi-vendor aliasing
- MIB parsing
- SNMP trap handling
- OpenTelemetry or Prometheus instrumentation
- SNMPv3 engine ID discovery
- CLI tooling
- Any contrib/ sub-module

If a user prompt implies one of these, respond with:
> "This is scoped to v0.2. See AGENT.md §12. Should I add it to the roadmap instead?"

---

## 13. Repository & Release Strategy

### Account & Namespace
Publish under the personal GitHub account: `github.com/waleed318/gosnmp-plus`.
The OSS reputation belongs to the author, not any employer.

### Branching Model
```
main          ← always green, always releasable; CI must pass
feat/<name>   ← one branch per milestone feature
fix/<name>    ← bug fixes branched from main
```
- **Never commit directly to `main`** — always open a PR, even when working solo.
- Open a Draft PR early; self-review before marking Ready.
- Push every branch at the end of every work session (backup + contribution graph).

### Release Tags

| Tag | After Milestone | Purpose |
|-----|----------------|---------|
| `v0.1.0-alpha.1` | M3 complete | Internal testing only |
| `v0.1.0-beta.1` | M5 complete | Invite early feedback |
| `v0.1.0` | M6 complete | Public announcement |

Tags are created with `git tag -s` (signed). GitHub Release notes are generated from the CHANGELOG.md section for that version.

### First Commit Checklist
The foundation commit (before any implementation) must include:
- `go.mod` with correct module path `github.com/waleed318/gosnmp-plus`
- `AGENTS.md`
- `README.md` (stub with badges, problem statement, and `> Status: Active Development` notice)
- `.golangci.yml`
- `.github/workflows/ci.yml`