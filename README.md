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
| 🔲 | M3 Pool | Per-target connection pool |
| 🔲 | M4 State | Desired-state reconciliation |
| 🔲 | M5 Rollback | Atomic Set with snapshot restore |
| 🔲 | M6 Release | Docs, examples, v0.1.0 |

## License

Apache 2.0 — see [LICENSE](LICENSE).
