# Task 2 performance report

## Runtime trace follow-up: STOP after one diagnostic (2026-10-14)

**Task2 remains NOT ACCEPTED.** Exactly one existing fixed128 bounded diagnostic ran; no matrix, retry, workload/rate/capacity/threshold or production changes. Preregistered executable stop rule: capture until first rejection or 60 seconds, maximum 64 MiB written (overflow explicitly invalidates trace); continue the unchanged 128-window workload once, stop regardless of reproduction. The runner refuses overwrite. GitNexus upstream impact for `TestMailboxPerformanceSample`: LOW, zero direct callers/processes. New support is test-only.

### Capture and exact commands

Parent `8a0bac541dd6c389e88ef01a5c019b10123ff7b8`; Go reports go1.24.9 windows/amd64, GOMAXPROCS=4, Ryzen 7 5700X. `tools/mailboxperf/results/trace128-diagnostic` contains timestamps, helper SHA256, full row, process logs, trace metadata, compact profile and excerpts. Run from this worktree:

```powershell
& .\tools\mailboxperf\trace128.ps1
go tool trace -pprof=sched tools\mailboxperf\results\trace128-diagnostic\runtime.trace > tools\mailboxperf\results\trace128-diagnostic\sched.pprof
go tool pprof -top tools\mailboxperf\results\trace128-diagnostic\sched.pprof
go tool trace -d=parsed tools\mailboxperf\results\trace128-diagnostic\runtime.trace | Select-String 'mailbox-block|mailbox-first-rejection|mailbox-trace-stop'
node tools\mailboxperf\extract-trace.cjs tools\mailboxperf\results\trace128-diagnostic 540321800000000 540323245000000
```

This installed Go tool documents `-d=parsed` (not numeric `-d=1`). Trace started 07:30:48.386050 UTC, stopped on first rejection 07:31:00.5068585: 5,672,478 bytes, zero discarded. Full trace and 37-MB expanded neighborhood were analyzed then deleted, as was redundant full flight JSON; SHA256 retained in `discarded-raw-hashes.json`. Commands describe the completed analysis, not a request to repeat it. Compact `target-events.txt`, `long-states.json`, `flight-excerpt.json`, markers and sched profile remain. `long-states` measures successive trace state records, including generation snapshots, NOT exclusively scheduler delays.

### What this trace establishes

- First rejection: window 5/index 17,134; actual block +12.1208085s vs deadline +10.9684158s (**1.1523927s late**); occupancy 4,096 on both sides, completions 340,718. GC cycle/pauses remain 3/6.
- Producer G50, between publications 11,647/11,648: 997.5643ms gap, queue 32→0 before the next submit, completions 339,296→339,328. The trace explicitly shows `Running→Waiting reason=sleep` at 540321919580360, `Waiting→Runnable` at 540322917404447 (**997.824087ms**), then `Runnable→Running` at 540322917437136 (**32.689µs**). Thus the dominant initial gap is **delayed sleep/timer resumption**, NOT a producer spending that second runnable. Its next deadline was only ~0.58ms beyond the previous observation. The queue had drained; this is not a full-queue stall.
- G50 wakes worker G51 at 540322917476907 (M42500/P1). Parsed trace puts G51 runnable until 540323218460112 on M39112/P0, nominal **300.983205ms**; worker identity is established by the `Worker.run → safeExec → perfInvoker.ExecuteJob → perftest.Since` stack. Producer subsequently runs on M26524/P3, where the rejection marker is emitted. P IDs are Go processors, M IDs runtime threads, not proof of continuous OS CPU residency.
- **Important clock/trace boundary limitation:** around 540323218460xxx there are `Running→Running`/`Runnable→Runnable` generation snapshots and many consecutive events spaced exactly 1ns. Producer also appears Running across ~300ms. These are ordered/reconstructed trace timestamps, not trustworthy independent measurements of a 300ms OS scheduling interval. `Running` alone cannot distinguish actual CPU execution from OS descheduling. Do not claim Windows thread starvation, a particular competing process, or exact worker starvation duration is proven.
- Catch-up from publication 11,648 to rejection: 5,486 positions, 1,390 further completions, net **4,096** pending growth from occupancy 0. From 12,240 to rejection: 4,894 positions minus 811 completions = 4,083 growth, 13→4,096. The unchanged finite-buffer accounting explains rejection.
- Scheduler profile is aggregate over the capture: 695.29ms total, 529.92ms attributed to `sync.Cond.Broadcast`, 96.14ms `runtime.asyncPreempt`, 28.37ms runtime metric reads. Wake-site attribution is NOT time spent inside Broadcast nor evidence of a production lock defect. It does not include the producer's timer Waiting interval. Instrumentation, QPC syscalls, trace startup/stop and trace writes perturb this run; stopping synchronously after rejection can affect later workload results.

Whole row: 8,388,608 offered, 226,744 main rejects, zero other rejects, 267.7964326s measured wall; exit 1 retained, no acceptance inference. This event is substantially different from the earlier 136.9ms capture; do not transfer exact durations across runs.

### Disposition and external prerequisite

The missing attribution is narrowed to a timer-wait/resumption delay plus observed worker runnable delay, with trace-clock limitations; no production queue defect is demonstrated. Environment/runtime/harness timing is implicated, but the exact OS cause is **not proven** by Go trace. A finite queue cannot guarantee losslessness when an open-loop deadline generator repays a large pause as a catch-up burst while consumer service is delayed. Extra production tuning is not a justified remedy. **This machine/run cannot establish non-overload acceptance.** Remaining external prerequisite is an independently controlled, scheduling-stable performance environment (with reliable OS-level timing evidence if attribution is required) that can satisfy the original unchanged acceptance protocol. No additional speculative local run is prescribed or authorized here. Tasks7/8/9 and production approval remain blocked.

Verification: fresh `go test ./engine/pkg/actor/mailbox ./engine/internal/perftest -count=1` and matching `-race` passed; these skip the opt-in performance sample. The bounded-writer test checks overflow accounting. The diagnostic itself failed for loss, as reported; it was not rerun.

## Latest diagnostic: first fixed128 rejection captured (2026-10-14)

