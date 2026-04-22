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
		"&_pragma=busy_timeout(5000)"
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
	if v != "1" {
		t.Fatalf("schema_version = %q, want %q", v, "1")
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
