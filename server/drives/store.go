package drives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultDataPath is where the SQLite drive-data store lives on the Pi.
// Changed from drive-data.json in the JSON->SQLite migration; the JSON
// mirror used for archive sync lives at defaultJSONMirrorPath below and is
// regenerated on demand (see future exportToJSON commit).
const DefaultDataPath = "/mutable/drive-data.db"

// defaultJSONMirrorPath is where exportToJSON writes the JSON staging file
// that post-archive-process.sh and SyncToArchive then ship to the archive
// server. Kept next to the DB so user tooling still finds it at the
// historical location.
const defaultJSONMirrorPath = "/mutable/drive-data.json"

// legacyJSONPath is the pre-SQLite data file on the read-only root
// filesystem. The JSON importer reads this on first boot if the primary
// /mutable/drive-data.json is missing.
const legacyJSONPath = "/root/drive-data.json"

// importSourceCandidates is the ordered list of paths the one-shot
// JSON->DB importer in Load() checks. The first one that exists wins.
// Tests override this via withImportSourceCandidates so they can drive
// the importer against a tempdir.
var importSourceCandidates = []string{
	defaultJSONMirrorPath, // /mutable/drive-data.json
	legacyJSONPath,        // /root/drive-data.json
}

// withImportSourceCandidates swaps importSourceCandidates for the duration
// of a test. Used by load_import_test.go to point the importer at
// tempdir paths.
func withImportSourceCandidates(t interface{ Cleanup(func()) }, paths []string) {
	original := importSourceCandidates
	importSourceCandidates = paths
	t.Cleanup(func() { importSourceCandidates = original })
}

// archiveDataPath is where the archive-side JSON copy lives when a CIFS/NFS
// archive is mounted at /mnt/archive. Rsync and rclone users bypass this
// path — their sync is handled by post-archive-process.sh.
const archiveDataPath = "/mnt/archive/drive-data.json"

// Store manages drive-map data backed by a SQLite database. The public API
// is intentionally identical to the prior JSON-backed implementation so
// server/api/drives.go and server/drives/processor.go don't need changes.
//
// Thread-safety: SQLite with WAL handles its own internal locking, but the
// Store still keeps a sync.RWMutex to preserve the "WithRoutes callback
// receives a stable slice" contract exercised by GroupSummaries and
// friends. Writes take the write lock only as long as it takes to hand
// bytes to the driver; the actual disk work is serialized by SQLite.
type Store struct {
	mu   sync.RWMutex
	path string // SQLite DB path
	db   *sql.DB

	// Cached row counts. Refreshed on mutation so the /api/drives/status
	// polling endpoint doesn't SELECT COUNT(*) every second. Atomic so
	// readers don't have to take the RWMutex just for a count.
	routeCount     atomic.Int64
	processedCount atomic.Int64

	// Migration (async aggregate backfill) state. migrationStatus is
	// updated by the background goroutine and read by the
	// /api/drives/migration-status handler without holding s.mu.
	// migrationDoneCh is closed by the goroutine on completion so
	// WaitForMigration can block without polling. backfillCancel is the
	// cancel func paired with the ctx Close() uses to interrupt an
	// in-flight backfill on SIGTERM.
	migrationStatus atomic.Value // *migrationState
	migrationDoneCh chan struct{}
	backfillCancel  context.CancelFunc
}

// NewStore creates a Store that will open/create a SQLite database at the
// given path when Load is called. Pass "" to use DefaultDataPath.
func NewStore(path string) *Store {
	if path == "" {
		path = DefaultDataPath
	}
	return &Store{path: path}
}

