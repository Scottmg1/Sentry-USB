#!/bin/bash
# Post-archive hook: process newly archived dashcam clips for GPS/drive data.
# Called by archiveloop after archive_clips completes, before awake_stop.
# Only runs if DRIVE_MAP_ENABLED is set to true in the config.

source /root/bin/envsetup.sh 2>/dev/null || true

if [ "${DRIVE_MAP_ENABLED:-false}" != "true" ]; then
  exit 0
fi

LOG_FILE="${LOG_FILE:-/mutable/archiveloop.log}"

function log() {
  echo "$(date): [drive-map] $*" >> "$LOG_FILE"
}

# Find the SentryUSB API port
SENTRYUSB_PORT="${SENTRYUSB_PORT:-80}"
API_URL="http://127.0.0.1:${SENTRYUSB_PORT}"

# Check if sentryusb service is running
if ! curl -sf "${API_URL}/api/drives/status" > /dev/null 2>&1; then
  log "SentryUSB API not reachable, skipping drive processing"
  exit 0
fi

# Determine clips directory — process from the local cam drive before files are deleted
CLIPS_DIR=""
for dir in /mnt/cam/TeslaCam/RecentClips /mnt/cam/TeslaCam/SavedClips /mnt/cam/TeslaCam/SentryClips; do
  if [ -d "$dir" ]; then
    CLIPS_DIR="$dir"
    break
  fi
done

if [ -z "$CLIPS_DIR" ]; then
  log "No clips directory found, skipping"
  exit 0
fi

log "Starting drive processing on $CLIPS_DIR"

# Trigger processing via the API (runs in background on the server)
RESPONSE=$(curl -sf -X POST "${API_URL}/api/drives/process" \
  -H "Content-Type: application/json" \
  -d "{\"clips_dir\": \"${CLIPS_DIR}\", \"throttle_ms\": 20}" 2>&1)

if [ $? -ne 0 ]; then
  log "Failed to trigger processing: $RESPONSE"
  exit 1
fi

log "Processing triggered: $RESPONSE"

# Wait for processing to complete (timeout after 30 minutes)
TIMEOUT=1800
ELAPSED=0
POLL_INTERVAL=10

while [ $ELAPSED -lt $TIMEOUT ]; do
  sleep $POLL_INTERVAL
  ELAPSED=$((ELAPSED + POLL_INTERVAL))

  STATUS=$(curl -sf "${API_URL}/api/drives/status" 2>/dev/null)
  if [ $? -ne 0 ]; then
    log "Failed to check status, continuing to wait..."
    continue
  fi

  RUNNING=$(echo "$STATUS" | grep -o '"running":true' || true)
  if [ -z "$RUNNING" ]; then
    # Processing complete
    ROUTES=$(echo "$STATUS" | grep -o '"routes_count":[0-9]*' | cut -d: -f2)
    log "Processing complete. Total routes: ${ROUTES:-unknown}"

    # Send notification if configured
    if [ -x /root/bin/send-push-message ]; then
      # Get drive stats for the notification
      STATS=$(curl -sf "${API_URL}/api/drives/stats" 2>/dev/null)
      if [ $? -eq 0 ]; then
        DRIVE_COUNT=$(echo "$STATS" | grep -o '"drives_count":[0-9]*' | cut -d: -f2)
        TOTAL_MI=$(echo "$STATS" | grep -o '"total_distance_mi":[0-9.]*' | cut -d: -f2)
        /root/bin/send-push-message "${NOTIFICATION_TITLE:-SentryUSB}:" \
          "Drive processing complete. ${DRIVE_COUNT:-0} total drives, ${TOTAL_MI:-0} miles mapped." \
          info || log "Failed to send notification"
      fi
    fi

    exit 0
  fi
done

log "Processing timed out after ${TIMEOUT}s"
exit 1
