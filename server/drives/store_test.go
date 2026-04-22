package drives

import (
	"path/filepath"
	"sort"
	"testing"
)

// newStore returns a fresh *Store backed by a SQLite DB in a tempdir.
// The store is loaded (migrate ran) and ready for AddRoute/etc. calls.
func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "test.db"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

// sampleRoute returns a Route populated with realistic-but-small data so
// tests assert the full AddRoute -> GetRoutes round-trip including all
// parallel slices and scalar counts.
func sampleRoute(file, date string) Route {
	return Route{
		File:            file,
		Date:            date,
		Points:          []GPSPoint{{40.7, -74.0}, {40.71, -74.01}, {40.72, -74.02}},
		GearStates:      []uint8{1, 1, 0},
		AutopilotStates: []uint8{0, 1, 1},
		Speeds:          []float32{10.5, 11.0, 0.0},
		AccelPositions:  []float32{0.2, 0.3, 0.0},
		RawParkCount:    1,
		RawFrameCount:   3,
		GearRuns:        []GearRun{{Gear: 1, Frames: 2}, {Gear: 0, Frames: 1}},
	}
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

func TestStore_LoadOnFreshDBSucceeds(t *testing.T) {
	s := newStore(t)
	if s.RouteCount() != 0 {
		t.Errorf("fresh store RouteCount = %d, want 0", s.RouteCount())
	}
	if s.ProcessedCount() != 0 {
		t.Errorf("fresh store ProcessedCount = %d, want 0", s.ProcessedCount())
	}
}

func TestStore_LoadIsIdempotent(t *testing.T) {
	s := newStore(t)
	r := sampleRoute("2026-04-20/clip-front.mp4", "2026-04-20_14-30-00")
	s.AddRoute(r.File, r.Date, r.Points, r.GearStates, r.AutopilotStates, r.Speeds, r.AccelPositions, r.RawParkCount, r.RawFrameCount, r.GearRuns)

	// Re-loading must not lose data.
	if err := s.Load(); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := s.RouteCount(); got != 1 {
		t.Errorf("RouteCount after reload = %d, want 1", got)
	}
}

func TestStore_SaveDoesNotError(t *testing.T) {
	s := newStore(t)
	// Save on an SQL-backed store is a wal_checkpoint hint, should never error.
	for i := 0; i < 3; i++ {
		if err := s.Save(); err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}
}

// -----------------------------------------------------------------------------
// AddRoute + MarkProcessed + Counts
// -----------------------------------------------------------------------------

func TestStore_AddRouteInsertsRowAndProcessed(t *testing.T) {
	s := newStore(t)
	r := sampleRoute("2026-04-20/a.mp4", "2026-04-20_14-30-00")
	s.AddRoute(r.File, r.Date, r.Points, r.GearStates, r.AutopilotStates, r.Speeds, r.AccelPositions, r.RawParkCount, r.RawFrameCount, r.GearRuns)

	if s.RouteCount() != 1 {
		t.Errorf("RouteCount = %d, want 1", s.RouteCount())
	}
	if s.ProcessedCount() != 1 {
		t.Errorf("ProcessedCount = %d, want 1 (AddRoute must add to processed_files)", s.ProcessedCount())
	}
}

func TestStore_AddRouteUpsertsOnSameFile(t *testing.T) {
	s := newStore(t)
	r1 := sampleRoute("2026-04-20/a.mp4", "2026-04-20_14-30-00")
	s.AddRoute(r1.File, r1.Date, r1.Points, r1.GearStates, r1.AutopilotStates, r1.Speeds, r1.AccelPositions, r1.RawParkCount, r1.RawFrameCount, r1.GearRuns)

	// Second AddRoute with same file: different content; must replace, not duplicate.
	newPoints := []GPSPoint{{1, 1}, {2, 2}}
	s.AddRoute(r1.File, r1.Date, newPoints, nil, nil, nil, nil, 0, 0, nil)

	if s.RouteCount() != 1 {
		t.Errorf("RouteCount = %d, want 1 (upsert, not append)", s.RouteCount())
	}
	routes := s.GetRoutes()
	if len(routes[0].Points) != 2 {
		t.Errorf("upserted Points len = %d, want 2", len(routes[0].Points))
	}
	if routes[0].Points[0] != (GPSPoint{1, 1}) {
		t.Errorf("upserted Points[0] = %v, want {1,1}", routes[0].Points[0])
	}
}

func TestStore_AddRouteEmptyPointsMarksProcessedOnly(t *testing.T) {
	// A clip with no GPS still counts as processed (so we don't keep
	// re-extracting it) but doesn't produce a routes row.
	s := newStore(t)
	s.AddRoute("clip.mp4", "2026-04-20_14-30-00", nil, nil, nil, nil, nil, 0, 0, nil)
	if s.RouteCount() != 0 {
		t.Errorf("empty-points AddRoute: RouteCount = %d, want 0", s.RouteCount())
	}
	if s.ProcessedCount() != 1 {
		t.Errorf("empty-points AddRoute: ProcessedCount = %d, want 1", s.ProcessedCount())
	}
}

func TestStore_MarkProcessed(t *testing.T) {
	s := newStore(t)
	s.MarkProcessed("no-gps.mp4")
	if s.ProcessedCount() != 1 {
		t.Errorf("ProcessedCount = %d, want 1", s.ProcessedCount())
	}
	// Repeated MarkProcessed must be idempotent.
	s.MarkProcessed("no-gps.mp4")
	if s.ProcessedCount() != 1 {
		t.Errorf("dup MarkProcessed: ProcessedCount = %d, want 1", s.ProcessedCount())
	}
}

func TestStore_ProcessedSetNormalizesBackslashes(t *testing.T) {
	// Windows-style paths must be normalized so the processor's lookup
	// (which uses forward slashes) finds them.
	s := newStore(t)
	s.MarkProcessed(`2026-04-20\clip.mp4`)
	set := s.ProcessedSet()
	if !set["2026-04-20/clip.mp4"] {
		t.Errorf("ProcessedSet did not normalize backslash: %v", set)
	}
}

// -----------------------------------------------------------------------------
// Round-trip fidelity
// -----------------------------------------------------------------------------

func TestStore_RouteRoundTripPreservesAllFields(t *testing.T) {
	s := newStore(t)
	in := sampleRoute("round-trip.mp4", "2026-04-20_14-30-00")
	s.AddRoute(in.File, in.Date, in.Points, in.GearStates, in.AutopilotStates, in.Speeds, in.AccelPositions, in.RawParkCount, in.RawFrameCount, in.GearRuns)

	routes := s.GetRoutes()
	if len(routes) != 1 {
		t.Fatalf("routes len = %d", len(routes))
	}
	got := routes[0]

	if got.File != in.File {
		t.Errorf("File: got %q, want %q", got.File, in.File)
	}
	if got.Date != in.Date {
		t.Errorf("Date: got %q, want %q", got.Date, in.Date)
	}
	if len(got.Points) != len(in.Points) {
		t.Errorf("Points len: got %d, want %d", len(got.Points), len(in.Points))
	}
	for i := range in.Points {
		if got.Points[i] != in.Points[i] {
			t.Errorf("Points[%d]: got %v, want %v", i, got.Points[i], in.Points[i])
		}
	}
	if len(got.GearStates) != len(in.GearStates) || got.GearStates[0] != in.GearStates[0] {
		t.Errorf("GearStates: got %v, want %v", got.GearStates, in.GearStates)
	}
	if len(got.AutopilotStates) != len(in.AutopilotStates) {
		t.Errorf("AutopilotStates len mismatch")
	}
	if len(got.Speeds) != len(in.Speeds) || got.Speeds[0] != in.Speeds[0] {
		t.Errorf("Speeds: got %v, want %v", got.Speeds, in.Speeds)
	}
	if got.RawParkCount != in.RawParkCount {
		t.Errorf("RawParkCount: got %d, want %d", got.RawParkCount, in.RawParkCount)
	}
	if got.RawFrameCount != in.RawFrameCount {
		t.Errorf("RawFrameCount: got %d, want %d", got.RawFrameCount, in.RawFrameCount)
	}
	if len(got.GearRuns) != len(in.GearRuns) {
		t.Errorf("GearRuns len: got %d, want %d", len(got.GearRuns), len(in.GearRuns))
	}
	for i := range in.GearRuns {
		if got.GearRuns[i] != in.GearRuns[i] {
			t.Errorf("GearRuns[%d]: got %v, want %v", i, got.GearRuns[i], in.GearRuns[i])
		}
	}
}

func TestStore_WithRoutesIterates(t *testing.T) {
	s := newStore(t)
	s.AddRoute("a.mp4", "2026-04-20_14-30-00", []GPSPoint{{1, 1}}, nil, nil, nil, nil, 0, 0, nil)
	s.AddRoute("b.mp4", "2026-04-20_15-00-00", []GPSPoint{{2, 2}}, nil, nil, nil, nil, 0, 0, nil)

	var files []string
	s.WithRoutes(func(rs []Route) {
		for _, r := range rs {
			files = append(files, r.File)
		}
	})
	sort.Strings(files)
	if len(files) != 2 || files[0] != "a.mp4" || files[1] != "b.mp4" {
		t.Errorf("WithRoutes visited: %v", files)
	}
}

// -----------------------------------------------------------------------------
// ReplaceData / GetData
// -----------------------------------------------------------------------------

func TestStore_ReplaceDataWipesAndSeeds(t *testing.T) {
	s := newStore(t)
	// Seed some state that ReplaceData must clobber.
	s.AddRoute("old.mp4", "2026-04-20_14-30-00", []GPSPoint{{1, 1}}, nil, nil, nil, nil, 0, 0, nil)
	s.SetDriveTags("2026-04-20T14:30:00", []string{"old-tag"})

	// Upload new state.
	newData := StoreData{
		ProcessedFiles: []string{"new-a.mp4", "new-b.mp4"},
		Routes: []Route{
			{File: "new-a.mp4", Date: "2026-04-21_09-00-00", Points: []GPSPoint{{3, 3}}},
		},
		DriveTags: map[string][]string{
			"2026-04-21T09:00:00": {"work", "commute"},
		},
	}
	s.ReplaceData(newData)

	if s.RouteCount() != 1 {
		t.Errorf("RouteCount after Replace = %d, want 1", s.RouteCount())
	}
	if s.ProcessedCount() != 2 {
		t.Errorf("ProcessedCount after Replace = %d, want 2", s.ProcessedCount())
	}
	gotRoutes := s.GetRoutes()
	if gotRoutes[0].File != "new-a.mp4" {
		t.Errorf("routes[0].File = %q, want new-a.mp4", gotRoutes[0].File)
	}
	tags := s.GetDriveTags("2026-04-21T09:00:00")
	if len(tags) != 2 {
		t.Errorf("new tags: %v (want 2)", tags)
	}
	// Old state must be gone.
	if len(s.GetDriveTags("2026-04-20T14:30:00")) != 0 {
		t.Error("old tag was not wiped")
	}
}

func TestStore_GetDataReflectsState(t *testing.T) {
	s := newStore(t)
	s.AddRoute("a.mp4", "2026-04-20_14-30-00", []GPSPoint{{40.7, -74.0}}, nil, nil, nil, nil, 0, 0, nil)
	s.MarkProcessed("no-gps.mp4")
	s.SetDriveTags("2026-04-20T14:30:00", []string{"work"})

	data := s.GetData()
	if len(data.Routes) != 1 {
		t.Errorf("GetData.Routes: got %d, want 1", len(data.Routes))
	}
	// ProcessedFiles should contain both "a.mp4" and "no-gps.mp4"
	if len(data.ProcessedFiles) != 2 {
		t.Errorf("GetData.ProcessedFiles: got %d entries, want 2", len(data.ProcessedFiles))
	}
	if len(data.DriveTags["2026-04-20T14:30:00"]) != 1 {
		t.Errorf("GetData.DriveTags: %v", data.DriveTags)
	}
}

// -----------------------------------------------------------------------------
// Tags
// -----------------------------------------------------------------------------

func TestStore_SetAndGetDriveTags(t *testing.T) {
	s := newStore(t)
	s.SetDriveTags("2026-04-20T14:30:00", []string{"work", "commute"})

	got := s.GetDriveTags("2026-04-20T14:30:00")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "commute" || got[1] != "work" {
		t.Errorf("tags: got %v, want [commute work]", got)
	}

	// Empty slice deletes.
	s.SetDriveTags("2026-04-20T14:30:00", nil)
	if tags := s.GetDriveTags("2026-04-20T14:30:00"); len(tags) != 0 {
		t.Errorf("after empty set: %v (want none)", tags)
	}
}

func TestStore_SetDriveTagsReplacesExisting(t *testing.T) {
	s := newStore(t)
	s.SetDriveTags("k", []string{"a", "b", "c"})
	s.SetDriveTags("k", []string{"x"})
	got := s.GetDriveTags("k")
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("replace: got %v, want [x]", got)
	}
}

