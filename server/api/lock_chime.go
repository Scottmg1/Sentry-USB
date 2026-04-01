package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const lockChimeDir = "/mutable/LockChime"

// lockChimeTarget is the path Tesla reads the lock sound from (root of USB drive).
const lockChimeTarget = "/mutable/LockChime.wav"

// lockChimeMaxBytes is the max upload size (5 MB — well above any 7-second WAV).
const lockChimeMaxBytes = 5 * 1024 * 1024

// lockChimeMaxSeconds is Tesla's documented maximum lock sound duration.
const lockChimeMaxSeconds = 7.0

const lockChimeConfigFile = "/mutable/LockChime/.random_config.json"
const lockChimeActiveFile = "/mutable/LockChime/.active_name"

var validLockChimeFile = regexp.MustCompile(`^[a-zA-Z0-9 _.-]+\.wav$`)

// RandomConfig stores the random mode settings.
type lockChimeRandomConfig struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`     // "on_connect" or "scheduled"
	Interval string `json:"interval"` // "hourly", "daily", "weekly" (scheduled mode only)
}

var (
	lockChimeSchedulerOnce sync.Once
	lockChimeSchedulerStop chan struct{}
)

// sanitizeLockChimeName returns a safe filename (keeps alphanum, spaces, hyphens, underscores, dots).
func sanitizeLockChimeName(name string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9 _.-]`)
	result := safe.ReplaceAllString(name, "")
	result = strings.TrimSpace(result)
	if result == "" {
		result = "sound"
	}
	// Ensure .wav extension
	if !strings.HasSuffix(strings.ToLower(result), ".wav") {
		result += ".wav"
	}
	return result
}

// parseWAVDuration reads WAV header bytes and returns duration in seconds.
// Supports standard PCM WAVs. Returns an error if not a valid WAV.
func parseWAVDuration(data []byte) (float64, error) {
	if len(data) < 44 {
		return 0, fmt.Errorf("file too small to be a valid WAV")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a WAV file — must be .wav format")
	}

	// Walk chunks to find fmt and data
	pos := 12
	var byteRate uint32
	var fmtFound bool

	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := binary.LittleEndian.Uint32(data[pos+4 : pos+8])

		if chunkID == "fmt " {
			if int(pos)+8+int(chunkSize) > len(data) {
				return 0, fmt.Errorf("malformed WAV fmt chunk")
			}
			if chunkSize < 16 {
				return 0, fmt.Errorf("unsupported WAV format")
			}
			// fmt chunk layout (relative to chunk start):
			//   +0..3 = chunk ID "fmt "
			//   +4..7 = chunk size
			//   +8..9 = audio format
			//   +10..11 = num channels
			//   +12..15 = sample rate
			//   +16..19 = byte rate  (sampleRate * channels * bitsPerSample / 8)
			byteRate = binary.LittleEndian.Uint32(data[pos+16 : pos+20])
			fmtFound = true
		} else if chunkID == "data" && fmtFound && byteRate > 0 {
			return float64(chunkSize) / float64(byteRate), nil
		}

		pos += 8 + int(chunkSize)
		if chunkSize%2 != 0 {
			pos++ // WAV chunk padding byte
		}
	}

	if !fmtFound {
		return 0, fmt.Errorf("not a WAV file — must be .wav format")
	}
	return 0, fmt.Errorf("could not determine WAV duration")
}

// ──────────────────────────────────────────────────────────────────
// Random config helpers
// ──────────────────────────────────────────────────────────────────

func loadLockChimeRandomConfig() lockChimeRandomConfig {
	var cfg lockChimeRandomConfig
	data, err := os.ReadFile(lockChimeConfigFile)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

func saveLockChimeRandomConfig(cfg lockChimeRandomConfig) error {
	// Validate values
	if cfg.Mode != "on_connect" && cfg.Mode != "scheduled" && cfg.Mode != "" {
		return fmt.Errorf("invalid mode: must be on_connect or scheduled")
	}
	if cfg.Mode == "scheduled" && cfg.Interval != "hourly" && cfg.Interval != "daily" && cfg.Interval != "weekly" {
		return fmt.Errorf("invalid interval: must be hourly, daily, or weekly")
	}
	os.MkdirAll(lockChimeDir, 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockChimeConfigFile, data, 0644)
}

// listWavFiles returns .wav filenames in the LockChime library.
func listWavFiles() []string {
	entries, err := os.ReadDir(lockChimeDir)
	if err != nil {
		return nil
	}
	var wavs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".wav") {
			wavs = append(wavs, name)
		}
	}
	return wavs
}

