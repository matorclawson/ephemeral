# Postal, OpenAgentInbox and Ephemeral

## Source findings

| Path in supplied source | Behavior | Owner in a Postal-style application |
|---|---|---|
| Postal app/lib/worker/jobs/process_queued_messages_job.rb and app/models/queued_message.rb | Locks eligible jobs, filters by outbound IP, supports delay and batches related recipients. | Dostoevsky distributes work. The application selects route, tenant and IP, budgets and recipient grouping. |
| Postal app/lib/message_dequeuer/outgoing_message_processor.rb and single_message_processor.rb | Consults suppression, quota and prior attempts. Emits Held or HardFail, sometimes suppresses a recipient. | OpenAgentInbox owns policy and recipient outcome. A generic workflow can stop, wait for a decision or schedule another attempt. |
| Postal app/lib/message_dequeuer/incoming_message_processor.rb and app/senders | Inbound route selection, bounce correlation, SMTP and HTTP delivery. | Implement these in mail adapters. Store protocol receipts and sensitive diagnostics at the application boundary. |
| Postal app/services/webhook_delivery_service.rb | Signed HTTP requests, stable UUID, recorded attempts and five increasing retry delays. | Adapter owns signing, response classification and remote receipts. The engine owns durable retry times and terminal failure. |
| Postal app/scheduled_tasks/expire_held_messages_scheduled_task.rb | A scheduled sweep cancels expired holds. | The application defines expiry and release policy. A future atomic signal-or-deadline primitive would let one workflow settle this race. |
| OpenAgentInbox internal/deliverystore/mail_batch.go, native_watch.go and unread_batch.go | Overlapping writes can share a SQLite commit. Reader notifications are signalled after commit. An unread adapter exports bounded Dostoevsky pages from the SQLite sequence. | Preserve this hot path. Adding a second synchronous workflow write per accepted message would introduce a failure window and a second durability cost. |
| OpenAgentInbox internal/backupflow/flow.go | Defines snapshot, encrypt, offsite, cleanup and restore activities. Compares input and configuration hashes, heartbeats, uses filesystem exclusion, scans older windows and polls every 250 ms. | Keep backup receipts and filesystem safety in this adapter. StartOnce can bind new runs, and NextDue can reduce idle probing. |
| Dostoevsky node.go, publish_batch.go, retention.go, notify.go | Durable partition records, progress checkpoints, stable publication IDs within retained history, batching and wake hints. A batch can leave a prefix. | Use the journal for fan-out, durable offsets and backpressure. Never treat a wake hint as proof of delivery. |

## Suggested first integration

Implement an opt-in webhook subscription in OpenAgentInbox's application layer. For each committed event and subscription revision, derive a stable run ID and frozen payload reference. Use a durable outbox or replayable acceptance log to bridge the mailbox transaction and the publisher. A process-local after-commit notification alone cannot repair a crash in that gap. Bound the dispatcher and apply quotas per tenant/destination before external work.

A Dostoevsky consumer calls StartOnce for each event requiring coordination, then commits its offset only after success or matching replay. A conflict stops that checkpoint for investigation. Acknowledge only contiguous processed records in the partition. Store request bytes, subscription revision and run identity consistently on replay. Changes to a route or payload need a new logical operation ID. Multiple recipients can each have a different outcome.

The webhook worker uses a stable external operation ID across retries. Adapters classify transient, permanent and ambiguous outcomes. Use FailPermanent for conclusive rejection, normal Finish for retryable failure, and a reconciliation activity or operator signal for ambiguous outcomes. Record protocol receipt references as activity results. A crash between remote acceptance and local Finish can still repeat a remote effect. Retrying requires recipient idempotency or a way to reconcile.

Keep high-volume single-step send and read traffic on the existing path. Use workflows selectively for retrying webhooks, deferred deliveries, held-message decisions and recovery operations. An indexed due time is a hint; external wake, bounded polling and recovery scans remain necessary.

## Remaining gaps

1. Atomic signal-or-deadline and revision-checked operator actions for hold/release/expiry races.
2. Durable external-operation intent plus reconciliation state for ambiguous outcomes.
3. Retention and compact deduplication receipts with an explicit replay horizon. Legacy Start IDs cannot be blindly adopted into StartOnce.
4. Fair admission and quotas across tenants and destinations, owned by the dispatch layer. Generic shared limits need demonstrated demand before inclusion in Ephemeral.
5. Acceptance-to-outbox replay, provider adapters, domain verification, SMTP/API intake, suppression policy, delivery tracking and operational UI are OpenAgentInbox work. This candidate changes Ephemeral only.

## Verification scope

The bridge fixture (bridge_test.go.txt) uses the supplied real Dostoevsky library, closes both stores after starting a workflow and before checkpointing, then reopens and redelivers. It checks a single workflow start, successful activity completion and the advanced consumer offset. This is a reopen test. The separate Ephemeral killed-writer test covers abrupt death inside a SQLite transaction.

The compatibility run compiled the supplied OpenAgentInbox 0.10.4 backupflow tests against this candidate's portable runtime. See compatibility.txt. It did not deploy or benchmark the mailbox server. Benchmark methodology and all individual results are in compare.py and benchmark.txt. Run the script from a disk-backed working directory with absolute paths to the baseline and candidate portable JSON CLI binaries. Columns are median and p95 milliseconds per trial. The busy-writer scenario deliberately holds a separate SQLite write transaction for 20 ms while polling an empty queue. The workflow scenario includes three CLI round trips per completed single-activity workflow.

The candidate also exposes FindInState to replace the backup worker's list-all-and-describe pattern. Query each relevant state for the current binding and page by run ID. Discovery remains advisory because another worker can claim or finish a run before the caller acts.