func TestStore_GetAllDriveTags(t *testing.T) {
	s := newStore(t)
	s.SetDriveTags("a", []string{"work"})
	s.SetDriveTags("b", []string{"home", "errand"})
	all := s.GetAllDriveTags()
	if len(all) != 2 {
		t.Fatalf("GetAllDriveTags len = %d, want 2", len(all))
	}
	if len(all["a"]) != 1 || all["a"][0] != "work" {
		t.Errorf("a: %v", all["a"])
	}
	sort.Strings(all["b"])
	if len(all["b"]) != 2 || all["b"][0] != "errand" || all["b"][1] != "home" {
		t.Errorf("b: %v", all["b"])
	}
}

func TestStore_GetAllTagNamesDedupsAndSorts(t *testing.T) {
	s := newStore(t)
	s.SetDriveTags("a", []string{"work", "commute"})
	s.SetDriveTags("b", []string{"work", "errand"})
	names := s.GetAllTagNames()
	if len(names) != 3 {
		t.Fatalf("names: %v (want 3 unique)", names)
	}
	if names[0] != "commute" || names[1] != "errand" || names[2] != "work" {
		t.Errorf("names not sorted: %v", names)
	}
}

// -----------------------------------------------------------------------------
// ClearProcessedForReprocess
// -----------------------------------------------------------------------------

func TestStore_ClearProcessedKeepsRoutesAndTags(t *testing.T) {
	s := newStore(t)
	s.AddRoute("a.mp4", "2026-04-20_14-30-00", []GPSPoint{{1, 1}}, nil, nil, nil, nil, 0, 0, nil)
	s.SetDriveTags("k", []string{"work"})

	s.ClearProcessedForReprocess()

	if s.ProcessedCount() != 0 {
		t.Errorf("after clear: ProcessedCount = %d, want 0", s.ProcessedCount())
	}
	if s.RouteCount() != 1 {
		t.Errorf("after clear: RouteCount = %d, want 1 (routes must be preserved)", s.RouteCount())
	}
	if len(s.GetDriveTags("k")) != 1 {
		t.Errorf("after clear: tags lost (must be preserved)")
	}
}

// -----------------------------------------------------------------------------
// Path
// -----------------------------------------------------------------------------

func TestStore_PathReturnsProvidedPath(t *testing.T) {
	s := NewStore("/custom/path/drive-data.db")
	if got := s.Path(); got != "/custom/path/drive-data.db" {
		t.Errorf("Path() = %q", got)
	}
}
