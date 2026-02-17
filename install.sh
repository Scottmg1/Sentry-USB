#!/bin/bash -eu
#
# SentryUSB Installer
#
# This script combines:
#   1. TeslaUSB pre-setup (symlinks, partition handling, rc.local, prerequisite packages)
#   2. SentryUSB binary + systemd service installation
#
# The rc.local boot-loop mechanism (from original TeslaUSB) handles setup across
# reboots. The SentryUSB web UI provides configuration via a setup wizard.
#
# Usage:
#   sudo -i
#   curl -fsSL https://raw.githubusercontent.com/Scottmg1/Sentry-USB/main-dev/install.sh | bash

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
error_exit() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

if [[ $EUID -ne 0 ]]; then
    error_exit "This script must be run as root. Try: sudo -i"
fi

# ── Step 1: TeslaUSB Pre-Setup ─────────────────────────────────────
# (Mirrors setup/generic/install.sh from original TeslaUSB)

info "Setting up /teslausb symlink..."
if [ ! -L /teslausb ]; then
    rm -rf /teslausb
    if [ -d /boot/firmware ] && findmnt --fstab /boot/firmware &> /dev/null; then
        ln -s /boot/firmware /teslausb
    else
        ln -s /boot /teslausb
    fi
fi
ok "/teslausb -> $(readlink /teslausb)"

