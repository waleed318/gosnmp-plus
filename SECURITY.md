# Security Policy

## Supported Versions

gosnmp-plus is pre-v1.0 and under active development. Security fixes are
made against the latest tagged pre-release; there is no long-term support
for older tags.

| Version | Supported |
|---------|-----------|
| latest tag on `main` | ✅ |
| anything older | ❌ |

## Reporting a Vulnerability

**Do not open a public GitHub issue for a security vulnerability.**

Preferred: use [GitHub's private vulnerability reporting](https://github.com/waleed318/gosnmp-plus/security/advisories/new)
for this repository.

Alternatively, email **waleedahmad318@gmail.com** with a description of
the issue, steps to reproduce, and its potential impact.

You should expect an initial response within 7 days. If the report is
confirmed, a fix will be prioritized and a new tag cut; credit will be
given in the release notes unless you ask to remain anonymous.

## Scope

gosnmp-plus is a resilience layer over [`gosnmp`](https://github.com/gosnmp/gosnmp)
(retry, pooling, reconciliation, rollback) — it is not an SNMP protocol
implementation. Vulnerabilities in the underlying SNMP wire-format
encoding/decoding belong to `gosnmp` itself; please report those upstream.
Issues specific to this library's retry, pooling, reconciliation, or
rollback logic (e.g. a way to bypass the rollback guarantee, or a
resource-exhaustion bug in the connection pool) are in scope here.