**One authorized diagnostic completed, no repeat and no acceptance inference. Production bug NOT proven.** A test-only, opt-in flight recorder captured producer deadline catch-up plus insufficient completion progress into a physically full queue. The initiating scheduling delay is not attributable to a specific goroutine/OS cause from this capture. Task2 remains NOT ACCEPTED; Tasks7/8/9 and production approval remain blocked. Historical fixed128 results below remain unchanged and invalid for bounded CPU comparison.

### Scope and reproducibility

`tools/mailboxperf/flight128.ps1` refuses a second run if its evidence directory exists. Exactly one bounded candidate used the existing `task2-v5-window-fixed128` scenario: priority/w1/RW-off/hot/steady, GOMAXPROCS=4, 128 × 65,536 jobs, 32 arrivals/ms with original catch-up, 64 work iterations, 4,096 normal-priority slots. No rate, capacity, workload, threshold or production source changes. Parent revision and helper hashes are in `tools/mailboxperf/results/flight128-diagnostic/`. The 265.6081435s measured row offered 8,388,608, executed 8,378,492, rejected 10,116 (all main), drained fully; exit 1 is the expected retained non-overload loss, not a passing experiment. No second diagnostic was run.

`MAILBOX_PERF_FLIGHT` enables a preallocated 32,768-publication ring, frozen at the first rejection. It records scheduled/actual/return times relative to the block, before/after occupancy and release-derived completion progress, per-batch process GC and scheduler histogram counters. Existing job execution, release and production queue paths are untouched. No trace was enabled. Runtime `gctrace=1,schedtrace=1000` is retained in raw output (go driver/compiler and test process logs interleave; distinguish their reset clocks). Serialization occurs after workload completion; after freeze sampling ceases but the original workload continues.

### Verified first-rejection sequence (zero-based window/job)

- Ring covers window 9, indices 30,024–62,791. First reject is index 62,791 at block +20.5907813s, scheduled +20.5548952s: **35.8861ms behind schedule**. Both queue observations are 4,096, completion progress 648,519. Immediately prior samples show occupancy incrementing 4,089 through 4,096 with unchanged completions. This is actual capacity exhaustion, not an unexplained rejection at low occupancy.
- Between indices 58,250 and 58,251, publication observations have a **135.76ms gap**; occupancy is 1 on either side and completion progress does not advance. At index 58,268 producer lag is **136.9279ms** and occupancy 18. This is not evidence of 135ms of an already-full queue; producer scheduling/publication itself fell behind.
- From index 58,268 to 62,791 the producer advances **4,523 publication positions in 40.9582ms**, while completion progress advances only **445**. Occupancy rises **18 → 4,096**: 4,523 − 445 = 4,078 additional pending jobs, exactly matching observed growth. The unchanged deadline policy demonstrably compresses overdue arrivals into a catch-up burst. Consumer progress is inadequate during this interval, but not zero throughout.
- A second 33.54ms publication gap occurs between 60,676 and 60,677 while completion progress advances 83. From 60,785 to rejection, completion count is fixed at 648,519 for 3.3521ms while queue occupancy grows 2,090 → 4,096. This documents stalled completion progress, not proof of runnable-consumer starvation: release can trail execution and no per-goroutine trace was captured.
- **GC is excluded as the first captured event's concurrent trigger:** all retained samples have GC cycles=3 and GC pauses=6. Test-process GC3 is around 0.150s; next GC4 is at 43.537s, well after the first rejection. This does not exclude GC from later rejects or historical runs. Process scheduler histogram observations gain two events in buckets with lower bounds >=1ms; these are aggregate/sampled runtime observations, not precise worker attribution. One-second schedtrace is too coarse to explain the 135.76ms gap.

### Interpretation and limits

Established proximate mechanism: a late producer catches up against absolute batch deadlines while consumer completions fail to keep pace, filling the unchanged finite queue. A sustained queue algorithm defect is not demonstrated; queue-full behavior and observed accounting agree. The **cause of the initial simultaneous publication/progress gap remains unresolved** (OS scheduling, runtime scheduling, pauses outside GC, and harness contention cannot be separated here). Do not label this an established producer-only bug or proven consumer starvation; do not extrapolate this event to all 10,116 new or 33,992 historical rejects.

Timing caveat: two occupancy reads lock the queue per publication, clocks and per-batch runtime metrics add overhead, and schedtrace performs runtime logging. Observations are non-transactional; release-derived counts are not execution-start timestamps or exactly-once proofs. Windows `time.Now` readings visibly repeat/coarsen; QPC latency uses a different clock. This diagnostic is **not acceptance**, a CPU comparison, or permission to relax workload/capacity/loss thresholds. Instrumentation can affect both producer/consumer scheduling. Actionable next investigation, only with separate authorization: scoped goroutine/OS scheduling tracing around an equivalent late-publication event; distinguish producer descheduling from runnable consumer delay before proposing any production fix. No automatic rerun or production change is justified.

### Verification and impact

GitNexus upstream impact for the only edited existing function `TestMailboxPerformanceSample`: LOW, zero direct callers and zero affected processes (including tests). Recorder is new `_test.go` support, single-producer owned, bounded and allocation-free on record. Deterministic tests first failed for missing recorder, then passed for ring wrap, empty history, first-rejection freezing and zero record allocations. Fresh full `go test ./engine/pkg/actor/mailbox ./engine/internal/perftest -count=1` and full corresponding `-race` suite passed, as did targeted recorder/window race tests. These correctness runs skip the opt-in diagnostic and do not erase its exit 1. No production approval.

## Latest disposition: fixed128 focused experiment — STOP (2026-10-14)

**Authorized fixed experiment completed; Task2 remains NOT ACCEPTED.** CPU precision is now sufficient in all 15 rows, but bounded lost submissions in three of five rounds. Its five-round CPU comparison is INVALID, not a demonstrated CPU regression or a pass. No selective removal, replacement, rerun or tuning was performed. No production changes. Tasks7/8/9 remain blocked/not executed; no deployment, canary, merge or production approval.

