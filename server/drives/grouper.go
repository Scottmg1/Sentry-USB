package drives

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FSDEvent records the location of a notable FSD event (disengagement or accel push).
type FSDEvent struct {
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	Type string  `json:"type"` // "disengagement" or "accel_push"
}

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
	GearStates  []int        `json:"gearStates,omitempty"` // parallel to Points: 0=P, 1=D, 2=R, 3=N
	FSDStates   []int        `json:"fsdStates,omitempty"` // parallel to Points: 0=manual, >0=FSD engaged
	FSDEvents   []FSDEvent   `json:"fsdEvents,omitempty"` // locations of disengagements and accel pushes
	Tags        []string     `json:"tags,omitempty"`
	// FSD analytics per drive (state=1 only — Full Self-Driving)
	FSDEngagedMs      int64   `json:"fsdEngagedMs"`
	FSDDisengagements int     `json:"fsdDisengagements"`
	FSDAccelPushes    int     `json:"fsdAccelPushes"`
	FSDPercent        float64 `json:"fsdPercent"`
	FSDDistanceKm     float64 `json:"fsdDistanceKm"`
	FSDDistanceMi     float64 `json:"fsdDistanceMi"`
	// Autosteer analytics (state=2)
	AutosteerEngagedMs  int64   `json:"autosteerEngagedMs"`
	AutosteerPercent    float64 `json:"autosteerPercent"`
	AutosteerDistanceKm float64 `json:"autosteerDistanceKm"`
	AutosteerDistanceMi float64 `json:"autosteerDistanceMi"`
	// TACC analytics (state=3)
	TACCEngagedMs  int64   `json:"taccEngagedMs"`
	TACCPercent    float64 `json:"taccPercent"`
	TACCDistanceKm float64 `json:"taccDistanceKm"`
	TACCDistanceMi float64 `json:"taccDistanceMi"`
	// Assisted driving aggregate (any state > 0 — for map/UI)
	AssistedPercent float64 `json:"assistedPercent"`
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
	// FSD analytics summary (state=1 only)
	FSDEngagedMs      int64   `json:"fsdEngagedMs"`
	FSDDisengagements int     `json:"fsdDisengagements"`
	FSDAccelPushes    int     `json:"fsdAccelPushes"`
	FSDPercent        float64 `json:"fsdPercent"`
	FSDDistanceKm     float64 `json:"fsdDistanceKm"`
	FSDDistanceMi     float64 `json:"fsdDistanceMi"`
	// Autosteer (state=2)
	AutosteerEngagedMs  int64   `json:"autosteerEngagedMs"`
	AutosteerPercent    float64 `json:"autosteerPercent"`
	AutosteerDistanceKm float64 `json:"autosteerDistanceKm"`
	AutosteerDistanceMi float64 `json:"autosteerDistanceMi"`
	// TACC (state=3)
	TACCEngagedMs  int64   `json:"taccEngagedMs"`
	TACCPercent    float64 `json:"taccPercent"`
	TACCDistanceKm float64 `json:"taccDistanceKm"`
	TACCDistanceMi float64 `json:"taccDistanceMi"`
	// Assisted aggregate (any > 0)
	AssistedPercent float64 `json:"assistedPercent"`
	// Provenance — "sei" or "tessie". Tessie drives are excluded from
	// aggregate FSD/AP/TACC score in /api/drives/stats but counted in
	// drive count, total miles, and total duration. The UI uses Source
	// to render a "Tessie" chip on the drive list.
	Source                 string  `json:"source,omitempty"`
	TessieAutopilotPercent float64 `json:"tessieAutopilotPercent,omitempty"`
}

// driveGapMs is the time gap threshold to split clips into separate drives (5 minutes).
const driveGapMs = 5 * 60 * 1000

var fileTimestampRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})_(\d{2})-(\d{2})-(\d{2})`)

type timedRoute struct {
	Route
	timestamp time.Time
}

// groupClips performs the lightweight grouping of routes into drive clip-groups
// (dedup, timestamp sort, time-gap split, gear-state split) WITHOUT building
// full Drive objects or merging point arrays. This is O(routes) in memory.
func groupClips(routes []Route) [][]timedRoute {
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

	// Second pass: split each time group further by gear state (Park transitions),
	// then by ExternalSignature so adjacent Tessie drives stay separate. Without
	// the signature pass two back-to-back Tessie drives whose synthetic clips
	// abut at a minute boundary would merge into one drive in the UI.
	var groups [][]timedRoute
	for _, tg := range timeGroups {
		for _, gearGroup := range splitByGearState(tg) {
			groups = append(groups, splitByExternalSignature(gearGroup)...)
		}
	}

	return groups
}

// splitByExternalSignature buckets clips inside a single gear-group by
// their ExternalSignature. Clips without a signature (native SEI) stay
// together as one bucket; each non-empty signature becomes its own
// bucket. This is order-independent: even if two Tessie drives' clips
// interleave on tied timestamps after sorting, every clip with the same
// signature ends up in the same drive.
func splitByExternalSignature(group []timedRoute) [][]timedRoute {
	if len(group) <= 1 {
		return [][]timedRoute{group}
	}
	hasAny := false
	for _, c := range group {
		if c.ExternalSignature != "" {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return [][]timedRoute{group}
	}
	buckets := make(map[string][]timedRoute, 4)
	var noSig []timedRoute
	var order []string
	for _, c := range group {
		if c.ExternalSignature == "" {
			noSig = append(noSig, c)
			continue
		}
		if _, exists := buckets[c.ExternalSignature]; !exists {
			order = append(order, c.ExternalSignature)
		}
		buckets[c.ExternalSignature] = append(buckets[c.ExternalSignature], c)
	}
	var result [][]timedRoute
	if len(noSig) > 0 {
		result = append(result, noSig)
	}
	for _, sig := range order {
		result = append(result, buckets[sig])
	}
	return result
}

// GroupIntoDrives groups routes into logical drives based on time gaps and gear state.
// First pass: split on time gaps > 5 minutes between clips.
// Second pass: split further when gear state transitions through Park.
//
// WARNING: This builds full Drive objects with merged point arrays for every
// drive. For large datasets this allocates hundreds of MB. Prefer GroupSummaries,
// GroupRoutesOverview, or BuildSingleDrive when you don't need all drives' full
// point data.
func GroupIntoDrives(routes []Route) []Drive {
	groups := groupClips(routes)

	// Build drive stats
	drives := make([]Drive, 0, len(groups))
	for idx, group := range groups {
		drives = append(drives, buildDriveStats(group, idx))
	}

	return drives
}

// BuildSingleDrive builds the full Drive object for a single drive by index,
// without allocating point arrays for all other drives. Much cheaper than
// GroupIntoDrives when you only need one drive.
func BuildSingleDrive(routes []Route, id int) (Drive, bool) {
	groups := groupClips(routes)
	if id < 0 || id >= len(groups) {
		return Drive{}, false
	}
	return buildDriveStats(groups[id], id), true
}

// BuildDriveFromFiles builds a full Drive object from the routes whose
// normalized file paths are in `files`. The caller determines which files
// belong to a drive (e.g. via DriveClipFilesFromSummaries) so the grouping
// is guaranteed to match the summary-path drive list. The resulting Drive
// uses `id` as its ID field.
func BuildDriveFromFiles(routes []Route, files map[string]bool, id int) (Drive, bool) {
	var matched []Route
	for _, r := range routes {
		norm := strings.ReplaceAll(r.File, "\\", "/")
		if files[norm] {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return Drive{}, false
	}

	var timed []timedRoute
	for _, r := range matched {
		if t := parseFileTimestamp(r.File); !t.IsZero() {
			timed = append(timed, timedRoute{Route: r, timestamp: t})
		}
	}
	if len(timed) == 0 {
		return Drive{}, false
	}
	sort.Slice(timed, func(i, j int) bool {
		return timed[i].timestamp.Before(timed[j].timestamp)
	})
	return buildDriveStats(timed, id), true
}

// DriveStartTime returns the start time string for the drive at the given index.
// Used for tag lookups without building full drive objects.
func DriveStartTime(routes []Route, id int) (string, bool) {
	groups := groupClips(routes)
	if id < 0 || id >= len(groups) {
		return "", false
	}
	return groups[id][0].timestamp.Format("2006-01-02T15:04:05"), true
}

// DriveCount returns the number of drives without building full Drive objects.
func DriveCount(routes []Route) int {
	return len(groupClips(routes))
}

// parkGapSeconds is the minimum Park duration (seconds) that splits drives.
// Any Park period longer than this ends the current drive; if driving resumes
// within the same clip it becomes a new drive.
const parkGapSeconds = 2.0

// splitByGearState splits a group of clips into sub-groups when the gear state
// shows a Park period >= parkGapSeconds between driving segments.
// Uses GearRuns (raw frame transitions) for sub-clip precision when available.
// Falls back to clip-level heuristic for legacy data without GearRuns.
func splitByGearState(group []timedRoute) [][]timedRoute {
	if len(group) == 0 {
		return nil
	}

	// Check if any clip has gear run data (new format)
	hasGearRuns := false
	for _, clip := range group {
		if len(clip.GearRuns) > 0 {
			hasGearRuns = true
			break
		}
	}
	if !hasGearRuns {
		return splitByGearStateLegacy(group)
	}

	// Sub-clip splitting: walk through each clip's gear runs and split at
	// Park gaps that exceed the threshold.
	var result [][]timedRoute
	var current []timedRoute

	for _, clip := range group {
		if len(clip.GearRuns) == 0 {
			// No gear data for this clip — include it in the current drive
			current = append(current, clip)
			continue
		}

		segments := splitClipAtParkGaps(clip)
		for _, seg := range segments {
			if seg.parked {
				// Park boundary — finalize the current drive
				if len(current) > 0 {
					result = append(result, current)
					current = nil
				}
			} else if len(seg.route.Points) > 0 {
				current = append(current, seg.route)
			}
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

// clipSegment represents a portion of a clip — either a driving segment
// (with route data) or a park boundary marker.
type clipSegment struct {
	route  timedRoute
	parked bool
}

// splitClipAtParkGaps analyses a clip's GearRuns and splits its deduped points
// at any Park gap >= parkGapSeconds. Returns one or more segments.
func splitClipAtParkGaps(clip timedRoute) []clipSegment {
	totalRawFrames := 0
	for _, run := range clip.GearRuns {
		totalRawFrames += run.Frames
	}
	if totalRawFrames == 0 {
		return []clipSegment{{route: clip, parked: false}}
	}

	secondsPerFrame := 60.0 / float64(totalRawFrames)
	nPoints := len(clip.Points)

	// Identify which raw segments are park gaps
	type rawSeg struct {
		startFrame int
		endFrame   int // exclusive
		parked     bool
	}
	var rawSegs []rawSeg
	frame := 0
	for _, run := range clip.GearRuns {
		duration := float64(run.Frames) * secondsPerFrame
		isParkGap := run.Gear == GearPark && duration >= parkGapSeconds
		rawSegs = append(rawSegs, rawSeg{
			startFrame: frame,
			endFrame:   frame + run.Frames,
			parked:     isParkGap,
		})
		frame += run.Frames
	}

	// Merge consecutive non-parked segments
	var merged []rawSeg
	for _, seg := range rawSegs {
		if len(merged) > 0 && !merged[len(merged)-1].parked && !seg.parked {
			merged[len(merged)-1].endFrame = seg.endFrame
		} else {
			merged = append(merged, seg)
		}
	}

	// Check if any split is needed
	hasParkGap := false
	for _, seg := range merged {
		if seg.parked {
			hasParkGap = true
			break
		}
	}
	if !hasParkGap {
		return []clipSegment{{route: clip, parked: false}}
	}

	// Map raw frame ranges to deduped point indices and build segments
	var result []clipSegment
	for _, seg := range merged {
		if seg.parked {
			result = append(result, clipSegment{parked: true})
			continue
		}

		// Map frame range → point range (approximate linear interpolation)
		startFrac := float64(seg.startFrame) / float64(totalRawFrames)
		endFrac := float64(seg.endFrame) / float64(totalRawFrames)

		startIdx := int(math.Round(startFrac * float64(nPoints)))
		endIdx := int(math.Round(endFrac * float64(nPoints)))

		if startIdx >= nPoints {
			startIdx = nPoints - 1
		}
		if endIdx > nPoints {
			endIdx = nPoints
		}
		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx <= startIdx {
			continue
		}

		// Use slice windows instead of copying — all consumers
		// (GroupSummaries, BuildSingleDrive, etc.) only read point data.
		// This avoids duplicating potentially hundreds of MB of GPS data
		// during grouping, which is critical on memory-constrained Pis.
		segPoints := clip.Points[startIdx:endIdx:endIdx]

		var segGears []uint8
		if len(clip.GearStates) >= endIdx {
			segGears = clip.GearStates[startIdx:endIdx:endIdx]
		}

		var segAP []uint8
		if len(clip.AutopilotStates) >= endIdx {
			segAP = clip.AutopilotStates[startIdx:endIdx:endIdx]
		}

		var segSpeeds []float32
		if len(clip.Speeds) >= endIdx {
			segSpeeds = clip.Speeds[startIdx:endIdx:endIdx]
		}

		var segAccel []float32
		if len(clip.AccelPositions) >= endIdx {
			segAccel = clip.AccelPositions[startIdx:endIdx:endIdx]
		}

		// Compute timestamp offset for this segment within the clip
		offsetDuration := time.Duration(startFrac * float64(60*time.Second))

		result = append(result, clipSegment{
			route: timedRoute{
				Route: Route{
					File:            clip.File,
					Date:            clip.Date,
					Points:          segPoints,
					GearStates:      segGears,
					AutopilotStates: segAP,
					Speeds:          segSpeeds,
					AccelPositions:  segAccel,
				},
				timestamp: clip.timestamp.Add(offsetDuration),
			},
			parked: false,
		})
	}

	return result
}

// splitByGearStateLegacy is the fallback for routes processed before GearRuns
// were stored. Uses clip-level heuristic: clips that are majority Park are
// treated as drive boundaries.
func splitByGearStateLegacy(group []timedRoute) [][]timedRoute {
	if len(group) <= 1 {
		return [][]timedRoute{group}
	}

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
		if clipIsMostlyParkedLegacy(clip) {
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

	if len(result) == 0 {
		return [][]timedRoute{group}
	}
	return result
}

// clipIsMostlyParkedLegacy returns true if the clip is majority Park.
// Used only for legacy routes without GearRuns.
func clipIsMostlyParkedLegacy(clip timedRoute) bool {
	if clip.RawFrameCount > 0 {
		return float64(clip.RawParkCount)/float64(clip.RawFrameCount) > 0.5
	}
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
// This computes stats directly from the raw clips without ever merging all
// points into a single array — using a fraction of the memory of GroupIntoDrives.
func GroupSummaries(routes []Route) []DriveSummary {
	groups := groupClips(routes)
	summaries := make([]DriveSummary, len(groups))

	for idx, clips := range groups {
		firstClip := clips[0]
		lastClip := clips[len(clips)-1]
		startTime := firstClip.timestamp
		endTime := lastClip.timestamp.Add(time.Minute)
		durationMs := endTime.Sub(startTime).Milliseconds()

		var totalDistM, maxSpeedMps float64
		var speedSum float64
		var speedCount int
		var pointCount int
		var fsdEngagedMs, autosteerEngagedMs, taccEngagedMs int64
		var fsdDistM, autosteerDistM, taccDistM, assistedDistM float64
		var fsdDisengagements, fsdAccelPushes int
		var startPoint, endPoint *[2]float64

		// First pass: compute median location from the middle 50% of valid
		// points across all clips (mirrors buildDriveStats outlier filter).
		var validLats, validLngs []float64
		for _, clip := range clips {
			for _, p := range clip.Points {
				if !(math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1) {
					validLats = append(validLats, p[0])
					validLngs = append(validLngs, p[1])
				}
			}
		}
		var medLat, medLng float64
		hasMedian := len(validLats) > 2
		if hasMedian {
			q1 := len(validLats) / 4
			q3 := len(validLats) * 3 / 4
			var sumLat, sumLng float64
			count := 0
			for i := q1; i <= q3; i++ {
				sumLat += validLats[i]
				sumLng += validLngs[i]
				count++
			}
			medLat = sumLat / float64(count)
			medLng = sumLng / float64(count)
		}
		// Free the temp slices
		validLats = nil
		validLngs = nil

		// Second pass: compute stats, filtering outliers per-clip
		for _, clip := range clips {
			n := len(clip.Points)
			if n == 0 {
				continue
			}

			// Build a validity mask for this clip's points:
			// exclude null-island, median-cluster outliers (>1000km),
			// and isolated neighbor-jump outliers (>5km from both neighbors).
			const maxFromMedianM = 1000000.0
			const maxJumpM = 5000.0
			valid := make([]bool, n)
			for i, p := range clip.Points {
				if math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1 {
					continue // null island
				}
				if hasMedian && haversineM(p[0], p[1], medLat, medLng) > maxFromMedianM {
					continue // too far from median cluster
				}
				valid[i] = true
			}
			// Neighbor-jump filter: remove points far from both neighbors
			if n > 2 {
				for i := range valid {
					if !valid[i] {
						continue
					}
					hasPrev := i > 0 && valid[i-1]
					hasNext := i < n-1 && valid[i+1]
					farFromPrev := hasPrev && haversineM(clip.Points[i-1][0], clip.Points[i-1][1], clip.Points[i][0], clip.Points[i][1]) > maxJumpM
					farFromNext := hasNext && haversineM(clip.Points[i][0], clip.Points[i][1], clip.Points[i+1][0], clip.Points[i+1][1]) > maxJumpM
					if (hasPrev && hasNext && farFromPrev && farFromNext) ||
						(!hasPrev && farFromNext) ||
						(!hasNext && farFromPrev) {
						valid[i] = false
					}
				}
			}

			// Track start/end points (first valid point of first clip, last of last)
			if startPoint == nil {
				for i := 0; i < n; i++ {
					if valid[i] {
						sp := [2]float64{clip.Points[i][0], clip.Points[i][1]}
						startPoint = &sp
						break
					}
				}
			}
			for i := n - 1; i >= 0; i-- {
				if valid[i] {
					ep := [2]float64{clip.Points[i][0], clip.Points[i][1]}
					endPoint = &ep
					break
				}
			}

			for i := 0; i < n; i++ {
				if valid[i] {
					pointCount++
				}
			}
			clipDurationMs := float64(60000)
			hasAP := len(clip.AutopilotStates) == n
			hasGears := len(clip.GearStates) == n
			hasAccel := len(clip.AccelPositions) == n
			hasSpeeds := len(clip.Speeds) == n
			hasSEISpeeds := false
			if hasSpeeds {
				for _, sp := range clip.Speeds {
					if sp > 0 {
						hasSEISpeeds = true
						break
					}
				}
			}

			// Per-clip FSD event tracking state
			var inAccelPress bool
			var fsdEngageIdx int = -1 // point index where FSD was last engaged
			var pendingDisengage bool
			var pendingDisengageIdx int

			for i := 1; i < n; i++ {
				// Skip pairs where either point is an outlier
				if !valid[i] || !valid[i-1] {
					continue
				}
				d := haversineM(clip.Points[i-1][0], clip.Points[i-1][1], clip.Points[i][0], clip.Points[i][1])

				// Skip GPS teleportation artifacts
				if !hasSEISpeeds {
					dtSec := (clipDurationMs / float64(n-1)) / 1000.0
					if dtSec > 0 && d/dtSec > 70 {
						continue
					}
				}

				totalDistM += d
				dtMs := clipDurationMs / float64(n-1)

				// Speed
				if hasSEISpeeds {
					speed := float64(clip.Speeds[i])
					if speed >= 0 && speed < 100 {
						speedSum += speed
						speedCount++
						if speed > maxSpeedMps {
							maxSpeedMps = speed
						}
					}
				} else if dtSec := dtMs / 1000.0; dtSec > 0 {
					speed := d / dtSec
					if speed < 70 {
						speedSum += speed
						speedCount++
						if speed > maxSpeedMps {
							maxSpeedMps = speed
						}
					}
				}

				// Autopilot stats
				if hasAP {
					curAP := clip.AutopilotStates[i]
					prevAP := clip.AutopilotStates[i-1]

					if curAP != AutopilotOff {
						assistedDistM += d
						switch curAP {
						case AutopilotFSD:
							fsdEngagedMs += int64(dtMs)
							fsdDistM += d
						case AutopilotAutosteer:
							autosteerEngagedMs += int64(dtMs)
							autosteerDistM += d
						case AutopilotTACC:
							taccEngagedMs += int64(dtMs)
							taccDistM += d
						}
					}

					// Track FSD engagement start
					if prevAP != AutopilotFSD && curAP == AutopilotFSD {
						fsdEngageIdx = i
						inAccelPress = false
					}

					// Resolve pending disengagement: if Park arrives within
					// 2 seconds, FSD parked the car — not a driver override.
					if pendingDisengage {
						timeSinceMs := float64(i-pendingDisengageIdx) * dtMs
						if hasGears && clip.GearStates[i] == GearPark && timeSinceMs <= 2000.0 {
							pendingDisengage = false
						} else if timeSinceMs > 2000.0 || curAP == AutopilotFSD {
							fsdDisengagements++
							pendingDisengage = false
						}
					}

					// Detect FSD disengagement — defer for Park grace period
					if prevAP == AutopilotFSD && curAP != AutopilotFSD {
						pendingDisengage = true
						pendingDisengageIdx = i
						inAccelPress = false
					}

					// Accel push detection: pedal > 1% while FSD, returns to 0%.
					// Skip presses within 3 seconds of FSD engagement.
					if curAP == AutopilotFSD && hasAccel {
						accelPct := float64(clip.AccelPositions[i])
						if accelPct <= 1.0 {
							accelPct *= 100.0
						}
						timeSinceEngageMs := float64(0)
						if fsdEngageIdx >= 0 {
							timeSinceEngageMs = float64(i-fsdEngageIdx) * dtMs
						}
						if !inAccelPress && accelPct > 1.0 && timeSinceEngageMs >= 3000.0 {
							inAccelPress = true
						} else if inAccelPress && accelPct <= 0.0 {
							fsdAccelPushes++
							inAccelPress = false
						}
					} else if curAP != AutopilotFSD {
						inAccelPress = false
					}
				}
			}

			// Flush pending disengagement at end of clip — if last point is
			// Park, FSD parked the car; otherwise it's a real disengagement.
			if pendingDisengage {
				if !(hasGears && clip.GearStates[n-1] == GearPark) {
					fsdDisengagements++
				}
			}
		}

		var avgSpeedMps float64
		if speedCount > 0 {
			avgSpeedMps = speedSum / float64(speedCount)
		}

		var fsdPercent, autosteerPercent, taccPercent, assistedPercent float64
		if totalDistM > 0 {
			fsdPercent = math.Round(fsdDistM/totalDistM*1000) / 10
			autosteerPercent = math.Round(autosteerDistM/totalDistM*1000) / 10
			taccPercent = math.Round(taccDistM/totalDistM*1000) / 10
			assistedPercent = math.Round(assistedDistM/totalDistM*1000) / 10
		}

		// Propagate provenance from the clips. After splitByExternalSignature
		// every clip in a group shares a signature (or is signature-less SEI).
		source := firstClip.Source
		if source == "" {
			source = "sei"
		}

		summaries[idx] = DriveSummary{
			ID:                     idx,
			Date:                   firstClip.Date,
			StartTime:              startTime.Format("2006-01-02T15:04:05"),
			EndTime:                endTime.Format("2006-01-02T15:04:05"),
			DurationMs:             durationMs,
			DistanceMi:             math.Round(totalDistM/1609.344*100) / 100,
			DistanceKm:             math.Round(totalDistM/1000*100) / 100,
			AvgSpeedMph:            math.Round(avgSpeedMps*2.23694*100) / 100,
			MaxSpeedMph:            math.Round(maxSpeedMps*2.23694*100) / 100,
			AvgSpeedKmh:            math.Round(avgSpeedMps*3.6*100) / 100,
			MaxSpeedKmh:            math.Round(maxSpeedMps*3.6*100) / 100,
			ClipCount:              len(clips),
			PointCount:             pointCount,
			StartPoint:             startPoint,
			EndPoint:               endPoint,
			FSDEngagedMs:           fsdEngagedMs,
			FSDDisengagements:      fsdDisengagements,
			FSDAccelPushes:         fsdAccelPushes,
			FSDPercent:             fsdPercent,
			FSDDistanceKm:          math.Round(fsdDistM/1000*100) / 100,
			FSDDistanceMi:          math.Round(fsdDistM/1609.344*100) / 100,
			AutosteerEngagedMs:     autosteerEngagedMs,
			AutosteerPercent:       autosteerPercent,
			AutosteerDistanceKm:    math.Round(autosteerDistM/1000*100) / 100,
			AutosteerDistanceMi:    math.Round(autosteerDistM/1609.344*100) / 100,
			TACCEngagedMs:          taccEngagedMs,
			TACCPercent:            taccPercent,
			TACCDistanceKm:         math.Round(taccDistM/1000*100) / 100,
			TACCDistanceMi:         math.Round(taccDistM/1609.344*100) / 100,
			AssistedPercent:        assistedPercent,
			Source:                 source,
			TessieAutopilotPercent: firstClip.TessieAutopilotPercent,
		}
	}
	return summaries
}

// RouteOverview is a lightweight per-drive route for the overview map.
// Source lets the renderer color Tessie-imported drives differently
// (purple/dashed) from native dashcam drives (solid blue).
type RouteOverview struct {
	ID     int          `json:"id"`
	Points [][2]float64 `json:"points"`
	Source string       `json:"source,omitempty"`
}

// GroupRoutesOverview returns downsampled route polylines for every drive
// without building full Drive objects. Collects lat/lng directly from the raw
// clips, applies the same outlier filtering as buildDriveStats, and
// downsamples — no merged point arrays allocated.
func GroupRoutesOverview(routes []Route, maxPointsPerDrive int) []RouteOverview {
	groups := groupClips(routes)
	result := make([]RouteOverview, 0, len(groups))

	const maxFromMedianM = 1000000.0
	const maxJumpM = 5000.0

	for idx, clips := range groups {
		// Collect valid (non-null-island) lat/lng from each clip
		var pts [][2]float64
		for _, clip := range clips {
			for _, p := range clip.Points {
				if !(math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1) {
					pts = append(pts, [2]float64{p[0], p[1]})
				}
			}
		}

		// Median-cluster filter: drop points >1000km from median
		if len(pts) > 2 {
			q1 := len(pts) / 4
			q3 := len(pts) * 3 / 4
			var sumLat, sumLng float64
			count := 0
			for i := q1; i <= q3; i++ {
				sumLat += pts[i][0]
				sumLng += pts[i][1]
				count++
			}
			medLat := sumLat / float64(count)
			medLng := sumLng / float64(count)

			n := 0
			for _, p := range pts {
				if haversineM(p[0], p[1], medLat, medLng) <= maxFromMedianM {
					pts[n] = p
					n++
				}
			}
			pts = pts[:n]
		}

		// Neighbor-jump filter: drop points far from both neighbors
		if len(pts) > 2 {
			remove := make([]bool, len(pts))
			for i := range pts {
				hasPrev := i > 0
				hasNext := i < len(pts)-1
				farFromPrev := hasPrev && haversineM(pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1]) > maxJumpM
				farFromNext := hasNext && haversineM(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]) > maxJumpM
				if (hasPrev && hasNext && farFromPrev && farFromNext) ||
					(!hasPrev && farFromNext) ||
					(!hasNext && farFromPrev) {
					remove[i] = true
				}
			}
			n := 0
			for i, p := range pts {
				if !remove[i] {
					pts[n] = p
					n++
				}
			}
			pts = pts[:n]
		}

		// All clips in a group share the same Source post-grouping
		// (splitByExternalSignature buckets by signature so a Tessie drive
		// never mixes with SEI clips inside a single group).
		source := clips[0].Source
		if source == "" {
			source = "sei"
		}

		result = append(result, RouteOverview{
			ID:     idx,
			Points: Downsample(pts, maxPointsPerDrive),
			Source: source,
		})
	}

	return result
}

// BuildRoutesOverviewFromClipSets produces overview polylines whose drive
// IDs match the summary-path grouping. Each element in clipSets is the
// set of normalized file paths for one drive (from AllDriveClipFilesFromSummaries).
// Routes are indexed once, then each drive's files are collected and filtered
// using the same outlier logic as GroupRoutesOverview.
func BuildRoutesOverviewFromClipSets(routes []Route, clipSets []map[string]bool, maxPointsPerDrive int) []RouteOverview {
	routeByFile := make(map[string]*Route, len(routes))
	for i := range routes {
		norm := strings.ReplaceAll(routes[i].File, "\\", "/")
		routeByFile[norm] = &routes[i]
	}

	const maxFromMedianM = 1000000.0
	const maxJumpM = 5000.0

	result := make([]RouteOverview, 0, len(clipSets))
	for idx, files := range clipSets {
		// Iterating `files` directly is non-deterministic (Go map order),
		// which jumbles per-clip point segments within a drive and produces
		// straight-line "teleportation" between non-adjacent clip endpoints
		// in the overview polyline. Sort matched routes by file timestamp
		// so points concatenate in chronological order, matching what
		// BuildDriveFromFiles does for the single-drive endpoint.
		var timed []timedRoute
		var untimed []*Route
		source := "sei"
		for f := range files {
			r := routeByFile[f]
			if r == nil {
				continue
			}
			if r.Source != "" {
				source = r.Source
			}
			if t := parseFileTimestamp(r.File); !t.IsZero() {
				timed = append(timed, timedRoute{Route: *r, timestamp: t})
			} else {
				untimed = append(untimed, r)
			}
		}
		sort.Slice(timed, func(i, j int) bool {
			return timed[i].timestamp.Before(timed[j].timestamp)
		})

		var pts [][2]float64
		appendPoints := func(rPoints []GPSPoint) {
			for _, p := range rPoints {
				if !(math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1) {
					pts = append(pts, [2]float64{p[0], p[1]})
				}
			}
		}
		for _, tr := range timed {
			appendPoints(tr.Points)
		}
		for _, r := range untimed {
			appendPoints(r.Points)
		}

		if len(pts) > 2 {
			q1 := len(pts) / 4
			q3 := len(pts) * 3 / 4
			var sumLat, sumLng float64
			count := 0
			for i := q1; i <= q3; i++ {
				sumLat += pts[i][0]
				sumLng += pts[i][1]
				count++
			}
			medLat := sumLat / float64(count)
			medLng := sumLng / float64(count)
			n := 0
			for _, p := range pts {
				if haversineM(p[0], p[1], medLat, medLng) <= maxFromMedianM {
					pts[n] = p
					n++
				}
			}
			pts = pts[:n]
		}
		if len(pts) > 2 {
			remove := make([]bool, len(pts))
			for i := range pts {
				hasPrev := i > 0
				hasNext := i < len(pts)-1
				farFromPrev := hasPrev && haversineM(pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1]) > maxJumpM
				farFromNext := hasNext && haversineM(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]) > maxJumpM
				if (hasPrev && hasNext && farFromPrev && farFromNext) ||
					(!hasPrev && farFromNext) ||
					(!hasNext && farFromPrev) {
					remove[i] = true
				}
			}
			n := 0
			for i, p := range pts {
				if !remove[i] {
					pts[n] = p
					n++
				}
			}
			pts = pts[:n]
		}

		result = append(result, RouteOverview{
			ID:     idx,
			Points: Downsample(pts, maxPointsPerDrive),
			Source: source,
		})
	}
	return result
}

func buildDriveStats(clips []timedRoute, idx int) Drive {
	firstClip := clips[0]
	lastClip := clips[len(clips)-1]
	startTime := firstClip.timestamp
	endTime := lastClip.timestamp.Add(time.Minute)

	// Merge all points with interpolated timestamps and autopilot state
	type annotatedPoint struct {
		lat, lng float64
		timeMs   float64
		apState  uint8
		gear     uint8
		seiSpeed float32
		accelPos float32
	}
	var allPoints []annotatedPoint

	for _, clip := range clips {
		clipStart := float64(clip.timestamp.UnixMilli())
		n := len(clip.Points)
		clipDurationMs := float64(60000)
		hasAP := len(clip.AutopilotStates) == n
		hasGears := len(clip.GearStates) == n
		hasSpeeds := len(clip.Speeds) == n
		hasAccel := len(clip.AccelPositions) == n
		for i := 0; i < n; i++ {
			var t float64
			if n > 1 {
				t = clipStart + (clipDurationMs * float64(i) / float64(n-1))
			} else {
				t = clipStart
			}
			ap := annotatedPoint{
				lat:    clip.Points[i][0],
				lng:    clip.Points[i][1],
				timeMs: t,
			}
			if hasAP {
				ap.apState = clip.AutopilotStates[i]
			}
			if hasGears {
				ap.gear = clip.GearStates[i]
			}
			if hasSpeeds {
				ap.seiSpeed = clip.Speeds[i]
			}
			if hasAccel {
				ap.accelPos = clip.AccelPositions[i]
			}
			allPoints = append(allPoints, ap)
		}
	}

	// Remove invalid GPS coordinates (near 0,0 "Null Island")
	{
		n := 0
		for _, p := range allPoints {
			if !(math.Abs(p.lat) < 1 && math.Abs(p.lng) < 1) {
				allPoints[n] = p
				n++
			}
		}
		allPoints = allPoints[:n]
	}

	// Filter GPS outliers — points impossibly far from the median cluster or both neighbors
	if len(allPoints) > 2 {
		// Step 1: Find median location from middle 50% of points
		q1 := len(allPoints) / 4
		q3 := len(allPoints) * 3 / 4
		var medLat, medLng float64
		count := 0
		for i := q1; i <= q3; i++ {
			medLat += allPoints[i].lat
			medLng += allPoints[i].lng
			count++
		}
		medLat /= float64(count)
		medLng /= float64(count)

		// Step 2: Remove points >1,000 km from median cluster
		const maxFromMedianM = 1000000.0
		n := 0
		for _, p := range allPoints {
			if haversineM(p.lat, p.lng, medLat, medLng) <= maxFromMedianM {
				allPoints[n] = p
				n++
			}
		}
		allPoints = allPoints[:n]

		// Step 3: Remove isolated outliers far from both neighbors
		const maxJumpM = 5000.0
		remove := make([]bool, len(allPoints))
		for i := range allPoints {
			hasPrev := i > 0
			hasNext := i < len(allPoints)-1
			farFromPrev := hasPrev && haversineM(allPoints[i-1].lat, allPoints[i-1].lng, allPoints[i].lat, allPoints[i].lng) > maxJumpM
			farFromNext := hasNext && haversineM(allPoints[i].lat, allPoints[i].lng, allPoints[i+1].lat, allPoints[i+1].lng) > maxJumpM
			if (hasPrev && hasNext && farFromPrev && farFromNext) ||
				(!hasPrev && farFromNext) ||
				(!hasNext && farFromPrev) {
				remove[i] = true
			}
		}
		n = 0
		for i, p := range allPoints {
			if !remove[i] {
				allPoints[n] = p
				n++
			}
		}
		allPoints = allPoints[:n]
	}

	// Compute distance and speeds
	// Prefer actual vehicle speed sensor data (seiSpeed) over GPS-derived speed.
	var totalDistanceM float64
	var maxSpeedMps float64
	var speeds []float64

	hasSEISpeeds := false
	for _, p := range allPoints {
		if p.seiSpeed > 0 {
			hasSEISpeeds = true
			break
		}
	}

	for i := 1; i < len(allPoints); i++ {
		d := haversineM(allPoints[i-1].lat, allPoints[i-1].lng, allPoints[i].lat, allPoints[i].lng)
		totalDistanceM += d

		if hasSEISpeeds {
			// Use vehicle speedometer reading (m/s) from SEI protobuf
			speed := float64(allPoints[i].seiSpeed)
			if speed >= 0 && speed < 100 {
				speeds = append(speeds, speed)
				if speed > maxSpeedMps {
					maxSpeedMps = speed
				}
			}
		} else {
			// Fallback: derive speed from GPS distance / interpolated time
			dt := (allPoints[i].timeMs - allPoints[i-1].timeMs) / 1000.0
			if dt > 0 {
				speed := d / dt
				if speed < 70 {
					speeds = append(speeds, speed)
					if speed > maxSpeedMps {
						maxSpeedMps = speed
					}
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
	gearStates := make([]int, len(allPoints))
	fsdStates := make([]int, len(allPoints))
	hasFSDData := false
	hasGearData := false
	for i, p := range allPoints {
		var speed float64
		if hasSEISpeeds {
			speed = float64(p.seiSpeed)
		} else if i > 0 {
			d := haversineM(allPoints[i-1].lat, allPoints[i-1].lng, p.lat, p.lng)
			dt := (p.timeMs - allPoints[i-1].timeMs) / 1000.0
			if dt > 0 {
				speed = math.Min(d/dt, 70)
			}
		}
		pointData[i] = [4]float64{p.lat, p.lng, p.timeMs, math.Round(speed*100) / 100}
		gearStates[i] = int(p.gear)
		if p.gear != GearPark {
			hasGearData = true
		}
		fsdStates[i] = int(p.apState)
		if p.apState != AutopilotOff {
			hasFSDData = true
		}
	}

	// Compute autopilot analytics — split by mode (FSD=1, Autosteer=2, TACC=3)
	var fsdEngagedMs int64
	var fsdDisengagements int
	var fsdAccelPushes int
	var fsdDistanceM float64
	var autosteerEngagedMs int64
	var autosteerDistanceM float64
	var taccEngagedMs int64
	var taccDistanceM float64
	var assistedDistanceM float64
	var fsdEvents []FSDEvent

	if hasFSDData && len(allPoints) > 1 {
		// Track engagement transitions.
		//
		// Disengagement: transition from FSD engaged (state=1) to off (0), BUT not counted
		// if the car enters Park within 2 seconds — that means FSD completed a
		// parking maneuver (AutoPark / Smart Summon) and wasn't overridden by the driver.
		//
		// Accel press: pedal position rises above 1% while FSD is active, then
		// returns to 0%. Tesla does not record FSD-commanded pedal input, so any
		// reading > 0% while autopilot is active is always the human driver.
		// Disengagements and accel pushes are only tracked for FSD (state=1).
		var inAccelPress bool
		var accelPressLat, accelPressLng float64
		var fsdEngageTimeMs float64 // timestamp when FSD was last engaged (for grace period)

		var pendingDisengage bool    // a disengagement is waiting for the 2-second Park check
		var pendingDisengageTimeMs float64
		var pendingDisengageLat, pendingDisengageLng float64

		for i := 1; i < len(allPoints); i++ {
			prev := allPoints[i-1]
			cur := allPoints[i]
			dt := cur.timeMs - prev.timeMs
			d := haversineM(prev.lat, prev.lng, cur.lat, cur.lng)

			prevFSD := prev.apState == AutopilotFSD
			curFSD := cur.apState == AutopilotFSD
			curEngaged := cur.apState != AutopilotOff

			// Resolve any pending FSD disengagement
			if pendingDisengage {
				timeSince := cur.timeMs - pendingDisengageTimeMs
				if cur.gear == GearPark && timeSince <= 2000.0 {
					// FSD parked the car — not a driver disengagement
					pendingDisengage = false
				} else if timeSince > 2000.0 || curFSD {
					// 2-second window passed with no Park, or FSD re-engaged — real disengagement
					fsdDisengagements++
					fsdEvents = append(fsdEvents, FSDEvent{Lat: pendingDisengageLat, Lng: pendingDisengageLng, Type: "disengagement"})
					pendingDisengage = false
				}
			}

			// Track FSD engagement start (state=1 only)
			if !prevFSD && curFSD {
				inAccelPress = false
				fsdEngageTimeMs = cur.timeMs
			}

			// Count engaged time and distance by mode
			if curEngaged {
				assistedDistanceM += d
				switch cur.apState {
				case AutopilotFSD:
					fsdEngagedMs += int64(dt)
					fsdDistanceM += d
				case AutopilotAutosteer:
					autosteerEngagedMs += int64(dt)
					autosteerDistanceM += d
				case AutopilotTACC:
					taccEngagedMs += int64(dt)
					taccDistanceM += d
				}
			}

			// Detect FSD disengagement — defer counting until we know if Park follows
			if prevFSD && !curFSD {
				pendingDisengage = true
				pendingDisengageTimeMs = cur.timeMs
				pendingDisengageLat = cur.lat
				pendingDisengageLng = cur.lng
				inAccelPress = false
			}

			// Normalize pedal position to 0-100% range.
			// Tesla firmware may encode as 0-1 or 0-100 depending on version.
			accelPct := float64(cur.accelPos)
			if accelPct <= 1.0 {
				accelPct *= 100.0
			}

			// Detect start of a human accelerator press while FSD is active (state=1 only).
			// Skip presses within 3 seconds of FSD engagement — the driver's
			// foot is often still on the pedal when they activate autopilot.
			if curFSD && !inAccelPress && accelPct > 1.0 && (cur.timeMs-fsdEngageTimeMs) >= 3000.0 {
				inAccelPress = true
				accelPressLat = cur.lat
				accelPressLng = cur.lng
			}

			// Press is complete when pedal returns to 0%
			if inAccelPress && accelPct <= 0.0 {
				fsdAccelPushes++
				fsdEvents = append(fsdEvents, FSDEvent{Lat: accelPressLat, Lng: accelPressLng, Type: "accel_push"})
				inAccelPress = false
			}
		}

		// Flush any FSD disengagement still pending at the end of the drive.
		// If the last point is Park the car was parked by FSD; otherwise count it.
		if pendingDisengage && len(allPoints) > 0 {
			if allPoints[len(allPoints)-1].gear != GearPark {
				fsdDisengagements++
				fsdEvents = append(fsdEvents, FSDEvent{Lat: pendingDisengageLat, Lng: pendingDisengageLng, Type: "disengagement"})
			}
		}
	}

	durationMs := endTime.Sub(startTime).Milliseconds()
	var fsdPercent, autosteerPercent, taccPercent, assistedPercent float64
	if totalDistanceM > 0 {
		fsdPercent = math.Round(fsdDistanceM/totalDistanceM*1000) / 10
		autosteerPercent = math.Round(autosteerDistanceM/totalDistanceM*1000) / 10
		taccPercent = math.Round(taccDistanceM/totalDistanceM*1000) / 10
		assistedPercent = math.Round(assistedDistanceM/totalDistanceM*1000) / 10
	}

	var gearStateResult []int
	if hasGearData {
		gearStateResult = gearStates
	}
	var fsdStateResult []int
	if hasFSDData {
		fsdStateResult = fsdStates
	}

	return Drive{
		ID:                idx,
		Date:              firstClip.Date,
		StartTime:         startTime.Format("2006-01-02T15:04:05"),
		EndTime:           endTime.Format("2006-01-02T15:04:05"),
		DurationMs:        durationMs,
		DistanceMi:        math.Round(totalDistanceM/1609.344*100) / 100,
		DistanceKm:        math.Round(totalDistanceM/1000*100) / 100,
		AvgSpeedMph:       math.Round(avgSpeedMps*2.23694*100) / 100,
		MaxSpeedMph:       math.Round(maxSpeedMps*2.23694*100) / 100,
		AvgSpeedKmh:       math.Round(avgSpeedMps*3.6*100) / 100,
		MaxSpeedKmh:       math.Round(maxSpeedMps*3.6*100) / 100,
		ClipCount:         len(clips),
		PointCount:        len(allPoints),
		Points:            pointData,
		GearStates:        gearStateResult,
		FSDStates:         fsdStateResult,
		FSDEvents:         fsdEvents,
		FSDEngagedMs:      fsdEngagedMs,
		FSDDisengagements: fsdDisengagements,
		FSDAccelPushes:    fsdAccelPushes,
		FSDPercent:        fsdPercent,
		FSDDistanceKm:       math.Round(fsdDistanceM/1000*100) / 100,
		FSDDistanceMi:       math.Round(fsdDistanceM/1609.344*100) / 100,
		AutosteerEngagedMs:  autosteerEngagedMs,
		AutosteerPercent:    autosteerPercent,
		AutosteerDistanceKm: math.Round(autosteerDistanceM/1000*100) / 100,
		AutosteerDistanceMi: math.Round(autosteerDistanceM/1609.344*100) / 100,
		TACCEngagedMs:       taccEngagedMs,
		TACCPercent:         taccPercent,
		TACCDistanceKm:      math.Round(taccDistanceM/1000*100) / 100,
		TACCDistanceMi:      math.Round(taccDistanceM/1609.344*100) / 100,
		AssistedPercent:     assistedPercent,
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

// routeTimestamp pairs a parsed timestamp with its index into the routes slice.
// Used by lightweight aggregate stats to avoid copying route data.
type routeTimestamp struct {
	ts  time.Time
	idx int
}

// AggregateStats holds computed aggregate statistics across all drives.
type AggregateStats struct {
	DrivesCount          int
	RoutesCount          int
	TotalDistanceKm      float64
	TotalDistanceMi      float64
	TotalDurationMs      int64
	FSDEngagedMs         int64
	FSDDistanceKm        float64
	FSDDistanceMi        float64
	FSDPercent           float64
	FSDDisengagements    int
	FSDAccelPushes       int
	AutosteerEngagedMs   int64
	AutosteerDistanceKm  float64
	AutosteerDistanceMi  float64
	TACCEngagedMs        int64
	TACCDistanceKm       float64
	TACCDistanceMi       float64
	AssistedPercent      float64
}

// ComputeAggregateStats computes aggregate statistics from drive summaries
// without needing full point arrays in memory.
func ComputeAggregateStats(summaries []DriveSummary) AggregateStats {
	var s AggregateStats
	s.DrivesCount = len(summaries)
	for _, d := range summaries {
		s.TotalDistanceKm += d.DistanceKm
		s.TotalDistanceMi += d.DistanceMi
		s.TotalDurationMs += d.DurationMs
		s.FSDEngagedMs += d.FSDEngagedMs
		s.FSDDistanceKm += d.FSDDistanceKm
		s.FSDDistanceMi += d.FSDDistanceMi
		s.FSDDisengagements += d.FSDDisengagements
		s.FSDAccelPushes += d.FSDAccelPushes
		s.AutosteerEngagedMs += d.AutosteerEngagedMs
		s.AutosteerDistanceKm += d.AutosteerDistanceKm
		s.AutosteerDistanceMi += d.AutosteerDistanceMi
		s.TACCEngagedMs += d.TACCEngagedMs
		s.TACCDistanceKm += d.TACCDistanceKm
		s.TACCDistanceMi += d.TACCDistanceMi
	}
	if s.TotalDistanceKm > 0 {
		s.FSDPercent = math.Round(s.FSDDistanceKm/s.TotalDistanceKm*1000) / 10
		totalAssistedKm := s.FSDDistanceKm + s.AutosteerDistanceKm + s.TACCDistanceKm
		s.AssistedPercent = math.Round(totalAssistedKm/s.TotalDistanceKm*1000) / 10
	}
	return s
}

// ComputeAggregateStatsFromRoutes computes aggregate statistics directly from
// routes WITHOUT calling GroupIntoDrives. This avoids the massive memory spike
// of building full Drive objects with merged point arrays — critical for 1GB Pi
// devices where GroupIntoDrives can trigger the OOM killer.
//
// Drive count is determined by a lightweight timestamp-gap grouping (no point
// data copied). Distance, duration, and autopilot stats are computed per-route
// and summed.
func ComputeAggregateStatsFromRoutes(routes []Route) AggregateStats {
	var s AggregateStats

	if len(routes) == 0 {
		return s
	}

	// Deduplicate by normalized file path (same as GroupIntoDrives)
	seen := make(map[string]bool, len(routes))
	var timed []routeTimestamp
	for i, r := range routes {
		norm := strings.ReplaceAll(r.File, "\\", "/")
		if seen[norm] {
			continue
		}
		seen[norm] = true
		if t := parseFileTimestamp(r.File); !t.IsZero() {
			timed = append(timed, routeTimestamp{ts: t, idx: i})
		}
	}
	sort.Slice(timed, func(i, j int) bool {
		return timed[i].ts.Before(timed[j].ts)
	})

	// --- Lightweight drive count + duration via timestamp + gear-state grouping ---
	if len(timed) > 0 {
		groupStart := 0
		for i := 1; i <= len(timed); i++ {
			isEnd := i == len(timed)
			isGap := !isEnd && timed[i].ts.Sub(timed[i-1].ts).Milliseconds() > driveGapMs
			if isEnd || isGap {
				group := timed[groupStart:i]
				// Count gear-based splits within this time group
				s.DrivesCount += countGearSplitsInGroup(routes, group)
				// Duration: first clip start → last clip start + 60s
				s.TotalDurationMs += group[len(group)-1].ts.Add(time.Minute).Sub(group[0].ts).Milliseconds()
				if !isEnd {
					groupStart = i
				}
			}
		}
	}

	// --- Per-route distance and autopilot stats ---
	// totalDistanceM counts EVERY drive (SEI + Tessie) — feeds the
	// "X miles driven" headline. seiDistanceM is the SEI-only denominator
	// for FSD/AP/TACC percentages so Tessie miles don't dilute the score.
	// FSD-related accumulators (FSDEngagedMs, FSDDisengagements, etc.) are
	// only updated for non-Tessie routes — Tessie's per-point autopilot
	// signal is fuzzier than dashcam SEI and would skew the score.
	var totalDistanceM, seiDistanceM float64
	var totalFSDDistM, totalAutosteerDistM, totalTACCDistM float64

	for ti := range timed {
		r := &routes[timed[ti].idx]
		n := len(r.Points)
		if n < 2 {
			continue
		}
		isTessie := r.Source == "tessie"

		clipDurationMs := float64(60000)
		clipStartMs := float64(timed[ti].ts.UnixMilli())
		hasAP := len(r.AutopilotStates) == n
		hasGears := len(r.GearStates) == n
		hasAccel := len(r.AccelPositions) == n
		hasSEISpeeds := false
		if len(r.Speeds) == n {
			for _, sp := range r.Speeds {
				if sp > 0 {
					hasSEISpeeds = true
					break
				}
			}
		}

		inAccelPress := false

		for i := 1; i < n; i++ {
			d := haversineM(r.Points[i-1][0], r.Points[i-1][1], r.Points[i][0], r.Points[i][1])

			// Skip GPS teleportation artifacts
			if !hasSEISpeeds {
				dtSec := (clipDurationMs / float64(n-1)) / 1000.0
				if dtSec > 0 && d/dtSec > 70 {
					continue
				}
			}

			totalDistanceM += d
			dtMs := clipDurationMs / float64(n-1)

			// Only SEI clips contribute to FSD denominator and per-mode totals.
			// Tessie distance still counts toward the global mileage above.
			if isTessie {
				continue
			}
			seiDistanceM += d

			if hasAP {
				prevAP := r.AutopilotStates[i-1]
				curAP := r.AutopilotStates[i]

				switch curAP {
				case AutopilotFSD:
					s.FSDEngagedMs += int64(dtMs)
					totalFSDDistM += d
				case AutopilotAutosteer:
					s.AutosteerEngagedMs += int64(dtMs)
					totalAutosteerDistM += d
				case AutopilotTACC:
					s.TACCEngagedMs += int64(dtMs)
					totalTACCDistM += d
				}

				// FSD disengagement: FSD → non-FSD
				if prevAP == AutopilotFSD && curAP != AutopilotFSD {
					skipDisengage := false
					if hasGears {
						tCur := clipStartMs + (clipDurationMs * float64(i) / float64(n-1))
						for j := i; j < n; j++ {
							tJ := clipStartMs + (clipDurationMs * float64(j) / float64(n-1))
							if (tJ - tCur) > 2000.0 {
								break
							}
							if r.GearStates[j] == GearPark {
								skipDisengage = true
								break
							}
						}
					}
					if !skipDisengage {
						s.FSDDisengagements++
					}
					inAccelPress = false
				}

				// FSD accel push detection
				if curAP == AutopilotFSD && hasAccel {
					accelPct := float64(r.AccelPositions[i])
					if accelPct <= 1.0 {
						accelPct *= 100.0
					}
					if !inAccelPress && accelPct > 1.0 {
						inAccelPress = true
					} else if inAccelPress && accelPct <= 0.0 {
						s.FSDAccelPushes++
						inAccelPress = false
					}
				} else if curAP != AutopilotFSD {
					inAccelPress = false
				}
			}
		}
	}

	s.TotalDistanceKm = totalDistanceM / 1000.0
	s.TotalDistanceMi = totalDistanceM / 1609.344
	s.FSDDistanceKm = totalFSDDistM / 1000.0
	s.FSDDistanceMi = totalFSDDistM / 1609.344
	s.AutosteerDistanceKm = totalAutosteerDistM / 1000.0
	s.AutosteerDistanceMi = totalAutosteerDistM / 1609.344
	s.TACCDistanceKm = totalTACCDistM / 1000.0
	s.TACCDistanceMi = totalTACCDistM / 1609.344

	// Percentages use the SEI-only denominator so importing Tessie
	// drives doesn't artificially deflate the FSD score.
	if seiDistanceM > 0 {
		s.FSDPercent = math.Round(totalFSDDistM/seiDistanceM*1000) / 10
		totalAssistedM := totalFSDDistM + totalAutosteerDistM + totalTACCDistM
		s.AssistedPercent = math.Round(totalAssistedM/seiDistanceM*1000) / 10
	}

	return s
}

// countGearSplitsInGroup counts how many drives result from gear-state
// splitting within a single time group. Mirrors splitByGearState logic but
// only counts — no Drive objects or point arrays are allocated.
func countGearSplitsInGroup(routes []Route, group []routeTimestamp) int {
	if len(group) == 0 {
		return 0
	}

	hasGearRuns := false
	for _, entry := range group {
		if len(routes[entry.idx].GearRuns) > 0 {
			hasGearRuns = true
			break
		}
	}

	if !hasGearRuns {
		count := 1
		prevAllPark := false
		for _, entry := range group {
			r := &routes[entry.idx]
			if r.RawFrameCount > 0 && r.RawParkCount > 0 {
				isAllPark := float64(r.RawParkCount)/float64(r.RawFrameCount) > 0.6
				if prevAllPark && !isAllPark {
					count++
				}
				prevAllPark = isAllPark
			} else {
				prevAllPark = false
			}
		}
		return count
	}

	// Mirror splitByGearState: count non-parked segments separated by park
	// gaps. A park run >= parkGapSeconds ends the current drive; a subsequent
	// non-park run starts a new one. All-park groups count as 1 (fallback).
	count := 0
	inDrive := false

	for _, entry := range group {
		r := &routes[entry.idx]
		totalFrames := 0
		for _, run := range r.GearRuns {
			totalFrames += run.Frames
		}
		if totalFrames == 0 {
			// No gear data — treat as driving (same as splitByGearState)
			if !inDrive {
				inDrive = true
				count++
			}
			continue
		}
		secPerFrame := 60.0 / float64(totalFrames)
		for _, run := range r.GearRuns {
			if run.Gear == GearPark {
				duration := float64(run.Frames) * secPerFrame
				if duration >= parkGapSeconds {
					inDrive = false
				}
			} else {
				if !inDrive {
					inDrive = true
					count++
				}
			}
		}
	}

	// If everything was parked, count as 1 (matches splitByGearState fallback)
	if count == 0 {
		return 1
	}
	return count
}
