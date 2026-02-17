#!/bin/bash
set -euo pipefail

# SentryUSB Installer
# Downloads or builds and installs the SentryUSB server binary on a Raspberry Pi
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/.../install.sh | bash
#   bash install.sh                      # download release or build from source
#   bash install.sh /path/to/binary      # install a pre-built binary directly

REPO="Scottmg1/Sentry-USB"
BRANCH="main-dev"
INSTALL_DIR="/opt/sentryusb"
SERVICE_NAME="sentryusb"
BINARY_NAME="sentryusb"
GO_VERSION="1.23.4"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
err()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Must be root
if [ "$(id -u)" -ne 0 ]; then
    err "This script must be run as root. Try: sudo bash install.sh"
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    aarch64)  BINARY_SUFFIX="linux-arm64"; GO_ARCH="arm64" ;;
    armv7l)   BINARY_SUFFIX="linux-armv7"; GO_ARCH="armv6l" ;;
    armv6l)   BINARY_SUFFIX="linux-armv7"; GO_ARCH="armv6l" ;;
    x86_64)   BINARY_SUFFIX="linux-amd64"; GO_ARCH="amd64" ;;
    *)        err "Unsupported architecture: $ARCH" ;;
esac

info "Detected architecture: $ARCH → $BINARY_SUFFIX"

# Create install directory
mkdir -p "$INSTALL_DIR"

# ── Step 1: Get the binary ──────────────────────────────────────────

BINARY_INSTALLED=false

# Option A: User provided a local binary path as argument
if [ "${1:-}" != "" ] && [ -f "${1:-}" ]; then
    info "Installing from local binary: $1"
    cp "$1" "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    BINARY_INSTALLED=true
    ok "Binary installed from local file"
fi

# Option B: Download from GitHub Releases
if [ "$BINARY_INSTALLED" = false ]; then
    info "Downloading SentryUSB binary from GitHub Releases..."
    DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$BINARY_NAME-$BINARY_SUFFIX"
    if curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null; then
        chmod +x "$INSTALL_DIR/$BINARY_NAME"
        BINARY_INSTALLED=true
        ok "Binary downloaded from release"
    else
        warn "No release binary found (this is normal for first-time setup)"
    fi
fi

# Option C: Build from source on the Pi
if [ "$BINARY_INSTALLED" = false ]; then
    info "Building from source..."

    # Install Go if not present
    if ! command -v go &> /dev/null; then
        info "Installing Go ${GO_VERSION}..."
        GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
        curl -fsSL "https://go.dev/dl/${GO_TAR}" -o "/tmp/${GO_TAR}" || err "Failed to download Go"
        rm -rf /usr/local/go
        tar -C /usr/local -xzf "/tmp/${GO_TAR}"
        rm "/tmp/${GO_TAR}"
        export PATH="/usr/local/go/bin:$PATH"
        ok "Go ${GO_VERSION} installed"
    else
        info "Go already installed: $(go version)"
    fi

    # Install Node.js/npm if not present (for frontend build)
    if ! command -v node &> /dev/null; then
        info "Installing Node.js..."
        if command -v apt-get &> /dev/null; then
            curl -fsSL https://deb.nodesource.com/setup_20.x | bash - 2>/dev/null
            apt-get install -y nodejs 2>/dev/null || {
                # Fallback: install from NodeSource manually
                warn "NodeSource setup failed, trying apt default..."
                apt-get install -y nodejs npm 2>/dev/null || err "Failed to install Node.js"
            }
        else
            err "Cannot install Node.js automatically. Please install it first."
        fi
        ok "Node.js installed: $(node --version)"
    fi

    # Clone repo
    BUILD_DIR="/tmp/sentryusb-build"
    rm -rf "$BUILD_DIR"
    info "Cloning repository..."
    if command -v git &> /dev/null; then
        git clone --depth 1 -b "$BRANCH" "https://github.com/$REPO.git" "$BUILD_DIR"
    else
        info "git not found, downloading tarball..."
        mkdir -p "$BUILD_DIR"
        curl -fsSL "https://github.com/$REPO/archive/$BRANCH.tar.gz" | tar xz --strip-components=1 -C "$BUILD_DIR"
    fi

    # Build frontend
    info "Building frontend (this may take a few minutes on Pi)..."
    cd "$BUILD_DIR/web"
    npm ci --no-audit --no-fund 2>&1 | tail -3
    npm run build 2>&1 | tail -5
    ok "Frontend built"

    # Build backend with embedded frontend
    info "Building server binary..."
    cd "$BUILD_DIR/server"
    make build 2>&1 | tail -3
    cp bin/sentryusb "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    BINARY_INSTALLED=true
    ok "Binary built and installed"

    # Cleanup
    rm -rf "$BUILD_DIR"
fi

if [ "$BINARY_INSTALLED" = false ]; then
    err "Failed to obtain SentryUSB binary. Try building on another machine:\n  cd server && make build-arm64\n  scp bin/sentryusb-linux-arm64 pi@<pi-ip>:/tmp/sentryusb\n  Then run: bash install.sh /tmp/sentryusb"
fi

ok "Binary installed to $INSTALL_DIR/$BINARY_NAME"

# Create config file if it doesn't exist
CONFIG_PATH="/root/teslausb_setup_variables.conf"
if [ ! -f "$CONFIG_PATH" ]; then
    BOOT_CONFIG="/boot/firmware/teslausb_setup_variables.conf"
    BOOT_CONFIG_ALT="/boot/teslausb_setup_variables.conf"
    if [ -f "$BOOT_CONFIG" ]; then
        info "Found config at $BOOT_CONFIG"
    elif [ -f "$BOOT_CONFIG_ALT" ]; then
        info "Found config at $BOOT_CONFIG_ALT"
    else
        info "Creating initial config file at $CONFIG_PATH"
        cat > "$CONFIG_PATH" << 'CONF'
# SentryUSB Configuration
# Configure these values via the web UI Setup Wizard at http://sentryusb.local
# Or edit this file directly and re-run setup.

#export SSID='YourWiFiSSID'
#export WIFIPASS='YourWiFiPassword'
#export TESLAUSB_HOSTNAME=sentryusb

#export ARCHIVE_SYSTEM=cifs
#export ARCHIVE_SERVER=your-server
#export SHARE_NAME=your-share
#export SHARE_USER=your-user
#export SHARE_PASSWORD='your-password'

#export CAM_SIZE=40G
#export MUSIC_SIZE=
#export LIGHTSHOW_SIZE=
#export BOOMBOX_SIZE=
CONF
        ok "Created $CONFIG_PATH"
    fi
fi

# Create systemd service
info "Installing systemd service..."
cat > "/etc/systemd/system/$SERVICE_NAME.service" << EOF
[Unit]
Description=SentryUSB Web Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$BINARY_NAME -port 80
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"
ok "Service installed and started"

# Check if it's running
sleep 2
if systemctl is-active --quiet "$SERVICE_NAME"; then
    ok "SentryUSB is running!"
    echo ""
    echo -e "  ${GREEN}Open your browser to:${NC}"
    # Try to get the IP
    IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    if [ -n "$IP" ]; then
        echo -e "    http://$IP"
    fi
    HOSTNAME=$(hostname 2>/dev/null)
    echo -e "    http://${HOSTNAME}.local"
    echo ""
    echo -e "  Then click ${BLUE}Settings → Open Wizard${NC} to configure."
    echo ""
else
    err "Service failed to start. Check: journalctl -u $SERVICE_NAME -f"
fi
