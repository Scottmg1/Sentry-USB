package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/config"
	"github.com/Scottmg1/Sentry-USB/server/shell"
)

// Setup finished marker paths (in priority order)
var setupFinishedPaths = []string{
	"/sentryusb/SENTRYUSB_SETUP_FINISHED",
	"/boot/firmware/SENTRYUSB_SETUP_FINISHED",
	"/boot/SENTRYUSB_SETUP_FINISHED",
}

// Setup started marker paths (in priority order)
var setupStartedPaths = []string{
	"/sentryusb/SENTRYUSB_SETUP_STARTED",
	"/boot/firmware/SENTRYUSB_SETUP_STARTED",
	"/boot/SENTRYUSB_SETUP_STARTED",
}

var setupRunning struct {
	sync.Mutex
	running bool
}

func isSetupFinished() bool {
	for _, p := range setupFinishedPaths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func isSetupStarted() bool {
	for _, p := range setupStartedPaths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func (h *handlers) getSetupStatus(w http.ResponseWriter, r *http.Request) {
	setupRunning.Lock()
	running := setupRunning.running
	setupRunning.Unlock()

	finished := isSetupFinished()

	// If setup was started (marker file on disk) but not finished,
	// treat it as running even if the in-memory flag was lost during reboot.
	if !running && !finished && isSetupStarted() {
		running = true
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_finished": finished,
		"setup_running":  running,
	})
}

func (h *handlers) getSetupConfig(w http.ResponseWriter, r *http.Request) {
	configPath := config.FindConfigPath()

	active, commented, err := config.ParseFile(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read config: "+err.Error())
		return
	}

	// Merge: active values take precedence, commented values are defaults
	merged := make(map[string]interface{})
	for k, v := range commented {
		merged[k] = map[string]interface{}{
			"value":  v,
			"active": false,
		}
	}
	for k, v := range active {
		merged[k] = map[string]interface{}{
			"value":  v,
			"active": true,
		}
	}

	writeJSON(w, http.StatusOK, merged)
}

func (h *handlers) saveSetupConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var newConfig config.SetupConfig
	if err := json.Unmarshal(body, &newConfig); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Remount filesystem read-write — root fs is mounted read-only by default,
	// so writing to /root/sentryusb.conf will fail without this.
	shell.Run("bash", "-c", "/root/bin/remountfs_rw")

	configPath := config.FindConfigPath()
	if err := config.WriteFile(configPath, newConfig); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to write config: "+err.Error())
		return
	}

	writeOK(w)
}