// pickAndActivateRandom selects a random .wav from the library and copies it
// to the Tesla target path. Returns the chosen filename or empty string.
func pickAndActivateRandom() string {
	wavs := listWavFiles()
	if len(wavs) == 0 {
		return ""
	}
	chosen := wavs[rand.Intn(len(wavs))]
	srcPath := filepath.Join(lockChimeDir, chosen)

	data, err := os.ReadFile(srcPath)
	if err != nil {
		log.Printf("lockchime: failed to read %s: %v", chosen, err)
		return ""
	}
	os.Remove(lockChimeTarget)
	if err := os.WriteFile(lockChimeTarget, data, 0644); err != nil {
		log.Printf("lockchime: failed to write target: %v", err)
		return ""
	}
	os.WriteFile(lockChimeActiveFile, []byte(chosen), 0644)
	log.Printf("lockchime: random mode activated %q", chosen)
	return chosen
}

// RandomizeOnConnect is called when the USB gadget is enabled (drive mounted).
// It randomizes the lock sound if random mode is enabled with on_connect mode.
func RandomizeOnConnect() {
	cfg := loadLockChimeRandomConfig()
	if !cfg.Enabled || cfg.Mode != "on_connect" {
		return
	}
	pickAndActivateRandom()
}

// StartLockChimeScheduler starts the background goroutine for scheduled
// random mode. Safe to call multiple times — only starts once.
func StartLockChimeScheduler() {
	lockChimeSchedulerOnce.Do(func() {
		lockChimeSchedulerStop = make(chan struct{})
		go lockChimeSchedulerLoop(lockChimeSchedulerStop)
	})
}

func lockChimeSchedulerLoop(stop chan struct{}) {
	// Check every minute; only act when the interval has elapsed.
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	var lastRun time.Time

	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			cfg := loadLockChimeRandomConfig()
			if !cfg.Enabled || cfg.Mode != "scheduled" {
				lastRun = time.Time{} // reset so next enable fires immediately
				continue
			}

			var interval time.Duration
			switch cfg.Interval {
			case "hourly":
				interval = 1 * time.Hour
			case "daily":
				interval = 24 * time.Hour
			case "weekly":
				interval = 7 * 24 * time.Hour
			default:
				continue
			}

			if lastRun.IsZero() || now.Sub(lastRun) >= interval {
				pickAndActivateRandom()
				lastRun = now
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────
// HTTP Handlers
// ──────────────────────────────────────────────────────────────────

// GET /api/lockchime/list — list .wav files in the LockChime library
func (h *handlers) lockChimeList(w http.ResponseWriter, r *http.Request) {
	type soundEntry struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Active bool   `json:"active"`
	}

	if err := os.MkdirAll(lockChimeDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not access LockChime directory")
		return
	}

	entries, err := os.ReadDir(lockChimeDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list sounds")
		return
	}

	// Determine which file is currently active from sidecar metadata
	activeName := ""
	if data, err := os.ReadFile(lockChimeActiveFile); err == nil {
		activeName = strings.TrimSpace(string(data))
	}

	sounds := make([]soundEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".wav") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		sounds = append(sounds, soundEntry{
			Name:   name,
			Size:   info.Size(),
			Active: name == activeName,
		})
	}

	// Check if target exists (a sound is active)
	_, targetErr := os.Stat(lockChimeTarget)
	activeExists := targetErr == nil

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sounds":      sounds,
		"active_name": activeName,
		"active_set":  activeExists,
	})
}

// POST /api/lockchime/upload — upload a .wav file to the LockChime library
func (h *handlers) lockChimeUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, lockChimeMaxBytes)

	if err := r.ParseMultipartForm(lockChimeMaxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "Upload too large (max 5 MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing file field")
		return
	}
	defer file.Close()

	// Validate extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".wav") {
		writeError(w, http.StatusBadRequest, "Only .wav files are supported")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read file")
		return
	}

	// Validate WAV format and duration
	duration, err := parseWAVDuration(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if duration > lockChimeMaxSeconds {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Sound is %.1f seconds — Tesla requires 7 seconds or less", duration))
		return
	}

	if err := os.MkdirAll(lockChimeDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create LockChime directory")
		return
	}

	// Sanitize filename and reject reserved "lockchime" name
	baseName := sanitizeLockChimeName(header.Filename)
	if strings.EqualFold(strings.TrimSuffix(baseName, filepath.Ext(baseName)), "lockchime") {
		writeError(w, http.StatusBadRequest, "File cannot be named \"lockchime\" — please rename it before uploading")
		return
	}
	destPath := filepath.Join(lockChimeDir, baseName)
	if _, err := os.Stat(destPath); err == nil {
		ext := filepath.Ext(baseName)
		stem := strings.TrimSuffix(baseName, ext)
		found := false
		for i := 1; i <= 100; i++ {
			candidate := filepath.Join(lockChimeDir, fmt.Sprintf("%s_%d%s", stem, i, ext))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				destPath = candidate
				baseName = filepath.Base(candidate)
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusConflict, "Too many files with the same name")
			return
		}
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"name":     baseName,
		"duration": duration,
		"size":     len(data),
	})
}

