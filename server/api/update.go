package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

const telemetrySalt = "SENTRYUSB_2026_PROD"
var telemetryURL = APIBaseURL + "/sentryusb/telemetry"

var (
	cachedFingerprint string
	fingerprintOnce   sync.Once
)

// getSBCModel reads the hardware model string (e.g. "Raspberry Pi 4 Model B Rev 1.4")
func getSBCModel() string {
	for _, p := range []string{"/proc/device-tree/model", "/sys/firmware/devicetree/base/model"} {
		raw, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimRight(string(raw), "\x00\n ")
		}
	}
	return "unknown"
}

// getFingerprint generates a SHA-256 hash of a stable hardware identifier + salt.
// Uses the SBC's hardware serial number (survives reflash) with fallback to machine-id.
// Cached after first call.
func getFingerprint() string {
	fingerprintOnce.Do(func() {
		var id string

		// Prefer hardware serial — persists across SD card reflashes
		for _, p := range []string{
			"/sys/firmware/devicetree/base/serial-number",
			"/proc/device-tree/serial-number",
		} {
			raw, err := os.ReadFile(p)
			if err == nil {
				id = strings.TrimRight(string(raw), "\x00\n ")
				if id != "" {
					break
				}
			}
		}

		// Fallback to machine-id (changes on reflash, but better than nothing)
		if id == "" {
			for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
				raw, err := os.ReadFile(p)
				if err == nil {
					id = strings.TrimSpace(string(raw))
					if id != "" {
						break
					}
				}
			}
		}

		if id == "" {
			log.Printf("[telemetry] Cannot determine device identity (no serial or machine-id)")
			return
		}

		hash := sha256.Sum256([]byte(id + telemetrySalt))
		cachedFingerprint = hex.EncodeToString(hash[:])
	})
	return cachedFingerprint
}

// doSendTelemetry performs the actual telemetry POST (blocking).
func doSendTelemetry(currentVersion string, updateAvailable bool, newVersion string) {
	fp := getFingerprint()
	if fp == "" {
		log.Printf("[telemetry] Skipped: no fingerprint available")
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"fingerprint":      fp,
		"current_version":  currentVersion,
		"update_available": updateAvailable,
		"new_version":      newVersion,
		"arch":             runtime.GOARCH,
		"model":            getSBCModel(),
	})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(telemetryURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[telemetry] Failed to send: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("[telemetry] Sent (status %d)", resp.StatusCode)
}

// sendTelemetry fires and forgets a telemetry POST to the support server.
func sendTelemetry(currentVersion string, updateAvailable bool, newVersion string) {
	go doSendTelemetry(currentVersion, updateAvailable, newVersion)
}

// sendTelemetrySync sends telemetry and blocks until complete.
func sendTelemetrySync(currentVersion string, updateAvailable bool, newVersion string) {
	doSendTelemetry(currentVersion, updateAvailable, newVersion)
}

const updateRepo = "Scottmg1/Sentry-USB"

// parseSemver extracts major, minor, patch from a version string like "v1.2.3" or "v1.2.3-beta.1".
// Returns (major, minor, patch, prerelease, ok). The prerelease part is everything after the first "-".
func parseSemver(v string) (int, int, int, string, bool) {
	v = strings.TrimPrefix(v, "v")
	pre := ""
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		pre = v[idx+1:]
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0, 0, 0, "", false
	}
	var nums [3]int
	for i := 0; i < 3; i++ {
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				return 0, 0, 0, "", false
			}
			n = n*10 + int(c-'0')
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], pre, true
}

// isVersionNewer returns true if candidate is a newer version than current.
// Prerelease versions (e.g. v2.5.0-beta.1) are considered newer than any stable
// release with a lower base version, but older than the same base version without
// a prerelease suffix (v2.5.0-beta.1 < v2.5.0).
func isVersionNewer(candidate, current string) bool {
	cMaj, cMin, cPat, cPre, cOK := parseSemver(candidate)
	uMaj, uMin, uPat, uPre, uOK := parseSemver(current)
	if !cOK || !uOK {
		// Fallback: if we can't parse either, treat as "different = newer" for safety
		return candidate != current
	}

	// Compare base versions
	if cMaj != uMaj {
		return cMaj > uMaj
	}
	if cMin != uMin {
		return cMin > uMin
	}
	if cPat != uPat {
		return cPat > uPat
	}

	// Same base version: stable (no prerelease) beats prerelease
	if uPre == "" && cPre == "" {
		return false // same version
	}
	if uPre != "" && cPre == "" {
		// User is on prerelease, candidate is stable with same base → candidate is newer
		return true
	}
	if uPre == "" && cPre != "" {
		// User is on stable, candidate is prerelease with same base → candidate is older
		return false
	}
	// Both prerelease with same base — simple string compare (beta.1 < beta.2)
	return cPre > uPre
}