// Load opens the SQLite database, applies any pending schema migrations,
// and primes the row-count caches. Safe to call multiple times: a second
// call rebuilds the caches without clobbering the database.
//
// A follow-up commit will extend this to run a one-shot JSON import from
// legacyJSONPath or /mutable/drive-data.json if meta.imported_from_json_at
// is not yet set. Until then, existing installs see an empty Drives page
// after upgrading.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Open (idempotent) or re-open.
	if s.db == nil {
		if dir := filepath.Dir(s.path); dir != "" && dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("Load: mkdir %q: %w", dir, err)
			}
		}
		// auto_vacuum=INCREMENTAL lets SQLite reclaim freelist pages via
		// PRAGMA incremental_vacuum instead of letting the .db file grow
		// forever after DELETEs (notably ReplaceData's wipe-and-rewrite).
		// SQLite only honors this on a brand-new DB; on existing DBs
		// opened by a pre-fix binary the pragma is a silent no-op and the
		// file won't shrink without a manual VACUUM. Acceptable tradeoff
		// for the Pi target: fresh reflashes get the new behavior for free.
		dsn := "file:" + s.path +
			"?_pragma=journal_mode(WAL)" +
			"&_pragma=synchronous(NORMAL)" +
			"&_pragma=foreign_keys(on)" +
			"&_pragma=busy_timeout(5000)" +
			"&_pragma=temp_store(FILE)" +
			"&_pragma=auto_vacuum(incremental)"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return fmt.Errorf("Load: sql.Open %q: %w", s.path, err)
		}
		// One writer at a time keeps WAL contention bounded and simplifies
		// reasoning about transactions. The Pi workload is read-mostly and
		// doesn't benefit from a larger pool.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		s.db = db
	}

	ctx := context.Background()

	// Assert that the DSN pragmas actually took effect. A future
	// modernc.org/sqlite release that silently changes pragma parsing
	// must fail loudly here rather than silently shipping with e.g.
	// journal_mode=DELETE (which would corrupt on power loss).
	var journalMode string
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		return fmt.Errorf("Load: read journal_mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("Load: journal_mode is %q, expected wal -- DSN pragma may have been ignored by the driver", journalMode)
	}

	if err := migrate(ctx, s.db); err != nil {
		return fmt.Errorf("Load: migrate: %w", err)
	}

	// One-shot JSON -> DB import. Runs only if meta.imported_from_json_at
	// is unset (fresh install, or upgrading from the JSON-backed binary).
	if err := s.runOneShotImportLocked(ctx); err != nil {
		return fmt.Errorf("Load: import: %w", err)
	}

	// NOTE: the aggregate backfill that used to run synchronously here
	// is now triggered by the caller (NewDriveHandlers) via
	// StartBackgroundBackfill so HTTP can start serving immediately on
	// a first-boot-after-upgrade even when the backfill will take
	// minutes. The NULL-aggregate sentinel keeps everything crash-safe:
	// summary endpoints read 0 for NULL columns (graceful degradation)
	// until the backfill populates the real values.

	if err := s.refreshCountsLocked(ctx); err != nil {
		return fmt.Errorf("Load: refresh counts: %w", err)
	}
	return nil
}

// runOneShotImportLocked performs the JSON->DB upgrade dance:
//
//  1. If meta.imported_from_json_at is already set, return — import has
//     either run before or this is a fresh install on a binary that
//     doesn't need to import.
//  2. Walk importSourceCandidates and take the first that exists. If
//     none exist (true fresh install), set the marker to now and return.
//  3. Stream the chosen JSON into the DB via importJSON. The whole
//     import is wrapped in one transaction; any failure rolls it back
//     and returns the error so Load() fails and the user sees the
//     problem on the next boot.
//  4. On success: rename the source JSON to .bak-<unix-epoch>-<rand4>.
//     Never deleted -- this is the user's safety net if the migration
//     ever needs to be undone. The rand4 suffix protects against Pis
//     without an RTC where time.Now() may be 1970 on first boot and
//     two consecutive imports could collide on filename.
//  5. Set meta.imported_from_json_at to now so future boots skip this.
//
// Caller must hold s.mu (write lock). Safe to call when the DB already
// has data (in that case it just returns after the marker check).
func (s *Store) runOneShotImportLocked(ctx context.Context) error {
	if v, err := metaGet(ctx, s.db, "imported_from_json_at"); err == nil && v != "" {
		return nil // already imported
	}

	var sourcePath string
	var alsoPresent []string
	for _, p := range importSourceCandidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			if sourcePath == "" {
				sourcePath = p
			} else {
				alsoPresent = append(alsoPresent, p)
			}
		}
	}
	if len(alsoPresent) > 0 {
		log.Printf("[drives] WARNING: multiple drive-data.json candidates exist; importing %s and ignoring %v. Delete the unused file(s) to silence this warning.",
			sourcePath, alsoPresent)
	}

	if sourcePath == "" {
		// True fresh install: no prior JSON to import. Mark it so we
		// don't keep checking on every boot.
		log.Printf("[drives] No legacy drive-data.json found; treating as fresh install")
		return metaSet(ctx, s.db, "imported_from_json_at", time.Now().UTC().Format(time.RFC3339))
	}

	log.Printf("[drives] Importing legacy JSON from %s", sourcePath)
	f, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open %q: %w", sourcePath, err)
	}
	defer f.Close()

	stats, err := importJSON(ctx, s.db, f, func(routesImported int) {
		log.Printf("[drives] Import progress: %d routes", routesImported)
	})
	if err != nil {
		return fmt.Errorf("importJSON %q: %w", sourcePath, err)
	}
	log.Printf("[drives] Import complete: %d routes, %d processed files, %d tags",
		stats.Routes, stats.ProcessedFiles, stats.DriveTags)

	// Set the marker BEFORE renaming the source. If the process dies
	// between those two steps, the worst outcome on the next boot is a
	// source JSON that gets left alone (because the marker is set), not
	// a double-import of already-imported data. Any orphan source JSON
	// is logged loudly below so the user can clean it up.
	if err := metaSet(ctx, s.db, "imported_from_json_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("set import marker: %w", err)
	}

	// Rename source out of the way so the user has a safety-net backup
	// and so they don't get confused later by a stale-looking JSON
	// sitting next to the DB. Use epoch+random suffix to survive
	// Pi-without-RTC clock skew. On cross-filesystem mounts (EXDEV),
	// fall back to copy+unlink so we don't leave the orphan in place.
	bakPath := fmt.Sprintf("%s.bak-%d-%04x",
		sourcePath, time.Now().Unix(), randSuffix4())
	if err := renameOrCopy(sourcePath, bakPath); err != nil {
		log.Printf("[drives] WARNING: import succeeded but failed to archive %s -> %s: %v",
			sourcePath, bakPath, err)
	} else {
		log.Printf("[drives] Renamed source JSON to %s (backup; safe to delete after verifying drives page)", bakPath)
	}
	return nil
}

