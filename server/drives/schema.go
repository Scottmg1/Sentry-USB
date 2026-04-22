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
//
// v1 -> v2: add precomputed per-route aggregate columns (distance, speeds,
// autopilot-mode time/distance, disengagement counts, start/end lat-lon)
// so the Drives-page summary endpoints can scan BLOB-free rows. See
// aggregate.go for the semantics.
const currentSchemaVersion = 2

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

// v2SummaryColumns lists the per-route aggregate columns added in v2.
// Types are kept simple (REAL for floats, INTEGER for counts) and all
// are nullable so pre-v2 rows don't need a synchronous backfill during
// migrate(); the one-shot backfill in Load() populates them afterward.
// The `distance_m` column already existed in v1 (as REAL NOT NULL
// DEFAULT 0 but never meaningfully populated) and is reused here.
var v2RouteSummaryColumns = []struct {
	name string
	typ  string
}{
	{"max_speed_mps", "REAL"},
	{"avg_speed_mps", "REAL"},
	{"speed_sample_count", "INTEGER"},
	{"valid_point_count", "INTEGER"},
	{"fsd_engaged_ms", "INTEGER"},
	{"autosteer_engaged_ms", "INTEGER"},
	{"tacc_engaged_ms", "INTEGER"},
	{"fsd_distance_m", "REAL"},
	{"autosteer_distance_m", "REAL"},
	{"tacc_distance_m", "REAL"},
	{"assisted_distance_m", "REAL"},
	{"fsd_disengagements", "INTEGER"},
	{"fsd_accel_pushes", "INTEGER"},
	{"start_lat", "REAL"},
	{"start_lon", "REAL"},
	{"end_lat", "REAL"},
	{"end_lon", "REAL"},
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

	// v2 upgrade path: add per-route aggregate columns on existing v1
	// DBs. ALTER TABLE ADD COLUMN is cheap in SQLite (just a metadata
	// update — rows keep their existing layout, new column reads as
	// NULL until written). We check column presence via pragma_table_info
	// rather than parsing schema_version to stay robust against DBs
	// that were restored from future-version backups (schema_version
	// might be ahead of the actual columns present).
	existing, err := listRouteColumns(ctx, db)
	if err != nil {
		return fmt.Errorf("migrate: list routes columns: %w", err)
	}
	for _, col := range v2RouteSummaryColumns {
		if existing[col.name] {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE routes ADD COLUMN %s %s`, col.name, col.typ)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: adding routes.%s: %w", col.name, err)
		}
	}

	// schema_version handling:
	//   - first-ever migrate: seed to currentSchemaVersion.
	//   - upgrading from an older version (e.g. v1 DB): bump up to
	//     currentSchemaVersion. Downgrades (e.g. a v3 DB hit by a v2
	//     binary) are preserved — never clobber a future-version marker
	//     we don't understand.
	cur, err := metaGet(ctx, db, "schema_version")
	switch {
	case err == sql.ErrNoRows:
		if err := metaSet(ctx, db, "schema_version",
			fmt.Sprintf("%d", currentSchemaVersion)); err != nil {
			return fmt.Errorf("migrate: setting schema_version: %w", err)
		}
	case err != nil:
		return fmt.Errorf("migrate: reading schema_version: %w", err)
	case cur != "":
		// Only advance when the stored version is strictly older than
		// what this binary knows about; keep anything at or ahead of
		// currentSchemaVersion untouched.
		if storedLessThan(cur, currentSchemaVersion) {
			if err := metaSet(ctx, db, "schema_version",
				fmt.Sprintf("%d", currentSchemaVersion)); err != nil {
				return fmt.Errorf("migrate: advancing schema_version: %w", err)
			}
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

// storedLessThan returns true when the string-encoded schema_version is
// numerically less than target. Non-numeric values (corrupted meta
// table) are treated as "older" so migrate() gets a chance to heal them.
func storedLessThan(stored string, target int) bool {
	// Handle the common case (single-digit versions) without importing
	// strconv for just a tiny parse — this keeps the migration path
	// dependency-free. For multi-digit we fall through to numeric parse.
	if len(stored) == 1 && stored[0] >= '0' && stored[0] <= '9' {
		return int(stored[0]-'0') < target
	}
	var n int
	for i := 0; i < len(stored); i++ {
		c := stored[i]
		if c < '0' || c > '9' {
			return true // unparseable → treat as older
		}
		n = n*10 + int(c-'0')
	}
	return n < target
}

// listRouteColumns returns a set of column names present on the routes
// table. Used by migrate to decide which v2 ALTER TABLEs to run.
func listRouteColumns(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('routes')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
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
