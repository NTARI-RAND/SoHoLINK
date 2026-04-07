# SoHoLINK Quick Start Guide

Get up and running with SoHoLINK in under 15 minutes!

---

## For End Users: Pre-Built Installer (Recommended)

### Windows Installation

1. **Download the installer package**
   - Download `SoHoLINK-v{VERSION}-windows-amd64.zip` from releases
   - Extract the ZIP file

2. **Run the installer**
   - Right-click `install.bat` → "Run as administrator"
   - Follow the installation prompts

3. **Launch the configuration wizard**
   - Double-click the desktop shortcut "SoHoLINK Setup"
   - OR run from Start Menu: SoHoLINK → SoHoLINK Wizard
   - The wizard will:
     - Detect your system hardware
     - Calculate operating costs
     - Suggest competitive pricing
     - Generate complete configuration

4. **Start the service**
   ```cmd
   fedaaa start
   ```

**See [INSTALLER.md](INSTALLER.md) for complete installation instructions.**

---

## For Developers: Building from Source

### Prerequisites

#### Required
- **Go 1.22+** - [Download from golang.org](https://go.dev/dl/)
- **Git** - For version control

#### Optional (for GUI)
- **GUI Dependencies** (only if you want the graphical wizard)
  - Linux: `sudo apt-get install gcc libgl1-mesa-dev xorg-dev`
  - macOS: `xcode-select --install`
  - Windows: No additional dependencies needed

---

## Step 1: Build the Application

### Option A: Build Complete Installer Package (Recommended)

```bash
# Build complete Windows installer with embedded Go
make build-installer-windows-portable

# Or build with system Go (smaller package)
make build-installer-windows

# Output: dist/SoHoLINK-v0.1.0-windows-amd64.zip
```

This creates a complete distribution package ready for deployment.

### Option B: Build Individual Components

```bash
# Build main service
make build-cli

# Build configuration wizards
make build-wizards

# Build all
make all
```

**Outputs:**
- `bin/fedaaa.exe` - Main service
- `bin/soholink-wizard.exe` - GUI configuration wizard
- `bin/soholink-wizard-cli.exe` - CLI configuration wizard

---

## Step 2: Available Commands

Once built, run the executable to see available commands:

```bash
# Show help
.\fedaaa.exe --help
```

### Main Commands

| Command | Description |
|---------|-------------|
| `fedaaa install` | Run installation wizard (creates config, directories, database) |
| `fedaaa start` | Start the FedAAA server |
| `fedaaa status` | Check server status |
| `fedaaa users` | User management commands |
| `fedaaa lbtas` | LBTAS (reputation/accounting) commands |
| `fedaaa logs` | View server logs |
| `fedaaa policy` | Policy management |
| `fedaaa version` | Show version information |

### Global Flags

```bash
--config FILE      # Custom config file location
--data-dir DIR     # Custom data directory
-v, --verbose      # Enable verbose output
```

---

## Step 3: First-Time Setup

### Method 1: CLI Installer (Headless/Server)

```bash
# Run the installation wizard
.\fedaaa.exe install

# The wizard will prompt you for:
# - Node name
# - RADIUS auth port (default: 1812)
# - RADIUS accounting port (default: 1813)
# - Data directory
# - Shared secret
```

### Method 2: GUI Installer (Desktop)

```bash
# Launch the graphical installer
.\fedaaa-gui.exe

# The GUI wizard will guide you through:
# 1. Welcome screen
# 2. License acceptance
# 3. Deployment mode (Standalone/SaaS)
# 4. Basic configuration
# 5. Advanced features (P2P, updates, metrics, payments)
# 6. Review and install
```

---

## Step 4: Launch the Server

### Start the Server

```bash
# Start FedAAA server
.\fedaaa.exe start

# Or with custom config
.\fedaaa.exe start --config config.yaml

# Or with verbose logging
.\fedaaa.exe start -v
```

### What Runs When You Start

When you run `fedaaa start`, the following services start:
- **RADIUS Server**: Authentication (port 1812) and Accounting (port 1813)
- **HTTP API**: REST API server (default port 8080)
- **P2P Network**: Mesh networking (port 9090, if enabled)
- **Prometheus Metrics**: Monitoring endpoint (port 9100, if enabled)
- **GraphQL API**: Query interface
- **Dashboard**: Web dashboard

### Check Status

```bash
# Check if server is running
.\fedaaa.exe status

# View recent logs
.\fedaaa.exe logs --tail 50
```

---

## Step 5: Access the Services

Once the server is running, you can access:

### HTTP REST API
```
http://localhost:8080/api/
```

**Example Endpoints:**
- `GET /api/workloads` - List workloads
- `GET /api/services` - List services
- `GET /api/revenue/balance` - Check revenue
- `GET /api/storage/buckets` - List storage buckets

### GraphQL API
```
http://localhost:8080/graphql
```

### Prometheus Metrics (if enabled)
```
http://localhost:9100/metrics
```

### Dashboard (Web UI)
```
http://localhost:8080/dashboard
```

---

## Step 6: Basic Operations

### User Management

```bash
# Add a user
.\fedaaa.exe users add <username>

# List users
.\fedaaa.exe users list

# Remove a user
.\fedaaa.exe users remove <username>
```

### View Logs

```bash
# View recent logs (last 50 lines)
.\fedaaa.exe logs --tail 50

# Follow logs in real-time
.\fedaaa.exe logs --follow

# Filter by level
.\fedaaa.exe logs --level error
```

### Policy Management

```bash
# Validate policy file
.\fedaaa.exe policy validate policy.rego

# Reload policy
.\fedaaa.exe policy reload
```

### LBTAS (Reputation/Accounting)

```bash
# View reputation scores
.\fedaaa.exe lbtas reputation

# View accounting records
.\fedaaa.exe lbtas accounting

# Generate report
.\fedaaa.exe lbtas report --output report.json
```

---

## Configuration

### Default Configuration Locations

**Windows:**
- Config: `C:\ProgramData\fedaaa\config.yaml`
- Data: `C:\ProgramData\fedaaa\data\`
- Logs: `C:\ProgramData\fedaaa\logs\`

**Linux:**
- Config: `/etc/fedaaa/config.yaml`
- Data: `/var/lib/fedaaa/`
- Logs: `/var/log/fedaaa/`

**macOS:**
- Config: `~/Library/Application Support/fedaaa/config.yaml`
- Data: `~/Library/Application Support/fedaaa/data/`
- Logs: `~/Library/Logs/fedaaa/`

### Example Configuration

```yaml
# config.yaml
server:
  listen_address: "0.0.0.0:8080"

radius:
  auth_address: "0.0.0.0:1812"
  acct_address: "0.0.0.0:1813"
  shared_secret: "your-secret-here"

node:
  name: "my-fedaaa-node"

storage:
  base_path: "C:\\ProgramData\\fedaaa\\data"

p2p:
  enabled: true
  listen_port: 9090

monitoring:
  prometheus:
    enabled: true
    port: 9100

updates:
  enabled: true
  check_interval: "24h"
```

---

## Development Mode

### Run Without Building

```bash
# Run CLI directly (no build)
go run ./cmd/fedaaa --help

# Run with specific command
go run ./cmd/fedaaa install

# Run GUI directly
go run -tags gui ./cmd/fedaaa-gui
```

### Run Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/radius/...

# Run GUI tests (requires build tag)
go test -tags gui ./internal/gui/dashboard/...
```

---

## Advanced Usage

### Custom Config File

```bash
# Start with custom config
.\fedaaa.exe start --config my-config.yaml
```

### Custom Data Directory

```bash
# Use custom data directory
.\fedaaa.exe start --data-dir "D:\fedaaa-data"
```

### Running as a Service

#### Windows Service

```powershell
# Install as Windows service (requires admin)
sc create FedAAA binPath= "C:\path\to\fedaaa.exe start"
sc start FedAAA

# Stop service
sc stop FedAAA

# Remove service
sc delete FedAAA
```

#### Linux Systemd

```bash
# Using the DEB/RPM package (recommended)
sudo systemctl start fedaaa
sudo systemctl enable fedaaa
sudo systemctl status fedaaa

# View logs
sudo journalctl -u fedaaa -f
```

#### macOS LaunchDaemon

```bash
# Using the PKG installer (recommended)
sudo launchctl load /Library/LaunchDaemons/com.soholink.fedaaa.plist
sudo launchctl start com.soholink.fedaaa

# View logs
tail -f /usr/local/var/log/fedaaa/*.log
```

---

## Troubleshooting

### Server Won't Start

1. **Check if port is already in use:**
   ```bash
   netstat -an | findstr :1812
   netstat -an | findstr :8080
   ```

2. **Check logs:**
   ```bash
   .\fedaaa.exe logs --tail 100
   ```

3. **Verify configuration:**
   ```bash
   # Check if config file exists
   dir "C:\ProgramData\fedaaa\config.yaml"
   ```

4. **Run in verbose mode:**
   ```bash
   .\fedaaa.exe start -v
   ```

### Build Errors

1. **Missing Go:**
   - Install Go from https://go.dev/dl/
   - Add to PATH: `C:\Program Files\Go\bin`

2. **Missing dependencies:**
   ```bash
   go mod download
   go mod tidy
   ```

3. **GUI build fails:**
   - Install platform-specific GUI dependencies (see Prerequisites)
   - Or build CLI-only: `go build -o fedaaa.exe ./cmd/fedaaa`

### Permission Errors

**Windows:**
```powershell
# Run as Administrator
# Right-click fedaaa.exe → Run as administrator
```

**Linux:**
```bash
# RADIUS ports require root/sudo
sudo ./fedaaa start

# Or use higher ports (>1024) in config
```

---

## Quick Reference

### Most Common Commands

```bash
# First time setup
.\fedaaa.exe install

# Start server
.\fedaaa.exe start

# Check status
.\fedaaa.exe status

# View logs
.\fedaaa.exe logs --tail 50

# Add user
.\fedaaa.exe users add alice

# Stop server (Ctrl+C or kill process)
```

### Key Files

```
fedaaa.exe              # Main CLI executable
fedaaa-gui.exe          # GUI executable (optional)
config.yaml             # Configuration file
data/                   # Database and storage
logs/                   # Log files
policy.rego             # OPA policy (optional)
```

---

## Next Steps

1. **Configure Payment Processors** (if needed)
   - Edit `config.yaml` to add Stripe/Lightning credentials
   - See documentation: `docs/payment-processors.md`

2. **Set Up P2P Mesh** (if needed)
   - P2P is auto-enabled with mDNS discovery
   - Configure bootstrap nodes if needed

3. **Enable Monitoring**
   - Prometheus metrics available at `:9100/metrics`
   - Set up Grafana dashboards

4. **Configure Auto-Updates**
   - Updates check daily by default
   - Configure update endpoint in config

5. **Review Security**
   - Change default shared secret
   - Configure TLS certificates
   - Review firewall rules

---

## Getting Help

### Documentation
- `BUILD.md` - Build instructions
- `DEVELOPMENT.md` - Development guide
- `packaging/README.md` - Packaging guide
- `PHASE4_COMPLETE.md` - Feature documentation

### Command Help
```bash
.\fedaaa.exe --help
.\fedaaa.exe install --help
.\fedaaa.exe start --help
```

### Support
- GitHub Issues: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/issues
- Documentation: https://docs.soholink.com

---

## Summary

**Quick Start (3 steps):**

1. Build: `go build -o fedaaa.exe ./cmd/fedaaa`
2. Install: `.\fedaaa.exe install`
3. Run: `.\fedaaa.exe start`

**Access:** `http://localhost:8080/api/`

That's it! FedAAA is now running. 🚀

