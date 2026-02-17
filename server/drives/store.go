package drives

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// DefaultDataPath is where drive data is stored on the Pi.
const DefaultDataPath = "/root/drive-data.json"

// Route represents GPS data extracted from a single front-camera clip.
type Route struct {
	File   string     `json:"file"`
	Date   string     `json:"date"`
	Points []GPSPoint `json:"points"`
}

// StoreData is the persistent JSON structure.
type StoreData struct {
	ProcessedFiles []string `json:"processedFiles"`
	Routes         []Route  `json:"routes"`
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
func (s *Store) ProcessedSet() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	set := make(map[string]bool, len(s.data.ProcessedFiles))
	for _, f := range s.data.ProcessedFiles {
		set[f] = true
	}
	return set
}

// AddRoute adds a processed file and its route data.
func (s *Store) AddRoute(relativePath, dateDir string, points []GPSPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.ProcessedFiles = append(s.data.ProcessedFiles, relativePath)
	if len(points) > 0 {
		s.data.Routes = append(s.data.Routes, Route{
			File:   relativePath,
			Date:   dateDir,
			Points: points,
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
	return StoreData{
		ProcessedFiles: append([]string{}, s.data.ProcessedFiles...),
		Routes:         append([]Route{}, s.data.Routes...),
	}
}
