package drives

import (
	"context"
	"errors"
	"log"
	"time"
)

// MigrationStatus is a snapshot of the async aggregate-backfill state.
// Served via the /api/drives/migration-status endpoint and broadcast on
// WebSocket progress updates so the UI can show a banner + progress bar
// during the upgrade.
//
// Active distinguishes "migration running right now" from "migration has
// never run" (fresh install) and "migration finished cleanly" (idle).
// Done/Total are valid only while Active=true; afterwards they reflect
// the last run's counts until the server restarts.
type MigrationStatus struct {
	Active   bool      `json:"active"`
	Done     int       `json:"done"`
	Total    int       `json:"total"`
	Error    string    `json:"error,omitempty"`
	DiskFull bool      `json:"disk_full,omitempty"`
	StartAt  time.Time `json:"start_at,omitempty"`
	EndAt    time.Time `json:"end_at,omitempty"`
}

// migrationState is the concrete value stored in Store.migrationStatus.
// Kept separate from MigrationStatus so internal transitions
// (setActive, recordProgress) don't require callers to construct a
// fully-populated snapshot struct.
type migrationState struct {
	snap MigrationStatus
}

// StartBackgroundBackfill kicks off the aggregate-column backfill in a
// goroutine and returns immediately so Load()'s caller can start serving
// HTTP while the migration continues in the background. Safe to call
// more than once: subsequent calls are no-ops if a backfill is already
// running OR if there's no NULL-aggregate work pending.
//
// The supplied ctx is used for cancellation -- on server shutdown, the
// caller should cancel it so the in-flight batch's tx rolls back
// cleanly (the NULL-aggregate sentinel guarantees resume on next boot).
//
// onProgress is called from the backfill goroutine after every batch
// commits; it receives (done, total) so callers can broadcast to WS
// clients or update a status dashboard. Pass nil to disable.
//
// Returns true if a new goroutine was started, false if the backfill
// was already running or had nothing to do.
func (s *Store) StartBackgroundBackfill(ctx context.Context, onProgress func(done, total int)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only one concurrent backfill. Checking the atomic before spawning
	// (and publishing "active=true" inside the goroutine under the same
	// lock) keeps the invariant simple.
	cur := s.loadMigrationStatus()
	if cur.Active {
		return false
	}

	// Cheap pre-check: count NULL rows. If zero, don't even spawn.
	// This mirrors the sentinel the backfill itself uses, so a fresh
	// install with zero routes skips the whole async dance.
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM routes WHERE max_speed_mps IS NULL`,
	).Scan(&total); err != nil {
		log.Printf("[drives] StartBackgroundBackfill pre-count: %v", err)
		return false
	}
	if total == 0 {
		return false
	}

	start := time.Now()
	s.storeMigrationStatus(MigrationStatus{
		Active:  true,
		Done:    0,
		Total:   total,
		StartAt: start,
	})
	if s.migrationDoneCh != nil {
		// Drain any leftover signal from a prior completed run before
		// we start a new one so future Close()/waitForMigration calls
		// don't see a stale "done" tick.
		select {
		case <-s.migrationDoneCh:
		default:
		}
	} else {
		s.migrationDoneCh = make(chan struct{})
	}

	go s.runBackgroundBackfill(ctx, start, onProgress)
	return true
}

// runBackgroundBackfill is the goroutine body spawned by
// StartBackgroundBackfill. It takes no store lock of its own --
// backfillRouteAggregates only uses the *sql.DB and SQLite serializes
// internally via the WAL + SetMaxOpenConns(1) pool.
func (s *Store) runBackgroundBackfill(ctx context.Context, start time.Time, onProgress func(done, total int)) {
	defer func() {
		s.mu.Lock()
		if s.migrationDoneCh != nil {
			select {
			case <-s.migrationDoneCh:
			default:
				close(s.migrationDoneCh)
			}
		}
		s.mu.Unlock()
	}()

	stats, err := backfillRouteAggregates(ctx, s.db, func(done, total int) {
		// Publish progress snapshot. Total can only grow on new inserts
		// during the run, which is rare at boot time; if it happens,
		// the later pass catches the new NULL rows via the sentinel.
		snap := s.loadMigrationStatus()
		snap.Done = done
		if total > snap.Total {
			snap.Total = total
		}
		s.storeMigrationStatus(snap)
		if onProgress != nil {
			onProgress(done, snap.Total)
		}
	})

	// Finalize status snapshot.
	final := s.loadMigrationStatus()
	final.Active = false
	final.EndAt = time.Now()
	final.Done = stats.Updated + final.Done // stats.Updated is the count-so-far; prefer whichever is larger for idempotence
	if stats.Updated > final.Done {
		final.Done = stats.Updated
	}
	if err != nil {
		final.Error = err.Error()
		if errors.Is(err, ErrBackfillDiskFull) {
			final.DiskFull = true
			log.Printf("[drives] Background backfill paused: disk full (%d/%d)", final.Done, final.Total)
		} else if !errors.Is(err, context.Canceled) {
			log.Printf("[drives] Background backfill error after %s: %v", time.Since(start), err)
		} else {
			log.Printf("[drives] Background backfill cancelled after %s (%d/%d done, will resume on next boot)",
				time.Since(start), final.Done, final.Total)
		}
	} else {
		log.Printf("[drives] Background backfill complete in %s: %d routes updated",
			time.Since(start), stats.Updated)
		// Clean-run observability marker matches the pre-async behavior.
		if stats.Updated > 0 {
			if err := metaSet(ctx, s.db, "summary_backfilled_at",
				time.Now().UTC().Format(time.RFC3339)); err != nil {
				log.Printf("[drives] summary_backfilled_at write: %v", err)
			}
		}
	}
	s.storeMigrationStatus(final)
}

// MigrationStatus returns the current migration snapshot. Safe to call
// concurrently with an in-flight backfill; reads are atomic.
func (s *Store) MigrationStatus() MigrationStatus {
	return s.loadMigrationStatus()
}

// WaitForMigration blocks until the background backfill completes or
// the supplied ctx is cancelled. Returns nil if backfill completed (or
// was never running), or the ctx error. Used by Close() for graceful
// shutdown and by tests.
func (s *Store) WaitForMigration(ctx context.Context) error {
	s.mu.Lock()
	ch := s.migrationDoneCh
	status := s.loadMigrationStatus()
	s.mu.Unlock()
	if ch == nil || !status.Active {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loadMigrationStatus reads the current snapshot atomically. Initial
// state (before any StartBackgroundBackfill) returns a zero
// MigrationStatus with Active=false.
func (s *Store) loadMigrationStatus() MigrationStatus {
	v := s.migrationStatus.Load()
	if v == nil {
		return MigrationStatus{}
	}
	st, ok := v.(*migrationState)
	if !ok || st == nil {
		return MigrationStatus{}
	}
	return st.snap
}

// storeMigrationStatus publishes a new snapshot atomically.
func (s *Store) storeMigrationStatus(snap MigrationStatus) {
	s.migrationStatus.Store(&migrationState{snap: snap})
}

// Close cancels any in-flight background backfill and releases the DB
// handle. waitTimeout bounds how long we wait for the backfill
// goroutine to acknowledge cancellation before we give up and close
// the DB anyway -- SQLite's open tx will roll back either way, so the
// DB is always consistent; we just might leave a no-op goroutine to
// finish at its own pace.
func (s *Store) Close(waitTimeout time.Duration) error {
	// Signal cancel to any running backfill. The goroutine uses the
	// ctx we were given via StartBackgroundBackfill, which the caller
	// derived from the server shutdown ctx. Signal by closing the
	// store-scoped cancel func if we own one.
	if s.backfillCancel != nil {
		s.backfillCancel()
	}
	wctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	if err := s.WaitForMigration(wctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		log.Printf("[drives] Close: wait for migration: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}
