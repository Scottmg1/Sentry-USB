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

# Process a single clips directory: trigger API, wait for completion
function process_clips_dir() {
  local clips_dir="$1"
  log "Starting drive processing on $clips_dir"

  RESPONSE=$(curl -sf -X POST "${API_URL}/api/drives/process" \
    -H "Content-Type: application/json" \
    -d "{\"clips_dir\": \"${clips_dir}\", \"throttle_ms\": 20}" 2>&1)

  if [ $? -ne 0 ]; then
    log "Failed to trigger processing for $clips_dir: $RESPONSE"
    return 1
  fi

  log "Processing triggered: $RESPONSE"

  local timeout=1800
  local elapsed=0
  local poll_interval=10
  local last_progress_log=0

  while [ $elapsed -lt $timeout ]; do
    sleep $poll_interval
    elapsed=$((elapsed + poll_interval))

    STATUS=$(curl -sf "${API_URL}/api/drives/status" 2>/dev/null)
    if [ $? -ne 0 ]; then
      log "Failed to check status, continuing to wait..."
      continue
    fi

    RUNNING=$(echo "$STATUS" | grep -o '"running":true' || true)
    if [ -z "$RUNNING" ]; then
      ROUTES=$(echo "$STATUS" | grep -o '"routes_count":[0-9]*' | cut -d: -f2)
      PROCESSED=$(echo "$STATUS" | grep -o '"processed_count":[0-9]*' | cut -d: -f2)
      log "Processing complete for $clips_dir. Routes: ${ROUTES:-0}, Files processed: ${PROCESSED:-0}"
      return 0
    fi

    # Log progress every 60 seconds
    if [ $((elapsed - last_progress_log)) -ge 60 ]; then
      PROCESSED=$(echo "$STATUS" | grep -o '"processed_count":[0-9]*' | cut -d: -f2)
      ROUTES=$(echo "$STATUS" | grep -o '"routes_count":[0-9]*' | cut -d: -f2)
      log "Still processing $clips_dir... (${elapsed}s elapsed, ${PROCESSED:-?} files processed, ${ROUTES:-?} routes)"
      last_progress_log=$elapsed
    fi
  done

  log "Processing timed out for $clips_dir after ${timeout}s"
  return 1
}

# Process RecentClips from local snapshot storage (not NFS archive, not live cam)
CLIPS_DIR="/mutable/TeslaCam/RecentClips"
if [ ! -d "$CLIPS_DIR" ]; then
  log "RecentClips directory not found at $CLIPS_DIR, skipping"
  exit 0
fi

process_clips_dir "$CLIPS_DIR"
PROCESSED=$?

log "Drive processing complete. $PROCESSED directories processed."

# Send notification if configured
if [ -x /root/bin/send-push-message ]; then
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
