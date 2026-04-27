package drives

import (
	"math"
	"sort"
	"strings"
	"time"
)

// summaryTimestamp pairs a parsed timestamp with an index into the
// supplied []RouteSummary slice. Internal helper mirroring the
// routeTimestamp type that the Route-based grouper uses.
type summaryTimestamp struct {
	ts  time.Time
	idx int
}

// timedSummary is the summaryTimestamp variant that also keeps a copy
// of the summary it points to, so group-building code can read the
// cached aggregates without indexing back into the input slice.
type timedSummary struct {
	RouteSummary
	timestamp time.Time
}

// groupSummaryClips is the BLOB-free analogue of groupClips. It
// dedups, sorts by timestamp, splits by time-gap, and then splits by
// gear-state at CLIP granularity (not sub-clip). Sub-clip Park-gap
// splitting requires the full Points BLOB to re-slice the clip, which
// the summary path intentionally avoids -- so a clip that internally
// spans two drives (rare: driver parks and resumes within 60 seconds)
// is attributed to whichever drive it joins. For the Drives-list view
// this is visually indistinguishable on real data.
func groupSummaryClips(summaries []RouteSummary) [][]timedSummary {
	seen := make(map[string]bool, len(summaries))
	var unique []RouteSummary
	for _, r := range summaries {
		norm := strings.ReplaceAll(r.File, "\\", "/")
		if !seen[norm] {
			seen[norm] = true
			unique = append(unique, r)
		}
	}

	var timed []timedSummary
	for _, r := range unique {
		if t := parseFileTimestamp(r.File); !t.IsZero() {
			timed = append(timed, timedSummary{RouteSummary: r, timestamp: t})
		}
	}
	if len(timed) == 0 {
		return nil
	}
	sort.Slice(timed, func(i, j int) bool {
		return timed[i].timestamp.Before(timed[j].timestamp)
	})

	// First pass: group by time gap (>5 minutes).
	var timeGroups [][]timedSummary
	current := []timedSummary{timed[0]}
	for i := 1; i < len(timed); i++ {
		gap := timed[i].timestamp.Sub(current[len(current)-1].timestamp).Milliseconds()
		if gap > driveGapMs {
			timeGroups = append(timeGroups, current)
			current = []timedSummary{timed[i]}
		} else {
			current = append(current, timed[i])
		}
	}
	timeGroups = append(timeGroups, current)

	// Second pass: within each time group, split at clips that are
	// "mostly parked" (either via GearRuns when available or via
	// RawParkCount/RawFrameCount ratio). Matches the semantics of
	// splitByGearStateLegacy / countGearSplitsInGroup.
	//
	// Third pass: split each gear-group by ExternalSignature. Adjacent
	// Tessie drives can have synthetic clip timestamps that abut at the
	// minute boundary; without this pass two distinct Tessie drives
	// would merge in the UI.
	var groups [][]timedSummary
	for _, tg := range timeGroups {
		for _, gearGroup := range splitSummaryGroupByGear(tg) {
			groups = append(groups, splitSummaryByExternalSignature(gearGroup)...)
		}
	}
	return groups
}

