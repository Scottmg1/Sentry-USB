package drives

import (
	"context"
	"database/sql"
	"fmt"
)

// BackfillStats is what backfillRouteAggregates reports to its caller
// and (via the Load path) to WebSocket progress listeners. Updated is
// the number of route rows that had NULL distance_m before the run.
type BackfillStats struct {
	Updated int
}

// backfillBatchSize is the number of routes processed per transaction
// during the one-shot upgrade pass. Small enough to stay inside the
// ~5 MB heap budget targeted by the migration (each Route decodes into
// ~50 KB), large enough to amortize fsync cost. 200 was chosen to match
// the JSON importer which has the same memory profile.
const backfillBatchSize = 200

// backfillRouteAggregates walks every route row whose aggregate columns
// are still NULL (the pre-v2 state) and populates them from the stored
// BLOBs via ComputeRouteAggregates. Runs in batched transactions so the
// WAL doesn't grow unbounded on a 5500-route upgrade.
//
// The WHERE max_speed_mps IS NULL filter is the idempotency gate:
// max_speed_mps is a v2-added nullable column, so pre-v2 rows have it
// as NULL and new AddRoute inserts set a concrete value. distance_m
// can't serve as the sentinel because in v1 it was NOT NULL DEFAULT 0
// -- we'd never find old rows. The caller's marker check
// (summary_backfilled_at) is additional insurance, not the primary
// exit condition -- an interrupted previous run leaves some rows
// populated and some NULL, and this pass correctly continues from
// wherever the previous one stopped.
//
// onProgress is called with (done, total) after each successful batch
// commit; pass nil if the caller doesn't want progress callbacks. The
// function runs on the caller's goroutine and must be cheap.
func backfillRouteAggregates(
	ctx context.Context,
	db *sql.DB,
	onProgress func(done, total int),
) (BackfillStats, error) {
	var stats BackfillStats

	// Figure out total work for progress reporting. An approximate count
	// is fine -- new inserts during the backfill are rare (processor is
	// usually idle at boot) and wouldn't change the order of magnitude.
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM routes WHERE max_speed_mps IS NULL`,
	).Scan(&total); err != nil {
		return stats, fmt.Errorf("count: %w", err)
	}
	if total == 0 {
		return stats, nil
	}

	for {
		batch, err := backfillOneBatch(ctx, db)
		if err != nil {
			return stats, err
		}
		if batch == 0 {
			break // no more NULL rows
		}
		stats.Updated += batch
		if onProgress != nil {
			onProgress(stats.Updated, total)
		}
	}
	return stats, nil
}

// backfillOneBatch selects up to backfillBatchSize NULL-aggregate rows,
// decodes their BLOBs, computes aggregates, and UPDATEs them inside a
// single transaction. Returns the number of rows updated so the caller
// can loop until drained.
func backfillOneBatch(ctx context.Context, db *sql.DB) (int, error) {
	// Read phase: pull BLOBs + metadata for up to backfillBatchSize
	// routes that still need aggregates. We deliberately read before
	// opening the write tx so the UPDATE transaction stays short.
	type row struct {
		file          string
		date          string
		rawParkCount  int
		rawFrameCount int
		pb, gb, ab    []byte
		sb, acb, rb   []byte
	}
	rows, err := db.QueryContext(ctx, `
		SELECT file, date_dir, raw_park_count, raw_frame_count,
		       points_blob, gear_states_blob, ap_states_blob,
		       speeds_blob, accel_blob, gear_runs_blob
		FROM routes
		WHERE max_speed_mps IS NULL
		LIMIT ?`, backfillBatchSize)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(
			&r.file, &r.date, &r.rawParkCount, &r.rawFrameCount,
			&r.pb, &r.gb, &r.ab, &r.sb, &r.acb, &r.rb,
		); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(batch) == 0 {
		return 0, nil
	}

	// Compute phase (outside transaction): decode BLOBs and compute
	// aggregates for every row in memory. Heap usage is bounded by
	// batch size × avg route size.
	type agg struct {
		file string
		RouteAggregates
	}
	aggs := make([]agg, 0, len(batch))
	for _, r := range batch {
		route := Route{
			File:          r.file,
			Date:          r.date,
			RawParkCount:  r.rawParkCount,
			RawFrameCount: r.rawFrameCount,
		}
		pts, err := decodePoints(r.pb)
		if err != nil {
			return 0, fmt.Errorf("decode points %q: %w", r.file, err)
		}
		route.Points = pts
		route.GearStates = decodeUint8s(r.gb)
		route.AutopilotStates = decodeUint8s(r.ab)
		if sp, err := decodeFloat32s(r.sb); err == nil {
			route.Speeds = sp
		}
		if ac, err := decodeFloat32s(r.acb); err == nil {
			route.AccelPositions = ac
		}
		if runs, err := decodeGearRuns(r.rb); err == nil {
			route.GearRuns = runs
		}
		aggs = append(aggs, agg{file: r.file, RouteAggregates: ComputeRouteAggregates(route)})
	}

	// Write phase: apply updates in one transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx, `UPDATE routes SET
		distance_m           = ?,
		max_speed_mps        = ?,
		avg_speed_mps        = ?,
		speed_sample_count   = ?,
		valid_point_count    = ?,
		fsd_engaged_ms       = ?,
		autosteer_engaged_ms = ?,
		tacc_engaged_ms      = ?,
		fsd_distance_m       = ?,
		autosteer_distance_m = ?,
		tacc_distance_m      = ?,
		assisted_distance_m  = ?,
		fsd_disengagements   = ?,
		fsd_accel_pushes     = ?,
		start_lat            = ?,
		start_lon            = ?,
		end_lat              = ?,
		end_lon              = ?
		WHERE file = ?`)
	if err != nil {
		return 0, fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close()

	for _, a := range aggs {
		if _, err := stmt.ExecContext(ctx,
			a.DistanceM, a.MaxSpeedMps, a.AvgSpeedMps, a.SpeedSampleCount,
			a.ValidPointCount, a.FSDEngagedMs, a.AutosteerEngagedMs,
			a.TACCEngagedMs, a.FSDDistanceM, a.AutosteerDistanceM,
			a.TACCDistanceM, a.AssistedDistanceM,
			a.FSDDisengagements, a.FSDAccelPushes,
			nullFloat(a.StartLat), nullFloat(a.StartLng),
			nullFloat(a.EndLat), nullFloat(a.EndLng),
			a.file,
		); err != nil {
			return 0, fmt.Errorf("update %q: %w", a.file, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	tx = nil
	return len(aggs), nil
}
