$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$out = Join-Path $PSScriptRoot 'results\fixed128-v5'
$baseline = Join-Path $root '.perf-fixed128-baseline'
if (Test-Path "$out\started.txt") { throw 'Fixed budget already started; no reruns' }
if (Test-Path $baseline) { throw 'Baseline path already exists' }
(Get-Date).ToUniversalTime().ToString('o') | Set-Content "$out\started.txt"
Get-FileHash $PSCommandPath,"$out\protocol.md" -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json | Set-Content "$out\registration-hashes.json"
git worktree add --detach $baseline 73ecd28
if ($LASTEXITCODE) { throw 'Baseline isolation failed' }
try {
 if ((git -C $baseline rev-parse HEAD) -cne '73ecd2818ad1e8c7c6153e48c88aa94fb5b20fb5') { throw 'Wrong baseline' }
 New-Item -ItemType Directory -Force "$baseline\engine\internal\perftest" | Out-Null
 Copy-Item "$root\engine\internal\perftest\*.go" "$baseline\engine\internal\perftest"
 Copy-Item "$root\engine\pkg\actor\mailbox\mailbox_performance*_test.go" "$baseline\engine\pkg\actor\mailbox"
 $files = @(Get-ChildItem "$root\engine\internal\perftest\*.go") + @(Get-ChildItem "$root\engine\pkg\actor\mailbox\mailbox_performance*_test.go")
 $manifest = foreach ($f in $files) {
  $relative = $f.FullName.Substring($root.Length+1)
  $hash = (Get-FileHash $f.FullName -Algorithm SHA256).Hash
  if ($hash -ne (Get-FileHash (Join-Path $baseline $relative) -Algorithm SHA256).Hash) { throw 'Fixture mismatch' }
  [pscustomobject]@{Path=$relative;SHA256=$hash}
 }
 $manifest | ConvertTo-Json | Set-Content "$out\helpers.json"
 $env:GOMAXPROCS='4'; $env:MAILBOX_PERF_SAMPLE='1'; $env:MAILBOX_PERF_PROTOCOL='task2-v5-window-fixed128'; $env:GODEBUG='gctrace=1'
 $env:PERF_CPU_MODEL=(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)
 $current=git rev-parse HEAD
 $attempts=@()
 for ($round=0; $round -lt 5; $round++) {
  $names=@('baseline','unbounded','bounded')
  for ($offset=0; $offset -lt 3; $offset++) {
   $candidate=$names[($round+$offset)%3]
   $env:PERF_CANDIDATE=$candidate
   $env:PERF_COMMIT=if($candidate -eq 'baseline'){'73ecd2818ad1e8c7c6153e48c88aa94fb5b20fb5'}else{$current}
   $env:PERF_OUTPUT=Join-Path $out "$candidate-$round.json"
   if(Test-Path $env:PERF_OUTPUT){throw 'Refusing overwrite'}
   $source=if($candidate -eq 'baseline'){$baseline}else{$root}
   Push-Location $source
   try {
    go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$/^priority-w1-rwfalse-hottrue-steady$' -count=1 -timeout 600s -v *> "$out\$candidate-$round.txt"
    $exit=$LASTEXITCODE
   } finally {Pop-Location}
   $attempts += [pscustomobject]@{Candidate=$candidate;Round=$round;Exit=$exit;Finished=(Get-Date).ToUniversalTime().ToString('o')}
   $attempts | ConvertTo-Json | Set-Content "$out\attempts.json"
   Write-Output "Completed $candidate round $round exit $exit"
  }
 }
} finally {
 git worktree remove --force $baseline 2>&1 | Set-Content "$out\cleanup.txt"
 if($LASTEXITCODE){throw 'Baseline cleanup failed'}
}
