package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/drives"
)

// broadcaster is a minimal interface for WebSocket broadcasts.
type broadcaster interface {
	Broadcast(msgType string, data interface{})
}

// DriveHandlers holds the drive map state.
type DriveHandlers struct {
	store     *drives.Store
	processor *drives.Processor
	hub       broadcaster

	importMu  sync.Mutex
	importing bool
}

// NewDriveHandlers creates handlers with a store at the given data path.
func NewDriveHandlers(dataPath string, hub broadcaster) *DriveHandlers {
	store := drives.NewStore(dataPath)
	if err := store.RestoreFromArchive(); err != nil {
		log.Printf("[drives] Warning: failed to restore from archive: %v", err)
	}
	if err := store.Load(); err != nil {
		log.Printf("[drives] Warning: failed to load drive data: %v", err)
	}
	return &DriveHandlers{
		store:     store,
		processor: drives.NewProcessor(store),
		hub:       hub,
	}
}

// RegisterDriveRoutes registers all drive map API routes.
func RegisterDriveRoutes(mux *http.ServeMux, dh *DriveHandlers) {
	mux.HandleFunc("GET /api/drives", dh.listDrives)
	mux.HandleFunc("GET /api/drives/routes", dh.allRoutes)
	mux.HandleFunc("GET /api/drives/tags", dh.listTags)
	mux.HandleFunc("GET /api/drives/process", dh.processingStatus)
	mux.HandleFunc("POST /api/drives/process", dh.processFiles)
	mux.HandleFunc("POST /api/drives/reprocess", dh.reprocessAll)
	mux.HandleFunc("GET /api/drives/status", dh.processingStatus)
	mux.HandleFunc("GET /api/drives/data/download", dh.downloadData)
	mux.HandleFunc("POST /api/drives/data/upload", dh.uploadData)
	mux.HandleFunc("POST /api/drives/data/export-for-sync", dh.exportForSync)
	mux.HandleFunc("GET /api/drives/stats", dh.driveStats)
	mux.HandleFunc("GET /api/drives/fsd-analytics", dh.fsdAnalytics)
	mux.HandleFunc("PUT /api/drives/{id}/tags", dh.setDriveTags)
	mux.HandleFunc("GET /api/drives/{id}", dh.singleDrive)
}

// Store returns the underlying drive store (for external integration like post-archive hooks).
func (dh *DriveHandlers) Store() *drives.Store {
	return dh.store
}

// Processor returns the underlying processor (for external integration).
func (dh *DriveHandlers) Processor() *drives.Processor {
	return dh.processor
}

// GET /api/drives — list all drives (summaries, no full point data)
// Query params: ?tag=Work (filter by tag)
func (dh *DriveHandlers) listDrives(w http.ResponseWriter, r *http.Request) {
	var summaries []drives.DriveSummary
	dh.store.WithRoutes(func(routes []drives.Route) {
		summaries = drives.GroupSummaries(routes)
	})
	runtime.GC()
	drives.ApplySummaryTags(summaries, dh.store.GetAllDriveTags())

	// Filter by tag if requested
	if tagFilter := r.URL.Query().Get("tag"); tagFilter != "" {
		var filtered []drives.DriveSummary
		for _, s := range summaries {
			for _, t := range s.Tags {
				if t == tagFilter {
					filtered = append(filtered, s)
					break
				}
			}
		}
		summaries = filtered
	}

	writeJSON(w, http.StatusOK, summaries)
}

// GET /api/drives/routes — all routes downsampled for overview map
func (dh *DriveHandlers) allRoutes(w http.ResponseWriter, r *http.Request) {
	var result []drives.RouteOverview
	dh.store.WithRoutes(func(routes []drives.Route) {
		result = drives.GroupRoutesOverview(routes, 500)
	})
	runtime.GC()
	writeJSON(w, http.StatusOK, result)
}

