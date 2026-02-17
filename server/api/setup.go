package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Scottmg1/Sentry-USB/server/config"
	"github.com/Scottmg1/Sentry-USB/server/shell"
)

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
	// Run setup in background and stream progress via WebSocket
	go func() {
		h.hub.Broadcast("setup_status", map[string]string{"status": "starting"})

		output, err := shell.RunWithTimeout(600_000_000_000, "bash", "/root/bin/setup-teslausb")
		if err != nil {
			h.hub.Broadcast("setup_status", map[string]string{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}

		h.hub.Broadcast("setup_status", map[string]string{
			"status": "complete",
			"output": output,
		})
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}
