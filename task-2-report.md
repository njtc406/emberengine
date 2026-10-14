# Task 2 performance report

## Status: BLOCKED / NOT APPROVED

Task 2 measurement artifacts are delivered, but **production acceptance is not established**. This is a development-machine screening run, not completion of every production evidence requirement. No production Mailbox code, defaults, ownership, shutdown behavior, or queue implementation was changed. No SequenceRing was implemented or registered. Gate thresholds were not relaxed.

## Source and isolation

- Working branch: `feature/bounded-mpsc-mailbox`, production source `4100678`.
- Explicit old baseline: `73ecd28`, checked out detached into `.perf-baseline`, independently compiled. The baseline fixture was copied into that checkout; the current working tree was never reset to collect old results.
- The exact same Mailbox fixture and internal measurement helpers compile against both revisions. New configuration is applied through JSON for old-version compatibility; the baseline ignores bounded-only fields and only runs unbounded.
- Old MPSC source has no diff against current. Nonetheless the old unbounded microbenchmark was independently built and measured. Copying `queue.go` into the baseline provides compilation of the shared microbenchmark adapters; only `UnboundedMPSC` is selected there, so it does not claim an old bounded implementation existed.
- All added Go files are test harnesses or their minimal internal support package. Untracked user briefs and plan files were not included.
- GitNexus impact queries for new harness entry points returned `not found`, `UNKNOWN`, zero indexed dependants (new files and a stale main-worktree index). Staged `detect-changes` returned `No changes detected` because the registered index resolves the main worktree; its output is retained in `results/gitnexus-scope.txt`. This is not a claim of graph-verified scope. Git staged-path review and `git diff --cached --check` verified additions only, with no existing production symbol edited.

## Frozen protocol and environment

AMD Ryzen 7 5700X 8-Core Processor; Windows amd64; Go 1.24.9; GOMAXPROCS=4. Deployment machine metadata/attestation was not supplied. Machine, compiler, OS, commit, effective RW mode, success/rejection counts and individual rounds are in JSON.

Production comparison thresholds: throughput >=0.95, p99 <=1.10, p999 <=1.15, process CPU/success <=1.10 of old baseline, MostlyEmpty total CPU <=1.10. No across-scenario averaging. Five rounds, rotating candidate order baseline/current-unbounded/bounded. Comparisons use the median of per-round metrics. The runner stores all raw samples before comparisons.

`task2-v1` pilot: 8,192 steady messages, 4,096 MostlyEmpty messages. CPU often returned zero. The pilot is preserved under `results/pilot`. Before repeating all candidates, measurement lengths were increased consistently, not thresholds: `task2-v2` uses 65,536 steady messages, 8,192 MostlyEmpty messages. Zero CPU and zero latency denominators still occur. They are explicitly unresolved/failed, never converted to PASS. MostlyEmpty CPU is now nonzero in all five rounds. This environment needs more appropriate timing/workload duration for many other scenarios.

Steady arrival schedule: 32 jobs each 1 ms, predetermined deadlines; MostlyEmpty: one job per 1 ms. Jobs are preallocated, with 256 sequential warmup jobs. Real EventBus Job wrappers and an invoker execute 64 deterministic arithmetic iterations and an observable atomic update. End-to-end time ends after this work, not at queue Pop. RW scenarios mark 75% of jobs read-only. Overload uses 65,536 submissions without pacing and 16,384 work iterations. Configuration uses per-physical-queue capacity 4,096, a 16 MiB layout budget, and 32 max concurrent reads. Single-worker RW is reported off; effective RW-on is only compared with four workers.

Matrix: dual/priority × workers 1/4 × effective RW off/on where supported × uniform/one-hot key (12 steady scenarios), plus dual four-worker RW-off MostlyEmpty, resize 4→6→4, and overload (15 total). Five rounds × three candidates = 225 scenario observations per protocol.

## CPU and latency measurement

`engine/internal/perftest` is shared by both harnesses. Windows uses `GetProcessTimes` user+kernel ticks for the **test process**, not PowerShell or the Go build process. Supported Unix platforms use `getrusage(RUSAGE_SELF)`. Unsupported OS implementations return an error. No wall clock is substituted for CPU. Unix implementation was not executed on this Windows machine.

Queue samples have distinct preallocated message ownership; a timestamp is written before publication. Successful-call duration and first-attempt-to-final-acceptance duration are distinct; retries retain their first timestamp. Push-to-Pop is a separate metric. Sorting occurs after producer/consumer completion. Bounded full rejection samples are reported separately. Race verification passed for the queue latency harness.

Microbenchmarks use the same tryQueue interface, nonblocking channel sends, Gosched retries, and successful-consumption accounting. Setup/payload/goroutine creation and teardown are excluded from benchmark timing. Batch16/64 use native destination-slice PopBatch for MutexRing and repeated Pop fallback for other adapters (not allocating MPSC BatchPop). MostlyEmpty microbenchmarks serialize admissions by consumption count; they are not the paced Mailbox MostlyEmpty CPU gate. AlwaysFull is bounded-only and correctly reports zero successful msg/s; ns/op there describes a rejection attempt, not throughput.

## Results

