param(
  [string]$RepoPath = ".",
  [string]$ControlAddr = "127.0.0.1:44777",
  [switch]$SkipBuild,
  [switch]$SkipInstall,
  [switch]$DebugVisibleWindows
)

$ErrorActionPreference = "Stop"

$projectRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$resolvedRepoPath = Resolve-Path $RepoPath
$binaryPath = Join-Path $projectRoot "dist\orchestrator.exe"
$shellPath = Join-Path $projectRoot "console\v2-shell"
$logsPath = Join-Path $resolvedRepoPath ".orchestrator\logs"

Write-Host "project.root: $projectRoot"
Write-Host "dogfood.repo_path: $resolvedRepoPath"
Write-Host "dogfood.control_addr: $ControlAddr"
Write-Host "dogfood.binary_path: $binaryPath"
Write-Host "dogfood.debug_visible_windows: $DebugVisibleWindows"

if (-not $SkipBuild) {
  Write-Host "dogfood.step: build orchestrator"
  New-Item -ItemType Directory -Force -Path (Split-Path $binaryPath -Parent) | Out-Null
  & go build -o $binaryPath (Join-Path $projectRoot "cmd\orchestrator")
}

if (-not (Test-Path $binaryPath)) {
  throw "orchestrator binary not found at $binaryPath"
}

if (-not $SkipInstall -and -not (Test-Path (Join-Path $shellPath "node_modules"))) {
  Write-Host "dogfood.step: npm install"
  Push-Location $shellPath
  try {
    & npm install
  } finally {
    Pop-Location
  }
}

New-Item -ItemType Directory -Force -Path $logsPath | Out-Null
$controlOut = Join-Path $logsPath "v2-control-server.out.log"
$controlErr = Join-Path $logsPath "v2-control-server.err.log"
$shellOut = Join-Path $logsPath "v2-shell.out.log"
$shellErr = Join-Path $logsPath "v2-shell.err.log"

$controlCommand = "& `"$binaryPath`" control serve --addr $ControlAddr"
$shellCommand = "$env:ORCHESTRATOR_V2_SHELL_ADDR = `"http://$ControlAddr`"; cd `"$shellPath`"; npm run dev"

if ($DebugVisibleWindows) {
  Write-Host "dogfood.step: launch visible control server"
  Start-Process powershell -WorkingDirectory $resolvedRepoPath -ArgumentList @(
    "-NoExit",
    "-ExecutionPolicy", "Bypass",
    "-Command", $controlCommand
  )

  Start-Sleep -Milliseconds 700

  Write-Host "dogfood.step: launch visible shell"
  Start-Process powershell -WorkingDirectory $shellPath -ArgumentList @(
    "-NoExit",
    "-ExecutionPolicy", "Bypass",
    "-Command", $shellCommand
  )

  Write-Host "dogfood.status: launched in debug-visible mode"
  Write-Host "dogfood.note: close the spawned PowerShell windows when you are done."
  return
}

$controlProcess = $null
$shellProcess = $null

try {
  Write-Host "dogfood.step: launch hidden control server"
  $controlProcess = Start-Process powershell -WorkingDirectory $resolvedRepoPath -WindowStyle Hidden -PassThru -RedirectStandardOutput $controlOut -RedirectStandardError $controlErr -ArgumentList @(
    "-NoProfile",
    "-ExecutionPolicy", "Bypass",
    "-Command", $controlCommand
  )

  Start-Sleep -Milliseconds 700

  Write-Host "dogfood.step: launch Electron shell"
  $shellProcess = Start-Process powershell -WorkingDirectory $shellPath -WindowStyle Hidden -PassThru -RedirectStandardOutput $shellOut -RedirectStandardError $shellErr -ArgumentList @(
    "-NoProfile",
    "-ExecutionPolicy", "Bypass",
    "-Command", $shellCommand
  )

  Write-Host "dogfood.status: launched"
  Write-Host "dogfood.control_server: $ControlAddr"
  Write-Host "dogfood.logs: $logsPath"
  Write-Host "dogfood.note: close the Electron window to stop the dogfood-owned control server."

  Wait-Process -Id $shellProcess.Id
} finally {
  if ($controlProcess -and -not $controlProcess.HasExited) {
    try {
      $stopEnvelope = @{
        id = "dogfood_shutdown"
        type = "request"
        action = "stop_safe"
        payload = @{
          reason = "dogfood_shell_closed"
        }
      } | ConvertTo-Json -Depth 5
      Invoke-RestMethod -Method Post -Uri "http://$ControlAddr/v2/control" -ContentType "application/json" -Body $stopEnvelope -TimeoutSec 2 | Out-Null
      Start-Sleep -Milliseconds 1200
    } catch {
      Write-Host "dogfood.note: safe-stop request during shutdown was unavailable; stopping owned control server."
    }
    Write-Host "dogfood.step: stop owned control server"
    Stop-Process -Id $controlProcess.Id -Force -ErrorAction SilentlyContinue
  }
}
