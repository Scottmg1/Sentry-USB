package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/config"
	"github.com/Scottmg1/Sentry-USB/server/shell"
)

const lastHashFile = "/mutable/backups/.last_hash"

// BackupData is the JSON structure written to each backup file.
type BackupData struct {
	Version           int               `json:"version"`
	Date              string            `json:"date"`
	Timestamp         string            `json:"timestamp"`
	Hostname          string            `json:"hostname"`
	Config            string            `json:"config"`
	Preferences       map[string]string `json:"preferences"`
	DriveDataIncluded bool              `json:"drive_data_included"`
	// SSH keys for rsync archive access
	SSHPrivateKey string `json:"ssh_private_key,omitempty"`
	SSHPublicKey  string `json:"ssh_public_key,omitempty"`
	// rclone config for cloud archive
	RcloneConfig string `json:"rclone_config,omitempty"`
	// Tesla BLE pairing keys
	BLEPrivateKey string `json:"ble_private_key,omitempty"`
	BLEPublicKey  string `json:"ble_public_key,omitempty"`
	// Notification device credentials (mobile push pairing)
	NotificationCredentials string `json:"notification_credentials,omitempty"`
}

// BackupEntry describes a single backup for the list endpoint.
type BackupEntry struct {
	Date      string `json:"date"`
	Timestamp string `json:"timestamp"`
	Location  string `json:"location"`
	Size      int64  `json:"size"`
	Filename  string `json:"filename"`
}

const (
	localBackupDir   = "/mutable/backups"
	archiveBackupDir = "/mnt/archive/backups"
)

// backupFilename returns the per-day backup filename.
func backupFilename(date string) string {
	return fmt.Sprintf("sentryusb-backup-%s.json", date)
}

// readFileIfExists reads a file and returns its contents, or empty string if not found.
func readFileIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// createBackupData reads the current config, preferences, SSH keys, and rclone config into a BackupData struct.
func createBackupData() (*BackupData, error) {
	configPath := config.FindConfigPath()
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	prefs := loadPreferences()

	hostname, _ := os.Hostname()
	now := time.Now()

	return &BackupData{
		Version:           1,
		Date:              now.Format("2006-01-02"),
		Timestamp:         now.UTC().Format(time.RFC3339),
		Hostname:          hostname,
		Config:            string(configBytes),
		Preferences:       prefs,
		DriveDataIncluded: false,
		SSHPrivateKey:     readFileIfExists("/root/.ssh/id_rsa"),
		SSHPublicKey:      readFileIfExists("/root/.ssh/id_rsa.pub"),
		RcloneConfig:      readFileIfExists("/root/.config/rclone/rclone.conf"),
		BLEPrivateKey:     readFileIfExists("/root/.ble/key_private.pem"),
		BLEPublicKey:      readFileIfExists("/root/.ble/key_public.pem"),
		NotificationCredentials: readFileIfExists("/root/.sentryusb/notification-credentials.json"),
	}, nil
}

// computeBackupHash computes a SHA256 hash of all backup-relevant data.
// This is used to detect whether anything has changed since the last backup.
// Time-varying fields (date, timestamp) are excluded so the hash is stable.
func computeBackupHash(data *BackupData) string {
	h := sha256.New()
	h.Write([]byte(data.Config))
	// Sort preference keys for deterministic hashing
	keys := make([]string, 0, len(data.Preferences))
	for k := range data.Preferences {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(data.Preferences[k]))
	}
	h.Write([]byte(data.SSHPrivateKey))
	h.Write([]byte(data.SSHPublicKey))
	h.Write([]byte(data.RcloneConfig))
	h.Write([]byte(data.BLEPrivateKey))
	h.Write([]byte(data.BLEPublicKey))
	h.Write([]byte(data.NotificationCredentials))
	return hex.EncodeToString(h.Sum(nil))
}

// readLastHash reads the last backup hash from disk.
func readLastHash() string {
	data, err := os.ReadFile(lastHashFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeLastHash stores the backup hash to disk.
func writeLastHash(hash string) {
	os.MkdirAll(filepath.Dir(lastHashFile), 0755)
	os.WriteFile(lastHashFile, []byte(hash+"\n"), 0644)
}

// writeBackupToDir writes a backup JSON file to the given directory.
func writeBackupToDir(dir string, data *BackupData) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create backup dir %s: %w", dir, err)
	}

	filename := backupFilename(data.Date)
	path := filepath.Join(dir, filename)

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, jsonBytes, 0644); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to write backup: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to finalize backup: %w", err)
	}

	log.Printf("[backup] Wrote backup to %s (%d bytes)", path, len(jsonBytes))
	return nil
}

