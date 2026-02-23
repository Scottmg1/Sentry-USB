package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func (h *handlers) reboot(w http.ResponseWriter, r *http.Request) {
	go func() {
		shell.Run("reboot")
	}()
	writeOK(w)
}

func (h *handlers) toggleDrives(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat("/sys/kernel/config/usb_gadget/sentryusb"); err == nil {
		shell.Run("bash", "/root/bin/disable_gadget.sh")
	} else {
		shell.Run("bash", "/root/bin/enable_gadget.sh")
	}
	writeOK(w)
}

func (h *handlers) triggerSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		shell.Run("bash", "/root/bin/force_sync.sh")
	}()
	writeOK(w)
}

func (h *handlers) blePair(w http.ResponseWriter, r *http.Request) {
	// Read VIN from config
	configPath := findConfigFilePath()
	if configPath == "" {
		writeError(w, http.StatusInternalServerError, "Config file not found")
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Cannot read config")
		return
	}

	var vin string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export TESLA_BLE_VIN=") {
			vin = strings.TrimPrefix(line, "export TESLA_BLE_VIN=")
			vin = strings.Trim(vin, "'\"")
			break
		}
	}

	if vin == "" {
		writeError(w, http.StatusBadRequest, "TESLA_BLE_VIN not configured")
		return
	}

	go func() {
		h.hub.Broadcast("ble_status", map[string]string{"status": "pairing"})
		vin = strings.ToUpper(vin)

		// Stop bluetoothd so tesla-control can get exclusive access to hci0.
		// Without this, tesla-control fails with "can't down device: device or resource busy".
		shell.Run("systemctl", "stop", "bluetooth")
		defer shell.Run("systemctl", "start", "bluetooth")

		output, err := shell.RunWithTimeout(120*time.Second,
			"/root/bin/tesla-control", "-ble", "-vin", vin,
			"add-key-request", "/root/.ble/key_public.pem", "owner", "cloud_key")
		if err != nil {
			errMsg := err.Error()
			// Strip verbose prefix so the UI shows only the meaningful part
			if idx := strings.Index(errMsg, "stderr: "); idx >= 0 {
				errMsg = errMsg[idx+len("stderr: "):]
			}
			h.hub.Broadcast("ble_status", map[string]string{"status": "error", "error": errMsg})
			return
		}
		h.hub.Broadcast("ble_status", map[string]string{"status": "waiting", "output": output})
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "pairing_started"})
}

func (h *handlers) bleStatus(w http.ResponseWriter, r *http.Request) {
	// Check if the BLE key files exist
	if _, err := os.Stat("/root/.ble/key_public.pem"); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_paired"})
		return
	}
	if _, err := os.Stat("/root/.ble/key_private.pem"); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_paired"})
		return
	}

	vin := readBLEVin()
	if vin == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "keys_generated"})
		return
	}

	// ?quick=true skips the slow BLE session-info probe (up to 15s).
	// Used on page load to avoid blocking the UI.
	if r.URL.Query().Get("quick") == "true" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "paired"})
		return
	}

	// Full verification: attempt to get session info from the vehicle
	_, err := shell.RunWithTimeout(15*time.Second,
		"/root/bin/tesla-control", "-ble", "-vin", strings.ToUpper(vin),
		"session-info", "/root/.ble/key_private.pem", "infotainment")
	if err != nil {
		// Keys exist but pairing not confirmed — could be out of range or not paired
		writeJSON(w, http.StatusOK, map[string]string{"status": "keys_generated", "note": "Car not reachable or key not paired"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "paired"})
}

// readBLEVin reads the TESLA_BLE_VIN from the config file.
func readBLEVin() string {
	configPath := findConfigFilePath()
	if configPath == "" {
		return ""
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export TESLA_BLE_VIN=") {
			vin := strings.TrimPrefix(line, "export TESLA_BLE_VIN=")
			vin = strings.Trim(vin, "'\"")
			return vin
		}
	}
	return ""
}

func (h *handlers) refreshDiagnostics(w http.ResponseWriter, r *http.Request) {
	// Run setup-sentryusb diagnose, which writes to /tmp/diagnostics.txt
	_, err := shell.RunWithTimeout(60*time.Second, "bash", "-c", "(sudo /root/bin/setup-sentryusb diagnose) &> /tmp/diagnostics.txt")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate diagnostics: "+err.Error())
		return
	}
	writeOK(w)
}

