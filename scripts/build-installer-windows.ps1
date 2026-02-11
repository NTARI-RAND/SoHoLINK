# SoHoLINK Windows Installer Build Script
# Creates a complete installation package with embedded Go runtime

param(
    [string]$Version = "0.1.0",
    [switch]$SkipGoDownload = $false
)

$ErrorActionPreference = "Stop"

Write-Host "╔══════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║          SoHoLINK Windows Installer Builder                 ║" -ForegroundColor Cyan
Write-Host "║          Network Theory Applied Research Institute          ║" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# Configuration
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$BuildDir = Join-Path $ProjectRoot "build"
$DistDir = Join-Path $ProjectRoot "dist"
$InstallerDir = Join-Path $ProjectRoot "installer\windows"
$TempDir = Join-Path $ProjectRoot "temp-installer"

# Go configuration
$GoVersion = "1.22.0"
$GoDownloadUrl = "https://go.dev/dl/go$GoVersion.windows-amd64.zip"
$GoArchive = Join-Path $TempDir "go.zip"
$GoExtractPath = Join-Path $TempDir "go"

Write-Host "[1/8] Creating directories..." -ForegroundColor Yellow
New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null

Write-Host "[2/8] Checking for Go installation..." -ForegroundColor Yellow
$GoInstalled = $null
try {
    $GoInstalled = Get-Command go -ErrorAction SilentlyContinue
} catch {}

if (-not $GoInstalled) {
    Write-Host "    Go not found on system. Will embed portable Go in installer." -ForegroundColor Cyan
    $EmbedGo = $true
} else {
    $CurrentGoVersion = (go version) -replace '.*go(\d+\.\d+\.\d+).*', '$1'
    Write-Host "    Found Go $CurrentGoVersion" -ForegroundColor Green
    $EmbedGo = $false
}

# Download portable Go if needed
if ($EmbedGo -and -not $SkipGoDownload) {
    Write-Host "[3/8] Downloading portable Go $GoVersion..." -ForegroundColor Yellow

    if (-not (Test-Path $GoArchive)) {
        Write-Host "    Downloading from $GoDownloadUrl" -ForegroundColor Cyan
        Invoke-WebRequest -Uri $GoDownloadUrl -OutFile $GoArchive -UseBasicParsing
        Write-Host "    Download complete!" -ForegroundColor Green
    } else {
        Write-Host "    Using cached Go archive" -ForegroundColor Green
    }

    Write-Host "    Extracting Go..." -ForegroundColor Cyan
    Expand-Archive -Path $GoArchive -DestinationPath $TempDir -Force
    Write-Host "    Extraction complete!" -ForegroundColor Green
} else {
    Write-Host "[3/8] Skipping Go download (using system Go)" -ForegroundColor Yellow
}

# Build all binaries
Write-Host "[4/8] Building SoHoLINK binaries..." -ForegroundColor Yellow

# Determine which Go to use
if ($GoInstalled) {
    $GoBin = "go"
} else {
    $GoBin = Join-Path $GoExtractPath "bin\go.exe"
}

# Build main binary
Write-Host "    Building fedaaa.exe..." -ForegroundColor Cyan
& $GoBin build -ldflags "-s -w -X main.version=$Version" -o (Join-Path $BuildDir "fedaaa.exe") .\cmd\fedaaa
if ($LASTEXITCODE -ne 0) {
    Write-Host "    ❌ Failed to build fedaaa.exe" -ForegroundColor Red
    exit 1
}
Write-Host "    ✓ fedaaa.exe built successfully" -ForegroundColor Green

# Build GUI wizard
Write-Host "    Building soholink-wizard.exe (GUI)..." -ForegroundColor Cyan
& $GoBin build -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o (Join-Path $BuildDir "soholink-wizard.exe") .\cmd\soholink-wizard
if ($LASTEXITCODE -ne 0) {
    Write-Host "    ❌ Failed to build soholink-wizard.exe" -ForegroundColor Red
    exit 1
}
Write-Host "    ✓ soholink-wizard.exe built successfully" -ForegroundColor Green

# Build CLI wizard (fallback)
Write-Host "    Building soholink-wizard-cli.exe..." -ForegroundColor Cyan
& $GoBin build -ldflags "-s -w -X main.version=$Version" -o (Join-Path $BuildDir "soholink-wizard-cli.exe") .\cmd\soholink-wizard-cli
if ($LASTEXITCODE -ne 0) {
    Write-Host "    ❌ Failed to build soholink-wizard-cli.exe" -ForegroundColor Red
    exit 1
}
Write-Host "    ✓ soholink-wizard-cli.exe built successfully" -ForegroundColor Green