// releaseInfo mirrors the fields we need from a GitHub release object.
type releaseInfo struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// fetchReleases fetches the most recent GitHub releases (both stable and prerelease).
func fetchReleases() ([]releaseInfo, error) {
	output, err := shell.RunWithTimeout(10*time.Second, "curl", "-sfL", "--max-time", "8",
		fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", updateRepo))
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	var releases []releaseInfo
	if err := json.Unmarshal([]byte(output), &releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases: %w", err)
	}
	return releases, nil
}

// findLatestReleases picks the first stable and first prerelease from a list.
// Draft releases are skipped — only published releases are considered.
func findLatestReleases(releases []releaseInfo) (stable *releaseInfo, prerelease *releaseInfo) {
	for i := range releases {
		if releases[i].Draft {
			continue
		}
		if releases[i].Prerelease {
			if prerelease == nil {
				prerelease = &releases[i]
			}
		} else {
			if stable == nil {
				stable = &releases[i]
			}
		}
		if stable != nil && prerelease != nil {
			break
		}
	}
	return
}

func (h *handlers) checkInternet(w http.ResponseWriter, r *http.Request) {
	_, err := shell.RunWithTimeout(10*time.Second, "curl", "-sf", "--max-time", "8",
		"-o", "/dev/null", "https://github.com")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"connected": false,
			"error":     "Cannot reach github.com",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connected": true,
	})
}