// randSuffix4 returns a 16-bit random integer used to disambiguate
// .bak-<ts>-XXXX filenames on Pis without an RTC (where time.Now() may
// be 1970 on first boot and two consecutive runs could otherwise
// collide). math/rand is fine; this isn't security-sensitive.
var randSuffix4 = func() uint16 {
	// Re-seed off the high-resolution monotonic clock so multiple calls
	// in the same second don't repeat. crypto/rand would be overkill;
	// a quick xorshift on time.Now().UnixNano() is enough.
	t := time.Now().UnixNano()
	t ^= t >> 13
	t ^= t << 7
	t ^= t >> 17
	return uint16(t & 0xffff)
}

// renameOrCopy renames src to dst, falling back to copy+unlink when
// rename fails with EXDEV (cross-device link). Pis in the wild mount
// /mutable, /root, and /mnt/archive on different filesystems often
// enough that os.Rename is not portable across those boundaries.
// On copy, the destination is fsynced before the source is removed
// so a crash mid-fallback leaves src intact rather than losing data.
func renameOrCopy(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}

// Save is a no-op in the SQL backend — writes are already durable via WAL
// after each AddRoute/MarkProcessed/SetDriveTags call. Kept on the public
// API because the processor still calls it periodically; we use the call
// as a hint to run a passive WAL checkpoint so the -wal file doesn't grow
// unbounded during long processing runs.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	// Passive checkpoint: don't block other readers/writers; whatever can
	// be checkpointed, is. Errors are not fatal — the data is already
	// durable in the WAL.
	_, _ = s.db.ExecContext(context.Background(), `PRAGMA wal_checkpoint(PASSIVE)`)
	return nil
}

// ProcessedSet returns all processed file paths, normalized to forward
// slashes. Called once per ProcessDirectory run.
func (s *Store) ProcessedSet() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := map[string]bool{}
	rows, err := s.db.QueryContext(context.Background(), `SELECT file FROM processed_files`)
	if err != nil {
		log.Printf("[drives] ProcessedSet: %v", err)
		return set
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			log.Printf("[drives] ProcessedSet scan: %v", err)
			continue
		}
		set[normalizePath(f)] = true
	}
	return set
}

