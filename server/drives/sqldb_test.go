package drives

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestSQLiteDriverLoads is a smoke test that proves the pure-Go
// modernc.org/sqlite driver is compiled in and can open an in-memory
// database. If this test fails the whole SQLite migration is broken, so it
// runs first and gets the most defensive assertions.
//
// It also catches cross-compile/CGO surprises early: the test must pass
// natively AND `go vet ./drives/...` must stay clean, which exercises the
// package's type-checking against the sqlite import.
func TestSQLiteDriverLoads(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open(sqlite, :memory:): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}

	// Minimal round-trip: create a table, insert, read back. Proves the
	// driver actually works end-to-end, not just registers.
	if _, err := db.Exec(`CREATE TABLE probe (k TEXT PRIMARY KEY, v INTEGER)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO probe(k, v) VALUES (?, ?)`, "answer", 42); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	var v int
	if err := db.QueryRow(`SELECT v FROM probe WHERE k = ?`, "answer").Scan(&v); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if v != 42 {
		t.Fatalf("round-trip: got %d, want 42", v)
	}
}

// TestSQLiteWALAndPragmas verifies the pragmas we plan to use in production
// (WAL, synchronous=NORMAL, foreign_keys=on, busy_timeout, temp_store=FILE)
// are accepted and reported back correctly by the driver. This pins a known-
// good configuration so a future modernc.org/sqlite upgrade that changes
// pragma handling fails here visibly instead of silently regressing
// durability.
//
// temp_store is set to FILE (not MEMORY) so SQLite spills internal sort/
// group temporaries to /mutable on 512MB Pis instead of bloating the Go
// heap during backfill and ReplaceData.
func TestSQLiteWALAndPragmas(t *testing.T) {
	// Use a file-backed DB in a tempdir so WAL can actually engage
	// (WAL is not supported on :memory: databases).
	dir := t.TempDir()
	dsn := "file:" + dir + "/probe.db" +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(on)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=temp_store(FILE)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Forcing a connection materializes the pragmas.
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}

	checks := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"synchronous", "1"}, // NORMAL
		{"foreign_keys", "1"},
		{"temp_store", "1"}, // FILE
	}
	for _, c := range checks {
		var got string
		if err := db.QueryRow(`PRAGMA ` + c.pragma).Scan(&got); err != nil {
			t.Errorf("PRAGMA %s: %v", c.pragma, err)
			continue
		}
		if got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
}
