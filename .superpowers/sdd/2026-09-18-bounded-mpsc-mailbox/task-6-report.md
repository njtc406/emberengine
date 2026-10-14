# Task 6 report

## Status

Completed with test-only changes in `engine/pkg/actor/mailbox/bounded_lifecycle_test.go`. Production queue admission, ownership and shutdown protocols remain unchanged. Related coverage is consolidated in a dedicated file rather than expanding the three existing test files. This report is included in the Task 6 commit (`test(mailbox): cover bounded lifecycle and ownership contracts`); obtain its hash with `git log -1 --format=%H -- engine/pkg/actor/mailbox/bounded_lifecycle_test.go`.

## Baseline and RED assessment

Before edits, `go test ./engine/pkg/actor/mailbox -run 'Test.*(Admission|LateSubmit|Bounded|Full|Wake|Drain|Ownership)' -count=1` passed (3.943s). No production defect or legitimate production RED was found. Initial new-test failures were fixture issues: RW forwarding/enum and the existing single-worker RW-disable rule. Correcting the fixture to two workers made the tests pass without production changes.

## Coverage

- Started single-worker mailbox, capacity two, consumer blocked by a channel barrier: fourth PostJob synchronously returns ErrMailboxWorkerIsFull; discard, complete, release order and exactly-once ownership; no execution or successful-submit count increment.
- Actual Cond.Wait observed before one bounded submission; finite-time wakeup and completion.
- Dual and priority bounded factories construct boundedmpsc.Queue (MutexRing); sequence backend rejected.
- Explicitly skipped future SequenceRing reservation/StopTimeout design test documents the required closure/wait ownership contract.
- Concurrent BeginStop/submissions with occupied queues, bounded and unbounded, DrainExecute and DrainDiscard: accepted/rejected ownership, successful-submit count, terminal ordering, zero length.
- RW readCh saturation on both backends: PostJob succeeds, later overflow discards once in order, no execution, and primary dispatch-failure metric stays zero.
- RW timeout while a read holds the semaphore: waiting read receives framework cleanup and release only, not ordinary business callbacks; executing read retains ownership until completion. Both backends covered.
- Existing Task L deterministic gate-inside/gate-outside, late-submit, and fallback-logging tests rerun unchanged, not replaced by stress coverage.

## Final validation

All passed after the final edits:

- `go test -race ./engine/pkg/actor/mailbox -run 'Test.*(Bounded|Full|Wake|Drain|Ownership)' -count=20` (63.307s).
- `go test -race ./engine/pkg/actor/mailbox -run 'Test.*(Admission|LateSubmit|Drain|Ownership)' -count=20` (66.824s).
- `go test ./engine/pkg/actor/mailbox ./engine/pkg/utils/boundedmpsc` (5.942s / 0.292s).
- `go vet ./engine/pkg/actor/mailbox ./engine/pkg/utils/boundedmpsc`.
- gofmt applied; `gofmt -l` empty; `git diff --check` clean; editor diagnostics empty.

## Scope and concerns

Only new tests and this report are committed. Existing untracked briefs and plan are untouched. No admission lock changes, timeout changes, dependencies, metrics implementation or experimental sequence backend integration.

GitNexus impact queries for new/worktree-only test symbols returned target-not-found / UNKNOWN; the registered root index is not current for this worktree. `detect-changes --scope all --repo emberengine` from the worktree reported no changes despite the new untracked test file. This is an index/scope limitation, not proof of zero impact. Manual review shows test-only blast radius, no production callers or execution-flow changes. Sequence reservation behavior remains intentionally unverified and isolated from production.