function flash_rapidly {
    for led in /sys/class/leds/*; do
        if [ -e "$led/trigger" ]; then
            if ! grep -q timer "$led/trigger"; then
                modprobe ledtrig-timer || true
            fi
            echo timer > "$led/trigger" || true
            if [ -e "$led/delay_off" ]; then
                echo 150 > "$led/delay_off" || true
                echo 50 > "$led/delay_on" || true
            fi
        fi
    done
}

# Handle root partition shrinking (same as TeslaUSB)
rootpart=$(findmnt -n -o SOURCE /)
rootname=$(lsblk -no pkname "${rootpart}")
rootdev="/dev/${rootname}"
marker="/root/RESIZE_ATTEMPTED"

lastpart=$(sfdisk -q -l "$rootdev" | tail +2 | sort -n -k 2 | tail -1 | awk '{print $1}')
unpart=$(sfdisk -F "$rootdev" | grep -o '[0-9]* bytes' | head -1 | awk '{print $1}')

if [ "${1:-}" != "norootshrink" ] && [ "$unpart" -lt $(( (1<<30) * 32)) ]; then
    if [ "$rootpart" != "$lastpart" ]; then
        error_exit "Insufficient unpartitioned space, and root partition is not the last partition."
    fi

    devsectorsize=$(cat "/sys/block/${rootname}/queue/hw_sector_size")
    read -r fsblockcount fsblocksize < <(tune2fs -l "${rootpart}" | grep "Block count:\|Block size:" | awk ' {print $2}' FS=: | tr -d ' ' | tr '\n' ' ' | (cat; echo))
    fsnumsectors=$((fsblockcount * fsblocksize / devsectorsize))
    partnumsectors=$(sfdisk -q -l -o Sectors "${rootdev}" | tail +2 | sort -n | tail -1)
    partnumsectors=$((partnumsectors - 1))

    if [ "$partnumsectors" -le "$fsnumsectors" ]; then
        if [ -f "$marker" ]; then
            error_exit "Previous resize attempt failed. Delete $marker before retrying."
        fi
        touch "$marker"

        info "Insufficient unpartitioned space, attempting to shrink root file system"

        cat <<- EOF > /etc/rc.local
		#!/bin/bash
		{
		  while ! curl -s https://raw.githubusercontent.com/$REPO/$BRANCH/install.sh
		  do
		    sleep 1
		  done
		} | bash
		EOF
        chmod a+x /etc/rc.local

        if [ ! -e "/boot/initrd.img-$(uname -r)" ]; then
            if [ -f /etc/os-release ] && grep -q Raspbian /etc/os-release && [ -e /teslausb/config.txt ]; then
                info "Temporarily switching Raspberry Pi OS to use initramfs"
                update-initramfs -c -k "$(uname -r)"
                echo "initramfs initrd.img-$(uname -r) followkernel # TESLAUSB-REMOVE" >> /teslausb/config.txt
            else
                error_exit "Can't automatically shrink root partition for this OS, please shrink it manually before proceeding"
            fi
        fi

        {
            while ! curl -s "https://raw.githubusercontent.com/$REPO/$BRANCH/tools/debian-resizefs.sh"; do
                sleep 1
            done
        } | bash -s 3G
        exit 0
    fi

    rm -f "$marker"
    info "Shrinking root partition to match root fs, $fsnumsectors sectors"
    sleep 3
    rootpartstartsector=$(sfdisk -q -l -o Start "${rootdev}" | tail +2 | sort -n | tail -1)
    partnum=${rootpart:0-1}
    echo "${rootpartstartsector},${fsnumsectors}" | sfdisk --force "${rootdev}" -N "${partnum}"

    if [ -e /teslausb/config.txt ] && grep -q TESLAUSB-REMOVE /teslausb/config.txt; then
        sed -i '/TESLAUSB-REMOVE/d' /teslausb/config.txt
        rm -rf "/boot/initrd.img-$(uname -r)"
    else
        update-initramfs -u
    fi

    reboot
    exit 0
fi

# Copy config template if no config exists
if [ ! -e /teslausb/teslausb_setup_variables.conf ] && [ ! -e /root/teslausb_setup_variables.conf ]; then
    info "Downloading config template..."
    while ! curl -fsSL -o /root/teslausb_setup_variables.conf \
        "https://raw.githubusercontent.com/$REPO/$BRANCH/pi-gen-sources/00-teslausb-tweaks/files/teslausb_setup_variables.conf.sample"; do
        sleep 1
    done
    ok "Config template saved to /root/teslausb_setup_variables.conf"
fi

# Download wifi config template
if [ ! -e /teslausb/wpa_supplicant.conf.sample ]; then
    while ! curl -fsSL -o /teslausb/wpa_supplicant.conf.sample \
        "https://raw.githubusercontent.com/$REPO/$BRANCH/pi-gen-sources/00-teslausb-tweaks/files/wpa_supplicant.conf.sample"; do
        sleep 1
    done
fi

# User configured networking manually, skip wifi setup in rc.local
touch /teslausb/WIFI_ENABLED

# Install rc.local — this is the boot-loop mechanism that runs setup-teslausb
# on every boot until TESLAUSB_SETUP_FINISHED exists (same as original TeslaUSB)
info "Installing rc.local (setup boot-loop)..."
rm -f /etc/rc.local
while ! curl -fsSL -o /etc/rc.local \
    "https://raw.githubusercontent.com/$REPO/$BRANCH/pi-gen-sources/00-teslausb-tweaks/files/rc.local"; do
    sleep 1
done
chmod a+x /etc/rc.local
ok "rc.local installed"

# Install prerequisite packages
info "Installing prerequisite packages..."
apt-get update -qq
for pkg in dos2unix parted fdisk sudo curl; do
    if ! command -v "$pkg" &> /dev/null; then
        apt-get install -y "$pkg" 2>/dev/null || true
    fi
done
if ! command -v sntp &> /dev/null && ! command -v ntpdig &> /dev/null; then
    apt-get install -y sntp 2>/dev/null || apt-get install -y ntpsec-ntpdig 2>/dev/null || true
fi
ok "Prerequisites installed"

# ── Step 2: Install SentryUSB Binary ───────────────────────────────

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    aarch64)  BINARY_SUFFIX="linux-arm64"; GO_ARCH="arm64" ;;
    armv7l)   BINARY_SUFFIX="linux-armv7"; GO_ARCH="armv6l" ;;
    armv6l)   BINARY_SUFFIX="linux-armv7"; GO_ARCH="armv6l" ;;
    x86_64)   BINARY_SUFFIX="linux-amd64"; GO_ARCH="amd64" ;;
    *)        error_exit "Unsupported architecture: $ARCH" ;;
esac

info "Detected architecture: $ARCH → $BINARY_SUFFIX"
mkdir -p "$INSTALL_DIR"

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
        ok "Binary downloaded from latest release"
    else
        info "No stable release found, checking pre-releases..."
        ASSET_URL=$(curl -fsSL "https://api.github.com/repos/$REPO/releases" 2>/dev/null \
            | grep -o "\"browser_download_url\": *\"[^\"]*$BINARY_NAME-$BINARY_SUFFIX\"" \
            | head -1 \
            | grep -o 'https://[^"]*' || true)
        if [ -n "$ASSET_URL" ]; then
            if curl -fsSL "$ASSET_URL" -o "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null; then
                chmod +x "$INSTALL_DIR/$BINARY_NAME"
                BINARY_INSTALLED=true
                ok "Binary downloaded from pre-release"
            fi
        fi
        if [ "$BINARY_INSTALLED" = false ]; then
            warn "No release binary found (this is normal for first-time setup)"
        fi
    fi
fi

# Option C: Build from source on the Pi
if [ "$BINARY_INSTALLED" = false ]; then
    info "Building from source..."

    if ! command -v go &> /dev/null; then
        info "Installing Go ${GO_VERSION}..."
        GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
        curl -fsSL "https://go.dev/dl/${GO_TAR}" -o "/tmp/${GO_TAR}" || error_exit "Failed to download Go"
        rm -rf /usr/local/go
        tar -C /usr/local -xzf "/tmp/${GO_TAR}"
        rm "/tmp/${GO_TAR}"
        export PATH="/usr/local/go/bin:$PATH"
        ok "Go ${GO_VERSION} installed"
    fi

    if ! command -v node &> /dev/null; then
        info "Installing Node.js..."
        curl -fsSL https://deb.nodesource.com/setup_20.x | bash - 2>/dev/null
        apt-get install -y nodejs 2>/dev/null || {
            warn "NodeSource setup failed, trying apt default..."
            apt-get install -y nodejs npm 2>/dev/null || error_exit "Failed to install Node.js"
        }
        ok "Node.js installed: $(node --version)"
    fi

    BUILD_DIR="/tmp/sentryusb-build"
    rm -rf "$BUILD_DIR"
    info "Cloning repository..."
    if command -v git &> /dev/null; then
        git clone --depth 1 -b "$BRANCH" "https://github.com/$REPO.git" "$BUILD_DIR"
    else
        mkdir -p "$BUILD_DIR"
        curl -fsSL "https://github.com/$REPO/archive/$BRANCH.tar.gz" | tar xz --strip-components=1 -C "$BUILD_DIR"
    fi

    info "Building frontend..."
    cd "$BUILD_DIR/web"
    npm ci --no-audit --no-fund 2>&1 | tail -3
    npm run build 2>&1 | tail -5
    ok "Frontend built"

    info "Building server binary..."
    cd "$BUILD_DIR/server"
    make build 2>&1 | tail -3
    cp bin/sentryusb "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    BINARY_INSTALLED=true
    ok "Binary built and installed"
    rm -rf "$BUILD_DIR"
fi

if [ "$BINARY_INSTALLED" = false ]; then
    error_exit "Failed to obtain SentryUSB binary. Try building on another machine:\n  cd server && make build-arm64\n  scp bin/sentryusb-linux-arm64 pi@<pi-ip>:/tmp/sentryusb\n  Then run: bash install.sh /tmp/sentryusb"
fi

ok "Binary installed to $INSTALL_DIR/$BINARY_NAME"

# ── Step 3: Install systemd service ────────────────────────────────

info "Installing systemd service..."
cat > "/etc/systemd/system/$SERVICE_NAME.service" << EOF
[Unit]
Description=SentryUSB Web Server
After=network-online.target
Wants=network-online.target
Conflicts=nginx.service

[Service]
Type=simple
ExecStartPre=-/bin/systemctl stop nginx
ExecStartPre=-/bin/systemctl disable nginx
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

# ── Step 4: Remove stale cached setup scripts ──────────────────────
# Force fresh download on next setup run so latest fixes are used
rm -f /root/bin/setup-teslausb /root/bin/envsetup.sh

# ── Done ───────────────────────────────────────────────────────────

sleep 2
if systemctl is-active --quiet "$SERVICE_NAME"; then
    ok "SentryUSB is running!"
    echo ""
    echo -e "  ${GREEN}Open your browser to:${NC}"
    IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    if [ -n "$IP" ]; then
        echo -e "    http://$IP"
    fi
    HOSTNAME=$(hostname 2>/dev/null)
    echo -e "    http://${HOSTNAME}.local"
    echo ""
    echo -e "  Configure via the ${BLUE}Setup Wizard${NC} in the web UI."
    echo -e "  Or edit /root/teslausb_setup_variables.conf and run /etc/rc.local"
    echo ""
else
    error_exit "Service failed to start. Check: journalctl -u $SERVICE_NAME -f"
fi