Preregistered protocol and minimum CPU exposure are in `tools/mailboxperf/results/fixed128-v5/protocol.md`, with pre-execution registration and identical baseline/current helper SHA256 manifests. The only fixture change is a separately named fixed128 protocol; original V5/V4 remain 32 windows. The unchanged focused scenario is priority/w1/RW-off/hot/steady, 65,536 jobs/window, 128 windows, 8,388,608 offered jobs/row. Minimum CPU remains 3.125s, CPU ratio limit 1.10. Five rotating rounds ran once, sequentially, 06:01:25–07:08:38 UTC (~67 minutes), against isolated baseline 73ecd28 and current 5b703f3 plus recorded test-only support. Baseline isolation was removed afterward. Machine: Ryzen 7 5700X, Windows/amd64, Go 1.24.9, GOMAXPROCS=4; gctrace=1 for all rows.

### Complete focused results

All rows drained and resolved CPU. All rejection counts below are main-queue rejects; other rejection/drop categories are zero. Successful count equals 8,388,608 minus rejects. Bounded rounds 0–2 exited 1 for non-overload loss; all other exits were 0. Wall time, full quantiles, per-window distributions, raw GC/test output and exact exit/order timestamps are retained in individual JSON/TXT files, `summary.csv` and `attempts.json`. Raw fixture `Round` defaults to zero; filename and attempts ledger are the authoritative actual round IDs (raw rows intentionally not rewritten).

| Round | Baseline CPU s | Unbounded CPU s | Bounded CPU s | Bounded rejects | Bounded valid |
|---|---:|---:|---:|---:|---|
| 0 | 11.734375 | 13.203125 | 14.343750 | 30,926 | No |
| 1 | 13.187500 | 12.765625 | 11.015625 | 2,112 | No |
| 2 | 11.312500 | 10.703125 | 11.078125 | 954 | No |
| 3 | 11.906250 | 11.500000 | 10.921875 | 0 | Yes |
| 4 | 12.062500 | 12.796875 | 11.687500 | 0 | Yes |

Baseline and current-unbounded are loss-free in all five rounds. Their median CPU/success ratio is **1.0721784777**, below unchanged 1.10 (focused CPU criterion only). Baseline median CPU is 11.90625s; unbounded is 12.765625s. Paired ratios, descriptive only: 1.12516644, 0.96800948, 0.94613260, 0.96587927, 1.06088083. Host variability is visible; this is not a confidence-bound attestation. Bounded's 33,992 total rejected submissions invalidate the series; no bounded aggregate ratio is offered using the two surviving rounds. CPU exposure no longer blocks measurement, but loss blocks the requested bounded inference. The cause of stalls/loss is not newly diagnosed here.

### Verification and exact remaining status

Selector regression failed before implementation and passed afterward (`red.txt`, `green.txt`). Fresh full `go test ./engine/internal/perftest ./engine/pkg/actor/mailbox -count=1` passed; those correctness tests skip opt-in performance and do not override the three recorded measurement failures. GitNexus impact was LOW; final detect_changes identifies exactly three test symbols in two files, zero affected processes, low risk. `git diff --check` passed. Helper hashes verified identical before running; prior evidence untouched. No new race run or OS scheduler attestation is claimed.

The fixed authorization is exhausted: **STOP, no further experiment tweaks.** Remaining Task2 work is unresolved bounded non-overload loss and a valid bounded focused comparison, followed by the complete frozen 15-scenario, three-candidate, five-round matrix (225 observations) with compatible valid exposure for every scenario: dual/priority × worker/RW combinations (w1/RW-off, w4/RW-off, w4/RW-on) × hot/distributed, plus mostly-empty, resize and overload. Full throughput, CPU/success, p99/p999, idle CPU and overload-specific criteria remain unaccepted; this experiment substitutes for none of those full-matrix gates. Further work requires separate authorization, not automatic reruns. Tasks7/8/9 must not proceed. Earlier sections retain chronological stops and are not current approvals.

## Bounded rejection investigation (2026-10-14)

**STOP after one diagnostic: physical queue exhaustion is established; the triggering stall behind the historical 2,144 rejects is NOT isolated. No harness bug is demonstrated, no production fix is justified, and Task2 remains unaccepted.** This investigation did not retry acceptance, tune rates/capacities, discard arrivals, or resume Tasks7/8/9.

### Direct trace and quantitative evidence

- Original evidence: `results/replicated-v4/pilot-bounded.{json,txt}`, pilot script and fixture at commit `2446867`. The script selects exactly priority/w1/RW-off/hot/steady, GOMAXPROCS=4, 32 windows. Each window offers 65,536 distinct preallocated wrappers; 256 earlier warmup jobs are excluded. All three original candidates have inadequate CPU exposure.
- Pacing in `TestMailboxPerformanceSample` uses `start := time.Now()`, then deadline `start + (i/32)*1ms`. It sleeps only for positive `time.Until(deadline)`. Deadlines restart after each window drains. Late batches are submitted immediately until the schedule catches up: 32,000/s is the **offered average**, not a burst limit. No deadline multiplication overflow or cross-window accumulated deadline was found. Pacing uses Go's clock; completion latency independently uses QPC. A clock-resolution mismatch exists, but is not evidence of a 200ms clock error.
- `newSimpleConf(1)` disables autoscaling. `DispatchJob` takes `snap.sole` without hashing; the hot key cannot spread work. `newMetricsTestJob` assigns `PriorityNormal`. The JSON fixture supplies capacity 4,096 to each of six priority queues; normalization and `newPriorityQueueManager` pass that exact capacity to `boundedmpsc.New`. Thus usable capacity for this workload is **4,096**, not 6×4,096; idle priorities cannot lend slots. BatchSize=16 controls scheduling, not capacity; the worker main loop uses `NextJob`/single Pop.
- `PostJob → DispatchJob → SubmitJob → PriorityQueueManager.Submit → Queue.Push`: for this single-worker, registered-priority path, `ErrMailboxWorkerIsFull` means count equals 4,096 under the queue mutex. It is not an RW dispatch drop, hashing failure, or rejected ownership transfer. With RW disabled, the sole worker calls `safeExec → perfInvoker.ExecuteJob`: 64 arithmetic iterations, observable atomic update, QPC latency, then normal release. No production EventBus business handler is invoked by this synthetic invoker.
- The original 2,095,008 executed + 2,144 rejected = 2,097,152 offered; discard-derived RW drops and other rejects are zero, and releases drain. These reject returns reflect real instantaneous full queues, not inferred missing executions. Aggregate release accounting alone cannot prove exactly-once ownership for every ID, but neither code trace nor evidence demonstrates an ownership failure; targeted ownership tests also pass.
- At 32 arrivals/ms, 4,096 slots cover **128ms of arrivals without dequeue progress** from empty. The original window index 14 has p99=175.4114ms and p999=193.944ms; these are measured *after actual publication attempts*, so producer sleep before stamping does not itself explain them. A long queued/execution delay or consumer scheduling stall is implicated, possibly combined with deadline catch-up. Other windows largely have tens-of-microseconds tails. This favors a transient stall over sustained 32k/s consumer insufficiency, but original rows contain neither per-window rejection counts nor enqueue/dequeue timelines. Do not attribute all rejects to window 14 or derive an exact stall length from those quantiles. 2,144 rejects correspond to 67ms of nominal arrivals, not a measured duration or proof of a single burst.

