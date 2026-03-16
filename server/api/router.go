package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/Scottmg1/Sentry-USB/server/ws"
)

func RegisterRoutes(mux *http.ServeMux, hub *ws.Hub) {
	// Ensure Wraps and LicensePlate folders exist on startup
	ensureMediaFolders()

	h := &handlers{hub: hub}

	// Status & config
	mux.HandleFunc("GET /api/status", h.getStatus)
	mux.HandleFunc("GET /api/status/storage", h.getStorageBreakdown)
	mux.HandleFunc("GET /api/config", h.getConfig)

	// WiFi detection
	mux.HandleFunc("GET /api/wifi", h.getWifiConfig)

	// Setup configuration
	mux.HandleFunc("GET /api/setup/status", h.getSetupStatus)
	mux.HandleFunc("GET /api/setup/config", h.getSetupConfig)
	mux.HandleFunc("PUT /api/setup/config", h.saveSetupConfig)
	mux.HandleFunc("POST /api/setup/run", h.runSetup)
	mux.HandleFunc("POST /api/setup/test-archive", h.testArchive)

	// Clips
	mux.HandleFunc("GET /api/clips", h.getClips)
	mux.HandleFunc("GET /api/clips/telemetry", h.getClipTelemetry)

	// File operations
	mux.HandleFunc("GET /api/files/ls", h.listFiles)
	mux.HandleFunc("POST /api/files/mkdir", h.createDir)
	mux.HandleFunc("POST /api/files/mv", h.moveFile)
	mux.HandleFunc("POST /api/files/cp", h.copyFile)
	mux.HandleFunc("DELETE /api/files", h.deleteFile)
	mux.HandleFunc("POST /api/files/upload", h.uploadFile)
	mux.HandleFunc("GET /api/files/download", h.downloadFile)
	mux.HandleFunc("GET /api/files/download-zip", h.downloadZip)
	mux.HandleFunc("POST /api/files/download-zip-multi", h.downloadZipMulti)

	// Logs
	mux.HandleFunc("GET /api/logs/{name}", h.getLog)

	// Diagnostics
	mux.HandleFunc("POST /api/diagnostics/refresh", h.refreshDiagnostics)
	mux.HandleFunc("GET /api/diagnostics", h.getDiagnostics)
	mux.HandleFunc("GET /api/system/health-check", h.healthCheck)

	// System actions
	mux.HandleFunc("POST /api/system/reboot", h.reboot)
	mux.HandleFunc("POST /api/system/toggle-drives", h.toggleDrives)
	mux.HandleFunc("POST /api/system/trigger-sync", h.triggerSync)
	mux.HandleFunc("POST /api/system/ble-pair", h.blePair)
	mux.HandleFunc("GET /api/system/ble-status", h.bleStatus)
	mux.HandleFunc("GET /api/system/speedtest", h.speedtest)
	mux.HandleFunc("GET /api/system/rtc-status", h.getRTCStatus)

	// SSH key management (for rsync to NAS)
	mux.HandleFunc("GET /api/system/ssh-pubkey", h.getSSHPubKey)
	mux.HandleFunc("POST /api/system/ssh-keygen", h.generateSSHKey)

	// Updates
	mux.HandleFunc("GET /api/system/check-internet", h.checkInternet)
	mux.HandleFunc("POST /api/system/update", h.runUpdate)
	mux.HandleFunc("GET /api/system/version", h.getVersion)
	mux.HandleFunc("POST /api/system/check-update", h.checkForUpdate)
	mux.HandleFunc("GET /api/system/update-status", h.getUpdateStatus)

	// User preferences
	mux.HandleFunc("GET /api/config/preference", h.getPreference)
	mux.HandleFunc("PUT /api/config/preference", h.setPreference)

	// Block devices (for data drive selection)
	mux.HandleFunc("GET /api/system/block-devices", h.listBlockDevices)

	// Notification pairing (mobile app push notifications)
	mux.HandleFunc("POST /api/notifications/generate-code", h.generateNotificationPairingCode)
	mux.HandleFunc("GET /api/notifications/paired-devices", h.listNotificationPairedDevices)
	mux.HandleFunc("DELETE /api/notifications/paired-devices/{id}", h.removeNotificationPairedDevice)
	mux.HandleFunc("POST /api/notifications/test", h.sendTestNotification)

	// Support chat (proxy to api.sentry-six.com)
	mux.HandleFunc("GET /api/support/check", h.checkSupportAvailable)
	mux.HandleFunc("POST /api/support/ticket", h.createSupportTicket)
	mux.HandleFunc("POST /api/support/ticket/{id}/message", h.sendSupportMessage)
	mux.HandleFunc("POST /api/support/ticket/{id}/media", h.uploadSupportMedia)
	mux.HandleFunc("GET /api/support/ticket/{id}/messages", h.fetchSupportMessages)
	mux.HandleFunc("POST /api/support/ticket/{id}/close", h.closeSupportTicket)
	mux.HandleFunc("POST /api/support/ticket/{id}/mark-read", h.markSupportRead)
	mux.HandleFunc("POST /api/support/ticket/{id}/register-device", h.registerSupportDevice)
	mux.HandleFunc("POST /api/support/ticket/{id}/unregister-device", h.unregisterSupportDevice)

	// Community wraps (proxy to api.sentry-six.com)
	mux.HandleFunc("GET /api/wraps/library", h.communityWrapsLibrary)
	mux.HandleFunc("GET /api/wraps/thumbnail/{code}", h.communityWrapsThumbnail)
	mux.HandleFunc("POST /api/wraps/upload", h.communityWrapsUpload)
	mux.HandleFunc("POST /api/wraps/download/{code}", h.communityWrapsDownload)

	// Authentication
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/check", h.authCheck)

	// Web terminal (PTY over WebSocket)
	mux.HandleFunc("/api/terminal", h.handleTerminal)
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
