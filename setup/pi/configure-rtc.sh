#!/bin/bash

function log_progress () {
  if declare -F setup_progress > /dev/null
  then
    setup_progress "configure-rtc: $1"
    return
  fi
  echo "configure-rtc: $1"
}

# Read system time → write to RTC via sysfs
rtc_sync_systohc() {
  if [ ! -e /dev/rtc0 ]; then
    log_progress "Warning: /dev/rtc0 not found"
    return 1
  fi
  local utc_time
  utc_time=$(date -u +%s)
  echo "$utc_time" > /sys/class/rtc/rtc0/since_epoch
  log_progress "Synced system time to RTC"
}

# Read RTC → set system time
rtc_sync_hctosys() {
  if [ ! -e /sys/class/rtc/rtc0/since_epoch ]; then
    return 1
  fi
  local epoch
  epoch=$(cat /sys/class/rtc/rtc0/since_epoch 2>/dev/null)
  if [ -n "$epoch" ] && [ "$epoch" -gt 1704067200 ]; then
    # Only set if RTC has a sane date (after 2024-01-01)
    date -u -s "@$epoch" > /dev/null
  fi
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

  # Create RTC sync service using sysfs (hwclock is not available on minimal images)
  log_progress "Creating sentryusb-hwclock.service"
  cat > /lib/systemd/system/sentryusb-hwclock.service << 'UNIT'
[Unit]
Description=SentryUSB hardware clock sync
DefaultDependencies=no
After=dev-rtc0.device
Before=time-sync.target sysinit.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/bash -c 'epoch=$(cat /sys/class/rtc/rtc0/since_epoch 2>/dev/null); [ -n "$epoch" ] && [ "$epoch" -gt 1704067200 ] && date -u -s "@$epoch" > /dev/null || true'
ExecStop=/bin/bash -c 'date -u +%%s > /sys/class/rtc/rtc0/since_epoch'

[Install]
WantedBy=sysinit.target
UNIT

  systemctl daemon-reload
  systemctl enable sentryusb-hwclock.service

  # Sync current system time to RTC
  rtc_sync_systohc || log_progress "Warning: failed to sync time to RTC"

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
