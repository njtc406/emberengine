$ErrorActionPreference = 'Stop'
$source = Get-Content "$PSScriptRoot\results\corrected-v3\all.json" -Raw
$work = Join-Path $PSScriptRoot ('test-summary-' + [guid]::NewGuid())
New-Item -ItemType Directory $work | Out-Null
try {
  foreach ($case in @('valid','protocol','commit','duplicate','missing','metadata','round')) {
    $rows = @($source | ConvertFrom-Json)
    switch ($case) {
      protocol { $rows[0].Protocol = 'task2-v2' }
      commit { ($rows | Where-Object Candidate -eq baseline)[0].Machine.Commit = 'wrong' }
      duplicate { $rows[0].Round = 1 }
      missing { $name=$rows[0].Scenario; foreach ($r in $rows) { if ($r.Scenario -eq $name) { $r.Scenario='unknown' } } }
      metadata { $rows[0].Machine.CPU = 'other' }
      round { $rows[0].Round = 5 }
    }
    $inputFile = Join-Path $work "$case.json"
    $rows | ConvertTo-Json -Depth 10 | Set-Content $inputFile
    $failed = $false
    try { & "$PSScriptRoot\run.ps1" -SummarizeOnly -InputPath $inputFile -SummaryDirectory (Join-Path $work $case) } catch { $failed=$true; Write-Output "$case rejected: $_" }
    if ($failed -ne ($case -ne 'valid')) { throw "Unexpected validation result: $case rejected=$failed" }
  }
} finally { Remove-Item $work -Recurse -Force }
