package drives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"
)

// replaceBatchSize is the number of routes inserted per transaction
// during ReplaceDataFromReader. Mirrors backfillBatchSize so the
// per-batch WAL footprint matches the already-validated backfill
// profile (~10 MB on the 512 MB Pi target).
const replaceBatchSize = 200

// routeUpsertSQL matches the ON CONFLICT(file) DO UPDATE pattern used
// by importJSON (see import.go). Kept here too because ReplaceData
// runs under different transactional semantics (per-batch commits)
// and extracting a shared const would risk the two call sites drifting
// silently if one ever needs a column but the other doesn't.
const routeUpsertSQL = `
	INSERT INTO routes(
		file, date_dir, point_count, raw_park_count, raw_frame_count,
		start_ts, end_ts, distance_m, first_lat, first_lon,
		points_blob, gear_states_blob, ap_states_blob,
		speeds_blob, accel_blob, gear_runs_blob, updated_at,
		max_speed_mps, avg_speed_mps, speed_sample_count, valid_point_count,
		fsd_engaged_ms, autosteer_engaged_ms, tacc_engaged_ms,
		fsd_distance_m, autosteer_distance_m, tacc_distance_m, assisted_distance_m,
		fsd_disengagements, fsd_accel_pushes,
		start_lat, start_lon, end_lat, end_lon)
	VALUES(?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(file) DO UPDATE SET
		date_dir            = excluded.date_dir,
		point_count         = excluded.point_count,
		raw_park_count      = excluded.raw_park_count,
		raw_frame_count     = excluded.raw_frame_count,
		distance_m          = excluded.distance_m,
		first_lat           = excluded.first_lat,
		first_lon           = excluded.first_lon,
		points_blob         = excluded.points_blob,
		gear_states_blob    = excluded.gear_states_blob,
		ap_states_blob      = excluded.ap_states_blob,
		speeds_blob         = excluded.speeds_blob,
		accel_blob          = excluded.accel_blob,
		gear_runs_blob      = excluded.gear_runs_blob,
		updated_at          = excluded.updated_at,
		max_speed_mps       = excluded.max_speed_mps,
		avg_speed_mps       = excluded.avg_speed_mps,
		speed_sample_count  = excluded.speed_sample_count,
		valid_point_count   = excluded.valid_point_count,
		fsd_engaged_ms      = excluded.fsd_engaged_ms,
		autosteer_engaged_ms= excluded.autosteer_engaged_ms,
		tacc_engaged_ms     = excluded.tacc_engaged_ms,
		fsd_distance_m      = excluded.fsd_distance_m,
		autosteer_distance_m= excluded.autosteer_distance_m,
		tacc_distance_m     = excluded.tacc_distance_m,
		assisted_distance_m = excluded.assisted_distance_m,
		fsd_disengagements  = excluded.fsd_disengagements,
		fsd_accel_pushes    = excluded.fsd_accel_pushes,
		start_lat           = excluded.start_lat,
		start_lon           = excluded.start_lon,
		end_lat             = excluded.end_lat,
		end_lon             = excluded.end_lon`

