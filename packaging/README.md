# FedAAA Packaging

This directory contains packaging configurations for distributing FedAAA across multiple platforms.

## Supported Platforms

- **Linux**: DEB, RPM, AppImage
- **macOS**: PKG installer
- **Windows**: MSI installer

## Building Packages

### Prerequisites

#### Linux
- Go 1.21+
- nFPM (`go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`)
- AppImage tools

#### macOS
- Xcode Command Line Tools
- `pkgbuild` and `productbuild` (included with Xcode)

#### Windows
- WiX Toolset 4.0+ (`dotnet tool install --global wix`)

### Automated Builds (GitHub Actions)

Packages are automatically built on:
- Push to `main` or `develop` branches
- Pull requests
- Version tags (e.g., `v1.0.0`)

See `.github/workflows/build.yml` for the complete CI/CD pipeline.

### Manual Builds

#### Linux Packages

```bash
# Set version
export VERSION="1.0.0"

# Build binary
GOOS=linux GOARCH=amd64 go build -o dist/fedaaa-linux-amd64 ./cmd/fedaaa

# Build DEB
nfpm package --packager deb --target dist/ -f packaging/linux/nfpm.yaml

# Build RPM
nfpm package --packager rpm --target dist/ -f packaging/linux/nfpm.yaml

# Build AppImage
bash packaging/linux/build-appimage.sh
```

#### macOS Package

```bash
export VERSION="1.0.0"

# Build universal binary
GOOS=darwin GOARCH=amd64 go build -o dist/fedaaa-darwin-amd64 ./cmd/fedaaa
GOOS=darwin GOARCH=arm64 go build -o dist/fedaaa-darwin-arm64 ./cmd/fedaaa
lipo -create -output dist/fedaaa dist/fedaaa-darwin-amd64 dist/fedaaa-darwin-arm64

# Build PKG
bash packaging/macos/build-pkg.sh
```

#### Windows Package

```bash
export VERSION="1.0.0"

# Build binary
GOOS=windows GOARCH=amd64 go build -o dist/fedaaa-windows-amd64.exe ./cmd/fedaaa

# Build MSI
bash packaging/windows/build-msi.sh
```

## Package Contents

### Linux (DEB/RPM)

- **Binary**: `/usr/bin/fedaaa`
- **Config**: `/etc/fedaaa/config.yaml`
- **Data**: `/var/lib/fedaaa/`
- **Logs**: `/var/log/fedaaa/`
- **Service**: `/usr/lib/systemd/system/fedaaa.service`

**Post-Install**:
```bash
sudo systemctl enable fedaaa
sudo systemctl start fedaaa
```

### Linux (AppImage)

Self-contained executable that runs on any Linux distribution without installation.

**Usage**:
```bash
chmod +x FedAAA-*.AppImage
./FedAAA-*.AppImage
```

### macOS (PKG)

- **Binary**: `/usr/local/bin/fedaaa`
- **Config**: `/usr/local/etc/fedaaa/config.yaml`
- **Data**: `/usr/local/var/lib/fedaaa/`
- **Logs**: `/usr/local/var/log/fedaaa/`
- **Service**: `/Library/LaunchDaemons/com.soholink.fedaaa.plist`

**Post-Install**:
```bash
sudo launchctl load /Library/LaunchDaemons/com.soholink.fedaaa.plist
sudo launchctl start com.soholink.fedaaa
```

### Windows (MSI)

- **Binary**: `C:\Program Files\FedAAA\bin\fedaaa.exe`
- **Config**: `C:\Program Files\FedAAA\config\config.yaml`
- **Data**: `C:\Program Files\FedAAA\data\`
- **Logs**: `C:\Program Files\FedAAA\logs\`

**Post-Install**:
- Binary is automatically added to PATH
- Desktop and Start Menu shortcuts created
- Run as Windows Service (optional)

## Version Numbering

FedAAA uses semantic versioning (semver): `MAJOR.MINOR.PATCH`

- **MAJOR**: Incompatible API changes
- **MINOR**: New functionality (backward compatible)
- **PATCH**: Bug fixes (backward compatible)

Examples:
- `1.0.0` - First stable release
- `1.1.0` - New features added
- `1.1.1` - Bug fixes
- `2.0.0` - Breaking changes

## Signing and Verification

### Linux (DEB/RPM)
Packages should be signed with GPG:
```bash
# Sign DEB
dpkg-sig --sign builder package.deb

# Sign RPM
rpm --addsign package.rpm
```

### macOS (PKG)
Packages should be signed with Apple Developer Certificate:
```bash
productsign --sign "Developer ID Installer: Company Name" \
  unsigned.pkg signed.pkg
```

### Windows (MSI)
Packages should be signed with code signing certificate:
```bash
signtool sign /n "Network Theory Applied Research Institute Inc" /tr http://timestamp.sectigo.com /td SHA256 /fd SHA256 /d "SoHoLINK Agent" installer.msi
```

## Release Checklist

- [ ] Update version in `cmd/fedaaa/main.go`
- [ ] Update CHANGELOG.md
- [ ] Create git tag: `git tag -a v1.0.0 -m "Release 1.0.0"`
- [ ] Push tag: `git push origin v1.0.0`
- [ ] GitHub Actions builds and uploads packages
- [ ] Verify package installations on each platform
- [ ] Sign packages (if applicable)
- [ ] Publish release notes

## Configuration

Each platform includes an example configuration file:
- Linux: `/etc/fedaaa/config.yaml`
- macOS: `/usr/local/etc/fedaaa/config.yaml`
- Windows: `C:\Program Files\FedAAA\config\config.yaml`

Copy the example and customize for your deployment.

## Troubleshooting

### Linux

**Service won't start**:
```bash
# Check status
sudo systemctl status fedaaa

# View logs
sudo journalctl -u fedaaa -f

# Check permissions
ls -la /var/lib/fedaaa /var/log/fedaaa
```

### macOS

**Service won't load**:
```bash
# Check plist syntax
plutil -lint /Library/LaunchDaemons/com.soholink.fedaaa.plist

# View logs
tail -f /usr/local/var/log/fedaaa/*.log

# Check permissions
ls -la /usr/local/var/lib/fedaaa
```

### Windows

**Service won't start**:
```powershell
# Check Event Viewer
Get-EventLog -LogName Application -Source FedAAA -Newest 20

# View logs
Get-Content "C:\Program Files\FedAAA\logs\fedaaa.log" -Wait

# Check permissions
icacls "C:\Program Files\FedAAA"
```

## Support

For issues with packaging or installation:
- GitHub Issues: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/issues
- Documentation: https://docs.soholink.com

## License

See LICENSE file in repository root.