func (h *handlers) getDiagnostics(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("/tmp/diagnostics.txt")
	if err != nil {
		// File doesn't exist yet — not an error, just not generated
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("Diagnostics have not been generated yet.\nClick the Refresh button above to generate a diagnostics report."))
		return
	}
	// Sanitize: ensure valid UTF-8, strip ANSI escape codes and control chars
	// to prevent issues when diagnostics are forwarded to PostgreSQL JSONB
	cleaned := sanitizeDiagnostics(data)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(cleaned))
}

// sanitizeDiagnostics cleans raw shell output for safe JSON/JSONB transport
func sanitizeDiagnostics(raw []byte) string {
	// Ensure valid UTF-8 — replace invalid sequences
	s := string(raw)
	if !utf8.ValidString(s) {
		var b strings.Builder
		for i := 0; i < len(s); {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				i++
				continue
			}
			b.WriteRune(r)
			i += size
		}
		s = b.String()
	}
	// Strip ANSI escape codes
	s = ansiRegex.ReplaceAllString(s, "")
	// Remove control chars except \t \n \r
	var out strings.Builder
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (h *handlers) speedtest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-cache")

	flusher, ok := w.(http.Flusher)
	buf := make([]byte, 65536)

	for i := 0; i < 1000; i++ {
		rand.Read(buf)
		if _, err := w.Write(buf); err != nil {
			return
		}
		if ok {
			flusher.Flush()
		}
	}
}

func (h *handlers) getClips(w http.ResponseWriter, r *http.Request) {
	categories := []string{"RecentClips", "SavedClips", "SentryClips"}

	type eventMeta struct {
		Timestamp string `json:"timestamp,omitempty"`
		City      string `json:"city,omitempty"`
		Reason    string `json:"reason,omitempty"`
		Camera    string `json:"camera,omitempty"`
		Latitude  string `json:"latitude,omitempty"`
		Longitude string `json:"longitude,omitempty"`
	}

	type clipEntry struct {
		Date  string     `json:"date"`
		Path  string     `json:"path"`
		Files []string   `json:"files"`
		Event *eventMeta `json:"event,omitempty"`
	}
	type clipGroup struct {
		Name  string      `json:"name"`
		Clips []clipEntry `json:"clips"`
	}

	var groups []clipGroup

	for _, cat := range categories {
		basePath := filepath.Join("/mutable/TeslaCam", cat)
		group := clipGroup{Name: cat, Clips: []clipEntry{}}

		dirs, err := os.ReadDir(basePath)
		if err != nil {
			groups = append(groups, group)
			continue
		}

		sort.Slice(dirs, func(i, j int) bool {
			return dirs[i].Name() > dirs[j].Name()
		})

		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			clipPath := filepath.Join(basePath, dir.Name())
			files, err := os.ReadDir(clipPath)
			if err != nil {
				continue
			}

			var fileNames []string
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".mp4") {
					fileNames = append(fileNames, f.Name())
				}
			}

			if len(fileNames) > 0 {
				sort.Strings(fileNames)
				entry := clipEntry{
					Date:  dir.Name(),
					Path:  fmt.Sprintf("/TeslaCam/%s/%s", cat, dir.Name()),
					Files: fileNames,
				}

				// Try to read event.json for metadata
				eventPath := filepath.Join(clipPath, "event.json")
				if data, err := os.ReadFile(eventPath); err == nil {
					var raw map[string]interface{}
					if json.Unmarshal(data, &raw) == nil {
						meta := &eventMeta{}
						if v, ok := raw["timestamp"].(string); ok {
							meta.Timestamp = v
						}
						if v, ok := raw["city"].(string); ok {
							meta.City = v
						}
						if v, ok := raw["reason"].(string); ok {
							meta.Reason = v
						}
						if v, ok := raw["camera"].(string); ok {
							meta.Camera = v
						}
						if v, ok := raw["est_lat"].(string); ok {
							meta.Latitude = v
						}
						if v, ok := raw["est_lon"].(string); ok {
							meta.Longitude = v
						}
						entry.Event = meta
					}
				}

				group.Clips = append(group.Clips, entry)
			}
		}

		groups = append(groups, group)
	}

	writeJSON(w, http.StatusOK, groups)
}
