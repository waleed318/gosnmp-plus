# gosnmp-plus

[![Go version](https://img.shields.io/badge/go-1.22+-blue)](go.mod)
[![CI](https://github.com/waleed318/gosnmp-plus/actions/workflows/ci.yml/badge.svg)](https://github.com/waleed318/gosnmp-plus/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-green)](LICENSE)

> **Status: Active Development** — not yet suitable for production use.

A production-grade SNMP client SDK that layers **retry**, **connection pooling**, **desired-state reconciliation**, and **rollback** on top of [`gosnmp`](https://github.com/gosnmp/gosnmp). It is a resilience library.

## Problem

Working directly with `gosnmp` in production exposes several gaps:

- No built-in retry with configurable backoff strategies
- No connection pooling for multi-target polling at scale
- No concept of desired state — callers must diff and reconcile manually
- No atomic Set with rollback on partial failure

`gosnmp-plus` fills these gaps with a clean, composable API.

## Milestones

| Status | Milestone | Description |
|--------|-----------|-------------|
| ✅ | M1 Scaffold | Module, errors, test agent, CI |
| ✅ | M2 Retry | Fixed / Exponential / Jitter backoff |
| ✅ | M3 Pool | Per-target connection pool |
| ✅ | M4 State | Desired-state reconciliation |
| ✅ | M5 Rollback | Atomic Set with snapshot restore |
| 🔲 | M6 Release | Docs, examples, v0.1.0 |

## Usage

### Quickstart

`client.Client` wires retry, connection pooling, desired-state reconciliation, and rollback together behind a single target. `Get`/`Set` apply the configured retry policy automatically, and `Set` (including the `Set` issued internally by `Reconcile`) always goes through `rollback.Tx` — a failure restores the pre-Set snapshot rather than leaving the agent partially updated.

```go
c, err := client.NewClient("192.0.2.1:161",
    client.WithCredentials("public", gosnmp.Version2c),
    client.WithRetry(retry.Policy{MaxAttempts: 3, Backoff: retry.Exponential(100*time.Millisecond, 2, 2*time.Second)}),
)
if err != nil {
    log.Fatal(err)
}
defer c.Close()

packet, err := c.Get(ctx, []string{".1.3.6.1.2.1.1.5.0"})

err = c.Set(ctx, []gosnmp.SnmpPDU{
    {Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("router-1")},
})

result, err := c.Reconcile(ctx, []state.DesiredState{
    {OID: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 1},
})
// result.Applied / result.Drifted / result.Unchanged / result.RolledBack / result.Errors
```

Runnable versions of these live under [`examples/`](examples/).

### Advanced: using the lower-level packages directly

`retry`, `client.Pool`, and `state.Reconciler` are independently usable if you don't want the full `Client` wrapper — e.g. to plug your own connection management in front of `state.Reconciler`, or to apply a retry policy to non-SNMP work.

### Retry with backoff

```go
policy := retry.Policy{
    MaxAttempts: 3,
    Backoff:     retry.Jitter(retry.Exponential(100*time.Millisecond, 2, 2*time.Second)),
}

err := policy.Do(ctx, func() error {
    return doSomethingThatMightTimeOut()
})
```

### Per-target connection pool

```go
factory := func(ctx context.Context, target string) (*gosnmp.GoSNMP, error) {
    conn := &gosnmp.GoSNMP{Target: target, Port: 161, Community: "public", Version: gosnmp.Version2c, Timeout: 2 * time.Second}
    if err := conn.Connect(); err != nil {
        return nil, err
    }
    return conn, nil
}

pool := client.NewPool(factory, client.WithPool(4, 30*time.Second))
defer pool.Close()

conn, err := pool.Get(ctx, "192.0.2.1:161")
if err != nil {
    log.Fatal(err)
}
defer pool.Put("192.0.2.1:161", conn)
```

### Desired-state reconciliation

```go
// snmpClient is anything satisfying state.SNMPSetter — Get(ctx, oids) and Set(ctx, pdus).
reconciler := state.NewReconciler(snmpClient)

result, err := reconciler.Apply(ctx, []state.DesiredState{
    {OID: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("router-1")},
    {OID: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 1, Tolerance: 0},
})
// result.Applied / result.Drifted / result.Unchanged / result.RolledBack / result.Errors
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