### One diagnostic, not acceptance

Evidence is in `tools/mailboxperf/results/reject-diagnostic/`. Before temporary instrumentation, GitNexus upstream impact for `TestMailboxPerformanceSample` returned UNKNOWN/not found in the shared index; direct scope is Go's opt-in test runner only, no production callers or flows. Instrumentation patch is preserved, but reverted from source after this one run. It records per-batch Go/QPC elapsed time, deadline lateness, requested sleep, queue-depth snapshot and cumulative rejections; logs are emitted after each window drains. Queue-depth reads add locking and logging adds inter-window overhead, so the observation is diagnostic-only, not comparable acceptance data. CPU model metadata was not supplied; runtime is the same Go 1.24.9/windows/amd64, GOMAXPROCS=4.

Command: `go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$/^priority-w1-rwfalse-hottrue-steady$' -count=1 -timeout 240s -v`, with `MAILBOX_PERF_SAMPLE=1`, `MAILBOX_PERF_PROTOCOL=task2-v4-replicated`, `PERF_CANDIDATE=bounded` and unique `PERF_OUTPUT`. No changed work, capacity, rate, window count, deliberate delay, or arrival filtering.

Result: exit 0; 2,097,152 successes, zero rejects, drained; 2.890625s CPU (still INVALID), 65.5924889s wall. Across 65,536 batch probes: maximum pre-batch queue snapshot 96; maximum post-sleep deadline lateness 3.3255ms; Go-minus-QPC elapsed discrepancy ranged −0.9321ms to +0.8572ms. Only 45 batches had no positive requested sleep. This run did **not reproduce** the historical stall/full queue and does not exonerate intermittent clock/scheduler/consumer behavior. Snapshot depth is not an exact high-water mark. No proven harness bug was fixed and no speculative regression was added.

After reverting probes, targeted clock/CPU, replication, bounded-full ownership and PostJob ownership tests passed (see `targeted.txt`). Production and test source are unchanged. GitNexus `detect_changes --scope all --repo emberengine` returned “No changes detected”; the shared stale index does not cover this worktree's report/evidence changes, so explicit Git diff/staging verifies scope instead. Original replicated-v4 artifacts are untouched. Diagnostic raw output is archived in `raw.zip` (contains `raw.txt`); patch, output row, summary and validation logs are retained separately.

### Remaining executable action / limit

The authorized one-diagnostic budget is exhausted. The exact historical trigger remains unresolved because that run lacks causal timestamps. If separately authorized, apply the preserved diagnostic patch on `2446867` and collect an OS/runtime scheduling trace alongside one fixed diagnostic, with per-ID execution/release and consumer timestamps, to distinguish producer catch-up from consumer descheduling/GC/host interference on a rejecting run. Do not run acceptance or alter requirements automatically. A requirement for zero loss under unbounded producer catch-up and unbounded consumer stalls cannot be guaranteed by a finite 4,096-slot queue; resolving that would require an explicit arrival/stall envelope or changed backpressure requirements, not silent workload tuning. Independently, the original CPU-resolution failure still blocks the frozen plan.

## Final disposition — planned STOP (2026-10-14)

**The overall requested implementation is INCOMPLETE. Task2 is NOT ACCEPTED. Tasks7/8/9 were NOT EXECUTED because the Task2 acceptance gate failed.** This commit preserves the existing implementation and measurement/failure evidence only. It grants **NO production approval, NO Task7/8 productionization, NO canary authorization and NO merge**. The user is unavailable to choose a new experiment; the conservative action is to stop, not reinterpret failed acceptance as completion. Earlier sections are chronological evidence, not current completion claims; their “no commit” statements describe those earlier runs.

The fixed v4 pilot remains INVALID for all three candidates (CPU below 3.125 seconds); bounded additionally lost 2,144 submissions in the nominal non-overload trajectory. No pilot rerun, replication retuning, threshold relaxation or full v4 comparison was performed during finalization. Passing regression tests validate harness behavior, not Task2 performance acceptance.

Final validation (fresh evidence in `tools/mailboxperf/results/final-stop/`):

- `go test ./engine/internal/perftest ./engine/pkg/actor/mailbox -run 'TestClock|TestCPUTime|TestMailboxPerfReplicationProtocol|TestMailboxPerformanceComparison|TestMailboxPerformanceRejects|TestMailboxPerformanceGateRequiresBaseline|TestPerfMemoryAccounting' -count=1 -v`: PASS, exit 0, both packages; the replicated protocol test actually ran.
- The same package/selector set with `-race -count=1`: PASS, exit 0, both packages.
- `tools/mailboxperf/summary_test.ps1`: PASS; valid matrix accepted and all six malformed cases rejected (protocol, baseline commit, duplicate round, missing/replaced scenario, metadata and round range).
- `go test -short ./... -count=1`: PASS, exit 0; 45 packages passed, 67 packages had no tests. Explicit performance experiments/gates remain opt-in; this is not execution of Task9 production validation.
- Editor diagnostics: no errors. Initial tracked `git diff --check`: PASS (CRLF conversion notices only). After staging previously untracked evidence, `git diff --cached --check` reports two existing generated-summary trailing blank lines (`corrected-v3/comparison.txt:34` and `review-fixes/summary/comparison.txt:34`); preserved verbatim rather than rewriting historical evidence. The staged code/report/script check excluding result artifacts passes.

