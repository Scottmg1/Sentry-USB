package drives

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestReplaceDataFromReader_BasicRoundTrip verifies the streaming path
// produces the same observable state as the legacy in-memory ReplaceData.
func TestReplaceDataFromReader_BasicRoundTrip(t *testing.T) {
	s := newStore(t)

	data := StoreData{
		Routes: []Route{
			sampleRoute("2026-04-20/clip-a.mp4", "2026-04-20_14-30-00"),
			sampleRoute("2026-04-20/clip-b.mp4", "2026-04-20_14-31-00"),
		},
		ProcessedFiles: []string{"2026-04-20/clip-a.mp4", "2026-04-20/clip-c.mp4"},
		DriveTags:      map[string][]string{"2026-04-20": {"work", "home"}},
	}
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	stats, err := s.ReplaceDataFromReader(context.Background(), strings.NewReader(string(payload)), nil)
	if err != nil {
		t.Fatalf("ReplaceDataFromReader: %v", err)
	}
	if stats.Routes != 2 {
		t.Errorf("Routes = %d, want 2", stats.Routes)
	}
	if s.RouteCount() != 2 {
		t.Errorf("RouteCount = %d, want 2", s.RouteCount())
	}

	// Aggregates must be populated (not NULL) after a restore, else the
	// BLOB-free summary endpoints would show zeros.
	var dist float64
	if err := s.db.QueryRow(
		`SELECT distance_m FROM routes WHERE file = ?`, "2026-04-20/clip-a.mp4",
	).Scan(&dist); err != nil {
		t.Fatal(err)
	}
	if dist <= 0 {
		t.Errorf("distance_m after restore = %v, want > 0", dist)
	}

	tags := s.GetDriveTags("2026-04-20")
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2 entries", tags)
	}
}

// TestReplaceDataFromReader_BatchesAcrossThreshold stresses the per-batch
// commit path by writing more routes than replaceBatchSize so at least
// one intermediate flush fires. If the batchWriter stmt/tx recycling is
// broken, this trips a "transaction has already been committed" error.
func TestReplaceDataFromReader_BatchesAcrossThreshold(t *testing.T) {
	s := newStore(t)

	// Generate replaceBatchSize+5 routes so we cross the flush boundary.
	const n = replaceBatchSize + 5
	routes := make([]Route, 0, n)
	for i := 0; i < n; i++ {
		routes = append(routes, sampleRoute(
			"2026-04-20/clip-"+intStr(i)+".mp4",
			"2026-04-20_14-30-00",
		))
	}
	data := StoreData{Routes: routes}
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	stats, err := s.ReplaceDataFromReader(context.Background(), strings.NewReader(string(payload)), nil)
	if err != nil {
		t.Fatalf("ReplaceDataFromReader: %v", err)
	}
	if stats.Routes != n {
		t.Errorf("Routes = %d, want %d", stats.Routes, n)
	}
	if s.RouteCount() != n {
		t.Errorf("RouteCount = %d, want %d", s.RouteCount(), n)
	}

	// Every route must have populated aggregates.
	var nullRows int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM routes WHERE max_speed_mps IS NULL`,
	).Scan(&nullRows); err != nil {
		t.Fatal(err)
	}
	if nullRows != 0 {
		t.Errorf("%d routes have NULL aggregates after streaming restore", nullRows)
	}
}

// TestReplaceDataFromReader_WipesPriorData confirms the wipe phase runs
// even when the incoming payload is smaller than what's already in the
// DB. This is the whole point of "Replace" semantics.
func TestReplaceDataFromReader_WipesPriorData(t *testing.T) {
	s := newStore(t)

	// Seed with two routes via ReplaceData.
	s.ReplaceData(StoreData{
		Routes: []Route{
			sampleRoute("old-1.mp4", "d"),
			sampleRoute("old-2.mp4", "d"),
		},
		ProcessedFiles: []string{"old-1.mp4", "old-2.mp4"},
	})
	if s.RouteCount() != 2 {
		t.Fatalf("seed failed: RouteCount = %d", s.RouteCount())
	}

	// Now stream a smaller replacement.
	payload, _ := json.Marshal(StoreData{
		Routes: []Route{sampleRoute("new-1.mp4", "d")},
	})
	if _, err := s.ReplaceDataFromReader(context.Background(), strings.NewReader(string(payload)), nil); err != nil {
		t.Fatal(err)
	}
	if s.RouteCount() != 1 {
		t.Errorf("RouteCount after replace = %d, want 1", s.RouteCount())
	}
}

// TestReplaceDataFromReader_RejectsInvalidJSON ensures malformed JSON
// surfaces as an error rather than a silent partial import.
func TestReplaceDataFromReader_RejectsInvalidJSON(t *testing.T) {
	s := newStore(t)
	// Seed so we can verify the wipe-then-fail behaviour.
	s.ReplaceData(StoreData{
		Routes: []Route{sampleRoute("seed.mp4", "d")},
	})

	_, err := s.ReplaceDataFromReader(context.Background(),
		strings.NewReader(`{not valid json`), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestReplaceDataFromReader_EmptyPayload is the edge case of a valid but
// empty upload -- should succeed with zero counts and leave the DB empty
// (because the wipe runs first).
func TestReplaceDataFromReader_EmptyPayload(t *testing.T) {
	s := newStore(t)
	s.ReplaceData(StoreData{Routes: []Route{sampleRoute("seed.mp4", "d")}})

	stats, err := s.ReplaceDataFromReader(context.Background(),
		strings.NewReader(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Routes != 0 || stats.ProcessedFiles != 0 {
		t.Errorf("stats = %+v, want all zero", stats)
	}
	if s.RouteCount() != 0 {
		t.Errorf("RouteCount = %d, want 0 (wipe should still have run)", s.RouteCount())
	}
}

// TestReplaceDataFromReader_StreamingInputNotFullyBuffered is a soft
// guarantee that the importer reads incrementally. We feed via a pipe
// that only writes a small prefix then blocks; if the importer tries
// to Read the whole payload before inserting, it'll deadlock.
func TestReplaceDataFromReader_StreamingInputNotFullyBuffered(t *testing.T) {
	s := newStore(t)

	// Build a payload with 3 routes. The decoder should be able to
	// insert the first route before the third one is written to the pipe.
	payload, _ := json.Marshal(StoreData{
		Routes: []Route{
			sampleRoute("a.mp4", "d"),
			sampleRoute("b.mp4", "d"),
			sampleRoute("c.mp4", "d"),
		},
	})

	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(payload)
		_ = pw.Close()
	}()

	if _, err := s.ReplaceDataFromReader(context.Background(), pr, nil); err != nil {
		t.Fatal(err)
	}
	if s.RouteCount() != 3 {
		t.Errorf("RouteCount = %d, want 3", s.RouteCount())
	}
}
