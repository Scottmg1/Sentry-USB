#!/bin/bash

function log_progress () {
  if declare -F setup_progress > /dev/null
  then
    setup_progress "configure-rtc: $1"
    return
  fi
  echo "configure-rtc: $1"
}

RTC_BATTERY_ENABLED=${RTC_BATTERY_ENABLED:-false}

# Only relevant on Pi 5
if ! grep -qi "Raspberry Pi 5" /proc/device-tree/model 2>/dev/null; then
  log_progress "Not a Pi 5, skipping RTC configuration"
  exit 0
fi

if [ "$RTC_BATTERY_ENABLED" = "true" ]; then
  log_progress "Enabling RTC battery support"

  # Disable fake-hwclock
  if systemctl is-enabled fake-hwclock.service 2>/dev/null | grep -q enabled; then
    log_progress "Disabling fake-hwclock"
    systemctl stop fake-hwclock.service || true
    systemctl disable fake-hwclock.service || true
  fi

  # Create hwclock sync service
  log_progress "Creating sentryusb-hwclock.service"
  cat > /lib/systemd/system/sentryusb-hwclock.service << 'EOF'
[Unit]
Description=SentryUSB hardware clock sync
DefaultDependencies=no
After=dev-rtc0.device
Before=time-sync.target sysinit.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/sbin/hwclock --hctosys --utc
ExecStop=/sbin/hwclock --systohc --utc

[Install]
WantedBy=sysinit.target
EOF

  systemctl daemon-reload
  systemctl enable sentryusb-hwclock.service

  # Sync current system time to RTC
  if [ -e /dev/rtc0 ]; then
    log_progress "Syncing system time to RTC"
    hwclock --systohc --utc || log_progress "Warning: failed to sync time to RTC"
  else
    log_progress "Warning: /dev/rtc0 not found — RTC device not available"
  fi

  log_progress "RTC battery support enabled"
else
  log_progress "RTC battery support disabled, ensuring fake-hwclock is active"

  # Remove hwclock service if it exists
  if [ -e /lib/systemd/system/sentryusb-hwclock.service ]; then
    systemctl stop sentryusb-hwclock.service 2>/dev/null || true
    systemctl disable sentryusb-hwclock.service 2>/dev/null || true
    rm -f /lib/systemd/system/sentryusb-hwclock.service
    systemctl daemon-reload
  fi

  # Re-enable fake-hwclock
  if systemctl is-enabled fake-hwclock.service 2>/dev/null | grep -q disabled; then
    log_progress "Re-enabling fake-hwclock"
    systemctl enable fake-hwclock.service || true
  fi

  log_progress "fake-hwclock restored"
fi