// POST /api/lockchime/activate/{filename} — copy selected sound to Tesla's expected location
func (h *handlers) lockChimeActivate(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !validLockChimeFile.MatchString(filename) {
		writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}

	srcPath := filepath.Join(lockChimeDir, filename)

	// Validate source is inside lockChimeDir (no traversal)
	cleanSrc := filepath.Clean(srcPath)
	if !strings.HasPrefix(cleanSrc, lockChimeDir+"/") {
		writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}

	if _, err := os.Stat(cleanSrc); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "Sound file not found")
		return
	}

	// Read source
	data, err := os.ReadFile(cleanSrc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read source file")
		return
	}

	// Remove existing target (file or symlink)
	os.Remove(lockChimeTarget)

	// Write to target
	if err := os.WriteFile(lockChimeTarget, data, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to activate lock sound")
		return
	}

	// Record the active filename in sidecar metadata
	os.WriteFile(lockChimeActiveFile, []byte(filename), 0644)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"active":  filename,
	})
}

// POST /api/lockchime/clear-active — remove active lock sound from Tesla location
func (h *handlers) lockChimeClear(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(lockChimeTarget); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "Failed to clear active sound")
		return
	}
	os.Remove(lockChimeActiveFile)
	writeOK(w)
}

// DELETE /api/lockchime/{filename} — delete a sound from the library
func (h *handlers) lockChimeDelete(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !validLockChimeFile.MatchString(filename) {
		writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}

	destPath := filepath.Join(lockChimeDir, filename)
	cleanPath := filepath.Clean(destPath)
	if !strings.HasPrefix(cleanPath, lockChimeDir+"/") {
		writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}

	if err := os.Remove(cleanPath); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "File not found")
		} else {
			writeError(w, http.StatusInternalServerError, "Failed to delete file")
		}
		return
	}

	writeOK(w)
}

// ──────────────────────────────────────────────────────────────────
// Random mode endpoints
// ──────────────────────────────────────────────────────────────────

// GET /api/lockchime/random-config — get random mode settings
func (h *handlers) lockChimeGetRandomConfig(w http.ResponseWriter, r *http.Request) {
	cfg := loadLockChimeRandomConfig()

	// Also return RTC status so the frontend knows which options to show
	rtcInfo := getRTCInfo()
	hasRTC := rtcInfo.RTCPresent && rtcInfo.RTCHealthy

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":  cfg.Enabled,
		"mode":     cfg.Mode,
		"interval": cfg.Interval,
		"has_rtc":  hasRTC,
	})
}

// PUT /api/lockchime/random-config — update random mode settings
func (h *handlers) lockChimeSaveRandomConfig(w http.ResponseWriter, r *http.Request) {
	var req lockChimeRandomConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// If scheduled mode, verify RTC is available
	if req.Enabled && req.Mode == "scheduled" {
		rtcInfo := getRTCInfo()
		if !rtcInfo.RTCPresent || !rtcInfo.RTCHealthy {
			writeError(w, http.StatusBadRequest, "Scheduled mode requires a working RTC (Pi 5 with battery)")
			return
		}
	}

	if err := saveLockChimeRandomConfig(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"enabled":  req.Enabled,
		"mode":     req.Mode,
		"interval": req.Interval,
	})
}

// POST /api/lockchime/randomize — manually trigger a random selection
func (h *handlers) lockChimeRandomize(w http.ResponseWriter, r *http.Request) {
	chosen := pickAndActivateRandom()
	if chosen == "" {
		writeError(w, http.StatusBadRequest, "No sounds in library to randomize")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"active":  chosen,
	})
}

// ──────────────────────────────────────────────────────────────────
// Community lock chime endpoints (proxy to support server)
// ──────────────────────────────────────────────────────────────────

var validLockChimeCode = regexp.MustCompile(`^[A-Za-z0-9]{3,10}$`)

