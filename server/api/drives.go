package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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
	routes := dh.store.GetRoutes()
	summaries := drives.GroupSummaries(routes)
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
	routes := dh.store.GetRoutes()
	allDrives := drives.GroupIntoDrives(routes)

	type routeOverview struct {
		ID     int          `json:"id"`
		Points [][2]float64 `json:"points"`
	}

	result := make([]routeOverview, 0, len(allDrives))
	for _, d := range allDrives {
		pts := make([][2]float64, len(d.Points))
		for i, p := range d.Points {
			pts[i] = [2]float64{p[0], p[1]}
		}
		result = append(result, routeOverview{
			ID:     d.ID,
			Points: drives.Downsample(pts, 200),
		})
	}

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

	routes := dh.store.GetRoutes()
	allDrives := drives.GroupIntoDrives(routes)
	drives.ApplyTags(allDrives, dh.store.GetAllDriveTags())

	if id < 0 || id >= len(allDrives) {
		writeError(w, http.StatusNotFound, "drive not found")
		return
	}

	writeJSON(w, http.StatusOK, allDrives[id])
}

// PUT /api/drives/{id}/tags — set tags for a drive
// Body: { "tags": ["Work", "Commute"] }
func (dh *DriveHandlers) setDriveTags(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid drive id")
		return
	}

	routes := dh.store.GetRoutes()
	allDrives := drives.GroupIntoDrives(routes)

	if id < 0 || id >= len(allDrives) {
		writeError(w, http.StatusNotFound, "drive not found")
		return
	}

	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	driveKey := allDrives[id].StartTime
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
	fmt.Fprintf(f, "%s: [drive-map] %s\n", time.Now().Format("Mon 02 Jan 15:04:05 MST 2006"), msg)
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
		archiveLog("Drive processing complete. Files: %d, GPS: %d, Drives: %d, Errors: %d (%s)",
			result.FilesNew, result.FilesWithGPS, result.DrivesFound, result.Errors, result.Duration)

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
		archiveLog("Reprocess complete. Files: %d, GPS: %d, Drives: %d, Errors: %d (%s)",
			result.FilesNew, result.FilesWithGPS, result.DrivesFound, result.Errors, result.Duration)

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
	resp := map[string]interface{}{
		"running":         dh.processor.IsRunning(),
		"routes_count":    dh.store.RouteCount(),
		"processed_count": dh.store.ProcessedCount(),
		"archiving":       IsArchiving(),
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

// POST /api/drives/data/upload — upload a drive-data.json file to replace current data
func (dh *DriveHandlers) uploadData(w http.ResponseWriter, r *http.Request) {
	if dh.processor.IsRunning() {
		writeError(w, http.StatusConflict, "cannot upload while processing is running")
		return
	}

	// Limit to 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024)

	var data drives.StoreData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	dh.store.ReplaceData(data)
	if err := dh.store.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"routes_count":    len(data.Routes),
		"processed_count": len(data.ProcessedFiles),
	})
}

// GET /api/drives/stats — aggregate statistics
func (dh *DriveHandlers) driveStats(w http.ResponseWriter, r *http.Request) {
	routes := dh.store.GetRoutes()
	allDrives := drives.GroupIntoDrives(routes)

	var totalDistKm, totalDistMi float64
	var totalDurationMs int64
	var totalFSDEngagedMs int64
	var totalFSDDistKm, totalFSDDistMi float64
	var totalDisengagements, totalAccelPushes int
	for _, d := range allDrives {
		totalDistKm += d.DistanceKm
		totalDistMi += d.DistanceMi
		totalDurationMs += d.DurationMs
		totalFSDEngagedMs += d.FSDEngagedMs
		totalFSDDistKm += d.FSDDistanceKm
		totalFSDDistMi += d.FSDDistanceMi
		totalDisengagements += d.FSDDisengagements
		totalAccelPushes += d.FSDAccelPushes
	}

	var fsdPercent float64
	if totalDistKm > 0 {
		fsdPercent = math.Round(totalFSDDistKm/totalDistKm*1000) / 10
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"drives_count":       len(allDrives),
		"routes_count":       len(routes),
		"processed_count":    dh.store.ProcessedCount(),
		"total_distance_km":  math.Round(totalDistKm*100) / 100,
		"total_distance_mi":  math.Round(totalDistMi*100) / 100,
		"total_duration_ms":  totalDurationMs,
		"fsd_engaged_ms":     totalFSDEngagedMs,
		"fsd_distance_km":    math.Round(totalFSDDistKm*100) / 100,
		"fsd_distance_mi":    math.Round(totalFSDDistMi*100) / 100,
		"fsd_percent":        fsdPercent,
		"fsd_disengagements": totalDisengagements,
		"fsd_accel_pushes":   totalAccelPushes,
	})
}

// GET /api/drives/fsd-analytics — FSD analytics with daily/weekly breakdowns
// Query params: ?period=week (default), ?period=day, ?period=trip
func (dh *DriveHandlers) fsdAnalytics(w http.ResponseWriter, r *http.Request) {
	routes := dh.store.GetRoutes()
	allDrives := drives.GroupIntoDrives(routes)

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
	var periodDrives []drives.Drive
	for _, d := range allDrives {
		dt, err := time.Parse("2006-01-02T15:04:05", d.StartTime)
		if err != nil {
			continue
		}
		if !periodStart.IsZero() && dt.Before(periodStart) {
			continue
		}
		periodDrives = append(periodDrives, d)
	}

	// Aggregate stats
	var totalDurationMs, fsdEngagedMs int64
	var totalDistKm, totalDistMi, fsdDistKm, fsdDistMi float64
	var disengagements, accelPushes, fsdSessions int

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
	})
}
