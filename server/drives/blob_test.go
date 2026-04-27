package drives

import (
	"bytes"
	"math"
	"testing"
)

// -----------------------------------------------------------------------------
// Points: 2×float64 little-endian per point
// -----------------------------------------------------------------------------

func TestEncodeDecodePoints_EmptyRoundTrip(t *testing.T) {
	if got := encodePoints(nil); got != nil {
		t.Errorf("encodePoints(nil): got %v, want nil", got)
	}
	if got := encodePoints([]GPSPoint{}); len(got) != 0 {
		t.Errorf("encodePoints([]): got len %d, want 0", len(got))
	}
	out, err := decodePoints(nil)
	if err != nil {
		t.Fatalf("decodePoints(nil): %v", err)
	}
	if out != nil {
		t.Errorf("decodePoints(nil): got %v, want nil", out)
	}
}

func TestEncodeDecodePoints_RoundTripMany(t *testing.T) {
	in := []GPSPoint{
		{34.052235, -118.243683}, // LA
		{40.712776, -74.005974},  // NYC
		{0, 0},
		{-89.999, 179.999},
	}
	buf := encodePoints(in)
	if len(buf) != 16*len(in) {
		t.Fatalf("encoded len = %d, want %d", len(buf), 16*len(in))
	}
	out, err := decodePoints(buf)
	if err != nil {
		t.Fatalf("decodePoints: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("point[%d]: got %v, want %v", i, out[i], in[i])
		}
	}
}

func TestDecodePoints_RejectsOddLength(t *testing.T) {
	// 16 bytes = 1 valid point; 24 bytes = invalid (1.5 points)
	_, err := decodePoints(make([]byte, 24))
	if err == nil {
		t.Fatal("expected error on bad length")
	}
}

func TestEncodeDecodePoints_NaNPreserved(t *testing.T) {
	in := []GPSPoint{{math.NaN(), 0}}
	buf := encodePoints(in)
	out, err := decodePoints(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(out[0][0]) {
		t.Errorf("NaN lost: %v", out[0])
	}
}

// -----------------------------------------------------------------------------
// uint8 slices (gear states, autopilot states) — identity encoding
// -----------------------------------------------------------------------------

func TestEncodeDecodeUint8s_RoundTrip(t *testing.T) {
	in := []uint8{0, 1, 2, 3, 255, 128, 0}
	buf := encodeUint8s(in)
	if !bytes.Equal(buf, in) {
		t.Fatalf("encoded bytes != input: %v vs %v", buf, in)
	}
	out := decodeUint8s(buf)
	if !bytes.Equal(out, in) {
		t.Fatalf("round-trip: got %v, want %v", out, in)
	}
}

func TestEncodeDecodeUint8s_NilRoundTrip(t *testing.T) {
	if encodeUint8s(nil) != nil {
		t.Error("encodeUint8s(nil) should be nil")
	}
	if got := decodeUint8s(nil); got != nil {
		t.Errorf("decodeUint8s(nil): got %v, want nil", got)
	}
}

// -----------------------------------------------------------------------------
// float32 slices (speeds, accel positions)
// -----------------------------------------------------------------------------

func TestEncodeDecodeFloat32s_RoundTripMany(t *testing.T) {
	in := []float32{0, 1.5, -3.14, 65535.125, float32(math.Inf(1)), float32(math.Inf(-1))}
	buf := encodeFloat32s(in)
	if len(buf) != 4*len(in) {
		t.Fatalf("encoded len = %d, want %d", len(buf), 4*len(in))
	}
	out, err := decodeFloat32s(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if math.Float32bits(in[i]) != math.Float32bits(out[i]) {
			t.Errorf("float[%d]: got %v, want %v", i, out[i], in[i])
		}
	}
}

func TestDecodeFloat32s_RejectsBadLength(t *testing.T) {
	_, err := decodeFloat32s(make([]byte, 7))
	if err == nil {
		t.Fatal("expected error on len not divisible by 4")
	}
}

// -----------------------------------------------------------------------------
// GearRuns: 1 uint8 gear + 4 int32 frames per run (little-endian)
// -----------------------------------------------------------------------------

func TestEncodeDecodeGearRuns_RoundTrip(t *testing.T) {
	in := []GearRun{
		{Gear: 1, Frames: 1200},
		{Gear: 0, Frames: 500},   // Park
		{Gear: 2, Frames: 1},     // Reverse, 1 frame
		{Gear: 255, Frames: 999}, // max gear byte
	}
	buf := encodeGearRuns(in)
	if len(buf) != 5*len(in) {
		t.Fatalf("encoded len = %d, want %d", len(buf), 5*len(in))
	}
	out, err := decodeGearRuns(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("run[%d]: got %+v, want %+v", i, out[i], in[i])
		}
	}
}

