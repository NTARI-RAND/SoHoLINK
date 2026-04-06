#!/usr/bin/env bash
# SoHoLINK — full server deployment script (Ubuntu 24, single command)
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/NetworkTheoryAppliedResearchInstitute/soholink/master/deploy/install.sh)
# Make executable: chmod +x deploy/install.sh
set -euo pipefail

# ── Clone the repo ────────────────────────────────────────────────────────────
git clone https://github.com/NetworkTheoryAppliedResearchInstitute/soholink /opt/soholink
cd /opt/soholink

# ── Install Go 1.24.2 ─────────────────────────────────────────────────────────
wget -q https://go.dev/dl/go1.24.2.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.24.2.linux-amd64.tar.gz
rm go1.24.2.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin

# ── Build the backend (vendored — no network required) ───────────────────────
make build-vendor
# Binary lands at bin/fedaaa

# ── Install Caddy from the official apt repo ──────────────────────────────────
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudflare.com/direct/caddy/stable/gpg.key' \
    | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudflare.com/direct/caddy/stable/debian.deb.txt' \
    | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy

# ── Install and enable the fedaaa systemd service ─────────────────────────────
sudo cp /opt/soholink/deploy/soholink.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable soholink
sudo systemctl start soholink

# ── Point Caddy at the SoHoLINK Caddyfile ────────────────────────────────────
sudo mkdir -p /etc/systemd/system/caddy.service.d
sudo tee /etc/systemd/system/caddy.service.d/override.conf > /dev/null <<'EOF'
[Service]
ExecStart=
ExecStart=/usr/bin/caddy run --environ --config /opt/soholink/Caddyfile
EOF
sudo systemctl daemon-reload
sudo systemctl restart caddy

echo ""
echo "SoHoLINK deployed."
echo "  Backend:  sudo systemctl status soholink"
echo "  Caddy:    sudo systemctl status caddy"
