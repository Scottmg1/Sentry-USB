#!/bin/bash -e

# ── SentryUSB Image Setup ──
# This runs inside pi-gen's chroot during image build.
# Goal: produce an image where the user flashes, boots, and gets a web UI.

touch "${ROOTFS_DIR}/boot/ssh"

# Remove firstrun.sh so Raspberry Pi Imager can inject WiFi and other
# customization. pi-gen writes this file (and a cmdline.txt boot hook) when
# FIRST_USER_PASS is set, which causes Imager to treat the image as already
# customized and lock its settings panel. The sentryusb user is created at
# build time, resize2fs_once is disabled below, and WiFi is handled by
# rc.local via sentryusb.conf — so firstrun.sh is not needed.
rm -f "${ROOTFS_DIR}/boot/firmware/firstrun.sh"
rm -f "${ROOTFS_DIR}/boot/firmware/userconf.txt"
if [ -f "${ROOTFS_DIR}/boot/firmware/cmdline.txt" ]; then
    sed -i \
        -e 's| systemd\.run=/boot/firmware/firstrun\.sh||g' \
        -e 's| systemd\.run=/boot/firstrun\.sh||g' \
        -e 's| systemd\.run_success_action=reboot||g' \
        -e 's| systemd\.unit=kernel-command-line\.target||g' \
        -e 's| init=/usr/lib/raspberrypi-sys-mods/firstboot||g' \
        "${ROOTFS_DIR}/boot/firmware/cmdline.txt"
fi

install -m 755 files/rc.local                             "${ROOTFS_DIR}/etc/"
install -m 666 files/sentryusb.conf.sample                "${ROOTFS_DIR}/boot/firmware/sentryusb.conf"
install -m 666 files/wpa_supplicant.conf.sample           "${ROOTFS_DIR}/boot/firmware"
install -m 666 files/run_once                             "${ROOTFS_DIR}/boot/firmware"
install -d "${ROOTFS_DIR}/root/bin"
install -d "${ROOTFS_DIR}/opt/sentryusb"

# Create /sentryusb symlink → /boot/firmware
ln -sf /boot/firmware "${ROOTFS_DIR}/sentryusb"

# ensure dwc2 module is loaded for USB gadget
echo "dtoverlay=dwc2" >> "${ROOTFS_DIR}/boot/firmware/config.txt"

# ── Pre-install SentryUSB binary ──
# Detect target architecture from the pi-gen build context
REPO="Scottmg1/Sentry-USB"
case "$(dpkg --print-architecture 2>/dev/null || echo arm64)" in
    arm64|aarch64) BINARY_SUFFIX="linux-arm64" ;;
    armhf|armv7l)  BINARY_SUFFIX="linux-armv7" ;;
    *)             BINARY_SUFFIX="linux-arm64" ;;
esac
BINARY_URL="https://github.com/${REPO}/releases/latest/download/sentryusb-${BINARY_SUFFIX}"

if [ -n "${SENTRYUSB_BINARY:-}" ] && [ -f "${SENTRYUSB_BINARY}" ]; then
    # Allow local binary override for CI builds
    cp "${SENTRYUSB_BINARY}" "${ROOTFS_DIR}/opt/sentryusb/sentryusb"
elif [ -f "files/sentryusb-binary" ]; then
    # Injected by build-image.sh or CI
    cp "files/sentryusb-binary" "${ROOTFS_DIR}/opt/sentryusb/sentryusb"
else
    curl -fsSL "${BINARY_URL}" -o "${ROOTFS_DIR}/opt/sentryusb/sentryusb" || {
        echo "WARNING: Could not download binary from releases. Image will need manual install."
    }
fi
chmod +x "${ROOTFS_DIR}/opt/sentryusb/sentryusb"

# Write version file
RELEASE_TAG=$(curl -fsSL --max-time 10 "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
    | grep '"tag_name"' | head -1 \
    | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/' || true)
if [ -n "${RELEASE_TAG:-}" ]; then
    echo "$RELEASE_TAG" > "${ROOTFS_DIR}/opt/sentryusb/version"
    echo "Version: $RELEASE_TAG"
fi

# ── Install systemd service for the web UI ──
cat > "${ROOTFS_DIR}/lib/systemd/system/sentryusb.service" << 'SERVICEEOF'
[Unit]
Description=SentryUSB Web Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/sentryusb/sentryusb -port 80
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
SERVICEEOF

# ── Install prerequisite packages and clean up ──
on_chroot << EOF
# Enable the web server service
systemctl enable sentryusb.service

# Install prerequisites needed by setup scripts
apt-get update -qq
apt-get install -y dos2unix parted fdisk sudo curl

# Remove unwanted packages, disable unwanted services, and disable swap
apt-get remove -y --purge triggerhappy userconf-pi dphys-swapfile firmware-libertas firmware-realtek firmware-atheros mkvtoolnix 2>/dev/null || true
apt-get -y autoremove
systemctl disable keyboard-setup || true
systemctl disable resize2fs_once || true
systemctl disable dpkg-db-backup || true
update-rc.d resize2fs_once remove || true
rm -f /etc/init.d/resize2fs_once
rm -f /usr/share/initramfs-tools/scripts/local-premount/firstboot
update-initramfs -u || true

# Clean apt cache to reduce image size
apt-get clean
rm -rf /var/lib/apt/lists/*
EOF
