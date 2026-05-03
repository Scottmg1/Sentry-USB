package drives

import (
	"reflect"
	"testing"
)

// TestBuildRoutesOverviewFromClipSets_PointsAreTimeOrdered verifies that the
// overview endpoint concatenates per-clip point segments in chronological
// order. The previous implementation iterated the clip-file map directly,
// which was non-deterministic and produced criss-cross "teleportation" lines
// in the overview polyline whenever the random map order differed from the
// timestamp order of the underlying clips.
//
// We run the call repeatedly because Go randomizes map iteration order
// per-process; a single iteration could pass by chance.
func TestBuildRoutesOverviewFromClipSets_PointsAreTimeOrdered(t *testing.T) {
	routes := []Route{
		{
			File: "/clips/2026-05-01/2026-05-01_10-00-00-front.mp4",
			Points: []GPSPoint{
				{47.6000, -122.3000},
				{47.6010, -122.3010},
			},
		},
		{
			File: "/clips/2026-05-01/2026-05-01_10-01-00-front.mp4",
			Points: []GPSPoint{
				{47.6020, -122.3020},
				{47.6030, -122.3030},
			},
		},
		{
			File: "/clips/2026-05-01/2026-05-01_10-02-00-front.mp4",
			Points: []GPSPoint{
				{47.6040, -122.3040},
				{47.6050, -122.3050},
			},
		},
		{
			File: "/clips/2026-05-01/2026-05-01_10-03-00-front.mp4",
			Points: []GPSPoint{
				{47.6060, -122.3060},
				{47.6070, -122.3070},
			},
		},
	}

	clipSets := []map[string]bool{
		{
			"/clips/2026-05-01/2026-05-01_10-00-00-front.mp4": true,
			"/clips/2026-05-01/2026-05-01_10-01-00-front.mp4": true,
			"/clips/2026-05-01/2026-05-01_10-02-00-front.mp4": true,
			"/clips/2026-05-01/2026-05-01_10-03-00-front.mp4": true,
		},
	}

	expected := [][2]float64{
		{47.6000, -122.3000}, {47.6010, -122.3010},
		{47.6020, -122.3020}, {47.6030, -122.3030},
		{47.6040, -122.3040}, {47.6050, -122.3050},
		{47.6060, -122.3060}, {47.6070, -122.3070},
	}

	for trial := 0; trial < 50; trial++ {
		got := BuildRoutesOverviewFromClipSets(routes, clipSets, 1000)
		if len(got) != 1 {
			t.Fatalf("trial %d: want 1 RouteOverview, got %d", trial, len(got))
		}
		if !reflect.DeepEqual(got[0].Points, expected) {
			t.Fatalf("trial %d: points out of chronological order\n want: %v\n  got: %v",
				trial, expected, got[0].Points)
		}
	}
}
