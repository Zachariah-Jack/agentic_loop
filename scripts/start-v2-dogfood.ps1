param(
  [string]$RepoPath = ".",
  [string]$ControlAddr = "127.0.0.1:44777",
  [switch]$SkipBuild,
  [switch]$SkipInstall,
  [switch]$StartupLauncher,
  [switch]$DebugVisibleWindows
)

$ErrorActionPreference = "Stop"

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$resolvedRepoPath = (Resolve-Path $RepoPath).Path
$binaryPath = Join-Path $projectRoot "dist\orchestrator.exe"
$shellPath = Join-Path $projectRoot "console\v2-shell"
$logsPath = Join-Path $resolvedRepoPath ".orchestrator\logs"
$statePath = Join-Path $resolvedRepoPath ".orchestrator\state"
$artifactsPath = Join-Path $resolvedRepoPath ".orchestrator\artifacts"
$backendMetaPath = Join-Path $statePath "dogfood-backend.json"
$ownerMarker = "orchestrator-v2-dogfood"

function New-DogfoodSessionId {
  return [guid]::NewGuid().ToString()
}

function Get-BinaryModifiedTime {
  param([Parameter(Mandatory=$true)][string]$Path)
  try {
    return (Get-Item -LiteralPath $Path -ErrorAction Stop).LastWriteTimeUtc.ToString("o")
  } catch {
    return ""
  }
}

