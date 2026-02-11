# SoHoLINK Installation Guide

This guide explains how to create and distribute the SoHoLINK installation package.

## For Developers: Building the Installer

### Prerequisites

- Windows 10 or later (64-bit)
- PowerShell 5.1 or later
- Git (for version tracking)
- Optional: Go 1.22+ (will be embedded if not present)

### Building the Installer Package

The easiest way to build the complete installer is using the Makefile:

```bash
# Option 1: Build with system Go (smaller package, requires Go on target machine)
make build-installer-windows

# Option 2: Build with embedded Go (larger package, no dependencies)
make build-installer-windows-portable

# Option 3: Quick build (reuses downloaded Go from previous build)
make build-installer-windows-quick
```

Alternatively, you can run the PowerShell script directly:

```powershell
# Build with system Go
.\scripts\build-installer-windows.ps1 -Version "0.1.0"

# Build with embedded portable Go
.\scripts\build-installer-windows.ps1 -Version "0.1.0"

# Quick build (skip Go download)
.\scripts\build-installer-windows.ps1 -Version "0.1.0" -SkipGoDownload
```

### What Gets Built

The build process creates a distribution package in `dist/SoHoLINK-v{VERSION}-windows-amd64.zip` containing:

```
SoHoLINK-v0.1.0-windows-amd64.zip
├── fedaaa.exe                    # Main SoHoLINK service
├── soholink-wizard.exe           # Configuration wizard (GUI)
├── soholink-wizard-cli.exe       # Configuration wizard (CLI fallback)
├── install.bat                   # Automated installation script
├── README.txt                    # Installation instructions
├── docs/                         # Documentation
│   ├── README.md
│   ├── LICENSE.txt
│   └── ...
└── go/                           # Portable Go runtime (only in portable build)
    └── bin/
        └── go.exe
```

### Package Sizes

- **With System Go**: ~15-25 MB (requires Go on target machine)
- **With Embedded Go**: ~120-150 MB (fully portable, no dependencies)

## For End Users: Installing SoHoLINK

### System Requirements

- Windows 10 or later (64-bit)
- 4 GB RAM minimum (8 GB recommended)
- 10 GB available disk space
- Administrator privileges

### Installation Steps

1. **Download the Package**
   - Download `SoHoLINK-v{VERSION}-windows-amd64.zip` from the release page
   - Extract the ZIP file to a temporary location

2. **Run the Installer**
   - Right-click `install.bat`
   - Select "Run as administrator"
   - Follow the on-screen prompts

3. **Launch the Configuration Wizard**
   - The installer will create a desktop shortcut: "SoHoLINK Setup"
   - Double-click to launch the configuration wizard
   - OR run from Start Menu: SoHoLINK → SoHoLINK Wizard

4. **Complete the Setup Wizard**

   The wizard will guide you through:

   - **System Detection**: Automatically detects CPU, RAM, storage
   - **Cost Calculation**: Calculates electricity and hardware costs
   - **Pricing Suggestions**: Compares to AWS and suggests competitive pricing
   - **Configuration Generation**: Creates all necessary config files
   - **Dependency Check**: Verifies hypervisor and required software

5. **Start the Service**
   ```cmd
   fedaaa start
   ```

### Manual Installation (Alternative)

If you prefer not to use the automated installer:

1. **Copy Files**
   ```cmd
   mkdir "C:\Program Files\SoHoLINK"
   xcopy /E /I extracted-folder\* "C:\Program Files\SoHoLINK"
   ```

2. **Add to PATH**
   ```cmd
   setx PATH "%PATH%;C:\Program Files\SoHoLINK" /M
   ```

3. **Create Data Directories**
   ```cmd
   mkdir "%USERPROFILE%\.soholink"
   mkdir "%USERPROFILE%\.soholink\data"
   mkdir "%USERPROFILE%\.soholink\logs"
   ```

4. **Run Configuration Wizard**
   ```cmd
   "C:\Program Files\SoHoLINK\soholink-wizard.exe"
   ```

### Verifying Installation

After installation, verify everything is working:

```cmd
# Check version
fedaaa --version

# Check status
fedaaa status

# View help
fedaaa --help
```

## Configuration Wizard Features

The SoHoLINK configuration wizard provides:

### 1. System Detection

Automatically detects:
- CPU model and core count
- Total and available RAM
- Storage capacity
- Hypervisor support (Hyper-V, VirtualBox, VMware)
- Network configuration

### 2. Intelligent Cost Calculation

Calculates:
- **Electricity costs**: Based on your local rate ($/kWh)
- **Cooling costs**: Optional for dedicated server rooms
- **Hardware depreciation**: Optional hardware cost amortization
- **Total operating cost**: Complete cost breakdown

### 3. Competitive Pricing

- Compares to AWS EC2 equivalent instances
- Suggests pricing with configurable profit margin
- Shows potential monthly revenue
- Calculates break-even point

### 4. Policy Configuration