// syncBackupToRsync copies the backup file to an rsync archive server.
func syncBackupToRsync(data *BackupData) error {
	active, _, err := config.ParseFile(config.FindConfigPath())
	if err != nil {
		return err
	}

	server := active["RSYNC_SERVER"]
	user := active["RSYNC_USER"]
	rsyncPath := active["RSYNC_PATH"]
	if server == "" || user == "" {
		return fmt.Errorf("rsync not configured")
	}

	// Write to local temp first
	tmpDir := "/tmp/sentryusb-backup-sync"
	os.MkdirAll(tmpDir, 0755)
	filename := backupFilename(data.Date)
	tmpPath := filepath.Join(tmpDir, filename)

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, jsonBytes, 0644); err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	dest := fmt.Sprintf("%s@%s:%s/backups/%s", user, server, rsyncPath, filename)
	// Ensure remote backups/ dir exists
	shell.RunWithTimeout(10*time.Second, "ssh",
		"-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=no", "-o", "BatchMode=yes",
		fmt.Sprintf("%s@%s", user, server),
		"mkdir", "-p", fmt.Sprintf("%s/backups", rsyncPath))

	_, err = shell.RunWithTimeout(60*time.Second, "rsync", "-avh", "--no-perms", "--omit-dir-times", "--timeout=60",
		tmpPath, dest)
	return err
}

// syncBackupToRclone copies the backup file to an rclone archive destination.
func syncBackupToRclone(data *BackupData) error {
	active, _, err := config.ParseFile(config.FindConfigPath())
	if err != nil {
		return err
	}

	drive := active["RCLONE_DRIVE"]
	rclonePath := active["RCLONE_PATH"]
	if drive == "" {
		return fmt.Errorf("rclone not configured")
	}

	tmpDir := "/tmp/sentryusb-backup-sync"
	os.MkdirAll(tmpDir, 0755)
	filename := backupFilename(data.Date)
	tmpPath := filepath.Join(tmpDir, filename)

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, jsonBytes, 0644); err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	dest := fmt.Sprintf("%s:%s/backups/", drive, rclonePath)
	_, err = shell.RunWithTimeout(60*time.Second, "rclone",
		"--config", "/root/.config/rclone/rclone.conf",
		"copy", tmpPath, dest)
	return err
}

// listBackupsInDir scans a directory for backup JSON files and returns entries.
func listBackupsInDir(dir, location string) []BackupEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var backups []BackupEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "sentryusb-backup-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}

		// Read the file to get timestamp
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var bd BackupData
		if err := json.Unmarshal(data, &bd); err != nil {
			continue
		}

		backups = append(backups, BackupEntry{
			Date:      bd.Date,
			Timestamp: bd.Timestamp,
			Location:  location,
			Size:      info.Size(),
			Filename:  e.Name(),
		})
	}
	return backups
}

// POST /api/system/backup — create a config backup
// Query params: ?force=1 to skip change detection (used by manual "Backup Now")
func (h *handlers) createBackup(w http.ResponseWriter, r *http.Request) {
	data, err := createBackupData()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create backup: %v", err))
		return
	}

	// Change detection: skip backup if nothing changed since last time
	force := r.URL.Query().Get("force") == "1"
	currentHash := computeBackupHash(data)
	if !force && currentHash == readLastHash() {
		log.Printf("[backup] Skipped — no changes detected (hash %s)", currentHash[:12])
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"skipped":  true,
			"reason":   "no changes detected",
			"date":     data.Date,
		})
		return
	}

	prefs := loadPreferences()
	location := prefs["backup_location"]
	if location == "" {
		location = "archive"
	}

	var backupErr error

	if location == "ssd" {
		// Write to local SSD
		backupErr = writeBackupToDir(localBackupDir, data)
	} else {
		// Write to archive destination
		active, _, _ := config.ParseFile(config.FindConfigPath())
		archiveSystem := active["ARCHIVE_SYSTEM"]

		switch archiveSystem {
		case "cifs", "nfs":
			// CIFS/NFS: archive is mounted at /mnt/archive
			if _, err := os.Stat("/mnt/archive"); err == nil {
				backupErr = writeBackupToDir(archiveBackupDir, data)
			} else {
				backupErr = fmt.Errorf("archive not mounted at /mnt/archive")
			}
		case "rsync":
			backupErr = syncBackupToRsync(data)
		case "rclone":
			backupErr = syncBackupToRclone(data)
		default:
			// Fallback to local SSD if no archive configured
			log.Printf("[backup] No archive system configured, falling back to local SSD backup")
			backupErr = writeBackupToDir(localBackupDir, data)
		}
	}

	// Also always keep a local copy on the SSD as a safety net
	if location != "ssd" {
		if localErr := writeBackupToDir(localBackupDir, data); localErr != nil {
			log.Printf("[backup] Warning: failed to write local backup copy: %v", localErr)
		}
	}

	if backupErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Backup failed: %v", backupErr))
		return
	}

	// Store the hash so next run can detect changes
	writeLastHash(currentHash)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"date":     data.Date,
		"location": location,
	})
}

