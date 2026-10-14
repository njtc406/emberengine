$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$out = Join-Path $PSScriptRoot 'results\debug-verification'
if (Test-Path $out) { throw 'Refusing to overwrite verification evidence' }
New-Item -ItemType Directory $out | Out-Null
$env:GOMAXPROCS='4'
$env:PERF_CPU_MODEL=(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)
Remove-Item Env:MAILBOX_PERF_SAMPLE -ErrorAction SilentlyContinue
Copy-Item engine\pkg\actor\mailbox\mailbox_performance_memory_test.go .perf-baseline\engine\pkg\actor\mailbox
foreach ($candidate in @('baseline','unbounded','bounded')) {
 $env:MAILBOX_PERF_MEMORY='1'
 $env:PERF_CANDIDATE=$candidate
 $env:PERF_MEMORY_OUTPUT=Join-Path $out "memory-$candidate.json"
 $source=if($candidate -eq 'baseline'){Join-Path $root '.perf-baseline'}else{$root}
 Push-Location $source
 try { go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceMemory$' -count=1 -v -timeout 60s 2>&1 | Tee-Object (Join-Path $out "memory-$candidate.txt") } finally {Pop-Location}
 if($LASTEXITCODE){throw "memory failed: $candidate"}
}
Remove-Item Env:MAILBOX_PERF_MEMORY
Remove-Item Env:PERF_MEMORY_OUTPUT
$env:PERF_COMMIT=git rev-parse --short HEAD
go test -race ./engine/internal/perftest ./engine/pkg/utils/boundedmpsc ./engine/pkg/actor/mailbox -run 'TestClock|TestCPUTime|TestQueueLatency|TestMailboxPerformanceComparison|TestMailboxPerformanceRejects|TestMailboxPerformanceGateRequiresBaseline|TestPostJobBounded' -count=1 -timeout 180s -v 2>&1 | Tee-Object "$out\race-targeted.txt"
if($LASTEXITCODE){throw 'targeted race failed'}
$env:MAILBOX_PERF_MEMORY='1'
go test -race ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceMemory$' -count=1 -timeout 90s -v 2>&1 | Tee-Object "$out\race-memory.txt"
if($LASTEXITCODE){throw 'memory race failed'}
Remove-Item Env:MAILBOX_PERF_MEMORY
$env:MAILBOX_PERF_SAMPLE='1'
$env:PERF_OUTPUT=Join-Path $out 'race-sample.json'
go test -race ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$' -count=1 -timeout 180s -v 2>&1 | Tee-Object "$out\race-sample.txt"
$sampleExit=$LASTEXITCODE
Remove-Item Env:MAILBOX_PERF_SAMPLE
"Instrumented sample exit=$sampleExit (loss remains a failure; not performance comparison data)" | Tee-Object "$out\race-sample-exit.txt"
