$ErrorActionPreference='Stop'
$out=Join-Path $PSScriptRoot 'results\fixed128-v5'
$attempts=@(Get-Content "$out\attempts.json" -Raw | ConvertFrom-Json)
if($attempts.Count -ne 15){throw 'Incomplete fixed budget'}
$reference=@(Get-Content "$out\baseline-0.json" -Raw | ConvertFrom-Json)[0].Machine
$seen=@{}
$summary=foreach($a in $attempts){
 $key="$($a.Candidate)-$($a.Round)"
 if($seen.ContainsKey($key)){throw 'Duplicate attempt'}; $seen[$key]=$true
 $rows=@(Get-Content "$out\$key.json" -Raw | ConvertFrom-Json)
 if($rows.Count -ne 1){throw 'Wrong row count'}
 $r=$rows[0]
 $metadata=$true
 foreach($f in @('CPU','OS','Arch','Go','Procs')){if($r.Machine.$f -cne $reference.$f){$metadata=$false}}
 $commit=if($a.Candidate -eq 'baseline'){'73ecd2818ad1e8c7c6153e48c88aa94fb5b20fb5'}else{'5b703f3e675e3e83c03ccd91fee8fb516cdac4a5'}
 $valid=$metadata -and $r.Machine.Commit -ceq $commit -and $r.Protocol -ceq 'task2-v5-window-fixed128' -and $r.Candidate -ceq $a.Candidate -and $r.Scenario -ceq 'priority-w1-rwfalse-hottrue-steady' -and $r.Workers -eq 1 -and $r.EffectiveRW -eq $false -and $r.Windows -eq 128 -and $r.WindowCompletions.Count -eq 128 -and $r.Samples -eq 8388608 -and $r.Success -eq $r.Samples -and $r.MainRejected -eq 0 -and $r.RWDropped -eq 0 -and $r.StopRejected -eq 0 -and $r.OtherRejected -eq 0 -and $r.Drained -and $r.CPUResolved -and $r.CPU -ge 3125000000 -and $r.Wall -gt 0 -and $a.Exit -eq 0
 [pscustomobject]@{Candidate=$a.Candidate;Round=$a.Round;CPUSeconds=$r.CPU/1e9;WallSeconds=$r.Wall/1e9;Success=$r.Success;Rejected=$r.MainRejected+$r.RWDropped+$r.StopRejected+$r.OtherRejected;CPUResolved=$r.CPUResolved;Drained=$r.Drained;Exit=$a.Exit;Valid=$valid;CPUPerSuccess=$r.CPU/$r.Success}
}
$summary | Export-Csv "$out\summary.csv" -NoTypeInformation
function Median($values){$v=@($values|Sort-Object);$v[[int][math]::Floor($v.Count/2)]}
$base=@($summary|Where-Object Candidate -eq baseline)
$comparisons=foreach($c in @('unbounded','bounded')){
 $rows=@($summary|Where-Object Candidate -eq $c)
 $valid=$base.Count -eq 5 -and $rows.Count -eq 5 -and @($base+$rows|Where-Object {!$_.Valid}).Count -eq 0
 $ratio=if($valid){(Median $rows.CPUPerSuccess)/(Median $base.CPUPerSuccess)}else{$null}
 $paired=if($valid){foreach($r in $rows){$b=$base|Where-Object Round -eq $r.Round; $r.CPUPerSuccess/$b.CPUPerSuccess}}else{@()}
 [pscustomobject]@{Candidate=$c;Valid=$valid;CPUPerSuccessRatio=$ratio;Limit=1.10;Pass=($valid -and $ratio -le 1.10);PairedRatios=@($paired)}
}
$comparisons|ConvertTo-Json -Depth 5|Set-Content "$out\comparison.json"
$summary|Format-Table -AutoSize
$comparisons|ConvertTo-Json -Depth 5
