package api

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

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
			// Parse the tag_name from JSON (simple extraction)
			if idx := strings.Index(tagOutput, `"tag_name"`); idx >= 0 {
				rest := tagOutput[idx:]
				if start := strings.Index(rest, `":"`); start >= 0 {
					rest = rest[start+3:]
					if end := strings.Index(rest, `"`); end >= 0 {
						versionTag = rest[:end]
					}
				}
			}
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

		broadcast("restarting", "Restarting SentryUSB service...")

		// 8. Restart the service — this kills us, so it must be last
		time.Sleep(500 * time.Millisecond) // brief pause so the broadcast reaches the client
		shell.Run("systemctl", "restart", "sentryusb")
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "update_started"})
}

func (h *handlers) getVersion(w http.ResponseWriter, r *http.Request) {
	version := "dev"
	if data, err := os.ReadFile("/opt/sentryusb/version"); err == nil {
		version = strings.TrimSpace(string(data))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version": version,
		"arch":    runtime.GOARCH,
		"os":      runtime.GOOS,
	})
}