// AddRoute adds a processed file and its route data. If points is empty the
// route row is skipped (the clip is still marked processed). If a route
// for file already exists it is upserted in place.
func (s *Store) AddRoute(
	relativePath, dateDir string,
	points []GPSPoint, gears []uint8, apStates []uint8,
	speeds []float32, accelPositions []float32,
	rawParkCount, rawFrameCount int,
	gearRuns []GearRun,
) {
	norm := normalizePath(relativePath)

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()

	// Precompute BLOBs + aggregates once. They're pure functions of the
	// arguments, so the retry loop below can safely replay the tx using
	// the same values without re-encoding on every attempt.
	var (
		pb, gb, ab, sb, acb, rb []byte
		firstLat, firstLon      sql.NullFloat64
		agg                     RouteAggregates
		startLat, startLon      sql.NullFloat64
		endLat, endLon          sql.NullFloat64
	)
	if len(points) > 0 {
		pb = encodePoints(points)
		gb = encodeUint8s(gears)
		ab = encodeUint8s(apStates)
		sb = encodeFloat32s(speeds)
		acb = encodeFloat32s(accelPositions)
		rb = encodeGearRuns(gearRuns)

		firstLat.Float64, firstLat.Valid = points[0][0], true
		firstLon.Float64, firstLon.Valid = points[0][1], true

		// Compute the per-route aggregate columns once here so the
		// Drives-page summary endpoints can operate on BLOB-free rows.
		// Semantics live in aggregate.go; this call is the only place
		// AddRoute walks the parallel slices for stats purposes.
		agg = ComputeRouteAggregates(Route{
			File:            relativePath,
			Date:            dateDir,
			Points:          points,
			GearStates:      gears,
			AutopilotStates: apStates,
			Speeds:          speeds,
			AccelPositions:  accelPositions,
			RawParkCount:    rawParkCount,
			RawFrameCount:   rawFrameCount,
			GearRuns:        gearRuns,
		})
		startLat = nullFloat(agg.StartLat)
		startLon = nullFloat(agg.StartLng)
		endLat = nullFloat(agg.EndLat)
		endLon = nullFloat(agg.EndLng)
	}

	err := withBusyRetry(ctx, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		// Mark processed (INSERT OR IGNORE so repeated calls are cheap).
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO processed_files(file, added_at) VALUES(?, ?)`,
			norm, time.Now().Unix()); err != nil {
			return fmt.Errorf("processed_files insert: %w", err)
		}

		if len(points) > 0 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO routes(
				file, date_dir, point_count, raw_park_count, raw_frame_count,
				start_ts, end_ts, distance_m, first_lat, first_lon,
				points_blob, gear_states_blob, ap_states_blob,
				speeds_blob, accel_blob, gear_runs_blob, updated_at,
				max_speed_mps, avg_speed_mps, speed_sample_count, valid_point_count,
				fsd_engaged_ms, autosteer_engaged_ms, tacc_engaged_ms,
				fsd_distance_m, autosteer_distance_m, tacc_distance_m, assisted_distance_m,
				fsd_disengagements, fsd_accel_pushes,
				start_lat, start_lon, end_lat, end_lon)
			VALUES(
				?, ?, ?, ?, ?,
				NULL, NULL, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?, ?,
				?, ?, ?, ?,
				?, ?,
				?, ?, ?, ?)
			ON CONFLICT(file) DO UPDATE SET
				date_dir            = excluded.date_dir,
				point_count         = excluded.point_count,
				raw_park_count      = excluded.raw_park_count,
				raw_frame_count     = excluded.raw_frame_count,
				distance_m          = excluded.distance_m,
				first_lat           = excluded.first_lat,
				first_lon           = excluded.first_lon,
				points_blob         = excluded.points_blob,
				gear_states_blob    = excluded.gear_states_blob,
				ap_states_blob      = excluded.ap_states_blob,
				speeds_blob         = excluded.speeds_blob,
				accel_blob          = excluded.accel_blob,
				gear_runs_blob      = excluded.gear_runs_blob,
				updated_at          = excluded.updated_at,
				max_speed_mps       = excluded.max_speed_mps,
				avg_speed_mps       = excluded.avg_speed_mps,
				speed_sample_count  = excluded.speed_sample_count,
				valid_point_count   = excluded.valid_point_count,
				fsd_engaged_ms      = excluded.fsd_engaged_ms,
				autosteer_engaged_ms= excluded.autosteer_engaged_ms,
				tacc_engaged_ms     = excluded.tacc_engaged_ms,
				fsd_distance_m      = excluded.fsd_distance_m,
				autosteer_distance_m= excluded.autosteer_distance_m,
				tacc_distance_m     = excluded.tacc_distance_m,
				assisted_distance_m = excluded.assisted_distance_m,
				fsd_disengagements  = excluded.fsd_disengagements,
				fsd_accel_pushes    = excluded.fsd_accel_pushes,
				start_lat           = excluded.start_lat,
				start_lon           = excluded.start_lon,
				end_lat             = excluded.end_lat,
				end_lon             = excluded.end_lon`,
			norm, dateDir, len(points), rawParkCount, rawFrameCount,
			agg.DistanceM, firstLat, firstLon,
			pb, gb, ab, sb, acb, rb, time.Now().Unix(),
			agg.MaxSpeedMps, agg.AvgSpeedMps, agg.SpeedSampleCount, agg.ValidPointCount,
			agg.FSDEngagedMs, agg.AutosteerEngagedMs, agg.TACCEngagedMs,
			agg.FSDDistanceM, agg.AutosteerDistanceM, agg.TACCDistanceM, agg.AssistedDistanceM,
			agg.FSDDisengagements, agg.FSDAccelPushes,
				startLat, startLon, endLat, endLon); err != nil {
				return fmt.Errorf("route upsert: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		committed = true
		return nil
	})
	if err != nil {
		log.Printf("[drives] AddRoute: %v", err)
		return
	}

	_ = s.refreshCountsLocked(ctx)
}