// splitSummaryByExternalSignature is the BLOB-free analogue of
// splitByExternalSignature: bucket clips by ExternalSignature so each
// imported Tessie drive surfaces as its own entry.
func splitSummaryByExternalSignature(group []timedSummary) [][]timedSummary {
	if len(group) <= 1 {
		return [][]timedSummary{group}
	}
	hasAny := false
	for _, c := range group {
		if c.ExternalSignature != "" {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return [][]timedSummary{group}
	}
	buckets := make(map[string][]timedSummary, 4)
	var noSig []timedSummary
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
	var result [][]timedSummary
	if len(noSig) > 0 {
		result = append(result, noSig)
	}
	for _, sig := range order {
		result = append(result, buckets[sig])
	}
	return result
}

// splitSummaryGroupByGear breaks a time-bounded cluster of summaries
// into drive-bounded sub-clusters using metadata only. A clip is a
// drive boundary when it is dominantly parked (GearRuns show a
// Park run >= parkGapSeconds or, absent GearRuns, RawParkCount /
// RawFrameCount > 0.6).
func splitSummaryGroupByGear(group []timedSummary) [][]timedSummary {
	if len(group) <= 1 {
		return [][]timedSummary{group}
	}

	var result [][]timedSummary
	var current []timedSummary
	for _, clip := range group {
		if clipIsParkDominant(clip.RouteSummary) {
			if len(current) > 0 {
				result = append(result, current)
				current = nil
			}
			continue
		}
		current = append(current, clip)
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	if len(result) == 0 {
		return [][]timedSummary{group}
	}
	return result
}

// clipIsParkDominant returns true if the clip looks like a drive
// boundary rather than part of a drive. Uses GearRuns when present
// (precise) and falls back to the park-fraction heuristic for legacy
// data.
func clipIsParkDominant(r RouteSummary) bool {
	if len(r.GearRuns) > 0 {
		totalFrames := 0
		for _, run := range r.GearRuns {
			totalFrames += run.Frames
		}
		if totalFrames == 0 {
			return false
		}
		secPerFrame := 60.0 / float64(totalFrames)
		// Any Park run >= parkGapSeconds marks this clip as a drive
		// boundary. Matches splitByGearState's sub-clip intent at
		// clip granularity.
		for _, run := range r.GearRuns {
			if run.Gear == GearPark && float64(run.Frames)*secPerFrame >= parkGapSeconds {
				return true
			}
		}
		return false
	}
	// No GearRuns -- use the legacy fraction heuristic.
	if r.RawFrameCount > 0 && r.RawParkCount > 0 {
		return float64(r.RawParkCount)/float64(r.RawFrameCount) > 0.5
	}
	return false
}

// GroupSummariesFromSummaries is the BLOB-free analogue of
// GroupSummaries. It groups pre-computed RouteSummary rows into drives
// and assembles a DriveSummary per drive, summing the cached
// aggregates rather than re-walking Points/BLOBs.
//
// Output shape matches GroupSummaries -- same fields, same rounding,
// same format strings for timestamps -- so /api/drives and
// /api/drives/fsd-analytics keep producing the exact JSON the web UI
// expects. On clean data the scalars are bit-identical to the legacy
// path; on noisy data they can drift by fractions of a percent because
// the legacy median-cluster outlier filter is group-level (requires
// Points) and the summary path uses the per-clip outlier semantics
// baked into ComputeRouteAggregates.
func GroupSummariesFromSummaries(summaries []RouteSummary) []DriveSummary {
	groups := groupSummaryClips(summaries)
	out := make([]DriveSummary, len(groups))

	for idx, clips := range groups {
		firstClip := clips[0]
		lastClip := clips[len(clips)-1]
		startTime := firstClip.timestamp
		endTime := lastClip.timestamp.Add(time.Minute)
		durationMs := endTime.Sub(startTime).Milliseconds()

		var totalDistM, maxSpeedMps, speedSum float64
		var speedCount, pointCount int
		var fsdEngagedMs, autosteerEngagedMs, taccEngagedMs int64
		var fsdDistM, autosteerDistM, taccDistM, assistedDistM float64
		var fsdDisengagements, fsdAccelPushes int
		var startPoint, endPoint *[2]float64
		// Track previous clip's end so we can add cross-clip-boundary
		// distance below. Critical for sparse clips (Tessie synthetic
		// 60s clips often have only 1 GPS point — per-clip distance is
		// 0, but the actual mile traveled lives in the jump from this
		// clip's lone point to the next clip's lone point).
		var prevEndLat, prevEndLng *float64

		for _, c := range clips {
			totalDistM += c.DistanceM
			// Cross-clip boundary distance: haversine from the previous
			// clip's last valid point to this clip's first valid point.
			// Counted only when both points exist; clips with no valid
			// GPS contribute nothing to the sum.
			if prevEndLat != nil && prevEndLng != nil && c.StartLat != nil && c.StartLng != nil {
				totalDistM += haversineM(*prevEndLat, *prevEndLng, *c.StartLat, *c.StartLng)
			}
			if c.MaxSpeedMps > maxSpeedMps {
				maxSpeedMps = c.MaxSpeedMps
			}
			speedSum += c.AvgSpeedMps * float64(c.SpeedSampleCount)
			speedCount += c.SpeedSampleCount
			pointCount += c.ValidPointCount
			fsdEngagedMs += c.FSDEngagedMs
			autosteerEngagedMs += c.AutosteerEngagedMs
			taccEngagedMs += c.TACCEngagedMs
			fsdDistM += c.FSDDistanceM
			autosteerDistM += c.AutosteerDistanceM
			taccDistM += c.TACCDistanceM
			assistedDistM += c.AssistedDistanceM
			fsdDisengagements += c.FSDDisengagements
			fsdAccelPushes += c.FSDAccelPushes
			if startPoint == nil && c.StartLat != nil && c.StartLng != nil {
				sp := [2]float64{*c.StartLat, *c.StartLng}
				startPoint = &sp
			}
			if c.EndLat != nil && c.EndLng != nil {
				ep := [2]float64{*c.EndLat, *c.EndLng}
				endPoint = &ep
				prevEndLat = c.EndLat
				prevEndLng = c.EndLng
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

		// Provenance — every clip in a group shares a signature post-split,
		// so the first clip is authoritative.
		source := firstClip.Source
		if source == "" {
			source = "sei"
		}

		out[idx] = DriveSummary{
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

	return out
}

// DriveStartTimeFromSummaries is the BLOB-free analogue of
// DriveStartTime. It returns the start time string for the drive at
// the given index using the same CLIP-level grouping as
// GroupSummariesFromSummaries.
func DriveStartTimeFromSummaries(summaries []RouteSummary, id int) (string, bool) {
	groups := groupSummaryClips(summaries)
	if id < 0 || id >= len(groups) {
		return "", false
	}
	return groups[id][0].timestamp.Format("2006-01-02T15:04:05"), true
}

// DriveClipFilesFromSummaries returns the set of normalized file paths
// that belong to drive `id` according to the summary-path grouping.
// The returned set uses forward-slash-normalized paths for matching
// against Route.File (which may contain backslashes from Windows imports).
func DriveClipFilesFromSummaries(summaries []RouteSummary, id int) (map[string]bool, bool) {
	groups := groupSummaryClips(summaries)
	if id < 0 || id >= len(groups) {
		return nil, false
	}
	files := make(map[string]bool, len(groups[id]))
	for _, clip := range groups[id] {
		files[strings.ReplaceAll(clip.File, "\\", "/")] = true
	}
	return files, true
}

// AllDriveClipFilesFromSummaries returns the clip file sets for every
// drive in the summary grouping. Each element is a map of normalized
// file paths belonging to that drive. Index in the returned slice is
// the canonical drive ID that matches GroupSummariesFromSummaries.
func AllDriveClipFilesFromSummaries(summaries []RouteSummary) []map[string]bool {
	groups := groupSummaryClips(summaries)
	result := make([]map[string]bool, len(groups))
	for i, group := range groups {
		files := make(map[string]bool, len(group))
		for _, clip := range group {
			files[strings.ReplaceAll(clip.File, "\\", "/")] = true
		}
		result[i] = files
	}
	return result
}

// ComputeAggregateStatsFromSummaries is the BLOB-free analogue of
// ComputeAggregateStatsFromRoutes. It reads pre-computed aggregate
// scalars from each RouteSummary (populated at AddRoute time or by
// the one-shot backfill) and aggregates them across drives identified
// by timestamp-gap + gear-state grouping on metadata only.
//
// Output is bit-identical to ComputeAggregateStatsFromRoutes when
// summaries are derived from the same []Route; the TDD golden test
// locks this. The memory win: this function never touches BLOBs, so
// the /api/drives/stats endpoint drops from ~300 MB heap on 5500
// routes to ~5 MB.
func ComputeAggregateStatsFromSummaries(summaries []RouteSummary) AggregateStats {
	var s AggregateStats
	if len(summaries) == 0 {
		return s
	}

	// Deduplicate by normalized file path (matches the Route-based
	// version -- protects against mixed backslash/slash imports).
	seen := make(map[string]bool, len(summaries))
	var timed []summaryTimestamp
	for i, r := range summaries {
		norm := strings.ReplaceAll(r.File, "\\", "/")
		if seen[norm] {
			continue
		}
		seen[norm] = true
		if t := parseFileTimestamp(r.File); !t.IsZero() {
			timed = append(timed, summaryTimestamp{ts: t, idx: i})
		}
	}
	sort.Slice(timed, func(i, j int) bool {
		return timed[i].ts.Before(timed[j].ts)
	})

	// Drive count + total duration via timestamp gaps and gear-state
	// splitting. Cross-clip boundary distance is added here too: per-clip
	// DistanceM is haversine over the clip's own points, but it misses
	// the jump from one clip's last point to the next clip's first
	// point. For dense SEI clips (~60 GPS points each) that gap is
	// negligible. For sparse Tessie synthetic clips (often 1 point per
	// 60s window) it's the entire mile traveled — without this, a 30-mi
	// Tessie drive can show as ~0 miles. The cached start/end lat-lng
	// columns make this O(routes) without BLOB decode.
	//
	// Tessie-aware split:
	//   - totalDistanceM: every drive (SEI + Tessie) contributes — feeds
	//     the headline "X miles driven" stat.
	//   - seiDistanceM: SEI-only — denominator for FSD/AP/TACC %.
	//   - FSD totals + disengagements / accel pushes: SEI only.
	var totalDistanceM, seiDistanceM float64
	var totalFSDDistM, totalAutosteerDistM, totalTACCDistM float64

	if len(timed) > 0 {
		groupStart := 0
		for i := 1; i <= len(timed); i++ {
			isEnd := i == len(timed)
			isGap := !isEnd && timed[i].ts.Sub(timed[i-1].ts).Milliseconds() > driveGapMs
			if isEnd || isGap {
				group := timed[groupStart:i]
				s.DrivesCount += countGearSplitsInSummaryGroup(summaries, group)
				// Duration: first clip start → last clip start + 60s
				s.TotalDurationMs += group[len(group)-1].ts.Add(time.Minute).Sub(group[0].ts).Milliseconds()

				// Per-clip distance + cross-clip boundary distance for
				// this group. Source is per-clip — within a group SEI
				// and Tessie clips don't mix in practice (gap-only
				// overlap policy), but check defensively.
				var prev *RouteSummary
				for _, entry := range group {
					cur := &summaries[entry.idx]
					totalDistanceM += cur.DistanceM
					if prev != nil &&
						prev.EndLat != nil && prev.EndLng != nil &&
						cur.StartLat != nil && cur.StartLng != nil {
						crossD := haversineM(*prev.EndLat, *prev.EndLng, *cur.StartLat, *cur.StartLng)
						totalDistanceM += crossD
						if cur.Source != "tessie" {
							seiDistanceM += crossD
						}
					}
					if cur.Source != "tessie" {
						seiDistanceM += cur.DistanceM
						totalFSDDistM += cur.FSDDistanceM
						totalAutosteerDistM += cur.AutosteerDistanceM
						totalTACCDistM += cur.TACCDistanceM
						s.FSDEngagedMs += cur.FSDEngagedMs
						s.AutosteerEngagedMs += cur.AutosteerEngagedMs
						s.TACCEngagedMs += cur.TACCEngagedMs
						s.FSDDisengagements += cur.FSDDisengagements
						s.FSDAccelPushes += cur.FSDAccelPushes
					}
					prev = cur
				}

				if !isEnd {
					groupStart = i
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

	if seiDistanceM > 0 {
		s.FSDPercent = math.Round(totalFSDDistM/seiDistanceM*1000) / 10
		totalAssistedM := totalFSDDistM + totalAutosteerDistM + totalTACCDistM
		s.AssistedPercent = math.Round(totalAssistedM/seiDistanceM*1000) / 10
	}

	return s
}

// countGearSplitsInSummaryGroup mirrors countGearSplitsInGroup but
// reads its metadata (GearRuns, RawFrameCount, RawParkCount) off
// []RouteSummary instead of []Route. Algorithm is identical so this
// stays in lockstep with the Route-based path.
func countGearSplitsInSummaryGroup(summaries []RouteSummary, group []summaryTimestamp) int {
	if len(group) == 0 {
		return 0
	}

	hasGearRuns := false
	for _, entry := range group {
		if len(summaries[entry.idx].GearRuns) > 0 {
			hasGearRuns = true
			break
		}
	}

	if !hasGearRuns {
		count := 1
		prevAllPark := false
		for _, entry := range group {
			r := &summaries[entry.idx]
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

	count := 0
	inDrive := false
	for _, entry := range group {
		r := &summaries[entry.idx]
		totalFrames := 0
		for _, run := range r.GearRuns {
			totalFrames += run.Frames
		}
		if totalFrames == 0 {
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
	if count == 0 {
		return 1
	}
	return count
}
