package drives

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreFromArchive_StreamsCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "archive-drive-data.json")
	dst := filepath.Join(dir, "mutable", "drive-data.json")

	content := []byte(`{"routes":[],"processed_files":[]}`)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := restoreFromArchive(src, dst); err != nil {
		t.Fatalf("restoreFromArchive: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("dst content mismatch: got %q, want %q", got, content)
	}

	// tmp file must be gone after rename
	if _, err := os.Stat(dst + ".restore.tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file lingered after rename: err=%v", err)
	}
}

func TestRestoreFromArchive_SkipsWhenDestExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "archive.json")
	dst := filepath.Join(dir, "dst.json")

	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := restoreFromArchive(src, dst); err != nil {
		t.Fatalf("restoreFromArchive: %v", err)
	}

	got, _ := os.ReadFile(dst)
	if string(got) != "existing" {
		t.Errorf("dst was overwritten: got %q, want %q", got, "existing")
	}
}

func TestRestoreFromArchive_SilentWhenSourceMissing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "does-not-exist.json")
	dst := filepath.Join(dir, "dst.json")

	// Missing source is a best-effort no-op, not an error -- the archive
	// mount may simply not be present on this boot.
	if err := restoreFromArchive(src, dst); err != nil {
		t.Fatalf("restoreFromArchive on missing src: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst was created despite missing src: err=%v", err)
	}
}

func TestRestoreFromArchive_RefusesOversizedSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "huge.json")
	dst := filepath.Join(dir, "dst.json")

	// Create a sparse file exceeding the 2GiB cap without actually
	// writing 2GiB of data -- Truncate is instant on any modern FS.
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxRestoreSize + 1); err != nil {
		f.Close()
		t.Fatalf("truncate: %v", err)
	}
	f.Close()

	err = restoreFromArchive(src, dst)
	if err == nil {
		t.Fatal("expected error for oversized source, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error %q doesn't mention size cap", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst was created despite oversized src: err=%v", err)
	}
}

// TestRestoreFromArchive_AtomicOnCrash verifies that a failed copy leaves
// the destination in its prior state (either absent or unchanged). We
// simulate the crash by pointing the dest into a directory that isn't
// writable; the tmp-file strategy must not leave partial data at the
// final path.
func TestRestoreFromArchive_AtomicOnCrash(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions; test is meaningless")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "archive.json")
	ro := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(ro, "dst.json")

	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := restoreFromArchive(src, dst)
	if err == nil {
		t.Fatal("expected error copying into read-only dir, got nil")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst exists despite failed copy: err=%v", err)
	}
	if _, err := os.Stat(dst + ".restore.tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file lingered after failed copy: err=%v", err)
	}
}