Review covered the changed measurement code, QPC helpers, per-ID memory accounting, protocol and comparator regressions, and collection/summary scripts rather than bulk raw observations. No additional concrete code defect requiring a fix was identified. No production source was edited. Scripts are preserved experiment procedures; removed detached baseline worktrees must be explicitly recreated before any separately authorized future collection. Existing output directories are not permission to rerun or overwrite evidence.

GitNexus `detect_changes --scope all --repo emberengine` was executed before commit and returned **“No changes detected.”** This is a stale/shared main-worktree index limitation, **not** proof of zero changes or a LOW-risk result. Earlier impact queries returned UNKNOWN for the new harness symbols. Git diff and explicit staging are the authoritative scope check: only Task2 test/measurement helpers, scripts, evidence and this report; no production queue/mailbox source, briefs or untracked implementation plan. Owned summary-test scratch was cleaned by its `finally` block; no owned detached performance worktree remains. Unrelated/untracked briefs and plan are preserved and excluded, not deleted.

Remaining prerequisites: explicit authorization of a new measurement design; reliable actual process CPU exposure without artificial burn; investigation and resolution of the observed non-overload rejection without hiding loss; representative application workload and target-deployment attestation; complete compatible baseline/current/bounded matrix and unchanged acceptance thresholds; remaining memory/RSS, idle, overload/RW and platform evidence described below. Only a subsequently accepted Task2 gate permits reconsidering Tasks7/8, followed by Task9 validation and a separate production/merge decision. **Do not label all tasks complete.**

## Fixed replication experiment (2026-10-14, preregistration)

Using systematic debugging, brainstorming, writing-plans, TDD and Go testing. Autonomous scope follows the requested Task2 continuation; no approval or commit is implied. Impact query for `TestMailboxPerformanceSample` returned UNKNOWN/not found in the shared graph (see `results/replicated-v4/impact.txt`). Direct callers are Go's explicit test runner only; affected flow is test-only workload collection, not production. No graph-derived LOW claim.

Design decision: summing separately bracketed CPU deltas does **not** reliably amortize endpoint quantization (worst-case uncertainty grows with window count). Instead v4 brackets one uninterrupted block of **32 identical workload windows** with actual GetProcessTimes reads. A mailbox is warmed once, all message wrappers are preallocated before the CPU bracket, every window restarts the same offered schedule and drains, and sorting/reporting occurs after the bracket. Inter-window drain and loop overhead are included, not subtracted. This is a repeated-window estimand, not v3 single-window data; no cross-protocol pooling. Work, rate, capacity, routing and each window's message count remain unchanged. No CPU burning or adaptive repeats.

Frozen reliability rule: at least **3.125 seconds actual process CPU** per observation (200 observed 15.625ms quanta). Under a conservative two-quantum endpoint uncertainty model this is at most 1% exposure error; this is a resolution screen, not a statistical confidence guarantee or OS accounting accuracy proof. Low exposure is INVALID, never PASS. Performance thresholds remain .95/1.10/1.10/1.15 and mostly-empty 1.10.

Plan: (1) add tested fixed protocol selection and exposure validation; (2) preserve v3 entry behavior, add continuous 32-window v4 mode; (3) one fixed pilot, priority/w1/RW-off/hot-key steady (previous zero-CPU scenario), baseline73ecd28 then current unbounded then bounded, one observation each, identical helpers; (4) only if all three are resolved and loss-free, run full 15×3×5 matrix with the same 32 windows and rotating candidate order; otherwise stop without increasing replication or selecting a different pilot. Pilot is resolution feasibility only, excluded from any full comparison. Tests/race and owned scratch cleanup follow. Raw prior evidence stays untouched.


### Fixed replication outcome — STOP

One preregistered pilot was executed, no performance reruns or replication tuning. Exact source SHA256 parity was checked for all copied performance fixtures and perftest helpers (`results/replicated-v4/helpers.json`); detached baseline HEAD was verified before execution. CPU and latency use the same helpers across all candidates. Current source identity is HEAD a68ab9a plus the preserved uncommitted test changes, not a clean-commit attestation.

| Candidate | Actual CPU | 15.625ms quanta | Wall seconds | Completed / offered | Resolution |
|---|---:|---:|---:|---:|---|
| baseline 73ecd28 | 2.562500 s | 164 | 65.5288282 | 2,097,152 / 2,097,152 | INVALID |
| current unbounded | 2.562500 s | 164 | 65.5304507 | 2,097,152 / 2,097,152 | INVALID |
| current bounded | 3.015625 s | 193 | 65.6188003 | 2,095,008 / 2,097,152 | INVALID + LOSS |

All three exposures are below the frozen 3.125s minimum. Bounded additionally rejected **2,144 main-queue submissions** (0.10223%) in the nominal non-overload trajectory; it drained and conserved all messages, but the no-loss requirement fails. Its aggregate message p99 was 147.2µs and p999 131.4843ms; per-window quantiles are retained rather than averaging scenario percentiles. These are descriptive only, not five-round acceptance ratios. A scheduled workload can catch up in bursts after scheduling delays; this observation does not isolate a production root cause or excuse the rejection.

**Full comparison was not run**: the preregistered feasibility condition failed, and increasing windows or selecting another scenario would violate the one-experiment stop. Repetition accumulated real CPU without burning, but did not establish the required reliable exposure. The test-only implementation remains available; v3 comparators deliberately reject v4 rather than silently compare protocols. No v4 approval comparator is claimed. All message wrappers/latencies remain distinct until observation ends, preserving the existing job release/execution accounting; repetition increases fixture live memory and can influence GC, another reason not to mix it with v3. Per-window warmup is not repeated; this is one warmed mailbox with repeated offered windows, explicitly different from 32 independent process runs.

Evidence: `tools/mailboxperf/results/replicated-v4/` includes RED missing-helper build failure, GREEN protocol-boundary/invalid-mode and existing gate regression tests, immutable pilot JSON/logs, helper hashes, decision and summary CSV. Targeted clocks/protocol/gate/bounded-post/ownership tests passed under race; the initial queue selector matched no tests, so the complete boundedmpsc race suite was separately run and passed (`race-queue.txt`). The busy-loop clock unit test is a separate pre-existing helper diagnostic, never part of pilot CPU exposure. Editor diagnostics and `git diff --check` passed (CRLF notices only). The owned `.perf-replicated-baseline` worktree was removed; original worktrees/evidence and edits remain. No production edits, SequenceRing, commit, target-deployment attestation or approval. **Task2 productionization remains stopped** for insufficient measurement reliability and observed bounded non-overload loss, in addition to prior deployment/real-workload blockers.