// GET /api/lockchime/community/library — proxy browse request to support server
func (h *handlers) communityLockChimeLibrary(w http.ResponseWriter, r *http.Request) {
	query := r.URL.RawQuery
	path := "/lockchime/library"
	if query != "" {
		path += "?" + query
	}

	var headers map[string]string
	if passcode := r.Header.Get("X-Passcode"); passcode != "" {
		headers = map[string]string{"X-Passcode": passcode}
	}

	var respBody []byte
	var status int
	var err error
	if headers != nil {
		respBody, status, err = supportProxyWithHeaders("GET", path, nil, headers, 15*time.Second)
	} else {
		respBody, status, err = supportProxy("GET", path, nil, "", 15*time.Second)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community lock chime service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// GET /api/lockchime/community/stream/{code} — proxy sound stream from support server for in-browser preview
func (h *handlers) communityLockChimeStream(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validLockChimeCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(supportServerURL + "/lockchime/download/" + code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to fetch sound")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.Copy(w, resp.Body)
}

// POST /api/lockchime/community/upload — proxy sound upload to support server with fingerprint
func (h *handlers) communityLockChimeUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, lockChimeMaxBytes)

	if err := r.ParseMultipartForm(lockChimeMaxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "Failed to parse upload")
		return
	}

	file, header, err := r.FormFile("sound")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing sound file")
		return
	}
	defer file.Close()

	name := r.FormValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "Missing name")
		return
	}

	fileData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read file")
		return
	}

	// Build multipart request to support server
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("sound", header.Filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create multipart")
		return
	}
	part.Write(fileData)

	writer.WriteField("name", name)
	writer.Close()

	req, err := http.NewRequest("POST", supportServerURL+"/lockchime/upload", &buf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Fingerprint", getFingerprint())

	// Forward passcode if present (admin bypasses rate limiting)
	if passcode := r.Header.Get("X-Passcode"); passcode != "" {
		req.Header.Set("X-Passcode", passcode)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community lock chime service unreachable")
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// POST /api/lockchime/community/download/{code} — download sound from support server, save to Pi
func (h *handlers) communityLockChimeDownload(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validLockChimeCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	req, err := http.NewRequest("GET", supportServerURL+"/lockchime/download/"+code, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("X-Fingerprint", getFingerprint())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community lock chime service unreachable")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, lockChimeMaxBytes+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to download sound")
		return
	}
	if len(data) > lockChimeMaxBytes {
		writeError(w, http.StatusBadRequest, "Downloaded sound exceeds 5 MB size limit")
		return
	}

	// Validate WAV format and duration
	duration, err := parseWAVDuration(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Downloaded file is not a valid WAV: "+err.Error())
		return
	}
	if duration > lockChimeMaxSeconds {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Sound is %.1f seconds — Tesla requires 7 seconds or less", duration))
		return
	}

	// Determine filename from header or code
	soundName := resp.Header.Get("X-Sound-Name")
	if soundName == "" {
		soundName = code
	}
	soundName = sanitizeLockChimeName(soundName)
	// Rename reserved "lockchime" name to avoid collision with Tesla target
	if strings.EqualFold(strings.TrimSuffix(soundName, filepath.Ext(soundName)), "lockchime") {
		soundName = code + ".wav"
	}

	if err := os.MkdirAll(lockChimeDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to prepare lock chime directory")
		return
	}

	destPath := filepath.Join(lockChimeDir, soundName)
	if _, err := os.Stat(destPath); err == nil {
		ext := filepath.Ext(soundName)
		stem := strings.TrimSuffix(soundName, ext)
		found := false
		for i := 1; i <= 100; i++ {
			candidate := filepath.Join(lockChimeDir, fmt.Sprintf("%s_%d%s", stem, i, ext))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				destPath = candidate
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusConflict, "Too many files with the same name")
			return
		}
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save sound")
		return
	}

	log.Printf("[LOCKCHIME] Community sound saved: %s (%d bytes)", filepath.Base(destPath), len(data))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"filename": filepath.Base(destPath),
		"size":     len(data),
	})
}

// POST /api/lockchime/community/admin/validate — proxy admin passcode validation
func (h *handlers) communityLockChimeAdminValidate(w http.ResponseWriter, r *http.Request) {
	passcode := r.Header.Get("X-Passcode")
	if passcode == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	headers := map[string]string{"X-Passcode": passcode}
	respBody, status, err := supportProxyWithHeaders("POST", "/lockchime/admin/validate", nil, headers, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community lock chime service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// PUT /api/lockchime/community/admin/edit/{code} — proxy admin edit
func (h *handlers) communityLockChimeAdminEdit(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validLockChimeCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	passcode := r.Header.Get("X-Passcode")
	if passcode == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	headers := map[string]string{"X-Passcode": passcode}
	respBody, status, err := supportProxyWithHeaders("PUT", "/lockchime/admin/edit/"+code, body, headers, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community lock chime service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// DELETE /api/lockchime/community/admin/delete/{code} — proxy admin deletion
func (h *handlers) communityLockChimeAdminDelete(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validLockChimeCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	passcode := r.Header.Get("X-Passcode")
	if passcode == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	headers := map[string]string{"X-Passcode": passcode}
	respBody, status, err := supportProxyWithHeaders("DELETE", "/lockchime/admin/delete/"+code, nil, headers, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community lock chime service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}