# Create installer staging directory
Write-Host "[5/8] Staging installer files..." -ForegroundColor Yellow
$StagingDir = Join-Path $TempDir "staging"
New-Item -ItemType Directory -Force -Path $StagingDir | Out-Null

# Copy binaries
Copy-Item (Join-Path $BuildDir "fedaaa.exe") -Destination $StagingDir
Copy-Item (Join-Path $BuildDir "soholink-wizard.exe") -Destination $StagingDir
Copy-Item (Join-Path $BuildDir "soholink-wizard-cli.exe") -Destination $StagingDir

# Copy documentation
$DocsStaging = Join-Path $StagingDir "docs"
New-Item -ItemType Directory -Force -Path $DocsStaging | Out-Null
if (Test-Path (Join-Path $ProjectRoot "docs")) {
    Copy-Item -Path (Join-Path $ProjectRoot "docs\*") -Destination $DocsStaging -Recurse -Force
}
Copy-Item (Join-Path $ProjectRoot "README.md") -Destination $DocsStaging -Force
Copy-Item (Join-Path $ProjectRoot "LICENSE.txt") -Destination $DocsStaging -Force -ErrorAction SilentlyContinue

# Copy portable Go if embedded
if ($EmbedGo) {
    Write-Host "    Embedding portable Go runtime..." -ForegroundColor Cyan
    $GoStaging = Join-Path $StagingDir "go"
    Copy-Item -Path $GoExtractPath -Destination $GoStaging -Recurse -Force
    Write-Host "    ✓ Go runtime embedded" -ForegroundColor Green
}

# Create installation script
Write-Host "[6/8] Creating installation scripts..." -ForegroundColor Yellow

$InstallScript = @"
@echo off
REM SoHoLINK Installation Script
REM Auto-generated by build-installer-windows.ps1

echo.
echo ╔══════════════════════════════════════════════════════════════╗
echo ║          SoHoLINK Installation                               ║
echo ║          Network Theory Applied Research Institute          ║
echo ╚══════════════════════════════════════════════════════════════╝
echo.

SET INSTALL_DIR=%~dp0
SET GO_EMBEDDED=0

REM Check if Go is embedded
if exist "%INSTALL_DIR%go\bin\go.exe" (
    echo [✓] Found embedded Go runtime
    SET GO_EMBEDDED=1
    SET PATH=%INSTALL_DIR%go\bin;%PATH%
) else (
    echo [i] Checking for system Go installation...
    where go >nul 2>&1
    if errorlevel 1 (
        echo [✗] Go not found! Please install Go from https://go.dev/dl/
        echo.
        pause
        exit /b 1
    )
    echo [✓] Using system Go installation
)

REM Add SoHoLINK to PATH
echo [i] Adding SoHoLINK to system PATH...
setx PATH "%PATH%;%INSTALL_DIR%" /M >nul 2>&1
if errorlevel 1 (
    echo [!] Could not add to system PATH automatically
    echo     Please add manually: %INSTALL_DIR%
) else (
    echo [✓] Added to PATH successfully
)

REM Create data directories
echo [i] Creating data directories...
if not exist "%USERPROFILE%\.soholink" mkdir "%USERPROFILE%\.soholink"
if not exist "%USERPROFILE%\.soholink\data" mkdir "%USERPROFILE%\.soholink\data"
if not exist "%USERPROFILE%\.soholink\logs" mkdir "%USERPROFILE%\.soholink\logs"
echo [✓] Data directories created

REM Create desktop shortcut
echo [i] Creating desktop shortcut...
powershell -Command "$WshShell = New-Object -ComObject WScript.Shell; $Shortcut = $WshShell.CreateShortcut('%USERPROFILE%\Desktop\SoHoLINK Setup.lnk'); $Shortcut.TargetPath = '%INSTALL_DIR%soholink-wizard.exe'; $Shortcut.WorkingDirectory = '%INSTALL_DIR%'; $Shortcut.Description = 'SoHoLINK Configuration Wizard'; $Shortcut.Save()"
echo [✓] Desktop shortcut created

