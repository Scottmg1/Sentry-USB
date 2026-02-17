package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/Scottmg1/Sentry-USB/server/config"
	"github.com/Scottmg1/Sentry-USB/server/shell"
)

// Setup finished marker paths (in priority order)
var setupFinishedPaths = []string{
	"/teslausb/TESLAUSB_SETUP_FINISHED",
	"/boot/firmware/TESLAUSB_SETUP_FINISHED",
	"/boot/TESLAUSB_SETUP_FINISHED",
}

var setupStartedPaths = []string{
	"/teslausb/TESLAUSB_SETUP_STARTED",
	"/boot/firmware/TESLAUSB_SETUP_STARTED",
	"/boot/TESLAUSB_SETUP_STARTED",
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

// IsSetupIncomplete returns true if setup was started but never finished (e.g. reboot mid-setup).
func IsSetupIncomplete() bool {
	return isSetupStarted() && !isSetupFinished()
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

// executeSetup runs setup-teslausb, downloading scripts if needed.
// It broadcasts progress via WebSocket and is safe to call from goroutines.
func executeSetup(hub broadcaster) {
	setupRunning.Lock()
	if setupRunning.running {
		setupRunning.Unlock()
		return
	}
	setupRunning.running = true
	setupRunning.Unlock()

	defer func() {
		setupRunning.Lock()
		setupRunning.running = false
		setupRunning.Unlock()
	}()

	hub.Broadcast("setup_status", map[string]string{"status": "starting"})

	setupScript := "/root/bin/setup-teslausb"

	// If the setup script doesn't exist, download it from the repo.
	// The script itself handles downloading all other dependencies via copy_script.
	if _, err := os.Stat(setupScript); os.IsNotExist(err) {
		log.Println("[setup] setup-teslausb not found locally, downloading from repo...")
		hub.Broadcast("setup_status", map[string]string{"status": "downloading_scripts"})

		_, dlErr := shell.RunWithTimeout(60_000_000_000, "bash", "-c",
			"mkdir -p /root/bin && "+
				"curl -fsSL https://raw.githubusercontent.com/Scottmg1/Sentry-USB/main-dev/setup/pi/setup-teslausb -o /root/bin/setup-teslausb && "+
				"curl -fsSL https://raw.githubusercontent.com/Scottmg1/Sentry-USB/main-dev/setup/pi/envsetup.sh -o /root/bin/envsetup.sh && "+
				"chmod +x /root/bin/setup-teslausb /root/bin/envsetup.sh")
		if dlErr != nil {
			hub.Broadcast("setup_status", map[string]string{
				"status": "error",
				"error":  "Failed to download setup script: " + dlErr.Error(),
			})
			return
		}
		log.Println("[setup] Setup script downloaded")
	}

	hub.Broadcast("setup_status", map[string]string{"status": "running"})

	// setup-teslausb can take a long time (package installs, partitioning, etc.)
	// Run directly (not via "bash") so child scripts see parent comm as "setup-teslausb"
	output, err := shell.RunWithTimeout(1800_000_000_000, setupScript)
	if err != nil {
		log.Printf("[setup] Setup failed: %v", err)
		hub.Broadcast("setup_status", map[string]string{
			"status": "error",
			"error":  err.Error(),
			"output": output,
		})
		return
	}

	log.Println("[setup] Setup completed successfully")
	hub.Broadcast("setup_status", map[string]string{
		"status": "complete",
		"output": output,
	})
}

func (h *handlers) runSetup(w http.ResponseWriter, r *http.Request) {
	setupRunning.Lock()
	if setupRunning.running {
		setupRunning.Unlock()
		writeError(w, http.StatusConflict, "Setup is already running")
		return
	}
	setupRunning.Unlock()

	go executeSetup(h.hub)

	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// AutoResumeSetup checks if a previous setup was interrupted (e.g. by a reboot)
// and automatically continues it. Called once at server startup.
func AutoResumeSetup(hub broadcaster) {
	if !IsSetupIncomplete() {
		return
	}
	log.Println("[setup] Detected incomplete setup (STARTED exists, FINISHED missing). Auto-resuming...")
	go executeSetup(hub)
}