// ReplaceDataFromReader wipes routes, processed_files, and drive_tags,
// then stream-decodes the supplied JSON and re-inserts in batches of
// replaceBatchSize routes per transaction.
//
// Replaces the legacy ReplaceData(StoreData) path which json.Unmarshal'd
// the entire upload into heap and then bulk-inserted under one mega-
// transaction -- catastrophic for 512MB Pis on multi-GB backups.
//
// Tradeoffs vs the legacy single-tx behaviour:
//   - Peak Go heap: ~one Route (~200 KB) + a batch's worth of BLOBs
//     (~10 MB) instead of proportional to upload size.
//   - Peak WAL: ~10 MB (one batch), reclaimed between batches, instead
//     of growing to the full upload size.
//   - Crash-during-restore semantics: an interrupted restore leaves the
//     DB with whatever batches committed before the crash, not the old
//     pre-restore state. This is acceptable because the user's source
//     backup is still intact on disk and can be re-uploaded; the
//     alternative (legacy behaviour) is guaranteed OOM on the 512MB
//     target, which is strictly worse.
//
// Each batch's transaction is wrapped in withBusyRetry so a transient
// SQLITE_BUSY from a concurrent AddRoute or WAL checkpoint doesn't
// silently drop a batch's rows.
func (s *Store) ReplaceDataFromReader(ctx context.Context, r io.Reader, onProgress func(routesImported int)) (ImportStats, error) {
	var stats ImportStats

	s.mu.Lock()
	defer s.mu.Unlock()

	// Wipe phase: single small tx, fast regardless of table size because
	// SQLite DELETE without WHERE is O(pages) not O(rows).
	if err := withBusyRetry(ctx, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("wipe begin: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		for _, stmt := range []string{
			`DELETE FROM routes`,
			`DELETE FROM processed_files`,
			`DELETE FROM drive_tags`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("wipe %q: %w", stmt, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("wipe commit: %w", err)
		}
		committed = true
		return nil
	}); err != nil {
		return stats, err
	}

	// Stream-import with per-batch commits.
	bt := &batchWriter{db: s.db, ctx: ctx, threshold: replaceBatchSize, now: time.Now().Unix()}
	defer bt.close()

	dec := json.NewDecoder(skipUTF8BOM(r))

	tok, err := dec.Token()
	if err != nil {
		return stats, fmt.Errorf("ReplaceDataFromReader: read opening token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return stats, fmt.Errorf("ReplaceDataFromReader: expected top-level object, got %v", tok)
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return stats, fmt.Errorf("ReplaceDataFromReader: read key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return stats, fmt.Errorf("ReplaceDataFromReader: expected string key, got %v", keyTok)
		}
		switch key {
		case "processedFiles":
			n, err := replaceProcessedFiles(ctx, dec, bt)
			if err != nil {
				return stats, err
			}
			stats.ProcessedFiles += n
		case "routes":
			n, nPf, err := replaceRoutes(ctx, dec, bt, onProgress)
			if err != nil {
				return stats, err
			}
			stats.Routes += n
			stats.ProcessedFiles += nPf
		case "driveTags":
			n, err := replaceDriveTags(ctx, dec, bt)
			if err != nil {
				return stats, err
			}
			stats.DriveTags += n
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return stats, fmt.Errorf("ReplaceDataFromReader: skip unknown key %q: %w", key, err)
			}
		}
	}

	if tok, err := dec.Token(); err != nil {
		return stats, fmt.Errorf("ReplaceDataFromReader: read closing token: %w", err)
	} else if d, ok := tok.(json.Delim); !ok || d != '}' {
		return stats, fmt.Errorf("ReplaceDataFromReader: expected closing }, got %v", tok)
	}
	if dec.More() {
		return stats, fmt.Errorf("ReplaceDataFromReader: unexpected trailing data after root object")
	}

	// Flush the final partial batch.
	if err := bt.flush(); err != nil {
		return stats, fmt.Errorf("ReplaceDataFromReader: final flush: %w", err)
	}

	_ = s.refreshCountsLocked(ctx)
	return stats, nil
}

// batchWriter owns the current batch transaction and its prepared
// statements. Each Write* method increments a row counter; when the
// counter reaches the threshold, the tx commits and a fresh one opens.
// flush() commits any partial batch; close() rolls back an uncommitted
// batch (called via defer on the error path).
//
// The prepared statements are re-created on each new tx because SQLite
// prepared statements are tx-scoped.
type batchWriter struct {
	db        *sql.DB
	ctx       context.Context
	threshold int
	now       int64

	tx        *sql.Tx
	routeStmt *sql.Stmt
	pfStmt    *sql.Stmt
	tagStmt   *sql.Stmt
	count     int // rows since last commit
}

// ensure opens a new tx + prepared statements if none is active. Idempotent.
func (b *batchWriter) ensure() error {
	if b.tx != nil {
		return nil
	}
	return withBusyRetry(b.ctx, func() error {
		tx, err := b.db.BeginTx(b.ctx, nil)
		if err != nil {
			return fmt.Errorf("batch begin: %w", err)
		}
		// Any error after this point must rollback.
		pfStmt, err := tx.PrepareContext(b.ctx,
			`INSERT OR IGNORE INTO processed_files(file, added_at) VALUES(?, ?)`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("batch prepare pf: %w", err)
		}
		routeStmt, err := tx.PrepareContext(b.ctx, routeUpsertSQL)
		if err != nil {
			_ = pfStmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("batch prepare routes: %w", err)
		}
		tagStmt, err := tx.PrepareContext(b.ctx,
			`INSERT OR IGNORE INTO drive_tags(drive_key, tag) VALUES(?, ?)`)
		if err != nil {
			_ = routeStmt.Close()
			_ = pfStmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("batch prepare tags: %w", err)
		}
		b.tx = tx
		b.pfStmt = pfStmt
		b.routeStmt = routeStmt
		b.tagStmt = tagStmt
		return nil
	})
}

