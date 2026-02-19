package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"

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
	mux.HandleFunc("GET /api/drives/status", dh.processingStatus)
	mux.HandleFunc("GET /api/drives/data/download", dh.downloadData)
	mux.HandleFunc("POST /api/drives/data/upload", dh.uploadData)
	mux.HandleFunc("GET /api/drives/stats", dh.driveStats)
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

// POST /api/drives/process — trigger GPS extraction on a clips directory
// Body: { "clips_dir": "/path/to/RecentClips", "throttle_ms": 10 }
func (dh *DriveHandlers) processFiles(w http.ResponseWriter, r *http.Request) {
	if dh.processor.IsRunning() {
		writeError(w, http.StatusConflict, "processing already in progress")
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

	// Run in background
	go func() {
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
			dh.hub.Broadcast("drive_process", map[string]interface{}{
				"status": "error", "error": err.Error(),
			})
			return
		}

		// Sync drive data to archive mount (best-effort)
		if err := dh.store.SyncToArchive(); err != nil {
			log.Printf("[drives] Warning: failed to sync to archive: %v", err)
		}

		dh.hub.Broadcast("drive_process", map[string]interface{}{
			"status": "complete", "result": result,
		})
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":    "started",
		"clips_dir": body.ClipsDir,
	})
}

// GET /api/drives/status — check if processing is running
func (dh *DriveHandlers) processingStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"running":         dh.processor.IsRunning(),
		"routes_count":    dh.store.RouteCount(),
		"processed_count": dh.store.ProcessedCount(),
	})
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
	for _, d := range allDrives {
		totalDistKm += d.DistanceKm
		totalDistMi += d.DistanceMi
		totalDurationMs += d.DurationMs
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"drives_count":     len(allDrives),
		"routes_count":     len(routes),
		"processed_count":  dh.store.ProcessedCount(),
		"total_distance_km": math.Round(totalDistKm*100) / 100,
		"total_distance_mi": math.Round(totalDistMi*100) / 100,
		"total_duration_ms": totalDurationMs,
	})
}
