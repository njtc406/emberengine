$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$out = Join-Path $PSScriptRoot 'results\window-v5'
if (Test-Path "$out\pilot-bounded.json") { throw 'Fixed pilot already exists; no retuning or overwrite' }
$env:GOMAXPROCS = '4'
$env:MAILBOX_PERF_SAMPLE = '1'
$env:MAILBOX_PERF_PROTOCOL = 'task2-v5-window'
$env:PERF_CANDIDATE = 'bounded'
$env:PERF_COMMIT = git rev-parse HEAD
$env:PERF_CPU_MODEL = (Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)
$env:PERF_OUTPUT = Join-Path $out 'pilot-bounded.json'
$env:GODEBUG = 'gctrace=1'
go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$/^priority-w1-rwfalse-hottrue-steady$' -count=1 -timeout 240s -v 2>&1 | Tee-Object "$out\pilot-bounded.txt"
$exit = $LASTEXITCODE
$row = @(Get-Content $env:PERF_OUTPUT -Raw | ConvertFrom-Json)[0]
if ($exit -ne 0 -or !$row.Drained -or $row.Success -ne $row.Samples -or !$row.CPUResolved) {
 'INVALID for comparison: no full comparison, no retuning.'
} else { 'Measurement pilot only; not acceptance or approval.' }