function Normalize-DogfoodPath {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path)) {
    return ""
  }
  try {
    return ([System.IO.Path]::GetFullPath($Path)).TrimEnd("\").ToLowerInvariant()
  } catch {
    return ([string]$Path).TrimEnd("\").ToLowerInvariant()
  }
}

function Get-DogfoodControlPort {
  return [int]($ControlAddr.Split(":")[-1])
}

function Get-DogfoodProcessInfo {
  param([Parameter(Mandatory=$true)][int]$ProcessId)

  $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
  $cim = $null
  try {
    $cim = Get-CimInstance Win32_Process -Filter "ProcessId = $ProcessId" -ErrorAction SilentlyContinue
  } catch {
    $cim = $null
  }

  [pscustomobject]@{
    pid = $ProcessId
    exists = $null -ne $process
    path = if ($null -ne $cim -and $cim.ExecutablePath) { [string]$cim.ExecutablePath } elseif ($null -ne $process) { [string]$process.Path } else { "" }
    command_line = if ($null -ne $cim -and $cim.CommandLine) { [string]$cim.CommandLine } else { "" }
    parent_pid = if ($null -ne $cim -and $cim.ParentProcessId) { [int]$cim.ParentProcessId } else { 0 }
  }
}

function Get-DogfoodPortListeners {
  $port = Get-DogfoodControlPort
  $listeners = @()
  try {
    $connections = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
  } catch {
    return @()
  }
  foreach ($connection in $connections) {
    $pidValue = [int]$connection.OwningProcess
    $info = Get-DogfoodProcessInfo -ProcessId $pidValue
    $listeners += [pscustomobject]@{
      pid = $pidValue
      path = $info.path
      command_line = $info.command_line
      parent_pid = $info.parent_pid
    }
  }
  return $listeners
}

function Test-DogfoodOwnedProcess {
  param(
    [Parameter(Mandatory=$true)][object]$Metadata,
    [Parameter(Mandatory=$true)][object]$ProcessInfo
  )

  if ($null -eq $ProcessInfo -or $null -eq $ProcessInfo.pid) {
    return $false
  }
  if ($null -ne $Metadata.pid -and [int]$Metadata.pid -eq [int]$ProcessInfo.pid) {
    return $true
  }
  if ($null -ne $Metadata.pid -and [int]$Metadata.pid -eq [int]$ProcessInfo.parent_pid) {
    return $true
  }

  $expectedBinary = Normalize-DogfoodPath ([string]$Metadata.binary_path)
  $actualPath = Normalize-DogfoodPath ([string]$ProcessInfo.path)
  $commandLine = ([string]$ProcessInfo.command_line).ToLowerInvariant()
  $controlAddrText = ([string]$Metadata.control_addr).ToLowerInvariant()
  if ($expectedBinary -ne "" -and $actualPath -eq $expectedBinary -and $commandLine.Contains("control serve") -and $commandLine.Contains($controlAddrText)) {
    return $true
  }
  return $false
}

function Invoke-DogfoodTaskKill {
  param(
    [Parameter(Mandatory=$true)][int]$ProcessId,
    [Parameter(Mandatory=$true)][string]$Label
  )

  try {
    $output = (& taskkill /PID $ProcessId /T /F 2>&1 | Out-String).Trim()
    if ([string]::IsNullOrWhiteSpace($output)) {
      $output = "taskkill completed with no output"
    }
    return "taskkill /PID $ProcessId /T /F ($Label): $output"
  } catch {
    Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
    return "Stop-Process -Force fallback ($Label): $($_.Exception.Message)"
  }
}

function Format-DogfoodPortDiagnostic {
  param(
    [Parameter(Mandatory=$true)][object]$Metadata,
    [Parameter(Mandatory=$true)][array]$Listeners,
    [Parameter(Mandatory=$true)][array]$Attempts,
    [Parameter(Mandatory=$true)][string]$Context
  )

  $attemptedInfo = if ($null -ne $Metadata.pid -and "$($Metadata.pid)" -ne "") { Get-DogfoodProcessInfo -ProcessId ([int]$Metadata.pid) } else { $null }
  $lines = @()
  $lines += "dogfood.error: port $ControlAddr did not clear during $Context."
  $lines += "attempted owned PID: $($Metadata.pid)"
  $lines += "attempted process path: $($attemptedInfo.path)"
  $lines += "attempted command line: $($attemptedInfo.command_line)"
  $lines += "attempted kill methods:"
  if ($Attempts.Count -eq 0) {
    $lines += "  - none recorded"
  } else {
    foreach ($attempt in $Attempts) {
      $lines += "  - $attempt"
    }
  }
  if ($Listeners.Count -eq 0) {
    $lines += "current port holders: none observed in final check"
  } else {
    $lines += "current port holders:"
    foreach ($listener in $Listeners) {
      $owned = Test-DogfoodOwnedProcess -Metadata $Metadata -ProcessInfo $listener
      $samePid = ($null -ne $Metadata.pid -and [int]$Metadata.pid -eq [int]$listener.pid)
      $lines += "  - pid: $($listener.pid)"
      $lines += "    path: $($listener.path)"
      $lines += "    command_line: $($listener.command_line)"
      $lines += "    parent_pid: $($listener.parent_pid)"
      $lines += "    matches_owned_metadata: $owned"
      $lines += "    same_as_attempted_pid: $samePid"
    }
  }
  $lines += "next safe action: if matches_owned_metadata is true, rerun this helper or use Recover Backend / Unlock Repo; if false, close that listed process or choose another -ControlAddr. Unknown processes are not killed automatically."
  return ($lines -join [Environment]::NewLine)
}

function Clear-OwnedDogfoodPort {
  param(
    [Parameter(Mandatory=$true)][object]$Metadata,
    [int]$TimeoutSeconds = 14
  )

  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  $attempts = @()
  while ((Get-Date) -lt $deadline) {
    $listeners = @(Get-DogfoodPortListeners)
    if ($listeners.Count -eq 0) {
      return [pscustomobject]@{ cleared = $true; listeners = @(); attempts = $attempts }
    }

    $killedAny = $false
    foreach ($listener in $listeners) {
      if (Test-DogfoodOwnedProcess -Metadata $Metadata -ProcessInfo $listener) {
        $attempts += Invoke-DogfoodTaskKill -ProcessId ([int]$listener.pid) -Label "owned port listener"
        $killedAny = $true
      }
    }

    if (-not $killedAny) {
      return [pscustomobject]@{ cleared = $false; listeners = $listeners; attempts = $attempts }
    }
    Start-Sleep -Milliseconds 700
  }

  return [pscustomobject]@{ cleared = $false; listeners = @(Get-DogfoodPortListeners); attempts = $attempts }
}

function Stop-OwnedDogfoodBackend {
  param(
    [Parameter(Mandatory=$true)][object]$Metadata,
    [Parameter(Mandatory=$true)][string]$Reason,
    [bool]$AllowFailure = $false
  )

  if ($null -eq $Metadata.pid) {
    return
  }
  $pidValue = [int]$Metadata.pid
  $process = Get-Process -Id $pidValue -ErrorAction SilentlyContinue

  Write-Host "dogfood.step: stop previous owned backend pid=$pidValue reason=$Reason"
  try {
    $stopEnvelope = @{
      id = "dogfood_stale_backend_shutdown"
      type = "request"
      action = "stop_safe"
      payload = @{
        reason = $Reason
      }
    } | ConvertTo-Json -Depth 5
    Invoke-RestMethod -Method Post -Uri "http://$($Metadata.control_addr)/v2/control" -ContentType "application/json" -Body $stopEnvelope -TimeoutSec 2 | Out-Null
    Start-Sleep -Milliseconds 800
  } catch {
    Write-Host "dogfood.note: previous backend did not accept safe-stop request; stopping owned process tree."
  }

  $attempts = @()
  if ($null -ne $process) {
    $attempts += Invoke-DogfoodTaskKill -ProcessId $pidValue -Label "metadata pid"
  } else {
    $attempts += "metadata pid $pidValue was not running when cleanup started"
  }

  $clearResult = Clear-OwnedDogfoodPort -Metadata $Metadata -TimeoutSeconds 14
  $attempts += @($clearResult.attempts)
  if (-not $clearResult.cleared) {
    $diagnostic = Format-DogfoodPortDiagnostic -Metadata $Metadata -Listeners @($clearResult.listeners) -Attempts $attempts -Context $Reason
    if ($AllowFailure) {
      Write-Host $diagnostic
      return
    }
    throw $diagnostic
  }
}

function Stop-StaleOwnedBackendIfPresent {
  if (-not (Test-Path $backendMetaPath)) {
    return
  }
  try {
    $metadata = Get-Content $backendMetaPath -Raw | ConvertFrom-Json
  } catch {
    Write-Host "dogfood.note: ignoring unreadable owned-backend metadata at $backendMetaPath"
    return
  }

  if ($metadata.owner -ne $ownerMarker) {
    Write-Host "dogfood.note: backend metadata is not dogfood-owned; not stopping pid=$($metadata.pid)"
    return
  }
  if (([string]$metadata.repo_path) -ne ([string]$resolvedRepoPath)) {
    Write-Host "dogfood.note: backend metadata is for another repo; not stopping pid=$($metadata.pid)"
    return
  }
  if (([string]$metadata.control_addr) -ne $ControlAddr) {
    Write-Host "dogfood.note: backend metadata is for another address; not stopping pid=$($metadata.pid)"
    return
  }

  $activeStatus = Test-DogfoodBackendActivelyProcessing -Metadata $metadata
  if ($activeStatus.active -eq $true) {
    throw "dogfood.error: existing owned backend pid=$($metadata.pid) is actively processing run=$($activeStatus.run_id) version=$($activeStatus.backend_version) protocol=$($activeStatus.protocol_version). It was not stopped automatically. Wait for the run to reach a safe boundary, request Safe Stop, or close it manually if you truly want to interrupt active work."
  }

  Stop-OwnedDogfoodBackend -Metadata $metadata -Reason "dogfood_restarting_owned_backend"
  Remove-Item -Path $backendMetaPath -Force -ErrorAction SilentlyContinue
}

function Wait-PortClear {
  param([int]$TimeoutSeconds = 8)
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  while ((Get-Date) -lt $deadline) {
    $listeners = @(Get-DogfoodPortListeners)
    if ($listeners.Count -eq 0) {
      return $true
    }
    Start-Sleep -Milliseconds 250
  }
  return $false
}

function Warn-IfUnknownProcessOwnsPort {
  $listeners = @(Get-DogfoodPortListeners)
  foreach ($listener in $listeners) {
    throw "dogfood.error: port $ControlAddr is already owned by unknown process pid=$($listener.pid) path=$($listener.path) command_line=$($listener.command_line). It was not killed automatically. Close that process or choose another -ControlAddr."
  }
}

function Get-DogfoodStatusSnapshot {
  $envelope = @{
    id = "dogfood_repo_binding_check"
    type = "request"
    action = "get_status_snapshot"
    payload = @{}
  } | ConvertTo-Json -Depth 5

  return Invoke-RestMethod -Method Post -Uri "http://$ControlAddr/v2/control" -ContentType "application/json" -Body $envelope -TimeoutSec 2
}

function Test-DogfoodBackendActivelyProcessing {
  param([Parameter(Mandatory=$true)][object]$Metadata)

  try {
    $response = Get-DogfoodStatusSnapshot
    if (-not $response.ok) {
      return [pscustomobject]@{ active = $false; reason = "status snapshot response was not ok" }
    }
    $payload = $response.payload
    $guard = $payload.active_run_guard
    $run = $payload.run
    $active = $false
    if ($null -ne $guard -and $guard.currently_processing -eq $true) {
      $active = $true
    }
    if ($null -ne $run -and $run.actively_processing -eq $true) {
      $active = $true
    }
    return [pscustomobject]@{
      active = $active
      run_id = if ($null -ne $run -and $run.id) { [string]$run.id } elseif ($null -ne $guard -and $guard.run_id) { [string]$guard.run_id } else { "" }
      backend_version = if ($null -ne $payload.backend -and $payload.backend.binary_version) { [string]$payload.backend.binary_version } elseif ($null -ne $payload.update_status -and $payload.update_status.current_version) { [string]$payload.update_status.current_version } else { "" }
      protocol_version = if ($null -ne $payload.protocol -and $payload.protocol.version) { [string]$payload.protocol.version } elseif ($null -ne $payload.backend -and $payload.backend.protocol_version) { [string]$payload.backend.protocol_version } else { "" }
      reason = "status snapshot inspected before owned backend cleanup"
    }
  } catch {
    return [pscustomobject]@{ active = $false; reason = $_.Exception.Message }
  }
}

function Wait-DogfoodBackendRepoMatch {
  param([int]$TimeoutSeconds = 12)

  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  $lastError = ""
  while ((Get-Date) -lt $deadline) {
    try {
      $response = Get-DogfoodStatusSnapshot
      if (-not $response.ok) {
        $lastError = if ($response.error.message) { [string]$response.error.message } else { "status snapshot response was not ok" }
        Start-Sleep -Milliseconds 350
        continue
      }
      $actualRepo = [string]$response.payload.runtime.repo_root
      if ((Normalize-DogfoodPath $actualRepo) -eq (Normalize-DogfoodPath $resolvedRepoPath)) {
        return $response.payload
      }
      throw "dogfood.error: backend repo mismatch after launch. expected_repo=$resolvedRepoPath actual_repo=$actualRepo control_addr=$ControlAddr. The shell was not launched because this backend would show the wrong repo/run state."
    } catch {
      $lastError = $_.Exception.Message
      if ($lastError -like "dogfood.error: backend repo mismatch*") {
        throw $lastError
      }
      Start-Sleep -Milliseconds 350
    }
  }

  throw "dogfood.error: control server did not report target repo before timeout. expected_repo=$resolvedRepoPath control_addr=$ControlAddr last_error=$lastError"
}

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

if (-not $StartupLauncher -and -not (Test-Path $binaryPath)) {
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
New-Item -ItemType Directory -Force -Path $statePath | Out-Null
New-Item -ItemType Directory -Force -Path $artifactsPath | Out-Null
$controlOut = Join-Path $logsPath "v2-control-server.out.log"
$controlErr = Join-Path $logsPath "v2-control-server.err.log"
$shellOut = Join-Path $logsPath "v2-shell.out.log"
$shellErr = Join-Path $logsPath "v2-shell.err.log"

if ($StartupLauncher) {
  Write-Host "dogfood.step: launch Aurora startup launcher"
  $launcherCommand = "`$env:ORCHESTRATOR_V2_FORCE_LAUNCHER = `"1`"; Remove-Item Env:\ORCHESTRATOR_V2_EXPECTED_REPO -ErrorAction SilentlyContinue; cd `"$shellPath`"; npm run dev"
  if ($DebugVisibleWindows) {
    Start-Process powershell -WorkingDirectory $shellPath -ArgumentList @(
      "-NoExit",
      "-ExecutionPolicy", "Bypass",
      "-Command", $launcherCommand
    )
    Write-Host "dogfood.status: startup launcher opened in debug-visible mode"
    return
  }
  $shellProcess = Start-Process powershell -WorkingDirectory $shellPath -WindowStyle Hidden -PassThru -RedirectStandardOutput $shellOut -RedirectStandardError $shellErr -ArgumentList @(
    "-NoProfile",
    "-ExecutionPolicy", "Bypass",
    "-Command", $launcherCommand
  )
  Start-Sleep -Seconds 2
  if ($shellProcess.HasExited) {
    $shellOutTail = if (Test-Path $shellOut) { (Get-Content $shellOut -Tail 40 | Out-String).Trim() } else { "" }
    $shellErrTail = if (Test-Path $shellErr) { (Get-Content $shellErr -Tail 80 | Out-String).Trim() } else { "" }
    throw "dogfood.error: Aurora startup launcher exited before a window could stay open. exit_code=$($shellProcess.ExitCode) shell_out=$shellOut shell_err=$shellErr`n--- shell stdout tail ---`n$shellOutTail`n--- shell stderr tail ---`n$shellErrTail"
  }
  Write-Host "dogfood.status: startup launcher opened"
  Write-Host "dogfood.logs: $logsPath"
  Wait-Process -Id $shellProcess.Id
  return
}

Write-Host "dogfood.step: repair target repo setup"
Push-Location $resolvedRepoPath
try {
  & $binaryPath init
  if ($LASTEXITCODE -ne 0) {
    throw "orchestrator init failed with exit code $LASTEXITCODE"
  }
} finally {
  Pop-Location
}

Stop-StaleOwnedBackendIfPresent
if (-not (Wait-PortClear)) {
  $listeners = @(Get-DogfoodPortListeners)
  throw (Format-DogfoodPortDiagnostic -Metadata ([pscustomobject]@{ pid = ""; binary_path = $binaryPath; control_addr = $ControlAddr }) -Listeners $listeners -Attempts @("waited for port to clear before launch") -Context "prelaunch_port_check")
}
Warn-IfUnknownProcessOwnsPort

$controlCommand = "& `"$binaryPath`" control serve --addr $ControlAddr"
$shellCommand = "`$env:ORCHESTRATOR_V2_SHELL_ADDR = `"http://$ControlAddr`"; `$env:ORCHESTRATOR_V2_BACKEND_META = `"$backendMetaPath`"; `$env:ORCHESTRATOR_V2_EXPECTED_REPO = `"$resolvedRepoPath`"; cd `"$shellPath`"; npm run dev"

if ($DebugVisibleWindows) {
  Write-Host "dogfood.step: launch visible control server"
  Start-Process powershell -WorkingDirectory $resolvedRepoPath -ArgumentList @(
    "-NoExit",
    "-ExecutionPolicy", "Bypass",
    "-Command", $controlCommand
  )

  Start-Sleep -Milliseconds 700
  Wait-DogfoodBackendRepoMatch | Out-Null

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
  $controlProcess = Start-Process -FilePath $binaryPath -WorkingDirectory $resolvedRepoPath -WindowStyle Hidden -PassThru -RedirectStandardOutput $controlOut -RedirectStandardError $controlErr -ArgumentList @(
    "control",
    "serve",
    "--addr",
    $ControlAddr
  )

  $metadata = @{
    owner = $ownerMarker
    owner_session_id = New-DogfoodSessionId
    pid = $controlProcess.Id
    repo_path = [string]$resolvedRepoPath
    control_addr = $ControlAddr
    binary_path = $binaryPath
    binary_mtime_at_launch = Get-BinaryModifiedTime -Path $binaryPath
    started_at = (Get-Date).ToUniversalTime().ToString("o")
    logs_path = $logsPath
  }
  $metadata | ConvertTo-Json -Depth 5 | Set-Content -Path $backendMetaPath -Encoding UTF8

  Start-Sleep -Milliseconds 700
  Wait-DogfoodBackendRepoMatch | Out-Null

  Write-Host "dogfood.step: launch Electron shell"
  $shellProcess = Start-Process powershell -WorkingDirectory $shellPath -WindowStyle Hidden -PassThru -RedirectStandardOutput $shellOut -RedirectStandardError $shellErr -ArgumentList @(
    "-NoProfile",
    "-ExecutionPolicy", "Bypass",
    "-Command", $shellCommand
  )

  Start-Sleep -Seconds 2
  if ($shellProcess.HasExited) {
    $shellOutTail = if (Test-Path $shellOut) { (Get-Content $shellOut -Tail 40 | Out-String).Trim() } else { "" }
    $shellErrTail = if (Test-Path $shellErr) { (Get-Content $shellErr -Tail 80 | Out-String).Trim() } else { "" }
    throw "dogfood.error: Electron shell exited before a window could stay open. exit_code=$($shellProcess.ExitCode) shell_out=$shellOut shell_err=$shellErr`n--- shell stdout tail ---`n$shellOutTail`n--- shell stderr tail ---`n$shellErrTail"
  }

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
    Invoke-DogfoodTaskKill -ProcessId $controlProcess.Id -Label "owned control server on shell close" | Write-Host
  }
  if (Test-Path $backendMetaPath) {
    try {
      $currentMetadata = Get-Content $backendMetaPath -Raw | ConvertFrom-Json
      if ($currentMetadata.owner -eq $ownerMarker -and ([string]$currentMetadata.repo_path) -eq ([string]$resolvedRepoPath) -and ([string]$currentMetadata.control_addr) -eq $ControlAddr) {
        Stop-OwnedDogfoodBackend -Metadata $currentMetadata -Reason "dogfood_shell_closed" -AllowFailure $true
      }
    } catch {
      Write-Host "dogfood.note: unable to read owned-backend metadata during shutdown."
    }
  }
  Remove-Item -Path $backendMetaPath -Force -ErrorAction SilentlyContinue
}
