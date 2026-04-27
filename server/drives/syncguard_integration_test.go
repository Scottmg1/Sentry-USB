package drives

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a small helper: creates parent dirs, writes size bytes of 'x'
// (representative dummy content, size is what we actually care about).
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = 'x'
	}
	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatal(err)
	}
}

// newStoreForTest builds a *Store rooted at dir/drive-data.json (source side).
// Tests control the destination and cache paths explicitly via syncToPath.
func newStoreForTest(t *testing.T, dir string, sourceSize int) *Store {
	t.Helper()
	src := filepath.Join(dir, "drive-data.json")
	writeFile(t, src, sourceSize)
	return NewStore(src)
}

func TestSyncToPath_FirstTimeCreatesDestAndCache(t *testing.T) {
	dir := t.TempDir()
	s := newStoreForTest(t, dir, 50*1024*1024) // 50 MB source

	dest := filepath.Join(dir, "archive", "drive-data.json")
	cache := filepath.Join(dir, ".drive-data-last-sync")

	if err := s.syncToPath(s.path, dest, cache); err != nil {
		t.Fatalf("first sync should succeed, got %v", err)
	}

	// Destination exists and matches source size
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("destination not created: %v", err)
	}
	if info.Size() != 50*1024*1024 {
		t.Fatalf("dest size = %d, want %d", info.Size(), 50*1024*1024)
	}
	// Cache was recorded
	cached, _ := readSyncCache(cache)
	if cached != 50*1024*1024 {
		t.Fatalf("cache size = %d, want %d", cached, 50*1024*1024)
	}
}

func TestSyncToPath_NormalGrowthUpdatesBoth(t *testing.T) {
	dir := t.TempDir()
	s := newStoreForTest(t, dir, 200*1024*1024) // 200 MB source

	dest := filepath.Join(dir, "archive", "drive-data.json")
	cache := filepath.Join(dir, ".drive-data-last-sync")
	writeFile(t, dest, 100*1024*1024) // 100 MB existing archive
	if err := writeSyncCache(cache, 100*1024*1024); err != nil {
		t.Fatal(err)
	}

	if err := s.syncToPath(s.path, dest, cache); err != nil {
		t.Fatalf("normal growth sync should succeed, got %v", err)
	}

	info, _ := os.Stat(dest)
	if info.Size() != 200*1024*1024 {
		t.Fatalf("dest size = %d, want %d (new should overwrite existing)", info.Size(), 200*1024*1024)
	}
	cached, _ := readSyncCache(cache)
	if cached != 200*1024*1024 {
		t.Fatalf("cache size = %d, want %d (updated to new size)", cached, 200*1024*1024)
	}
}

func TestSyncToPath_RefusesIncidentScenarioAndPreservesArchive(t *testing.T) {
	// The Mon 20 Apr 2026 incident: 122 MB new file attempting to
	// overwrite 762 MB archive. Guard must refuse and the archive
	// file must be left untouched (same size, same content).
	dir := t.TempDir()
	const stubSize = 122_627_161
	const goodSize = 762_954_115

	// Use smaller sizes in the test but preserve the ratio for speed.
	// 16% ratio matches the incident.
	const testStub = 16 * 1024 * 1024
	const testGood = 100 * 1024 * 1024

	s := newStoreForTest(t, dir, testStub)
	dest := filepath.Join(dir, "archive", "drive-data.json")
	cache := filepath.Join(dir, ".drive-data-last-sync")

	// Seed an existing healthy archive + cache recording the good size.
	writeFile(t, dest, testGood)
	if err := writeSyncCache(cache, testGood); err != nil {
		t.Fatal(err)
	}
	// Mark the existing archive file so we can verify it was preserved,
	// not overwritten then restored.
	origContents, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}

	err = s.syncToPath(s.path, dest, cache)
	if err == nil {
		t.Fatal("guard must refuse the 16% overwrite")
	}
	var guardErr *ErrSyncGuard
	if !errors.As(err, &guardErr) {
		t.Fatalf("expected *ErrSyncGuard, got %T: %v", err, err)
	}

	// Archive unchanged
	nowContents, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(nowContents) != len(origContents) {
		t.Fatalf("archive was overwritten: size was %d, now %d", len(origContents), len(nowContents))
	}
	// Cache unchanged — still records the good size, not the stub.
	cached, _ := readSyncCache(cache)
	if cached != testGood {
		t.Fatalf("cache was polluted by refused sync: got %d, want %d", cached, testGood)
	}
	// No leftover .tmp
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("leftover .tmp file after refused sync: %v", err)
	}

	// Sanity check: the real-world numbers also trip the guard.
	if err := checkSyncSizeGuard(stubSize, goodSize); err == nil {
		t.Fatal("real incident numbers should also trip the guard (sanity check)")
	}
}

func TestSyncToPath_MissingSourceReturnsError(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "does-not-exist.json"))
	dest := filepath.Join(dir, "archive", "drive-data.json")
	cache := filepath.Join(dir, ".drive-data-last-sync")

	err := s.syncToPath(s.path, dest, cache)
	if err == nil {
		t.Fatal("expected error when source is missing")
	}
	// And cache must not have been polluted
	cached, _ := readSyncCache(cache)
	if cached != 0 {
		t.Fatalf("cache polluted on error: got %d, want 0", cached)
	}
}