## Bounded review fixes (2026-10-14)

Only the summary harness, memory fixture, regression tests and this report were changed in this follow-up; production code and thresholds are unchanged. Skills used: systematic-debugging, receiving-code-review, test-driven-development, golang-testing, GitNexus impact analysis and verification-before-completion. Upstream impact queries for `TestMailboxPerformanceMemory`, `perfMemoryInvoker`, `ExecuteJob`, `OnJobDiscarded`, `validatePerfMemoryStates`, `compareMailboxPerf` and `Median` returned **not found / UNKNOWN** in the shared main-worktree index. Direct inspection limits affected callers to the memory test and its invoker callbacks, accounting regression tests, and the PowerShell summary entry point; no production execution flow changes. This is not a graph-derived LOW-risk claim.

- `run.ps1 -SummarizeOnly` now validates the complete frozen 15-scenario × three-candidate × five-round matrix **before ratios**: rounds 0–4 exactly once, protocol `task2-v3-qpc`, explicit baseline `73ecd28`, consistent current commit, compatible CPU/OS/architecture/Go/GOMAXPROCS, scenario worker/RW/sample settings, conservation/drain and positive wall/success. PowerShell mirrors the Go comparator contract (the comparator lives in test-only Go code), additionally pins baseline identity and complete matrix coverage. CPU/latency resolution remains separately reported as invalid/failed screening, not silently accepted. Numerical thresholds are unchanged.
- `summary_test.ps1` demonstrated RED (wrong protocol accepted), then GREEN for valid input and rejection of wrong protocol, wrong baseline commit, duplicate round, replaced/missing scenario, mismatched machine metadata and out-of-range round. It uses isolated project-local files and cleans them. `-InputPath` permits fixture inputs; summaries now use a new directory, refuse overwrites, and never rewrite input JSON.
- Memory accounting preallocates 32,769 scalar-only ID records before the first heap point. Each submission owns its record; execute/discard/release callbacks increment atomic per-ID counters without retaining jobs or payloads. After drain and worker stop, each accepted ID must execute once, never discard, and release once; each rejected ID must never execute, discard once, and release once. Regressions demonstrated RED with the old aggregate-equivalent validator for compensating duplicate/missing execute, discard and release, and swapped acceptance disposition, then GREEN with per-ID validation. Constant accounting storage is included in all heap points; absolute heap values are not directly comparable to the old wrapper fixture.

New evidence is under `tools/mailboxperf/results/review-fixes/`: `summary-tests.txt`, `summary/comparison.{csv,txt}`, `targeted.txt`, `race.txt`, `memory-{unbounded,bounded}.json` and `memory-baseline-73ecd28.json` with matching logs. The raw 225 timing rows were reused; no performance sample was rerun. The 11 unresolved CPU rows and all previous deployment blockers remain.

Memory reruns: verified detached baseline HEAD `73ecd2818ad1e8c7c6153e48c88aa94fb5b20fb5`; baseline heap 2,582,336 → 7,568,520 bytes and current unbounded 2,579,216 → 7,558,136 as occupancy grows 0 → 32,768. Bounded occupancy stays 4,096 across full windows; heap 3,229,968 → 3,230,400 (432-byte drift), accepted 4,097/rejected 28,672. All three pass per-ID assertions; bounded also passes under race (`memory-bounded-race.json`). An initial baseline invocation used the current worktree because the detached directory was absent: `memory-baseline.{json,txt}` is **invalid baseline evidence and excluded**; it is retained for transparency and superseded only by the explicitly verified `-73ecd28` files.

Verification: targeted accounting/comparison/unresolved-latency/missing-baseline tests PASS; the same tests plus actual bounded memory fixture under `-race` PASS; all three uninstrumented memory fixtures PASS; PowerShell negative/positive summary tests PASS; editor diagnostics clear; `git diff --check` PASS (only Git CRLF notices). No commit. Production approval remains BLOCKED by CPU resolution, development-only synthetic workload, deployment attestation and previously documented instrumented overload; no threshold relaxation or further tuning attempted.


## Measurement debugging follow-up (2026-10-14)

The original sections and `results/` evidence below are preserved. Corrected latency observations use a distinct **task2-v3-qpc** protocol and `results/corrected-v3/`; they must not be mixed with v1/v2. No production source, workload, offered arrival rate, sample count, capacity or acceptance threshold was changed. Deployment approval remains **BLOCKED**.

### Demonstrated causes and harness corrections

- **Zero latency:** Go 1.24.9 Windows amd64 `runtime.nanotime1` reads `_INTERRUPT_TIME` (`GOROOT/src/runtime/sys_windows_amd64.s:279–284`), not QueryPerformanceCounter. The regression reproduced **0/1000** resolved 64-iteration work samples using `time.Now`/`time.Since`. The harness now uses QPC for message latency and queue call/admission/transit/rejection latency. On other OSes the helper retains Go monotonic time. QPC calls add measurement overhead; identical helpers are used on all three revisions/candidates. Arrival pacing and wall-window measurement remain unchanged. QPC is not used as a CPU substitute.
- **Zero CPU:** no conversion/handle defect found in `GetProcessTimes`: it reads the current process, adds user+kernel FILETIME ticks and multiplies by 100 ns. A fixed 500 ms busy-loop diagnostic returned 500 ms CPU before correction and 484.375 ms afterward. Recorded deltas are quantized in 15.625 ms steps. Sleep-heavy, short actual CPU workloads can therefore return zero even across a two-second wall window. This remains an external measurement-resolution blocker, not zero work. No artificial CPU burn, clamping, wall-clock replacement or success-until-pass repeats were added to the Mailbox workload.
- **False numerical acceptance:** the Go comparison accepted zero current p99/p999 as an improvement against nonzero baseline. A regression first failed, then passed after rejecting unresolved current latency. CSV screening now also requires every round's latency to resolve and verifies no loss; it no longer relies only on median baseline latency. Numerical thresholds are untouched.
- **Instrumented rejection:** original race evidence reports genuine main-capacity errors, not a data-race warning: 1,943–21,193 rejections in RW cases, execution windows 2.57–2.79 s versus a 2.047 s offered schedule. A fixed open-loop 32,000 jobs/s workload is not guaranteed non-overload with instrumentation. Deadline catch-up may compound bursts. Rejections are retained as failures; no retry, throttling, larger capacity or production change hides them. Targeted race correctness tests and the full instrumented sample are separate from numerical performance acceptance.

