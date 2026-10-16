# Task L report

## Status
Blocked before implementation: the required exact requirements brief does not exist at `.superpowers\sdd\2026-09-18-bounded-mpsc-mailbox\task-L-brief.md` in the supplied isolated worktree.

## Evidence
- Reading the exact absolute brief path returned `Path ... does not exist`.
- Enumerating the task directory found only `task-1-report.md` before writing this report.
- A recursive search for `task-L-brief.md` under the repository returned no results.
- Searching the existing `docs\superpowers\plans\2026-09-18-bounded-mpsc-mailbox.md` for `Task L` or `admission` returned no matches.
- `git -C .worktrees\bounded-mpsc-mailbox status --short` returned only the pre-existing untracked plan: `?? docs/superpowers/plans/2026-09-18-bounded-mpsc-mailbox.md`.

## Changes and validation
No implementation or tests were modified. No tests were run: neither the admission-gate specification nor the exact targeted race command could be retrieved. No commit was created; inventing requirements or committing unrelated existing work would be inappropriate.

## Concerns
The missing brief prevents establishing the requested deterministic failing tests and implementing or verifying the specified gate, including its unbounded semantics and ownership cleanup. Restore the exact brief before continuing Task L.

---

## Completed implementation (2026-10-14)

The exact task-L-brief.md is now present and was read before implementation. This section supersedes the earlier missing-brief blocker.

### Scope and protocol
- `engine/pkg/actor/mailbox/worker.go`: replace the timed submitter counter with `admissionMu`. The final running check and queue publication share the same gate as running -> closing -> closed. Publication success is never reinterpreted as rejection.
- BeginStop may wait for an admitted publisher, but never waits for consumers while holding the gate. StopTimeout cannot force shutdown past that publisher; it remains an RW-cleanup budget, not a hard Stop deadline.
- Statistics and Wake remain outside admissionMu. No execution, discard, Release, or wait-for-consumer operation was introduced under the gate.
- Built-in priority fallback normalization and warn-once logging happen before gate acquisition. The queue manager therefore sees a registered priority and does not log under the gate. Direct queue-manager behavior is unchanged.
- Unbounded dual/priority defaults are unchanged. No boundedmpsc, mpsc, mpmc, queue-manager production files, or configuration files were modified. Bounded tests use a test-only IQueueManager adapter around the existing real boundedmpsc queue; Push(false) still exclusively denotes full.
- RW timeout unsafe drain behavior is unchanged.

### Tests and ownership
- `mailbox_lifecycle_test.go`: paused publication after the final state check, before underlying Push, with both DrainExecute/DrainDiscard and unbounded/bounded backends. The consumer is deliberately prevented from normal dequeue so shutdown drain determines ownership. Stop is observed beyond five times the 10ms StopTimeout before publication resumes. Added blocking priority-fallback logger test proving stop can finish while a producer remains outside admission and that resuming rejects it.
- `postjob_ownership_test.go`: independent per-Job release counters (wrappers survive pooled-job release), outside-gate pause/rejection, full bounded queue under both drain policies. Successful jobs release exactly once through drain; rejected jobs remain caller-owned and receive exactly one caller Release. Execution/discard counts assert terminal paths.
- `worker_pool_test.go` (new): shrink waits for the victim's paused publication, drains it before removing the snapshot entry, and rejects stale-worker submissions without taking ownership.

### RED evidence
First test fixture run exposed a nil idler in the fixture, not the production defect; corrected by constructing the real Worker. No production implementation was changed before the subsequent deterministic regression failed:

`go test ./engine/pkg/actor/mailbox -run '^TestWorkerAdmissionLateSubmit$' -count=1`

Both policies failed with:
- `BeginStop deadline exceeded, submitters=1 still in-flight, forcing stop`
- `Worker exited before paused publication; late enqueue after exit: len=1`
- `releases=0, want 1`
- execute/discard `0/0`, expected `1/0` or `0/1`.

Additional fallback mutation check temporarily removed only the pre-gate fallback handling and restored it in a finally block. The dedicated regression failed with `fallback logger held admission gate` and `outside gate submission returned <nil>, want closed`. This additional test was mutation-verified after implementing that fallback adjustment, rather than test-first; the original admission fix followed RED-before-implementation.

### Final verification
- `go test -race ./engine/pkg/actor/mailbox -run 'Test.*(Admission|LateSubmit|Drain|Ownership)' -count=20`: PASS, 59.284s (earlier pass 59.439s before fallback addition).
- `go test ./engine/pkg/actor/mailbox/... -count=1`: PASS; mailbox 5.294s, job 0.920s.
- `go test ./engine/pkg/actor/mailbox -list 'Test.*(Admission|LateSubmit|Drain|Ownership)'`: nine top-level matches, including all five new admission/ownership tests, both pre-existing PostJob drain policy tests, multi-worker drain, and RW inflight timeout. The initial verbose admission run also executed all bounded/unbounded and policy subtests; no empty-selector PASS was accepted.
- gofmt applied; staged `git diff --check` passed. IDE diagnostics on all four Go files reported no errors before the final fallback addition; final Go test compilation verifies that addition.

### Impact and review
GitNexus upstream impact for SubmitJob reported LOW, zero indexed direct callers/processes (interface dispatch is not fully represented); BeginStop reported LOW, one direct caller (Worker.Stop), four transitive symbols, zero processes. Worker type analysis reported LOW, four impacted symbols and one process group (WorkerPool.Start). Actual runtime reach includes mailbox dispatch, stop, pool shrink, and both queue modes, covered by package tests. Precommit `gitnexus detect-changes --scope staged -r emberengine` reported four files, four production symbols (SubmitJob, BeginStop, env, Worker), zero affected processes, LOW risk. Manual staged diff review found only the requested four Go files. Report is included separately; pre-existing brief and plan remain untracked and untouched.

### Concerns / limitations
- Admission serialization adds mutex contention by design; no performance claim or benchmark is made.
- A stalled publisher may extend Stop indefinitely; this is necessary to avoid draining before publication and is explicitly documented.
- Priority preparation is duplicated in Worker to satisfy the requested four-file production scope without changing the queue-manager API. Future queue-manager changes must preserve the no-logging/no-callback-under-admission contract.
- The test-only queue pause does not change production queue semantics. Timeouts are watchdogs/required beyond-StopTimeout observations; channels establish the producer pause.
- Existing expected discard-warning logs remain in tests. Existing RW unsafe-drain limitations remain unchanged.
