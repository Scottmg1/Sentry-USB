package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

// InitMQTTFromPreferences loads MQTT config from preferences and initializes the manager
func InitMQTTFromPreferences() {
	prefs := loadPreferences()

	config := MQTTConfig{
		Enabled:   prefs["mqtt_enabled"] == "true",
		Host:      prefs["mqtt_host"],
		Port:      1883, // default MQTT port
		Username:  prefs["mqtt_username"],
		Password:  prefs["mqtt_password"],
		BaseTopic: prefs["mqtt_base_topic"],
	}

	// Try to parse port
	if portStr := prefs["mqtt_port"]; portStr != "" && validatePort(portStr) {
		config.Port = parsePortInt(portStr)
	}

	// Only init if enabled
	if !config.Enabled || config.Host == "" {
		log.Println("MQTT not configured or disabled, skipping initialization")
		return
	}

	mgr := GetMQTTManager()
	if err := mgr.Init(config); err != nil {
		log.Printf("Failed to initialize MQTT: %v", err)
		return
	}

	log.Println("MQTT initialized successfully")
}

// getMQTTConfig retrieves the current MQTT configuration
func (h *handlers) getMQTTConfig(w http.ResponseWriter, r *http.Request) {
	prefs := loadPreferences()

	config := MQTTConfig{
		Enabled:   prefs["mqtt_enabled"] == "true",
		Host:      prefs["mqtt_host"],
		Port:      8883, // default, can be parsed from prefs
		Username:  prefs["mqtt_username"],
		Password:  prefs["mqtt_password"],
		BaseTopic: prefs["mqtt_base_topic"],
	}

	// Try to parse port
	if portStr := prefs["mqtt_port"]; portStr != "" && validatePort(portStr) {
		config.Port = parsePortInt(portStr)
	}

	writeJSON(w, http.StatusOK, config)
}

// saveMQTTConfig saves MQTT configuration
func (h *handlers) saveMQTTConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var config MQTTConfig
	if err := json.Unmarshal(body, &config); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Validate host is provided if enabled
	if config.Enabled && config.Host == "" {
		writeError(w, http.StatusBadRequest, "MQTT host is required when enabled")
		return
	}

	// Validate port range
	if config.Port < 1 || config.Port > 65535 {
		writeError(w, http.StatusBadRequest, "MQTT port must be between 1 and 65535")
		return
	}

	// Validate base topic
	if config.Enabled && config.BaseTopic == "" {
		config.BaseTopic = "sentryusb"
	}

	// Save to preferences
	prefs := loadPreferences()
	if config.Enabled {
		prefs["mqtt_enabled"] = "true"
	} else {
		prefs["mqtt_enabled"] = "false"
	}
	prefs["mqtt_host"] = config.Host
	prefs["mqtt_port"] = fmt.Sprintf("%d", config.Port)
	prefs["mqtt_username"] = config.Username
	prefs["mqtt_password"] = config.Password
	prefs["mqtt_base_topic"] = config.BaseTopic

	if err := savePreferences(prefs); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save preferences: "+err.Error())
		return
	}

	// Reconnect MQTT with new config
	if config.Enabled {
		mgr := GetMQTTManager()
		if err := mgr.Init(config); err != nil {
			writeError(w, http.StatusBadRequest, "Failed to connect to MQTT broker: "+err.Error())
			return
		}
	} else {
		// Disconnect if disabling
		GetMQTTManager().Close()
	}

	writeOK(w)
}

// testMQTT tests the MQTT connection with provided credentials
func (h *handlers) testMQTT(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var config MQTTConfig
	if err := json.Unmarshal(body, &config); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Validate required fields
	if config.Host == "" {
		writeError(w, http.StatusBadRequest, "MQTT host is required")
		return
	}

	if config.Port < 1 || config.Port > 65535 {
		writeError(w, http.StatusBadRequest, "MQTT port must be between 1 and 65535")
		return
	}

	// Test the connection
	testMgr := &MQTTManager{}
	if err := testMgr.Init(config); err != nil {
		writeError(w, http.StatusBadRequest, "Connection failed: "+err.Error())
		return
	}

	// Clean up test connection
	testMgr.Close()

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "connected",
		"message": "Successfully connected to MQTT broker",
	})
}

// getMQTTStatus returns the current MQTT connection status
func (h *handlers) getMQTTStatus(w http.ResponseWriter, r *http.Request) {
	mgr := GetMQTTManager()
	config := mgr.config

	status := map[string]interface{}{
		"enabled":    config.Enabled,
		"connected":  mgr.IsConnected(),
		"host":       config.Host,
		"port":       config.Port,
		"base_topic": config.BaseTopic,
	}

	writeJSON(w, http.StatusOK, status)
}

// triggerArchiveFromMQTT triggers archiving (can be called from MQTT commands)
func (h *handlers) triggerArchiveFromMQTT(w http.ResponseWriter, r *http.Request) {
	log.Println("Archive trigger received via MQTT API endpoint")

	// Check if archiving is already running
	if IsArchiving() {
		writeError(w, http.StatusConflict, "Archiving is already in progress")
		return
	}

	// Get the archive trigger script
	if err := shell.Run("bash", "-c", "/root/bin/force_sync.sh"); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to trigger archive: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "archive_triggered",
		"message": "Archive process started",
	})
}

// Helper function to parse port
func parsePortInt(portStr string) int {
	port, _ := strconv.Atoi(portStr)
	return port
}

// Helper to check if port is valid
func validatePort(portStr string) bool {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}
	return port > 0 && port <= 65535
}
