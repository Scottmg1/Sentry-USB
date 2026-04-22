package drives

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJSON writes a marshaled StoreData to the given path. Used to seed
// "existing user upgrading" scenarios for Load().
func writeJSON(t *testing.T, path string, sd StoreData) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_FreshInstallSetsImportMarkerWithoutImporting(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "drive-data.db")

	// Override the JSON-source candidate paths to look in this tempdir.
	// (The production paths point at /mutable; tests need an isolated dir.)
	withImportSourceCandidates(t, []string{
		filepath.Join(dir, "drive-data.json"),
	})

	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// No JSON existed -> meta.imported_from_json_at must still be set so
	// we don't try to import again on subsequent boots.
	v, err := metaGet(context.Background(), s.db, "imported_from_json_at")
	if err != nil {
		t.Fatalf("imported_from_json_at: %v (must be set even on fresh installs)", err)
	}
	if v == "" {
		t.Error("imported_from_json_at is empty")
	}
	if s.RouteCount() != 0 {
		t.Errorf("RouteCount = %d, want 0 (no JSON to import)", s.RouteCount())
	}
}

func TestLoad_FirstBootImportsExistingJSON(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "drive-data.db")
	jsonPath := filepath.Join(dir, "drive-data.json")

	withImportSourceCandidates(t, []string{jsonPath})

	// Seed a JSON the user "had before upgrade".
	writeJSON(t, jsonPath, StoreData{
		ProcessedFiles: []string{"a.mp4", "b.mp4", "no-gps.mp4"},
		Routes: []Route{
			{File: "a.mp4", Date: "2026-04-20_14-30-00", Points: []GPSPoint{{40.7, -74.0}}},
			{File: "b.mp4", Date: "2026-04-21_09-00-00", Points: []GPSPoint{{30, -80}}},
		},
		DriveTags: map[string][]string{"2026-04-20T14:30:00": {"work"}},
	})

	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.RouteCount() != 2 {
		t.Errorf("RouteCount = %d, want 2 (JSON should have been imported)", s.RouteCount())
	}
	if s.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount = %d, want 3", s.ProcessedCount())
	}
	tags := s.GetDriveTags("2026-04-20T14:30:00")
	if len(tags) != 1 || tags[0] != "work" {
		t.Errorf("imported tags: %v", tags)
	}

	// Source JSON must have been renamed to .bak-<ts> after a successful
	// import, never deleted -- it's the user's safety net.
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Errorf("source JSON not renamed/removed after import: %v", err)
	}
	matches, _ := filepath.Glob(jsonPath + ".bak-*")
	if len(matches) == 0 {
		t.Errorf("no .bak-* file created from successful import")
	} else {
		// Sanity: the .bak file should still parse as JSON.
		b, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		var sd StoreData
		if err := json.Unmarshal(b, &sd); err != nil {
			t.Errorf(".bak file is not valid JSON: %v", err)
		}
	}

	// imported_from_json_at must now be set.
	v, err := metaGet(context.Background(), s.db, "imported_from_json_at")
	if err != nil || v == "" {
		t.Errorf("import marker not set: v=%q err=%v", v, err)
	}
}

func TestLoad_DoesNotReimportWhenMarkerPresent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "drive-data.db")
	jsonPath := filepath.Join(dir, "drive-data.json")

	withImportSourceCandidates(t, []string{jsonPath})

	// First load: import 2 routes.
	writeJSON(t, jsonPath, StoreData{
		Routes: []Route{
			{File: "a.mp4", Date: "2026-04-20_14-30-00", Points: []GPSPoint{{1, 1}}},
			{File: "b.mp4", Date: "2026-04-20_14-30-00", Points: []GPSPoint{{2, 2}}},
		},
	})
	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.RouteCount() != 2 {
		t.Fatalf("first load: RouteCount = %d", s.RouteCount())
	}

	// Re-create a JSON at the source path with DIFFERENT contents that,
	// if mistakenly imported, would change the row count. Marker must
	// prevent the second import.
	writeJSON(t, jsonPath, StoreData{
		Routes: []Route{
			{File: "x.mp4", Date: "2026-04-20_14-30-00", Points: []GPSPoint{{9, 9}}},
		},
	})

	if err := s.Load(); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if s.RouteCount() != 2 {
		t.Errorf("RouteCount after re-load = %d, want 2 (must NOT re-import)", s.RouteCount())
	}
}

