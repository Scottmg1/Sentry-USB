package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
)

const preferencesPath = "/mutable/sentryusb-prefs.json"

var prefsMu sync.RWMutex

func loadPreferences() map[string]string {
	prefsMu.RLock()
	defer prefsMu.RUnlock()

	data, err := os.ReadFile(preferencesPath)
	if err != nil {
		return map[string]string{}
	}
	var prefs map[string]string
	if err := json.Unmarshal(data, &prefs); err != nil {
		return map[string]string{}
	}
	return prefs
}

func savePreferences(prefs map[string]string) error {
	prefsMu.Lock()
	defer prefsMu.Unlock()

	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(preferencesPath, data, 0644)
}

func (h *handlers) getPreference(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	prefs := loadPreferences()
	if key != "" {
		writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": prefs[key]})
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (h *handlers) setPreference(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Key == "" {
		writeError(w, http.StatusBadRequest, "Invalid request: need key and value")
		return
	}

	prefs := loadPreferences()
	prefs[req.Key] = req.Value
	if err := savePreferences(prefs); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save preference")
		return
	}

	writeOK(w)
}
