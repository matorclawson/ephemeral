# Ephemeral 0.3.0

A compact single-host durable workflow engine for Linux. Embed package ephemeral in a Go application or drive the included CLI from a worker in any language. Each invocation opens the same local SQLite database. A long-running process can keep the Store open or stream requests to the CLI.

## Try it

On a Linux ARM64 host, download the versioned Linux ARM64 tarball and SHA256SUMS from the release. Verify the downloaded file and install the standalone binary.

```sh
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf ephemeral-0.3.0-linux-arm64.tar.gz
install -Dm755 ephemeral-0.3.0-linux-arm64/ephemeral "$HOME/.local/bin/ephemeral"
"$HOME/.local/bin/ephemeral" --version
```

The source tarball includes dependencies for offline builds. From its root, run `make arm64` to cross-compile from Go 1.25+ without a C toolchain. The static build embeds SQLite. Use `make build` for a smaller native Linux binary linked to the system SQLite library (requires a C compiler). Run `make test test-portable` for both backends and `make demo` for a local workflow.

`make release verify` builds both distribution archives and checksums in dist/. The GitHub Actions workflow runs native ARM64 tests on pull requests and version tags, then attaches the archives to a GitHub Release when the tag matches VERSION.

## Branches, child workflows and parallel steps

A switch selects a forward label by comparing a top-level string field in the current JSON value. A missing field, another JSON type, or an unmatched string selects the default. A step falls through to the next step unless it sets next. The reserved target $end completes the run with its current value.

Register referenced child versions before registering the parent. This example runs a full review in two independent queues or routes a quick review to a single child.

```json
[
  {"kind":"switch","field":"mode","cases":{"full":"full_review"},"default":"quick_review"},
  {"kind":"child","label":"quick_review","children":[{"name":"lint","version":1}],"next":"$end"},
  {"kind":"parallel","label":"full_review","children":[{"name":"lint","version":1,"queue":"lint"},{"name":"audit","version":2,"queue":"audit"}]}
]
```

Child creation and the parent's wait commit together. Children inherit the parent queue unless they specify another. Poll every participating queue, including the parent queue, to drive progress. A single child returns its value directly. A parallel step waits for all children to settle and returns an array ordered by declaration. A failed or independently cancelled child fails the parent at the join. Cancelling a parent cancels its active descendants atomically. Parent history records each group's child IDs and the joined result.

## Runnable examples

Run `make examples` from the source root to execute the CLI approval workflow, activity worker, parallel review, and embedded Go retry example. Individual commands, expected results, and an inspectable JSON request sequence are in [the examples guide](examples/README.md).

## Execution model

Register an immutable (name, positive version) definition containing an ordered list of steps. Start a unique run ID pinned to that version on a named queue. The initial JSON value becomes the first activity's input. A successful activity replaces it with its JSON result. A signal step replaces it with the consumed payload. Timers preserve it. The final value is the workflow result. New versions can coexist with active older runs.

Poll from a worker loop, execute the named activity, and finish with its token. Poll returns JSON null when it has no task. The caller sets idle polling frequency. Timers and timeouts advance only when their queue is polled, including after a restart. Each poll processes up to 32 internal transitions. A timer is an earliest eligible time. Worker clocks on a shared host use wall time. A periodic heartbeat can extend an active lease. Attempts includes the initial attempt and defaults to one. Backoff is a fixed delay. On expiry or explicit failure, the engine either schedules a fresh attempt or closes the workflow as failed. Cancellation fences off subsequent results. A running activity may already have acted externally.

The local state, pending work, and history for each transition commit together under SQLite WAL with synchronous FULL. Keep the database on local storage. Use SQLite's online backup facility for live backups. Copying only the main file while WAL is active can lose recent commits. The database and its directory should be private to trusted clients.

Activities run with at-least-once semantics when retries are enabled. Use (run ID, step index) as a provider idempotency key across attempts. Tokens protect local commits. External side effects require provider-specific handling. Workflow recovery resumes from persisted state. The engine does not replay arbitrary Go code.

## CLI protocol

Run ./ephemeral DATABASE and send JSON objects, one per line. Each successful request returns one JSON value. A malformed or failed request exits nonzero and prints the error to stderr. Earlier successful requests remain committed. Each operation is independently atomic. Times and leases use milliseconds. For paginated reads use limit 1–1000.

| op | Fields | Result |
|---|---|---|
| register | name, version, steps | null; same immutable definition can be registered again |
| start | id, name, version, queue, value, attributes | null; duplicate run ID fails |
| poll | queue, leaseMS | task or null |
| heartbeat | id, token, leaseMS | null |
| finish | id, token, value or nonempty failure | null |
| signal | id, key, name, value | null; retry with identical key, name and payload bytes is safe |
| cancel | id | null |
| attributes | id, attributes | upsert string keys and values |
| describe | id | run state, value, version, step, attempts, due time and attributes |
| history | id, after, limit | ordered events after the exclusive global sequence cursor |
| list | state (optional), afterID, limit | run IDs in ID order |
| find | key, equal, afterID, limit | indexed exact string match in ID order |

Activity steps accept kind, name, attempts and backoff_ms. Signal steps accept kind and name. Timer steps accept kind and millis. Queued signals survive restarts and are consumed once in arrival order within each name. History records transition events, including activity outcomes, signals and attribute changes. Event Data is text, with JSON payloads for structured events. History is retained until the database is removed. Indexed state and attribute queries read committed data directly.

## Event-driven workers

Use `StartOnce` (CLI `start-once`) when a durable input stream can redeliver a record. Choose a stable run ID scoped to the consumer, tenant and logical operation. After creation (true) or a matching replay (false), the consumer may commit its input offset. A conflicting request returns ErrStartConflict. The comparison covers definition name and version, queue, exact JSON input bytes and initial attributes. Attribute map order is immaterial. Nil and empty attribute maps are equivalent. IDs created with ordinary Start or earlier releases have no original fingerprint and return a conflict. Existing backup jobs can finish using the legacy registration path. Retain the workflow database for the entire redelivery window.

Activities may set `retry_delays_ms` to a list of nonnegative durations, one per retry. For example, attempts=3 and retry_delays_ms=[1000,10000] schedules the second attempt one second after failure and the third ten seconds after the next failure. The same policy applies after lease expiry. Use Finish for success or retryable failure and FailPermanent (CLI `fail-permanent`, fields id, token, failure) for a conclusive rejection. A lost external response can require reconciliation.

`NextDue(queue)` (CLI `next-due`) returns the earliest runnable time in milliseconds or an empty result. Workers can subscribe to an application's wake source before consulting NextDue and sleep until the earliest of a hint, due time or bounded fallback. The hint can become stale as other processes commit. Poll still verifies and claims work inside a write transaction. A queue without eligible work uses a read-only check.

See evidence/POSTAL-REVIEW.md for source findings and the integration contract.

## Scope

Workflows use forward-only graphs and statically declared child groups. The engine runs on one host with local SQLite storage. Remote transport, dynamic fan-out, loops and multi-host failover remain future work.

For scoped recovery, use FindInState(key, value, state, afterID, limit), or pass state to the existing find CLI operation. SQLite filters by the indexed attribute and workflow state before applying the page limit. A backup worker can select its binding and ready state directly and fetch details only for the matching runs.
