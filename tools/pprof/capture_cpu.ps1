param(
  [string]$RemoteHost = '127.0.0.1',
  [int]$BenchTotal = 20000000,
  [int]$BenchConcurrency = 500,
  [int]$NatsSenderPool = 0,
  [int]$ProfileSeconds = 10,
  [int]$WarmupSeconds = 2,
  [int]$WaitBenchSeconds = 180
)

$ErrorActionPreference = 'Stop'
Set-Location (Split-Path -Parent $PSScriptRoot) | Out-Null
Set-Location .. | Out-Null

$ts = Get-Date -Format 'yyyyMMdd_HHmmss'
$outDir = Join-Path $PWD 'example\data\pprof\cpu'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

function Get-ListeningPid([int]$port) {
  $line = (netstat -ano | findstr (":${port} ") | findstr 'LISTENING' | Select-Object -First 1)
  if (-not $line) { return $null }
  $parts = ($line -split '\s+')
  return [int]$parts[-1]
}

function Kill-ByPort([int]$port) {
  $procId = Get-ListeningPid $port
  if ($procId) {
    Write-Host "Stopping PID=$procId on port $port"
    cmd /c "taskkill /PID $procId /F" | Out-Null
    Start-Sleep -Milliseconds 300
  }
}

# Keep receiver running (6061). Only ensure sender port is free.
Kill-ByPort 6060

$senderLog = Join-Path $outDir ("sender_run_${ts}.log")
$senderErr = Join-Path $outDir ("sender_run_${ts}.err.log")

$env:REMOTE_HOST = $RemoteHost
$env:BENCH_TYPE = 'send'
$env:BENCH_TOTAL = "$BenchTotal"
$env:BENCH_CONCURRENCY = "$BenchConcurrency"
if ($NatsSenderPool -gt 0) {
  $env:EMBER_NATS_SENDER_POOL = "$NatsSenderPool"
} else {
  Remove-Item Env:\EMBER_NATS_SENDER_POOL -ErrorAction SilentlyContinue
}

Write-Host "Starting sender: BENCH_TOTAL=$BenchTotal BENCH_CONCURRENCY=$BenchConcurrency REMOTE_HOST=$RemoteHost EMBER_NATS_SENDER_POOL=$env:EMBER_NATS_SENDER_POOL"
$goProc = Start-Process -FilePath 'go' -ArgumentList @('run','.\example\node_concurrency') -RedirectStandardOutput $senderLog -RedirectStandardError $senderErr -PassThru

# Wait for sender pprof (6060)
$senderPid = $null
for ($i=0; $i -lt 200; $i++) {
  $senderPid = Get-ListeningPid 6060
  if ($senderPid) { break }
  Start-Sleep -Milliseconds 200
}
if (-not $senderPid) {
  Write-Host "Sender failed to listen on 6060; see: $senderErr"
  exit 2
}

Write-Host "Sender is listening on 6060 (PID=$senderPid). Warmup ${WarmupSeconds}s, then capture CPU profile ${ProfileSeconds}s."
Start-Sleep -Seconds $WarmupSeconds

$senderTop = Join-Path $outDir ("cpu_sender_${ts}.top.txt")
$receiverTop = Join-Path $outDir ("cpu_receiver_${ts}.top.txt")

& go tool pprof -top -nodecount=30 .\example\data\pprof\bin\node_concurrency.exe "http://127.0.0.1:6060/debug/pprof/profile?seconds=$ProfileSeconds" | Out-File -Encoding utf8 $senderTop
& go tool pprof -top -nodecount=30 .\example\data\pprof\bin\node_concurrency1.exe "http://127.0.0.1:6061/debug/pprof/profile?seconds=$ProfileSeconds" | Out-File -Encoding utf8 $receiverTop

Write-Host "CPU profile top saved:"
Write-Host "  $senderTop"
Write-Host "  $receiverTop"

# Extract QPS line from sender log (best-effort)
$deadline = (Get-Date).AddSeconds($WaitBenchSeconds)
$qpsLine = $null
while ((Get-Date) -lt $deadline) {
  if (Test-Path $senderLog) {
    $tail = Get-Content $senderLog -Tail 400 -ErrorAction SilentlyContinue
    $qpsLine = $tail | Where-Object { $_ -match '^QPS \(overall\)' } | Select-Object -Last 1
    if ($qpsLine) { break }
  }
  Start-Sleep -Seconds 1
}
if ($qpsLine) {
  Write-Host "Sender bench: $qpsLine"
} else {
  Write-Host "Sender bench QPS line not found within ${WaitBenchSeconds}s (sender may still be running). See: $senderLog"
}

# Stop sender (by port owner for reliability)
Kill-ByPort 6060

Write-Host "Done. Logs:"
Write-Host "  $senderLog"
Write-Host "  $senderErr"
