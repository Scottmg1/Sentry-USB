#!/bin/bash -eu
#
# SentryUSB Installer
#
# This script combines:
#   1. SentryUSB pre-setup (symlinks, partition handling, rc.local, prerequisite packages)
#   2. SentryUSB binary + systemd service installation
#
# The rc.local boot-loop mechanism handles setup across reboots.
# The SentryUSB web UI provides configuration via a setup wizard.
#
# Usage:
#   sudo -i
#   curl -fsSL https://sentryusb.sentry-six.com | bash

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

# ── Step 1: SentryUSB Pre-Setup ─────────────────────────────────────

info "Setting up /sentryusb symlink..."
if [ ! -L /sentryusb ]; then
    rm -rf /sentryusb
    if [ -d /boot/firmware ] && findmnt --fstab /boot/firmware &> /dev/null; then
        ln -s /boot/firmware /sentryusb
    else
        ln -s /boot /sentryusb
    fi
fi
ok "/sentryusb -> $(readlink /sentryusb)"

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

# Handle root partition shrinking
rootpart=$(findmnt -n -o SOURCE /)
rootname=$(lsblk -no pkname "${rootpart}")
rootdev="/dev/${rootname}"
marker="/root/RESIZE_ATTEMPTED"

lastpart=$(sfdisk -q -l "$rootdev" | tail +2 | sort -n -k 2 | tail -1 | awk '{print $1}')
unpart=$(sfdisk -F "$rootdev" | grep -o '[0-9]* bytes' | head -1 | awk '{print $1}')

# Dynamic threshold: at least 40% of disk should be unpartitioned for SentryUSB data.
# Old hardcoded 32 GiB limit caused infinite loops on cards smaller than ~38 GB
# because even after shrinking root, unpartitioned space never reached 32 GiB.
disksize_bytes=$(blockdev --getsize64 "$rootdev")
min_free_bytes=$((disksize_bytes * 40 / 100))
if [ "$min_free_bytes" -lt $((4 * (1<<30))) ]; then
    min_free_bytes=$((4 * (1<<30)))
fi

if [ "${1:-}" != "norootshrink" ] && [ "$unpart" -lt "$min_free_bytes" ]; then
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
            if [ -t 0 ]; then
                warn "Previous resize attempt failed. Retrying..."
                rm -f "$marker"
            else
                error_exit "Previous resize attempt failed. Delete $marker before retrying."
            fi
        fi
        touch "$marker"

        # Calculate a safe resize target: current usage + 2G headroom, minimum 6G
        used_kb=$(df --output=used -k / | tail -1 | tr -d ' ')
        target_gb=$(( (used_kb / 1024 / 1024) + 2 ))
        if [ "$target_gb" -lt 6 ]; then
            target_gb=6
        fi
        info "Root filesystem uses ~$((used_kb / 1024 / 1024))G, will shrink to ${target_gb}G"

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

        INITRD_NAME="initrd.img-$(uname -r)"
        # On Bookworm the boot partition is /boot/firmware/, not /boot/.
        # The bootloader loads files relative to the boot partition, so the
        # initramfs must live there, but update-initramfs writes to /boot/.
        BOOT_PART="$(readlink -f /sentryusb)"
        if [ ! -e "${BOOT_PART}/${INITRD_NAME}" ] && [ ! -e "/boot/${INITRD_NAME}" ]; then
            if [ -f /etc/os-release ] && grep -q Raspbian /etc/os-release && [ -e /sentryusb/config.txt ]; then
                info "Temporarily switching Raspberry Pi OS to use initramfs"
                update-initramfs -c -k "$(uname -r)"
                echo "initramfs ${INITRD_NAME} followkernel # SENTRYUSB-REMOVE" >> /sentryusb/config.txt
            else
                error_exit "Can't automatically shrink root partition for this OS, please shrink it manually before proceeding"
            fi
        fi
        # Ensure initramfs is on the boot partition where the bootloader can find it
        if [ "/boot" != "${BOOT_PART}" ] && [ -e "/boot/${INITRD_NAME}" ]; then
            cp "/boot/${INITRD_NAME}" "${BOOT_PART}/${INITRD_NAME}"
        fi

        {
            while ! curl -s "https://raw.githubusercontent.com/$REPO/$BRANCH/tools/debian-resizefs.sh"; do
                sleep 1
            done
        } | bash -s "${target_gb}G"
        exit 0
    fi

    rm -f "$marker"
    info "Shrinking root partition to match root fs, $fsnumsectors sectors"
    sleep 3
    rootpartstartsector=$(sfdisk -q -l -o Start "${rootdev}" | tail +2 | sort -n | tail -1)
    partnum=$(echo "$rootpart" | grep -o '[0-9]*$')
    echo "${rootpartstartsector},${fsnumsectors}" | sfdisk --force "${rootdev}" -N "${partnum}"

    if [ -e /sentryusb/config.txt ] && grep -q SENTRYUSB-REMOVE /sentryusb/config.txt; then
        sed -i '/SENTRYUSB-REMOVE/d' /sentryusb/config.txt
        rm -rf "/boot/initrd.img-$(uname -r)"
    else
        update-initramfs -u
    fi

    reboot
    exit 0
