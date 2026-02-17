package api

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

const updateRepo = "Scottmg1/Sentry-USB"

func (h *handlers) checkInternet(w http.ResponseWriter, r *http.Request) {
	// Try to reach GitHub
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
		h.hub.Broadcast("update_status", map[string]string{"status": "checking_internet"})

		// 1. Check internet connectivity
		_, err := shell.RunWithTimeout(10*time.Second, "curl", "-sf", "--max-time", "8",
			"-o", "/dev/null", "https://github.com")
		if err != nil {
			h.hub.Broadcast("update_status", map[string]string{
				"status": "error",
				"error":  "No internet connection. Connect to WiFi first.",
			})
			return
		}

		h.hub.Broadcast("update_status", map[string]string{"status": "remounting_rw"})

		// 2. Remount filesystem as read-write
		shell.Run("bash", "-c", "/root/bin/remountfs_rw")

		h.hub.Broadcast("update_status", map[string]string{"status": "downloading"})

		// 3. Determine architecture and download URL
		arch := runtime.GOARCH
		suffix := "linux-arm64"
		if arch == "arm" {
			suffix = "linux-armv7"
		}

		downloadURL := fmt.Sprintf(
			"https://github.com/%s/releases/latest/download/sentryusb-%s",
			updateRepo, suffix,
		)

		// Download to temp location
		tmpPath := "/tmp/sentryusb-update"
		_, err = shell.RunWithTimeout(120*time.Second, "curl", "-fsSL",
			"-o", tmpPath, downloadURL)
		if err != nil {
			h.hub.Broadcast("update_status", map[string]string{
				"status": "error",
				"error":  "Failed to download update: " + err.Error(),
			})
			return
		}

		// Make executable
		os.Chmod(tmpPath, 0755)

		h.hub.Broadcast("update_status", map[string]string{"status": "installing"})

		// 4. Also update setup scripts from the repo
		_, _ = shell.RunWithTimeout(60*time.Second, "bash", "-c",
			fmt.Sprintf("curl -fsSL https://raw.githubusercontent.com/%s/main-dev/install.sh | bash", updateRepo))

		// 5. Replace the running binary
		installDir := "/opt/sentryusb"
		os.MkdirAll(installDir, 0755)

		binaryPath := installDir + "/sentryusb"
		// Backup current binary
		if _, err := os.Stat(binaryPath); err == nil {
			os.Rename(binaryPath, binaryPath+".bak")
		}

		// Move new binary into place
		_, err = shell.Run("mv", tmpPath, binaryPath)
		if err != nil {
			// Restore backup
			os.Rename(binaryPath+".bak", binaryPath)
			h.hub.Broadcast("update_status", map[string]string{
				"status": "error",
				"error":  "Failed to install update: " + err.Error(),
			})
			return
		}
		os.Chmod(binaryPath, 0755)

		h.hub.Broadcast("update_status", map[string]string{"status": "restarting"})

		// 6. Restart the service
		shell.Run("systemctl", "restart", "sentryusb")
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "update_started"})
}

func (h *handlers) getVersion(w http.ResponseWriter, r *http.Request) {
	version := "dev"
	// Try to read version from file
	if data, err := os.ReadFile("/opt/sentryusb/version"); err == nil {
		version = strings.TrimSpace(string(data))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version": version,
		"arch":    runtime.GOARCH,
		"os":      runtime.GOOS,
	})
}
