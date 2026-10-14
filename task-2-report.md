# Task 2 performance report

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
