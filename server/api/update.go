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
const telemetryURL = "https://api.sentry-six.com/sentryusb/telemetry"

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

		// 2. Check if a release actually exists
		broadcast("checking", "Checking for latest release...")
		arch := runtime.GOARCH
		suffix := "linux-arm64"
		if arch == "arm" {
			suffix = "linux-armv7"
		}
		downloadURL := fmt.Sprintf(
			"https://github.com/%s/releases/latest/download/sentryusb-%s",
			updateRepo, suffix,
		)

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
		broadcast("downloading", "Downloading latest release...")
		tmpPath := "/tmp/sentryusb-update"
		_, err = shell.RunWithTimeout(120*time.Second, "curl", "-fsSL",
			"-o", tmpPath, downloadURL)
		if err != nil {
			broadcastErr("Failed to download update: " + shell.CleanStderr(err.Error()))
			return
		}
		os.Chmod(tmpPath, 0755)

		broadcast("installing", "Installing update...")

		// 5. Also fetch the latest version tag
		versionTag := "unknown"
		tagOutput, tagErr := shell.RunWithTimeout(10*time.Second, "curl", "-sfL", "--max-time", "8",
			fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo))
		if tagErr == nil {
			var release struct {
				TagName string `json:"tag_name"`
			}
			if json.Unmarshal([]byte(tagOutput), &release) == nil && release.TagName != "" {
				versionTag = release.TagName
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
`, tarballURL)
		scriptOut, scriptErr := shell.RunWithTimeout(120*time.Second, "bash", "-c", scriptUpdateCmd)
		if scriptErr != nil {
			log.Printf("[update] Warning: failed to update scripts: %v\n%s", scriptErr, scriptOut)
			// Non-fatal — binary was already updated, so continue to restart
		} else {
			log.Printf("[update] Shell scripts updated from %s", scriptRef)
		}

		// Send telemetry: "updated to new version" (synchronous so it completes before restart)
		sendTelemetrySync(versionTag, false, "")

		broadcast("restarting", "Restarting SentryUSB service...")

		// 9. Restart the service — this kills us, so it must be last
		time.Sleep(500 * time.Millisecond) // brief pause so the broadcast reaches the client
		shell.Run("systemctl", "restart", "sentryusb")
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "update_started"})
}

// checkForUpdate checks GitHub for a newer release and stores the result.
// Always sends a telemetry heartbeat regardless of whether GitHub is reachable,
// so the API server sees the device even when called from shell scripts
// (e.g. post-archive-process.sh) that may run without full internet.
func (h *handlers) checkForUpdate(w http.ResponseWriter, r *http.Request) {
	log.Printf("[update] checkForUpdate called (source: %s)", r.RemoteAddr)

	currentVersion := ""
	if data, err := os.ReadFile("/opt/sentryusb/version"); err == nil {
		currentVersion = strings.TrimSpace(string(data))
	}

	// Track whether we sent rich telemetry; if not, the defer sends a basic heartbeat.
	telemetrySent := false
	defer func() {
		if !telemetrySent {
			log.Printf("[update] Sending basic telemetry heartbeat (GitHub check didn't complete)")
			sendTelemetry(currentVersion, false, "")
		}
	}()

	tagOutput, tagErr := shell.RunWithTimeout(10*time.Second, "curl", "-sfL", "--max-time", "8",
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo))
	if tagErr != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"update_available": false,
			"error":            "Cannot reach GitHub",
		})
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(tagOutput), &release); err != nil || release.TagName == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"update_available": false,
			"error":            "Failed to parse release info",
		})
		return
	}

	updateAvailable := release.TagName != currentVersion && currentVersion != "" && currentVersion != "dev"

	// Store the check result for the Settings UI to read
	updateInfo := map[string]interface{}{
		"update_available":  updateAvailable,
		"current_version":   currentVersion,
		"latest_version":    release.TagName,
		"release_url":       release.HTMLURL,
		"release_notes":     release.Body,
		"checked_at":        time.Now().UTC().Format(time.RFC3339),
	}
	if data, err := json.Marshal(updateInfo); err == nil {
		os.WriteFile("/tmp/sentryusb-update-check.json", data, 0644)
	}

	// Send telemetry with update info
	newVer := ""
	if updateAvailable {
		newVer = release.TagName
	}
	sendTelemetry(currentVersion, updateAvailable, newVer)
	telemetrySent = true

	writeJSON(w, http.StatusOK, updateInfo)
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
