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
	"syscall"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/shell"
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

// lockChimeLog writes to the archiveloop log with [lock-chime] prefix.
func lockChimeLog(format string, args ...interface{}) {
	const logPath = "/mutable/archiveloop.log"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s: [lock-chime] %s\n", time.Now().Format("Mon 02 Jan 15:04:05 MST 2006"), msg)
}

// writeChimeFileAtomic writes data to destPath using the same atomic pattern as
// the old TeslaUSB codebase: write to temp → fsync file → rename → fsync dir →
// touch timestamps → system sync.  This ensures Tesla reads the new content
// instead of a stale cached version.
func writeChimeFileAtomic(destPath string, data []byte) error {
	dir := filepath.Dir(destPath)
	tmpPath := filepath.Join(dir, "."+filepath.Base(destPath)+".tmp")

	// 1. Write to temp file
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}

	// 2. Fsync the temp file to flush to disk
	f, err := os.Open(tmpPath)
	if err == nil {
		_ = f.Sync()
		f.Close()
	}

	// 3. Remove old target so rename is clean
	os.Remove(destPath)

	// 4. Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath) // cleanup on failure
		return fmt.Errorf("rename: %w", err)
	}

	// 5. Fsync the directory to make the rename durable
	d, err := os.Open(dir)
	if err == nil {
		_ = d.Sync()
		d.Close()
	}

	// 6. Touch timestamps to help Tesla detect the change even if size is identical
	now := time.Now()
	_ = os.Chtimes(destPath, now, now)

	// 7. Full system sync for exFAT / backing-file durability
	syscall.Sync()

	return nil
}

// camDiskImage is the backing file for the cam USB drive that Tesla reads.
const camDiskImage = "/backingfiles/cam_disk.bin"
const camMountPoint = "/mnt/cam"

// gadgetConfigDir is the configfs path for the SentryUSB gadget.
const gadgetConfigDir = "/sys/kernel/config/usb_gadget/sentryusb"

// camDiskMu serializes all operations that disable/enable the USB gadget and
// mount/unmount the cam disk to prevent races from concurrent goroutines.
var camDiskMu sync.Mutex

// isGadgetActive returns true if the USB gadget is currently configured.
func isGadgetActive() bool {
	_, err := os.Stat(gadgetConfigDir)
	return err == nil
}

// isMountPointActive checks /proc/mounts to see if a mount point is currently active.
func isMountPointActive(mountPoint string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == mountPoint {
			return true
		}
	}
	return false
}

// copyLockChimeToCamMount copies /mutable/LockChime.wav to the root of the cam
// disk so Tesla can read it via USB mass storage.  The cam disk must NOT be in
// use by the USB gadget — the caller is responsible for ensuring the gadget is
// disabled before calling this function.  The function mounts the cam disk,
// copies the file, and unmounts.
func copyLockChimeToCamMount() error {
	data, err := os.ReadFile(lockChimeTarget)
	if err != nil {
		// No staged LockChime.wav — nothing to copy
		return nil
	}

	if _, err := os.Stat(camDiskImage); os.IsNotExist(err) {
		log.Printf("lockchime: cam disk image not found, skipping cam sync")
		return nil
	}

	// Check if cam disk is already mounted (e.g. archiveloop has it mounted).
	// If so, skip — archiveloop's sync_user_media_to_cam will handle the copy.
	alreadyMounted := isMountPointActive(camMountPoint)
	if alreadyMounted {
		log.Printf("lockchime: cam disk already mounted (archiveloop?), skipping to avoid conflict")
		return nil
	}

	if _, err := shell.RunWithTimeout(10*time.Second, "mount", camMountPoint); err != nil {
		return fmt.Errorf("mount cam disk: %w", err)
	}

	camTarget := filepath.Join(camMountPoint, "LockChime.wav")
	writeErr := writeChimeFileAtomic(camTarget, data)

	if _, err := shell.RunWithTimeout(10*time.Second, "umount", camMountPoint); err != nil {
		log.Printf("lockchime: umount cam disk failed: %v", err)
	}

	if writeErr != nil {
		return fmt.Errorf("write LockChime.wav to cam disk: %w", writeErr)
	}

	log.Printf("lockchime: synced LockChime.wav to cam disk (%d bytes)", len(data))
	return nil
}

