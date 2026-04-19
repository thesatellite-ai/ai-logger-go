<#
.SYNOPSIS
    Install ailog on Windows.
.DESCRIPTION
    Downloads and installs the latest ailog binary for Windows.
    Installs to $env:LOCALAPPDATA\ailog and adds it to the user PATH.
.EXAMPLE
    irm https://raw.githubusercontent.com/khanakia/ai-logger/main/install.ps1 | iex
#>

$ErrorActionPreference = "Stop"

$REPO = "khanakia/ai-logger"
$BINARY = "ailog"
$INSTALL_DIR = Join-Path $env:LOCALAPPDATA "ailog"

# Detect architecture (with fallback for 32-bit PowerShell on 64-bit Windows).
$ProcessorArch = $env:PROCESSOR_ARCHITECTURE
if ($ProcessorArch -eq "x86" -and $env:PROCESSOR_ARCHITEW6432) {
    $ProcessorArch = $env:PROCESSOR_ARCHITEW6432
}

$ARCH = switch ($ProcessorArch) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    "x86" {
        Write-Host "Error: 32-bit x86 is not supported. Please use 64-bit Windows." -ForegroundColor Red
        exit 1
    }
    default {
        Write-Host "Warning: Unknown architecture '$ProcessorArch', assuming amd64..." -ForegroundColor Yellow
        "amd64"
    }
}

Write-Host "Detected architecture: $ARCH"

# Fetch latest release tag.
Write-Host "Fetching latest release from GitHub..."
try {
    $RELEASE = Invoke-RestMethod -Uri "https://api.github.com/repos/$REPO/releases/latest" -TimeoutSec 30
    $TAG = $RELEASE.tag_name
} catch {
    Write-Host "Error: Failed to fetch latest release from GitHub." -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
}

if (-not $TAG) {
    Write-Host "Error: No release found." -ForegroundColor Red
    exit 1
}

Write-Host "Latest version: $TAG"

# Download the windows zip asset for this arch.
$ASSET = "${BINARY}_windows_${ARCH}.zip"
$URL = "https://github.com/$REPO/releases/download/$TAG/$ASSET"

Write-Host "Downloading $ASSET..."
$TempDir = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
$ZipFile = Join-Path $TempDir $ASSET

try {
    Invoke-WebRequest -Uri $URL -OutFile $ZipFile -TimeoutSec 60 -UseBasicParsing
} catch {
    Write-Host "Error: Failed to download $ASSET" -ForegroundColor Red
    Write-Host "URL: $URL" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    Remove-Item $TempDir -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
}

# Extract.
try {
    Write-Host "Extracting archive..."
    Expand-Archive -Path $ZipFile -DestinationPath $TempDir -Force
    $BinaryFile = Get-ChildItem -Path $TempDir -Filter "$BINARY.exe" -Recurse | Select-Object -First 1
    if (-not $BinaryFile) {
        Write-Host "Error: Binary '$BINARY.exe' not found in downloaded archive." -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "Error: Failed to extract archive or locate binary." -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    Remove-Item $TempDir -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
}

# Install (replacing any previous install).
Write-Host "Installing to $INSTALL_DIR..."
if (Test-Path $INSTALL_DIR) {
    Remove-Item $INSTALL_DIR -Recurse -Force
}
New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null
Move-Item $BinaryFile.FullName -Destination $INSTALL_DIR
Remove-Item $TempDir -Recurse -Force

# Ensure install dir is on the user PATH (idempotent, case-insensitive).
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
$NormalizedInstallDir = $INSTALL_DIR.TrimEnd('\').ToLowerInvariant()
$NormalizedPath = $UserPath.TrimEnd('\').ToLowerInvariant()

if (-not ($NormalizedPath -split ';' | Where-Object { $_.TrimEnd('\') -eq $NormalizedInstallDir })) {
    Write-Host "Adding $INSTALL_DIR to your PATH..."
    $NewPath = $UserPath + ";$INSTALL_DIR"
    [Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
    $env:PATH = $env:PATH + ";$INSTALL_DIR"
    Write-Host "PATH updated. Restart your terminal for the change to take effect." -ForegroundColor Yellow
} else {
    Write-Host "$INSTALL_DIR is already in your PATH."
}

Write-Host ""
Write-Host "ailog installed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "Installed to: $INSTALL_DIR\$BINARY.exe"
Write-Host ""
Write-Host "Next steps:"
Write-Host "  ailog.exe init                  # create %USERPROFILE%\.ailog\log.db"
Write-Host "  ailog.exe hooks install         # wire Claude Code auto-capture"
Write-Host "  ailog.exe skill install         # install the recall/search skill"
Write-Host ""
Write-Host "Verify: ailog.exe version" -ForegroundColor Cyan
