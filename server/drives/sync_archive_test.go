package drives

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExportJSONToFile_AtomicWrite(t *testing.T) {
	s := newStore(t)
	s.AddRoute("a.mp4", "2026-04-20_14-30-00", []GPSPoint{{40.7, -74.0}}, nil, nil, nil, nil, 0, 0, nil)
	s.SetDriveTags("2026-04-20T14:30:00", []string{"work"})

	out := filepath.Join(t.TempDir(), "exported.json")
	if err := s.ExportJSONToFile(out); err != nil {
		t.Fatalf("ExportJSONToFile: %v", err)
	}

	// File exists at final path; no leftover .tmp
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("leftover .tmp: %v", err)
	}

	// Re-import elsewhere and verify counts.
	dst := newStore(t)
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var sd StoreData
	if err := json.Unmarshal(b, &sd); err != nil {
		t.Fatalf("exported JSON invalid: %v", err)
	}
	if len(sd.Routes) != 1 {
		t.Errorf("exported routes = %d, want 1", len(sd.Routes))
	}
	if len(sd.DriveTags["2026-04-20T14:30:00"]) != 1 {
		t.Errorf("exported tags missing")
	}
	_ = dst
}

func TestExportJSONToFile_CreatesParentDir(t *testing.T) {
	s := newStore(t)
	out := filepath.Join(t.TempDir(), "deep", "nested", "exported.json")
	if err := s.ExportJSONToFile(out); err != nil {
		t.Fatalf("ExportJSONToFile: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}
