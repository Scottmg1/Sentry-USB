package drives

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
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
// Store against it, calls StartBackgroundBackfill, waits for it to
// drain, and verifies the aggregates + marker landed.
//
// The backfill is intentionally async now: Load() no longer runs it
// synchronously so HTTP can start serving immediately on first boot
// after upgrade. The server wiring in cmd/main.go calls
// StartBackgroundBackfill right after Load(); this test simulates that.
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

	// Simulate the server-startup sequence: Load() doesn't run the
	// backfill anymore -- the HTTP layer triggers it asynchronously so
	// the API can start serving immediately. Drive that path here.
	if !s2.StartBackgroundBackfill(context.Background(), nil) {
		t.Fatal("StartBackgroundBackfill returned false; expected a NULL row to trigger it")
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s2.WaitForMigration(waitCtx); err != nil {
		t.Fatalf("WaitForMigration: %v", err)
	}

	// After backfill: the NULL aggregates should now be populated.
	var dist float64
	if err := s2.db.QueryRow(
		`SELECT distance_m FROM routes WHERE file = ?`, route.File,
	).Scan(&dist); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if dist <= 0 {
		t.Errorf("distance_m after upgrade backfill = %v, want > 0", dist)
	}
	// Marker should be set.
	m, err := metaGet(context.Background(), s2.db, "summary_backfilled_at")
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if m == "" {
		t.Error("summary_backfilled_at should be non-empty after upgrade")
	}
	// MigrationStatus should report non-active after drain.
	status := s2.MigrationStatus()
	if status.Active {
		t.Error("MigrationStatus.Active still true after WaitForMigration")
	}
	if status.Error != "" {
		t.Errorf("MigrationStatus.Error = %q, want empty", status.Error)
	}
}

// TestBackfill_CancelsOnContext verifies that cancelling the context
// aborts the backfill mid-run and that a fresh run (with a fresh ctx)
// picks up the remaining NULL rows. This is the behaviour the async
// runner in Store depends on when the server receives SIGTERM.
func TestBackfill_CancelsOnContext(t *testing.T) {
	s := newStore(t)

	// Seed enough NULL-aggregate rows that at least one batch will
	// complete before the cancel lands, leaving remaining work for the
	// resume run.
	const total = backfillBatchSize*2 + 10
	for i := 0; i < total; i++ {
		route := sampleRoute(
			"2026-04-20/clip-"+intStr(i)+".mp4",
			"2026-04-20_14-30-00",
		)
		pb := encodePoints(route.Points)
		gb := encodeUint8s(route.GearStates)
		ab := encodeUint8s(route.AutopilotStates)
		sb := encodeFloat32s(route.Speeds)
		acb := encodeFloat32s(route.AccelPositions)
		rb := encodeGearRuns(route.GearRuns)
		if _, err := s.db.Exec(`
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
	}

	// Cancel after one batch has completed.
	ctx, cancel := context.WithCancel(context.Background())
	progressCh := make(chan struct{}, 4)
	go func() {
		<-progressCh // wait for first onProgress
		cancel()
	}()
	_, err := backfillRouteAggregates(ctx, s.db, func(done, _ int) {
		select {
		case progressCh <- struct{}{}:
		default:
		}
	})
	if err == nil {
		t.Fatal("expected context.Canceled, got nil")
	}
	if !isCanceledErr(err) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Count rows still NULL. Must be non-zero (cancel came before all
	// batches) AND less than total (at least one batch committed).
	var remaining int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM routes WHERE max_speed_mps IS NULL`,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining == 0 || remaining == total {
		t.Fatalf("cancel test didn't exercise mid-run cancel: remaining=%d, total=%d", remaining, total)
	}

	// Resume: fresh ctx, second run should finish the rest.
	stats, err := backfillRouteAggregates(context.Background(), s.db, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if stats.Updated != remaining {
		t.Errorf("resume updated %d, want %d", stats.Updated, remaining)
	}
	var stillNull int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM routes WHERE max_speed_mps IS NULL`,
	).Scan(&stillNull); err != nil {
		t.Fatal(err)
	}
	if stillNull != 0 {
		t.Errorf("rows still NULL after resume: %d", stillNull)
	}
}

// intStr is a minimal int-to-string for generating unique file names
// in test loops without pulling strconv into the test imports.
func intStr(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

func isCanceledErr(err error) bool {
	return errors.Is(err, context.Canceled)
}