// syncLockChimeToCamDisk is used when the USB gadget may be active (e.g. manual
// activate or scheduled randomize).  It disables the gadget, copies the file
// into the cam disk, and re-enables the gadget so Tesla reads the new sound.
// Only re-enables the gadget if it was active before the operation.
func syncLockChimeToCamDisk() error {
	camDiskMu.Lock()
	defer camDiskMu.Unlock()

	if _, err := os.Stat(camDiskImage); os.IsNotExist(err) {
		log.Printf("lockchime: cam disk image not found, skipping cam sync")
		return nil
	}

	// Remember gadget state so we only re-enable if it was active
	gadgetWasActive := isGadgetActive()

	if gadgetWasActive {
		if _, err := shell.RunWithTimeout(10*time.Second, "bash", "/root/bin/disable_gadget.sh"); err != nil {
			log.Printf("lockchime: disable_gadget.sh failed: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	copyErr := copyLockChimeToCamMount()

	if gadgetWasActive {
		if _, err := shell.RunWithTimeout(10*time.Second, "bash", "/root/bin/enable_gadget.sh"); err != nil {
			log.Printf("lockchime: enable_gadget.sh failed: %v", err)
			return fmt.Errorf("re-enable gadget: %w", err)
		}
		log.Printf("lockchime: USB gadget re-enabled — Tesla will read the new lock sound")
	}

	return copyErr
}

// clearLockChimeFromCamDisk removes LockChime.wav from the cam disk image.
// Same gadget disable/mount/unmount/enable cycle as syncLockChimeToCamDisk.
func clearLockChimeFromCamDisk() error {
	camDiskMu.Lock()
	defer camDiskMu.Unlock()

	if _, err := os.Stat(camDiskImage); os.IsNotExist(err) {
		return nil
	}

	gadgetWasActive := isGadgetActive()

	if gadgetWasActive {
		if _, err := shell.RunWithTimeout(10*time.Second, "bash", "/root/bin/disable_gadget.sh"); err != nil {
			log.Printf("lockchime: disable_gadget.sh failed: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if _, err := shell.RunWithTimeout(10*time.Second, "mount", camMountPoint); err != nil {
		if gadgetWasActive {
			shell.RunWithTimeout(10*time.Second, "bash", "/root/bin/enable_gadget.sh")
		}
		return fmt.Errorf("mount cam disk: %w", err)
	}

	camTarget := filepath.Join(camMountPoint, "LockChime.wav")
	os.Remove(camTarget)
	syscall.Sync()

	if _, err := shell.RunWithTimeout(10*time.Second, "umount", camMountPoint); err != nil {
		log.Printf("lockchime: umount cam disk failed: %v", err)
	}

	if gadgetWasActive {
		if _, err := shell.RunWithTimeout(10*time.Second, "bash", "/root/bin/enable_gadget.sh"); err != nil {
			return fmt.Errorf("re-enable gadget: %w", err)
		}
	}

	log.Printf("lockchime: cleared LockChime.wav from cam disk")
	return nil
}

// RandomConfig stores the random mode settings.
type lockChimeRandomConfig struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`     // "on_connect", "scheduled", or "smart"
	Interval string `json:"interval"` // "hourly", "daily", "weekly" (scheduled/smart mode)
	Hour     int    `json:"hour"`     // 0-23, hour of day for daily/weekly schedules
	Day      int    `json:"day"`      // 0=Sun, 1=Mon, ..., 6=Sat (weekly only)
}

// queryBLEShiftState queries the vehicle's current shift state via BLE.
// Returns the shift state ("P", "D", "R", "N", "SNA") and nil on success,
// or empty string and a descriptive error on failure.
func queryBLEShiftState() (string, error) {
	vin := readBLEVin()
	if vin == "" {
		return "", fmt.Errorf("no VIN configured — set TESLA_BLE_VIN in your config")
	}

	if _, err := os.Stat("/root/.ble/paired"); err != nil {
		return "", fmt.Errorf("BLE not paired — pair your Pi in Settings first")
	}

	if _, err := os.Stat("/root/.ble/key_private.pem"); err != nil {
		return "", fmt.Errorf("BLE private key missing at /root/.ble/key_private.pem")
	}

	// Stop the BLE GATT daemon so tesla-control gets exclusive hci0 access.
	// Without this, concurrent BLE usage can cause "device or resource busy".
	shell.RunWithTimeout(5*time.Second, "systemctl", "stop", "sentryusb-ble")

	out, err := shell.RunWithTimeout(15*time.Second,
		"/root/bin/tesla-control", "-ble", "-vin", strings.ToUpper(vin),
		"-key-file", "/root/.ble/key_private.pem",
		"state", "drive")

	// Always restart the BLE daemon regardless of success/failure
	shell.RunWithTimeout(5*time.Second, "systemctl", "start", "sentryusb-ble")

	if err != nil {
		errMsg := shell.CleanStderr(err.Error())
		log.Printf("lockchime: BLE drive state query failed: %s", errMsg)
		return "", fmt.Errorf("tesla-control failed: %s", errMsg)
	}

	// Parse the JSON response to extract shift state.
	// The response contains {"driveState":{"shiftState":{"p":{}},...}}
	var resp struct {
		DriveState struct {
			ShiftState json.RawMessage `json:"shiftState"`
		} `json:"driveState"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		log.Printf("lockchime: failed to parse drive state JSON: %v\nRaw output: %s", err, out)
		return "", fmt.Errorf("unexpected response from tesla-control — raw: %s", strings.TrimSpace(out))
	}

	if len(resp.DriveState.ShiftState) == 0 {
		return "", fmt.Errorf("vehicle returned empty drive state — car may be asleep")
	}

	// shiftState is a oneof — the key name IS the state (p, d, r, n, SNA)
	var stateMap map[string]interface{}
	if err := json.Unmarshal(resp.DriveState.ShiftState, &stateMap); err != nil {
		return "", fmt.Errorf("could not parse shiftState field — raw: %s", string(resp.DriveState.ShiftState))
	}

	for key := range stateMap {
		state := strings.ToUpper(key)
		log.Printf("lockchime: vehicle shift state: %s", state)
		return state, nil
	}

	return "", fmt.Errorf("shiftState was empty in response")
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
	if cfg.Mode != "on_connect" && cfg.Mode != "scheduled" && cfg.Mode != "smart" && cfg.Mode != "" {
		return fmt.Errorf("invalid mode: must be on_connect, scheduled, or smart")
	}
	if cfg.Mode == "scheduled" || cfg.Mode == "smart" {
		if cfg.Interval == "" {
			cfg.Interval = "daily"
		}
		if cfg.Interval != "hourly" && cfg.Interval != "daily" && cfg.Interval != "weekly" {
			return fmt.Errorf("invalid interval: must be hourly, daily, or weekly")
		}
		if cfg.Hour < 0 || cfg.Hour > 23 {
			cfg.Hour = 0
		}
		if cfg.Day < 0 || cfg.Day > 6 {
			cfg.Day = 0
		}
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

// pickAndActivateRandom selects a random .wav from the library (avoiding the
// currently active chime when possible) and stages it at /mutable/LockChime.wav.
// The caller is responsible for syncing the file to the cam disk and managing
// the USB gadget lifecycle.  Returns the chosen filename or empty string.
func pickAndActivateRandom() string {
	wavs := listWavFiles()
	if len(wavs) == 0 {
		return ""
	}

	// Read current active name so we can avoid picking it again
	currentActive := ""
	if data, err := os.ReadFile(lockChimeActiveFile); err == nil {
		currentActive = strings.TrimSpace(string(data))
	}

	// Filter out the current chime if we have more than one option
	candidates := wavs
	if currentActive != "" && len(wavs) > 1 {
		filtered := make([]string, 0, len(wavs)-1)
		for _, w := range wavs {
			if w != currentActive {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) > 0 {
			candidates = filtered
		}
	}

	chosen := candidates[rand.Intn(len(candidates))]
	srcPath := filepath.Join(lockChimeDir, chosen)

	data, err := os.ReadFile(srcPath)
	if err != nil {
		log.Printf("lockchime: failed to read %s: %v", chosen, err)
		return ""
	}

	if err := writeChimeFileAtomic(lockChimeTarget, data); err != nil {
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
	if chosen := pickAndActivateRandom(); chosen != "" {
		lockChimeLog("on_connect mode — changed lock chime to %q", chosen)
	}
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
	// Check every minute; fire when the configured schedule matches.
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Track the last date-string to avoid firing twice per window.
	var lastScheduledKey string
	// Smart mode: when the schedule fires but the car isn't parked, we set
	// smartPending=true and retry every 15 minutes until parked or the next
	// schedule window arrives.
	var smartPending bool
	var lastSmartRetry time.Time

	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			cfg := loadLockChimeRandomConfig()
			if !cfg.Enabled {
				lastScheduledKey = ""
				continue
			}

			if cfg.Mode == "scheduled" || cfg.Mode == "smart" {
				// Time-of-day based scheduling
				var runKey string
				switch cfg.Interval {
				case "hourly":
					if now.Minute() != 0 {
						continue
					}
					runKey = now.Format("2006-01-02-15")
				case "daily":
					if now.Hour() != cfg.Hour || now.Minute() != 0 {
						continue
					}
					runKey = now.Format("2006-01-02")
				case "weekly":
					if int(now.Weekday()) != cfg.Day || now.Hour() != cfg.Hour || now.Minute() != 0 {
						continue
					}
					runKey = now.Format("2006-W01")
				default:
					continue
				}

				// Smart mode: support retries every 15 min if car wasn't parked
				if cfg.Mode == "smart" {
					isNewWindow := runKey != lastScheduledKey
					isRetry := smartPending && !lastSmartRetry.IsZero() && now.Sub(lastSmartRetry) >= 15*time.Minute

					if !isNewWindow && !isRetry {
						continue
					}

					// Query vehicle shift state via BLE
					shiftState, err := queryBLEShiftState()
					if err != nil {
						log.Printf("lockchime: smart mode — BLE query failed: %v, will retry in 15 min", err)
						lockChimeLog("smart mode — BLE query failed: %v, will retry in 15 min", err)
						smartPending = true
						lastSmartRetry = now
						lastScheduledKey = runKey
						continue
					}
					if shiftState != "P" {
						log.Printf("lockchime: smart mode — vehicle in %s, will retry in 15 min", shiftState)
						lockChimeLog("smart mode — vehicle in %s, will retry in 15 min", shiftState)
						smartPending = true
						lastSmartRetry = now
						lastScheduledKey = runKey
						continue
					}

					log.Printf("lockchime: smart mode — vehicle in Park, proceeding with chime change")
					lockChimeLog("smart mode — vehicle in Park, proceeding with chime change")
					smartPending = false
				} else {
					// Scheduled mode: simple dedup
					if runKey == lastScheduledKey {
						continue
					}
				}

				if chosen := pickAndActivateRandom(); chosen != "" {
					lockChimeLog("%s mode — changed lock chime to %q", cfg.Mode, chosen)
					if err := syncLockChimeToCamDisk(); err != nil {
						log.Printf("lockchime: %s cam sync failed: %v", cfg.Mode, err)
						lockChimeLog("%s mode — cam disk sync FAILED: %v", cfg.Mode, err)
					} else {
						lockChimeLog("%s mode — cam disk sync OK, Tesla will use new sound", cfg.Mode)
					}
				}
				lastScheduledKey = runKey
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

	// Atomic write to target with fsync + system sync
	if err := writeChimeFileAtomic(lockChimeTarget, data); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to activate lock sound")
		return
	}

	// Record the active filename in sidecar metadata
	os.WriteFile(lockChimeActiveFile, []byte(filename), 0644)

	// Sync the file into the cam disk image and re-enable the USB gadget so
	// Tesla actually reads the new sound (runs in background to avoid blocking
	// the HTTP response — the gadget cycle takes several seconds).
	go func() {
		if err := syncLockChimeToCamDisk(); err != nil {
			log.Printf("lockchime: cam disk sync failed after activate: %v", err)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"active":       filename,
		"usb_rebound":  true,
	})
}

// POST /api/lockchime/clear-active — remove active lock sound from Tesla location
func (h *handlers) lockChimeClear(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(lockChimeTarget); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "Failed to clear active sound")
		return
	}
	os.Remove(lockChimeActiveFile)
	syscall.Sync()

	// Also remove from the cam disk so Tesla no longer has a lock sound
	go func() {
		if err := clearLockChimeFromCamDisk(); err != nil {
			log.Printf("lockchime: cam disk clear failed: %v", err)
		}
	}()

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

	// If the deleted file was the active chime, clear it from staging and cam disk
	if data, err := os.ReadFile(lockChimeActiveFile); err == nil {
		if strings.TrimSpace(string(data)) == filename {
			os.Remove(lockChimeTarget)
			os.Remove(lockChimeActiveFile)
			syscall.Sync()
			go func() {
				if err := clearLockChimeFromCamDisk(); err != nil {
					log.Printf("lockchime: cam disk clear after delete failed: %v", err)
				}
			}()
		}
	}

	writeOK(w)
}

// ──────────────────────────────────────────────────────────────────
// Random mode endpoints
// ──────────────────────────────────────────────────────────────────

// GET /api/lockchime/random-config — get random mode settings
func (h *handlers) lockChimeGetRandomConfig(w http.ResponseWriter, r *http.Request) {
	cfg := loadLockChimeRandomConfig()

	// Check for actual RTC hardware. getRTCInfo().RTCPresent depends on the
	// RTC_BATTERY_ENABLED config flag which may not be set even when hardware
	// exists, so check /dev/rtc0 directly.
	_, rtcErr := os.Stat("/dev/rtc0")
	hasRTC := rtcErr == nil

	// Smart mode requires BLE paired + RTC
	_, blePairedErr := os.Stat("/root/.ble/paired")
	hasBLE := blePairedErr == nil && readBLEVin() != ""

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":  cfg.Enabled,
		"mode":     cfg.Mode,
		"interval": cfg.Interval,
		"hour":     cfg.Hour,
		"day":      cfg.Day,
		"has_rtc":  hasRTC,
		"has_ble":  hasBLE,
	})
}

// GET /api/lockchime/ble-shift-state — test BLE shift state query
func (h *handlers) lockChimeBLEShiftState(w http.ResponseWriter, r *http.Request) {
	state, err := queryBLEShiftState()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	labels := map[string]string{
		"P":   "Park",
		"D":   "Drive",
		"R":   "Reverse",
		"N":   "Neutral",
		"SNA": "Not Available",
	}
	label := labels[state]
	if label == "" {
		label = state
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"shift_state": state,
		"label":       label,
	})
}

// PUT /api/lockchime/random-config — update random mode settings
func (h *handlers) lockChimeSaveRandomConfig(w http.ResponseWriter, r *http.Request) {
	var req lockChimeRandomConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// If scheduled or smart mode, verify RTC hardware is present
	if req.Enabled && (req.Mode == "scheduled" || req.Mode == "smart") {
		if _, err := os.Stat("/dev/rtc0"); err != nil {
			modeName := req.Mode
			if modeName == "smart" {
				modeName = "Smart"
			} else {
				modeName = "Scheduled"
			}
			writeError(w, http.StatusBadRequest, modeName+" mode requires a working RTC (Pi 5 with battery)")
			return
		}
	}

	// Smart mode requires both RTC and BLE
	if req.Enabled && req.Mode == "smart" {
		if _, err := os.Stat("/dev/rtc0"); err != nil {
			writeError(w, http.StatusBadRequest, "Smart mode requires a working RTC (Pi 5 with battery)")
			return
		}
		if _, err := os.Stat("/root/.ble/paired"); err != nil {
			writeError(w, http.StatusBadRequest, "Smart mode requires a paired BLE key — pair your Pi in Settings first")
			return
		}
		if readBLEVin() == "" {
			writeError(w, http.StatusBadRequest, "Smart mode requires a VIN configured for BLE")
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
		"hour":     req.Hour,
		"day":      req.Day,
	})
}

// POST /api/lockchime/randomize-on-connect — conditionally randomize if on_connect mode is active.
// Called by archiveloop before enabling the USB gadget so Tesla reads a fresh file on mount.
func (h *handlers) lockChimeRandomizeOnConnect(w http.ResponseWriter, r *http.Request) {
	cfg := loadLockChimeRandomConfig()
	if !cfg.Enabled || cfg.Mode != "on_connect" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"skipped": true,
			"reason":  "random on_connect mode not active",
		})
		return
	}
	chosen := pickAndActivateRandom()
	if chosen == "" {
		writeError(w, http.StatusBadRequest, "No sounds in library to randomize")
		return
	}
	lockChimeLog("on_connect mode (archiveloop) — changed lock chime to %q", chosen)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"active":  chosen,
	})
}

// POST /api/lockchime/randomize — manually trigger a random selection
func (h *handlers) lockChimeRandomize(w http.ResponseWriter, r *http.Request) {
	chosen := pickAndActivateRandom()
	if chosen == "" {
		writeError(w, http.StatusBadRequest, "No sounds in library to randomize")
		return
	}

	lockChimeLog("manual randomize — changed lock chime to %q", chosen)

	// Sync to cam disk in background (gadget may be active)
	go func() {
		if err := syncLockChimeToCamDisk(); err != nil {
			log.Printf("lockchime: cam disk sync failed after manual randomize: %v", err)
			lockChimeLog("manual randomize — cam disk sync FAILED: %v", err)
		} else {
			lockChimeLog("manual randomize — cam disk sync OK")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"active":      chosen,
		"usb_rebound": true,
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
