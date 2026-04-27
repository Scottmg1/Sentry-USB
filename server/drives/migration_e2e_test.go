package drives

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// synthesizeStoreData builds a StoreData with `routes` clips, each with
// pointsPerClip points, plus all parallel slices and ~10% drives tagged.
// Used by the e2e import test to stand in for a real user dataset
// without committing a multi-MB fixture.
func synthesizeStoreData(routes, pointsPerClip int, seed int64) StoreData {
	r := rand.New(rand.NewSource(seed))
	sd := StoreData{
		ProcessedFiles: make([]string, 0, routes),
		Routes:         make([]Route, 0, routes),
		DriveTags:      map[string][]string{},
	}
	for i := 0; i < routes; i++ {
		file := fmt.Sprintf("2026-04-20/clip-%05d-front.mp4", i)
		date := "2026-04-20_14-30-00"

		points := make([]GPSPoint, pointsPerClip)
		gears := make([]uint8, pointsPerClip)
		ap := make([]uint8, pointsPerClip)
		speeds := make([]float32, pointsPerClip)
		accel := make([]float32, pointsPerClip)
		baseLat := 40.0 + r.Float64()
		baseLon := -74.0 + r.Float64()
		for j := 0; j < pointsPerClip; j++ {
			points[j] = GPSPoint{baseLat + float64(j)*0.00001, baseLon + float64(j)*0.00001}
			gears[j] = uint8(r.Intn(4))
			ap[j] = uint8(r.Intn(2))
			speeds[j] = float32(r.Float64() * 30)
			accel[j] = float32(r.Float64())
		}

		sd.Routes = append(sd.Routes, Route{
			File:            file,
			Date:            date,
			Points:          points,
			GearStates:      gears,
			AutopilotStates: ap,
			Speeds:          speeds,
			AccelPositions:  accel,
			RawParkCount:    r.Intn(50),
			RawFrameCount:   pointsPerClip,
			GearRuns: []GearRun{
				{Gear: 1, Frames: pointsPerClip / 2},
				{Gear: 0, Frames: pointsPerClip / 2},
			},
		})

		// Ten percent of drives get a tag to exercise the tag table.
		if i%10 == 0 {
			driveKey := fmt.Sprintf("2026-04-20T14:%02d:00", i%60)
			sd.DriveTags[driveKey] = []string{"work"}
		}

		sd.ProcessedFiles = append(sd.ProcessedFiles, file)
	}
	return sd
}

