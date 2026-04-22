package drives

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// currentSchemaVersion is the schema version this binary writes. It is stored
// in the `meta` table and checked on every open so future upgrades can run
// targeted migrations between versions.
const currentSchemaVersion = 1

// schemaStatements is the DDL for version 1. Each statement is idempotent
// (CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS) so migrate() is
// safe to run on every startup.
//
// Design notes:
//   - routes.file is the normalized (forward-slash) relative path from the
//     clips directory; it is the upsert key for AddRoute.
//   - Parallel point-data slices are packed into fixed-stride little-endian
//     BLOBs rather than broken out into a separate points table. At ~2000
//     points per clip and ~5500 clips that would be ~11M rows, which makes
//     every summary query an 11M-row scan. One row per clip keeps the hot
//     paths O(routes).
//   - start_ts / end_ts / distance_m / first_lat / first_lon are computed
//     once on insert so DriveSummary and GroupRoutesOverview queries never
//     have to decode the point BLOB.
//   - processed_files is a separate table because a clip can be marked
//     processed without producing a route (no GPS found).
//   - drive_tags uses a composite (drive_key, tag) PK so looking up "all
//     drives with tag X" is a single index scan.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	) WITHOUT ROWID`,

	`CREATE TABLE IF NOT EXISTS routes (
		file              TEXT PRIMARY KEY,
		date_dir          TEXT NOT NULL,
		point_count       INTEGER NOT NULL DEFAULT 0,
		raw_park_count    INTEGER NOT NULL DEFAULT 0,
		raw_frame_count   INTEGER NOT NULL DEFAULT 0,
		start_ts          INTEGER,
		end_ts            INTEGER,
		distance_m        REAL NOT NULL DEFAULT 0,
		first_lat         REAL,
		first_lon         REAL,
		points_blob       BLOB NOT NULL,
		gear_states_blob  BLOB,
		ap_states_blob    BLOB,
		speeds_blob       BLOB,
		accel_blob        BLOB,
		gear_runs_blob    BLOB,
		updated_at        INTEGER NOT NULL
	) WITHOUT ROWID`,

	`CREATE INDEX IF NOT EXISTS idx_routes_date_dir ON routes(date_dir)`,
	`CREATE INDEX IF NOT EXISTS idx_routes_start_ts ON routes(start_ts)`,

	`CREATE TABLE IF NOT EXISTS processed_files (
		file      TEXT PRIMARY KEY,
		added_at  INTEGER NOT NULL
	) WITHOUT ROWID`,

	`CREATE TABLE IF NOT EXISTS drive_tags (
		drive_key TEXT NOT NULL,
		tag       TEXT NOT NULL,
		PRIMARY KEY (drive_key, tag)
	) WITHOUT ROWID`,

	`CREATE INDEX IF NOT EXISTS idx_drive_tags_tag ON drive_tags(tag)`,
}

// migrate brings the DB up to currentSchemaVersion. Safe to call on every
// open; idempotent by construction (all DDL is IF NOT EXISTS, and the
// schema_version key is only set if missing so it survives future bumps).
//
// For schema upgrades between versions, extend this function with a switch
// on the stored schema_version. Today there is only version 1, so we just
// apply the schema and mark it as such.
func migrate(ctx context.Context, db *sql.DB) error {
	for _, stmt := range schemaStatements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: applying DDL %q: %w", truncate(stmt, 60), err)
		}
	}

	// Seed schema_version only if not already set — respects any
	// future-version DB that was restored onto an older binary, and
	// leaves user-written state untouched.
	if _, err := metaGet(ctx, db, "schema_version"); err == sql.ErrNoRows {
		if err := metaSet(ctx, db, "schema_version", fmt.Sprintf("%d", currentSchemaVersion)); err != nil {
			return fmt.Errorf("migrate: setting schema_version: %w", err)
		}
	}

	// Record DB creation time for observability. Only set on first migrate.
	if _, err := metaGet(ctx, db, "created_at"); err == sql.ErrNoRows {
		if err := metaSet(ctx, db, "created_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("migrate: setting created_at: %w", err)
		}
	}

	return nil
}

// metaGet reads a value from the meta table. Returns sql.ErrNoRows when
// the key doesn't exist so callers can distinguish "unset" from "empty".
func metaGet(ctx context.Context, db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v, err
}

// metaSet upserts a meta key/value pair.
func metaSet(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// truncate returns s clipped to maxLen with an ellipsis marker; used for
// error messages so a full DDL dump doesn't blow up log output.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
