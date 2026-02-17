package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/Scottmg1/Sentry-USB/server/config"
	"github.com/Scottmg1/Sentry-USB/server/shell"
)

// Setup finished marker paths (in priority order)
var setupFinishedPaths = []string{
	"/sentryusb/SENTRYUSB_SETUP_FINISHED",
	"/boot/firmware/SENTRYUSB_SETUP_FINISHED",
	"/boot/SENTRYUSB_SETUP_FINISHED",
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

func (h *handlers) getSetupStatus(w http.ResponseWriter, r *http.Request) {
	setupRunning.Lock()
	running := setupRunning.running
	setupRunning.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_finished": isSetupFinished(),
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
	// rc.local creates SENTRYUSB_SETUP_STARTED, downloads setup-sentryusb,
	// runs it, and reboots when done. On each boot, rc.local re-runs
	// setup until SENTRYUSB_SETUP_FINISHED exists.
	go func() {
		defer func() {
			setupRunning.Lock()
			setupRunning.running = false
			setupRunning.Unlock()
		}()

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
