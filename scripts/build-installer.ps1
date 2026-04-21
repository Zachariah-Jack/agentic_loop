param(
    [string]$Version = "",
    [string]$Revision = "",
    [string]$BuildTime = "",
    [string]$OutputRoot = ""
)

$ErrorActionPreference = "Stop"

function Resolve-GitValue {
    param(
        [string[]]$Arguments,
        [string]$Fallback
    )

    try {
        $value = & git @Arguments 2>$null
        if ($LASTEXITCODE -eq 0 -and $null -ne $value) {
            $text = ($value | Select-Object -First 1).ToString().Trim()
            if (-not [string]::IsNullOrWhiteSpace($text)) {
                return $text
            }
        }
    } catch {
    }

    return $Fallback
}

function Resolve-IsccPath {
    if (-not [string]::IsNullOrWhiteSpace($env:INNO_SETUP_PATH)) {
        if (Test-Path $env:INNO_SETUP_PATH) {
            return $env:INNO_SETUP_PATH
        }
    }

    $candidates = @(
        (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe"),
        (Join-Path $env:ProgramFiles "Inno Setup 6\ISCC.exe")
    )

    foreach ($candidate in $candidates) {
        if (-not [string]::IsNullOrWhiteSpace($candidate) -and (Test-Path $candidate)) {
            return $candidate
        }
    }

    throw "Inno Setup 6 was not found. Install ISCC.exe or set INNO_SETUP_PATH."
}

$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = Resolve-GitValue -Arguments @("describe", "--tags", "--always", "--dirty") -Fallback "dev"
}
if ([string]::IsNullOrWhiteSpace($Revision)) {
    $Revision = Resolve-GitValue -Arguments @("rev-parse", "HEAD") -Fallback "unknown"
}
if ([string]::IsNullOrWhiteSpace($BuildTime)) {
    $BuildTime = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
}
if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
    $OutputRoot = Join-Path $RepoRoot "dist\windows-amd64"
}

$PortableDir = Join-Path $OutputRoot "portable"
$BinaryPath = Join-Path $PortableDir "orchestrator.exe"
$InstallerOutputDir = Join-Path $OutputRoot "installer"
$ScriptPath = Join-Path $RepoRoot "install\windows\orchestrator.iss"

if (-not (Test-Path $BinaryPath)) {
    & (Join-Path $PSScriptRoot "build-release.ps1") -Version $Version -Revision $Revision -BuildTime $BuildTime -OutputRoot $OutputRoot
}

New-Item -ItemType Directory -Force -Path $InstallerOutputDir | Out-Null

$iscc = Resolve-IsccPath

& $iscc `
    "/DAppVersion=$Version" `
    "/DAppRevision=$Revision" `
    "/DAppBuildTime=$BuildTime" `
    "/DPortableDir=$PortableDir" `
    "/DReleaseDir=$InstallerOutputDir" `
    $ScriptPath

Write-Output ("installer.output_dir: {0}" -f $InstallerOutputDir)
Write-Output ("installer.portable_dir: {0}" -f $PortableDir)
Write-Output ("installer.iscc: {0}" -f $iscc)
Write-Output ("installer.version: {0}" -f $Version)
Write-Output ("installer.revision: {0}" -f $Revision)
Write-Output ("installer.build_time: {0}" -f $BuildTime)