fi

# Copy config template if no config exists
if [ ! -e /sentryusb/sentryusb.conf ] && [ ! -e /root/sentryusb.conf ]; then
    info "Downloading config template..."
    while ! curl -fsSL -o /root/sentryusb.conf \
        "https://raw.githubusercontent.com/$REPO/$BRANCH/pi-gen-sources/00-sentryusb-tweaks/files/sentryusb.conf.sample"; do
        sleep 1
    done
    ok "Config template saved to /root/sentryusb.conf"
fi

# Download wifi config template
if [ ! -e /sentryusb/wpa_supplicant.conf.sample ]; then
    while ! curl -fsSL -o /sentryusb/wpa_supplicant.conf.sample \
        "https://raw.githubusercontent.com/$REPO/$BRANCH/pi-gen-sources/00-sentryusb-tweaks/files/wpa_supplicant.conf.sample"; do
        sleep 1
    done
fi

# User configured networking manually, skip wifi setup in rc.local
touch /sentryusb/WIFI_ENABLED

# Install rc.local — this is the boot-loop mechanism that runs setup-sentryusb
# on every boot until SENTRYUSB_SETUP_FINISHED exists
info "Installing rc.local (setup boot-loop)..."
rm -f /etc/rc.local
while ! curl -fsSL -o /etc/rc.local \
    "https://raw.githubusercontent.com/$REPO/$BRANCH/pi-gen-sources/00-sentryusb-tweaks/files/rc.local"; do
    sleep 1
done
chmod a+x /etc/rc.local
ok "rc.local installed"

# Install prerequisite packages
info "Installing prerequisite packages..."
apt-get update -qq
for pkg in dos2unix parted fdisk sudo curl rsync; do
    if ! command -v "$pkg" &> /dev/null; then
        apt-get install -y "$pkg" 2>/dev/null || true
    fi
done
if ! command -v sntp &> /dev/null && ! command -v ntpdig &> /dev/null; then
    apt-get install -y sntp 2>/dev/null || apt-get install -y ntpsec-ntpdig 2>/dev/null || true
fi
ok "Prerequisites installed"

# ── Set hostname early so sentryusb.local works before setup wizard ──
DESIRED_HOSTNAME="sentryusb"
CURRENT_HOSTNAME=$(hostname 2>/dev/null)
if [ "$CURRENT_HOSTNAME" != "$DESIRED_HOSTNAME" ]; then
    info "Setting hostname to '$DESIRED_HOSTNAME' (was '$CURRENT_HOSTNAME')..."
    hostnamectl set-hostname "$DESIRED_HOSTNAME" 2>/dev/null || {
        echo "$DESIRED_HOSTNAME" > /etc/hostname
        hostname "$DESIRED_HOSTNAME"
    }
    # Update /etc/hosts so hostname resolves locally via 127.0.1.1
    if grep -q "^127\.0\.1\.1" /etc/hosts 2>/dev/null; then
        sed -i "s/^127\.0\.1\.1.*/127.0.1.1\t$DESIRED_HOSTNAME/" /etc/hosts 2>/dev/null
    else
        echo "127.0.1.1	$DESIRED_HOSTNAME" >> /etc/hosts
    fi
    systemctl enable avahi-daemon 2>/dev/null || true
    systemctl restart avahi-daemon 2>/dev/null || true
    ok "Hostname set to '$DESIRED_HOSTNAME' — ${DESIRED_HOSTNAME}.local is now available"
else
    # Still ensure avahi is running even if hostname is correct
    if ! command -v avahi-daemon &> /dev/null; then
        apt-get install -y avahi-daemon 2>/dev/null || true
    fi
    systemctl enable avahi-daemon 2>/dev/null || true
    ok "Hostname already set to '$DESIRED_HOSTNAME'"
fi

# Always install/refresh avahi mDNS service for iOS app discovery
# (runs regardless of whether hostname changed, so upgrades get it too)
if ! command -v avahi-daemon &> /dev/null; then
    apt-get install -y avahi-daemon 2>/dev/null || true
fi
mkdir -p /etc/avahi/services
curl -fsSL "https://raw.githubusercontent.com/$REPO/$BRANCH/setup/pi/avahi-sentryusb.service" \
    -o /etc/avahi/services/sentryusb.service 2>/dev/null || \
    warn "Failed to install avahi mDNS service (iOS auto-discovery may not work)"
