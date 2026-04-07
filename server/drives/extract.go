package drives

import (
	"encoding/binary"
	"io"
	"math"
	"os"
)

// GPSPoint is a single [lat, lon] coordinate extracted from SEI data.
type GPSPoint [2]float64

// Gear constants matching Tesla's SeiMetadata.Gear enum.
const (
	GearPark    = 0
	GearDrive   = 1
	GearReverse = 2
	GearNeutral = 3
)

// Autopilot state constants matching Tesla's Dashcam.proto AutopilotState enum.
const (
	AutopilotOff       = 0 // Manual driving
	AutopilotFSD       = 1 // Full Self-Driving (Supervised)
	AutopilotAutosteer = 2 // Autopilot (Autosteer)
	AutopilotTACC      = 3 // Traffic-Aware Cruise Control
)

// ExtractGPSFromFile opens an MP4 file and extracts GPS points, gear states,
// autopilot states, speeds, and accelerator pedal positions from SEI NAL units.
// Memory-efficient: reads the mdat box in chunks rather than loading the entire file.
// The returned slices are parallel to points (same length, same index).
func ExtractGPSFromFile(path string) ([]GPSPoint, []uint8, []uint8, []float32, []float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	defer f.Close()

	mdatOffset, mdatSize, err := findMdatBox(f)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if mdatSize == 0 {
		return nil, nil, nil, nil, nil, nil
	}

	return extractFromMdat(f, mdatOffset, mdatSize)
}

// findMdatBox scans MP4 top-level boxes to find the mdat box.
// Returns the data offset (after header) and data size.
func findMdatBox(f *os.File) (offset int64, size int64, err error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	fileSize := fi.Size()

	var pos int64
	header := make([]byte, 16)

	for pos < fileSize {
		if _, err := f.ReadAt(header[:8], pos); err != nil {
			return 0, 0, err
		}

		boxSize := int64(binary.BigEndian.Uint32(header[:4]))
		boxType := string(header[4:8])
		headerSize := int64(8)

		if boxSize == 1 {
			// Extended size
			if _, err := f.ReadAt(header[8:16], pos+8); err != nil {
				return 0, 0, err
			}
			boxSize = int64(binary.BigEndian.Uint64(header[8:16]))
			headerSize = 16
		} else if boxSize == 0 {
			boxSize = fileSize - pos
		}

		if boxType == "mdat" {
			return pos + headerSize, boxSize - headerSize, nil
		}

		if boxSize < 8 {
			break
		}
		pos += boxSize
	}

	return 0, 0, nil
}

// extractFromMdat reads through the mdat box parsing NAL units and extracting GPS from SEI.
// Uses a 64KB read buffer to avoid loading large mdat sections into memory.
func extractFromMdat(f *os.File, offset, size int64) ([]GPSPoint, []uint8, []uint8, []float32, []float32, error) {
	const bufSize = 64 * 1024
	var points []GPSPoint
	var gears []uint8
	var apStates []uint8
	var speeds []float32
	var accelPositions []float32

	end := offset + size
	cursor := offset
	sizeBuf := make([]byte, 4)

	for cursor+4 <= end {
		// Read NAL size (4 bytes, big-endian)
		if _, err := f.ReadAt(sizeBuf, cursor); err != nil {
			if err == io.EOF {
				break
			}
			return points, gears, apStates, speeds, accelPositions, nil
		}
		cursor += 4

		nalSize := int64(binary.BigEndian.Uint32(sizeBuf))
		if nalSize < 2 || cursor+nalSize > end {
			break
		}

		// Read NAL type byte
		typeBuf := make([]byte, 1)
		if _, err := f.ReadAt(typeBuf, cursor); err != nil {
			break
		}

		nalType := typeBuf[0] & 0x1F

		// NAL type 6 = SEI
		if nalType == 6 && nalSize <= bufSize {
			nal := make([]byte, nalSize)
			if _, err := f.ReadAt(nal, cursor); err == nil {
				if lat, lon, gear, apState, speed, accelPos, ok := parseTeslaSEI(nal); ok {
					points = append(points, GPSPoint{
						math.Round(lat*1e6) / 1e6,
						math.Round(lon*1e6) / 1e6,
					})
					gears = append(gears, gear)
					apStates = append(apStates, apState)
					speeds = append(speeds, speed)
					accelPositions = append(accelPositions, accelPos)
				}
			}
		}

		cursor += nalSize
	}

	return points, gears, apStates, speeds, accelPositions, nil
}

