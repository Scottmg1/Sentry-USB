#!/bin/bash
# Fix networking on SentryUSB installs that used the old read-only setup,
# where /var/lib/NetworkManager and related dirs were symlinked to /mutable.
# That caused WiFi/AP to fail after reboot when the USB drive wasn't ready.
# Run as root after: /root/bin/remountfs_rw
# Then run this script (e.g. via setup-sentryusb fix_networking). Reboot after.

set -e

function log_progress () {
  if declare -F setup_progress &> /dev/null; then
    setup_progress "fix-readonly-networking: $1"
  else
    echo "fix-readonly-networking: $1"
  fi
}

if [ "$(id -u)" -ne 0 ]; then
  echo "Run as root (e.g. sudo -i)"
  exit 1
fi

# ---- Check if the old broken state is present; skip if already fixed ----
_needs_fix=false
[ -L /var/lib/NetworkManager ] && _needs_fix=true
[ -L /etc/NetworkManager/system-connections ] && _needs_fix=true
[ -L /var/lib/dhcp ] || [ -L /var/lib/dhcpcd ] && _needs_fix=true
readlink -f /etc/resolv.conf 2>/dev/null | grep -q /mutable && _needs_fix=true
readlink -f /etc/resolv.conf 2>/dev/null | grep -q /run/systemd/resolve && _needs_fix=true
systemctl is-active --quiet systemd-resolved 2>/dev/null && _needs_fix=true
grep -w -q "/var/lib/NetworkManager" /etc/fstab || _needs_fix=true
grep -q "LABEL=mutable" /etc/fstab && ! grep "LABEL=mutable" /etc/fstab | grep -q "nofail" && _needs_fix=true
grep -q "LABEL=backingfiles" /etc/fstab && ! grep "LABEL=backingfiles" /etc/fstab | grep -q "nofail" && _needs_fix=true
[ ! -e /etc/tmpfiles.d/resolv-fallback.conf ] && _needs_fix=true

if [ "$_needs_fix" = false ]; then
  log_progress "No fix needed: networking is already using tmpfs / root (not symlinks to /mutable)."
  exit 0
fi

log_progress "Applying networking fix for read-only root..."

# Ensure /mutable is mounted so we can copy from it if needed
if ! findmnt --mountpoint /mutable &> /dev/null; then
  if grep -q 'LABEL=mutable' /etc/fstab; then
    mount /mutable || log_progress "Warning: could not mount /mutable, will create empty dirs where needed"
  fi
fi

# ---- /var/lib/NetworkManager: must be a real dir so tmpfs can mount over it ----
if [ -L /var/lib/NetworkManager ]; then
  log_progress "Replacing /var/lib/NetworkManager symlink with directory"
  rm /var/lib/NetworkManager
  mkdir -p /var/lib/NetworkManager
fi

# ---- NM connection profiles: restore to root so they exist before /mutable mounts ----
if [ -L /etc/NetworkManager/system-connections ]; then
  log_progress "Restoring NetworkManager connection profiles to root FS"
  rm /etc/NetworkManager/system-connections
  if [ -d /mutable/etc/NetworkManager/system-connections ]; then
    cp -a /mutable/etc/NetworkManager/system-connections /etc/NetworkManager/
  else
    mkdir -p /etc/NetworkManager/system-connections
  fi
fi

# ---- DHCP lease dirs: real dirs for tmpfs ----
for d in /var/lib/dhcp /var/lib/dhcpcd; do
  if [ -L "$d" ]; then
    log_progress "Replacing $d symlink with directory"
    rm "$d"
    mkdir -p "$d"
  fi
done

