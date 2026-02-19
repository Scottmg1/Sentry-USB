package drives

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Drive represents a logical driving session grouped from consecutive clips.
type Drive struct {
	ID          int        `json:"id"`
	Date        string     `json:"date"`
	StartTime   string     `json:"startTime"`
	EndTime     string     `json:"endTime"`
	DurationMs  int64      `json:"durationMs"`
	DistanceMi  float64    `json:"distanceMi"`
	DistanceKm  float64    `json:"distanceKm"`
	AvgSpeedMph float64    `json:"avgSpeedMph"`
	MaxSpeedMph float64    `json:"maxSpeedMph"`
	AvgSpeedKmh float64    `json:"avgSpeedKmh"`
	MaxSpeedKmh float64    `json:"maxSpeedKmh"`
	ClipCount   int        `json:"clipCount"`
	PointCount  int        `json:"pointCount"`
	Points      [][4]float64 `json:"points"` // [lat, lng, timeMs, speedMps]
	Tags        []string     `json:"tags,omitempty"`
}

// DriveSummary is a lighter version of Drive without full point data (for list views).
type DriveSummary struct {
	ID          int        `json:"id"`
	Date        string     `json:"date"`
	StartTime   string     `json:"startTime"`
	EndTime     string     `json:"endTime"`
	DurationMs  int64      `json:"durationMs"`
	DistanceMi  float64    `json:"distanceMi"`
	DistanceKm  float64    `json:"distanceKm"`
	AvgSpeedMph float64    `json:"avgSpeedMph"`
	MaxSpeedMph float64    `json:"maxSpeedMph"`
	AvgSpeedKmh float64    `json:"avgSpeedKmh"`
	MaxSpeedKmh float64    `json:"maxSpeedKmh"`
	ClipCount   int        `json:"clipCount"`
	PointCount  int        `json:"pointCount"`
	StartPoint  *[2]float64 `json:"startPoint"`
	EndPoint    *[2]float64 `json:"endPoint"`
	Tags        []string    `json:"tags,omitempty"`
}

// driveGapMs is the time gap threshold to split clips into separate drives (5 minutes).
const driveGapMs = 5 * 60 * 1000

var fileTimestampRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})_(\d{2})-(\d{2})-(\d{2})`)

type timedRoute struct {
	Route
	timestamp time.Time
}

// GroupIntoDrives groups routes into logical drives based on time gaps and gear state.
// First pass: split on time gaps > 5 minutes between clips.
// Second pass: split further when gear state transitions through Park.
func GroupIntoDrives(routes []Route) []Drive {
	// Deduplicate routes by normalized file path (handles mixed \ and / from imports)
	seen := make(map[string]bool, len(routes))
	var unique []Route
	for _, r := range routes {
		norm := strings.ReplaceAll(r.File, "\\", "/")
		if !seen[norm] {
			seen[norm] = true
			unique = append(unique, r)
		}
	}

	// Parse timestamps and sort
	var timed []timedRoute
	for _, r := range unique {
		if t := parseFileTimestamp(r.File); !t.IsZero() {
			timed = append(timed, timedRoute{Route: r, timestamp: t})
		}
	}

	if len(timed) == 0 {
		return nil
	}

	sort.Slice(timed, func(i, j int) bool {
		return timed[i].timestamp.Before(timed[j].timestamp)
	})

	// First pass: group by time gap
	var timeGroups [][]timedRoute
	current := []timedRoute{timed[0]}

	for i := 1; i < len(timed); i++ {
		gap := timed[i].timestamp.Sub(current[len(current)-1].timestamp).Milliseconds()
		if gap > driveGapMs {
			timeGroups = append(timeGroups, current)
			current = []timedRoute{timed[i]}
		} else {
			current = append(current, timed[i])
		}
	}
	timeGroups = append(timeGroups, current)

	// Second pass: split each time group further by gear state (Park transitions)
	var groups [][]timedRoute
	for _, tg := range timeGroups {
		groups = append(groups, splitByGearState(tg)...)
	}

	// Build drive stats
	drives := make([]Drive, 0, len(groups))
	for idx, group := range groups {
		drives = append(drives, buildDriveStats(group, idx))
	}

	return drives
}

// parkThresholdSeconds is the minimum time in Park (seconds) for a clip to be
// considered a drive boundary. Clips with >= this much park time split drives.
const parkThresholdSeconds = 15.0

// splitByGearState splits a group of clips into sub-groups when the gear state
// shows a Park period between driving segments. Clips with >= 15 seconds of
// Park time (estimated from raw frame counts) are treated as boundaries.
// Falls back to no splitting when no gear data is available.
func splitByGearState(group []timedRoute) [][]timedRoute {
	if len(group) <= 1 {
		return [][]timedRoute{group}
	}

	// Check if any clip has gear data
	hasGear := false
	for _, clip := range group {
		if len(clip.GearStates) > 0 {
			hasGear = true
			break
		}
	}
	if !hasGear {
		return [][]timedRoute{group}
	}

	var result [][]timedRoute
	var current []timedRoute

	for _, clip := range group {
		if clipIsMostlyParked(clip) {
			// Park clip — finalize the current drive segment
			if len(current) > 0 {
				result = append(result, current)
				current = nil
			}
		} else {
			current = append(current, clip)
		}
	}
	if len(current) > 0 {
		result = append(result, current)
	}

	// If everything was parked, return original group to avoid losing data
	if len(result) == 0 {
		return [][]timedRoute{group}
	}
	return result
}

// clipIsMostlyParked returns true if the clip has >= parkThresholdSeconds of Park time.
// Uses raw (pre-dedup) frame counts for accurate time estimation.
// Returns false if no gear data is available (legacy clips).
func clipIsMostlyParked(clip timedRoute) bool {
	// Use raw frame counts if available (accurate, pre-dedup)
	if clip.RawFrameCount > 0 {
		parkSeconds := float64(clip.RawParkCount) / float64(clip.RawFrameCount) * 60.0
		return parkSeconds >= parkThresholdSeconds
	}
	// Fallback for data processed without raw counts: use deduplicated gear states
	if len(clip.GearStates) == 0 {
		return false
	}
	parkCount := 0
	for _, g := range clip.GearStates {
		if g == GearPark {
			parkCount++
		}
	}
	return parkCount > len(clip.GearStates)/2
}

// ApplyTags populates the Tags field on each drive from a tag map (keyed by StartTime).
func ApplyTags(drives []Drive, tagMap map[string][]string) {
	if len(tagMap) == 0 {
		return
	}
	for i := range drives {
		if tags, ok := tagMap[drives[i].StartTime]; ok {
			drives[i].Tags = tags
		}
	}
}

// ApplySummaryTags populates the Tags field on each summary from a tag map.
func ApplySummaryTags(summaries []DriveSummary, tagMap map[string][]string) {
	if len(tagMap) == 0 {
		return
	}
	for i := range summaries {
		if tags, ok := tagMap[summaries[i].StartTime]; ok {
			summaries[i].Tags = tags
		}
	}
}

// GroupSummaries returns only the summary (no full points) for each drive.
func GroupSummaries(routes []Route) []DriveSummary {
	drives := GroupIntoDrives(routes)
	summaries := make([]DriveSummary, len(drives))
	for i, d := range drives {
		s := DriveSummary{
			ID:          d.ID,
			Date:        d.Date,
			StartTime:   d.StartTime,
			EndTime:     d.EndTime,
			DurationMs:  d.DurationMs,
			DistanceMi:  d.DistanceMi,
			DistanceKm:  d.DistanceKm,
			AvgSpeedMph: d.AvgSpeedMph,
			MaxSpeedMph: d.MaxSpeedMph,
			AvgSpeedKmh: d.AvgSpeedKmh,
			MaxSpeedKmh: d.MaxSpeedKmh,
			ClipCount:   d.ClipCount,
			PointCount:  d.PointCount,
		}
		if len(d.Points) > 0 {
			start := [2]float64{d.Points[0][0], d.Points[0][1]}
			end := [2]float64{d.Points[len(d.Points)-1][0], d.Points[len(d.Points)-1][1]}
			s.StartPoint = &start
			s.EndPoint = &end
		}
		summaries[i] = s
	}
	return summaries
}

func buildDriveStats(clips []timedRoute, idx int) Drive {
	firstClip := clips[0]
	lastClip := clips[len(clips)-1]
	startTime := firstClip.timestamp
	endTime := lastClip.timestamp.Add(time.Minute)

	// Merge all points with interpolated timestamps
	type annotatedPoint struct {
		lat, lng float64
		timeMs   float64
	}
	var allPoints []annotatedPoint

	for _, clip := range clips {
		clipStart := float64(clip.timestamp.UnixMilli())
		n := len(clip.Points)
		clipDurationMs := float64(60000)
		for i := 0; i < n; i++ {
			var t float64
			if n > 1 {
				t = clipStart + (clipDurationMs * float64(i) / float64(n-1))
			} else {
				t = clipStart
			}
			allPoints = append(allPoints, annotatedPoint{
				lat:    clip.Points[i][0],
				lng:    clip.Points[i][1],
				timeMs: t,
			})
		}
	}

	// Compute distance and speeds
	var totalDistanceM float64
	var maxSpeedMps float64
	var speeds []float64

	for i := 1; i < len(allPoints); i++ {
		d := haversineM(allPoints[i-1].lat, allPoints[i-1].lng, allPoints[i].lat, allPoints[i].lng)
		dt := (allPoints[i].timeMs - allPoints[i-1].timeMs) / 1000.0
		totalDistanceM += d

		if dt > 0 {
			speed := d / dt
			// Filter unreasonable speeds (GPS noise) > 70 m/s (~155 mph)
			if speed < 70 {
				speeds = append(speeds, speed)
				if speed > maxSpeedMps {
					maxSpeedMps = speed
				}
			}
		}
	}

	var avgSpeedMps float64
	if len(speeds) > 0 {
		var sum float64
		for _, s := range speeds {
			sum += s
		}
		avgSpeedMps = sum / float64(len(speeds))
	}

	// Build point data array: [lat, lng, timeMs, speedMps]
	pointData := make([][4]float64, len(allPoints))
	for i, p := range allPoints {
		var speed float64
		if i > 0 {
			d := haversineM(allPoints[i-1].lat, allPoints[i-1].lng, p.lat, p.lng)
			dt := (p.timeMs - allPoints[i-1].timeMs) / 1000.0
			if dt > 0 {
				speed = math.Min(d/dt, 70)
			}
		}
		pointData[i] = [4]float64{p.lat, p.lng, p.timeMs, math.Round(speed*100) / 100}
	}

	durationMs := endTime.Sub(startTime).Milliseconds()

	return Drive{
		ID:          idx,
		Date:        firstClip.Date,
		StartTime:   startTime.Format("2006-01-02T15:04:05"),
		EndTime:     endTime.Format("2006-01-02T15:04:05"),
		DurationMs:  durationMs,
		DistanceMi:  math.Round(totalDistanceM/1609.344*100) / 100,
		DistanceKm:  math.Round(totalDistanceM/1000*100) / 100,
		AvgSpeedMph: math.Round(avgSpeedMps*2.23694*100) / 100,
		MaxSpeedMph: math.Round(maxSpeedMps*2.23694*100) / 100,
		AvgSpeedKmh: math.Round(avgSpeedMps*3.6*100) / 100,
		MaxSpeedKmh: math.Round(maxSpeedMps*3.6*100) / 100,
		ClipCount:   len(clips),
		PointCount:  len(allPoints),
		Points:      pointData,
	}
}

func parseFileTimestamp(filePath string) time.Time {
	m := fileTimestampRegex.FindStringSubmatch(filePath)
	if m == nil {
		return time.Time{}
	}
	s := m[1] + "T" + m[2] + ":" + m[3] + ":" + m[4]
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// haversineM computes the distance in meters between two GPS coordinates.
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180.0 }

	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Downsample returns at most maxPoints entries from the slice, evenly spaced.
func Downsample(points [][2]float64, maxPoints int) [][2]float64 {
	if len(points) <= maxPoints {
		return points
	}
	step := float64(len(points)) / float64(maxPoints)
	result := make([][2]float64, 0, maxPoints+1)
	for i := 0; i < maxPoints; i++ {
		result = append(result, points[int(float64(i)*step)])
	}
	result = append(result, points[len(points)-1])
	return result
}
