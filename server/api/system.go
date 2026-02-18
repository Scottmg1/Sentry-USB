package api

import (
	"crypto/rand"
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
		output, err := shell.RunWithTimeout(120_000_000_000,
			"/root/bin/tesla-control", "-ble", "-vin", vin,
			"add-key-request", "/root/.ble/key_public.pem", "owner", "cloud_key")
		if err != nil {
			h.hub.Broadcast("ble_status", map[string]string{"status": "error", "error": err.Error()})
			return
		}
		h.hub.Broadcast("ble_status", map[string]string{"status": "waiting", "output": output})
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "pairing_started"})
}

func (h *handlers) bleStatus(w http.ResponseWriter, r *http.Request) {
	// Check if the BLE key exists
	if _, err := os.Stat("/root/.ble/key_public.pem"); err == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "paired"})
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_paired"})
	}
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
	type clipEntry struct {
		Date  string   `json:"date"`
		Path  string   `json:"path"`
		Files []string `json:"files"`
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
				group.Clips = append(group.Clips, clipEntry{
					Date:  dir.Name(),
					Path:  fmt.Sprintf("/TeslaCam/%s/%s", cat, dir.Name()),
					Files: fileNames,
				})
			}
		}

		groups = append(groups, group)
	}

	writeJSON(w, http.StatusOK, groups)
}
