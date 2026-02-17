#!/bin/bash
set -euo pipefail

# SentryUSB Installer
# Downloads and installs the SentryUSB server binary on a Raspberry Pi

REPO="Scottmg1/Sentry-USB"
INSTALL_DIR="/opt/sentryusb"
SERVICE_NAME="sentryusb"
BINARY_NAME="sentryusb"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
err()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Must be root
if [ "$(id -u)" -ne 0 ]; then
    err "This script must be run as root. Try: sudo bash install.sh"
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    aarch64)  BINARY_SUFFIX="linux-arm64" ;;
    armv7l)   BINARY_SUFFIX="linux-armv7" ;;
    armv6l)   BINARY_SUFFIX="linux-armv7" ;;
    x86_64)   BINARY_SUFFIX="linux-amd64" ;;
    *)        err "Unsupported architecture: $ARCH" ;;
esac

info "Detected architecture: $ARCH → $BINARY_SUFFIX"

# Create install directory
mkdir -p "$INSTALL_DIR"

# Download latest release binary
info "Downloading SentryUSB binary..."
DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$BINARY_NAME-$BINARY_SUFFIX"
if command -v curl &> /dev/null; then
    curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/$BINARY_NAME" || {
        # If no release exists yet, try building from source or using a direct URL
        info "No release found. Trying alternative download..."
        err "No pre-built binary available yet. Please build from source:\n  cd server && make build-arm64"
    }
elif command -v wget &> /dev/null; then
    wget -q "$DOWNLOAD_URL" -O "$INSTALL_DIR/$BINARY_NAME" || {
        err "Failed to download binary. Please build from source."
    }
else
    err "Neither curl nor wget found. Cannot download."
fi

chmod +x "$INSTALL_DIR/$BINARY_NAME"
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
