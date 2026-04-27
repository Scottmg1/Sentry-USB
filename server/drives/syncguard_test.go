package drives

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// -----------------------------------------------------------------------------
// checkSyncSizeGuard — pure decision logic
// -----------------------------------------------------------------------------

func TestCheckSyncSizeGuard_AllowsWhenNoCache(t *testing.T) {
	// lastSize=0 means "we've never successfully synced before" — always allow.
	if err := checkSyncSizeGuard(100, 0); err != nil {
		t.Fatalf("expected nil (first-ever sync), got %v", err)
	}
}

func TestCheckSyncSizeGuard_AllowsWhenLastSizeBelowMinThreshold(t *testing.T) {
	// Under 10 MB cached, don't enforce ratio — user might be legitimately
	// shrinking from a small baseline.
	if err := checkSyncSizeGuard(1_000, 5*1024*1024); err != nil {
		t.Fatalf("expected nil (lastSize below threshold), got %v", err)
	}
}

func TestCheckSyncSizeGuard_AllowsWhenNewIsLarger(t *testing.T) {
	// Normal growth case.
	if err := checkSyncSizeGuard(200_000_000, 100_000_000); err != nil {
		t.Fatalf("expected nil (growing file), got %v", err)
	}
}

func TestCheckSyncSizeGuard_AllowsWhenNewIsExactlyHalf(t *testing.T) {
	// Boundary: exactly 50% is allowed (guard only trips on < 50%).
	if err := checkSyncSizeGuard(50_000_000, 100_000_000); err != nil {
		t.Fatalf("expected nil (exactly 50%%), got %v", err)
	}
}

func TestCheckSyncSizeGuard_RefusesWhenNewIsUnderHalf(t *testing.T) {
	err := checkSyncSizeGuard(49_999_999, 100_000_000)
	if err == nil {
		t.Fatal("expected guard error, got nil")
	}
	var guardErr *ErrSyncGuard
	if !errors.As(err, &guardErr) {
		t.Fatalf("expected *ErrSyncGuard, got %T: %v", err, err)
	}
	if guardErr.NewSize != 49_999_999 || guardErr.LastSize != 100_000_000 {
		t.Fatalf("guard err has wrong fields: %+v", guardErr)
	}
}

func TestCheckSyncSizeGuard_TheIncidentRegression(t *testing.T) {
	// Regression for the Mon 20 Apr 2026 incident: a 122 MB stub
	// drive-data.json was synced over the healthy 762 MB archive copy.
	// This test proves the guard catches that exact scenario.
	const incidentNew = int64(122_627_161)  // from diagnostics.log:10850
	const incidentLast = int64(762_954_115) // from diagnostics.log:10279
	err := checkSyncSizeGuard(incidentNew, incidentLast)
	if err == nil {
		t.Fatal("guard must refuse the incident's 122 MB -> 762 MB overwrite")
	}
	var guardErr *ErrSyncGuard
	if !errors.As(err, &guardErr) {
		t.Fatalf("expected *ErrSyncGuard, got %T: %v", err, err)
	}
}

// -----------------------------------------------------------------------------
// Cache I/O
// -----------------------------------------------------------------------------

func TestSyncCache_MissingFileReturnsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist")
	size, err := readSyncCache(path)
	if err != nil {
		t.Fatalf("missing cache should return nil error, got %v", err)
	}
	if size != 0 {
		t.Fatalf("missing cache should return 0 size, got %d", size)
	}
}

func TestSyncCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-sync")
	if err := writeSyncCache(path, 762_954_115); err != nil {
		t.Fatalf("writeSyncCache failed: %v", err)
	}
	size, err := readSyncCache(path)
	if err != nil {
		t.Fatalf("readSyncCache failed: %v", err)
	}
	if size != 762_954_115 {
		t.Fatalf("round-trip mismatch: wrote 762954115, read %d", size)
	}
}

func TestSyncCache_CorruptFileFailsOpen(t *testing.T) {
	// If the cache file is garbage (disk corruption, manual edit), we must
	// NOT block sync — fail open, log nothing fatal, treat as missing.
	// A blocked sync because of a corrupted cache would be worse than
	// a missed guard.
	dir := t.TempDir()
	path := filepath.Join(dir, "last-sync")
	if err := os.WriteFile(path, []byte("garbage\x00\xff not a number"), 0644); err != nil {
		t.Fatal(err)
	}
	size, err := readSyncCache(path)
	if err != nil {
		t.Fatalf("corrupt cache should fail open (nil err), got %v", err)
	}
	if size != 0 {
		t.Fatalf("corrupt cache should return 0 size, got %d", size)
	}
}

func TestSyncCache_NegativeSizeRejected(t *testing.T) {
	// Negative sizes make no sense — writer should error.
	dir := t.TempDir()
	path := filepath.Join(dir, "last-sync")
	if err := writeSyncCache(path, -1); err == nil {
		t.Fatal("expected writeSyncCache to reject negative size")
	}
}

func TestSyncCache_OverwriteIsAtomic(t *testing.T) {
	// Repeated writes must leave a readable value at every observation
	// point. We can't easily test atomicity, but we can test that two
	// sequential writes leave the second value readable.
	dir := t.TempDir()
	path := filepath.Join(dir, "last-sync")
	if err := writeSyncCache(path, 100); err != nil {
		t.Fatal(err)
	}
	if err := writeSyncCache(path, 200); err != nil {
		t.Fatal(err)
	}
	size, err := readSyncCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if size != 200 {
		t.Fatalf("want 200 after overwrite, got %d", size)
	}
	// No leftover .tmp
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp to be removed, got stat err %v", err)
	}
}