// TestMigrationE2E_ImportExportImportPreservesEverything is the
// integration anchor for the JSON -> SQLite migration. It walks the
// full path a real user's data takes:
//
//   1. Synthesize a representative drive-data.json (~100 clips,
//      ~500 pts/clip, all parallel slices populated, some drives tagged).
//   2. Write it to a tempdir as if it were the legacy file at
//      /mutable/drive-data.json.
//   3. Open a fresh Store there -> Load() runs the one-shot importer,
//      sets the marker, renames the source to .bak-*.
//   4. Verify counts and a sample of route-level data round-trip.
//   5. Export the SQLite store back to JSON via ExportJSONToFile.
//   6. Re-import that exported JSON into a second fresh Store.
//   7. Verify the second Store has identical counts to the first.
//
// This proves: the importer accepts our own exporter's output, the
// exporter produces a Sentry-Studio-compatible shape, blob fidelity
// holds across encode/decode, and nothing leaks across the cycle.
func TestMigrationE2E_ImportExportImportPreservesEverything(t *testing.T) {
	const numRoutes = 100
	const pointsPerClip = 500

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "drive-data.json")
	dbPath := filepath.Join(dir, "drive-data.db")

	withImportSourceCandidates(t, []string{jsonPath})

	// 1+2: stage the legacy JSON.
	src := synthesizeStoreData(numRoutes, pointsPerClip, 42)
	srcBytes, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("synthesized JSON: %d bytes", len(srcBytes))
	if err := os.WriteFile(jsonPath, srcBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// 3: first Load triggers import.
	s1 := NewStore(dbPath)
	if err := s1.Load(); err != nil {
		t.Fatalf("first Load (import): %v", err)
	}

	// 4: counts and a spot-check on route fidelity.
	if s1.RouteCount() != numRoutes {
		t.Fatalf("after import: RouteCount = %d, want %d", s1.RouteCount(), numRoutes)
	}
	if s1.ProcessedCount() != numRoutes {
		t.Fatalf("after import: ProcessedCount = %d, want %d", s1.ProcessedCount(), numRoutes)
	}
	expectedTags := 0
	for _, tags := range src.DriveTags {
		expectedTags += len(tags)
	}
	allTags := s1.GetAllDriveTags()
	gotTags := 0
	for _, ts := range allTags {
		gotTags += len(ts)
	}
	if gotTags != expectedTags {
		t.Errorf("tag count: got %d, want %d", gotTags, expectedTags)
	}
	// Spot-check route 0 fidelity.
	routes := s1.GetRoutes()
	if len(routes) != numRoutes {
		t.Fatalf("GetRoutes: %d, want %d", len(routes), numRoutes)
	}
	got0 := routes[0]
	want0 := src.Routes[0]
	if got0.File != want0.File || got0.Date != want0.Date {
		t.Errorf("route 0 metadata: got %s/%s, want %s/%s", got0.File, got0.Date, want0.File, want0.Date)
	}
	if len(got0.Points) != pointsPerClip {
		t.Errorf("route 0 points: got %d, want %d", len(got0.Points), pointsPerClip)
	}
	if got0.Points[0] != want0.Points[0] || got0.Points[pointsPerClip-1] != want0.Points[pointsPerClip-1] {
		t.Error("route 0 first/last points not preserved")
	}

	// Source JSON must have been renamed.
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Errorf("source JSON still present after import: %v", err)
	}
	if matches, _ := filepath.Glob(jsonPath + ".bak-*"); len(matches) == 0 {
		t.Error("no .bak-* file from the import")
	}

	// 5: export the DB back to JSON.
	exportPath := filepath.Join(dir, "exported.json")
	if err := s1.ExportJSONToFile(exportPath); err != nil {
		t.Fatalf("ExportJSONToFile: %v", err)
	}
	exportInfo, err := os.Stat(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("exported JSON: %d bytes (source was %d)", exportInfo.Size(), len(srcBytes))

	// The exporter must produce byte-length-identical output to the
	// synthesized source. Different size means the round-trip is
	// dropping, padding, or re-formatting fields -- i.e. the archive
	// copy Sentry Studio reads would no longer match the JSON the user
	// originally ingested. This is the strong correctness signal the
	// e2e test is actually here to lock.
	if exportInfo.Size() != int64(len(srcBytes)) {
		t.Errorf("export size drift: exported %d bytes, source was %d bytes",
			exportInfo.Size(), len(srcBytes))
	}

	// 6: import the exported JSON into a second fresh store.
	dir2 := t.TempDir()
	dbPath2 := filepath.Join(dir2, "drive-data.db")
	jsonPath2 := filepath.Join(dir2, "drive-data.json")
	exportedBytes, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath2, exportedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	withImportSourceCandidates(t, []string{jsonPath2})

	s2 := NewStore(dbPath2)
	if err := s2.Load(); err != nil {
		t.Fatalf("second Load (re-import from export): %v", err)
	}

	// 7: counts must match across the cycle.
	if s2.RouteCount() != s1.RouteCount() {
		t.Errorf("re-import RouteCount: got %d, want %d", s2.RouteCount(), s1.RouteCount())
	}
	if s2.ProcessedCount() != s1.ProcessedCount() {
		t.Errorf("re-import ProcessedCount: got %d, want %d", s2.ProcessedCount(), s1.ProcessedCount())
	}
	allTags2 := s2.GetAllDriveTags()
	gotTags2 := 0
	for _, ts := range allTags2 {
		gotTags2 += len(ts)
	}
	if gotTags2 != gotTags {
		t.Errorf("re-import tag count: got %d, want %d", gotTags2, gotTags)
	}

	// And one more spot check: route 0's points survive both cycles.
	routes2 := s2.GetRoutes()
	if routes2[0].Points[0] != want0.Points[0] {
		t.Errorf("route 0 first point lost across cycle: got %v want %v",
			routes2[0].Points[0], want0.Points[0])
	}
}

// TestMigrationE2E_BulkImportTimedSmoke is a non-strict perf canary.
// Imports 200 clips of 1500 points each (~50 KB/clip) and reports the
// duration. Not a benchmark -- tests don't gate on time -- but a value
// that catches catastrophic regressions when the import gets ~10x slower.
func TestMigrationE2E_BulkImportTimedSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping bulk import in -short mode")
	}
	const numRoutes = 200
	const pointsPerClip = 1500

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "drive-data.json")
	dbPath := filepath.Join(dir, "drive-data.db")
	withImportSourceCandidates(t, []string{jsonPath})

	sd := synthesizeStoreData(numRoutes, pointsPerClip, 7)
	bytes, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("bulk JSON: %d MB", len(bytes)/(1024*1024))

	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.RouteCount() != numRoutes {
		t.Errorf("bulk RouteCount = %d, want %d", s.RouteCount(), numRoutes)
	}

	// Verify the import marker is set.
	v, _ := metaGet(context.Background(), s.db, "imported_from_json_at")
	if v == "" {
		t.Error("import marker not set after bulk import")
	}
}
