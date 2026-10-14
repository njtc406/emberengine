$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$out = Join-Path $PSScriptRoot 'results\trace128-diagnostic'
if (Test-Path $out) { throw 'One diagnostic only; refusing repeat or overwrite' }
New-Item -ItemType Directory $out | Out-Null
(Get-Date).ToUniversalTime().ToString('o') | Set-Content "$out\started.txt"
git rev-parse HEAD | Set-Content "$out\parent.txt"
Get-FileHash "$root\engine\pkg\actor\mailbox\mailbox_performance*_test.go",$PSCommandPath -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json | Set-Content "$out\helpers.json"
$env:GOMAXPROCS='4'
$env:MAILBOX_PERF_SAMPLE='1'
$env:MAILBOX_PERF_PROTOCOL='task2-v5-window-fixed128'
$env:PERF_CANDIDATE='bounded'
$env:PERF_COMMIT=git rev-parse HEAD
$env:PERF_CPU_MODEL=(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)
$env:PERF_OUTPUT=Join-Path $out 'row.json'
$env:MAILBOX_PERF_FLIGHT=Join-Path $out 'flight.json'
$env:GODEBUG='gctrace=1,schedtrace=1000'
$env:MAILBOX_PERF_TRACE=Join-Path $out 'runtime.trace'
go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$/^priority-w1-rwfalse-hottrue-steady$' -count=1 -timeout 600s -v *> "$out\raw.txt"
$code=$LASTEXITCODE
[pscustomobject]@{Exit=$code;Finished=(Get-Date).ToUniversalTime().ToString('o');Acceptance=$false;Attempts=1} | ConvertTo-Json | Set-Content "$out\attempt.json"
Write-Output "Single diagnostic completed: exit=$code (loss is retained, no retry)"
