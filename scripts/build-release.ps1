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

function Sanitize-FileToken {
    param([string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value)) {
        return "dev"
    }

    return ($Value -replace "[^0-9A-Za-z._-]", "-")
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
$ZipName = "orchestrator_{0}_windows_amd64_portable.zip" -f (Sanitize-FileToken -Value $Version)
$ZipPath = Join-Path $OutputRoot $ZipName
$BinaryPath = Join-Path $PortableDir "orchestrator.exe"
$MetadataPath = Join-Path $PortableDir "build-metadata.txt"

New-Item -ItemType Directory -Force -Path $PortableDir | Out-Null

$ldflags = @(
    "-s",
    "-w",
    "-X", "orchestrator/internal/buildinfo.Version=$Version",
    "-X", "orchestrator/internal/buildinfo.Revision=$Revision",
    "-X", "orchestrator/internal/buildinfo.BuildTime=$BuildTime"
) -join " "

go build `
    -trimpath `
    -buildvcs=false `
    -ldflags $ldflags `
    -o $BinaryPath `
    .\cmd\orchestrator

Copy-Item -Force README.md (Join-Path $PortableDir "README.md")
Copy-Item -Force docs\WINDOWS_INSTALL_AND_RELEASE.md (Join-Path $PortableDir "WINDOWS_INSTALL_AND_RELEASE.md")
Copy-Item -Force docs\REAL_APP_WORKFLOW.md (Join-Path $PortableDir "REAL_APP_WORKFLOW.md")

@(
    "version=$Version"
    "revision=$Revision"
    "build_time=$BuildTime"
    "binary=$BinaryPath"
) | Set-Content -Path $MetadataPath -Encoding ASCII

if (Test-Path $ZipPath) {
    Remove-Item -Force $ZipPath
}
Compress-Archive -Path (Join-Path $PortableDir "*") -DestinationPath $ZipPath -Force

Write-Output ("release.output_root: {0}" -f $OutputRoot)
Write-Output ("release.portable_dir: {0}" -f $PortableDir)
Write-Output ("release.binary: {0}" -f $BinaryPath)
Write-Output ("release.zip: {0}" -f $ZipPath)
Write-Output ("release.version: {0}" -f $Version)
Write-Output ("release.revision: {0}" -f $Revision)
Write-Output ("release.build_time: {0}" -f $BuildTime)
