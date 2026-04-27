package drives

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"time"

	"modernc.org/sqlite"
)

// SQLite result codes we branch on. Hardcoded to avoid importing the
// modernc.org/sqlite/lib internal package just for two constants.
const (
	sqliteBusy   = 5  // SQLITE_BUSY
	sqliteLocked = 6  // SQLITE_LOCKED
	sqliteFull   = 13 // SQLITE_FULL
)

// isBusyErr reports whether err came from the SQLite driver with a
// BUSY or LOCKED result code. These are the only codes worth retrying:
// CONSTRAINT / CORRUPT / FULL / etc. are terminal and should bubble up.
func isBusyErr(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	c := se.Code()
	return c == sqliteBusy || c == sqliteLocked
}

// isDiskFullErr reports whether err is a SQLITE_FULL. Callers use this
// to return a sentinel (errDiskFull) so the async backfill runner can
// surface "migration paused: disk full" to the UI instead of looping.
func isDiskFullErr(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	return se.Code() == sqliteFull
}

// busyRetryAttempts is the maximum number of attempts withBusyRetry
// makes before giving up. Four gets us ~1.5s of total wait in the
// worst case, which is well above the PRAGMA busy_timeout=5000ms
// already set at the driver level -- the retries cover busy classes
// the driver doesn't retry internally (e.g., WAL checkpoint contention).
const busyRetryAttempts = 4

// withBusyRetry runs fn, retrying on SQLITE_BUSY / SQLITE_LOCKED with
// jittered exponential backoff (50ms, 100ms, 200ms, 400ms + up to 50%
// jitter). All other errors -- and success -- return immediately.
//
// modernc.org/sqlite does not auto-retry on busy-class errors the way
// the C shell does; without this, AddRoute and SetDriveTags can silently
// drop writes when they collide with a WAL checkpoint on a slow SD card.
// Every retry logs a single line so the behaviour is visible in diagnostics.
func withBusyRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < busyRetryAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !isBusyErr(err) {
			return err
		}
		lastErr = err
		// Exponential backoff with jitter. Start at 50ms so the fast
		// path (contention clears within one checkpoint cycle) doesn't
		// pay a noticeable latency penalty.
		backoff := time.Duration(50<<attempt) * time.Millisecond
		backoff += time.Duration(rand.Int63n(int64(backoff) / 2))
		log.Printf("[drives] SQLITE_BUSY retry %d (backoff %s): %v", attempt+1, backoff, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return lastErr
}