Artifacts: `tools/mailboxperf/results/` contains raw benchmark text, latency logs, five per-candidate JSON files, aggregated JSON, pilot data, CSV comparisons, and test/gate logs. `run.ps1` reproduces the Windows Mailbox experiment and preserves each process's output.

Ten fixed-10,000-message microbenchmark repetitions were run for the full candidate/topology/payload/scenario matrix. Indicative upper-middle rates from the ten samples (not production criteria):

| Candidate | Int 1P1C msg/s | Int 4P1C msg/s |
|---|---:|---:|
| MutexRing | 39,729,837 | 17,211,704 |
| Channel | 15,862,944 | 9,332,711 |
| Current unbounded MPSC | 10,312,468 | 9,608,917 |

The complete old-unbounded micro data is `old-unbounded-micro.txt`. The short fixed operation count is useful for reproducible smoke coverage, not a stable deployment throughput estimate. Queue results cannot approve the Mailbox.

Examples of production screening failures from `comparison.csv`:

- Bounded MostlyEmpty CPU ratio **1.43**, above 1.10; its baseline p99 was unresolved.
- Bounded dual/four-worker/RW-off/hot p999 **1.43**, above 1.15.
- Current unbounded priority/four-worker/RW-on/uniform p99 **1.25**, p999 **1.46**.
- Bounded priority/four-worker/RW-on/uniform CPU/success **1.52**, p99 **1.25**, p999 **1.18**.
- Many steady cases have unresolved CPU and/or latency denominators. NaN means unavailable, not zero cost.
- All uninstrumented non-overload samples completed their submitted jobs without rejection/drop. Both unbounded revisions completed all 65,536 overload jobs. Bounded overload successes were 19,011 / 22,532 / 19,269 / 20,585 / 21,875, and corresponding main-full rejections 46,525 / 43,004 / 46,267 / 44,951 / 43,661. Every round conserves 65,536 and releases/drains all work. Rejection fractions are roughly 66–71%; lower successful-job tail latency must not hide those rejections.

## Verification

- `go test ./engine/internal/perftest ./engine/pkg/utils/boundedmpsc ./engine/pkg/actor/mailbox -short -count=1`: PASS.
- `go test ./engine/pkg/utils/boundedmpsc -run '^$' -bench '^BenchmarkQueue$' -benchtime=10000x -benchmem -count=10`: PASS; allocation metrics recorded.
- `go test ./engine/pkg/utils/boundedmpsc -run '^TestQueueLatency$' -count=10 -v`: PASS, each run itself rotates five rounds.
- Same microbenchmark at baseline, restricted to UnboundedMPSC, ten repetitions: PASS.
- Baseline/current/bounded Mailbox samples: all 15 process runs PASS per protocol (both pilot and extended).
- `TestMailboxPerformanceGateRequiresBaseline` and comparison validator tests: PASS. Tests inject empty baseline and malformed/missing rows, metadata mismatch, CPU zero, duplicate round, loss, throughput/CPU/p99 regressions.
- Targeted `-race` queue latency, bounded lifecycle, and missing-baseline tests: PASS.
- Actual bounded Mailbox sample under `-race`: **FAIL**, due to main-queue capacity rejections in four RW scenarios under instrumentation. No data-race warning was emitted. This is not recorded as a passing performance run and is excluded from comparison data; raw output retained.
- `MAILBOX_PERF_GATE=1` with missing baseline: expected FAIL. With recorded baseline: expected FAIL on invalid CPU resolution. Gate remains fail-closed even with valid numerical inputs because deployment attestation/memory evidence is not implemented here.

## Remaining gaps / concerns

1. Production target is unspecified and this shared Windows development machine produced zero-duration short work samples and coarse process CPU increments. Longer windows improved MostlyEmpty CPU but did not resolve all steady metrics. No production approval is possible.
2. This workload is synthetic real-Job execution, not representative application business logic. Deadline catch-up bursts and open-loop throughput capped by offered rate must be considered before interpreting throughput ratios.
3. Overload currently covers a finite sustained submission burst and verifies release conservation/drain. It does **not** prove a multi-window main-queue memory plateau or collect queue occupancy/RSS as backlog grows. Stop-rejection field remains zero because no concurrent stop experiment is performed in the performance fixture. RW-drop attribution is derived from discard callbacks; a dedicated read-pool saturation performance scenario is not implemented. Existing lifecycle tests are not a substitute for those performance measurements.
4. MostlyEmpty supplies idle/wakeup evidence, but there is no separately controlled long-idle wakeup phase. Resize samples perform explicit transitions; no autoscaler-driven load oscillation is measured.
5. Queue latency currently measures interface-pointer payload; int and interface-pointer are both covered by microbenchmarks, not both latency payload types. Queue CPU values are logged per round but are not aggregated into a separate median summary.
6. The executable Go gate validates five-round metadata, conservation and numerical thresholds, then explicitly blocks for missing target/memory evidence. It is intentionally **not a production approval endpoint yet**. The CSV Pass column only describes numerical screening; overload rows are never marked pass.
7. Benchmark duration is intentionally fixed and short; longer isolated deployment-machine runs are needed. Unix helper still requires target-platform execution validation.

**Decision:** retain default unbounded; no canary authorization. Complete missing production evidence in Task 9 or a follow-up explicitly scoped to those requirements. Do not substitute microbenchmark wins or loosen thresholds to bypass these blockers.
