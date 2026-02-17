package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/Scottmg1/Sentry-USB/server/ws"
)

func RegisterRoutes(mux *http.ServeMux, hub *ws.Hub) {
	h := &handlers{hub: hub}

	// Status & config
	mux.HandleFunc("GET /api/status", h.getStatus)
	mux.HandleFunc("GET /api/config", h.getConfig)

	// Setup configuration
	mux.HandleFunc("GET /api/setup/config", h.getSetupConfig)
	mux.HandleFunc("PUT /api/setup/config", h.saveSetupConfig)
	mux.HandleFunc("POST /api/setup/run", h.runSetup)

	// Clips
	mux.HandleFunc("GET /api/clips", h.getClips)

	// File operations
	mux.HandleFunc("GET /api/files/ls", h.listFiles)
	mux.HandleFunc("POST /api/files/mkdir", h.createDir)
	mux.HandleFunc("POST /api/files/mv", h.moveFile)
	mux.HandleFunc("POST /api/files/cp", h.copyFile)
	mux.HandleFunc("DELETE /api/files", h.deleteFile)
	mux.HandleFunc("POST /api/files/upload", h.uploadFile)
	mux.HandleFunc("GET /api/files/download", h.downloadFile)
	mux.HandleFunc("GET /api/files/download-zip", h.downloadZip)

	// Logs
	mux.HandleFunc("GET /api/logs/{name}", h.getLog)

	// Diagnostics
	mux.HandleFunc("POST /api/diagnostics/refresh", h.refreshDiagnostics)
	mux.HandleFunc("GET /api/diagnostics", h.getDiagnostics)

	// System actions
	mux.HandleFunc("POST /api/system/reboot", h.reboot)
	mux.HandleFunc("POST /api/system/toggle-drives", h.toggleDrives)
	mux.HandleFunc("POST /api/system/trigger-sync", h.triggerSync)
	mux.HandleFunc("POST /api/system/ble-pair", h.blePair)
	mux.HandleFunc("GET /api/system/ble-status", h.bleStatus)
	mux.HandleFunc("GET /api/system/speedtest", h.speedtest)

	// Updates
	mux.HandleFunc("GET /api/system/check-internet", h.checkInternet)
	mux.HandleFunc("POST /api/system/update", h.runUpdate)
	mux.HandleFunc("GET /api/system/version", h.getVersion)
}

type handlers struct {
	hub *ws.Hub
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error writing JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
