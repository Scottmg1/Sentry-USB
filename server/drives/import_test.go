package drives

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// marshalStoreData is a small helper: produces a drive-data.json byte
// payload in the same shape that Sentry Studio reads from the archive.
// We always use encoding/json (not any internal writer) so the importer
// can't accidentally shadow a real-world input format.
func marshalStoreData(t *testing.T, d StoreData) []byte {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestImportJSON_StripsUTF8BOM locks the fix for first-boot failures
// on users whose drive-data.json was touched on Windows and carries a
// leading UTF-8 BOM (EF BB BF). json.Decoder does not strip it, so
// without the importer's explicit BOM skip the whole Load() would fail
// with "expected top-level object" and the service wouldn't start.
func TestImportJSON_StripsUTF8BOM(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Minimal-but-real payload so we also prove subsequent decoding
	// works after the BOM is consumed (not just that the opening
	// token parses).
	payload := marshalStoreData(t, StoreData{
		ProcessedFiles: []string{"bom.mp4"},
	})
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, payload...)

	stats, err := importJSON(ctx, db, bytes.NewReader(withBOM), nil)
	if err != nil {
		t.Fatalf("importJSON with BOM: %v", err)
	}
	if stats.ProcessedFiles != 1 {
		t.Errorf("ProcessedFiles after BOM import = %d, want 1", stats.ProcessedFiles)
	}
}

func TestImportJSON_EmptyPayload(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	stats, err := importJSON(ctx, db, bytes.NewReader([]byte(`{}`)), nil)
	if err != nil {
		t.Fatalf("importJSON empty: %v", err)
	}
	if stats.Routes != 0 || stats.ProcessedFiles != 0 || stats.DriveTags != 0 {
		t.Errorf("empty import stats: %+v (want all 0)", stats)
	}
}

func TestImportJSON_ProcessedFilesOnly(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	payload := marshalStoreData(t, StoreData{
		// Windows-shaped third entry: single backslash inside a raw string.
		// normalizePath must convert it to "c/d.mp4" on insert.
		ProcessedFiles: []string{"a.mp4", "b.mp4", `c\d.mp4`},
	})
	stats, err := importJSON(ctx, db, bytes.NewReader(payload), nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.ProcessedFiles != 3 {
		t.Errorf("stats.ProcessedFiles = %d, want 3", stats.ProcessedFiles)
	}

	var got int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM processed_files`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("processed_files row count = %d, want 3", got)
	}
	// Backslash normalization: "c\\d.mp4" becomes "c/d.mp4" on insert.
	var found int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM processed_files WHERE file = 'c/d.mp4'`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Errorf("normalized path not present; query found %d rows", found)
	}
}

func TestImportJSON_RoutesAlsoMarkProcessed(t *testing.T) {
	// A route for file X must also create a processed_files row for X,
	// otherwise re-processing would re-extract the same clip next run.
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	payload := marshalStoreData(t, StoreData{
		Routes: []Route{
			{File: "a.mp4", Date: "2026-04-20_14-30-00", Points: []GPSPoint{{1, 2}, {3, 4}}},
			{File: "b.mp4", Date: "2026-04-20_15-00-00", Points: []GPSPoint{{5, 6}}},
		},
	})
	stats, err := importJSON(ctx, db, bytes.NewReader(payload), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Routes != 2 {
		t.Errorf("stats.Routes = %d, want 2", stats.Routes)
	}
	if stats.ProcessedFiles != 2 {
		t.Errorf("stats.ProcessedFiles = %d, want 2 (routes must mark-processed)", stats.ProcessedFiles)
	}

	var n int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes`).Scan(&n)
	if n != 2 {
		t.Errorf("routes rows = %d, want 2", n)
	}
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM processed_files`).Scan(&n)
	if n != 2 {
		t.Errorf("processed_files rows = %d, want 2", n)
	}
}

