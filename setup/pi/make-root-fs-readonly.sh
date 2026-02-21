#!/bin/bash

# Adapted from https://github.com/adafruit/Raspberry-Pi-Installer-Scripts/blob/master/read-only-fs.sh

function log_progress () {
  if declare -F setup_progress > /dev/null
  then
    setup_progress "make-root-fs-readonly: $1"
    return
  fi
  echo "make-root-fs-readonly: $1"
}

if [ "${SKIP_READONLY:-false}" = "true" ]
then
  log_progress "Skipping"
  exit 0
fi

log_progress "start"

function append_cmdline_txt_param() {
  local toAppend="$1"
  # Don't add the option if it is already added.
  # If the command line gets too long the pi won't boot.
  # Look for the option at the end ($) or in the middle
  # of the command line and surrounded by space (\s).
  if [ -f "$CMDLINE_PATH" ] && ! grep -P -q "\s${toAppend}(\$|\s)" "$CMDLINE_PATH"
  then
    sed -i "s/\'/ ${toAppend}/g" "$CMDLINE_PATH" >/dev/null
  fi
}

function remove_cmdline_txt_param() {
  if [ -f "$CMDLINE_PATH" ]
  then
    sed -i "s/\(\s\)${1}\(\s\|$\)//" "$CMDLINE_PATH" > /dev/null
  fi
}

log_progress "Disabling unnecessary service..."
systemctl disable apt-daily.timer
systemctl disable apt-daily-upgrade.timer

# adb service exists on some distributions and interferes with mass storage emulation
systemctl disable amlogic-adbd &> /dev/null || true
systemctl disable radxa-adbd radxa-usbnet &> /dev/null || true

# don't restore the led state from the time the root fs was made read-only
systemctl disable armbian-led-state &> /dev/null || true

log_progress "Removing unwanted packages..."
# Protect NetworkManager and WiFi-related packages from autoremove.
# On non-Raspbian distros (e.g. DietPi), these may be auto-installed
# dependencies that autoremove would purge, killing WiFi.
for pkg in network-manager wpasupplicant wpa-supplicant ifupdown dhcpcd dhcpcd5 isc-dhcp-client firmware-brcm80211 firmware-realtek firmware-atheros firmware-iwlwifi firmware-misc-nonfree
do
  if dpkg -s "$pkg" &> /dev/null
  then
    apt-mark manual "$pkg" 2>/dev/null || true
  fi
done
apt-get remove -y --force-yes --purge triggerhappy logrotate dphys-swapfile
apt-get -y --force-yes autoremove --purge
# Replace log management with busybox (use logread if needed)
log_progress "Installing ntp and busybox-syslogd..."
apt-get -y --force-yes install ntp busybox-syslogd; dpkg --purge rsyslog

log_progress "Configuring system..."

# Add fsck.mode=auto, noswap and/or ro to end of cmdline.txt
# Remove the fastboot parameter because it makes fsck not run
remove_cmdline_txt_param fastboot
append_cmdline_txt_param fsck.mode=auto
append_cmdline_txt_param noswap
append_cmdline_txt_param ro

# set root and mutable max mount count to 1, so they're checked every boot
tune2fs -c 1 "$ROOT_PARTITION_DEVICE" || log_progress "tune2fs failed for rootfs"
tune2fs -c 1 /dev/disk/by-label/mutable || log_progress "tune2fs failed for mutable"

# we're not using swap, so delete the swap file for some extra space
rm -f /var/swap

# Move fake-hwclock.data to /mutable directory so it can be updated
if ! findmnt --mountpoint /mutable > /dev/null
then
  log_progress "Mounting the mutable partition..."
  mount /mutable
  log_progress "Mounted."
fi
if [ ! -e "/mutable/etc" ]
then
  mkdir -p /mutable/etc
fi

if [ ! -L "/etc/fake-hwclock.data" ] && [ -e "/etc/fake-hwclock.data" ]
then
  log_progress "Moving fake-hwclock data"
  mv /etc/fake-hwclock.data /mutable/etc/fake-hwclock.data
  ln -s /mutable/etc/fake-hwclock.data /etc/fake-hwclock.data