func (h *handlers) runUpdate(w http.ResponseWriter, r *http.Request) {
	// Parse optional target version from request body.
	// If empty, installs the latest stable release (backward-compatible).
	var req struct {
		Version string `json:"version"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	targetVersion := req.Version

	go func() {
		broadcast := func(status, msg string) {
			h.hub.Broadcast("update_status", map[string]string{"status": status, "message": msg})
		}
		broadcastErr := func(msg string) {
			log.Printf("[update] ERROR: %s", msg)
			h.hub.Broadcast("update_status", map[string]string{"status": "error", "error": msg})
		}

		broadcast("checking_internet", "Checking internet connection...")

		// 1. Check internet
		_, err := shell.RunWithTimeout(10*time.Second, "curl", "-sf", "--max-time", "8",
			"-o", "/dev/null", "https://github.com")
		if err != nil {
			broadcastErr("No internet connection. Connect to WiFi first.")
			return
		}

		// 2. Build download URL — tag-specific if a version was requested, otherwise latest
		broadcast("checking", "Checking for release...")
		suffix := binarySuffix()

		var downloadURL string
		if targetVersion != "" {
			downloadURL = fmt.Sprintf(
				"https://github.com/%s/releases/download/%s/sentryusb-%s",
				updateRepo, targetVersion, suffix,
			)
		} else {
			downloadURL = fmt.Sprintf(
				"https://github.com/%s/releases/latest/download/sentryusb-%s",
				updateRepo, suffix,
			)
		}

		// HEAD request to check the binary exists before downloading
		_, err = shell.RunWithTimeout(15*time.Second, "curl", "-sfI", "--max-time", "10",
			downloadURL)
		if err != nil {
			broadcastErr(fmt.Sprintf("No release binary found at GitHub. Publish a release with the binary first.\nURL: %s", downloadURL))
			return
		}

		// 2b. Read current version BEFORE updating (for before/after telemetry)
		oldVersion := ""
		if data, verErr := os.ReadFile("/opt/sentryusb/version"); verErr == nil {
			oldVersion = strings.TrimSpace(string(data))
		}

		// 3. Remount filesystem as read-write
		broadcast("remounting", "Remounting filesystem...")
		shell.Run("bash", "-c", "/root/bin/remountfs_rw")

		// 4. Download the binary
		broadcast("downloading", "Downloading update...")
		tmpPath := "/tmp/sentryusb-update"
		_, err = shell.RunWithTimeout(120*time.Second, "curl", "-fsSL",
			"-o", tmpPath, downloadURL)
		if err != nil {
			broadcastErr("Failed to download update: " + shell.CleanStderr(err.Error()))
			return
		}
		os.Chmod(tmpPath, 0755)

		broadcast("installing", "Installing update...")

		// 5. Determine version tag
		versionTag := targetVersion
		if versionTag == "" {
			// No specific version requested — fetch latest stable tag
			versionTag = "unknown"
			tagOutput, tagErr := shell.RunWithTimeout(10*time.Second, "curl", "-sfL", "--max-time", "8",
				fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo))
			if tagErr == nil {
				var rel struct {
					TagName string `json:"tag_name"`
				}
				if json.Unmarshal([]byte(tagOutput), &rel) == nil && rel.TagName != "" {
					versionTag = rel.TagName
				}
			}
		}

		// 5b. Send "before update" telemetry with old version so Discord shows the update is starting
		// Only send updateAvailable=true if the version is actually changing
		if oldVersion != "" && oldVersion != "dev" && oldVersion != "unknown" && oldVersion != versionTag {
			sendTelemetry(oldVersion, true, versionTag)
		}

		// 6. Replace the running binary
		installDir := "/opt/sentryusb"
		os.MkdirAll(installDir, 0755)
		binaryPath := installDir + "/sentryusb"

		// Backup current binary
		if _, statErr := os.Stat(binaryPath); statErr == nil {
			os.Rename(binaryPath, binaryPath+".bak")
		}

		// Move new binary into place
		_, err = shell.Run("mv", tmpPath, binaryPath)
		if err != nil {
			os.Rename(binaryPath+".bak", binaryPath)
			broadcastErr("Failed to install update: " + err.Error())
			return
		}
		os.Chmod(binaryPath, 0755)

		// 7. Write version file
		os.WriteFile(installDir+"/version", []byte(versionTag+"\n"), 0644)
		log.Printf("[update] Updated to %s (%s)", versionTag, suffix)

		// Clear the stale update-check cache so the UI doesn't show "update available to vX.Y.Z"
		// immediately after installing that same version. The next checkForUpdate call will
		// rewrite it correctly.
		os.Remove("/tmp/sentryusb-update-check.json")

		// 8. Update shell scripts from the repo source
		broadcast("updating_scripts", "Updating shell scripts...")
		scriptRef := versionTag
		if scriptRef == "unknown" || scriptRef == "" {
			scriptRef = "main-dev"
		}
		tarballURL := fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", updateRepo, scriptRef)
		scriptUpdateCmd := fmt.Sprintf(`set -e
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT
curl -fsSL "%s" | tar xz --strip-components=1 -C "$TMPDIR"

# Update top-level run/ scripts in /root/bin/
# Always install (not just update existing) so new scripts in future versions get deployed
for f in "$TMPDIR"/run/*; do
  [ -f "$f" ] || continue
  name=$(basename "$f")
  cp "$f" "/root/bin/$name"
  chmod +x "/root/bin/$name"
done

# Update archive module scripts from the configured ARCHIVE_SYSTEM only
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

# Update setup-sentryusb from setup/pi/
if [ -f "$TMPDIR/setup/pi/setup-sentryusb" ]; then
  cp "$TMPDIR/setup/pi/setup-sentryusb" "/root/bin/setup-sentryusb"
  chmod +x "/root/bin/setup-sentryusb"
fi

# Update envsetup.sh from setup/pi/
if [ -f "$TMPDIR/setup/pi/envsetup.sh" ]; then
  cp "$TMPDIR/setup/pi/envsetup.sh" "/root/bin/envsetup.sh"
  chmod +x "/root/bin/envsetup.sh"
fi

# Update BLE peripheral script from server/ble/
if [ -f "$TMPDIR/server/ble/sentryusb-ble.py" ]; then
  cp "$TMPDIR/server/ble/sentryusb-ble.py" "/root/bin/sentryusb-ble.py"
  chmod +x "/root/bin/sentryusb-ble.py"
fi
if [ -f "$TMPDIR/server/ble/sentryusb-ble.service" ]; then
  cp "$TMPDIR/server/ble/sentryusb-ble.service" "/etc/systemd/system/sentryusb-ble.service"
  systemctl daemon-reload
fi

# Install/update Avahi mDNS service for iOS app discovery
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

# Restart BLE service so it picks up new code and re-adds dynamic Avahi suffix
systemctl restart sentryusb-ble 2>/dev/null || true
`, tarballURL)
		scriptOut, scriptErr := shell.RunWithTimeout(120*time.Second, "bash", "-c", scriptUpdateCmd)
		if scriptErr != nil {
			log.Printf("[update] Warning: failed to update scripts: %v\n%s", scriptErr, scriptOut)
			// Non-fatal — binary was already updated, so continue to restart
		} else {
			log.Printf("[update] Shell scripts updated from %s", scriptRef)
		}

		// Write startup-migration marker so the new binary doesn't redundantly
		// re-run the migration on next boot (we just did the same work above).
		migrateMarker := fmt.Sprintf("/opt/sentryusb/.migrated-%s", versionTag)
		os.WriteFile(migrateMarker, []byte("migrated\n"), 0644)

		// Send telemetry: "updated to new version" (synchronous so it completes before restart)
		sendTelemetrySync(versionTag, false, "")

		broadcast("restarting", "Restarting SentryUSB service...")

		// 9. Restart the service — this kills us, so it must be last
		time.Sleep(500 * time.Millisecond) // brief pause so the broadcast reaches the client
		shell.Run("systemctl", "restart", "sentryusb")
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "update_started"})
}

// checkForUpdate checks GitHub for newer releases and stores the result.
// Returns both the latest stable release and (optionally) the latest prerelease
// so the UI can show both options and let the user choose.
//
// Query params:
//   - include_prerelease=true  — also return prerelease info (one-time check)
//
// If the user preference "update_channel" is "prerelease", prereleases are
// always included regardless of the query param.
//
// Telemetry only reports stable updates — prereleases are never reported.
func (h *handlers) checkForUpdate(w http.ResponseWriter, r *http.Request) {
	log.Printf("[update] checkForUpdate called (source: %s)", r.RemoteAddr)

	currentVersion := ""
	if data, err := os.ReadFile("/opt/sentryusb/version"); err == nil {
		currentVersion = strings.TrimSpace(string(data))
	}

	// Should we include prerelease info?
	includePrerelease := r.URL.Query().Get("include_prerelease") == "true"
	if !includePrerelease {
		prefs := loadPreferences()
		includePrerelease = prefs["update_channel"] == "prerelease"
	}

	// Track whether we sent rich telemetry; if not, the defer sends a basic heartbeat.
	telemetrySent := false
	defer func() {
		if !telemetrySent {
			log.Printf("[update] Sending basic telemetry heartbeat (GitHub check didn't complete)")
			sendTelemetry(currentVersion, false, "")
		}
	}()

	releases, err := fetchReleases()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"update_available": false,
			"error":            "Cannot reach GitHub",
		})
		return
	}

	latestStable, latestPrerelease := findLatestReleases(releases)

	canUpdate := currentVersion != "" && currentVersion != "dev"

	// Build response — top-level fields stay backward-compatible (stable info)
	result := map[string]interface{}{
		"current_version": currentVersion,
		"checked_at":      time.Now().UTC().Format(time.RFC3339),
	}

	// Detect if user is currently on a prerelease
	_, _, _, currentPre, currentOK := parseSemver(currentVersion)
	onPrerelease := currentOK && currentPre != ""

	if latestStable != nil {
		stableAvailable := canUpdate && isVersionNewer(latestStable.TagName, currentVersion)
		result["update_available"] = stableAvailable
		result["latest_version"] = latestStable.TagName
		result["release_url"] = latestStable.HTMLURL
		result["release_notes"] = latestStable.Body
		result["stable"] = map[string]interface{}{
			"version":       latestStable.TagName,
			"release_url":   latestStable.HTMLURL,
			"release_notes": latestStable.Body,
			"available":     stableAvailable,
		}

		// If user is on a prerelease and the latest stable isn't flagged as
		// a newer version (e.g. prerelease has a higher base), offer the
		// stable release as a revert/downgrade option.
		if onPrerelease && canUpdate && !stableAvailable {
			result["revert_stable"] = map[string]interface{}{
				"version":       latestStable.TagName,
				"release_url":   latestStable.HTMLURL,
				"release_notes": latestStable.Body,
			}
		}
	} else {
		result["update_available"] = false
	}

	if includePrerelease && latestPrerelease != nil {
		preAvailable := canUpdate && isVersionNewer(latestPrerelease.TagName, currentVersion)
		result["prerelease"] = map[string]interface{}{
			"version":       latestPrerelease.TagName,
			"release_url":   latestPrerelease.HTMLURL,
			"release_notes": latestPrerelease.Body,
			"available":     preAvailable,
		}
	}

	// Cache for the Settings UI to read on page load
	if data, err := json.Marshal(result); err == nil {
		os.WriteFile("/tmp/sentryusb-update-check.json", data, 0644)
	}

	// Telemetry — only report stable updates, never prereleases
	newVer := ""
	if latestStable != nil && canUpdate && isVersionNewer(latestStable.TagName, currentVersion) {
		newVer = latestStable.TagName
	}
	sendTelemetry(currentVersion, newVer != "", newVer)
	telemetrySent = true

	writeJSON(w, http.StatusOK, result)
}

// getUpdateStatus returns the last cached update check result
func (h *handlers) getUpdateStatus(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("/tmp/sentryusb-update-check.json")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"update_available": false,
			"checked_at":       "",
		})
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"update_available": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handlers) getVersion(w http.ResponseWriter, r *http.Request) {
	version := ""
	if data, err := os.ReadFile("/opt/sentryusb/version"); err == nil {
		version = strings.TrimSpace(string(data))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version": version,
		"arch":    runtime.GOARCH,
		"os":      runtime.GOOS,
	})
}

// binarySuffix returns the release binary suffix for the current platform.
// On ARM it checks uname -m to distinguish ARMv6 (Pi Zero W) from ARMv7.
func binarySuffix() string {
	if runtime.GOARCH == "arm" {
		if out, err := shell.RunWithTimeout(5*time.Second, "uname", "-m"); err == nil {
			if strings.TrimSpace(out) == "armv6l" {
				return "linux-armv6"
			}
		}
		return "linux-armv7"
	}
	return "linux-arm64"
}