func TestEncodeDecodeGearRuns_NilRoundTrip(t *testing.T) {
	if encodeGearRuns(nil) != nil {
		t.Error("encodeGearRuns(nil) should be nil")
	}
	out, err := decodeGearRuns(nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("decodeGearRuns(nil): got %v, want nil", out)
	}
}

func TestDecodeGearRuns_RejectsBadLength(t *testing.T) {
	_, err := decodeGearRuns(make([]byte, 7))
	if err == nil {
		t.Fatal("expected error on len not divisible by 5")
	}
}

// -----------------------------------------------------------------------------
// Realistic route sizing smoke test: 2000-point clip round-trips exactly
// -----------------------------------------------------------------------------

func TestBlobs_RealisticRouteRoundTrip(t *testing.T) {
	const n = 2000
	points := make([]GPSPoint, n)
	gears := make([]uint8, n)
	aps := make([]uint8, n)
	speeds := make([]float32, n)
	accel := make([]float32, n)
	runs := []GearRun{{Gear: 1, Frames: 1500}, {Gear: 0, Frames: 500}}
	for i := 0; i < n; i++ {
		points[i] = GPSPoint{40.0 + float64(i)*0.00001, -74.0 + float64(i)*0.00001}
		gears[i] = uint8(i % 4)
		aps[i] = uint8((i / 100) % 2)
		speeds[i] = float32(i) * 0.1
		accel[i] = float32(i) * 0.001
	}

	pbuf := encodePoints(points)
	gbuf := encodeUint8s(gears)
	abuf := encodeUint8s(aps)
	sbuf := encodeFloat32s(speeds)
	acbuf := encodeFloat32s(accel)
	rbuf := encodeGearRuns(runs)

	// Sanity on sizes (documents actual on-disk footprint for Pi planning)
	if len(pbuf) != n*16 {
		t.Errorf("pbuf size: %d, want %d", len(pbuf), n*16)
	}
	if len(sbuf) != n*4 {
		t.Errorf("sbuf size: %d, want %d", len(sbuf), n*4)
	}

	p2, err := decodePoints(pbuf)
	if err != nil {
		t.Fatal(err)
	}
	g2 := decodeUint8s(gbuf)
	a2 := decodeUint8s(abuf)
	s2, err := decodeFloat32s(sbuf)
	if err != nil {
		t.Fatal(err)
	}
	ac2, err := decodeFloat32s(acbuf)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := decodeGearRuns(rbuf)
	if err != nil {
		t.Fatal(err)
	}

	if len(p2) != n || len(g2) != n || len(a2) != n || len(s2) != n || len(ac2) != n {
		t.Fatalf("lengths: p=%d g=%d a=%d s=%d ac=%d (want all %d)", len(p2), len(g2), len(a2), len(s2), len(ac2), n)
	}
	if p2[0] != points[0] || p2[n-1] != points[n-1] {
		t.Error("points first/last mismatch")
	}
	if g2[n-1] != gears[n-1] {
		t.Error("gears last mismatch")
	}
	if math.Float32bits(s2[n-1]) != math.Float32bits(speeds[n-1]) {
		t.Error("speeds last mismatch")
	}
	if len(r2) != len(runs) || r2[0] != runs[0] || r2[1] != runs[1] {
		t.Errorf("runs mismatch: got %v, want %v", r2, runs)
	}
}