# ---- resolv.conf: point to /tmp (always writable) ----
# Also redirect away from systemd-resolved's stub (/run/systemd/resolve/...)
# because we configure NM with dns=none below and use a dispatcher script
# to populate resolv.conf; systemd-resolved would conflict with that.
_resolv_target=$(readlink -f /etc/resolv.conf 2>/dev/null || true)
if [ "$_resolv_target" != "/tmp/resolv.conf" ]; then
  log_progress "Redirecting resolv.conf to /tmp (was: ${_resolv_target:-empty})"
  > /tmp/resolv.conf
  if command -v nmcli &>/dev/null; then
    nmcli --terse --fields IP4.DNS dev show 2>/dev/null \
      | sed -n 's/^IP4\.DNS\[.*\]:/nameserver /p' \
      | head -3 >> /tmp/resolv.conf || true
  fi
  rm -f /etc/resolv.conf 2>/dev/null || true
  ln -sf /tmp/resolv.conf /etc/resolv.conf
fi

# ---- tmpfiles.d: seed /tmp/resolv.conf on every boot ----
# /tmp is a tmpfs that is empty after reboot, so without this rule the
# resolv.conf symlink dangles and DNS breaks until NM rewrites it.
# Note: no fallback nameserver is written here — NM will populate
# the file with DHCP-provided DNS (e.g. PiHole). Hardcoding 8.8.8.8 would
# bypass custom DNS setups on the user's network.
if [ ! -e /etc/tmpfiles.d/resolv-fallback.conf ]; then
  log_progress "Installing tmpfiles.d rule for resolv.conf"
  mkdir -p /etc/tmpfiles.d
  echo 'f /tmp/resolv.conf 0644 root root -' > /etc/tmpfiles.d/resolv-fallback.conf
fi

# ---- NetworkManager: dns=none + dispatcher to write resolv.conf ----
# NM's atomic-write for resolv.conf (temp file + rename in /etc/) always fails
# on a read-only root because /etc/ is not writable.  Use dns=none so NM does
# not attempt to write resolv.conf itself, and install a dispatcher script that
# writes DHCP-provided DNS servers directly to /tmp/resolv.conf instead.
log_progress "Configuring NetworkManager DNS handling (dns=none + dispatcher)"
mkdir -p /etc/NetworkManager/conf.d
cat > /etc/NetworkManager/conf.d/sentryusb-dns.conf << 'EOF'
[main]
dns=none
EOF

# ---- Install NM dispatcher to populate /tmp/resolv.conf ----
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

# ---- Disable systemd-resolved (conflicts with our resolv.conf management) ----
if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
  log_progress "Disabling systemd-resolved (dispatcher handles DNS directly)"
  systemctl stop systemd-resolved 2>/dev/null || true
  systemctl disable systemd-resolved 2>/dev/null || true
fi

# ---- Restart NM so dns=none takes effect and the dispatcher is loaded ----
if systemctl is-active --quiet NetworkManager 2>/dev/null; then
  log_progress "Restarting NetworkManager to apply DNS configuration"
  systemctl restart NetworkManager 2>/dev/null || true
fi

# ---- fstab: tmpfs entries for networking (idempotent) ----
for spec in \
  "/var/lib/NetworkManager:nodev,nosuid,mode=0700" \
  "/var/lib/dhcp:nodev,nosuid" \
  "/var/lib/dhcpcd:nodev,nosuid"
do
  _mountpoint="${spec%%:*}"
  _opts="${spec#*:}"
  if ! grep -w -q "$_mountpoint" /etc/fstab; then
    log_progress "Adding tmpfs fstab entry for $_mountpoint"
    mkdir -p "$_mountpoint"
    echo "tmpfs $_mountpoint tmpfs $_opts 0 0" >> /etc/fstab
  fi
done

# ---- fstab: add nofail to mutable and backingfiles so boot doesn't hang if USB is missing ----
for label in mutable backingfiles; do
  if grep -q "LABEL=$label" /etc/fstab && ! grep "LABEL=$label" /etc/fstab | grep -q "nofail"; then
    log_progress "Adding nofail to LABEL=$label in fstab"
    sed -i "/LABEL=$label/ s/auto,rw/auto,rw,nofail/" /etc/fstab
    sed -i "/LABEL=$label/ s/auto,rw,noatime/auto,rw,noatime,nofail/" /etc/fstab
  fi
done

touch -t 197001010000 /etc/fstab 2>/dev/null || true

log_progress "Done. Reboot for changes to take effect."
exit 0