fi
# By default fake-hwclock is run during early boot, before /mutable
# has been mounted and so will fail. Delay running it until /mutable
# has been mounted.
if [ -e /lib/systemd/system/fake-hwclock.service ]
then
  sed -i 's/Before=.*/After=mutable.mount/' /lib/systemd/system/fake-hwclock.service
fi

# ---- NetworkManager runtime state (/var/lib/NetworkManager) ----
# Use a tmpfs mount instead of symlinking to /mutable.  NM's built-in
# dnsmasq writes lease files here (e.g. dnsmasq-ap0.leases).  If the
# directory isn't writable the AP connection enters an enable/disable
# loop that thrashes the WiFi hardware and kills all wireless
# connectivity.  A tmpfs is always writable and doesn't depend on the
# USB drive being mounted in time.
if [ -d /var/lib/NetworkManager/ ] && [ ! -L /var/lib/NetworkManager ]
then
  log_progress "Backing up /var/lib/NetworkManager to mutable"
  mkdir -p /mutable/var/lib/
  cp -a /var/lib/NetworkManager /mutable/var/lib/ 2>/dev/null || true
fi
# Undo symlink left by a previous setup so the tmpfs mount works
if [ -L /var/lib/NetworkManager ]
then
  log_progress "Replacing /var/lib/NetworkManager symlink with directory for tmpfs"
  rm /var/lib/NetworkManager
  mkdir -p /var/lib/NetworkManager
fi

# ---- NetworkManager connection profiles ----
# Keep profiles on the root FS so they are always available at boot,
# even if /mutable (which may live on a USB drive) hasn't mounted yet.
# Save a backup copy to /mutable for reference / future restores.
if [ -d /etc/NetworkManager/system-connections ] && [ ! -L /etc/NetworkManager/system-connections ]
then
  log_progress "Backing up NetworkManager connection profiles to mutable"
  mkdir -p /mutable/etc/NetworkManager
  cp -a /etc/NetworkManager/system-connections /mutable/etc/NetworkManager/
fi
# Undo symlink left by a previous setup — restore the real directory
# from the mutable backup so NM finds the profiles on root at boot.
if [ -L /etc/NetworkManager/system-connections ]
then
  log_progress "Restoring NetworkManager connection profiles to root FS"
  rm /etc/NetworkManager/system-connections
  if [ -d /mutable/etc/NetworkManager/system-connections ]
  then
    cp -a /mutable/etc/NetworkManager/system-connections /etc/NetworkManager/
  else
    mkdir -p /etc/NetworkManager/system-connections
  fi
fi

# ---- DHCP lease directories ----
# Use tmpfs mounts.  Leases are ephemeral and re-requested at boot.
# Symlinking to /mutable caused failures when the USB drive wasn't
# mounted in time (DHCP clients can't write leases → no IP address).
if [ -L /var/lib/dhcp ]
then
  log_progress "Replacing /var/lib/dhcp symlink with directory for tmpfs"
  rm /var/lib/dhcp
  mkdir -p /var/lib/dhcp
fi
if [ -L /var/lib/dhcpcd ]
then
  log_progress "Replacing /var/lib/dhcpcd symlink with directory for tmpfs"
  rm /var/lib/dhcpcd
  mkdir -p /var/lib/dhcpcd
fi

# Create a configs directory for others to use
if [ ! -e "/mutable/configs" ]
then
  mkdir -p /mutable/configs
fi

# Move /var/spool to /tmp
if [ -L /var/spool ]
then
  log_progress "fixing /var/spool"
  rm /var/spool
  mkdir /var/spool
  chmod 755 /var/spool
  # a tmpfs fstab entry for /var/spool will be added below