### Scope and impact

GitNexus upstream impact was invoked before editing existing harness symbols (`TestMailboxPerformanceSample`, `perfInvoker.ExecuteJob`, `TestQueueLatency`, comparison helpers; CPUTime queried but unchanged). Results are retained in `results/debug-impact.txt`: **not found / UNKNOWN**, because the registered graph belongs to the main worktree at `73ecd28` (indexed 2026-08-28), not this branch's new harness. There is no graph-derived LOW-risk claim. Direct source review bounds changes to test entry points and `engine/internal/perftest`, their shared measurement helper; no production execution flow or queue implementation is edited. Reindexing the shared main-worktree graph would not validate these worktree-only symbols.

### Reproduction commands

From this worktree:

```powershell
go test ./engine/internal/perftest -count=1 -v
go test ./engine/pkg/actor/mailbox -run 'TestMailboxPerformanceComparison|TestMailboxPerformanceRejectsUnresolvedCurrentLatency|TestMailboxPerformanceGateRequiresBaseline' -count=1 -v
.\tools\mailboxperf\run.ps1
.\tools\mailboxperf\debug-verify.ps1
```

`run.ps1` refuses to overwrite corrected evidence. It copies the compatible fixture/helper into detached `73ecd28`, uses five rotated rounds and the frozen v2 workload, and preserves failed samples. `debug-verify.ps1` separately collects three-candidate controlled-backlog heap evidence, targeted race tests, bounded recovery under race, and the unchanged full bounded arrival schedule under race. Original red/green diagnostics are `results/debug-clock-{red,green}.txt` and `results/debug-gate-{red,green}.txt`.


### Corrected observations and bounded stop

Five rotated rounds completed: **225 observations**, baseline `73ecd28` versus current `a68ab9a` (production source unchanged from `4100678`), all using identical QPC latency logic. `fixture-hashes.csv` records exact copies: Mailbox fixture, CPU helper and memory fixture match byte-for-byte; QPC file hashes differ because current was gofmt-formatted after baseline copying (whitespace/import ordering only). No timing logic changed during collection. The validator/runner summary corrections were completed during collection, then `run.ps1 -SummarizeOnly` recomputed the CSV from the unchanged raw observations without rerunning samples. All 15 uninstrumented sample processes passed conservation/non-overload loss checks. **Zero p99: 0/225. Zero CPU: 11/225** (baseline 1, current unbounded 5, bounded 5). These unresolved CPU observations remain invalid; numerical screening and the executable gate cannot approve them. No further workload tuning was attempted.

Examples from corrected medians (descriptive development-machine results, not deployment claims):

| Scenario / candidate | CPU/success ratio | p99 ratio | Outcome |
|---|---:|---:|---|
| dual / 1 worker / uniform / bounded | 1.25 | 1.031 | CPU threshold failure |
| dual / 4 workers / uniform / RW-off / bounded | 1.50 | 0.929 | CPU threshold failure |
| priority / 4 workers / uniform / RW-on / unbounded | 1.944 | 1.079 | CPU threshold failure |
| priority / 4 workers / uniform / RW-on / bounded | 1.889 | 0.577 | CPU threshold failure |
| dual / 4 workers / MostlyEmpty / bounded | 1.00 | 0.996 | Numerical screening only |

Bounded finite-overload successes: **20,511 / 19,727 / 22,720 / 18,787 / 22,489**; main rejections **45,025 / 45,809 / 42,816 / 46,749 / 43,047** (65–71%). Each round conserves 65,536 and drains. Original and corrected failures are both retained; improved MostlyEmpty screening does not replace the original failure or authorize canary.

### Windows overload memory evidence

The separate identical three-candidate fixture holds one real worker inside execution, then offers eight windows of 4,096 additional real jobs to one hot user queue. Jobs are allocated per submission rather than preallocated/retained by the harness. Each point records exact queued occupancy plus post-GC process-wide live Go heap. The blocked worker is released afterward and all accepted jobs execute; rejected jobs are discarded/released exactly once.

- Old baseline: occupancy 0 → **32,768**, live heap **1,525,744 → 7,031,128 bytes**; accepted 32,769, rejected 0.
- Current unbounded: occupancy 0 → **32,768**, live heap **1,541,848 → 7,044,976 bytes**; accepted 32,769, rejected 0.
- Bounded: occupancy stays at **4,096** from window 1 through 8; live heap **2,246,144 → 2,251,728 bytes** across those full windows (5,584 bytes drift); accepted 4,097 including the in-flight job, rejected 28,672. Recovery/conservation passed, including under race.

This is controlled main-queue capacity/retained-heap plateau evidence on Windows, **not RSS**, not a long-running concurrent steady overload experiment, not every queue layout, and not deployment attestation. Full per-window data is in `results/debug-verification/memory-*.json`.

### Final verification and limitations

- Final three-package `go test ... -short -count=1`: PASS (`debug-verification/tests-final.txt`).
- Clock regression red→green; sustained process-CPU diagnostic: PASS. CPU helper remained unchanged.
- Zero-current-latency comparison regression red→green; existing validator tests: PASS.
- Corrected `TestQueueLatency`, five internally rotated rounds: PASS (`queue-latency.txt`).
- Targeted `-race` on measurement helpers, queue latency, bounded PostJob lifecycle and comparison/missing-baseline tests: PASS; bounded controlled-memory recovery under race: PASS.
- Full bounded fixed-arrival `-race` sample: **FAIL**, four RW scenarios with main rejections **8,599 / 22,549 / 4,512 / 16,519**; no `WARNING: DATA RACE` emitted. All drain/conserve. This confirms instrumentation turns the fixed arrival schedule into overload; it is not fixed by changing the production queue or hiding rejected jobs.
- Corrected executable production gate: expected **FAIL**, `invalid baseline metadata or CPU resolution`; later attestation requirements remain unsatisfied.
- `git diff --check`: PASS. No commit made; no production code/SequenceRing changes. The scratch baseline worktree created for this run is removed after retaining source/helper hashes and all evidence.