Generates secure defaults for:
- Maximum VMs per customer
- Resource limits (CPU, RAM, storage)
- Contract terms and lead times
- Rate limiting and quotas

### 5. Network Setup

- Public vs. private network mode
- RADIUS authentication setup
- Port configuration (1812/1813)
- Firewall rules

## CLI Options

If the GUI wizard fails or you prefer command-line:

```cmd
# Run CLI wizard
soholink-wizard-cli.exe

# Or use individual commands
fedaaa install              # Initialize node
fedaaa users add <name>     # Add user
fedaaa start                # Start service
fedaaa status               # Check status
```

## Troubleshooting

### Installer Issues

**Problem**: "Go not found" error
- **Solution**: Use the portable installer (`build-installer-windows-portable`) or install Go from https://go.dev/dl/

**Problem**: "Access denied" error
- **Solution**: Run `install.bat` as administrator (right-click → "Run as administrator")

**Problem**: Wizard crashes immediately
- **Solution**: Try the CLI wizard: `soholink-wizard-cli.exe`

### Configuration Issues

**Problem**: System requirements validation fails
- **Solution**: Check the detailed error message. Your system may not meet minimum requirements:
  - Minimum 4 cores recommended for provider mode
  - Minimum 8 GB RAM recommended
  - Virtualization support required (Intel VT-x or AMD-V)

**Problem**: Hypervisor not detected
- **Solution**:
  1. Enable Hyper-V: `dism.exe /online /enable-feature /featurename:Microsoft-Hyper-V-All /all`
  2. OR install VirtualBox from https://www.virtualbox.org/
  3. Restart and re-run wizard

**Problem**: Cost calculation seems wrong
- **Solution**: Verify your electricity rate (check your utility bill)
- The wizard uses industry-standard power consumption estimates
- Cooling costs are estimated at 30-50% of power consumption

### Runtime Issues

**Problem**: Service won't start
```cmd
# Check logs
type "%USERPROFILE%\.soholink\logs\fedaaa.log"

# Check RADIUS ports
netstat -an | findstr "1812 1813"

# Check configuration
type "%USERPROFILE%\.soholink\config.yaml"
```

**Problem**: Authentication failures
```cmd
# Check user status
fedaaa users list

# Verify RADIUS secret matches client configuration
fedaaa status
```

## Uninstallation

### Using the Installer

If you used the NSIS installer (Windows only):
1. Go to Settings → Apps → Apps & features
2. Find "SoHoLINK"
3. Click "Uninstall"

### Manual Uninstallation

```cmd
# Stop service
fedaaa stop

# Remove from PATH (run as admin)
# (Edit system environment variables manually)

# Remove installation directory
rmdir /S "C:\Program Files\SoHoLINK"

# Remove user data (CAUTION: This deletes all configurations and logs)
rmdir /S "%USERPROFILE%\.soholink"

# Remove desktop shortcut
del "%USERPROFILE%\Desktop\SoHoLINK Setup.lnk"
```

## Advanced: Customizing the Installer

### Modifying the Build Script

Edit `scripts/build-installer-windows.ps1`:

```powershell
# Change Go version
$GoVersion = "1.22.0"

# Change package paths
$BuildDir = Join-Path $ProjectRoot "custom-build"

# Add custom files to staging
Copy-Item "path\to\custom\file" -Destination $StagingDir
```

### Embedding Custom Configuration

Create a default `config.yaml` and add to staging:

```powershell
# In build-installer-windows.ps1, after creating $StagingDir
Copy-Item "templates\config.yaml.example" -Destination (Join-Path $StagingDir "config.yaml")
```

### Creating an NSIS Installer

For a proper Windows installer with GUI:

1. Install NSIS: https://nsis.sourceforge.io/Download
2. Edit `installer/windows/installer.nsi`
3. Run:
   ```cmd
   makensis installer\windows\installer.nsi
   ```

This creates a `SoHoLINK-Setup.exe` with:
- Install/uninstall wizard
- Start Menu shortcuts
- Automatic PATH configuration
- Registry integration
- Optional service installation

## Distribution Checklist

Before distributing the installer:

- [ ] Update version number in all files
- [ ] Test installation on clean Windows 10/11 machine
- [ ] Verify all binaries are signed (production builds)
- [ ] Test wizard with various hardware configurations
- [ ] Verify documentation is current
- [ ] Test uninstallation process
- [ ] Create release notes
- [ ] Generate checksums (SHA256)

### Generating Checksums

```powershell
# Generate SHA256 checksum
Get-FileHash dist\SoHoLINK-v0.1.0-windows-amd64.zip -Algorithm SHA256
```

Include checksum in release notes for verification.

## Getting Help

- **Documentation**: See `docs/` folder in installation directory
- **GitHub Issues**: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/issues
- **License**: AGPL-3.0 (see LICENSE.txt)

## Credits

Built by the Network Theory Applied Research Institute (NTARI)

SoHoLINK is free and open-source software supporting community networking and digital sovereignty.