// GET /api/drives/{id} — full drive data including all points
func (dh *DriveHandlers) singleDrive(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid drive id")
		return
	}

	var d drives.Drive
	var ok bool
	dh.store.WithRoutes(func(routes []drives.Route) {
		d, ok = drives.BuildSingleDrive(routes, id)
	})
	runtime.GC()
	if !ok {
		writeError(w, http.StatusNotFound, "drive not found")
		return
	}

	// Apply tags to the single drive
	tagMap := dh.store.GetAllDriveTags()
	if tags, exists := tagMap[d.StartTime]; exists {
		d.Tags = tags
	}

	writeJSON(w, http.StatusOK, d)
}

// PUT /api/drives/{id}/tags — set tags for a drive
// Body: { "tags": ["Work", "Commute"], "start_time": "2025-03-10T08:30:00" }
// If start_time is provided, it is used directly as the drive key (fast path).
// Otherwise falls back to looking up the drive by index via DriveStartTime.
func (dh *DriveHandlers) setDriveTags(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid drive id")
		return
	}

	var body struct {
		Tags      []string `json:"tags"`
		StartTime string   `json:"start_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	driveKey := body.StartTime
	if driveKey == "" {
		// Fallback: look up drive start time by index (no full point merge)
		var ok bool
		dh.store.WithRoutes(func(routes []drives.Route) {
			driveKey, ok = drives.DriveStartTime(routes, id)
		})
		if !ok {
			writeError(w, http.StatusNotFound, "drive not found")
			return
		}
	}

	dh.store.SetDriveTags(driveKey, body.Tags)
	if err := dh.store.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"drive_id":   id,
		"start_time": driveKey,
		"tags":       body.Tags,
	})
}

// GET /api/drives/tags — list all tag names in use
func (dh *DriveHandlers) listTags(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, dh.store.GetAllTagNames())
}

// archiveLog appends a timestamped [drive-map] entry to the archiveloop log file,
// matching the format used by post-archive-process.sh so manual processing
// events appear alongside automatic archive events.
func archiveLog(format string, args ...interface{}) {
	const logPath = "/mutable/archiveloop.log"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s: [drive-map] %s\n", time.Now().Format("Mon _2 Jan 15:04:05 MST 2006"), msg)
}

// IsArchiving returns true if the archiveloop is currently archiving files.
// The archive_progress_monitor updates the status file every 5s, so if it
// hasn't been touched in over 120s, treat it as stale (archiveloop crashed
// or forgot to clear it).
func IsArchiving() bool {
	const statusFile = "/tmp/archive_status.json"
	info, err := os.Stat(statusFile)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) > 120*time.Second {
		os.Remove(statusFile)
		return false
	}
	archive := readArchiveStatus()
	if archive == nil {
		return false
	}
	phase, _ := archive["phase"].(string)
	return phase == "archiving"
}

// awakeShellPreamble sources the system config (so TESSIE_API_TOKEN, TESLA_BLE_VIN,
// SENTRY_CASE etc. are available) and defines a fallback log() function.
// awake_start/awake_stop rely on both being present but neither is set when the
// scripts are invoked directly from the Go server (as opposed to from archiveloop).
const awakeShellPreamble = `source /root/bin/envsetup.sh 2>/dev/null || true
declare -F log > /dev/null 2>&1 || {
  function log { echo "$(date): $*" >> "${LOG_FILE:-/mutable/archiveloop.log}" 2>/dev/null || true; }
  export -f log
}
# Single global PID file shared with archiveloop. awake_start kills any
# existing nudge loop before starting a new one, preventing BLE contention.
export KEEP_AWAKE_PID_FILE=/tmp/keep_awake_nudge_pid
`

// startKeepAwake runs awake_start in the background to keep the car from sleeping.
// reason is shown in nudge log lines (e.g. "Drive Processing", "Manual", "Auto Keep Awake").
// expiresAt, if non-zero, is passed so nudge logs can show time remaining.
func startKeepAwake(reason string, expiresAt time.Time) {
	go func() {
		preamble := awakeShellPreamble
		if reason != "" {
			preamble += fmt.Sprintf("export KEEP_AWAKE_REASON=%q\n", reason)
		}
		if !expiresAt.IsZero() {
			preamble += fmt.Sprintf("export KEEP_AWAKE_EXPIRES_AT=%d\n", expiresAt.Unix())
		}
		cmd := exec.Command("/bin/bash", "-c", preamble+"/root/bin/awake_start")
		if err := cmd.Run(); err != nil {
			log.Printf("[drives] Warning: awake_start failed: %v", err)
		}
	}()
}

// stopKeepAwake runs awake_stop to allow the car to sleep again.
func stopKeepAwake() {
	go func() {
		cmd := exec.Command("/bin/bash", "-c", awakeShellPreamble+"/root/bin/awake_stop")
		if err := cmd.Run(); err != nil {
			log.Printf("[drives] Warning: awake_stop failed: %v", err)
		}
	}()
}

// POST /api/drives/process — trigger GPS extraction on NEW clips only
// Body: { "clips_dir": "/path/to/RecentClips", "throttle_ms": 10 }
func (dh *DriveHandlers) processFiles(w http.ResponseWriter, r *http.Request) {
	if dh.processor.IsRunning() {
		writeError(w, http.StatusConflict, "processing already in progress")
		return
	}
	dh.importMu.Lock()
	importing := dh.importing
	dh.importMu.Unlock()
	if importing {
		writeError(w, http.StatusConflict, "drive data import in progress — please wait until it finishes")
		return
	}
	// post_archive=1 is set by post-archive-process.sh which runs after
	// archiving is complete.  Skip the stale-file check in that case.
	postArchive := r.URL.Query().Get("post_archive") == "1"
	if !postArchive && IsArchiving() {
		writeError(w, http.StatusConflict, "archive is currently running — please wait until it finishes")
		return
	}

	var body struct {
		ClipsDir   string `json:"clips_dir"`
		ThrottleMs int    `json:"throttle_ms"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.ClipsDir == "" {
		// Default: process RecentClips from local snapshot storage
		candidates := []string{
			"/mutable/TeslaCam/RecentClips",
		}
		for _, dir := range candidates {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				body.ClipsDir = dir
				break
			}
		}
		if body.ClipsDir == "" {
			writeError(w, http.StatusBadRequest, "no clips directory found; specify clips_dir in request body")
			return
		}
	}

	// Run in background. Skip keep-awake when post_archive=1 so the car can
	// sleep after archiving; archiveloop will stop its own keep-awake task.
	go func() {
		if !postArchive {
			startKeepAwake("Drive Processing", time.Time{})
			defer stopKeepAwake()
		}

		dh.hub.Broadcast("drive_process", map[string]interface{}{
			"status": "started", "clips_dir": body.ClipsDir,
		})

		result, err := dh.processor.ProcessDirectory(
			context.Background(),
			body.ClipsDir,
			body.ThrottleMs,
			func(current, total int) {
				dh.hub.Broadcast("drive_process", map[string]interface{}{
					"status": "progress", "current": current, "total": total,
				})
			},
		)

		if err != nil {
			archiveLog("Drive processing error: %v", err)
			dh.hub.Broadcast("drive_process", map[string]interface{}{
				"status": "error", "error": err.Error(),
			})
			return
		}
		archiveLog("Drive processing complete. Files: %d, GPS: %d, Routes: %d, Errors: %d (%s)",
			result.FilesNew, result.FilesWithGPS, result.RoutesFound, result.Errors, result.Duration)

		dh.hub.Broadcast("drive_process", map[string]interface{}{
			"status": "complete", "result": result,
		})
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":    "started",
		"clips_dir": body.ClipsDir,
	})
}

