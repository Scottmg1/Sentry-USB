package drives

import (
	"math"
	"testing"
)

// Tests for ComputeRouteAggregates.
//
// These lock the per-clip semantics that AddRoute, backfill, and the
// refactored summary endpoints all depend on. They are property-style
// rather than hand-computed-number checks so the intent of each
// behavior is obvious when a test fails.
//
// The golden "match the legacy inline loop" comparison lives separately
// in TestComputeRouteAggregates_MatchesComputeAggregateStatsFromRoutes
// below: it reuses the existing ComputeAggregateStatsFromRoutes as a
// reference implementation (that function is per-route with the same
// null-island + GPS-teleport semantics, so a Route's aggregates should
// be bit-identical when that function runs on a one-route input).

// straightLineRoute returns a Route with n points spaced ~1 m apart so
// distance math is stable, optional apStates, and a file path containing
// a parseable timestamp. Missing parallel slices are left nil so the
// helper doubles for the "no optional data" variants.
func straightLineRoute(n int, apStates []uint8, gears []uint8, speeds []float32, accel []float32) Route {
	points := make([]GPSPoint, n)
	for i := 0; i < n; i++ {
		// ~1.11m north per step at the equator (0.00001 deg lat).
		points[i] = GPSPoint{40.0 + float64(i)*0.00001, -74.0}
	}
	return Route{
		File:   "2026-04-20/2026-04-20_14-30-00-front.mp4",
		Date:   "2026-04-20_14-30-00",
		Points: points,
		// Only attach parallel slices when they match length — mirrors
		// the `hasAP := len(r.AutopilotStates) == n` guard in
		// ComputeAggregateStatsFromRoutes.
		AutopilotStates: matchOrNilU8(apStates, n),
		GearStates:      matchOrNilU8(gears, n),
		Speeds:          matchOrNilF32(speeds, n),
		AccelPositions:  matchOrNilF32(accel, n),
	}
}

func matchOrNilU8(v []uint8, n int) []uint8 {
	if len(v) != n {
		return nil
	}
	return v
}
func matchOrNilF32(v []float32, n int) []float32 {
	if len(v) != n {
		return nil
	}
	return v
}

func TestComputeRouteAggregates_EmptyRoute(t *testing.T) {
	agg := ComputeRouteAggregates(Route{File: "empty.mp4"})
	if agg.DistanceM != 0 || agg.ValidPointCount != 0 {
		t.Errorf("empty route should produce zero aggregates, got %+v", agg)
	}
	if agg.StartLat != nil || agg.EndLat != nil {
		t.Error("empty route should have nil Start/End points")
	}
}

func TestComputeRouteAggregates_SinglePoint(t *testing.T) {
	// One point can't produce a distance (needs a pair) but it IS a
	// valid start+end. The inline loop's `for i := 1; i < n; i++` pairs
	// skip this case for distance but start/end tracking should still
	// pick it up.
	agg := ComputeRouteAggregates(straightLineRoute(1, nil, nil, nil, nil))
	if agg.DistanceM != 0 {
		t.Errorf("single point: DistanceM=%v, want 0", agg.DistanceM)
	}
	if agg.StartLat == nil || agg.EndLat == nil {
		t.Fatal("single point: Start/End should be set")
	}
	if *agg.StartLat != 40.0 || *agg.StartLng != -74.0 {
		t.Errorf("single point Start: got (%v,%v)", *agg.StartLat, *agg.StartLng)
	}
	if *agg.EndLat != 40.0 {
		t.Errorf("single point End: got (%v,%v)", *agg.EndLat, *agg.EndLng)
	}
}

func TestComputeRouteAggregates_DistanceAccumulates(t *testing.T) {
	// 6 points ~1m apart → 5 pairs → ~5m total. The exact number depends
	// on haversine with Earth R=6371km so we bracket it rather than
	// assert equality.
	agg := ComputeRouteAggregates(straightLineRoute(6, nil, nil, nil, nil))
	if agg.DistanceM < 4.5 || agg.DistanceM > 6.5 {
		t.Errorf("DistanceM = %v, want ~5m", agg.DistanceM)
	}
	if agg.ValidPointCount != 6 {
		t.Errorf("ValidPointCount = %d, want 6", agg.ValidPointCount)
	}
}

