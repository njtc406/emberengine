$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
Set-Location $root
$out = Join-Path $PSScriptRoot 'results\replicated-v4'
$baseline = Join-Path $root '.perf-replicated-baseline'
if ((git -C $baseline rev-parse HEAD) -cne '73ecd2818ad1e8c7c6153e48c88aa94fb5b20fb5') { throw 'Wrong baseline checkout' }
New-Item -ItemType Directory -Force "$baseline\engine\internal\perftest" | Out-Null
Copy-Item "$root\engine\internal\perftest\*.go" "$baseline\engine\internal\perftest"
Copy-Item "$root\engine\pkg\actor\mailbox\mailbox_performance*_test.go" "$baseline\engine\pkg\actor\mailbox"
$files = @(Get-ChildItem "$root\engine\internal\perftest\*.go") + @(Get-ChildItem "$root\engine\pkg\actor\mailbox\mailbox_performance*_test.go")
$manifest = foreach ($file in $files) {
 $relative = $file.FullName.Substring($root.Length+1)
 $hash = (Get-FileHash $file.FullName -Algorithm SHA256).Hash
 if ($hash -ne (Get-FileHash (Join-Path $baseline $relative) -Algorithm SHA256).Hash) { throw 'Helper mismatch' }
 [pscustomobject]@{Path=$relative;SHA256=$hash}
}
$manifest | ConvertTo-Json | Set-Content "$out\helpers.json"
$env:GOMAXPROCS='4'
$env:PERF_CPU_MODEL=(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)
$env:MAILBOX_PERF_SAMPLE='1'
$env:MAILBOX_PERF_PROTOCOL='task2-v4-replicated'
$current=git rev-parse HEAD
$results=@()
foreach ($candidate in @('baseline','unbounded','bounded')) {
 $env:PERF_CANDIDATE=$candidate
 $env:PERF_COMMIT=if($candidate -eq 'baseline'){'73ecd28'}else{$current}
 $env:PERF_OUTPUT=Join-Path $out "pilot-$candidate.json"
 if(Test-Path $env:PERF_OUTPUT){throw 'Refusing to overwrite pilot'}
 $source=if($candidate -eq 'baseline'){$baseline}else{$root}
 Push-Location $source
 try {
  go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$/^priority-w1-rwfalse-hottrue-steady$' -count=1 -timeout 240s -v 2>&1 | Tee-Object "$out\pilot-$candidate.txt"
  $exit=$LASTEXITCODE
 } finally {Pop-Location}
 $rows=@(Get-Content $env:PERF_OUTPUT -Raw | ConvertFrom-Json)
 if($rows.Count -ne 1){throw 'Invalid pilot count'}
 $row=$rows[0]
 $valid=$exit -eq 0 -and $row.Protocol -ceq 'task2-v4-replicated' -and $row.Windows -eq 32 -and $row.Samples -eq 2097152 -and $row.Success -eq $row.Samples -and $row.Drained -and $row.CPUResolved -and $row.CPU -ge 3125000000
 $results += [pscustomobject]@{Candidate=$candidate;CPUSeconds=$row.CPU/1e9;Quanta=$row.CPU/15625000;WallSeconds=$row.Wall/1e9;Success=$row.Success;Samples=$row.Samples;Resolved=$valid;Exit=$exit}
}
$results | Export-Csv "$out\pilot-summary.csv" -NoTypeInformation
$results | Format-Table -AutoSize
if(@($results | Where-Object {!$_.Resolved}).Count){'INVALID: fixed pilot insufficient; full comparison prohibited, no retuning.' | Set-Content "$out\decision.txt"}else{'VIABLE: full frozen comparison required; pilot is not acceptance.' | Set-Content "$out\decision.txt"}
Get-Content "$out\decision.txt"
