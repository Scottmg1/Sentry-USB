package drives

import (
	"encoding/binary"
	"fmt"
	"math"
)

// BLOB encoders/decoders for the parallel-slice point data stored on each
// route row. All formats are little-endian, fixed-stride, and zero-copy
// where safe. The small wire format keeps total DB size comparable to the
// existing JSON (indented JSON is ~2× the binary size) while letting
// SQLite store each clip as one row.
//
// Invariants:
//   - encode<X>(nil) returns nil (not an empty slice). sqlite NULL round-trips
//     cleanly this way.
//   - decode<X>(nil) returns (nil, nil).
//   - decoders reject inputs whose length isn't a multiple of the stride.
//   - Float NaN/Inf bit patterns are preserved exactly — useful for
//     regression tracing if a garbage value ever appears in the source JSON.

// -----------------------------------------------------------------------------
// Points: [2]float64 per point (16 bytes)
// -----------------------------------------------------------------------------

const pointStride = 16 // 2 * float64

func encodePoints(pts []GPSPoint) []byte {
	if pts == nil {
		return nil
	}
	buf := make([]byte, len(pts)*pointStride)
	for i, p := range pts {
		off := i * pointStride
		binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(p[0]))
		binary.LittleEndian.PutUint64(buf[off+8:], math.Float64bits(p[1]))
	}
	return buf
}

func decodePoints(buf []byte) ([]GPSPoint, error) {
	if buf == nil {
		return nil, nil
	}
	if len(buf)%pointStride != 0 {
		return nil, fmt.Errorf("decodePoints: length %d not a multiple of %d", len(buf), pointStride)
	}
	n := len(buf) / pointStride
	out := make([]GPSPoint, n)
	for i := 0; i < n; i++ {
		off := i * pointStride
		lat := math.Float64frombits(binary.LittleEndian.Uint64(buf[off:]))
		lon := math.Float64frombits(binary.LittleEndian.Uint64(buf[off+8:]))
		out[i] = GPSPoint{lat, lon}
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// uint8 slices (gear states, autopilot states): identity encoding
// -----------------------------------------------------------------------------

func encodeUint8s(s []uint8) []byte {
	if s == nil {
		return nil
	}
	// Return a copy so the caller can mutate their slice without affecting
	// what we hand to the DB driver.
	out := make([]byte, len(s))
	copy(out, s)
	return out
}

func decodeUint8s(buf []byte) []uint8 {
	if buf == nil {
		return nil
	}
	out := make([]uint8, len(buf))
	copy(out, buf)
	return out
}

// -----------------------------------------------------------------------------
// float32 slices (speeds, accel positions): 4 bytes per value
// -----------------------------------------------------------------------------

const float32Stride = 4

func encodeFloat32s(s []float32) []byte {
	if s == nil {
		return nil
	}
	buf := make([]byte, len(s)*float32Stride)
	for i, v := range s {
		binary.LittleEndian.PutUint32(buf[i*float32Stride:], math.Float32bits(v))
	}
	return buf
}

func decodeFloat32s(buf []byte) ([]float32, error) {
	if buf == nil {
		return nil, nil
	}
	if len(buf)%float32Stride != 0 {
		return nil, fmt.Errorf("decodeFloat32s: length %d not a multiple of %d", len(buf), float32Stride)
	}
	n := len(buf) / float32Stride
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*float32Stride:]))
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// GearRuns: 1 uint8 gear + 4 bytes int32 frames (little-endian) per run
// -----------------------------------------------------------------------------

const gearRunStride = 5 // uint8 + int32

func encodeGearRuns(runs []GearRun) []byte {
	if runs == nil {
		return nil
	}
	buf := make([]byte, len(runs)*gearRunStride)
	for i, r := range runs {
		off := i * gearRunStride
		buf[off] = r.Gear
		// Frames is declared as int (Go native) but in practice fits in int32
		// (clip lengths are bounded by recording time * fps; nowhere near 2^31).
		// Explicitly use int32 to stabilize the wire format across 32/64-bit
		// Go builds.
		binary.LittleEndian.PutUint32(buf[off+1:], uint32(int32(r.Frames)))
	}
	return buf
}

func decodeGearRuns(buf []byte) ([]GearRun, error) {
	if buf == nil {
		return nil, nil
	}
	if len(buf)%gearRunStride != 0 {
		return nil, fmt.Errorf("decodeGearRuns: length %d not a multiple of %d", len(buf), gearRunStride)
	}
	n := len(buf) / gearRunStride
	out := make([]GearRun, n)
	for i := 0; i < n; i++ {
		off := i * gearRunStride
		out[i] = GearRun{
			Gear:   buf[off],
			Frames: int(int32(binary.LittleEndian.Uint32(buf[off+1:]))),
		}
	}
	return out, nil
}
