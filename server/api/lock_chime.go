package api

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const lockChimeDir = "/mutable/LockChime"

// lockChimeTarget is the path Tesla reads the lock sound from (root of USB drive).
const lockChimeTarget = "/mutable/LockChime.wav"

// lockChimeMaxBytes is the max upload size (5 MB — well above any 7-second WAV).
const lockChimeMaxBytes = 5 * 1024 * 1024

// lockChimeMaxSeconds is Tesla's documented maximum lock sound duration.
const lockChimeMaxSeconds = 7.0

var validLockChimeFile = regexp.MustCompile(`^[a-zA-Z0-9 \-_.]+\.wav$`)

// sanitizeLockChimeName returns a safe filename (keeps alphanum, spaces, hyphens, underscores, dots).
func sanitizeLockChimeName(name string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9 \-_.]`)
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

	// Determine which file is currently active by reading the target
	activeName := ""
	if linkDest, err := os.Readlink(lockChimeTarget); err == nil {
		activeName = filepath.Base(linkDest)
	} else if _, err := os.Stat(lockChimeTarget); err == nil {
		// Not a symlink — check by comparing contents or just mark unknown
		activeName = ""
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
		"sounds":       sounds,
		"active_name":  activeName,
		"active_set":   activeExists,
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

	// Sanitize filename and avoid collisions
	baseName := sanitizeLockChimeName(header.Filename)
	destPath := filepath.Join(lockChimeDir, baseName)
	if _, err := os.Stat(destPath); err == nil {
		// File exists — append suffix
		ext := filepath.Ext(baseName)
		stem := strings.TrimSuffix(baseName, ext)
		for i := 1; i <= 100; i++ {
			candidate := filepath.Join(lockChimeDir, fmt.Sprintf("%s_%d%s", stem, i, ext))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				destPath = candidate
				baseName = filepath.Base(candidate)
				break
			}
		}
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"name":     baseName,
		"path":     destPath,
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"active":  filename,
	})
}

// DELETE /api/lockchime/clear — remove active lock sound from Tesla location
func (h *handlers) lockChimeClear(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(lockChimeTarget); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "Failed to clear active sound")
		return
	}
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
