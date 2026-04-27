package drives

import "math"

// ComputeRouteAggregates walks a Route's parallel BLOB-backed slices
// once and returns every scalar the Drives-page summary endpoints need.
// This is the single source of truth: AddRoute calls it on insert, the
// one-shot backfill calls it for pre-v2 rows, and the refactored
// summary endpoints read the stored scalars instead of re-deriving them.
//
// Semantics match ComputeAggregateStatsFromRoutes's per-route inner loop
// (null-island filter + GPS-teleport guard, no group-level median):
//   - Null-island points (|lat| < 1 && |lon| < 1) are excluded from the
//     pair loop.
//   - When no SEI speeds are present we use a per-pair GPS derivation
//     d/dt and drop teleport pairs where d/dt > 70 m/s.
//   - FSD disengagement uses a 2-second Park grace (don't count when
//     FSD parked the car).
//   - FSD accel-push detection uses a 3-second engagement grace so the
//     driver's foot still on the pedal at engagement doesn't trip it.
//
// clipDurationMs is hard-coded to 60000 (one minute) to match every
// other consumer in this package; the recorder splits all clips at
// one-minute boundaries.
func ComputeRouteAggregates(r Route) RouteAggregates {
	var agg RouteAggregates

	n := len(r.Points)
	if n == 0 {
		return agg
	}

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

	// Start/End points: first and last non-null-island Points. Tracked
	// independently of the pair loop so single-point clips still report
	// sensible endpoints.
	for i := 0; i < n; i++ {
		p := r.Points[i]
		if !(math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1) {
			lat, lng := p[0], p[1]
			agg.StartLat = &lat
			agg.StartLng = &lng
			break
		}
	}
	for i := n - 1; i >= 0; i-- {
		p := r.Points[i]
		if !(math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1) {
			lat, lng := p[0], p[1]
			agg.EndLat = &lat
			agg.EndLng = &lng
			break
		}
	}

	// ValidPointCount is the count of non-null-island points.
	for _, p := range r.Points {
		if !(math.Abs(p[0]) < 1 && math.Abs(p[1]) < 1) {
			agg.ValidPointCount++
		}
	}

	if n < 2 {
		return agg
	}

	clipDurationMs := 60000.0
	dtMs := clipDurationMs / float64(n-1)
	dtSec := dtMs / 1000.0

	// Autopilot event tracking — state is reset per-clip; this matches
	// the GroupSummaries inner loop (the clip-by-clip iteration there
	// also resets these between clips).
	var inAccelPress bool
	fsdEngageIdx := -1
	var pendingDisengage bool
	var pendingDisengageIdx int

	var speedSum float64

	for i := 1; i < n; i++ {
		prev := r.Points[i-1]
		cur := r.Points[i]
		prevNull := math.Abs(prev[0]) < 1 && math.Abs(prev[1]) < 1
		curNull := math.Abs(cur[0]) < 1 && math.Abs(cur[1]) < 1
		if prevNull || curNull {
			continue
		}

		d := haversineM(prev[0], prev[1], cur[0], cur[1])

		// GPS-teleport guard when no SEI speeds are available.
		if !hasSEISpeeds && dtSec > 0 && d/dtSec > 70 {
			continue
		}

		agg.DistanceM += d

		// Speed accounting.
		if hasSEISpeeds {
			speed := float64(r.Speeds[i])
			if speed >= 0 && speed < 100 {
				speedSum += speed
				agg.SpeedSampleCount++
				if speed > agg.MaxSpeedMps {
					agg.MaxSpeedMps = speed
				}
			}
		} else if dtSec > 0 {
			speed := d / dtSec
			if speed < 70 {
				speedSum += speed
				agg.SpeedSampleCount++
				if speed > agg.MaxSpeedMps {
					agg.MaxSpeedMps = speed
				}
			}
		}

		// Autopilot accounting.
		if hasAP {
			curAP := r.AutopilotStates[i]
			prevAP := r.AutopilotStates[i-1]

			if curAP != AutopilotOff {
				agg.AssistedDistanceM += d
				switch curAP {
				case AutopilotFSD:
					agg.FSDEngagedMs += int64(dtMs)
					agg.FSDDistanceM += d
				case AutopilotAutosteer:
					agg.AutosteerEngagedMs += int64(dtMs)
					agg.AutosteerDistanceM += d
				case AutopilotTACC:
					agg.TACCEngagedMs += int64(dtMs)
					agg.TACCDistanceM += d
				}
			}

			// Track FSD engagement start (for the 3s accel grace).
			if prevAP != AutopilotFSD && curAP == AutopilotFSD {
				fsdEngageIdx = i
				inAccelPress = false
			}

			// Resolve any pending FSD disengagement.
			if pendingDisengage {
				timeSinceMs := float64(i-pendingDisengageIdx) * dtMs
				if hasGears && r.GearStates[i] == GearPark && timeSinceMs <= 2000.0 {
					pendingDisengage = false
				} else if timeSinceMs > 2000.0 || curAP == AutopilotFSD {
					agg.FSDDisengagements++
					pendingDisengage = false
				}
			}

			// Defer FSD disengagement for the Park grace check.
			if prevAP == AutopilotFSD && curAP != AutopilotFSD {
				pendingDisengage = true
				pendingDisengageIdx = i
				inAccelPress = false
			}

			// Accel-push detection (FSD only).
			if curAP == AutopilotFSD && hasAccel {
				accelPct := float64(r.AccelPositions[i])
				if accelPct <= 1.0 {
					accelPct *= 100.0
				}
				timeSinceEngageMs := 0.0
				if fsdEngageIdx >= 0 {
					timeSinceEngageMs = float64(i-fsdEngageIdx) * dtMs
				}
				if !inAccelPress && accelPct > 1.0 && timeSinceEngageMs >= 3000.0 {
					inAccelPress = true
				} else if inAccelPress && accelPct <= 0.0 {
					agg.FSDAccelPushes++
					inAccelPress = false
				}
			} else if curAP != AutopilotFSD {
				inAccelPress = false
			}
		}
	}

	// Flush pending disengagement at end of clip (match GroupSummaries).
	if pendingDisengage {
		if !(hasGears && r.GearStates[n-1] == GearPark) {
			agg.FSDDisengagements++
		}
	}

	if agg.SpeedSampleCount > 0 {
		agg.AvgSpeedMps = speedSum / float64(agg.SpeedSampleCount)
	}

	return agg
}