// POST /api/drives/reprocess — clear processed list and re-extract all clips from local snapshots.
// Only re-processes files that still exist on disk; does NOT delete existing drive data
// for clips whose snapshots have been removed.
func (dh *DriveHandlers) reprocessAll(w http.ResponseWriter, r *http.Request) {
	if dh.processor.IsRunning() {
		writeError(w, http.StatusConflict, "processing already in progress")
		return
	}
	dh.importMu.Lock()
	importing := dh.importing
	dh.importMu.Unlock()
	if importing {
		writeError(w, http.StatusConflict, "drive data import in progress — please wait until it finishes")
		return
	}
	if IsArchiving() {
		writeError(w, http.StatusConflict, "archive is currently running — please wait until it finishes")
		return
	}

	// Clear processed list so all existing clips are re-scanned
	dh.store.ClearProcessedForReprocess()
	if err := dh.store.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save: %v", err))
		return
	}

	clipsDir := "/mutable/TeslaCam/RecentClips"
	if info, err := os.Stat(clipsDir); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "no clips directory found on disk")
		return
	}

	go func() {
		startKeepAwake("Drive Processing", time.Time{})
		defer stopKeepAwake()

		archiveLog("Starting reprocess (all) on %s", clipsDir)
		dh.hub.Broadcast("drive_process", map[string]interface{}{
			"status": "started", "clips_dir": clipsDir, "mode": "reprocess",
		})

		result, err := dh.processor.ProcessDirectory(
			context.Background(), clipsDir, 15,
			func(current, total int) {
				dh.hub.Broadcast("drive_process", map[string]interface{}{
					"status": "progress", "current": current, "total": total,
				})
			},
		)

		if err != nil {
			archiveLog("Reprocess error: %v", err)
			dh.hub.Broadcast("drive_process", map[string]interface{}{
				"status": "error", "error": err.Error(),
			})
			return
		}
		archiveLog("Reprocess complete. Files: %d, GPS: %d, Routes: %d, Errors: %d (%s)",
			result.FilesNew, result.FilesWithGPS, result.RoutesFound, result.Errors, result.Duration)

		dh.hub.Broadcast("drive_process", map[string]interface{}{
			"status": "complete", "result": result,
		})
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status": "started",
		"mode":   "reprocess",
	})
}

