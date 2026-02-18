package drives

import (
	"math"
	"regexp"
	"sort"
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
}

// driveGapMs is the time gap threshold to split clips into separate drives (5 minutes).
const driveGapMs = 5 * 60 * 1000

var fileTimestampRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})_(\d{2})-(\d{2})-(\d{2})`)

type timedRoute struct {
	Route
	timestamp time.Time
}

// GroupIntoDrives groups routes into logical drives based on time gaps.
func GroupIntoDrives(routes []Route) []Drive {
	// Parse timestamps and sort
	var timed []timedRoute
	for _, r := range routes {
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

	// Group by time gap
	var groups [][]timedRoute
	current := []timedRoute{timed[0]}

	for i := 1; i < len(timed); i++ {
		gap := timed[i].timestamp.Sub(current[len(current)-1].timestamp).Milliseconds()
		if gap > driveGapMs {
			groups = append(groups, current)
			current = []timedRoute{timed[i]}
		} else {
			current = append(current, timed[i])
		}
	}
	groups = append(groups, current)

	// Build drive stats
	drives := make([]Drive, 0, len(groups))
	for idx, group := range groups {
		drives = append(drives, buildDriveStats(group, idx))
	}

	return drives
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
