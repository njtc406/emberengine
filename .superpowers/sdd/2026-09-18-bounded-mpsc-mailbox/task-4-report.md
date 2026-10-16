# Task 4 report: normalized Mailbox queue configuration

## Scope and baseline

Implemented section 8 only in the existing isolated `feature/bounded-mpsc-mailbox` worktree. Starting HEAD: `77fda773a3f2291ae12c31fd53f1ded14922368b`. Task L and Task 1 were already present. No QueueManager bounded storage factory, Push rejection changes, metrics, benchmarks, template rollout, or later-task implementation was added.

The pre-edit prescribed command `go test ./engine/pkg/actor/mailbox -run 'Test.*QueueConfig' -count=1` returned PASS **with no tests to run**, so this was not accepted as RED. Added queue_config_test.go, reran the same command, and observed the expected build failure for missing QueueBackend, DualQueueCapacityConf, MaxQueueStorageBytes and Capacity. Production implementation followed that failure.

## Completed checklist

- [x] Configuration fields with per-Worker physical capacities and layout-budget comments.
- [x] fixConf remains responsible for existing defaults and Min/Max repairs, without caller mutation.
- [x] NewWorkerPool validates and stores an independently owned normalizedQueueSpec before context/controller/Worker construction.
- [x] Empty backend means unbounded; unknown backend rejects. Unknown bounded mode rejects; unbounded retains dual fallback.
- [x] Bounded dual requires explicit exact power-of-two capacities >=2, with no rounding/default sizes.
- [x] Bounded priority rejects absent/empty maps, missing system queues, nil/invalid capacities, and missing user fallback.
- [x] Audited engine/pkg/event/category.go: framework emits Sys, Urgent, and High. All three require registered bounded queues, preventing these priorities from falling back to user storage.
- [x] Fallback uses the numerically greatest registered priority; bounded requires it to be a user priority (>=Normal).
- [x] Spec retains strategy, batch limit, batch sizes, weights and capacities. Maps store values; dual capacities are copied. clonePriorityBatches now copies the entire struct.
- [x] Initial and resized Workers receive the pool spec; queue settings are not re-read from caller configuration. Worker schedulers receive independent copies and counters.
- [x] Bounded autoscaling requires strategy, effective Max>=Initial, and positive cooldown. Tests cover fixConf's Min/Max repair; unbounded retains previous construction behavior.
- [x] Layout budgeting uses actual unsafe.Sizeof(Queue[IMailboxJob]) and interface slot size; includes each physical queue's fixed object. Checked additions/multiplications protect slot sums, array layout sums, and Worker multiplication. Array bytes must fit platform int. Fixed pools use Initial; autoscaling uses repaired Max.
- [x] Invalid configurations fail without mutating the source or allocating Workers/slot arrays.

## Task boundary / runtime concern

**This is configuration groundwork, not a runnable bounded Mailbox release.** Task 5 explicitly owns bounded QueueManager construction and rejection semantics. Valid bounded configuration passes NewWorkerPool validation, but attempting to instantiate its Workers currently panics with `bounded QueueManager integration is not yet available`. This deliberate guard prevents the prohibited silent unbounded fallback. A regression test locks that intermediate boundary; Task 5 must replace the guard and corresponding test when implementing the bounded factory. Do not enable bounded in deployments at this commit.

Unbounded managers retain existing constructors and behavior. Their priority constructor still normalizes an independent spec-derived configuration copy; it no longer parses the caller's mutable queue settings. The legacy constructors were intentionally not refactored as part of Task 4.

## Tests and evidence

All following checks passed:

- `go test ./engine/pkg/actor/mailbox -run 'Test.*QueueConfig' -count=1` (rerun after final formatting).
- `go test ./engine/pkg/actor/mailbox -run '^$'` (rerun after final formatting).
- `go test ./engine/pkg/actor/mailbox ./engine/pkg/config -count=1`.
- `go vet ./engine/pkg/actor/mailbox ./engine/pkg/config`.
- `go test -race ./engine/pkg/actor/mailbox -run 'Test.*(QueueConfig|Admission|LateSubmit|Drain|Ownership)' -count=10` (31.099s, no race failures).
- `go test ./... -run '^$'` (repository compile check, not the full test suite).
- `git diff --check` (only Git CRLF conversion notices).

Confirmed matched test names using `-list`: eight QueueConfig tests plus nine admission/lifecycle/ownership/drain tests. Configuration cases include exact budget and one-byte-over-budget boundaries, missing budgets, platform-array overflow, real aggregate layout overflow, real Worker multiplication overflow, direct checked arithmetic overflow, caller immutability on success/failure, capacity-preserving copies, and Worker scheduler isolation.

## Impact analysis and review

Before production edits, GitNexus upstream impact was run on NewWorkerPool, newWorker, createQueueManager, clonePriorityBatches, WorkerPool, MailboxConf, PriorityConfig, Start and resizeWorkers. Most targets LOW; MailboxConf MEDIUM (7 direct dependents). Direct construction paths are Mailbox construction -> NewWorkerPool, pool Start/resizeWorkers -> newWorker -> createQueueManager; clonePriorityBatches feeds both multi-level normalization functions. Impact centers on service initialization and pool start/resize flows.

**Precommit change detection reports HIGH aggregate risk:** 7 tracked changed files, 12 indexed symbols, 11 Start-related execution flows. This includes graph attribution to timing-wheel callees, not edits to their code. No mpsc, mpmc, Gate Session, or other non-Mailbox queue code changed. New tests/report are included explicitly in staging; the existing index cannot discover their new symbols until reindexed.

The requested `detect_changes --scope compare --base-ref main` reports CRITICAL (518 graph-mapped files, 7500 symbols, 275 processes). This is inherited branch divergence: before this task's commit, HEAD was already 149 commits ahead of main and `git diff main HEAD --name-only` contained 563 files. It is not a Task 4 change set. The scoped working-tree/staged diff is the task audit; no inherited changes were reverted or included anew.

Existing untracked task briefs and the larger plan remain untouched and unstaged. Small changes in three existing test files only pass the new required spec argument. The report is included in the task commit; obtain its identifier with `git log -1 --format=%H`.
