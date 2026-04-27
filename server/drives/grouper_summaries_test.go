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

// TestGroupSummariesFromSummaries_MatchesLegacyOnCleanData is the
// equivalence test for the list-view refactor. On a synthetic dataset
// where every clip is either fully-driving or fully-parked (no
// mid-clip Park gaps), GroupSummariesFromSummaries must produce the
// same drives and the same per-drive scalars as GroupSummaries.
// Mid-clip Park splitting IS a known divergence on real noisy data
// (documented in grouper_summaries.go) and is not covered by this test.
func TestGroupSummariesFromSummaries_MatchesLegacyOnCleanData(t *testing.T) {
	routes := buildSyntheticRoutes(8)
	summaries := summariesFromRoutes(routes)

	want := GroupSummaries(routes)
	got := GroupSummariesFromSummaries(summaries)

	if len(got) != len(want) {
		t.Fatalf("drive count: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Errorf("[%d] ID: got %d want %d", i, got[i].ID, want[i].ID)
		}
		if got[i].StartTime != want[i].StartTime {
			t.Errorf("[%d] StartTime: got %q want %q",
				i, got[i].StartTime, want[i].StartTime)
		}
		if got[i].ClipCount != want[i].ClipCount {
			t.Errorf("[%d] ClipCount: got %d want %d",
				i, got[i].ClipCount, want[i].ClipCount)
		}
		if math.Abs(got[i].DistanceKm-want[i].DistanceKm) > 1e-9 {
			t.Errorf("[%d] DistanceKm: got %v want %v",
				i, got[i].DistanceKm, want[i].DistanceKm)
		}
		if got[i].FSDEngagedMs != want[i].FSDEngagedMs {
			t.Errorf("[%d] FSDEngagedMs: got %d want %d",
				i, got[i].FSDEngagedMs, want[i].FSDEngagedMs)
		}
		if got[i].FSDDisengagements != want[i].FSDDisengagements {
			t.Errorf("[%d] FSDDisengagements: got %d want %d",
				i, got[i].FSDDisengagements, want[i].FSDDisengagements)
		}
		if math.Abs(got[i].FSDPercent-want[i].FSDPercent) > 1e-9 {
			t.Errorf("[%d] FSDPercent: got %v want %v",
				i, got[i].FSDPercent, want[i].FSDPercent)
		}
		if math.Abs(got[i].AssistedPercent-want[i].AssistedPercent) > 1e-9 {
			t.Errorf("[%d] AssistedPercent: got %v want %v",
				i, got[i].AssistedPercent, want[i].AssistedPercent)
		}
	}
}

func TestGroupSummariesFromSummaries_EmptyInput(t *testing.T) {
	got := GroupSummariesFromSummaries(nil)
	if len(got) != 0 {
		t.Errorf("empty input should produce 0 drives, got %d", len(got))
	}
}
