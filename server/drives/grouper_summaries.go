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
	// splitting. Same algorithm as the Route-based path, just reading
	// metadata fields off RouteSummary instead of Route.
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
				if !isEnd {
					groupStart = i
				}
			}
		}
	}

	// Per-route sums straight out of the cached aggregate columns. No
	// BLOB decoding, no inline haversine math. For each route that
	// contributed to `timed` (dedup + timestamp-parseable), accumulate
	// its pre-computed scalars.
	var totalDistanceM, totalFSDDistM, totalAutosteerDistM, totalTACCDistM float64
	for _, tr := range timed {
		r := &summaries[tr.idx]
		totalDistanceM += r.DistanceM
		totalFSDDistM += r.FSDDistanceM
		totalAutosteerDistM += r.AutosteerDistanceM
		totalTACCDistM += r.TACCDistanceM
		s.FSDEngagedMs += r.FSDEngagedMs
		s.AutosteerEngagedMs += r.AutosteerEngagedMs
		s.TACCEngagedMs += r.TACCEngagedMs
		s.FSDDisengagements += r.FSDDisengagements
		s.FSDAccelPushes += r.FSDAccelPushes
	}

	s.TotalDistanceKm = totalDistanceM / 1000.0
	s.TotalDistanceMi = totalDistanceM / 1609.344
	s.FSDDistanceKm = totalFSDDistM / 1000.0
	s.FSDDistanceMi = totalFSDDistM / 1609.344
	s.AutosteerDistanceKm = totalAutosteerDistM / 1000.0
	s.AutosteerDistanceMi = totalAutosteerDistM / 1609.344
	s.TACCDistanceKm = totalTACCDistM / 1000.0
	s.TACCDistanceMi = totalTACCDistM / 1609.344

	if s.TotalDistanceKm > 0 {
		s.FSDPercent = math.Round(s.FSDDistanceKm/s.TotalDistanceKm*1000) / 10
		totalAssistedKm := s.FSDDistanceKm + s.AutosteerDistanceKm + s.TACCDistanceKm
		s.AssistedPercent = math.Round(totalAssistedKm/s.TotalDistanceKm*1000) / 10
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