systemctl restart avahi-daemon 2>/dev/null || true

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
        RELEASE_TAG=$(curl -fsSL --max-time 10 "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
            | grep '"tag_name"' | head -1 \
            | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/' || true)
        if [ -n "${RELEASE_TAG:-}" ]; then
            echo "$RELEASE_TAG" > "$INSTALL_DIR/version"
            ok "Version $RELEASE_TAG"
        fi
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
                RELEASE_TAG=$(echo "$ASSET_URL" | sed 's|.*/releases/download/\([^/]*\)/.*|\1|' || true)
                if [ -n "${RELEASE_TAG:-}" ]; then
                    echo "$RELEASE_TAG" > "$INSTALL_DIR/version"
                    ok "Version $RELEASE_TAG"
                fi
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
    mkdir -p static
    if make build 2>&1 | tail -5; then
        cp bin/sentryusb "$INSTALL_DIR/$BINARY_NAME"
        chmod +x "$INSTALL_DIR/$BINARY_NAME"
        BINARY_INSTALLED=true
        ok "Binary built and installed"
        echo "dev" > "$INSTALL_DIR/version"
    else
        warn "Go build failed -- check output above"
    fi
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

# ── Step 3b: Install BLE peripheral daemon ────────────────────────

info "Installing BLE peripheral daemon..."
# Install python3-dbus and python3-gi if not present
for pkg in python3-dbus python3-gi bluez; do
    if ! dpkg-query -W --showformat='${db:Status-Status}\n' "$pkg" 2>/dev/null | grep -q '^installed$'; then
        apt-get install -y "$pkg" 2>/dev/null || warn "Failed to install $pkg"
    fi
done

# Download BLE daemon script to /root/bin (matching sentryusb-ble.service ExecStart path)
mkdir -p /root/bin
curl -fsSL "https://raw.githubusercontent.com/$REPO/$BRANCH/server/ble/sentryusb-ble.py" -o /root/bin/sentryusb-ble.py 2>/dev/null || \
    warn "Failed to download sentryusb-ble.py"
chmod +x /root/bin/sentryusb-ble.py 2>/dev/null || true

# BlueZ 5.69+ on Raspberry Pi requires --experimental for reliable LE peripheral
# advertisement registration (org.bluez.Error.Failed without it on many Pi models)
# Detect actual bluetoothd binary path from the running service to avoid wrong-path crash
BTDAEMON=$(systemctl cat bluetooth.service 2>/dev/null | grep '^ExecStart=' | head -1 | sed 's/ExecStart=//' | awk '{print $1}')
BTDAEMON=${BTDAEMON:-$(command -v bluetoothd || ls /usr/lib/bluetooth/bluetoothd /usr/sbin/bluetoothd 2>/dev/null | head -1)}
if [ -n "$BTDAEMON" ] && [ -x "$BTDAEMON" ]; then
    mkdir -p /etc/systemd/system/bluetooth.service.d
    cat > /etc/systemd/system/bluetooth.service.d/sentryusb-experimental.conf << EOF
[Service]
ExecStart=
ExecStart=$BTDAEMON --experimental
EOF
    systemctl daemon-reload
    systemctl restart bluetooth 2>/dev/null || true
    sleep 2
    ok "bluetoothd experimental mode enabled ($BTDAEMON)"
else
    warn "Could not find bluetoothd binary, skipping --experimental override"
fi

# Download and install systemd service file
curl -fsSL "https://raw.githubusercontent.com/$REPO/$BRANCH/server/ble/sentryusb-ble.service" -o /etc/systemd/system/sentryusb-ble.service 2>/dev/null || \
    warn "Failed to download sentryusb-ble.service"

if [ -f /etc/systemd/system/sentryusb-ble.service ]; then
    systemctl daemon-reload
    systemctl enable sentryusb-ble 2>/dev/null || true
    systemctl restart sentryusb-ble 2>/dev/null || true
    ok "BLE peripheral daemon installed"
else
    warn "BLE peripheral daemon installation skipped (service file not found)"
fi

# ── Step 4: Remove stale cached setup scripts ──────────────────────
# Force fresh download on next setup run so latest fixes are used
rm -f /root/bin/setup-sentryusb /root/bin/setup-teslausb /root/bin/envsetup.sh

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
    echo -e "  The device will reboot several times during setup — this is normal."
    echo -e "  The full process takes 10-20 minutes. Do NOT power off the device."
    echo -e "  Or edit /root/sentryusb.conf, touch /sentryusb/SENTRYUSB_SETUP_STARTED, and run /etc/rc.local"
    echo ""
else
    error_exit "Service failed to start. Check: journalctl -u $SERVICE_NAME -f"
fi
