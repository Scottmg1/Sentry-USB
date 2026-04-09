package drives

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DefaultDataPath is where drive data is stored on the Pi.
// Uses /mutable/ which is always writable (root FS is read-only).
const DefaultDataPath = "/mutable/drive-data.json"

// legacyDataPath is the old location on the root filesystem.
// Checked during Load() for migration.
const legacyDataPath = "/root/drive-data.json"

// GearRun represents a contiguous run of frames in the same gear state.
// Computed from raw (pre-dedup) frame data for accurate intra-clip splitting.
type GearRun struct {
	Gear   uint8 `json:"gear"`
	Frames int   `json:"frames"`
}

// Route represents GPS data extracted from a single front-camera clip.
type Route struct {
	File            string     `json:"file"`
	Date            string     `json:"date"`
	Points          []GPSPoint `json:"points"`
	GearStates      []uint8    `json:"gearStates,omitempty"`
	AutopilotStates []uint8    `json:"autopilotStates,omitempty"`
	Speeds          []float32  `json:"speeds,omitempty"`
	AccelPositions  []float32  `json:"accelPositions,omitempty"`
	RawParkCount    int        `json:"rawParkCount,omitempty"`
	RawFrameCount   int        `json:"rawFrameCount,omitempty"`
	GearRuns        []GearRun  `json:"gearRuns,omitempty"`
}

// StoreData is the persistent JSON structure.
type StoreData struct {
	ProcessedFiles []string            `json:"processedFiles"`
	Routes         []Route             `json:"routes"`
	DriveTags      map[string][]string `json:"driveTags,omitempty"`
}

// Store manages the drive data file with thread-safe access.
type Store struct {
	mu             sync.RWMutex
	path           string
	data           StoreData
	processedIndex map[string]bool // normalized path → present; rebuilt on Load
}

// NewStore creates a store at the given path.
func NewStore(path string) *Store {
	if path == "" {
		path = DefaultDataPath
	}
	return &Store{path: path, processedIndex: make(map[string]bool)}
}

// Load reads the data file from disk. Returns empty data if file doesn't exist.
// If the file is missing at the current path but exists at the legacy path
// (/root/drive-data.json), it is migrated to the new writable location.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Try migrating from the legacy path on the root filesystem
			if s.path != legacyDataPath {
				if lf, lerr := os.Open(legacyDataPath); lerr == nil {
					defer lf.Close()
					if decErr := json.NewDecoder(lf).Decode(&s.data); decErr == nil {
						log.Printf("[drives] Migrated drive data from %s to %s", legacyDataPath, s.path)
						// Best-effort: write to new path so future loads find it
						s.saveLocked()
						s.rebuildProcessedIndex()
						return nil
					}
				}
			}
			s.data = StoreData{}
			s.rebuildProcessedIndex()
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	if err := dec.Decode(&s.data); err != nil {
		return err
	}
	s.rebuildProcessedIndex()
	return nil
}

// rebuildProcessedIndex rebuilds the in-memory processed file index.
// Caller must hold the write lock.
func (s *Store) rebuildProcessedIndex() {
	s.processedIndex = make(map[string]bool, len(s.data.ProcessedFiles))
	for _, f := range s.data.ProcessedFiles {
		s.processedIndex[strings.ReplaceAll(f, "\\", "/")] = true
	}
}

// Save writes the current data to disk.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked writes data to disk without acquiring the lock (caller must hold it).
func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&s.data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()

	return os.Rename(tmp, s.path)
}

// ProcessedSet returns a set of already-processed file paths (normalized to forward slashes).
func (s *Store) ProcessedSet() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy of the index so callers can't mutate it
	set := make(map[string]bool, len(s.processedIndex))
	for k, v := range s.processedIndex {
		set[k] = v
	}
	return set
}

// AddRoute adds a processed file and its route data.
// gears is a parallel slice of gear states (same length as points); may be nil for legacy data.
// apStates is a parallel slice of autopilot states (0=off, >0=engaged).
// speeds is a parallel slice of speeds in m/s.
// accelPositions is a parallel slice of accelerator pedal positions (0-1 or 0-100 scale per firmware).
// rawParkCount/rawFrameCount are pre-dedup counts for accurate park time estimation.
// gearRuns stores contiguous gear transitions from raw data for intra-clip drive splitting.
//
// If a route for relativePath already exists it is replaced in-place (upsert),
// so reprocessing a file overwrites its old data rather than duplicating it.
func (s *Store) AddRoute(relativePath, dateDir string, points []GPSPoint, gears []uint8, apStates []uint8, speeds []float32, accelPositions []float32, rawParkCount, rawFrameCount int, gearRuns []GearRun) {
	s.mu.Lock()
	defer s.mu.Unlock()

	norm := strings.ReplaceAll(relativePath, "\\", "/")

	// Only add to processed list if not already present (O(1) lookup)
	if !s.processedIndex[norm] {
		s.data.ProcessedFiles = append(s.data.ProcessedFiles, relativePath)
		s.processedIndex[norm] = true
	}

	if len(points) == 0 {
		return
	}

	newRoute := Route{
		File:            relativePath,
		Date:            dateDir,
		Points:          points,
		GearStates:      gears,
		AutopilotStates: apStates,
		Speeds:          speeds,
		AccelPositions:  accelPositions,
		RawParkCount:    rawParkCount,
		RawFrameCount:   rawFrameCount,
		GearRuns:        gearRuns,
	}

	// Upsert: replace existing route for this file path if present.
	for i, r := range s.data.Routes {
		if strings.ReplaceAll(r.File, "\\", "/") == norm {
			s.data.Routes[i] = newRoute
			return
		}
	}
	s.data.Routes = append(s.data.Routes, newRoute)
}

