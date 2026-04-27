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

// Route represents GPS data extracted from a single front-camera clip,
// or a synthetic clip imported from an external source like Tessie.
//
// The parallel slices (Points, GearStates, AutopilotStates, Speeds,
// AccelPositions) all have the same length when populated; callers must
// treat them as a unit. Older drive-data.json files may have some of the
// optional slices empty or missing, which is why they're all omitempty.
//
// Source / ExternalSignature / TessieAutopilotPercent are populated by
// Sentry-Drive's Tessie API import flow. They flow through Sentry-USB's
// JSON import unchanged so the Drives page can exclude Tessie clips from
// the aggregate FSD score (Tessie's per-point autopilot signal is fuzzier
// than native SEI telemetry — counting it would skew the score).
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
	// Provenance: "" / "sei" = native dashcam, "tessie" = imported from
	// Tessie API. Empty is treated as "sei" in storage and aggregation
	// paths so legacy drive-data.json files round-trip unchanged.
	Source                 string  `json:"source,omitempty"`
	ExternalSignature      string  `json:"externalSignature,omitempty"`
	TessieAutopilotPercent float64 `json:"tessieAutopilotPercent,omitempty"`
}

// RouteAggregates is the per-clip scalar summary computed once from a
// Route's BLOB-backed parallel slices. Cached as columns on the routes
// table so the Drives-page summary endpoints never have to decode a
// Points/GearStates/AutopilotStates BLOB to produce a list view.
//
// Semantics match ComputeAggregateStatsFromRoutes's per-route inner loop
// (see grouper.go): null-island filter + GPS-teleport guard, no
// group-level median filter. For clean data this is bit-identical to
// the group-filtered path in GroupSummaries; for pathological GPS
// noise the two can drift by fractions of a percent on distance-derived
// fields, which the UI rounds away anyway.
type RouteAggregates struct {
	DistanceM            float64
	MaxSpeedMps          float64
	AvgSpeedMps          float64
	SpeedSampleCount     int
	ValidPointCount      int
	FSDEngagedMs         int64
	AutosteerEngagedMs   int64
	TACCEngagedMs        int64
	FSDDistanceM         float64
	AutosteerDistanceM   float64
	TACCDistanceM        float64
	AssistedDistanceM    float64
	FSDDisengagements    int
	FSDAccelPushes       int
	// Start/End points are the first and last non-null-island Points on
	// the clip. Pointers so a clip with no valid points can report nil
	// without overloading (0, 0) as a sentinel.
	StartLat *float64
	StartLng *float64
	EndLat   *float64
	EndLng   *float64
}

// RouteSummary is the BLOB-free row shape used by the summary
// endpoints. It carries metadata that groupClips needs plus all the
// pre-computed scalars from RouteAggregates. Reading 5500 summary rows
// costs ~5 MB of heap versus ~300 MB for the full Route slice.
//
// Source / ExternalSignature / TessieAutopilotPercent mirror the Route
// fields and are loaded straight from the new routes columns so the
// Tessie-aware aggregate path doesn't have to re-decode any BLOBs.
type RouteSummary struct {
	File                   string
	Date                   string
	RawParkCount           int
	RawFrameCount          int
	GearRuns               []GearRun // metadata-sized; bytes, not KB
	Source                 string
	ExternalSignature      string
	TessieAutopilotPercent float64

	RouteAggregates
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
