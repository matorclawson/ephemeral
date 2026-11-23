# Changelog

## 0.3.0 candidate — 2026-11-23

- Added idempotent workflow submission with conflict detection and immutable start receipts.
- Added per-attempt retry delays and fenced permanent failures.
- Added indexed deadline lookup and read-only idle polling. Reused loaded workflow definitions during transitions and removed redundant post-commit rollbacks.
- Added state-filtered attribute lookup for scoped, paginated recovery.
- Added cross-store replay coverage, compatibility results and reproducible polling measurements.

## 0.2.1 — 2026-11-22

- Added runnable CLI approval, branching and parallel review, and embedded Go retry examples with expected results. Run all four examples through make examples. The ARM64 package includes the CLI examples.

## 0.2.0 — 2026-11-22

- Added conditional routing on string fields, named forward jumps and explicit workflow completion targets.
- Added version-pinned child workflows and parallel groups of up to 64 children. All children receive the step input. Joins return results in declaration order and survive worker or engine restarts. Cancellation reaches active descendants.
- Added a statically linked Linux ARM64 CLI with embedded SQLite, offline vendored source distribution, SHA-256 checksums, and a tag-driven release workflow with native ARM64 tests.
- Expanded the Makefile with ARM64 cross-builds, portable tests, packaging, checksum verification and installation. The CLI reports its embedded release with --version.
- Existing 0.1 workflow definitions and run IDs remain valid. Opening an existing database creates the children table. Source builds now require Go 1.25 or later.

## 0.1.0 — 2026-11-21

- Introduced durable sequential workflows with activities, timers and signals.
- Added activity leases, heartbeats, retries, cancellation and recovery on reopen.
- Added ordered history, string search attributes, run inspection, a JSON CLI and an embedded Go API.