// MarkProcessed marks a file as processed without adding route data (e.g. no GPS found).
func (s *Store) MarkProcessed(relativePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm := strings.ReplaceAll(relativePath, "\\", "/")
	if s.processedIndex[norm] {
		return
	}
	s.data.ProcessedFiles = append(s.data.ProcessedFiles, relativePath)
	s.processedIndex[norm] = true
}

// RouteCount returns the number of routes stored.
func (s *Store) RouteCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Routes)
}

// ProcessedCount returns the number of processed files.
func (s *Store) ProcessedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.ProcessedFiles)
}

// GetRoutes returns a copy of all routes.
func (s *Store) GetRoutes() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()

	routes := make([]Route, len(s.data.Routes))
	copy(routes, s.data.Routes)
	return routes
}

// Path returns the data file path.
func (s *Store) Path() string {
	return s.path
}

// ReplaceData replaces the entire store data (used for upload).
func (s *Store) ReplaceData(data StoreData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	s.rebuildProcessedIndex()
}

// GetData returns a copy of the store data.
func (s *Store) GetData() StoreData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sd := StoreData{
		ProcessedFiles: append([]string{}, s.data.ProcessedFiles...),
		Routes:         append([]Route{}, s.data.Routes...),
	}
	if s.data.DriveTags != nil {
		sd.DriveTags = make(map[string][]string, len(s.data.DriveTags))
		for k, v := range s.data.DriveTags {
			sd.DriveTags[k] = append([]string{}, v...)
		}
	}
	return sd
}

// SetDriveTags sets the tags for a drive identified by its start time key.
func (s *Store) SetDriveTags(driveKey string, tags []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.DriveTags == nil {
		s.data.DriveTags = make(map[string][]string)
	}
	if len(tags) == 0 {
		delete(s.data.DriveTags, driveKey)
	} else {
		s.data.DriveTags[driveKey] = tags
	}
}

// GetDriveTags returns the tags for a drive identified by its start time key.
func (s *Store) GetDriveTags(driveKey string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.DriveTags[driveKey]
}

// GetAllDriveTags returns the full tag map.
func (s *Store) GetAllDriveTags() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.DriveTags == nil {
		return nil
	}
	copy := make(map[string][]string, len(s.data.DriveTags))
	for k, v := range s.data.DriveTags {
		copy[k] = append([]string{}, v...)
	}
	return copy
}

// GetAllTagNames returns a deduplicated sorted list of all tag names in use.
func (s *Store) GetAllTagNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool)
	for _, tags := range s.data.DriveTags {
		for _, t := range tags {
			seen[t] = true
		}
	}
	names := make([]string, 0, len(seen))
	for t := range seen {
		names = append(names, t)
	}
	sort.Strings(names)
	return names
}

// ClearProcessedForReprocess clears the processed-files list so every clip
// found on disk is eligible for re-extraction.  Existing route data is
// intentionally kept: clips that no longer exist on disk retain their
// previously extracted data, while clips that are found and re-scanned
// have their routes overwritten in-place by AddRoute's upsert behaviour.
// Drive tags are preserved.
func (s *Store) ClearProcessedForReprocess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.ProcessedFiles = nil
	s.processedIndex = make(map[string]bool)
}

// ArchivePath returns the path where drive data is backed up on the archive mount.
const archiveDataPath = "/mnt/archive/drive-data.json"

// SyncToArchive copies the local drive data file to the archive mount.
// This is best-effort — it silently returns nil if the archive is not mounted.
func (s *Store) SyncToArchive() error {
	// Check if archive directory exists
	if _, err := os.Stat("/mnt/archive"); err != nil {
		return nil
	}

	// Verify it's actually a mounted filesystem (not just an empty local dir).
	// On Linux, check /proc/mounts; on other platforms skip the check.
	if mounts, err := os.ReadFile("/proc/mounts"); err == nil {
		if !strings.Contains(string(mounts), "/mnt/archive") {
			return nil
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	src, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	tmp := archiveDataPath + ".tmp"
	if err := os.WriteFile(tmp, src, 0644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, archiveDataPath); err != nil {
		return err
	}
	log.Printf("[drives] Synced drive data to archive (%d bytes)", len(src))
	return nil
}

// RestoreFromArchive copies drive data from the archive mount to the local path
// if the local file does not exist but the archive copy does.
// This is best-effort and should be called before Load().
func (s *Store) RestoreFromArchive() error {
	// Only restore if local file is missing
	if _, err := os.Stat(s.path); err == nil {
		return nil
	}

	// Check if archive copy exists
	src, err := os.ReadFile(archiveDataPath)
	if err != nil {
		return nil // archive not mounted or no backup — nothing to do
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(s.path, src, 0644); err != nil {
		return err
	}

	log.Printf("[drives] Restored drive data from archive (%d bytes)", len(src))
	return nil
}