// GET /api/drives/status — check if processing is running
func (dh *DriveHandlers) processingStatus(w http.ResponseWriter, r *http.Request) {
	dh.importMu.Lock()
	importing := dh.importing
	dh.importMu.Unlock()

	resp := map[string]interface{}{
		"running":         dh.processor.IsRunning(),
		"importing":       importing,
		"routes_count":    dh.store.RouteCount(),
		"processed_count": dh.store.ProcessedCount(),
		"archiving":       IsArchiving(),
	}

	// Include processing progress so polling clients can pick it up
	// even if they missed the WebSocket messages.
	if cur, tot := dh.processor.Progress(); tot > 0 {
		resp["process_current"] = cur
		resp["process_total"] = tot
	}

	if archive := readArchiveStatus(); archive != nil {
		for k, v := range archive {
			resp[k] = v
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// readArchiveStatus reads the archive status file written by archiveloop.
func readArchiveStatus() map[string]interface{} {
	data, err := os.ReadFile("/tmp/archive_status.json")
	if err != nil {
		return nil
	}
	var status map[string]interface{}
	if err := json.Unmarshal(data, &status); err != nil {
		return nil
	}
	return status
}

// GET /api/drives/data/download — download the drive-data.json file
func (dh *DriveHandlers) downloadData(w http.ResponseWriter, r *http.Request) {
	data := dh.store.GetData()
	if len(data.Routes) == 0 && len(data.ProcessedFiles) == 0 {
		writeError(w, http.StatusNotFound, "no drive data found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=drive-data.json")
	json.NewEncoder(w).Encode(&data)
}

// POST /api/drives/data/upload — upload a drive-data.json file.
// The file is streamed to disk and decoded in the background so the Pi
// can handle arbitrarily large files without OOMing on the HTTP request.
// While importing, drive processing is blocked and the status endpoint
// reports importing=true so the frontend can show progress.
func (dh *DriveHandlers) uploadData(w http.ResponseWriter, r *http.Request) {
	if dh.processor.IsRunning() {
		writeError(w, http.StatusConflict, "cannot upload while processing is running")
		return
	}
	dh.importMu.Lock()
	if dh.importing {
		dh.importMu.Unlock()
		writeError(w, http.StatusConflict, "import already in progress")
		return
	}
	dh.importMu.Unlock()

	// Stream the request body to a temp file on the writable partition.
	// This uses minimal RAM regardless of file size.
	tmpPath := filepath.Join(filepath.Dir(dh.store.Path()), "drive-data-import.tmp")
	f, err := os.Create(tmpPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create temp file: %v", err))
		return
	}
	n, err := io.Copy(f, r.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save upload: %v", err))
		return
	}

	log.Printf("[drives] Received drive-data upload (%d bytes), starting background import", n)

	dh.importMu.Lock()
	dh.importing = true
	dh.importMu.Unlock()

	// Decode and replace store data in the background.
	go func() {
		defer func() {
			os.Remove(tmpPath)
			dh.importMu.Lock()
			dh.importing = false
			dh.importMu.Unlock()
		}()

		archiveLog("Drive data import started (%d bytes)", n)
		dh.hub.Broadcast("drive_import", map[string]interface{}{
			"status": "started",
		})

		f, err := os.Open(tmpPath)
		if err != nil {
			archiveLog("Drive data import error: failed to open temp file: %v", err)
			log.Printf("[drives] Import error: failed to open temp file: %v", err)
			dh.hub.Broadcast("drive_import", map[string]interface{}{
				"status": "error", "error": err.Error(),
			})
			return
		}

		var data drives.StoreData
		if err := json.NewDecoder(f).Decode(&data); err != nil {
			f.Close()
			archiveLog("Drive data import error: invalid JSON: %v", err)
			log.Printf("[drives] Import error: invalid JSON: %v", err)
			dh.hub.Broadcast("drive_import", map[string]interface{}{
				"status": "error", "error": fmt.Sprintf("invalid JSON: %v", err),
			})
			return
		}
		f.Close()

		if len(data.Routes) == 0 && len(data.ProcessedFiles) == 0 {
			archiveLog("Drive data import error: file contains no routes or processed files")
			log.Printf("[drives] Import error: decoded file contains no routes or processed files")
			dh.hub.Broadcast("drive_import", map[string]interface{}{
				"status": "error", "error": "file contains no drive data — import aborted",
			})
			return
		}

		// Free decoder memory before replacing store data
		runtime.GC()

		dh.store.ReplaceData(data)
		if err := dh.store.Save(); err != nil {
			archiveLog("Drive data import error: failed to save: %v", err)
			log.Printf("[drives] Import error: failed to save: %v", err)
			dh.hub.Broadcast("drive_import", map[string]interface{}{
				"status": "error", "error": fmt.Sprintf("failed to save: %v", err),
			})
			return
		}

		archiveLog("Drive data import complete: %d routes, %d processed files",
			len(data.Routes), len(data.ProcessedFiles))
		log.Printf("[drives] Import complete: %d routes, %d processed files",
			len(data.Routes), len(data.ProcessedFiles))
		dh.hub.Broadcast("drive_import", map[string]interface{}{
			"status":          "complete",
			"routes_count":    len(data.Routes),
			"processed_count": len(data.ProcessedFiles),
		})
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status": "importing",
	})
}

// POST /api/drives/data/export-for-sync — regenerates the canonical
// /mutable/drive-data.json mirror from the SQLite store so
// post-archive-process.sh's rsync/rclone block can ship it to the
// archive server.
//
// This is the small "make the archive copy current" hook the shell
// script calls before each remote sync. The Go-side SyncToArchive
// (CIFS/NFS users) regenerates the mirror itself; this endpoint exists
// for the rsync/rclone shell paths that don't go through Go to copy
// the file.
//
// Returns 200 + the new file size on success, 500 on export failure.
// Refuses (409) if processing or import is currently running so we
// don't snapshot a half-written DB during a heavy AddRoute burst.
func (dh *DriveHandlers) exportForSync(w http.ResponseWriter, r *http.Request) {
	if dh.processor.IsRunning() {
		writeError(w, http.StatusConflict, "drive processing in progress; export-for-sync deferred")
		return
	}
	dh.importMu.Lock()
	importing := dh.importing
	dh.importMu.Unlock()
	if importing {
		writeError(w, http.StatusConflict, "drive data import in progress; export-for-sync deferred")
		return
	}

	if err := dh.store.ExportJSONForSync(); err != nil {
		archiveLog("Export-for-sync failed: %v", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("export failed: %v", err))
		return
	}

	// Stat the resulting file to report size back to the shell script
	// (used in log lines and could feed an additional pre-rsync sanity
	// check at the shell layer).
	var size int64
	if info, err := os.Stat("/mutable/drive-data.json"); err == nil {
		size = info.Size()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "exported",
		"path":   "/mutable/drive-data.json",
		"bytes":  size,
	})
}

// GET /api/drives/stats — aggregate statistics
//
// Uses WithRouteSummaries so the endpoint reads the pre-computed
// aggregate columns rather than decoding every route's BLOBs. On a
// 5500-route rig this drops peak heap from ~300 MB to ~5 MB, which is
// what makes the migration useful on a 512 MB Pi Zero 2 W.
func (dh *DriveHandlers) driveStats(w http.ResponseWriter, r *http.Request) {
	var stats drives.AggregateStats
	var routeCount int
	dh.store.WithRouteSummaries(func(summaries []drives.RouteSummary) {
		stats = drives.ComputeAggregateStatsFromSummaries(summaries)
		routeCount = len(summaries)
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"drives_count":            stats.DrivesCount,
		"routes_count":            routeCount,
		"processed_count":         dh.store.ProcessedCount(),
		"total_distance_km":       math.Round(stats.TotalDistanceKm*100) / 100,
		"total_distance_mi":       math.Round(stats.TotalDistanceMi*100) / 100,
		"total_duration_ms":       stats.TotalDurationMs,
		"fsd_engaged_ms":          stats.FSDEngagedMs,
		"fsd_distance_km":         math.Round(stats.FSDDistanceKm*100) / 100,
		"fsd_distance_mi":         math.Round(stats.FSDDistanceMi*100) / 100,
		"fsd_percent":             stats.FSDPercent,
		"fsd_disengagements":      stats.FSDDisengagements,
		"fsd_accel_pushes":        stats.FSDAccelPushes,
		"autosteer_engaged_ms":    stats.AutosteerEngagedMs,
		"autosteer_distance_km":   math.Round(stats.AutosteerDistanceKm*100) / 100,
		"autosteer_distance_mi":   math.Round(stats.AutosteerDistanceMi*100) / 100,
		"tacc_engaged_ms":         stats.TACCEngagedMs,
		"tacc_distance_km":        math.Round(stats.TACCDistanceKm*100) / 100,
		"tacc_distance_mi":        math.Round(stats.TACCDistanceMi*100) / 100,
		"assisted_percent":        stats.AssistedPercent,
	})
}

// GET /api/drives/fsd-analytics — FSD analytics with daily/weekly breakdowns
// Query params: ?period=week (default), ?period=day, ?period=trip
func (dh *DriveHandlers) fsdAnalytics(w http.ResponseWriter, r *http.Request) {
	var allSummaries []drives.DriveSummary
	dh.store.WithRoutes(func(routes []drives.Route) {
		allSummaries = drives.GroupSummaries(routes)
	})
	runtime.GC()

	now := time.Now()
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}

	var periodStart time.Time
	switch period {
	case "day":
		periodStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		// Go back to most recent Monday (or 7 days ago)
		periodStart = now.AddDate(0, 0, -7)
	default:
		periodStart = time.Time{} // all time for "trip" or unknown
	}

	// Filter drives in period
	var periodDrives []drives.DriveSummary
	for _, d := range allSummaries {
		dt, err := time.Parse("2006-01-02T15:04:05", d.StartTime)
		if err != nil {
			continue
		}
		if !periodStart.IsZero() && dt.Before(periodStart) {
			continue
		}
		periodDrives = append(periodDrives, d)
	}

	// Aggregate stats (FSD = state 1 only for analytics)
	var totalDurationMs, fsdEngagedMs int64
	var totalDistKm, totalDistMi, fsdDistKm, fsdDistMi float64
	var disengagements, accelPushes, fsdSessions int
	var autosteerEngagedMs, taccEngagedMs int64
	var autosteerDistKm, autosteerDistMi, taccDistKm, taccDistMi float64

	// Daily breakdown
	type dayStats struct {
		Date            string  `json:"date"`
		DayName         string  `json:"dayName"`
		Disengagements  int     `json:"disengagements"`
		AccelPushes     int     `json:"accelPushes"`
		FSDPercent      float64 `json:"fsdPercent"`
		Drives          int     `json:"drives"`
		FSDDistanceKm   float64 `json:"fsdDistanceKm"`
		FSDDistanceMi   float64 `json:"fsdDistanceMi"`
		TotalDurationMs int64   `json:"totalDurationMs"`
		FSDEngagedMs    int64   `json:"fsdEngagedMs"`
		TotalDistanceKm float64 `json:"-"`
	}
	dailyMap := make(map[string]*dayStats)

	var bestDay string
	var bestDayPercent float64

	for _, d := range periodDrives {
		totalDurationMs += d.DurationMs
		fsdEngagedMs += d.FSDEngagedMs
		totalDistKm += d.DistanceKm
		totalDistMi += d.DistanceMi
		fsdDistKm += d.FSDDistanceKm
		fsdDistMi += d.FSDDistanceMi
		disengagements += d.FSDDisengagements
		accelPushes += d.FSDAccelPushes
		autosteerEngagedMs += d.AutosteerEngagedMs
		autosteerDistKm += d.AutosteerDistanceKm
		autosteerDistMi += d.AutosteerDistanceMi
		taccEngagedMs += d.TACCEngagedMs
		taccDistKm += d.TACCDistanceKm
		taccDistMi += d.TACCDistanceMi
		if d.FSDEngagedMs > 0 {
			fsdSessions++
		}

		// Daily stats
		dt, err := time.Parse("2006-01-02T15:04:05", d.StartTime)
		if err != nil {
			continue
		}
		dateKey := dt.Format("2006-01-02")
		ds, ok := dailyMap[dateKey]
		if !ok {
			ds = &dayStats{
				Date:    dateKey,
				DayName: dt.Weekday().String()[:3],
			}
			dailyMap[dateKey] = ds
		}
		ds.Disengagements += d.FSDDisengagements
		ds.AccelPushes += d.FSDAccelPushes
		ds.Drives++
		ds.FSDDistanceKm += d.FSDDistanceKm
		ds.FSDDistanceMi += d.FSDDistanceMi
		ds.TotalDurationMs += d.DurationMs
		ds.FSDEngagedMs += d.FSDEngagedMs
		ds.TotalDistanceKm += d.DistanceKm
	}

	// Compute daily FSD percent and find best day
	for _, ds := range dailyMap {
		if ds.TotalDistanceKm > 0 {
			ds.FSDPercent = math.Round(ds.FSDDistanceKm/ds.TotalDistanceKm*1000) / 10
		}
		ds.FSDDistanceKm = math.Round(ds.FSDDistanceKm*100) / 100
		ds.FSDDistanceMi = math.Round(ds.FSDDistanceMi*100) / 100
		if ds.FSDPercent > bestDayPercent {
			bestDayPercent = ds.FSDPercent
			bestDay = ds.Date
		}
	}

	// Sort daily stats by date
	var dailyStats []dayStats
	for _, ds := range dailyMap {
		dailyStats = append(dailyStats, *ds)
	}
	// Simple sort by date string
	for i := 0; i < len(dailyStats); i++ {
		for j := i + 1; j < len(dailyStats); j++ {
			if dailyStats[i].Date > dailyStats[j].Date {
				dailyStats[i], dailyStats[j] = dailyStats[j], dailyStats[i]
			}
		}
	}

	// Today's stats
	todayKey := now.Format("2006-01-02")
	var todayPercent float64
	if ds, ok := dailyMap[todayKey]; ok {
		todayPercent = ds.FSDPercent
	}

	var fsdPercent float64
	if totalDistKm > 0 {
		fsdPercent = math.Round(fsdDistKm/totalDistKm*1000) / 10
	}

	// Compute FSD grade (3 tiers)
	var fsdGrade string
	if fsdPercent >= 90 {
		fsdGrade = "Great"
	} else if fsdPercent >= 60 {
		fsdGrade = "Good"
	} else {
		fsdGrade = "Needs Improvement"
	}

	// Compute streak (consecutive days with FSD usage, counting backwards from today)
	streakDays := 0
	checkDate := now
	for {
		key := checkDate.Format("2006-01-02")
		ds, ok := dailyMap[key]
		if ok && ds.FSDEngagedMs > 0 {
			streakDays++
			checkDate = checkDate.AddDate(0, 0, -1)
		} else {
			break
		}
	}

	// Format FSD engaged time
	totalSec := fsdEngagedMs / 1000
	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	var fsdTimeFormatted string
	if hours > 0 {
		fsdTimeFormatted = fmt.Sprintf("%dh %dm", hours, mins)
	} else {
		fsdTimeFormatted = fmt.Sprintf("%dm", mins)
	}

	// Avg disengagements per drive
	var avgDisengagements float64
	if fsdSessions > 0 {
		avgDisengagements = math.Round(float64(disengagements)/float64(fsdSessions)*100) / 100
	}

	// Avg accel pushes per drive
	var avgAccelPushes float64
	if fsdSessions > 0 {
		avgAccelPushes = math.Round(float64(accelPushes)/float64(fsdSessions)*100) / 100
	}

	// Assisted totals
	totalAssistedDistKm := fsdDistKm + autosteerDistKm + taccDistKm
	var assistedPercent float64
	if totalDistKm > 0 {
		assistedPercent = math.Round(totalAssistedDistKm/totalDistKm*1000) / 10
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"period":                       period,
		"period_start":                 periodStart.Format("2006-01-02"),
		"total_drives":                 len(periodDrives),
		"fsd_sessions":                 fsdSessions,
		"fsd_percent":                  fsdPercent,
		"today_percent":                todayPercent,
		"best_day":                     bestDay,
		"best_day_percent":             bestDayPercent,
		"fsd_engaged_ms":               fsdEngagedMs,
		"fsd_distance_km":              math.Round(fsdDistKm*100) / 100,
		"fsd_distance_mi":              math.Round(fsdDistMi*100) / 100,
		"total_distance_km":            math.Round(totalDistKm*100) / 100,
		"total_distance_mi":            math.Round(totalDistMi*100) / 100,
		"disengagements":               disengagements,
		"accel_pushes":                 accelPushes,
		"daily":                        dailyStats,
		"fsd_grade":                    fsdGrade,
		"streak_days":                  streakDays,
		"fsd_time_formatted":           fsdTimeFormatted,
		"avg_disengagements_per_drive":  avgDisengagements,
		"avg_accel_pushes_per_drive":    avgAccelPushes,
		"autosteer_engaged_ms":         autosteerEngagedMs,
		"autosteer_distance_km":        math.Round(autosteerDistKm*100) / 100,
		"autosteer_distance_mi":        math.Round(autosteerDistMi*100) / 100,
		"tacc_engaged_ms":              taccEngagedMs,
		"tacc_distance_km":             math.Round(taccDistKm*100) / 100,
		"tacc_distance_mi":             math.Round(taccDistMi*100) / 100,
		"assisted_percent":             assistedPercent,
	})
}