// GET /api/system/backups — list all available backups
func (h *handlers) listBackups(w http.ResponseWriter, r *http.Request) {
	var allBackups []BackupEntry

	// Local backups
	allBackups = append(allBackups, listBackupsInDir(localBackupDir, "ssd")...)

	// Archive backups (only if mounted)
	if _, err := os.Stat(archiveBackupDir); err == nil {
		allBackups = append(allBackups, listBackupsInDir(archiveBackupDir, "archive")...)
	}

	// Deduplicate by date (prefer archive copy if both exist)
	seen := make(map[string]int)
	for i, b := range allBackups {
		if existing, ok := seen[b.Date]; ok {
			if b.Location == "archive" {
				allBackups[existing] = b
			}
			allBackups[i].Date = "" // mark for removal
		} else {
			seen[b.Date] = i
		}
	}

	var result []BackupEntry
	for _, b := range allBackups {
		if b.Date != "" {
			result = append(result, b)
		}
	}

	// Sort by date descending (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date > result[j].Date
	})

	if result == nil {
		result = []BackupEntry{}
	}

	writeJSON(w, http.StatusOK, result)
}

// GET /api/system/backup/{date} — download a specific backup file
func (h *handlers) getBackup(w http.ResponseWriter, r *http.Request) {
	date := r.PathValue("date")
	if date == "" {
		writeError(w, http.StatusBadRequest, "date parameter required")
		return
	}

	filename := backupFilename(date)

	// Try archive first, then local
	for _, dir := range []string{archiveBackupDir, localBackupDir} {
		path := filepath.Join(dir, filename)
		data, err := os.ReadFile(path)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
			w.Write(data)
			return
		}
	}

	writeError(w, http.StatusNotFound, "backup not found for date: "+date)
}

// POST /api/system/restore — restore config and preferences from a backup
func (h *handlers) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var backup BackupData
	if err := json.NewDecoder(r.Body).Decode(&backup); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid backup JSON: %v", err))
		return
	}

	if backup.Version == 0 || backup.Config == "" {
		writeError(w, http.StatusBadRequest, "Invalid backup: missing version or config data")
		return
	}

	// Remount filesystem read-write for config write
	shell.Run("bash", "-c", "/root/bin/remountfs_rw")

	// Write the config file
	configPath := config.FindConfigPath()
	if err := os.WriteFile(configPath, []byte(backup.Config), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to write config: %v", err))
		return
	}
	log.Printf("[backup] Restored config to %s", configPath)

	// Restore preferences
	if backup.Preferences != nil && len(backup.Preferences) > 0 {
		if err := savePreferences(backup.Preferences); err != nil {
			log.Printf("[backup] Warning: failed to restore preferences: %v", err)
		} else {
			log.Printf("[backup] Restored %d preferences", len(backup.Preferences))
		}
	}

	// Restore SSH keys (for rsync archive access)
	if backup.SSHPrivateKey != "" {
		os.MkdirAll("/root/.ssh", 0700)
		if err := os.WriteFile("/root/.ssh/id_rsa", []byte(backup.SSHPrivateKey), 0600); err != nil {
			log.Printf("[backup] Warning: failed to restore SSH private key: %v", err)
		} else {
			log.Printf("[backup] Restored SSH private key")
		}
		if backup.SSHPublicKey != "" {
			if err := os.WriteFile("/root/.ssh/id_rsa.pub", []byte(backup.SSHPublicKey), 0644); err != nil {
				log.Printf("[backup] Warning: failed to restore SSH public key: %v", err)
			}
		}
	}

	// Restore rclone config (for cloud archive access)
	if backup.RcloneConfig != "" {
		os.MkdirAll("/root/.config/rclone", 0755)
		if err := os.WriteFile("/root/.config/rclone/rclone.conf", []byte(backup.RcloneConfig), 0600); err != nil {
			log.Printf("[backup] Warning: failed to restore rclone config: %v", err)
		} else {
			log.Printf("[backup] Restored rclone config")
		}
	}

	// Restore Tesla BLE pairing keys (avoids needing to re-pair with the car)
	if backup.BLEPrivateKey != "" {
		os.MkdirAll("/root/.ble", 0700)
		if err := os.WriteFile("/root/.ble/key_private.pem", []byte(backup.BLEPrivateKey), 0600); err != nil {
			log.Printf("[backup] Warning: failed to restore BLE private key: %v", err)
		} else {
			log.Printf("[backup] Restored BLE private key")
			// Also restore public key and mark as paired
			if backup.BLEPublicKey != "" {
				os.WriteFile("/root/.ble/key_public.pem", []byte(backup.BLEPublicKey), 0644)
			}
			os.WriteFile("/root/.ble/paired", []byte("1"), 0644)
		}
	}

	// Restore notification device credentials (preserves mobile app pairing)
	if backup.NotificationCredentials != "" {
		os.MkdirAll("/root/.sentryusb", 0700)
		if err := os.WriteFile("/root/.sentryusb/notification-credentials.json", []byte(backup.NotificationCredentials), 0600); err != nil {
			log.Printf("[backup] Warning: failed to restore notification credentials: %v", err)
		} else {
			log.Printf("[backup] Restored notification credentials")
		}
	}

	// Parse the restored config so the wizard can populate fields
	active, _, _ := config.ParseFile(configPath)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"date":     backup.Date,
		"hostname": backup.Hostname,
		"config":   active,
	})
}
