package drives

import (
	"fmt"
	"math"
	"testing"
)

// buildSyntheticRoutes returns n routes whose filenames carry parseable
// timestamps (so groupClips can time-sort them) and whose parallel
// slices exercise FSD / autosteer / manual. Used by the golden
// equivalence tests below.
func buildSyntheticRoutes(n int) []Route {
	routes := make([]Route, 0, n)
	baseLat := 40.0
	baseLon := -74.0
	for i := 0; i < n; i++ {
		// Give each route a distinct minute-of-hour timestamp so
		// groupClips can parse it. 3 clips then a 10-min gap → 2 drives
		// per cluster of 4 routes — exercises the drive-count math.
		hour := 14 + (i/4)*1
		minute := (i % 4) * 15
		file := fmt.Sprintf("2026-04-20/2026-04-20_%02d-%02d-00-front.mp4",
			hour%24, minute%60)
		date := fmt.Sprintf("2026-04-20_%02d-%02d-00", hour%24, minute%60)

		const pts = 20
		points := make([]GPSPoint, pts)
		ap := make([]uint8, pts)
		gears := make([]uint8, pts)
		for j := 0; j < pts; j++ {
			points[j] = GPSPoint{
				baseLat + float64(i)*0.01 + float64(j)*0.0001,
				baseLon + float64(j)*0.0001,
			}
			gears[j] = 1 // Drive
			// Alternate AP states every couple routes.
			switch i % 3 {
			case 0:
				ap[j] = AutopilotFSD
			case 1:
				ap[j] = AutopilotAutosteer
			default:
				ap[j] = AutopilotOff
			}
		}
		routes = append(routes, Route{
			File:            file,
			Date:            date,
			Points:          points,
			AutopilotStates: ap,
			GearStates:      gears,
			RawFrameCount:   pts,
			GearRuns:        []GearRun{{Gear: 1, Frames: pts}},
		})
	}
	return routes
}

// summariesFromRoutes runs the AddRoute-equivalent compute pass over
// the routes and returns the resulting summaries. Used as a fixture
// builder by the equivalence tests — the real callers get summaries
// via WithRouteSummaries against a live DB.
func summariesFromRoutes(routes []Route) []RouteSummary {
	out := make([]RouteSummary, 0, len(routes))
	for _, r := range routes {
		agg := ComputeRouteAggregates(r)
		out = append(out, RouteSummary{
			File:            r.File,
			Date:            r.Date,
			RawParkCount:    r.RawParkCount,
			RawFrameCount:   r.RawFrameCount,
			GearRuns:        r.GearRuns,
			RouteAggregates: agg,
		})
	}
	return out
}

// TestComputeAggregateStatsFromSummaries_MatchesLegacy is the
// golden-equivalence test: for a dataset where summaries were derived
// from the same Routes, the two implementations must produce
// bit-identical AggregateStats. Locks the refactor.
func TestComputeAggregateStatsFromSummaries_MatchesLegacy(t *testing.T) {
	routes := buildSyntheticRoutes(12)
	summaries := summariesFromRoutes(routes)

	want := ComputeAggregateStatsFromRoutes(routes)
	got := ComputeAggregateStatsFromSummaries(summaries)

	if got.DrivesCount != want.DrivesCount {
		t.Errorf("DrivesCount: got %d want %d", got.DrivesCount, want.DrivesCount)
	}
	if got.RoutesCount != want.RoutesCount {
		t.Errorf("RoutesCount: got %d want %d", got.RoutesCount, want.RoutesCount)
	}
	if math.Abs(got.TotalDistanceKm-want.TotalDistanceKm) > 1e-9 {
		t.Errorf("TotalDistanceKm: got %v want %v",
			got.TotalDistanceKm, want.TotalDistanceKm)
	}
	if math.Abs(got.TotalDistanceMi-want.TotalDistanceMi) > 1e-9 {
		t.Errorf("TotalDistanceMi: got %v want %v",
			got.TotalDistanceMi, want.TotalDistanceMi)
	}
	if got.TotalDurationMs != want.TotalDurationMs {
		t.Errorf("TotalDurationMs: got %d want %d",
			got.TotalDurationMs, want.TotalDurationMs)
	}
	if got.FSDEngagedMs != want.FSDEngagedMs {
		t.Errorf("FSDEngagedMs: got %d want %d",
			got.FSDEngagedMs, want.FSDEngagedMs)
	}
	if math.Abs(got.FSDDistanceKm-want.FSDDistanceKm) > 1e-9 {
		t.Errorf("FSDDistanceKm: got %v want %v",
			got.FSDDistanceKm, want.FSDDistanceKm)
	}
	if got.FSDDisengagements != want.FSDDisengagements {
		t.Errorf("FSDDisengagements: got %d want %d",
			got.FSDDisengagements, want.FSDDisengagements)
	}
	if got.AutosteerEngagedMs != want.AutosteerEngagedMs {
		t.Errorf("AutosteerEngagedMs: got %d want %d",
			got.AutosteerEngagedMs, want.AutosteerEngagedMs)
	}
	if math.Abs(got.FSDPercent-want.FSDPercent) > 1e-9 {
		t.Errorf("FSDPercent: got %v want %v", got.FSDPercent, want.FSDPercent)
	}
	if math.Abs(got.AssistedPercent-want.AssistedPercent) > 1e-9 {
		t.Errorf("AssistedPercent: got %v want %v",
			got.AssistedPercent, want.AssistedPercent)
	}
}

func TestComputeAggregateStatsFromSummaries_EmptyInput(t *testing.T) {
	got := ComputeAggregateStatsFromSummaries(nil)
	if got.DrivesCount != 0 || got.TotalDistanceKm != 0 {
		t.Errorf("empty input should produce zero stats, got %+v", got)
	}
}
