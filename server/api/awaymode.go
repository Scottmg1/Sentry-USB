package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const awayModeFlagFile = "/mutable/sentryusb_away_mode.json"

// awayModeFlagData is the JSON structure persisted to the flag file.
// Pis with an RTC use expires_at (absolute timestamp).
// Pis without an RTC use remaining_sec (countdown), updated every 30s.
type awayModeFlagData struct {
	ExpiresAt    string `json:"expires_at"`
	EnabledAt    string `json:"enabled_at"`
	RemainingSec int    `json:"remaining_sec"`
	HasRTC       bool   `json:"has_rtc"`
}

// AwayModeManager controls Away Mode, which gives the AP exclusive radio
// access for a user-specified duration.  When active, the AP is started and
// wifi_cycle / rescan are suppressed in archiveloop via a flag file.
type AwayModeManager struct {
	mu        sync.RWMutex
	state     string // "idle" or "active"
	expiresAt time.Time
	enabledAt time.Time
	hasRTC    bool // true if /dev/rtc0 exists (Pi 5)
	stopCh    chan struct{}
}

// NewAwayModeManager creates a new manager in idle state.
// It probes for an RTC to decide the timekeeping strategy.
func NewAwayModeManager() *AwayModeManager {
	hasRTC := false
	if _, err := os.Stat("/dev/rtc0"); err == nil {
		hasRTC = true
	}
	if hasRTC {
		log.Printf("[away-mode] RTC detected — using timestamp-based expiration")
	} else {
		log.Printf("[away-mode] No RTC — using countdown-based expiration")
	}
	return &AwayModeManager{state: "idle", hasRTC: hasRTC}
}

// awayModeLog writes to the archiveloop log with [away-mode] prefix.
func awayModeLog(format string, args ...interface{}) {
	const logPath = "/mutable/archiveloop.log"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s: [away-mode] %s\n", time.Now().Format("Mon Jan _2 15:04:05 MST 2006"), msg)
}

// Enable activates Away Mode for the given duration.  It starts the AP and
// writes the flag file so archiveloop skips wifi_cycle.
func (m *AwayModeManager) Enable(duration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already active, just update the expiration
	if m.state == "active" {
		m.expiresAt = time.Now().Add(duration)
		m.writeFlagFile()
		awayModeLog("Extended to %s (duration: %s)", m.expiresAt.Format(time.RFC3339), duration)
		return nil
	}

	// Stop any previous goroutine
	if m.stopCh != nil {
		close(m.stopCh)
	}

	m.state = "active"
	m.enabledAt = time.Now()
	m.expiresAt = time.Now().Add(duration)
	m.stopCh = make(chan struct{})

	m.writeFlagFile()

	awayModeLog("Enabled (duration: %s, expires: %s, rtc: %v)", duration, m.expiresAt.Format(time.RFC3339), m.hasRTC)
	log.Printf("[away-mode] Enabled (duration: %s, rtc: %v)", duration, m.hasRTC)

	go m.startAP()
	go m.expirationWatcher()

	return nil
}

// Disable stops Away Mode, tears down the AP, and removes the flag file.
func (m *AwayModeManager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == "idle" {
		return
	}

	m.state = "idle"
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
	m.expiresAt = time.Time{}
	m.enabledAt = time.Time{}

	m.removeFlagFile()

	awayModeLog("Disabled by user")
	log.Printf("[away-mode] Disabled")

	go m.stopAP()
}

// Status returns the current Away Mode status as a JSON-friendly map.
func (m *AwayModeManager) Status() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := map[string]interface{}{
		"state":   m.state,
		"has_rtc": m.hasRTC,
	}

	// Always include AP info so the UI can show it in warnings/confirmations
	if ssid, ip := getAPInfo(); ssid != "" {
		result["ap_ssid"] = ssid
		result["ap_ip"] = ip
	}

	if m.state == "active" {
		remaining := time.Until(m.expiresAt)
		if remaining < 0 {
			remaining = 0
		}
		result["expires_at"] = m.expiresAt.Format(time.RFC3339)
		result["remaining_sec"] = int(remaining.Seconds())
		result["enabled_at"] = m.enabledAt.Format(time.RFC3339)
	}

	return result
}

