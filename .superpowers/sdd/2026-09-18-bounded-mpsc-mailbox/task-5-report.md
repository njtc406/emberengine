# Task 5 report

## Scope and completion

Implemented section 9 of task-5-brief.md in the existing isolated `feature/bounded-mpsc-mailbox` worktree. Only Task 5 production integration, focused tests, the superseded Task 4 assertion, and this requested report are included. No metrics, documentation rollout, benchmarks, sequence implementation, or changes to mpsc/mpmc were made.

- [x] Read section 9 and global constraints.
- [x] Run the specified bounded test selector before edits.
- [x] Add bounded Dual/Priority tests and establish actual RED before production edits.
- [x] Narrow the internal queue interface and add the physical queue factory.
- [x] Integrate normalized specs while retaining public unbounded constructors.
- [x] Propagate full rejection and preserve fallback ownership semantics.
- [x] Replace Task 4's intermediate bounded panic and its obsolete test.
- [x] Run targeted, package, race, formatting, vet, and diff checks.

## Implementation

`newQueue[T]` accepts `normalizedPhysicalQueueSpec`: unbounded creates the existing MPSC; bounded creates only Task 1's mutex-backed `boundedmpsc.Queue`. Invalid backends/capacities panic as internal programming errors. The private queue interface now consists of Push, Pop, Empty, and Len; no BatchPop adapter was introduced.

Internal Dual/Priority constructors consume complete normalized specs. Public constructor signatures and unbounded behavior remain unchanged, including ignoring capacity fields for legacy priority construction. Priority construction copies scheduling configuration per manager, maintaining worker-local scheduler state. Bounded priority construction rejects missing Sys/Urgent/High queues or a non-user fallback; Task 4's normalizer validates the complete public configuration before allocation.

Dual submission selects its physical queue and returns `def.ErrMailboxWorkerIsFull` when Push rejects. Priority fallback saves the requested priority, mutates before publication, restores on rejection, and never accesses the Job following successful Push. Registered priorities do not mutate on rejection. Capacities are per physical queue and never borrowed.

Worker construction now connects these internal constructors. The existing unbounded dual shortcut remains compatible with older internal specs that omit the dual payload. Worker fallback logging remains outside the admission gate, but early Job mutation was removed: QueueManager now owns mutation/rollback. This tightly coupled Worker change is required so full rejection callbacks retain the caller's original priority and late admission rejection does not expose premature mutation. The producer does not touch the published Job after Submit succeeds.

## RED evidence

The exact requested command initially returned `ok ... [no tests to run]`; this was not counted as RED. After adding focused tests, and before production edits:

`go test ./engine/pkg/actor/mailbox -run 'Test(Dual|Priority)QueueManager.*Bounded' -count=1`

failed with exit 1 because `createQueueManager` panicked with `mailbox: bounded QueueManager integration is not yet available`.

## Tests and verification

All final commands passed:

- `go test ./engine/pkg/utils/boundedmpsc ./engine/pkg/actor/mailbox ./engine/pkg/config -count=1`
- `go test ./engine/pkg/actor/mailbox -run 'Test(Dual|Priority)QueueManager' -count=20`
- `go test -race ./engine/pkg/actor/mailbox -run 'Test(Dual|Priority)QueueManager' -count=10`
- `go test -race ./engine/pkg/actor/mailbox -run 'Test.*(Admission|LateSubmit|Drain|Ownership)' -count=20`
- `go test -race ./engine/pkg/utils/boundedmpsc ./engine/pkg/actor/mailbox -count=1`
- `go vet ./engine/pkg/utils/boundedmpsc ./engine/pkg/actor/mailbox ./engine/pkg/config`
- gofmt applied to every changed Go file; editor diagnostics reported no errors for implementation and new test files.
- `git diff --check`

Coverage includes Dual capacities 2/4, independent rejection, FIFO and refill; all priority physical capacities; fallback target capacity and rollback; system admission while user queues remain full; drain counts; Worker-level fallback rollback; deterministic consumer ownership before Push returns; concurrent fallback producers with consumer mutation under race detection; backend selection and invalid factory specs; missing bounded system queue rejection; and legacy unbounded constructors accepting well beyond configured capacities.

Two issues surfaced during broader verification and were corrected: legacy internal unbounded dual specs omit the dual payload, and the Task 4 transitional test still expected bounded construction to panic. The latter now asserts both physical queues are the bounded implementation.

## Impact and concerns

Upstream GitNexus impact ran before production edits for constructors, queue interface, both Submit methods, createQueueManager, and Worker.SubmitJob. Constructors/createQueueManager reported LOW risk, one direct caller and the pool Start flow; the private queue interface reported three direct dependents. Method results reported no direct indexed callers, which undercounts interface dispatch. Actual affected execution is pool construction -> Worker construction -> QueueManager construction and Worker admission -> Submit. Existing admission/drain tests were consequently included.

Pre-commit `detect_changes --scope all` reported five tracked files/five mapped symbols, zero affected processes, LOW risk. The repository index predates some Task 4 symbols and produced an inaccurate NextJob mapping for priority edits; it is advisory, not a complete worktree-aware audit. The new test file is untracked until staging, so a staged check is also performed before committing. No HIGH/CRITICAL result was returned. No unrelated untracked brief/plan files are staged.

Remaining rollout/performance/metrics work belongs to later tasks. This implementation does not claim production performance approval. No known Task 5 correctness failures remain.
