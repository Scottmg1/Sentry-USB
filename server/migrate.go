package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

const (
	versionFile  = "/opt/sentryusb/version"
	migrateDir   = "/opt/sentryusb"
	migrateRepo  = "Scottmg1/Sentry-USB"
	migrateBranch = "main-dev"
)

// runStartupMigration checks whether peripheral files (shell scripts, BLE
// daemon, Avahi service, etc.) need updating and does so automatically.
//
// This solves the bootstrap problem for old beta testers whose Go binary was
// updated via an older runUpdate that only replaced the binary — their scripts,
// BLE daemon, and service files were left stale.  Once they get a binary that
// contains this function, future boots will self-heal automatically.
//
// The migration is gated by a marker file so it only runs once per version.
// It is safe to run repeatedly and never touches setup-wizard configuration.
func runStartupMigration() {
	// Skip in dev mode (no version file)
	currentVersion := ""
	if data, err := os.ReadFile(versionFile); err == nil {
		currentVersion = strings.TrimSpace(string(data))
	}
	if currentVersion == "" || currentVersion == "dev" {
		return
	}

	markerFile := fmt.Sprintf("%s/.migrated-%s", migrateDir, currentVersion)
	if _, err := os.Stat(markerFile); err == nil {
		// Already migrated for this version
		return
	}

	log.Printf("[migrate] Running startup migration for %s...", currentVersion)

	// Determine which branch/tag to pull scripts from
	scriptRef := currentVersion
	if scriptRef == "unknown" || scriptRef == "" {
		scriptRef = migrateBranch
	}

	tarballURL := fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", migrateRepo, scriptRef)

	// The migration script:
	// 1. Downloads the repo tarball for the current version tag
	// 2. Updates run/ scripts, archive module scripts, setup-sentryusb, envsetup.sh
	// 3. Updates BLE daemon (sentryusb-ble.py) and its systemd service
	// 4. Installs BLE Python dependencies if missing (python3-dbus, python3-gi, bluez)
	// 5. Ensures bluetoothd --experimental override is in place
	// 6. Installs/updates Avahi mDNS service
	// 7. Restarts BLE daemon
	//
	// This NEVER runs the setup wizard or changes user configuration.
	migrationScript := fmt.Sprintf(`set -e

# Remount filesystem as read-write (no-op if already rw)
/root/bin/remountfs_rw 2>/dev/null || mount -o remount,rw / 2>/dev/null || true

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# Download repo tarball — try version tag first, fall back to main-dev
if ! curl -fsSL "%s" | tar xz --strip-components=1 -C "$TMPDIR" 2>/dev/null; then
  FALLBACK="https://github.com/%s/archive/%s.tar.gz"
  curl -fsSL "$FALLBACK" | tar xz --strip-components=1 -C "$TMPDIR" 2>/dev/null || exit 1
fi

# ── Update run/ scripts ──
for f in "$TMPDIR"/run/*; do
  [ -f "$f" ] || continue
  name=$(basename "$f")
  cp "$f" "/root/bin/$name"
  chmod +x "/root/bin/$name"
done

# ── Update archive module scripts ──
ARCHIVE_SYSTEM=""
for conf in /root/sentryusb.conf /sentryusb/sentryusb.conf; do
  if [ -f "$conf" ]; then
    ARCHIVE_SYSTEM=$(grep -m1 'ARCHIVE_SYSTEM=' "$conf" 2>/dev/null | tail -1 | sed "s/.*ARCHIVE_SYSTEM=//;s/['\"]//g;s/#.*//" | tr -d ' ') || true
    [ -n "$ARCHIVE_SYSTEM" ] && break
  fi
done
if [ -n "$ARCHIVE_SYSTEM" ]; then
  subdir="${ARCHIVE_SYSTEM}_archive"
  if [ -d "$TMPDIR/run/$subdir" ]; then
    for f in "$TMPDIR/run/$subdir"/*; do
      [ -f "$f" ] || continue
      name=$(basename "$f")
      cp "$f" "/root/bin/$name"
      chmod +x "/root/bin/$name"
    done
  fi
fi

# ── Update setup-sentryusb ──
if [ -f "$TMPDIR/setup/pi/setup-sentryusb" ]; then
  cp "$TMPDIR/setup/pi/setup-sentryusb" "/root/bin/setup-sentryusb"
  chmod +x "/root/bin/setup-sentryusb"
fi

# ── Update envsetup.sh ──
if [ -f "$TMPDIR/setup/pi/envsetup.sh" ]; then
  cp "$TMPDIR/setup/pi/envsetup.sh" "/root/bin/envsetup.sh"
  chmod +x "/root/bin/envsetup.sh"
fi

# ── Update BLE peripheral daemon ──
if [ -f "$TMPDIR/server/ble/sentryusb-ble.py" ]; then
  cp "$TMPDIR/server/ble/sentryusb-ble.py" "/root/bin/sentryusb-ble.py"
  chmod +x "/root/bin/sentryusb-ble.py"
fi
if [ -f "$TMPDIR/server/ble/sentryusb-ble.service" ]; then
  cp "$TMPDIR/server/ble/sentryusb-ble.service" "/etc/systemd/system/sentryusb-ble.service"
  systemctl daemon-reload
fi

# ── Install BLE Python dependencies if missing ──
for pkg in python3-dbus python3-gi bluez; do
  if ! dpkg-query -W --showformat='${db:Status-Status}\n' "$pkg" 2>/dev/null | grep -q '^installed$'; then
    DEBIAN_FRONTEND=noninteractive apt-get -y --force-yes install "$pkg" 2>/dev/null || true
  fi
done

# ── Ensure bluetoothd --experimental override ──
if [ ! -f /etc/systemd/system/bluetooth.service.d/sentryusb-experimental.conf ]; then
  BTDAEMON=$(systemctl cat bluetooth.service 2>/dev/null | grep '^ExecStart=' | head -1 | sed 's/ExecStart=//' | awk '{print $1}')
  BTDAEMON=${BTDAEMON:-$(command -v bluetoothd 2>/dev/null)}
  if [ -n "$BTDAEMON" ] && [ -x "$BTDAEMON" ]; then
    mkdir -p /etc/systemd/system/bluetooth.service.d
    cat > /etc/systemd/system/bluetooth.service.d/sentryusb-experimental.conf << BTEOF
[Service]
ExecStart=
ExecStart=$BTDAEMON --experimental
BTEOF
    systemctl daemon-reload
    systemctl restart bluetooth 2>/dev/null || true
    sleep 2
  fi
fi

# ── Install/update Avahi mDNS service ──
if [ -f "$TMPDIR/setup/pi/avahi-sentryusb.service" ]; then
  if ! dpkg -s avahi-daemon >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -y -qq avahi-daemon avahi-utils >/dev/null 2>&1 || true
  fi
  if dpkg -s avahi-daemon >/dev/null 2>&1; then
    mkdir -p /etc/avahi/services
    cp "$TMPDIR/setup/pi/avahi-sentryusb.service" /etc/avahi/services/sentryusb.service
    systemctl enable avahi-daemon 2>/dev/null || true
    systemctl restart avahi-daemon 2>/dev/null || true
  fi
fi

# ── Migrate AP to Away Mode (AP off by default) ──
# Existing installs have SENTRYUSB_AP with autoconnect=true and a dispatcher
# that always recreates ap0.  Away Mode now controls when the AP is on.
if nmcli -t con show SENTRYUSB_AP &>/dev/null; then
  nmcli con modify SENTRYUSB_AP connection.autoconnect no 2>/dev/null || true
  # Tear down the currently running AP
  nmcli con down SENTRYUSB_AP 2>/dev/null || true
  iw dev ap0 del 2>/dev/null || true
fi
# Update the dispatcher script to only recreate ap0 when Away Mode is active
WLAN=$(nmcli -t -f TYPE,DEVICE c show --active 2>/dev/null | grep 802-11-wireless | grep -v ':ap0$' | cut -d: -f2 | head -1)
WLAN=${WLAN:-wlan0}
if [ -d /etc/NetworkManager/dispatcher.d ]; then
  cat > /etc/NetworkManager/dispatcher.d/10-sentryusb-ap << APEOF
#!/bin/bash
# Recreate ap0 only if Away Mode is active (flag file exists).
IFACE="\$1"
ACTION="\$2"
if [ "\$IFACE" = "$WLAN" ] && [ "\$ACTION" = "up" ]; then
  if [ -f /mutable/sentryusb_away_mode.json ]; then
    if ! iw dev ap0 info &> /dev/null; then
      iw dev $WLAN interface add ap0 type __ap || true
    fi
    iw $WLAN set power_save off 2>/dev/null || true
    iw ap0 set power_save off 2>/dev/null || true
    nmcli con up SENTRYUSB_AP 2>/dev/null || true
  fi
fi
APEOF
  chmod 755 /etc/NetworkManager/dispatcher.d/10-sentryusb-ap
fi

# ── Restart BLE daemon ──
systemctl enable sentryusb-ble 2>/dev/null || true
systemctl restart sentryusb-ble 2>/dev/null || true
`, tarballURL, migrateRepo, migrateBranch)

	out, err := shell.RunWithTimeout(180*time.Second, "bash", "-c", migrationScript)
	if err != nil {
		log.Printf("[migrate] Warning: startup migration failed: %v\n%s", err, out)
		// Don't write marker — retry on next boot
		return
	}

	// Write marker so we don't run again for this version
	os.MkdirAll(migrateDir, 0755)
	os.WriteFile(markerFile, []byte("migrated\n"), 0644)
	log.Printf("[migrate] Startup migration complete for %s", currentVersion)
}