func TestLoad_SkipsAlreadyMigratedDB(t *testing.T) {
	// If the DB already has the imported_from_json_at marker (someone
	// restored a snapshot, or this is a re-migrated DB from a previous
	// install), Load must not try to import even when a JSON is present.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "drive-data.db")
	jsonPath := filepath.Join(dir, "drive-data.json")

	withImportSourceCandidates(t, []string{jsonPath})

	// Pre-create the DB with the marker set, but otherwise empty.
	pre := NewStore(dbPath)
	if err := pre.Load(); err != nil {
		t.Fatal(err)
	}
	if err := metaSet(context.Background(), pre.db, "imported_from_json_at", "2025-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	// Now stage a JSON that "would" import 99 routes.
	routes := make([]Route, 99)
	for i := range routes {
		routes[i] = Route{File: filepath.Base(jsonPath) + "/r" + string(rune('A'+i%26)), Date: "2026-04-20_14-30-00", Points: []GPSPoint{{float64(i), float64(i)}}}
	}
	writeJSON(t, jsonPath, StoreData{Routes: routes})

	// Re-Load: must not import.
	if err := pre.Load(); err != nil {
		t.Fatal(err)
	}
	if pre.RouteCount() != 0 {
		t.Errorf("RouteCount = %d, want 0 (marker should have prevented import)", pre.RouteCount())
	}
	// The source JSON must still be present (we didn't try to .bak it).
	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("source JSON disappeared: %v", err)
	}
}

func TestLoad_FallsBackToLegacyJSONPath(t *testing.T) {
	// The legacy /root/drive-data.json path is checked when the primary
	// /mutable/drive-data.json is missing.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "drive-data.db")
	primary := filepath.Join(dir, "drive-data.json")
	legacy := filepath.Join(dir, "legacy", "drive-data.json")

	withImportSourceCandidates(t, []string{primary, legacy})

	// Only the legacy one exists.
	writeJSON(t, legacy, StoreData{
		Routes: []Route{
			{File: "legacy.mp4", Date: "2026-04-20_14-30-00", Points: []GPSPoint{{1, 1}}},
		},
	})

	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.RouteCount() != 1 {
		t.Errorf("RouteCount = %d, want 1 (legacy path import)", s.RouteCount())
	}
	// Legacy file should also have been .bak-renamed.
	matches, _ := filepath.Glob(legacy + ".bak-*")
	if len(matches) == 0 {
		t.Error("legacy JSON not renamed to .bak-* after import")
	}
}

func TestLoad_ImportFailureLeavesSourceUntouched(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "drive-data.db")
	jsonPath := filepath.Join(dir, "drive-data.json")

	withImportSourceCandidates(t, []string{jsonPath})

	// Write malformed JSON so importJSON errors out.
	if err := os.WriteFile(jsonPath, []byte(`{ "routes": [garbage`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dbPath)
	err := s.Load()
	if err == nil {
		t.Fatal("Load should have returned the import error")
	}
	if !strings.Contains(err.Error(), "import") {
		t.Errorf("error doesn't mention import: %v", err)
	}

	// Source JSON must still exist (no .bak rename on failure).
	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("source JSON disappeared after failed import: %v", err)
	}
	// And no .bak-* should have been created.
	matches, _ := filepath.Glob(jsonPath + ".bak-*")
	if len(matches) != 0 {
		t.Errorf(".bak file created on failed import: %v", matches)
	}
	// Marker must NOT be set (so next boot retries).
	if _, err := metaGet(context.Background(), s.db, "imported_from_json_at"); err == nil {
		t.Error("imported_from_json_at was set despite failed import")
	}
}
