$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$baseline = Join-Path $root '.perf-baseline'
if (!(Test-Path $baseline)) { git worktree add --detach $baseline 73ecd28; if ($LASTEXITCODE) { throw 'baseline checkout failed' } }
if ((git -C $baseline rev-parse --short HEAD) -ne '73ecd28') { throw 'wrong baseline' }
New-Item -ItemType Directory -Force "$baseline\engine\internal\perftest" | Out-Null
Copy-Item "$root\engine\internal\perftest\*.go" "$baseline\engine\internal\perftest"
Copy-Item "$root\engine\pkg\actor\mailbox\mailbox_performance*_test.go" "$baseline\engine\pkg\actor\mailbox"
$results = Join-Path $PSScriptRoot 'results'
New-Item -ItemType Directory -Force $results | Out-Null
$env:GOMAXPROCS = '4'
$env:PERF_CPU_MODEL = (Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)
$env:MAILBOX_PERF_SAMPLE = '1'
$current = git rev-parse --short HEAD
$all = @()
for ($round=0; $round -lt 5; $round++) {
  $names = @('baseline','unbounded','bounded')
  for ($offset=0; $offset -lt 3; $offset++) {
    $candidate = $names[($round+$offset)%3]
    $env:PERF_CANDIDATE = $candidate
    $env:PERF_COMMIT = if ($candidate -eq 'baseline') { '73ecd28' } else { $current }
    $env:PERF_OUTPUT = Join-Path $results "$candidate-$round.json"
    $source = if ($candidate -eq 'baseline') { $baseline } else { $root }
    Push-Location $source
    try {
      go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$' -count=1 -timeout 180s 2>&1 | Tee-Object (Join-Path $results "$candidate-$round.txt")
      $exit = $LASTEXITCODE
    } finally { Pop-Location }
    if (Test-Path $env:PERF_OUTPUT) {
      $rows = Get-Content $env:PERF_OUTPUT -Raw | ConvertFrom-Json
      foreach ($row in $rows) { $row.Round=$round; $all += $row }
      $rows | ConvertTo-Json -Depth 10 | Set-Content $env:PERF_OUTPUT
    }
    if ($exit) { throw "sample failed: $candidate round $round" }
  }
}
$all | ConvertTo-Json -Depth 10 | Set-Content "$results\all.json"
$all | Where-Object Candidate -eq baseline | ConvertTo-Json -Depth 10 | Set-Content "$results\baseline.json"
function Median($values) { $v=@($values | Sort-Object); return $v[[int][math]::Floor($v.Count/2)] }
$comparison = foreach ($scenario in ($all.Scenario | Sort-Object -Unique)) {
  $base=@($all | Where-Object { $_.Scenario -eq $scenario -and $_.Candidate -eq 'baseline' })
  foreach ($candidate in @('unbounded','bounded')) {
    $rows=@($all | Where-Object { $_.Scenario -eq $scenario -and $_.Candidate -eq $candidate })
    $throughput=(Median ($rows | ForEach-Object { $_.Success/$_.Wall })) / (Median ($base | ForEach-Object { $_.Success/$_.Wall }))
    $resolved = @($base + $rows | Where-Object { $_.CPU -le 0 }).Count -eq 0
    $cpu = if ($resolved) { (Median ($rows | ForEach-Object { $_.CPU/$_.Success })) / (Median ($base | ForEach-Object { $_.CPU/$_.Success })) } else { [double]::NaN }
    $latencyResolved = (Median $base.Completion.P99) -gt 0 -and (Median $base.Completion.P999) -gt 0
    $p99 = if ($latencyResolved) {(Median $rows.Completion.P99)/(Median $base.Completion.P99)} else {[double]::NaN}
    $p999 = if ($latencyResolved) {(Median $rows.Completion.P999)/(Median $base.Completion.P999)} else {[double]::NaN}
    $idleCPU = if ($resolved) { (Median $rows.CPU)/(Median $base.CPU) } else { [double]::NaN }
    [pscustomobject]@{Scenario=$scenario;Candidate=$candidate;ThroughputRatio=$throughput;CPUPerSuccessRatio=$cpu;P99Ratio=$p99;P999Ratio=$p999;IdleCPURatio=$idleCPU;CPUResolved=$resolved;LatencyResolved=$latencyResolved;Pass=(!$scenario.Contains('overload') -and $latencyResolved -and $resolved -and $throughput -ge .95 -and $cpu -le 1.10 -and $p99 -le 1.10 -and $p999 -le 1.15 -and (!$scenario.Contains('mostly-empty') -or $idleCPU -le 1.10));RejectionRatio=(Median $rows.RejectionRatio)}
  }
}
$comparison | Export-Csv "$results\comparison.csv" -NoTypeInformation
$comparison | Format-Table -AutoSize | Out-String -Width 240 | Set-Content "$results\comparison.txt"
Remove-Item Env:MAILBOX_PERF_SAMPLE -ErrorAction SilentlyContinue
Write-Output 'Development measurements only; NOT production approval. Overload ratios are descriptive, not steady-state gates.'
