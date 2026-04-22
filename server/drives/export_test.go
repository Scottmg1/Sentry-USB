package drives

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// exportThenImport is a test helper that exports `src` to a byte buffer,
// then imports those bytes into `dst`, then reloads `dst` so the cached
// row-count atomics (RouteCount / ProcessedCount) reflect the newly
// imported rows. Returns the export buffer for shape assertions plus the
// import stats.
func exportThenImport(t *testing.T, src, dst *Store) ([]byte, ImportStats) {
	t.Helper()
	var buf bytes.Buffer
	if err := exportJSON(context.Background(), src.db, &buf); err != nil {
		t.Fatalf("exportJSON: %v", err)
	}
	stats, err := importJSON(context.Background(), dst.db, bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	// importJSON writes directly to the DB and doesn't know about the
	// Store's atomic counters. Call Load() again to refresh them.
	if err := dst.Load(); err != nil {
		t.Fatalf("dst.Load after import: %v", err)
	}
	return buf.Bytes(), stats
}

func TestExportJSON_EmptyStoreProducesValidJSON(t *testing.T) {
	s := newStore(t)
	var buf bytes.Buffer
	if err := exportJSON(context.Background(), s.db, &buf); err != nil {
		t.Fatalf("exportJSON: %v", err)
	}
	// Must be valid JSON that decodes back into a StoreData value.
	var sd StoreData
	if err := json.Unmarshal(buf.Bytes(), &sd); err != nil {
		t.Fatalf("exported bytes don't parse as JSON StoreData: %v\n%s", err, buf.String())
	}
	if len(sd.Routes) != 0 || len(sd.ProcessedFiles) != 0 || len(sd.DriveTags) != 0 {
		t.Errorf("empty-store export decoded to non-empty StoreData: %+v", sd)
	}
}

func TestExportJSON_WritesTopLevelKeys(t *testing.T) {
	// Sentry Studio expects the top-level object to look like StoreData.
	// Pin the public shape so future refactors don't silently drop fields.
	s := newStore(t)
	s.AddRoute("a.mp4", "2026-04-20_14-30-00", []GPSPoint{{1, 2}}, nil, nil, nil, nil, 0, 0, nil)
	s.MarkProcessed("b.mp4")
	s.SetDriveTags("2026-04-20T14:30:00", []string{"work"})

	var buf bytes.Buffer
	if err := exportJSON(context.Background(), s.db, &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, key := range []string{`"processedFiles"`, `"routes"`, `"driveTags"`} {
		if !strings.Contains(got, key) {
			t.Errorf("exported JSON missing key %s:\n%s", key, got)
		}
	}
}

func TestExportImport_FullRoundTrip(t *testing.T) {
	src := newStore(t)
	src.AddRoute("a.mp4", "2026-04-20_14-30-00",
		[]GPSPoint{{40.7, -74.0}, {40.71, -74.01}},
		[]uint8{1, 1}, []uint8{0, 1},
		[]float32{10.5, 11.0}, []float32{0.2, 0.3},
		1, 3,
		[]GearRun{{Gear: 1, Frames: 2}},
	)
	src.AddRoute("b.mp4", "2026-04-21_09-00-00",
		[]GPSPoint{{30, -80}},
		nil, nil, nil, nil, 0, 1, nil,
	)
	src.MarkProcessed("no-gps.mp4")
	src.SetDriveTags("2026-04-20T14:30:00", []string{"work", "commute"})
	src.SetDriveTags("2026-04-21T09:00:00", []string{"errand"})

	dst := newStore(t)
	_, stats := exportThenImport(t, src, dst)

	if stats.Routes != 2 {
		t.Errorf("imported Routes = %d, want 2", stats.Routes)
	}
	if stats.ProcessedFiles != 3 {
		t.Errorf("imported ProcessedFiles = %d, want 3 (a + b + no-gps)", stats.ProcessedFiles)
	}
	if stats.DriveTags != 3 {
		t.Errorf("imported DriveTags = %d, want 3 (work+commute+errand)", stats.DriveTags)
	}

	if dst.RouteCount() != 2 {
		t.Errorf("dst.RouteCount = %d, want 2", dst.RouteCount())
	}
	if dst.ProcessedCount() != 3 {
		t.Errorf("dst.ProcessedCount = %d, want 3", dst.ProcessedCount())
	}
	// Blob fidelity: check the multi-point route.
	routes := dst.GetRoutes()
	for _, r := range routes {
		if r.File != "a.mp4" {
			continue
		}
		if len(r.Points) != 2 || r.Points[0] != (GPSPoint{40.7, -74.0}) {
			t.Errorf("points lost in export/import: %v", r.Points)
		}
		if len(r.GearStates) != 2 || r.GearStates[0] != 1 {
			t.Errorf("gear states lost: %v", r.GearStates)
		}
		if len(r.GearRuns) != 1 || r.GearRuns[0] != (GearRun{Gear: 1, Frames: 2}) {
			t.Errorf("gear runs lost: %v", r.GearRuns)
		}
	}
}

func TestExportJSON_RoutesSortedByFile(t *testing.T) {
	// Deterministic output order makes the export diff-able against
	// golden files and simplifies reproducibility for the archive copy.
	s := newStore(t)
	for _, f := range []string{"z.mp4", "a.mp4", "m.mp4"} {
		s.AddRoute(f, "2026-04-20_14-30-00", []GPSPoint{{1, 1}}, nil, nil, nil, nil, 0, 0, nil)
	}
	var buf bytes.Buffer
	if err := exportJSON(context.Background(), s.db, &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Verify "a.mp4" appears before "m.mp4" appears before "z.mp4".
	aIdx := strings.Index(got, `"a.mp4"`)
	mIdx := strings.Index(got, `"m.mp4"`)
	zIdx := strings.Index(got, `"z.mp4"`)
	if aIdx < 0 || mIdx < 0 || zIdx < 0 {
		t.Fatalf("all three file names should appear; indices = %d %d %d", aIdx, mIdx, zIdx)
	}
	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Errorf("routes not emitted in sorted file order: a=%d m=%d z=%d\n%s", aIdx, mIdx, zIdx, got)
	}
}

func TestExportJSON_IsValidStandaloneJSON(t *testing.T) {
	// Exported bytes must parse as valid JSON even with a large, mixed
	// dataset (many routes + tags). Catches any missing comma or bracket.
	s := newStore(t)
	for i := 0; i < 50; i++ {
		f := "2026-04-20/clip-" + string(rune('a'+i%26)) + "-"
		s.AddRoute(f, "2026-04-20_14-30-00",
			[]GPSPoint{{float64(i), float64(i) * 2}},
			[]uint8{1}, nil, []float32{float32(i)}, nil, 0, 1, nil)
	}
	s.MarkProcessed("skip-1.mp4")
	s.MarkProcessed("skip-2.mp4")
	s.SetDriveTags("k1", []string{"a", "b"})
	s.SetDriveTags("k2", []string{"c"})

	var buf bytes.Buffer
	if err := exportJSON(context.Background(), s.db, &buf); err != nil {
		t.Fatal(err)
	}
	var any map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &any); err != nil {
		t.Fatalf("exported JSON is invalid: %v\nfirst 500 bytes:\n%s", err, buf.String()[:min(len(buf.Bytes()), 500)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
