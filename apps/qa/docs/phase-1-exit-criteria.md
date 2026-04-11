# Phase 1 Exit Criteria — Test Coverage Map

Each criterion from `docs/architecture/mvp-scope.md` is mapped to the test(s) that validate it.

## Automated Criteria

| # | Criterion | Target | Test(s) | Status |
|---|-----------|--------|---------|--------|
| 1 | Pilot deployment stable 14 days | 500 endpoints, 14d | Load test + pilot ops | MANUAL |
| 2 | Agent CPU | < 2% avg | `footprint-bench` (Windows) | AUTOMATED (Windows CI) |
| 3 | Agent memory | < 150 MB RSS | `footprint-bench` (Windows) | AUTOMATED (Windows CI) |
| 4 | Agent disk | < 500 MB | `footprint-bench` (Windows) | AUTOMATED (Windows CI) |
| 5 | Dashboard p95 | < 1s | `event_flow_test.go` | AUTOMATED |
| 6 | Event loss | < 0.01% | `500_steady.json`, `event_flow_test.go` | AUTOMATED |
| 7 | E2E latency p95 | < 5s | `event_flow_test.go` | AUTOMATED |
| 8 | Server uptime | >= 99.5% | Pilot ops + `flow7_silence_test.go` | MANUAL + AUTO |
| 9 | Keystroke isolation | Red team confirms | `keystroke_admin_blindness_test.go` | **AUTOMATED (BLOCKING)** |
| 10 | Live-view governance | Dual-control; audit chain intact | `liveview_test.go`, `audit_chain_test.go` | AUTOMATED |
| 11 | KVKK DPO sign-off | DPO signature | DPO review | MANUAL |
| 12 | Auto-update rollback | Canary + rollback demo | Manual drill | MANUAL |
| 13 | mTLS revocation | < 5 min propagation | `enrollment_test.go` | AUTOMATED |
| 14 | Anti-tamper | Kill → recover < 10s; ACL tamper < 60s | Manual + Windows test | MANUAL |
| 15 | Documentation | Runbooks published | Doc review | MANUAL |
| 16 | Security scan | No critical/high CVEs | SBOM scan (separate pipeline) | AUTOMATED |
| 17 | ClickHouse replication | Failover + catch-up + backup restore | Staging drill | MANUAL |
| 18 | Sensitive bucket routing | m.6 signal → sensitive bucket | `legalhold_test.go` | AUTOMATED |
| 19 | Legal hold e2e | Placement + TTL bypass + release + audit | `legalhold_test.go` | AUTOMATED |
| 20 | DSR SLA timer | open → at_risk → overdue | `dsr_test.go` | AUTOMATED |
| 21 | Destruction report | Renders + sig verifies + counts match | `dsr_test.go` + manual sig check | SEMI-AUTO |

**Automated coverage: 14/21 criteria (67% automated, 33% manual)**

## Key Decision: EC-9 Is a Hard Gate

EC-9 (keystroke admin-blindness) is deliberately a hard blocking gate in CI. The rationale from `key-hierarchy.md`:

> "If the threat model changes [...] it would require a deliberate, audited code change + Vault policy change + new proto — i.e., it is visibly non-accidental."

The test cannot be disabled or made non-blocking without a deliberate code review and security-engineer sign-off.
