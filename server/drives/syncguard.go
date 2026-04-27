package drives

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// syncGuardMinThreshold is the minimum cached size (bytes) required before the
// 50% ratio guard is enforced. Below this, we always allow the sync — tiny
// datasets don't need corruption protection because there's little to lose.
const syncGuardMinThreshold = int64(10 * 1024 * 1024) // 10 MB

// syncGuardRatio is the minimum fraction of lastSize that a new sync must
// meet to be allowed. 0.5 means "new file must be at least half the size of
// the last successful sync".
const syncGuardRatio = 0.5

// DefaultSyncCachePath is where the last-successful-sync size is recorded.
// Lives on /mutable so it survives reboots; small enough not to impact disk
// pressure (~20 bytes).
const DefaultSyncCachePath = "/mutable/.drive-data-last-sync"

// ErrSyncGuard is returned by SyncToArchive (and higher-level callers) when
// the size-guard refuses to overwrite an archive copy because the new file
// is dramatically smaller than the last known good sync — the signature of
// the data-loss scenario this guard was built to prevent.
type ErrSyncGuard struct {
	NewSize  int64
	LastSize int64
}

func (e *ErrSyncGuard) Error() string {
	return fmt.Sprintf(
		"size guard: refusing to sync %d bytes — less than %.0f%% of last successful sync (%d bytes). Local file may be corrupted; archive preserved.",
		e.NewSize, syncGuardRatio*100, e.LastSize,
	)
}

// checkSyncSizeGuard returns nil if a sync of newSize bytes should proceed, or
// *ErrSyncGuard if the new file is dramatically smaller than the last known
// good sync (and the last sync was above the minimum threshold).
//
// Fails open: lastSize=0 (no cache, first-ever sync, or corrupt cache) always
// allows the sync.
func checkSyncSizeGuard(newSize, lastSize int64) error {
	if lastSize <= 0 {
		return nil
	}
	if lastSize < syncGuardMinThreshold {
		return nil
	}
	minAllowed := int64(float64(lastSize) * syncGuardRatio)
	if newSize >= minAllowed {
		return nil
	}
	return &ErrSyncGuard{NewSize: newSize, LastSize: lastSize}
}

// readSyncCache returns the last-successful-sync size in bytes, or 0 if the
// cache file doesn't exist or is unreadable/corrupt. This is fail-open by
// design: a corrupted cache file must not block syncs — losing the guard
// temporarily is better than blocking a legitimate update forever.
func readSyncCache(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		// Read error on an existing file — treat as missing to fail open.
		return 0, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, nil
	}
	if n < 0 {
		return 0, nil
	}
	return n, nil
}

// writeSyncCache atomically records size as the last-successful-sync size.
// Uses tmp + rename to survive partial writes.
func writeSyncCache(path string, size int64) error {
	if size < 0 {
		return fmt.Errorf("writeSyncCache: size must be non-negative, got %d", size)
	}
	tmp := path + ".tmp"
	data := []byte(strconv.FormatInt(size, 10))
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