// RestoreFromFile checks for an existing flag file on startup and resumes
// Away Mode if time remains.
//
// Strategy depends on RTC presence:
//   - With RTC: trust expires_at timestamp (clock survives reboot)
//   - Without RTC: use remaining_sec from the flag file (last persisted
//     countdown value, updated every 30s by the watcher)
func (m *AwayModeManager) RestoreFromFile() {
	data, err := os.ReadFile(awayModeFlagFile)
	if err != nil {
		return // no flag file
	}

	var flag awayModeFlagData
	if err := json.Unmarshal(data, &flag); err != nil {
		log.Printf("[away-mode] Invalid flag file, removing: %v", err)
		os.Remove(awayModeFlagFile)
		return
	}

	var remaining time.Duration

	if m.hasRTC {
		// RTC present: trust the absolute timestamp
		expiresAt, err := time.Parse(time.RFC3339, flag.ExpiresAt)
		if err != nil {
			log.Printf("[away-mode] Invalid expires_at in flag file, removing: %v", err)
			os.Remove(awayModeFlagFile)
			return
		}
		remaining = time.Until(expiresAt)
	} else {
		// No RTC: use the persisted countdown (last written by the watcher)
		remaining = time.Duration(flag.RemainingSec) * time.Second
	}

	if remaining <= 0 {
		log.Printf("[away-mode] Flag file expired, cleaning up")
		os.Remove(awayModeFlagFile)
		go m.stopAP()
		return
	}

	enabledAt, _ := time.Parse(time.RFC3339, flag.EnabledAt)

	m.mu.Lock()
	m.state = "active"
	m.enabledAt = enabledAt
	m.expiresAt = time.Now().Add(remaining)
	m.stopCh = make(chan struct{})
	m.mu.Unlock()

	awayModeLog("Restored from flag file (%s remaining, rtc: %v)", remaining.Round(time.Second), m.hasRTC)
	log.Printf("[away-mode] Restored from flag file (%s remaining, rtc: %v)", remaining.Round(time.Second), m.hasRTC)

	go m.startAP()
	go m.expirationWatcher()
}

// expirationWatcher monitors the timer and auto-disables when expired.
// Every 30s it also persists remaining_sec to the flag file so that Pis
// without an RTC can restore accurately after a reboot.
func (m *AwayModeManager) expirationWatcher() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.mu.Lock()
			if m.state != "active" {
				m.mu.Unlock()
				return
			}

			expired := time.Now().After(m.expiresAt)
			if expired {
				m.state = "idle"
				m.expiresAt = time.Time{}
				m.enabledAt = time.Time{}
				m.removeFlagFile()
				if m.stopCh != nil {
					close(m.stopCh)
					m.stopCh = nil
				}
				m.mu.Unlock()

				awayModeLog("Timer expired, disabling")
				log.Printf("[away-mode] Timer expired")
				go m.stopAP()
				return
			}

			// Persist remaining_sec for no-RTC recovery
			m.writeFlagFile()
			m.mu.Unlock()
		}
	}
}

// writeFlagFile writes the away mode state to the flag file atomically.
// Includes both expires_at (for RTC Pis) and remaining_sec (for non-RTC Pis).
func (m *AwayModeManager) writeFlagFile() {
	remaining := time.Until(m.expiresAt)
	if remaining < 0 {
		remaining = 0
	}

	flag := awayModeFlagData{
		ExpiresAt:    m.expiresAt.Format(time.RFC3339),
		EnabledAt:    m.enabledAt.Format(time.RFC3339),
		RemainingSec: int(remaining.Seconds()),
		HasRTC:       m.hasRTC,
	}

	data, err := json.Marshal(flag)
	if err != nil {
		log.Printf("[away-mode] Failed to marshal flag file: %v", err)
		return
	}

	tmpFile := awayModeFlagFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		log.Printf("[away-mode] Failed to write flag file: %v", err)
		return
	}
	if err := os.Rename(tmpFile, awayModeFlagFile); err != nil {
		log.Printf("[away-mode] Failed to rename flag file: %v", err)
	}
}

