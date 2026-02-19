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
const DefaultDataPath = "/root/drive-data.json"

// Route represents GPS data extracted from a single front-camera clip.
type Route struct {
	File          string     `json:"file"`
	Date          string     `json:"date"`
	Points        []GPSPoint `json:"points"`
	GearStates    []uint8    `json:"gearStates,omitempty"`
	RawParkCount  int        `json:"rawParkCount,omitempty"`
	RawFrameCount int        `json:"rawFrameCount,omitempty"`
}

// StoreData is the persistent JSON structure.
type StoreData struct {
	ProcessedFiles []string            `json:"processedFiles"`
	Routes         []Route             `json:"routes"`
	DriveTags      map[string][]string `json:"driveTags,omitempty"`
}

// Store manages the drive data file with thread-safe access.
type Store struct {
	mu   sync.RWMutex
	path string
	data StoreData
}

// NewStore creates a store at the given path.
func NewStore(path string) *Store {
	if path == "" {
		path = DefaultDataPath
	}
	return &Store{path: path}
}

// Load reads the data file from disk. Returns empty data if file doesn't exist.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = StoreData{}
			return nil
		}
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	return dec.Decode(&s.data)
}

// Save writes the current data to disk.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	if err := enc.Encode(&s.data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()

	return os.Rename(tmp, s.path)
}

// ProcessedSet returns a set of already-processed file paths.
// Paths are stored with both original and normalized (forward-slash) forms
// so lookups work regardless of OS path separator.
func (s *Store) ProcessedSet() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	set := make(map[string]bool, len(s.data.ProcessedFiles)*2)
	for _, f := range s.data.ProcessedFiles {
		set[f] = true
		// Also index the normalized form so Windows \ paths match Linux / paths
		norm := strings.ReplaceAll(f, "\\", "/")
		set[norm] = true
	}
	return set
}

// AddRoute adds a processed file and its route data.
// gears is a parallel slice of gear states (same length as points); may be nil for legacy data.
// rawParkCount/rawFrameCount are pre-dedup counts for accurate park time estimation.
func (s *Store) AddRoute(relativePath, dateDir string, points []GPSPoint, gears []uint8, rawParkCount, rawFrameCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.ProcessedFiles = append(s.data.ProcessedFiles, relativePath)
	if len(points) > 0 {
		s.data.Routes = append(s.data.Routes, Route{
			File:          relativePath,
			Date:          dateDir,
			Points:        points,
			GearStates:    gears,
			RawParkCount:  rawParkCount,
			RawFrameCount: rawFrameCount,
		})
	}
}

// MarkProcessed marks a file as processed without adding route data (e.g. no GPS found).
func (s *Store) MarkProcessed(relativePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.ProcessedFiles = append(s.data.ProcessedFiles, relativePath)
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

// ArchivePath returns the path where drive data is backed up on the archive mount.
const archiveDataPath = "/mnt/archive/drive-data.json"

// SyncToArchive copies the local drive data file to the archive mount.
// This is best-effort — it silently returns nil if the archive is not mounted.
func (s *Store) SyncToArchive() error {
	// Check if archive is mounted
	if _, err := os.Stat("/mnt/archive"); err != nil {
		return nil
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
	return os.Rename(tmp, archiveDataPath)
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