// parseTeslaSEI finds the Tesla magic bytes (0x42...0x69) in a SEI NAL and decodes GPS + gear + autopilot + speed + accel.
func parseTeslaSEI(nal []byte) (lat, lon float64, gear uint8, apState uint8, speed float32, accelPos float32, ok bool) {
	// Skip NAL header, look for 0x42 sequence followed by 0x69
	i := 3
	for i < len(nal) && nal[i] == 0x42 {
		i++
	}
	if i <= 3 || i+1 >= len(nal) || nal[i] != 0x69 {
		return 0, 0, 0, 0, 0, 0, false
	}

	// Payload starts after 0x69, ends before trailing byte
	payload := nal[i+1:]
	if len(payload) > 1 {
		payload = payload[:len(payload)-1]
	}

	stripped := stripEmulationBytes(payload)
	return decodeSeiGPS(stripped)
}

// decodeFloat32LE decodes a little-endian float32 from 4 bytes.
func decodeFloat32LE(data []byte) float32 {
	bits := binary.LittleEndian.Uint32(data)
	return math.Float32frombits(bits)
}

// stripEmulationBytes removes H.264 emulation prevention bytes (0x00 0x00 0x03 → 0x00 0x00).
func stripEmulationBytes(data []byte) []byte {
	out := make([]byte, 0, len(data))
	zeros := 0
	for _, b := range data {
		if zeros >= 2 && b == 0x03 {
			zeros = 0
			continue
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

// decodeSeiGPS decodes protobuf SeiMetadata to extract latitude (field 11),
// longitude (field 12), gear_state (field 2), autopilot_state (field 10),
// vehicle_speed_mps (field 4, float32), and accelerator_pedal_position (field 5, float32).
// Hand-parses protobuf wire format to avoid external dependencies.
func decodeSeiGPS(data []byte) (lat, lon float64, gear uint8, apState uint8, speed float32, accelPos float32, ok bool) {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n

		fieldNum := tag >> 3
		wireType := tag & 0x7

		switch wireType {
		case 0: // varint
			val, vn := decodeVarint(data[i:])
			if vn == 0 {
				return 0, 0, 0, 0, 0, 0, false
			}
			i += vn
			if fieldNum == 2 {
				gear = uint8(val)
			} else if fieldNum == 10 {
				apState = uint8(val)
			}
		case 1: // 64-bit (fixed64, double)
			if i+8 > len(data) {
				return 0, 0, 0, 0, 0, 0, false
			}
			bits := binary.LittleEndian.Uint64(data[i : i+8])
			val := math.Float64frombits(bits)
			i += 8
			if fieldNum == 11 {
				lat = val
			} else if fieldNum == 12 {
				lon = val
			}
		case 2: // length-delimited
			length, vn := decodeVarint(data[i:])
			if vn == 0 {
				return 0, 0, 0, 0, 0, 0, false
			}
			i += vn + int(length)
		case 5: // 32-bit (fixed32, float)
			if i+4 > len(data) {
				return 0, 0, 0, 0, 0, 0, false
			}
			if fieldNum == 4 {
				speed = decodeFloat32LE(data[i : i+4])
			} else if fieldNum == 5 {
				// accelerator_pedal_position — Tesla does not record FSD-commanded
				// input here, so any value > 0 while autopilot is active is the human.
				accelPos = decodeFloat32LE(data[i : i+4])
			}
			i += 4
		default:
			return 0, 0, 0, 0, 0, 0, false
		}
	}

	ok = math.IsInf(lat, 0) == false && math.IsInf(lon, 0) == false &&
		math.IsNaN(lat) == false && math.IsNaN(lon) == false &&
		!(lat == 0 && lon == 0) &&
		math.Abs(lat) <= 90 && math.Abs(lon) <= 180

	return lat, lon, gear, apState, speed, accelPos, ok
}

// decodeVarint reads a protobuf varint from data. Returns value and bytes consumed.
func decodeVarint(data []byte) (uint64, int) {
	var val uint64
	var shift uint
	for i, b := range data {
		if i >= 10 {
			return 0, 0
		}
		val |= uint64(b&0x7F) << shift
		if b < 0x80 {
			return val, i + 1
		}
		shift += 7
	}
	return 0, 0
}
