package drives

// Data types shared between the Store, the processor, the grouper, and the
// JSON import/export paths. Kept in their own file so the Store
// implementation (store.go) stays focused on persistence.

// GearRun represents a contiguous run of frames in the same gear state.
// Computed from raw (pre-dedup) frame data for accurate intra-clip splitting.
type GearRun struct {
	Gear   uint8 `json:"gear"`
	Frames int   `json:"frames"`
}

// Route represents GPS data extracted from a single front-camera clip.
//
// The parallel slices (Points, GearStates, AutopilotStates, Speeds,
// AccelPositions) all have the same length when populated; callers must
// treat them as a unit. Older drive-data.json files may have some of the
// optional slices empty or missing, which is why they're all omitempty.
type Route struct {
	File            string     `json:"file"`
	Date            string     `json:"date"`
	Points          []GPSPoint `json:"points"`
	GearStates      []uint8    `json:"gearStates,omitempty"`
	AutopilotStates []uint8    `json:"autopilotStates,omitempty"`
	Speeds          []float32  `json:"speeds,omitempty"`
	AccelPositions  []float32  `json:"accelPositions,omitempty"`
	RawParkCount    int        `json:"rawParkCount,omitempty"`
	RawFrameCount   int        `json:"rawFrameCount,omitempty"`
	GearRuns        []GearRun  `json:"gearRuns,omitempty"`
}

// StoreData is the archive-side JSON structure that Sentry Studio reads
// from the archive server (rsync/CIFS/rclone). It is also the payload for
// /api/drives/data/download and /api/drives/data/upload.
//
// This shape must stay backward-compatible with existing Sentry Studio
// clients; the SQLite store translates to/from it on demand at the
// archive-sync and download boundaries.
type StoreData struct {
	ProcessedFiles []string            `json:"processedFiles"`
	Routes         []Route             `json:"routes"`
	DriveTags      map[string][]string `json:"driveTags,omitempty"`
}