else
  rm -rf /var/spool/*
fi

# Change spool permissions in var.conf (rondie/Margaret fix)
sed -i "s/spool\s*0755/spool 1777/g" /usr/lib/tmpfiles.d/var.conf >/dev/null

# Point resolv.conf at /tmp (a tmpfs that is always writable at boot).
# Previous versions symlinked to /mutable, but that broke DNS resolution
# if the USB drive was slow to mount.
# Also redirect away from systemd-resolved's stub (/run/systemd/resolve/...)
# because we configure NM with dns=none below and use a dispatcher script
# to populate resolv.conf; systemd-resolved would conflict with that.
# IMPORTANT: /tmp is wiped on every reboot (tmpfs), so we must also install
# a tmpfiles.d rule below to seed /tmp/resolv.conf on every boot.  Without
# that seed file the symlink dangles and DNS breaks.
_resolv_target=$(readlink -f /etc/resolv.conf 2>/dev/null || true)
if [ "$_resolv_target" != "/tmp/resolv.conf" ]
then
  log_progress "Redirecting resolv.conf to /tmp (was: ${_resolv_target:-empty})"
  # Seed /tmp/resolv.conf with current DHCP-provided DNS so name resolution
  # keeps working for the remainder of setup (e.g. apt-get upgrade).
  > /tmp/resolv.conf
  if command -v nmcli &>/dev/null; then
    nmcli --terse --fields IP4.DNS dev show 2>/dev/null \
      | sed -n 's/^IP4\.DNS\[.*\]:/nameserver /p' \
      | head -3 >> /tmp/resolv.conf || true
  fi
  rm -f /etc/resolv.conf 2>/dev/null || true
  ln -sf /tmp/resolv.conf /etc/resolv.conf
fi

# Ensure /tmp/resolv.conf exists on every boot so the symlink doesn't dangle.
# /tmp is a tmpfs that is empty after reboot, so /tmp/resolv.conf must be
# recreated each time.  systemd-tmpfiles-setup.service runs after tmpfs
# mounts but before NetworkManager, guaranteeing the file exists immediately.
# NM or dhcpcd will populate it with DHCP-provided DNS servers once connected.
# No fallback nameserver is hardcoded here to avoid overriding custom DNS
# setups (e.g. PiHole) on the user's network.
log_progress "Installing tmpfiles.d rule for resolv.conf"
mkdir -p /etc/tmpfiles.d
echo 'f /tmp/resolv.conf 0644 root root -' > /etc/tmpfiles.d/resolv-fallback.conf

# Tell NetworkManager NOT to manage /etc/resolv.conf itself.  On a read-only
# root filesystem NM's atomic-write (temp file + rename in /etc/) always fails
# because /etc/ is not writable.  Instead, a dispatcher script below populates
# /tmp/resolv.conf (the symlink target) whenever a DHCP lease is obtained.
log_progress "Configuring NetworkManager DNS handling (dns=none + dispatcher)"
mkdir -p /etc/NetworkManager/conf.d
cat > /etc/NetworkManager/conf.d/sentryusb-dns.conf << 'EOF'
[main]
dns=none
EOF

# Install an NM dispatcher script that writes DHCP-provided DNS servers to
# /tmp/resolv.conf.  This fires every time an interface comes up or renews
# its DHCP lease, so DNS stays current without NM touching /etc/ directly.
log_progress "Installing NetworkManager dispatcher for resolv.conf"
mkdir -p /etc/NetworkManager/dispatcher.d
cat > /etc/NetworkManager/dispatcher.d/50-write-resolv-conf << 'DISPATCHER'
#!/bin/bash
# Populate /tmp/resolv.conf with DHCP-provided DNS servers.
# Required because the root filesystem is read-only and NM cannot
# atomically write to /etc/resolv.conf (it needs a writable /etc/).
# /etc/resolv.conf is a symlink to /tmp/resolv.conf.
case "$2" in
  up|dhcp4-change)
    _servers="${DHCP4_DOMAIN_NAME_SERVERS:-${IP4_NAMESERVERS:-}}"
    if [ -n "$_servers" ]; then
      {
        for _ns in $_servers; do
          echo "nameserver $_ns"
        done
        _domain="${DHCP4_DOMAIN_NAME:-}"
        [ -n "$_domain" ] && echo "search $_domain"
      } > /tmp/resolv.conf
    fi
    ;;
esac
DISPATCHER
chmod 0755 /etc/NetworkManager/dispatcher.d/50-write-resolv-conf

# Disable systemd-resolved — it conflicts with our resolv.conf management
# and is unnecessary now that the dispatcher handles DNS directly.
if systemctl is-active --quiet systemd-resolved 2>/dev/null
then
  log_progress "Disabling systemd-resolved (dispatcher handles DNS directly)"
  systemctl stop systemd-resolved 2>/dev/null || true
  systemctl disable systemd-resolved 2>/dev/null || true
fi

# Restart NM so dns=none takes effect and the dispatcher is loaded.
if systemctl is-active --quiet NetworkManager 2>/dev/null
then
  log_progress "Restarting NetworkManager to apply DNS configuration"
  systemctl restart NetworkManager 2>/dev/null || true
fi

# Update /etc/fstab
# make /boot read-only
# make / read-only
# tmpfs /var/log tmpfs nodev,nosuid 0 0
# tmpfs /var/tmp tmpfs nodev,nosuid 0 0
# tmpfs /tmp     tmpfs nodev,nosuid 0 0
if ! grep -P -q "/boot\s+vfat\s+.+?(?=,ro)" /etc/fstab
then
  sed -i -r "s@(/boot\s+vfat\s+\S+)@\1,ro@" /etc/fstab
fi

if ! grep -P -q "/boot/firmware\s+vfat\s+.+?(?=,ro)" /etc/fstab
then
  sed -i -r "s@(/boot/firmware\s+vfat\s+\S+)@\1,ro@" /etc/fstab
fi

if ! grep -P -q "/\s+ext4\s+.+?(?=,ro)" /etc/fstab
then
  sed -i -r "s@(/\s+ext4\s+\S+)@\1,ro@" /etc/fstab
fi

if ! grep -w -q "/var/log" /etc/fstab
then
  echo "tmpfs /var/log tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi

if ! grep -w -q "/var/tmp" /etc/fstab
then
  echo "tmpfs /var/tmp tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi

if ! grep -w -q "/tmp" /etc/fstab
then
  echo "tmpfs /tmp    tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi

if ! grep -w -q "/var/spool" /etc/fstab
then
  echo "tmpfs /var/spool tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi

if ! grep -w -q "/var/lib/ntp" /etc/fstab
then
  if [ ! -d /var/lib/ntp ]
  then
    rm -rf /var/lib/ntp
    mkdir -p /var/lib/ntp
  fi
  echo "tmpfs /var/lib/ntp tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi

# Networking directories on tmpfs so they're always writable at boot,
# regardless of whether /mutable (potentially on USB) has mounted yet.
if ! grep -w -q "/var/lib/NetworkManager" /etc/fstab
then
  mkdir -p /var/lib/NetworkManager
  echo "tmpfs /var/lib/NetworkManager tmpfs nodev,nosuid,mode=0700 0 0" >> /etc/fstab
fi
if ! grep -w -q "/var/lib/dhcp" /etc/fstab
then
  mkdir -p /var/lib/dhcp
  echo "tmpfs /var/lib/dhcp tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi
if ! grep -w -q "/var/lib/dhcpcd" /etc/fstab
then
  mkdir -p /var/lib/dhcpcd
  echo "tmpfs /var/lib/dhcpcd tmpfs nodev,nosuid 0 0" >> /etc/fstab
fi

# work around 'mount' warning that's printed when /etc/fstab is
# newer than /run/systemd/systemd-units-load
touch -t 197001010000 /etc/fstab

# autofs by default has dependencies on various network services, because
# one of its purposes is to automount NFS filesystems.
# SentryUSB doesn't use NFS though, and removing those dependencies speeds
# up SentryUSB startup.
if [ ! -e /etc/systemd/system/autofs.service ]
then
  grep -v '^Wants=\|^After=' /lib/systemd/system/autofs.service  > /etc/systemd/system/autofs.service
fi

log_progress "done"
