# Project history

These documents are a **historical record** of the Python → Go migration
(completed and shipped in v0.1.0). They are kept for provenance and are **not**
maintained as current documentation — for that, see the [docs index](../).

| Document | What it records |
|----------|-----------------|
| [PORTING.md](PORTING.md) | Why Go, dependency map, and the high-level phasing of the port. |
| [go-parity-plan.md](go-parity-plan.md) | The authoritative, component-by-component parity specification and the autonomous execution runbook the port followed. |
| [SWEEP.md](SWEEP.md) | The unbiased post-port Go-vs-Python sweep: all findings, the 10 accepted fixes, and the documented divergences. |
| [VALIDATION.md](VALIDATION.md) | The evidence log: differential acceptance results (0 mismatches), byte-for-byte JSON parity, and the quality gates. |

Paths and references inside these files describe the repository **as it was
during the migration** (e.g. the now-removed `src/baseliner/**` Python tree).
