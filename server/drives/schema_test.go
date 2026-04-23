package drives

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// openTestDB opens a fresh file-backed SQLite DB in a tempdir and returns it
// plus a cleanup. File-backed (not :memory:) so WAL actually engages, matching
// production conditions.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "test.db") +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(on)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=temp_store(FILE)" +
		"&_pragma=auto_vacuum(incremental)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// listTables returns all user-created tables (excludes sqlite_* internals),
// sorted alphabetically for stable comparison.
func listTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

// listIndexes returns user-created indexes (excludes auto-generated and
// sqlite_* internals), sorted alphabetically.
func listIndexes(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func TestMigrate_CreatesAllTables(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := listTables(t, db)
	want := []string{"drive_tags", "meta", "processed_files", "routes"}
	if len(got) != len(want) {
		t.Fatalf("tables: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestMigrate_EnablesIncrementalAutoVacuum confirms that a fresh DB ends
// up in auto_vacuum=INCREMENTAL mode. This is the only mode SQLite can
// adopt via PRAGMA alone (FULL and NONE require a VACUUM on populated
// DBs). It's what stops the .db file from growing unbounded across
// repeated ReplaceData wipe-and-restore cycles on SD-card storage.
//
// auto_vacuum modes per SQLite: 0=NONE, 1=FULL, 2=INCREMENTAL.
func TestMigrate_EnablesIncrementalAutoVacuum(t *testing.T) {
	db := openTestDB(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var mode int
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		t.Fatalf("read auto_vacuum: %v", err)
	}
	if mode != 2 {
		t.Errorf("auto_vacuum = %d, want 2 (INCREMENTAL) on fresh DB", mode)
	}
}

func TestMigrate_CreatesExpectedIndexes(t *testing.T) {
	db := openTestDB(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := listIndexes(t, db)
	// The queries we expect to power: date-range (date_dir), time-range
	// (start_ts), and tag filtering.
	want := []string{"idx_drive_tags_tag", "idx_routes_date_dir", "idx_routes_start_ts"}
	for _, name := range want {
		found := false
		for _, g := range got {
			if g == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected index %q; have %v", name, got)
		}
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	// Running migrate multiple times on the same DB must succeed and leave
	// the schema unchanged — important because Load() will call it every
	// startup.
	for i := 0; i < 3; i++ {
		if err := migrate(ctx, db); err != nil {
			t.Fatalf("migrate pass %d: %v", i+1, err)
		}
	}
	got := listTables(t, db)
	if len(got) != 4 {
		t.Fatalf("tables after 3x migrate: %v (want 4)", got)
	}
}

func TestMigrate_SeedsSchemaVersion(t *testing.T) {
	db := openTestDB(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	v, err := metaGet(context.Background(), db, "schema_version")
	if err != nil {
		t.Fatalf("metaGet schema_version: %v", err)
	}
	if v != "2" {
		t.Fatalf("schema_version = %q, want %q", v, "2")
	}
}

// listColumns returns all column names on a table, sorted.
func listColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// v2SummaryColumns is the set of per-route aggregate columns added in
// schema v2. Fresh installs get them via migrate(); existing v1 DBs get
// them via the v2 ALTER TABLE path.
var v2SummaryColumns = []string{
	"max_speed_mps",
	"avg_speed_mps",
	"speed_sample_count",
	"valid_point_count",
	"fsd_engaged_ms",
	"autosteer_engaged_ms",
	"tacc_engaged_ms",
	"fsd_distance_m",
	"autosteer_distance_m",
	"tacc_distance_m",
	"assisted_distance_m",
	"fsd_disengagements",
	"fsd_accel_pushes",
	"start_lat",
	"start_lon",
	"end_lat",
	"end_lon",
}

func TestMigrate_V2AddsSummaryColumns(t *testing.T) {
	db := openTestDB(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := listColumns(t, db, "routes")
	have := map[string]bool{}
	for _, c := range got {
		have[c] = true
	}
	for _, want := range v2SummaryColumns {
		if !have[want] {
			t.Errorf("missing v2 column %q on routes; have %v", want, got)
		}
	}
}

// TestMigrate_V2UpgradesExistingV1DB simulates the production upgrade
// path: a DB that was created by the v1 binary gets the new columns
// ALTERed in on the next migrate() pass.
func TestMigrate_V2UpgradesExistingV1DB(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Build a v1 DB by running only the v1 statements.
	v1 := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE routes (
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
		`CREATE TABLE processed_files (file TEXT PRIMARY KEY, added_at INTEGER NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE drive_tags (drive_key TEXT NOT NULL, tag TEXT NOT NULL, PRIMARY KEY (drive_key, tag)) WITHOUT ROWID`,
	}
	for _, s := range v1 {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("v1 DDL: %v", err)
		}
	}
	if err := metaSet(ctx, db, "schema_version", "1"); err != nil {
		t.Fatal(err)
	}

	// Seed a route row so we can verify the upgrade preserves data.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO routes(file, date_dir, points_blob, updated_at)
		VALUES('clip/a.mp4', '2026-04-20', x'', 0)`); err != nil {
		t.Fatal(err)
	}

	// Run migrate(): should add v2 columns without dropping data.
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate v1->v2: %v", err)
	}
	got := listColumns(t, db, "routes")
	have := map[string]bool{}
	for _, c := range got {
		have[c] = true
	}
	for _, want := range v2SummaryColumns {
		if !have[want] {
			t.Errorf("after upgrade: missing %q; have %v", want, got)
		}
	}
	// Pre-existing row must survive and have NULL aggregates.
	var fsdEngaged sql.NullInt64
	var startLat sql.NullFloat64
	if err := db.QueryRowContext(ctx,
		`SELECT fsd_engaged_ms, start_lat FROM routes WHERE file = 'clip/a.mp4'`,
	).Scan(&fsdEngaged, &startLat); err != nil {
		t.Fatalf("scan upgraded row: %v", err)
	}
	if fsdEngaged.Valid {
		t.Errorf("expected NULL fsd_engaged_ms on upgraded row, got %d", fsdEngaged.Int64)
	}
	if startLat.Valid {
		t.Errorf("expected NULL start_lat on upgraded row, got %v", startLat.Float64)
	}
	// schema_version must have advanced.
	v, _ := metaGet(ctx, db, "schema_version")
	if v != "2" {
		t.Fatalf("schema_version after upgrade: got %q, want %q", v, "2")
	}
}

func TestMigrate_SeedsCreatedAt(t *testing.T) {
	db := openTestDB(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	v, err := metaGet(context.Background(), db, "created_at")
	if err != nil {
		t.Fatalf("metaGet created_at: %v", err)
	}
	if v == "" {
		t.Fatal("created_at should have been set by migrate")
	}
}

func TestMigrate_DoesNotOverwriteSchemaVersion(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Simulate a future version bump having already run by forcing schema_version=2.
	// migrate() must not clobber it back to 1.
	if err := metaSet(ctx, db, "schema_version", "2"); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate re-run: %v", err)
	}
	v, _ := metaGet(ctx, db, "schema_version")
	if v != "2" {
		t.Fatalf("schema_version after re-migrate: got %q, want %q (must not clobber)", v, "2")
	}
}

func TestMeta_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := metaSet(ctx, db, "hello", "world"); err != nil {
		t.Fatal(err)
	}
	v, err := metaGet(ctx, db, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if v != "world" {
		t.Fatalf("got %q, want %q", v, "world")
	}
	// Upsert overwrites
	if err := metaSet(ctx, db, "hello", "there"); err != nil {
		t.Fatal(err)
	}
	v, _ = metaGet(ctx, db, "hello")
	if v != "there" {
		t.Fatalf("after upsert: got %q, want %q", v, "there")
	}
}

func TestMeta_MissingKeyReturnsErrNoRows(t *testing.T) {
	db := openTestDB(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	_, err := metaGet(context.Background(), db, "not-present")
	if err != sql.ErrNoRows {
		t.Fatalf("want sql.ErrNoRows, got %v", err)
	}
}

func TestRoutes_UniqueOnFile(t *testing.T) {
	// The routes table uses file as PRIMARY KEY so AddRoute upserts work
	// and duplicate inserts error cleanly.
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO routes(file, date_dir, point_count, points_blob, updated_at) VALUES(?, ?, ?, ?, ?)`
	if _, err := db.ExecContext(ctx, insert, "a/b.mp4", "2026-04-20_14-30-00", 0, []byte{}, 0); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := db.ExecContext(ctx, insert, "a/b.mp4", "2026-04-20_14-30-00", 0, []byte{}, 0)
	if err == nil {
		t.Fatal("second insert with same file should fail (primary key)")
	}
}

func TestDriveTags_CompositePK(t *testing.T) {
	// (drive_key, tag) is the PK — same tag twice on the same drive errors,
	// different tags on the same drive are fine.
	db := openTestDB(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO drive_tags(drive_key, tag) VALUES(?, ?)`, "2026-04-20T14:30:00", "work"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO drive_tags(drive_key, tag) VALUES(?, ?)`, "2026-04-20T14:30:00", "commute"); err != nil {
		t.Fatalf("second tag same drive: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO drive_tags(drive_key, tag) VALUES(?, ?)`, "2026-04-20T14:30:00", "work"); err == nil {
		t.Fatal("duplicate (drive_key, tag) should fail")
	}
}