func TestComputeRouteAggregates_NullIslandFiltered(t *testing.T) {
	// Mix of null-island (0,0) and real points — null-island is dropped
	// from the pair loop just like ComputeAggregateStatsFromRoutes does.
	r := Route{
		File:   "2026-04-20/2026-04-20_14-30-00-front.mp4",
		Points: []GPSPoint{{0, 0}, {40.0, -74.0}, {40.00001, -74.0}, {0, 0}},
	}
	agg := ComputeRouteAggregates(r)
	// ValidPointCount counts non-null-island points that survive the
	// filter — 2 here. DistanceM covers the pair between the two real
	// points only.
	if agg.ValidPointCount != 2 {
		t.Errorf("ValidPointCount = %d, want 2 (two real points)", agg.ValidPointCount)
	}
	if agg.DistanceM < 0.5 || agg.DistanceM > 2.0 {
		t.Errorf("DistanceM = %v, want ~1m for 1 valid pair", agg.DistanceM)
	}
	// Start/End must be the first and last non-null-island points.
	if agg.StartLat == nil || *agg.StartLat != 40.0 {
		t.Errorf("StartLat should be 40.0, got %v", agg.StartLat)
	}
	if agg.EndLat == nil || math.Abs(*agg.EndLat-40.00001) > 1e-9 {
		t.Errorf("EndLat should be 40.00001, got %v", agg.EndLat)
	}
}

func TestComputeRouteAggregates_FSDEngagedDistance(t *testing.T) {
	// 6 points: first 3 with FSD on, last 3 with FSD off. Expect
	// fsd_distance_m ~= half of total distance (2 of 5 pairs are FSD-FSD;
	// actually the inner loop credits a pair's distance to cur.apState,
	// so pairs where cur == FSD count as FSD — that's pairs 1,2 here).
	ap := []uint8{1, 1, 1, 0, 0, 0}
	gears := []uint8{1, 1, 1, 1, 1, 1} // all drive, no Park → no grace skip
	agg := ComputeRouteAggregates(straightLineRoute(6, ap, gears, nil, nil))
	if agg.FSDDistanceM <= 0 {
		t.Errorf("FSDDistanceM should be > 0, got %v", agg.FSDDistanceM)
	}
	if agg.FSDDistanceM >= agg.DistanceM {
		t.Errorf("FSDDistanceM (%v) should be < total DistanceM (%v)",
			agg.FSDDistanceM, agg.DistanceM)
	}
	if agg.FSDEngagedMs <= 0 {
		t.Errorf("FSDEngagedMs should be > 0, got %v", agg.FSDEngagedMs)
	}
	// Exactly one disengagement: apState goes 1→0 at index 3, no Park
	// follows within 2s, so it counts.
	if agg.FSDDisengagements != 1 {
		t.Errorf("FSDDisengagements = %d, want 1", agg.FSDDisengagements)
	}
}

func TestComputeRouteAggregates_DisengagementParkGraceSkips(t *testing.T) {
	// FSD→Off transition immediately followed by Park within 2s should
	// NOT count as a disengagement (FSD parked the car, driver didn't
	// override). clipDurationMs=60000, n=6 → dt per pair = 12000ms, so
	// the very next point after disengage is 12 seconds later — too
	// late to qualify for grace. Use more points so dt shrinks.
	//
	// With n=60 points, dt = ~1017ms per pair → index 4 is ~4s after
	// index 3. So we need Park to arrive at index 4 or earlier (~1s
	// after disengage). Use 120 points: dt ≈ 504ms, first 4 post-
	// disengage frames fit within 2s.
	const n = 120
	ap := make([]uint8, n)
	gears := make([]uint8, n)
	for i := 0; i < n; i++ {
		if i < n/2 {
			ap[i] = 1      // FSD
			gears[i] = 1   // Drive
		} else {
			ap[i] = 0      // Off
			gears[i] = 0   // Park starts immediately
		}
	}
	agg := ComputeRouteAggregates(straightLineRoute(n, ap, gears, nil, nil))
	if agg.FSDDisengagements != 0 {
		t.Errorf("FSDDisengagements = %d, want 0 (Park within grace)",
			agg.FSDDisengagements)
	}
}

func TestComputeRouteAggregates_AccelPushCounted(t *testing.T) {
	// Clip with a single accel press after the 3-second FSD-engagement
	// grace. clipDurationMs=60000, n=120 → dt ≈ 504ms. We need an
	// explicit 0→1 AP transition for fsdEngageIdx to be set (matching
	// GroupSummaries semantics), then wait past the 3s grace.
	const n = 120
	ap := make([]uint8, n)
	ap[0] = 0   // Manual at frame 0…
	for i := 1; i < n; i++ {
		ap[i] = 1 // …FSD engages at frame 1 → engageIdx = 1
	}
	accel := make([]float32, n)
	// Grace = 3000ms → ~6 frames past engage. Press at frame 20 is safe.
	accel[20] = 0.5 // > 1% after ×100 normalization
	accel[21] = 0.5
	// Index 22 returns to 0 (slice zero value) → press completes.
	agg := ComputeRouteAggregates(straightLineRoute(n, ap, nil, nil, accel))
	if agg.FSDAccelPushes != 1 {
		t.Errorf("FSDAccelPushes = %d, want 1", agg.FSDAccelPushes)
	}
}

