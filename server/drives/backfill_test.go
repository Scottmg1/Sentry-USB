package drives

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestBackfillRouteAggregates_PopulatesNullRows simulates the upgrade
// flow: a row populated by an older binary (BLOBs present, aggregate
// columns NULL) gets its aggregates filled in by the one-shot backfill.
func TestBackfillRouteAggregates_PopulatesNullRows(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// Insert a route directly with BLOBs but NULL aggregates, mimicking
	// what a pre-v2 binary would have written. Using the store's own
	// encoders means we don't have to hand-roll BLOB formats here.
	route := sampleRoute("2026-04-20/2026-04-20_14-30-00-front.mp4", "2026-04-20_14-30-00")
	pb := encodePoints(route.Points)
	gb := encodeUint8s(route.GearStates)
	ab := encodeUint8s(route.AutopilotStates)
	sb := encodeFloat32s(route.Speeds)
	acb := encodeFloat32s(route.AccelPositions)
	rb := encodeGearRuns(route.GearRuns)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO routes(
			file, date_dir, point_count, raw_park_count, raw_frame_count,
			points_blob, gear_states_blob, ap_states_blob,
			speeds_blob, accel_blob, gear_runs_blob, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		route.File, route.Date, len(route.Points),
		route.RawParkCount, route.RawFrameCount,
		pb, gb, ab, sb, acb, rb); err != nil {
		t.Fatal(err)
	}

	// Sanity: max_speed_mps (our NULL sentinel) should start NULL.
	var maxBefore sql.NullFloat64
	if err := s.db.QueryRowContext(ctx,
		`SELECT max_speed_mps FROM routes WHERE file = ?`, route.File,
	).Scan(&maxBefore); err != nil {
		t.Fatal(err)
	}
	if maxBefore.Valid {
		t.Fatalf("max_speed_mps before backfill should be NULL, got %v", maxBefore)
	}

	stats, err := backfillRouteAggregates(ctx, s.db, nil)
	if err != nil {
		t.Fatalf("backfillRouteAggregates: %v", err)
	}
	if stats.Updated != 1 {
		t.Errorf("Updated = %d, want 1", stats.Updated)
	}

	// After: distance_m etc. should match ComputeRouteAggregates.
	want := ComputeRouteAggregates(route)
	var (
		distAfter    float64
		maxAfter     sql.NullFloat64
		validCount   sql.NullInt64
	)
	if err := s.db.QueryRowContext(ctx,
		`SELECT distance_m, max_speed_mps, valid_point_count
		 FROM routes WHERE file = ?`, route.File,
	).Scan(&distAfter, &maxAfter, &validCount); err != nil {
		t.Fatal(err)
	}
	if abs(distAfter-want.DistanceM) > 1e-9 {
		t.Errorf("distance_m after: got %v want %v", distAfter, want.DistanceM)
	}
	if !maxAfter.Valid {
		t.Error("max_speed_mps still NULL after backfill")
	}
	if !validCount.Valid || int(validCount.Int64) != want.ValidPointCount {
		t.Errorf("valid_point_count after: got %v want %d",
			validCount, want.ValidPointCount)
	}
}

// TestBackfillRouteAggregates_SkipsAlreadyPopulatedRows confirms that
// rows written by the new AddRoute (which already populates aggregates)
// are left untouched. The backfill query filters on `max_speed_mps IS
// NULL` so repeat invocations are no-ops.
func TestBackfillRouteAggregates_SkipsAlreadyPopulatedRows(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	s.AddRoute("a.mp4", "2026-04-20_14-30-00",
		[]GPSPoint{{40, -74}, {40.001, -74}}, nil, nil, nil, nil, 0, 0, nil)

	stats, err := backfillRouteAggregates(ctx, s.db, nil)
	if err != nil {
		t.Fatalf("backfillRouteAggregates: %v", err)
	}
	if stats.Updated != 0 {
		t.Errorf("Updated on already-populated rows = %d, want 0", stats.Updated)
	}
}

// TestBackfillRouteAggregates_EmptyDBSucceeds makes sure the backfill
// does not error on a fresh DB with zero routes.
func TestBackfillRouteAggregates_EmptyDBSucceeds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	stats, err := backfillRouteAggregates(ctx, s.db, nil)
	if err != nil {
		t.Fatalf("backfillRouteAggregates on empty DB: %v", err)
	}
	if stats.Updated != 0 {
		t.Errorf("Updated on empty DB = %d, want 0", stats.Updated)
	}
}

// TestLoad_TriggersBackfillOnUpgrade builds a v1-shaped DB in a tempdir
// (routes inserted with NULL aggregate columns via raw SQL), opens a
// Store against it, and verifies Load() runs the backfill.
func TestLoad_TriggersBackfillOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Build DB with v1 schema manually (no aggregate columns initially,
	// simulating pre-upgrade state).
	s1 := NewStore(dbPath)
	if err := s1.Load(); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	// Inject a pre-v2-style row: aggregates NULL.
	route := sampleRoute("x.mp4", "2026-04-20_14-30-00")
	pb := encodePoints(route.Points)
	gb := encodeUint8s(route.GearStates)
	ab := encodeUint8s(route.AutopilotStates)
	sb := encodeFloat32s(route.Speeds)
	acb := encodeFloat32s(route.AccelPositions)
	rb := encodeGearRuns(route.GearRuns)
	// Columns not listed here default to NULL (for nullable v2 columns)
	// or their DDL default (distance_m was NOT NULL DEFAULT 0 in v1).
	// That matches the pre-v2 state we want to simulate.
	if _, err := s1.db.Exec(`
		INSERT INTO routes(
			file, date_dir, point_count, raw_park_count, raw_frame_count,
			points_blob, gear_states_blob, ap_states_blob,
			speeds_blob, accel_blob, gear_runs_blob, updated_at)
		VALUES(?, ?, ?, 0, 0, ?, ?, ?, ?, ?, ?, 0)`,
		route.File, route.Date, len(route.Points),
		pb, gb, ab, sb, acb, rb); err != nil {
		t.Fatal(err)
	}
	// Backfill is self-healing on every Load (gated only by the NULL
	// sentinel), so no marker-reset is needed here.

	// Close and re-open to exercise the Load() path end-to-end.
	s1.db.Close()
	s1.db = nil

	s2 := NewStore(dbPath)
	if err := s2.Load(); err != nil {
		t.Fatalf("reopen Load: %v", err)
	}
	t.Cleanup(func() { s2.db.Close() })

	// After Load: the NULL aggregates should now be populated.
	var dist float64
	if err := s2.db.QueryRow(
		`SELECT distance_m FROM routes WHERE file = ?`, route.File,
	).Scan(&dist); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if dist <= 0 {
		t.Errorf("distance_m after upgrade Load = %v, want > 0", dist)
	}
	// Marker should be set.
	m, err := metaGet(context.Background(), s2.db, "summary_backfilled_at")
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if m == "" {
		t.Error("summary_backfilled_at should be non-empty after upgrade")
	}
}