// MarkProcessed marks a file as processed without adding route data.
// Idempotent: calling twice with the same file is a no-op.
func (s *Store) MarkProcessed(relativePath string) {
	norm := normalizePath(relativePath)
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	err := withBusyRetry(ctx, func() error {
		_, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO processed_files(file, added_at) VALUES(?, ?)`,
			norm, time.Now().Unix())
		return err
	})
	if err != nil {
		log.Printf("[drives] MarkProcessed: %v", err)
		return
	}
	_ = s.refreshCountsLocked(ctx)
}

// RouteCount returns the number of route rows. O(1) from cache.
func (s *Store) RouteCount() int {
	return int(s.routeCount.Load())
}

// ProcessedCount returns the number of processed_files rows. O(1) from cache.
func (s *Store) ProcessedCount() int {
	return int(s.processedCount.Load())
}

// GetRoutes returns a fresh []Route decoded from the DB. Used only by
// /api/drives/data/download and tests; the hot-path readers go through
// WithRoutes.
func (s *Store) GetRoutes() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routes, err := s.selectAllRoutesLocked(context.Background())
	if err != nil {
		log.Printf("[drives] GetRoutes: %v", err)
		return nil
	}
	return routes
}

// WithRoutes materializes all routes from the DB and invokes fn with the
// resulting slice while holding the read lock. The slice and its elements
// must not be retained after fn returns; the SQLite memory backing them
// will be reused.
//
// A follow-up commit will add ForEachRoute(func(Route) bool) for true
// streaming (important on the 512 MB Pi Zero 2 W), and convert the
// biggest callers (GroupSummaries, GroupRoutesOverview, stats). Today we
// preserve the existing signature so api/drives.go needs zero changes.
func (s *Store) WithRoutes(fn func(routes []Route)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routes, err := s.selectAllRoutesLocked(context.Background())
	if err != nil {
		log.Printf("[drives] WithRoutes: %v", err)
		fn(nil)
		return
	}
	fn(routes)
}

// WithRouteSummaries is the BLOB-free analogue of WithRoutes. It
// materializes the per-route metadata + pre-computed aggregate columns
// (populated by AddRoute and the one-shot backfill) into a slice of
// RouteSummary and invokes fn with it. Every BLOB column is excluded
// from the SELECT, so a 5500-route DB reads ~5 MB of heap instead of
// the ~300 MB that WithRoutes costs.
//
// The summary path is intended for the Drives-page list endpoints
// (GroupSummaries, ComputeAggregateStatsFromRoutes, DriveStartTime).
// Endpoints that need per-point data (BuildSingleDrive,
// GroupRoutesOverview) continue to use WithRoutes.
//
// Callers must not retain the slice or any RouteSummary past fn's
// return — same contract as WithRoutes.
func (s *Store) WithRouteSummaries(fn func(summaries []RouteSummary)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	summaries, err := s.selectAllRouteSummariesLocked(context.Background())
	if err != nil {
		log.Printf("[drives] WithRouteSummaries: %v", err)
		fn(nil)
		return
	}
	fn(summaries)
}

// Path returns the database path passed to NewStore (or DefaultDataPath).
func (s *Store) Path() string {
	return s.path
}

// ReplaceData is a thin compatibility shim around ReplaceDataFromReader.
// Production callers (the upload HTTP handler) should use
// ReplaceDataFromReader directly so the upload body never has to be
// json.Unmarshal'd into memory. This signature exists for the in-process
// test suite, where materializing a tiny StoreData is fine and rewriting
// every test to push through an io.Pipe would just add noise.
//
// Uses io.Pipe so the JSON is still streamed into the importer -- no
// intermediate full-payload buffer.
func (s *Store) ReplaceData(data StoreData) {
	ctx := context.Background()
	pr, pw := io.Pipe()
	encErr := make(chan error, 1)
	go func() {
		defer pw.Close()
		enc := json.NewEncoder(pw)
		encErr <- enc.Encode(data)
	}()
	if _, err := s.ReplaceDataFromReader(ctx, pr, nil); err != nil {
		_ = pr.CloseWithError(err)
		log.Printf("[drives] ReplaceData: %v", err)
		return
	}
	if err := <-encErr; err != nil {
		log.Printf("[drives] ReplaceData encode: %v", err)
	}
}

// GetData returns a copy of the entire store as a StoreData value. Used by
// /api/drives/data/download. A follow-up commit will add a streaming
// exporter for the Pi Zero 2 W; today GetData allocates the whole payload
// in memory.
func (s *Store) GetData() StoreData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()
	data := StoreData{}

	if rs, err := s.selectAllRoutesLocked(ctx); err == nil {
		data.Routes = rs
	} else {
		log.Printf("[drives] GetData routes: %v", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT file FROM processed_files ORDER BY file`)
	if err != nil {
		log.Printf("[drives] GetData processed_files: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var f string
			if err := rows.Scan(&f); err != nil {
				log.Printf("[drives] GetData scan: %v", err)
				continue
			}
			data.ProcessedFiles = append(data.ProcessedFiles, f)
		}
	}

	tagRows, err := s.db.QueryContext(ctx, `SELECT drive_key, tag FROM drive_tags`)
	if err != nil {
		log.Printf("[drives] GetData drive_tags: %v", err)
	} else {
		defer tagRows.Close()
		data.DriveTags = map[string][]string{}
		for tagRows.Next() {
			var key, tag string
			if err := tagRows.Scan(&key, &tag); err != nil {
				log.Printf("[drives] GetData tag scan: %v", err)
				continue
			}
			data.DriveTags[key] = append(data.DriveTags[key], tag)
		}
		if len(data.DriveTags) == 0 {
			data.DriveTags = nil
		}
	}

	return data
}

// SetDriveTags replaces the tags for a drive key. An empty/nil tags slice
// removes the entry entirely.
func (s *Store) SetDriveTags(driveKey string, tags []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()

	err := withBusyRetry(ctx, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		if _, err := tx.ExecContext(ctx, `DELETE FROM drive_tags WHERE drive_key = ?`, driveKey); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		for _, t := range tags {
			if t == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO drive_tags(drive_key, tag) VALUES(?, ?)`,
				driveKey, t); err != nil {
				return fmt.Errorf("insert: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		committed = true
		return nil
	})
	if err != nil {
		log.Printf("[drives] SetDriveTags: %v", err)
	}
}

// GetDriveTags returns the tags attached to a drive key, or nil if none.
func (s *Store) GetDriveTags(driveKey string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT tag FROM drive_tags WHERE drive_key = ? ORDER BY tag`, driveKey)
	if err != nil {
		log.Printf("[drives] GetDriveTags: %v", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out
}

// GetAllDriveTags returns the full drive_key -> tags map.
func (s *Store) GetAllDriveTags() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT drive_key, tag FROM drive_tags ORDER BY drive_key, tag`)
	if err != nil {
		log.Printf("[drives] GetAllDriveTags: %v", err)
		return nil
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var key, tag string
		if err := rows.Scan(&key, &tag); err != nil {
			continue
		}
		out[key] = append(out[key], tag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetAllTagNames returns a sorted, deduplicated list of every tag name in use.
func (s *Store) GetAllTagNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT DISTINCT tag FROM drive_tags ORDER BY tag`)
	if err != nil {
		log.Printf("[drives] GetAllTagNames: %v", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.Strings(out) // belt & suspenders
	return out
}

// ClearProcessedForReprocess empties processed_files so every clip on disk
// is eligible for re-extraction. Routes and drive_tags are preserved.
func (s *Store) ClearProcessedForReprocess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(context.Background(), `DELETE FROM processed_files`); err != nil {
		log.Printf("[drives] ClearProcessedForReprocess: %v", err)
		return
	}
	_ = s.refreshCountsLocked(context.Background())
}

// SyncToArchive regenerates the JSON mirror at defaultJSONMirrorPath
// from the SQLite DB, then copies it to /mnt/archive/drive-data.json
// atomically. The size-guard from PR 1 still applies via syncToPath:
// a regenerated JSON that is dramatically smaller than the last
// successful sync is refused.
//
// Best-effort: silently returns nil if /mnt/archive isn't a mounted
// filesystem (rsync and rclone users handle their own archive flow in
// run/post-archive-process.sh).
func (s *Store) SyncToArchive() error {
	if _, err := os.Stat("/mnt/archive"); err != nil {
		return nil
	}
	if mounts, err := os.ReadFile("/proc/mounts"); err == nil {
		if !strings.Contains(string(mounts), "/mnt/archive") {
			return nil
		}
	}

	mirror := defaultJSONMirrorPath
	if err := s.ExportJSONToFile(mirror); err != nil {
		return fmt.Errorf("SyncToArchive: regenerate JSON mirror: %w", err)
	}
	return s.syncToPath(mirror, archiveDataPath, DefaultSyncCachePath)
}

// maxRestoreSize caps what RestoreFromArchive will copy from /mnt/archive
// into /mutable. Bigger than this (>2GB) is almost certainly a corrupt or
// runaway file; the 512MB-RAM Pi would burn through free space on /mutable
// and the subsequent importer would spend hours trying to parse it. Fail
// loudly instead.
const maxRestoreSize = 2 << 30 // 2 GiB

// RestoreFromArchive copies a JSON drive-data file from the archive mount
// into one of the importSourceCandidates paths so the next Load() picks
// it up via the one-shot importer. Useful when /mutable has been wiped
// (Pi reflash) but the archive still has the user's data.
//
// Best-effort: silently returns nil if /mnt/archive isn't mounted or no
// JSON is present. Returns a non-nil error only if the copy itself fails
// after we've decided to proceed (disk full, permission, etc.).
func (s *Store) RestoreFromArchive() error {
	return restoreFromArchive(archiveDataPath, defaultJSONMirrorPath)
}

// restoreFromArchive is the testable body of RestoreFromArchive. The copy
// is streamed through an io.Copy + tmp/fsync/rename so peak memory is one
// 32KB buffer no matter how large the archive JSON is — critical on a
// 512MB Pi where the pre-fix ReadFile+WriteFile would OOM on anything
// larger than the free heap.
func restoreFromArchive(srcPath, dstPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil || info.IsDir() {
		return nil
	}
	// Don't restore if we already have data — the one-shot importer
	// will respect the import marker and skip anyway, but we'd rather
	// not rewrite /mutable and burn SD-card writes needlessly.
	if _, err := os.Stat(dstPath); err == nil {
		return nil
	}
	if info.Size() > maxRestoreSize {
		return fmt.Errorf("RestoreFromArchive: %q is %d bytes, exceeds %d-byte cap",
			srcPath, info.Size(), maxRestoreSize)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("RestoreFromArchive: mkdir %q: %w", filepath.Dir(dstPath), err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return nil // mount disappeared between stat and open; best-effort
	}
	defer src.Close()

	tmp := dstPath + ".restore.tmp"
	dst, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("RestoreFromArchive: create %q: %w", tmp, err)
	}
	n, copyErr := io.Copy(dst, src)
	syncErr := dst.Sync()
	closeErr := dst.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("RestoreFromArchive: copy/sync/close: copy=%v sync=%v close=%v",
			copyErr, syncErr, closeErr)
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("RestoreFromArchive: rename %q -> %q: %w", tmp, dstPath, err)
	}
	log.Printf("[drives] Restored drive-data.json from archive (%d bytes); next Load() will import it", n)
	return nil
}

// ExportJSONForSync regenerates the canonical /mutable/drive-data.json
// mirror that post-archive-process.sh ships to the rsync/rclone archive
// server. Idempotent; safe to call concurrently with reads.
//
// Wraps ExportJSONToFile with the well-known production path so the API
// handler doesn't have to import a package-private constant from drives.
func (s *Store) ExportJSONForSync() error {
	return s.ExportJSONToFile(defaultJSONMirrorPath)
}

// ExportJSONToFile streams the current DB contents out to path as a
// drive-data.json (StoreData shape), using a tmp + rename for atomicity.
// Exported here so server/api/drives.go's POST /api/drives/data/export-for-sync
// handler can call it; also used internally by SyncToArchive.
func (s *Store) ExportJSONToFile(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	s.mu.RLock()
	err = exportJSON(context.Background(), s.db, f)
	s.mu.RUnlock()
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// -----------------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------------

// selectAllRoutesLocked reads every route from the DB, decoding all BLOB
// columns into the Go representation. Caller must hold s.mu (read).
func (s *Store) selectAllRoutesLocked(ctx context.Context) ([]Route, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT file, date_dir, raw_park_count, raw_frame_count,
		       points_blob, gear_states_blob, ap_states_blob,
		       speeds_blob, accel_blob, gear_runs_blob
		FROM routes
		ORDER BY file`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Route
	for rows.Next() {
		var r Route
		var pb, gb, ab, sb, acb, rb []byte
		if err := rows.Scan(
			&r.File, &r.Date, &r.RawParkCount, &r.RawFrameCount,
			&pb, &gb, &ab, &sb, &acb, &rb,
		); err != nil {
			return nil, err
		}
		points, err := decodePoints(pb)
		if err != nil {
			return nil, fmt.Errorf("decode points for %q: %w", r.File, err)
		}
		r.Points = points
		r.GearStates = decodeUint8s(gb)
		r.AutopilotStates = decodeUint8s(ab)
		speeds, err := decodeFloat32s(sb)
		if err != nil {
			return nil, fmt.Errorf("decode speeds for %q: %w", r.File, err)
		}
		r.Speeds = speeds
		accel, err := decodeFloat32s(acb)
		if err != nil {
			return nil, fmt.Errorf("decode accel for %q: %w", r.File, err)
		}
		r.AccelPositions = accel
		runs, err := decodeGearRuns(rb)
		if err != nil {
			return nil, fmt.Errorf("decode gear_runs for %q: %w", r.File, err)
		}
		r.GearRuns = runs
		out = append(out, r)
	}
	return out, rows.Err()
}

// selectAllRouteSummariesLocked reads every route's metadata plus the
// v2 pre-computed aggregate columns, skipping all BLOB-backed columns
// entirely. Heap cost is ~1 KB/row (metadata + gear_runs_blob). Caller
// must hold s.mu (read).
//
// gear_runs_blob is the one "small BLOB" we still decode because
// groupClips / splitClipAtParkGaps need frame counts to figure out
// intra-clip drive boundaries. On a typical clip it's a handful of
// bytes, not kilobytes.
func (s *Store) selectAllRouteSummariesLocked(ctx context.Context) ([]RouteSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT file, date_dir, raw_park_count, raw_frame_count, gear_runs_blob,
		       distance_m, max_speed_mps, avg_speed_mps, speed_sample_count,
		       valid_point_count, fsd_engaged_ms, autosteer_engaged_ms,
		       tacc_engaged_ms, fsd_distance_m, autosteer_distance_m,
		       tacc_distance_m, assisted_distance_m,
		       fsd_disengagements, fsd_accel_pushes,
		       start_lat, start_lon, end_lat, end_lon
		FROM routes
		ORDER BY file`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RouteSummary
	for rows.Next() {
		var sum RouteSummary
		var rb []byte
		var (
			distanceM, maxSpeedMps, avgSpeedMps                 sql.NullFloat64
			fsdDistM, autosteerDistM, taccDistM, assistedDistM  sql.NullFloat64
			speedSampleCount, validPointCount                   sql.NullInt64
			fsdEngagedMs, autosteerEngagedMs, taccEngagedMs     sql.NullInt64
			fsdDisengagements, fsdAccelPushes                   sql.NullInt64
			startLat, startLon, endLat, endLon                  sql.NullFloat64
		)
		if err := rows.Scan(
			&sum.File, &sum.Date, &sum.RawParkCount, &sum.RawFrameCount, &rb,
			&distanceM, &maxSpeedMps, &avgSpeedMps, &speedSampleCount,
			&validPointCount, &fsdEngagedMs, &autosteerEngagedMs,
			&taccEngagedMs, &fsdDistM, &autosteerDistM,
			&taccDistM, &assistedDistM,
			&fsdDisengagements, &fsdAccelPushes,
			&startLat, &startLon, &endLat, &endLon,
		); err != nil {
			return nil, err
		}
		runs, err := decodeGearRuns(rb)
		if err != nil {
			return nil, fmt.Errorf("decode gear_runs for %q: %w", sum.File, err)
		}
		sum.GearRuns = runs
		// Collapse NULL to zero so callers don't have to handle
		// three-valued logic at every summary access. The backfill
		// guarantees every row is populated before the refactored
		// endpoints hit WithRouteSummaries.
		sum.DistanceM = distanceM.Float64
		sum.MaxSpeedMps = maxSpeedMps.Float64
		sum.AvgSpeedMps = avgSpeedMps.Float64
		sum.SpeedSampleCount = int(speedSampleCount.Int64)
		sum.ValidPointCount = int(validPointCount.Int64)
		sum.FSDEngagedMs = fsdEngagedMs.Int64
		sum.AutosteerEngagedMs = autosteerEngagedMs.Int64
		sum.TACCEngagedMs = taccEngagedMs.Int64
		sum.FSDDistanceM = fsdDistM.Float64
		sum.AutosteerDistanceM = autosteerDistM.Float64
		sum.TACCDistanceM = taccDistM.Float64
		sum.AssistedDistanceM = assistedDistM.Float64
		sum.FSDDisengagements = int(fsdDisengagements.Int64)
		sum.FSDAccelPushes = int(fsdAccelPushes.Int64)
		if startLat.Valid {
			v := startLat.Float64
			sum.StartLat = &v
		}
		if startLon.Valid {
			v := startLon.Float64
			sum.StartLng = &v
		}
		if endLat.Valid {
			v := endLat.Float64
			sum.EndLat = &v
		}
		if endLon.Valid {
			v := endLon.Float64
			sum.EndLng = &v
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// nullFloat converts an optional *float64 (as produced by aggregate.go's
// Start/End fields) to a sql.NullFloat64 suitable for INSERT binding.
func nullFloat(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

// refreshCountsLocked updates the atomic row-count caches. Caller must hold s.mu.
func (s *Store) refreshCountsLocked(ctx context.Context) error {
	var rc, pc int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes`).Scan(&rc); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM processed_files`).Scan(&pc); err != nil {
		return err
	}
	s.routeCount.Store(rc)
	s.processedCount.Store(pc)
	return nil
}

// normalizePath converts backslashes to forward slashes so Windows-shaped
// paths collide with their POSIX equivalents in processed_files and routes.
func normalizePath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// syncToPath copies srcPath to destPath atomically, enforcing the size-
// guard via cachePath. On success the cache is updated with the new size;
// on *ErrSyncGuard the destination and cache are left untouched.
//
// This is the primitive underneath SyncToArchive (which passes the
// regenerated JSON mirror as srcPath) and is exercised directly by the
// size-guard integration tests. It does not require s.db to be open, so
// it works even before Load() — the size-guard PR shipped this as
// usable before the SQLite migration was wired in.
func (s *Store) syncToPath(srcPath, destPath, cachePath string) error {
	// Stat source first so we know the new size before the guard decision.
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	newSize := srcInfo.Size()

	lastSize, _ := readSyncCache(cachePath) // fails open on error
	if err := checkSyncSizeGuard(newSize, lastSize); err != nil {
		log.Printf("[drives] %s", err.Error())
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	if dir := filepath.Dir(destPath); dir != "" && dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	tmp := destPath + ".tmp"
	dst, err := os.Create(tmp)
	if err != nil {
		return err
	}

	n, err := io.Copy(dst, src)
	if err != nil {
		dst.Close()
		os.Remove(tmp)
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		os.Remove(tmp)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Reject copy-truncation: if the source shrank between stat and
	// copy (mid-regeneration, filesystem hiccup), the destination is
	// short and the size-guard cache must NOT be primed with the
	// truncated length -- that would poison all future syncs, because
	// the guard would then compare real full-size exports against the
	// too-small cached value and refuse them.
	if n != newSize {
		os.Remove(tmp)
		return fmt.Errorf("syncToPath: short copy (%d of %d bytes); refusing to poison size-guard cache", n, newSize)
	}
	// The archive is commonly a CIFS/NFS mount on a different filesystem
	// than the temp file, which makes os.Rename return EXDEV. Fall back
	// to copy+unlink in that case.
	if err := renameOrCopy(tmp, destPath); err != nil {
		os.Remove(tmp)
		return err
	}

	if err := writeSyncCache(cachePath, newSize); err != nil {
		log.Printf("[drives] Warning: failed to update sync-size cache at %s: %v", cachePath, err)
	}
	log.Printf("[drives] Synced drive data to archive (%d bytes)", newSize)
	return nil
}
