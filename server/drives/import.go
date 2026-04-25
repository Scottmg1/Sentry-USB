package drives

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// utf8BOM is the 3-byte prefix Windows text editors and some export
// tooling prepend to UTF-8 files. json.Decoder does NOT strip it, so a
// legacy drive-data.json touched on Windows would fail importJSON with
// "expected top-level object". We peek-and-skip it before decoding.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// skipUTF8BOM returns a reader positioned just past any UTF-8 BOM at
// the start of r. If r does not start with a BOM, the returned reader
// yields the original bytes unchanged.
func skipUTF8BOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	prefix, err := br.Peek(len(utf8BOM))
	if err == nil && bytes.Equal(prefix, utf8BOM) {
		_, _ = br.Discard(len(utf8BOM))
	}
	return br
}

// importProgressEvery controls how often importJSON invokes the
// onProgress callback during the routes array. Keep it high enough that
// the callback doesn't dominate import time but low enough that the UI
// sees updates on a ~700 MB input within seconds.
const importProgressEvery = 100

// ImportStats summarizes what importJSON inserted. Used by tests,
// /api/drives/status, and the Load() log line.
type ImportStats struct {
	Routes         int
	ProcessedFiles int
	DriveTags      int
}

// importJSON streams a drive-data.json payload from r into db within a
// single transaction. On any error the transaction rolls back and db is
// unchanged. On success the transaction commits and the counts are
// returned via ImportStats.
//
// Design decisions:
//   - One outer tx: an import is either fully applied or fully reverted.
//     The WAL grows to the size of the inserted data (hundreds of MB on
//     a heavy user); that's fine because the WAL is on the same /mutable
//     partition and gets reclaimed after the COMMIT + wal_checkpoint.
//   - Streaming decode: json.Decoder.Token() walks the top-level object
//     key-by-key; each "routes" array element is dec.Decode()d one at a
//     time into a single Route value. Peak Go-side memory is ~one Route
//     (~100-200 KB) regardless of input size, which is the load-bearing
//     property for the 512 MB Pi Zero 2 W.
//   - Any order: processedFiles / routes / driveTags can appear in any
//     order at the top level (json object key order is not significant).
//   - Routes auto-mark-processed: inserting a routes row also ensures a
//     processed_files row so reprocess semantics stay consistent.
//   - Path normalization: all file paths are converted to forward slashes
//     on insert so Windows-shaped inputs collide with their POSIX keys.
//
// The importer does NOT write meta.imported_from_json_at — that's the
// caller's responsibility, so tests can exercise the importer against
// already-populated DBs without side-effecting the import marker.
func importJSON(ctx context.Context, db *sql.DB, r io.Reader, onProgress func(routesImported int)) (ImportStats, error) {
	var stats ImportStats

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("importJSON: begin tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	// Prepare all three insert statements once; reused across thousands
	// of rows. INSERT OR IGNORE on processed_files and drive_tags so
	// duplicates in the source JSON don't break the import.
	pfStmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO processed_files(file, added_at) VALUES(?, ?)`)
	if err != nil {
		return stats, fmt.Errorf("importJSON: prepare processed_files: %w", err)
	}
	defer pfStmt.Close()

	// The importer populates v2 aggregate columns inline so that a
	// first-boot upgrade doesn't have to re-decode every BLOB during
	// runAggregateBackfillLocked. On the 5500-route / 762 MB dataset
	// this halves wall-time for the one-shot migration.
	//
	// v3 adds source / external_signature / tessie_autopilot_percent
	// for Tessie-imported clips. The route-level source defaults to
	// 'sei' on missing input; Tessie clips carry "tessie" explicitly.
	routeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO routes(
			file, date_dir, point_count, raw_park_count, raw_frame_count,
			start_ts, end_ts, distance_m, first_lat, first_lon,
			points_blob, gear_states_blob, ap_states_blob,
			speeds_blob, accel_blob, gear_runs_blob, updated_at,
			max_speed_mps, avg_speed_mps, speed_sample_count, valid_point_count,
			fsd_engaged_ms, autosteer_engaged_ms, tacc_engaged_ms,
			fsd_distance_m, autosteer_distance_m, tacc_distance_m, assisted_distance_m,
			fsd_disengagements, fsd_accel_pushes,
			start_lat, start_lon, end_lat, end_lon,
			source, external_signature, tessie_autopilot_percent)
		VALUES(?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?)
		ON CONFLICT(file) DO UPDATE SET
			date_dir                = excluded.date_dir,
			point_count             = excluded.point_count,
			raw_park_count          = excluded.raw_park_count,
			raw_frame_count         = excluded.raw_frame_count,
			distance_m              = excluded.distance_m,
			first_lat               = excluded.first_lat,
			first_lon               = excluded.first_lon,
			points_blob             = excluded.points_blob,
			gear_states_blob        = excluded.gear_states_blob,
			ap_states_blob          = excluded.ap_states_blob,
			speeds_blob             = excluded.speeds_blob,
			accel_blob              = excluded.accel_blob,
			gear_runs_blob          = excluded.gear_runs_blob,
			updated_at              = excluded.updated_at,
			max_speed_mps           = excluded.max_speed_mps,
			avg_speed_mps           = excluded.avg_speed_mps,
			speed_sample_count      = excluded.speed_sample_count,
			valid_point_count       = excluded.valid_point_count,
			fsd_engaged_ms          = excluded.fsd_engaged_ms,
			autosteer_engaged_ms    = excluded.autosteer_engaged_ms,
			tacc_engaged_ms         = excluded.tacc_engaged_ms,
			fsd_distance_m          = excluded.fsd_distance_m,
			autosteer_distance_m    = excluded.autosteer_distance_m,
			tacc_distance_m         = excluded.tacc_distance_m,
			assisted_distance_m     = excluded.assisted_distance_m,
			fsd_disengagements      = excluded.fsd_disengagements,
			fsd_accel_pushes        = excluded.fsd_accel_pushes,
			start_lat               = excluded.start_lat,
			start_lon               = excluded.start_lon,
			end_lat                 = excluded.end_lat,
			end_lon                 = excluded.end_lon,
			source                  = excluded.source,
			external_signature      = excluded.external_signature,
			tessie_autopilot_percent = excluded.tessie_autopilot_percent`)
	if err != nil {
		return stats, fmt.Errorf("importJSON: prepare routes: %w", err)
	}
	defer routeStmt.Close()

	tagStmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO drive_tags(drive_key, tag) VALUES(?, ?)`)
	if err != nil {
		return stats, fmt.Errorf("importJSON: prepare drive_tags: %w", err)
	}
	defer tagStmt.Close()

	now := time.Now().Unix()
	dec := json.NewDecoder(skipUTF8BOM(r))

	// Expect an opening '{'.
	tok, err := dec.Token()
	if err != nil {
		return stats, fmt.Errorf("importJSON: read opening token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return stats, fmt.Errorf("importJSON: expected top-level object, got %v", tok)
	}

	// Walk key-value pairs at the top level.
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return stats, fmt.Errorf("importJSON: read key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return stats, fmt.Errorf("importJSON: expected string key, got %v", keyTok)
		}

		switch key {
		case "processedFiles":
			n, err := streamProcessedFiles(ctx, dec, pfStmt, now)
			if err != nil {
				return stats, err
			}
			stats.ProcessedFiles += n

		case "routes":
			n, nProcessed, err := streamRoutes(ctx, dec, routeStmt, pfStmt, now, onProgress)
			if err != nil {
				return stats, err
			}
			stats.Routes = n
			stats.ProcessedFiles += nProcessed

		case "driveTags":
			n, err := streamDriveTags(ctx, dec, tagStmt)
			if err != nil {
				return stats, err
			}
			stats.DriveTags += n

		default:
			// Unknown top-level key — skip its value. Preserves forward
			// compatibility if Sentry Studio ever writes extra keys.
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return stats, fmt.Errorf("importJSON: skip unknown key %q: %w", key, err)
			}
		}
	}

	// Expect closing '}'.
	if tok, err := dec.Token(); err != nil {
		return stats, fmt.Errorf("importJSON: read closing token: %w", err)
	} else if d, ok := tok.(json.Delim); !ok || d != '}' {
		return stats, fmt.Errorf("importJSON: expected closing }, got %v", tok)
	}
	// Reject trailing garbage after the object — protects against
	// truncated-but-appended inputs.
	if dec.More() {
		return stats, fmt.Errorf("importJSON: unexpected trailing data after root object")
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("importJSON: commit: %w", err)
	}
	tx = nil
	return stats, nil
}

// streamProcessedFiles reads a JSON array of strings and inserts each into
// processed_files. Returns the number of rows attempted (INSERT OR IGNORE
// means some may be no-ops on duplicates).
//
// Accepts both "null" and "[]" as the empty case, since json.Marshal
// serializes a nil []string as null (not []) and StoreData omits neither.
func streamProcessedFiles(ctx context.Context, dec *json.Decoder, stmt *sql.Stmt, now int64) (int, error) {
	open, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("processedFiles: open: %w", err)
	}
	if open == nil { // JSON null
		return 0, nil
	}
	if d, ok := open.(json.Delim); !ok || d != '[' {
		return 0, fmt.Errorf("processedFiles: expected [, got %v", open)
	}
	count := 0
	for dec.More() {
		var path string
		if err := dec.Decode(&path); err != nil {
			return count, fmt.Errorf("processedFiles: decode element %d: %w", count, err)
		}
		norm := normalizePath(path)
		if _, err := stmt.ExecContext(ctx, norm, now); err != nil {
			return count, fmt.Errorf("processedFiles: insert %q: %w", norm, err)
		}
		count++
	}
	if _, err := dec.Token(); err != nil { // ']'
		return count, fmt.Errorf("processedFiles: close: %w", err)
	}
	return count, nil
}

// streamRoutes reads a JSON array of Route objects and inserts each into
// routes and processed_files within the caller's transaction. Returns the
// total route rows inserted and the number of processed_files rows that
// were created as a side-effect.
func streamRoutes(
	ctx context.Context,
	dec *json.Decoder,
	routeStmt, pfStmt *sql.Stmt,
	now int64,
	onProgress func(int),
) (int, int, error) {
	open, err := dec.Token()
	if err != nil {
		return 0, 0, fmt.Errorf("routes: open: %w", err)
	}
	if open == nil { // JSON null (nil slice serialized by encoding/json)
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
			return count, pfCount, fmt.Errorf("routes: decode element %d: %w", count, err)
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

		// Source coercion: "" → "sei". The DB column also has DEFAULT
		// 'sei', but binding it explicitly keeps the data intentional
		// rather than relying on default fallback for legacy SEI clips.
		source := r.Source
		if source == "" {
			source = "sei"
		}
		var extSig sql.NullString
		if r.ExternalSignature != "" {
			extSig = sql.NullString{String: r.ExternalSignature, Valid: true}
		}
		var tessieAP sql.NullFloat64
		if r.TessieAutopilotPercent != 0 {
			tessieAP = sql.NullFloat64{Float64: r.TessieAutopilotPercent, Valid: true}
		}

		if _, err := routeStmt.ExecContext(ctx,
			norm, r.Date, len(r.Points), r.RawParkCount, r.RawFrameCount,
			agg.DistanceM, firstLat, firstLon,
			pb, gb, ab, sb, acb, rb, now,
			agg.MaxSpeedMps, agg.AvgSpeedMps, agg.SpeedSampleCount, agg.ValidPointCount,
			agg.FSDEngagedMs, agg.AutosteerEngagedMs, agg.TACCEngagedMs,
			agg.FSDDistanceM, agg.AutosteerDistanceM, agg.TACCDistanceM, agg.AssistedDistanceM,
			agg.FSDDisengagements, agg.FSDAccelPushes,
			nullFloat(agg.StartLat), nullFloat(agg.StartLng),
			nullFloat(agg.EndLat), nullFloat(agg.EndLng),
			source, extSig, tessieAP); err != nil {
			return count, pfCount, fmt.Errorf("routes: insert %q: %w", norm, err)
		}

		// Ensure processed_files has this file too. INSERT OR IGNORE in
		// the statement means this is cheap on duplicates.
		res, err := pfStmt.ExecContext(ctx, norm, now)
		if err != nil {
			return count, pfCount, fmt.Errorf("routes: processed_files insert %q: %w", norm, err)
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			pfCount++
		}

		count++
		if onProgress != nil && count%importProgressEvery == 0 {
			onProgress(count)
		}
	}
	if _, err := dec.Token(); err != nil { // ']'
		return count, pfCount, fmt.Errorf("routes: close: %w", err)
	}
	if onProgress != nil && count%importProgressEvery != 0 {
		onProgress(count) // final tick
	}
	return count, pfCount, nil
}

// streamDriveTags reads a JSON object mapping drive_key -> []tag and
// inserts rows into drive_tags. Returns the total tag-rows inserted.
func streamDriveTags(ctx context.Context, dec *json.Decoder, stmt *sql.Stmt) (int, error) {
	open, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("driveTags: open: %w", err)
	}
	if open == nil { // JSON null (nil map serialized by encoding/json)
		return 0, nil
	}
	if d, ok := open.(json.Delim); !ok || d != '{' {
		return 0, fmt.Errorf("driveTags: expected {, got %v", open)
	}
	count := 0
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return count, fmt.Errorf("driveTags: read key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return count, fmt.Errorf("driveTags: expected string key, got %v", keyTok)
		}
		var tags []string
		if err := dec.Decode(&tags); err != nil {
			return count, fmt.Errorf("driveTags: decode tags for %q: %w", key, err)
		}
		for _, t := range tags {
			if t == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, key, t); err != nil {
				return count, fmt.Errorf("driveTags: insert %q=%q: %w", key, t, err)
			}
			count++
		}
	}
	if _, err := dec.Token(); err != nil { // '}'
		return count, fmt.Errorf("driveTags: close: %w", err)
	}
	return count, nil
}