// maybeFlush commits the current batch if the row count has hit the
// threshold. Called after every row insertion.
func (b *batchWriter) maybeFlush() error {
	if b.count >= b.threshold {
		return b.flush()
	}
	return nil
}

// flush commits the active tx (if any) and resets. Safe to call with no
// active tx.
func (b *batchWriter) flush() error {
	if b.tx == nil {
		return nil
	}
	if err := withBusyRetry(b.ctx, func() error {
		// close stmts before commit so the driver doesn't complain
		// about dangling statements on commit.
		if b.routeStmt != nil {
			_ = b.routeStmt.Close()
		}
		if b.pfStmt != nil {
			_ = b.pfStmt.Close()
		}
		if b.tagStmt != nil {
			_ = b.tagStmt.Close()
		}
		err := b.tx.Commit()
		b.tx = nil
		b.routeStmt = nil
		b.pfStmt = nil
		b.tagStmt = nil
		b.count = 0
		if err != nil {
			return fmt.Errorf("batch commit: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	// Reclaim freelist pages created by the pre-flush DELETE (during
	// the initial wipe) or by prior batches. No-op on DBs that aren't
	// in INCREMENTAL auto_vacuum mode (i.e., pre-fix databases), so
	// this is safe to call unconditionally. Keeps the .db file from
	// growing unbounded across repeated upload-restore cycles.
	if _, err := b.db.ExecContext(b.ctx, `PRAGMA incremental_vacuum(200)`); err != nil {
		// Best-effort: an error here is never worth failing the
		// restore over. Logged so a regression is visible but the
		// user still gets their data.
		log.Printf("[drives] incremental_vacuum: %v", err)
	}
	return nil
}

// close rolls back any uncommitted tx. Safe to call multiple times.
// Does NOT commit partial work -- call flush() explicitly for that.
func (b *batchWriter) close() {
	if b.tx == nil {
		return
	}
	if b.routeStmt != nil {
		_ = b.routeStmt.Close()
	}
	if b.pfStmt != nil {
		_ = b.pfStmt.Close()
	}
	if b.tagStmt != nil {
		_ = b.tagStmt.Close()
	}
	if err := b.tx.Rollback(); err != nil {
		log.Printf("[drives] batch rollback: %v", err)
	}
	b.tx = nil
	b.routeStmt = nil
	b.pfStmt = nil
	b.tagStmt = nil
	b.count = 0
}

// writeProcessedFile inserts a single processed_files row.
func (b *batchWriter) writeProcessedFile(path string) error {
	if err := b.ensure(); err != nil {
		return err
	}
	if _, err := b.pfStmt.ExecContext(b.ctx, path, b.now); err != nil {
		return fmt.Errorf("pf insert %q: %w", path, err)
	}
	b.count++
	return b.maybeFlush()
}

// writeRoute inserts a route + its processed_files side-effect row.
// Returns whether a new processed_files row was created (not an IGNORE no-op).
func (b *batchWriter) writeRoute(r Route) (bool, error) {
	if err := b.ensure(); err != nil {
		return false, err
	}
	norm := normalizePath(r.File)
	var firstLat, firstLon sql.NullFloat64
	if len(r.Points) > 0 {
		firstLat.Float64, firstLat.Valid = r.Points[0][0], true
		firstLon.Float64, firstLon.Valid = r.Points[0][1], true
	}
	pb := encodePoints(r.Points)
	gb := encodeUint8s(r.GearStates)
	ab := encodeUint8s(r.AutopilotStates)
	sb := encodeFloat32s(r.Speeds)
	acb := encodeFloat32s(r.AccelPositions)
	rb := encodeGearRuns(r.GearRuns)
	agg := ComputeRouteAggregates(r)

	if _, err := b.routeStmt.ExecContext(b.ctx,
		norm, r.Date, len(r.Points), r.RawParkCount, r.RawFrameCount,
		agg.DistanceM, firstLat, firstLon,
		pb, gb, ab, sb, acb, rb, b.now,
		agg.MaxSpeedMps, agg.AvgSpeedMps, agg.SpeedSampleCount, agg.ValidPointCount,
		agg.FSDEngagedMs, agg.AutosteerEngagedMs, agg.TACCEngagedMs,
		agg.FSDDistanceM, agg.AutosteerDistanceM, agg.TACCDistanceM, agg.AssistedDistanceM,
		agg.FSDDisengagements, agg.FSDAccelPushes,
		nullFloat(agg.StartLat), nullFloat(agg.StartLng),
		nullFloat(agg.EndLat), nullFloat(agg.EndLng)); err != nil {
		return false, fmt.Errorf("route insert %q: %w", norm, err)
	}

	res, err := b.pfStmt.ExecContext(b.ctx, norm, b.now)
	if err != nil {
		return false, fmt.Errorf("route-pf insert %q: %w", norm, err)
	}
	newPf := false
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		newPf = true
	}
	b.count++
	return newPf, b.maybeFlush()
}

// writeDriveTag inserts a single drive_tag row.
func (b *batchWriter) writeDriveTag(key, tag string) error {
	if err := b.ensure(); err != nil {
		return err
	}
	if _, err := b.tagStmt.ExecContext(b.ctx, key, tag); err != nil {
		return fmt.Errorf("tag insert %q=%q: %w", key, tag, err)
	}
	b.count++
	return b.maybeFlush()
}

func replaceProcessedFiles(ctx context.Context, dec *json.Decoder, bt *batchWriter) (int, error) {
	open, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("processedFiles: open: %w", err)
	}
	if open == nil {
		return 0, nil
	}
	if d, ok := open.(json.Delim); !ok || d != '[' {
		return 0, fmt.Errorf("processedFiles: expected [, got %v", open)
	}
	count := 0
	for dec.More() {
		var path string
		if err := dec.Decode(&path); err != nil {
			return count, fmt.Errorf("processedFiles: decode %d: %w", count, err)
		}
		if err := bt.writeProcessedFile(normalizePath(path)); err != nil {
			return count, fmt.Errorf("processedFiles: %w", err)
		}
		count++
	}
	if _, err := dec.Token(); err != nil {
		return count, fmt.Errorf("processedFiles: close: %w", err)
	}
	return count, nil
}

func replaceRoutes(ctx context.Context, dec *json.Decoder, bt *batchWriter, onProgress func(int)) (int, int, error) {
	open, err := dec.Token()
	if err != nil {
		return 0, 0, fmt.Errorf("routes: open: %w", err)
	}
	if open == nil {
		return 0, 0, nil
	}
	if d, ok := open.(json.Delim); !ok || d != '[' {
		return 0, 0, fmt.Errorf("routes: expected [, got %v", open)
	}
	count := 0
	pfCount := 0
	for dec.More() {
		var r Route
		if err := dec.Decode(&r); err != nil {
			return count, pfCount, fmt.Errorf("routes: decode %d: %w", count, err)
		}
		newPf, err := bt.writeRoute(r)
		if err != nil {
			return count, pfCount, fmt.Errorf("routes: %w", err)
		}
		if newPf {
			pfCount++
		}
		count++
		if onProgress != nil && count%importProgressEvery == 0 {
			onProgress(count)
		}
	}
	if _, err := dec.Token(); err != nil {
		return count, pfCount, fmt.Errorf("routes: close: %w", err)
	}
	if onProgress != nil && count%importProgressEvery != 0 {
		onProgress(count)
	}
	return count, pfCount, nil
}

func replaceDriveTags(ctx context.Context, dec *json.Decoder, bt *batchWriter) (int, error) {
	open, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("driveTags: open: %w", err)
	}
	if open == nil {
		return 0, nil
	}
	if d, ok := open.(json.Delim); !ok || d != '{' {
		return 0, fmt.Errorf("driveTags: expected {, got %v", open)
	}
	count := 0
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return count, fmt.Errorf("driveTags: key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return count, fmt.Errorf("driveTags: expected string key, got %v", keyTok)
		}
		var tags []string
		if err := dec.Decode(&tags); err != nil {
			return count, fmt.Errorf("driveTags: decode %q: %w", key, err)
		}
		for _, t := range tags {
			if t == "" {
				continue
			}
			if err := bt.writeDriveTag(key, t); err != nil {
				return count, fmt.Errorf("driveTags: %w", err)
			}
			count++
		}
	}
	if _, err := dec.Token(); err != nil {
		return count, fmt.Errorf("driveTags: close: %w", err)
	}
	return count, nil
}