echo.
echo ╔══════════════════════════════════════════════════════════════╗
echo ║          Installation Complete!                              ║
echo ╚══════════════════════════════════════════════════════════════╝
echo.
echo Next Steps:
echo   1. Launch "SoHoLINK Setup" from your desktop
echo   2. Follow the configuration wizard
echo   3. Start earning with federated cloud!
echo.
echo Documentation: %INSTALL_DIR%docs\README.md
echo.
pause

REM Launch wizard
echo.
echo Launch configuration wizard now? (Y/N)
set /p LAUNCH_WIZARD=
if /i "%LAUNCH_WIZARD%"=="Y" (
    start "" "%INSTALL_DIR%soholink-wizard.exe"
)
"@

Set-Content -Path (Join-Path $StagingDir "install.bat") -Value $InstallScript
Write-Host "    ✓ install.bat created" -ForegroundColor Green

# Create README for the installer
$InstallerReadme = @"
SoHoLINK Installation Package
==============================

Version: $Version
Built: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")

Quick Start
-----------

1. Right-click 'install.bat' and select "Run as administrator"
2. Follow the installation prompts
3. Launch the configuration wizard from your desktop
4. Complete the setup process
5. Start earning with federated cloud!

What's Included
---------------

- fedaaa.exe              - Main SoHoLINK service
- soholink-wizard.exe     - Configuration wizard (GUI)
- soholink-wizard-cli.exe - Configuration wizard (CLI fallback)
$(if ($EmbedGo) {"- go/                     - Portable Go runtime (embedded)"} else {"- No Go runtime (uses system Go)"})
- docs/                   - Documentation
- install.bat             - Installation script

Requirements
------------

$(if (-not $EmbedGo) {"- Go $GoVersion or later (https://go.dev/dl/)"} else {"- No additional requirements (Go embedded)"})
- Windows 10 or later (64-bit)
- Administrator privileges for installation

Manual Installation
-------------------

If you prefer not to use install.bat:

1. Copy all files to your desired location (e.g., C:\Program Files\SoHoLINK)
2. Add the installation directory to your PATH
3. Run soholink-wizard.exe to configure

Support
-------

Contact: info@ntari.org
Website: https://ntari.org
Documentation: docs/README.md
GitHub: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink
License: AGPL-3.0 (see docs/LICENSE.txt)

(c) 2023 Network Theory Applied Research Institute
"@

Set-Content -Path (Join-Path $StagingDir "README.txt") -Value $InstallerReadme
Write-Host "    ✓ README.txt created" -ForegroundColor Green

# Create self-extracting archive or ZIP
Write-Host "[7/8] Creating distribution package..." -ForegroundColor Yellow

$OutputZip = Join-Path $DistDir "SoHoLINK-v$Version-windows-amd64.zip"
Write-Host "    Packaging to: $OutputZip" -ForegroundColor Cyan

# Remove existing archive
if (Test-Path $OutputZip) {
    Remove-Item $OutputZip -Force
}

# Create ZIP
Compress-Archive -Path "$StagingDir\*" -DestinationPath $OutputZip -Force
Write-Host "    ✓ Package created successfully!" -ForegroundColor Green

# Calculate size
$PackageSize = (Get-Item $OutputZip).Length / 1MB
Write-Host "    Package size: $([math]::Round($PackageSize, 2)) MB" -ForegroundColor Cyan

# Cleanup
Write-Host "[8/8] Cleaning up temporary files..." -ForegroundColor Yellow
Remove-Item $TempDir -Recurse -Force -ErrorAction SilentlyContinue
Write-Host "    ✓ Cleanup complete" -ForegroundColor Green

# Summary
Write-Host ""
Write-Host "╔══════════════════════════════════════════════════════════════╗" -ForegroundColor Green
Write-Host "║          Build Complete!                                     ║" -ForegroundColor Green
Write-Host "╚══════════════════════════════════════════════════════════════╝" -ForegroundColor Green
Write-Host ""
Write-Host "Distribution package:" -ForegroundColor Cyan
Write-Host "  $OutputZip" -ForegroundColor White
Write-Host ""
Write-Host "Installation instructions:" -ForegroundColor Cyan
Write-Host "  1. Extract the ZIP file" -ForegroundColor White
Write-Host "  2. Run install.bat as administrator" -ForegroundColor White
Write-Host "  3. Launch the configuration wizard" -ForegroundColor White
Write-Host ""
Write-Host "Distribution ready!" -ForegroundColor Green
Write-Host ""