**Bounded stop:** latency defects and false acceptance are corrected and regression-tested. CPU quantization remains substantiated on this OS/workload; target-machine attestation, representative business load, longer isolated CPU windows and broader overload/RW/stop scenarios remain external/follow-up blockers. Retain default unbounded and no canary approval.

## Original report (preserved)

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

## 2026-10-14: retained-fixture hypothesis / distinct task2-v5-window protocol

Evidence is additive in `tools/mailboxperf/results/window-v5`; v3/v4 and rejection diagnostics are unchanged. No random rejection reproduction was attempted. Required upstream impact on the freshly indexed worktree: `TestMailboxPerformanceSample` LOW (zero callers/flows), `mailboxPerfReplication` LOW (two test callers, zero flows); no production symbols changed.

**Hypothesis demonstrated, stall attribution not demonstrated.** The v4 slice roots every perfJob, each of which roots its real pooled EventBusJob even after Release; before submission all windows' Jobs are simultaneously checked out. Event payload is nil in this workload. The initial allocation therefore changes pool occupancy, live pointer graph, heap goal and GC work, not merely accumulated CPU. Deterministic 65,536-job diagnostic, GOMAXPROCS=4:

| Windows | Jobs retained | Retained bytes above clean heap | Allocated bytes | Live object delta | Forced GC wall |
|---|---:|---:|---:|---:|---:|
| 1 | 65,536 | 9,964,048 | 10,484,872 | 131,090 | 1.026 ms |
| 4 | 262,144 | 39,847,392 | 41,925,064 | 524,297 | 14.012 ms |
| 32 | 2,097,152 | 318,768,784 | 335,354,512 | 4,194,315 | 48.630 ms |

This is essentially 32x retention (~304 MiB versus ~9.5 MiB), violating the intended repeated-identical-window interpretation. Both baseline/current unbounded and bounded shared v4's defect; identical source alone does not remove different allocation/pool/collector interactions across candidates. V4 remains evidence for that specific large-live-heap workload, not a clean replication of v3. New/old protocols must never be combined for ratios or gate decisions.

`legacy-gc.txt` confirms up to ~40 ms concurrent marking and 44.4 ms forced-GC wall at 32 windows. ReadMemStats pause fields initially returned zero; a separate deterministic trace recorded 511 us at four windows. Zero reported pause is not proof of zero scheduling interruption. These are NOT 194 ms STW measurements: concurrent mark duration and runtime.GC wall include non-STW work. Heap pressure is a plausible contributor to scheduling/assist effects, but no causal explanation of the historical 194 ms stall is established.

**Design implemented:** task2-v5-window keeps n reusable wrappers, acquires real pooled Jobs for exactly one window, drains through the post-Release atomic counter, copies scalar latency/executed results, drops stale Job references, and only then prepares the next window. Underlying Jobs are reacquired from the pool, never manually resurrected. No nonnil Event payload exists to reuse. Preparation rejects active windows; collection rejects incomplete/double collection. Per-window key/RW mapping, offered deadlines/batch/tick, CPU loop, resize transitions, warmup and capacities remain identical. O(n*windows) scalar durations (16 MiB for the fixed pilot) are intentionally retained for exact message-level pooled percentiles, not percentiles of window percentiles. The window quantiles remain diagnostic only.

Accounting is explicit: wrapper/scalar-buffer setup remains outside row CPU/wall, like fixture setup in v4; every per-window Job/key acquisition, reset, collection and window sort is INSIDE row CPU/wall for ALL candidates. There is no disabling GC or moving per-window allocation off-clock. This changed accounting is another reason v5 cannot be compared numerically with v4. Setup/live allocation is reported by the standalone diagnostic rather than silently treated as zero cost. Final pooled sorting remains outside row timing in both protocols.

V5 deterministic diagnostic (including the fixed full scalar buffer): retained bytes at windows 1/4/32 were 28,843,136 / 28,843,424 / 28,839,200; wrappers stayed 65,536, live objects ~131,100. Total allocation through 32 windows was 51,737,400 bytes; reuse is not claimed allocation-free. Forced GC wall was ~2 ms. Runtime metrics reported heap scan 12,215,680 bytes and GC CPU 0.02594 s; pause histogram included one [458.752,524.288) us bucket. Forced GC is diagnostic-only, never used in the pilot.

**Fixed same-candidate pilot, once only:** priority-w1-rwfalse-hottrue-steady, bounded, 32 windows, GOMAXPROCS=4, normal GC with gctrace. 2,097,152/2,097,152 completed, zero rejection, drained; wall 65.84785 s; CPU 2.640625 s (169 quanta), below unchanged 3.125 s minimum. P50/P99/P999 = 8.9/20.7/43.1 us. Application GC trace (after RUN, not compiler traces) stabilizes at ~29 MiB live, ~2 ms concurrent marking. This supports bounded-retention validity, NOT acceptance or proof the historical stall was GC. CPU resolution still fails; no additional pilot or full comparison was run. `window-pilot.ps1` preserves that exact invocation and refuses overwrite.

Validation: new protocol regression failed first; ownership regression initially failed to compile before helper implementation. Targeted tests passed; short perftest/mailbox/boundedmpsc suites passed; race tests passed including 32 real-mailbox windows for both backends. Initial diagnostic integration test used a missing bounded capacity config and failed; its raw output is retained, configuration corrected to the unchanged 4096 capacities, subsequent race/full-suite results passed. No production, queue capacity, threshold, default-backend or acceptance gate changes.

**Next justified action:** retain the invalid-CPU decision. A separately preregistered measurement protocol using a sufficiently resolved process CPU source on controlled hardware is needed before any matched baseline/current/bounded comparison. Do not extend windows opportunistically or interpret this pilot as project completion, deployment approval, or proof of stall causality.