// removeFlagFile removes the away mode flag file.
func (m *AwayModeManager) removeFlagFile() {
	os.Remove(awayModeFlagFile)
	os.Remove(awayModeFlagFile + ".tmp")
}

// getAPInfo returns the configured AP SSID and IP address from the SENTRYUSB_AP profile.
func getAPInfo() (ssid, ip string) {
	out, err := exec.Command("nmcli", "-t", "-f", "802-11-wireless.ssid,ipv4.addresses", "con", "show", "SENTRYUSB_AP").Output()
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "802-11-wireless.ssid:") {
			ssid = strings.TrimPrefix(line, "802-11-wireless.ssid:")
		}
		if strings.HasPrefix(line, "ipv4.addresses:") {
			ip = strings.TrimPrefix(line, "ipv4.addresses:")
			// Strip CIDR suffix (e.g. "192.168.66.1/24" → "192.168.66.1")
			if idx := strings.Index(ip, "/"); idx > 0 {
				ip = ip[:idx]
			}
		}
	}
	return ssid, ip
}

// getWifiClientDevice returns the wifi client device name (e.g. "wlan0").
func getWifiClientDevice() string {
	out, err := exec.Command("nmcli", "-t", "-f", "TYPE,DEVICE", "c", "show", "--active").Output()
	if err != nil {
		return "wlan0" // sensible default
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "802-11-wireless:") && !strings.HasSuffix(line, ":ap0") {
			return strings.TrimPrefix(line, "802-11-wireless:")
		}
	}
	return "wlan0"
}

// startAP creates the ap0 virtual interface and brings up the SENTRYUSB_AP profile.
func (m *AwayModeManager) startAP() {
	wlan := getWifiClientDevice()

	// Create ap0 if it doesn't exist
	exec.Command("iw", "dev", wlan, "interface", "add", "ap0", "type", "__ap").Run()
	exec.Command("iw", wlan, "set", "power_save", "off").Run()
	exec.Command("iw", "ap0", "set", "power_save", "off").Run()

	// Bring up the pre-configured AP profile
	if out, err := exec.Command("nmcli", "con", "up", "SENTRYUSB_AP").CombinedOutput(); err != nil {
		awayModeLog("Failed to bring up AP: %s (%v)", strings.TrimSpace(string(out)), err)
		log.Printf("[away-mode] Failed to bring up AP: %v", err)
		return
	}

	awayModeLog("AP started on %s (ap0)", wlan)
	log.Printf("[away-mode] AP started")
}

// stopAP tears down the SENTRYUSB_AP connection and removes the ap0 interface.
func (m *AwayModeManager) stopAP() {
	exec.Command("nmcli", "con", "down", "SENTRYUSB_AP").Run()
	exec.Command("iw", "dev", "ap0", "del").Run()

	awayModeLog("AP stopped")
	log.Printf("[away-mode] AP stopped")
}

// RegisterAwayModeRoutes registers HTTP handlers for the Away Mode API.
func RegisterAwayModeRoutes(mux *http.ServeMux, awm *AwayModeManager) {
	mux.HandleFunc("POST /api/away-mode/enable", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DurationMin int `json:"duration_min"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if body.DurationMin <= 0 {
			writeError(w, http.StatusBadRequest, "duration_min must be positive")
			return
		}
		if body.DurationMin > 1440 {
			writeError(w, http.StatusBadRequest, "duration_min cannot exceed 24 hours (1440)")
			return
		}

		// Check that the SENTRYUSB_AP profile exists
		if out, err := exec.Command("nmcli", "-t", "con", "show", "SENTRYUSB_AP").Output(); err != nil || len(out) == 0 {
			writeError(w, http.StatusPreconditionFailed, "AP not configured. Run setup with AP settings first.")
			return
		}

		duration := time.Duration(body.DurationMin) * time.Minute
		if err := awm.Enable(duration); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, awm.Status())
	})

	mux.HandleFunc("DELETE /api/away-mode", func(w http.ResponseWriter, r *http.Request) {
		awm.Disable()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/away-mode/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, awm.Status())
	})
}