func TestImportJSON_DriveTags(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	payload := marshalStoreData(t, StoreData{
		DriveTags: map[string][]string{
			"2026-04-20T14:30:00": {"work", "commute"},
			"2026-04-21T09:00:00": {"errand"},
		},
	})
	stats, err := importJSON(ctx, db, bytes.NewReader(payload), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DriveTags != 3 {
		t.Errorf("stats.DriveTags = %d, want 3 (2 + 1)", stats.DriveTags)
	}
}

func TestImportJSON_FullRoundTripPreservesAllFields(t *testing.T) {
	// GetData -> marshal -> importJSON into a fresh DB -> GetData should
	// yield the same payload. This is the Sentry Studio archive-read
	// compat anchor: whatever we can export, we can re-import.
	src := newStore(t)
	src.AddRoute("round-trip.mp4", "2026-04-20_14-30-00",
		[]GPSPoint{{40.7, -74.0}, {40.71, -74.01}},
		[]uint8{1, 1}, []uint8{0, 1},
		[]float32{10.5, 11.0}, []float32{0.2, 0.3},
		1, 3,
		[]GearRun{{Gear: 1, Frames: 2}},
	)
	src.MarkProcessed("no-gps.mp4")
	src.SetDriveTags("2026-04-20T14:30:00", []string{"work"})

	payload := marshalStoreData(t, src.GetData())

	// Second store is fresh.
	dst := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := importJSON(ctx, dst, bytes.NewReader(payload), nil); err != nil {
		t.Fatal(err)
	}

	// Counts match.
	var n int
	dst.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes`).Scan(&n)
	if n != 1 {
		t.Errorf("routes = %d, want 1", n)
	}
	dst.QueryRowContext(ctx, `SELECT COUNT(*) FROM processed_files`).Scan(&n)
	if n != 2 {
		t.Errorf("processed_files = %d, want 2", n)
	}
	dst.QueryRowContext(ctx, `SELECT COUNT(*) FROM drive_tags`).Scan(&n)
	if n != 1 {
		t.Errorf("drive_tags = %d, want 1", n)
	}

	// Blob contents survive the round-trip.
	var pb []byte
	dst.QueryRowContext(ctx, `SELECT points_blob FROM routes WHERE file = ?`, "round-trip.mp4").Scan(&pb)
	points, err := decodePoints(pb)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0] != (GPSPoint{40.7, -74.0}) {
		t.Errorf("points not preserved: %v", points)
	}
}

func TestImportJSON_MalformedInputRollsBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Trailing garbage after routes array closes: not valid JSON.
	bad := `{"routes":[{"file":"a.mp4","date":"2026-04-20_14-30-00","points":[[1,2]]}], not-valid}`
	_, err := importJSON(ctx, db, strings.NewReader(bad), nil)
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
	var n int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes`).Scan(&n)
	if n != 0 {
		t.Errorf("rollback failed: routes rows = %d, want 0", n)
	}
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM processed_files`).Scan(&n)
	if n != 0 {
		t.Errorf("rollback failed: processed_files rows = %d, want 0", n)
	}
}

func TestImportJSON_ProgressCallbackFires(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Enough routes to cross at least one progress checkpoint.
	const n = 250
	routes := make([]Route, n)
	for i := 0; i < n; i++ {
		routes[i] = Route{
			File:   fmt.Sprintf("2026-04-20/clip-%04d.mp4", i),
			Date:   "2026-04-20_14-30-00",
			Points: []GPSPoint{{float64(i), float64(i)}},
		}
	}
	payload := marshalStoreData(t, StoreData{Routes: routes})

	var calls int
	var lastCount int
	stats, err := importJSON(ctx, db, bytes.NewReader(payload), func(routesImported int) {
		calls++
		lastCount = routesImported
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Routes != n {
		t.Errorf("stats.Routes = %d, want %d", stats.Routes, n)
	}
	if calls == 0 {
		t.Errorf("progress callback never fired for %d routes", n)
	}
	if lastCount > n {
		t.Errorf("last progress call reported %d routes (> total %d)", lastCount, n)
	}
}

func TestImportJSON_LargeRoutePayloadDecodes(t *testing.T) {
	// Representative-sized clip: 2000 points, all parallel slices present.
	const pts = 2000
	points := make([]GPSPoint, pts)
	gears := make([]uint8, pts)
	ap := make([]uint8, pts)
	speeds := make([]float32, pts)
	accel := make([]float32, pts)
	for i := 0; i < pts; i++ {
		points[i] = GPSPoint{40.0 + float64(i)*0.00001, -74.0 + float64(i)*0.00001}
		gears[i] = uint8(i % 4)
		ap[i] = uint8((i / 100) % 2)
		speeds[i] = float32(i) * 0.1
		accel[i] = float32(i) * 0.001
	}
	payload := marshalStoreData(t, StoreData{
		Routes: []Route{{
			File: "big.mp4", Date: "2026-04-20_14-30-00",
			Points: points, GearStates: gears, AutopilotStates: ap,
			Speeds: speeds, AccelPositions: accel,
			RawParkCount: 100, RawFrameCount: pts,
			GearRuns: []GearRun{{Gear: 1, Frames: 1500}, {Gear: 0, Frames: 500}},
		}},
	})

	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := importJSON(ctx, db, bytes.NewReader(payload), nil); err != nil {
		t.Fatal(err)
	}

	var pb []byte
	if err := db.QueryRowContext(ctx, `SELECT points_blob FROM routes WHERE file='big.mp4'`).Scan(&pb); err != nil {
		t.Fatal(err)
	}
	got, err := decodePoints(pb)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != pts {
		t.Fatalf("points len = %d, want %d", len(got), pts)
	}
	if got[0] != points[0] || got[pts-1] != points[pts-1] {
		t.Error("first/last point mismatch after import")
	}
}

func TestImportJSON_HandlesKeysInAnyOrder(t *testing.T) {
	// JSON object key order is not significant — the importer must
	// handle driveTags coming before routes coming before processedFiles.
	payload := `{
		"driveTags": {"k1": ["t1"]},
		"routes": [{"file":"r.mp4","date":"2026-04-20_14-30-00","points":[[1,2]]}],
		"processedFiles": ["p.mp4"]
	}`
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	stats, err := importJSON(ctx, db, strings.NewReader(payload), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Routes != 1 || stats.ProcessedFiles != 2 || stats.DriveTags != 1 {
		t.Errorf("stats = %+v", stats)
	}
}
