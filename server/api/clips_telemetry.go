package api

import (
	"net/http"
	"path/filepath"

	"github.com/Scottmg1/Sentry-USB/server/drives"
)

type telemetryFrame struct {
	T         float64 `json:"t"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	SpeedMPS  float32 `json:"speed_mps"`
	Gear      uint8   `json:"gear"`
	Autopilot uint8   `json:"autopilot"`
	AccelPos  float32 `json:"accel_pos"`
}

type telemetryResponse struct {
	Frames       []telemetryFrame `json:"frames"`
	DurationSec  float64          `json:"duration_sec"`
	HasGPS       bool             `json:"has_gps"`
	HasAutopilot bool             `json:"has_autopilot"`
}

// GET /api/clips/telemetry?path=/TeslaCam/SentryClips/2024-01-15_12-00-00&file=2024-01-15_12-00-00-front.mp4
func (h *handlers) getClipTelemetry(w http.ResponseWriter, r *http.Request) {
	clipPath := r.URL.Query().Get("path")
	fileName := r.URL.Query().Get("file")
	if clipPath == "" || fileName == "" {
		writeError(w, http.StatusBadRequest, "path and file query parameters are required")
		return
	}

	// Resolve to absolute path under /mutable
	absPath := filepath.Join("/mutable", clipPath, fileName)

	// Security: ensure the resolved path stays under /mutable/TeslaCam
	absPath = filepath.Clean(absPath)
	base := filepath.Clean("/mutable/TeslaCam")
	if len(absPath) < len(base) || absPath[:len(base)] != base {
		writeError(w, http.StatusForbidden, "path must be under /TeslaCam")
		return
	}

	points, gears, apStates, speeds, accelPositions, err := drives.ExtractGPSFromFile(absPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "could not read file: "+err.Error())
		return
	}

	const fps = 36.0
	frames := make([]telemetryFrame, len(points))
	hasGPS := false
	hasAutopilot := false

	for i, pt := range points {
		frames[i] = telemetryFrame{
			T:         float64(i) / fps,
			Lat:       pt[0],
			Lng:       pt[1],
			SpeedMPS:  speeds[i],
			Gear:      gears[i],
			Autopilot: apStates[i],
			AccelPos:  accelPositions[i],
		}
		if pt[0] != 0 || pt[1] != 0 {
			hasGPS = true
		}
		if apStates[i] != drives.AutopilotOff {
			hasAutopilot = true
		}
	}

	var durationSec float64
	if len(frames) > 0 {
		durationSec = float64(len(frames)) / fps
	}

	writeJSON(w, http.StatusOK, telemetryResponse{
		Frames:       frames,
		DurationSec:  durationSec,
		HasGPS:       hasGPS,
		HasAutopilot: hasAutopilot,
	})
}