func TestComputeRouteAggregates_AccelPushWithinGraceSkipped(t *testing.T) {
	// Same shape as the previous test but the press happens at frame 2,
	// well inside the 3s grace.
	const n = 120
	ap := make([]uint8, n)
	ap[0] = 0
	for i := 1; i < n; i++ {
		ap[i] = 1
	}
	accel := make([]float32, n)
	accel[2] = 0.5
	accel[3] = 0
	agg := ComputeRouteAggregates(straightLineRoute(n, ap, nil, nil, accel))
	if agg.FSDAccelPushes != 0 {
		t.Errorf("FSDAccelPushes = %d, want 0 (inside grace)", agg.FSDAccelPushes)
	}
}

func TestComputeRouteAggregates_SEISpeedPreferred(t *testing.T) {
	// Two points ~1m apart. SEI speed says 20 m/s; GPS-derived would
	// say ~0.083 m/s (1m / 12000ms). Aggregates should report the
	// SEI value.
	speeds := []float32{0, 20} // SEI on second point
	agg := ComputeRouteAggregates(straightLineRoute(6,
		nil, nil, []float32{0, 20, 20, 20, 20, 20}, nil))
	if math.Abs(agg.MaxSpeedMps-20) > 0.001 {
		t.Errorf("MaxSpeedMps = %v, want 20 (SEI preferred)", agg.MaxSpeedMps)
	}
	_ = speeds
	if agg.SpeedSampleCount < 1 {
		t.Errorf("SpeedSampleCount = %d, want >= 1", agg.SpeedSampleCount)
	}
}

// TestComputeRouteAggregates_MatchesComputeAggregateStatsFromRoutes is
// the golden contract: for any single Route, the per-route stats
// produced by ComputeRouteAggregates must bit-exactly match what
// ComputeAggregateStatsFromRoutes would compute on that same Route
// considered as a one-route input. This locks drift in the refactor.
func TestComputeRouteAggregates_MatchesComputeAggregateStatsFromRoutes(t *testing.T) {
	const n = 60
	ap := make([]uint8, n)
	gears := make([]uint8, n)
	accel := make([]float32, n)
	speeds := make([]float32, n)
	for i := 0; i < n; i++ {
		switch {
		case i < 20:
			ap[i] = 1 // FSD
			gears[i] = 1
			speeds[i] = 25
		case i < 40:
			ap[i] = 2 // Autosteer
			gears[i] = 1
			speeds[i] = 20
		default:
			ap[i] = 0 // Manual
			gears[i] = 1
			speeds[i] = 15
		}
	}
	accel[25] = 0.2 // accel press mid-autosteer (shouldn't count — FSD only)
	accel[10] = 0   // ensures no prior press detection
	r := straightLineRoute(n, ap, gears, speeds, accel)

	agg := ComputeRouteAggregates(r)
	ref := ComputeAggregateStatsFromRoutes([]Route{r})

	// Compare the overlapping fields. ref is in km/miles while agg is
	// in meters and milliseconds; convert to meters for comparison.
	refDistM := ref.TotalDistanceKm * 1000
	if math.Abs(agg.DistanceM-refDistM) > 0.001 {
		t.Errorf("DistanceM: agg=%v ref=%v", agg.DistanceM, refDistM)
	}
	refFSDDistM := ref.FSDDistanceKm * 1000
	if math.Abs(agg.FSDDistanceM-refFSDDistM) > 0.001 {
		t.Errorf("FSDDistanceM: agg=%v ref=%v", agg.FSDDistanceM, refFSDDistM)
	}
	refAutoDistM := ref.AutosteerDistanceKm * 1000
	if math.Abs(agg.AutosteerDistanceM-refAutoDistM) > 0.001 {
		t.Errorf("AutosteerDistanceM: agg=%v ref=%v", agg.AutosteerDistanceM, refAutoDistM)
	}
	if agg.FSDEngagedMs != ref.FSDEngagedMs {
		t.Errorf("FSDEngagedMs: agg=%d ref=%d", agg.FSDEngagedMs, ref.FSDEngagedMs)
	}
	if agg.AutosteerEngagedMs != ref.AutosteerEngagedMs {
		t.Errorf("AutosteerEngagedMs: agg=%d ref=%d", agg.AutosteerEngagedMs, ref.AutosteerEngagedMs)
	}
	if agg.FSDDisengagements != ref.FSDDisengagements {
		t.Errorf("FSDDisengagements: agg=%d ref=%d", agg.FSDDisengagements, ref.FSDDisengagements)
	}
	if agg.FSDAccelPushes != ref.FSDAccelPushes {
		t.Errorf("FSDAccelPushes: agg=%d ref=%d", agg.FSDAccelPushes, ref.FSDAccelPushes)
	}
}