func (h *handlers) runSetup(w http.ResponseWriter, r *http.Request) {
	setupRunning.Lock()
	if setupRunning.running {
		setupRunning.Unlock()
		writeError(w, http.StatusConflict, "Setup is already running")
		return
	}
	setupRunning.running = true
	setupRunning.Unlock()

	// Run /etc/rc.local — the SentryUSB boot-loop mechanism.
	// rc.local checks for SENTRYUSB_SETUP_STARTED, downloads setup-sentryusb,
	// runs it, and reboots when done. On each boot, rc.local re-runs
	// setup until SENTRYUSB_SETUP_FINISHED exists.
	go func() {
		defer func() {
			setupRunning.Lock()
			setupRunning.running = false
			setupRunning.Unlock()
		}()

		// Remove the finished marker so rc.local will actually re-run setup.
		// Without this, rc.local sees SENTRYUSB_SETUP_FINISHED and exits
		// immediately, making wizard re-runs a silent no-op.
		for _, p := range setupFinishedPaths {
			os.Remove(p)
		}
		// Create the started marker so rc.local knows the user explicitly
		// triggered setup via the wizard. rc.local only proceeds when this
		// marker exists, preventing auto-start on boot before the wizard
		// has been completed.
		for _, p := range setupStartedPaths {
			os.Remove(p)
		}
		if f, err := os.Create(setupStartedPaths[0]); err == nil {
			f.Close()
		}
		// Do NOT delete cached scripts here — they are runtime dependencies
		// (envsetup.sh for archiveloop, setup-sentryusb for diagnostics).
		// Deleting them before setup completes leaves the system broken if
		// setup fails or the Pi reboots mid-setup.  The setup process will
		// overwrite them with fresh copies when it downloads scripts.
		// Remove resize marker so a previous failed resize doesn't block setup
		os.Remove("/root/RESIZE_ATTEMPTED")

		h.hub.Broadcast("setup_status", map[string]string{"status": "running"})
		log.Println("[setup] Running /etc/rc.local (SentryUSB setup boot-loop)")

		// rc.local may reboot the system, which is expected.
		// Timeout is long because setup installs packages, partitions, etc.
		output, err := shell.RunWithTimeout(1800_000_000_000, "/etc/rc.local")
		if err != nil {
			errMsg := err.Error()
			log.Printf("[setup] rc.local exited: %v", err)

			// rc.local may exit due to reboot — this is expected during setup.
			// Detect reboot-related exits and report them as "rebooting" not "error".
			isReboot := strings.Contains(errMsg, "shutdown") ||
				strings.Contains(errMsg, "reboot") ||
				strings.Contains(errMsg, "sleep operation in progress")

			if isSetupFinished() {
				h.hub.Broadcast("setup_status", map[string]string{
					"status": "complete",
					"output": output,
				})
			} else if isReboot {
				log.Println("[setup] System is rebooting as part of setup — this is expected")
				h.hub.Broadcast("setup_status", map[string]string{
					"status":  "rebooting",
					"message": "System is rebooting to continue setup. This page will reconnect automatically.",
				})
			} else {
				h.hub.Broadcast("setup_status", map[string]string{
					"status": "error",
					"error":  shell.CleanStderr(errMsg),
					"output": output,
				})
			}
			return
		}

		log.Println("[setup] rc.local completed")
		h.hub.Broadcast("setup_status", map[string]string{
			"status": "complete",
			"output": output,
		})
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// testArchive tests archive connectivity without running full setup.
func (h *handlers) testArchive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var params map[string]string
	if err := json.Unmarshal(body, &params); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	system := params["ARCHIVE_SYSTEM"]
	if system == "" || system == "none" {
		writeError(w, http.StatusBadRequest, "No archive system specified")
		return
	}

	timeout := 15 * time.Second
	var testErr error

	switch system {
	case "cifs":
		server := params["ARCHIVE_SERVER"]
		share := params["SHARE_NAME"]
		user := params["SHARE_USER"]
		pass := params["SHARE_PASSWORD"]
		domain := params["SHARE_DOMAIN"]
		cifsVer := params["CIFS_VERSION"]
		if server == "" || share == "" || user == "" || pass == "" {
			writeError(w, http.StatusBadRequest, "Missing required CIFS fields")
			return
		}
		tmpDir := "/tmp/sentryusb-archive-test"
		os.MkdirAll(tmpDir, 0755)
		defer os.Remove(tmpDir)

		opts := fmt.Sprintf("username=%s,password=%s,iocharset=utf8", user, pass)
		if domain != "" {
			opts += ",domain=" + domain
		}
		if cifsVer != "" {
			opts += ",vers=" + cifsVer
		}
		src := fmt.Sprintf("//%s/%s", server, share)
		_, testErr = shell.RunWithTimeout(timeout, "mount", "-t", "cifs", src, tmpDir, "-o", opts)
		if testErr == nil {
			shell.RunWithTimeout(5*time.Second, "umount", tmpDir)
		}

	case "rsync":
		server := params["RSYNC_SERVER"]
		user := params["RSYNC_USER"]
		path := params["RSYNC_PATH"]
		if server == "" || user == "" || path == "" {
			writeError(w, http.StatusBadRequest, "Missing required rsync fields")
			return
		}
		// Test SSH connectivity (rsync uses SSH)
		_, testErr = shell.RunWithTimeout(timeout, "ssh",
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=no",
			"-o", "BatchMode=yes",
			fmt.Sprintf("%s@%s", user, server),
			"echo", "ok")

	case "rclone":
		drive := params["RCLONE_DRIVE"]
		rpath := params["RCLONE_PATH"]
		if drive == "" || rpath == "" {
			writeError(w, http.StatusBadRequest, "Missing required rclone fields")
			return
		}
		// Test rclone remote accessibility
		_, testErr = shell.RunWithTimeout(timeout, "rclone", "lsd", fmt.Sprintf("%s:%s", drive, rpath))

	case "nfs":
		server := params["ARCHIVE_SERVER"]
		exportPath := params["SHARE_NAME"]
		if server == "" || exportPath == "" {
			writeError(w, http.StatusBadRequest, "Missing required NFS fields")
			return
		}
		tmpDir := "/tmp/sentryusb-archive-test"
		os.MkdirAll(tmpDir, 0755)
		defer os.Remove(tmpDir)

		src := fmt.Sprintf("%s:%s", server, exportPath)
		_, testErr = shell.RunWithTimeout(timeout, "mount", "-t", "nfs", src, tmpDir, "-o", "nolock,soft,timeo=50")
		if testErr == nil {
			shell.RunWithTimeout(5*time.Second, "umount", tmpDir)
		}

	default:
		writeError(w, http.StatusBadRequest, "Unknown archive system: "+system)
		return
	}

	if testErr != nil {
		errMsg := testErr.Error()
		// Clean up verbose error prefix for display
		if idx := strings.Index(errMsg, "stderr: "); idx >= 0 {
			errMsg = errMsg[idx+len("stderr: "):]
		}
		log.Printf("[setup] Archive test failed for %s: %v", system, testErr)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   strings.TrimSpace(errMsg),
		})
		return
	}

	log.Printf("[setup] Archive test succeeded for %s", system)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
