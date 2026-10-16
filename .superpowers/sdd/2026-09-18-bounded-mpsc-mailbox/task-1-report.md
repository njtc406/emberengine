# Task 1 report

## Status and scope

Implemented the fixed-capacity mutex-ring MPSC queue only. No Task 0, Task L, Task 2+, Mailbox integration, benchmarks, dependency changes, or changes to existing mpsc/mpmc code were made.

The requested `task-1-brief.md` did not exist at the supplied path. This was checked before implementation and again after validation. Requirements were recovered from the existing worktree document `docs/superpowers/plans/2026-09-18-bounded-mpsc-mailbox.md`, specifically its global constraints, section 3.1, and Task 1. That pre-existing untracked plan was not modified or staged. Exact conformance to any additional requirements in the missing brief cannot be certified.

## Files changed

- `engine/pkg/utils/boundedmpsc/queue.go`: new generic Queue and constructor; exported capacity sentinel errors; noCopy marker; fixed preallocated ring; mutex-serialized Push, Pop, PopBatch, Empty and Len; immutable Cap; uint64 masked indices; zeroing before slot reuse. Public documentation covers initialization, no-copy requirement, single-consumer methods, multiple producers, snapshot observations, mutex waiting and capacity rejection semantics.
- `engine/pkg/utils/boundedmpsc/queue_test.go`: new tests for invalid/valid capacities, exact capacity, empty zero-value reads, FIFO, full rejection, repeated reuse, short/oversized/nil/empty batch destinations, untouched destination suffix, ring boundary traversal, pointer clearing after both Pop and PopBatch, nil payloads, uint64 counter overflow, separately measured allocation paths, and concurrent MPSC correctness.
- `.superpowers/sdd/2026-09-18-bounded-mpsc-mailbox/task-1-report.md`: this report.

## Implementation decisions

Queue contents, positions and count are protected by one mutex. Push(false) occurs only when count equals the fixed buffer length, not on lock contention. Mutex acquisition can block, but operations never wait for capacity or data. PopBatch acquires the mutex once, consumes at most len(dst), and clears each consumed slot before decrementing occupancy/unlocking. There are no sibling-utils imports, sleeps, retry policies, resizing, or production test hooks.

The concurrency test launches runtime.GOMAXPROCS(0) producers, each publishing 200 unique producer/sequence pairs, with a single consumer. A common start channel encourages contention. Full producers retry with runtime.Gosched. The test checks message ranges, total count, duplicates, per-producer exact order, snapshot bounds, and final emptiness. A 30-second deadline and cancellation/join cleanup prevent endless full-queue retry on failures.

## Validation evidence

Environment: `go version go1.24.9 windows/amd64`; GOOS=windows, GOARCH=amd64, CGO_ENABLED=1.

Tests were written before production code. Initial command:

```powershell
go test ./engine/pkg/utils/boundedmpsc -run TestNew -count=1
```

Expected RED, exit 1: undefined ErrCapacityTooSmall, ErrCapacityNotPower2, New, and Queue; package build failed because the implementation did not yet exist.

After implementation and gofmt:

```powershell
go test ./engine/pkg/utils/boundedmpsc -run TestNew -count=1
# ok github.com/njtc406/emberengine/engine/pkg/utils/boundedmpsc 0.302s
go test ./engine/pkg/utils/boundedmpsc -count=100
# ok github.com/njtc406/emberengine/engine/pkg/utils/boundedmpsc 0.365s
go test -race ./engine/pkg/utils/boundedmpsc -count=20
# ok github.com/njtc406/emberengine/engine/pkg/utils/boundedmpsc 2.823s
go vet ./engine/pkg/utils/boundedmpsc
# no output; exit 0
go test ./engine/pkg/utils/boundedmpsc -cover -count=1
# ok github.com/njtc406/emberengine/engine/pkg/utils/boundedmpsc 0.533s coverage: 100.0% of statements
```

All required commands passed. Allocation subtests separately assert zero allocations for successful Push, full Push rejection, successful Pop, and PopBatch using prefilled/preallocated queues and destinations; no benchmark/harness allocation estimates are substituted. IDE diagnostics reported no errors in either Go file. `git diff --cached --check` passed. No broader suite was needed because no existing package or integration was changed.

## Impact analysis and review

GitNexus upstream impact was attempted for Queue scoped to the new package file. After selecting repository emberengine, the response was `Target 'Queue' not found`, impactedCount=0, risk=UNKNOWN. This is expected for an entirely new, unindexed package; it is not a LOW-risk graph certification. Direct existing callers and affected existing execution flows are absent because no integration/imports were added. No existing symbol was modified.

The required pre-commit `detect-changes --repo emberengine --scope staged` was run from the isolated worktree, but returned `No changes detected`. The registered index refers to the primary repository and does not represent this worktree/new package. This limitation was not treated as proof of an empty change. Git's staged diff independently confirmed only the two new queue files (436 lines before adding this report), and the final commit is scoped explicitly to those files and this report. Self-review checked mutex coverage, fixed backing storage, slot clearing, overflow behavior, no spurious full rejection, API documentation, and tests against Task 1 requirements.

## Concerns and follow-up boundaries

1. Missing exact requirements brief: implementation follows the available parent plan; additional brief-only requirements remain unverifiable.
2. GitNexus's existing index does not cover this worktree's new symbols. Its impact/change results cannot certify the new package; manual scoped diff review and tests supply the available evidence.
3. This is a correctness baseline, not approval for production Mailbox use. Lifecycle admission, configuration, integration, and real workload performance gates remain separate tasks.
4. No implementation/test failures remain in the performed validation. The pre-existing untracked plan is deliberately left untouched.
