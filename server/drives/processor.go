package drives

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProcessResult contains the outcome of a processing run.
type ProcessResult struct {
	FilesScanned  int    `json:"files_scanned"`
	FilesNew      int    `json:"files_new"`
	FilesWithGPS  int    `json:"files_with_gps"`
	TotalPoints   int    `json:"total_points"`
	Errors        int    `json:"errors"`
	DrivesFound   int    `json:"drives_found"`
	Duration      string `json:"duration"`
	ErrorMessages []string `json:"error_messages,omitempty"`
}

// Processor handles scanning and extracting GPS data from Tesla dashcam clips.
type Processor struct {
	store      *Store
	mu         sync.Mutex
	running    bool
	cancelFunc context.CancelFunc
}

// NewProcessor creates a new processor attached to the given store.
func NewProcessor(store *Store) *Processor {
	return &Processor{store: store}
}

// IsRunning returns true if a processing job is currently active.
func (p *Processor) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// Cancel stops the current processing job if one is running.
func (p *Processor) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelFunc != nil {
		p.cancelFunc()
	}
}

// ProcessDirectory scans clipsDir for front-camera MP4s and extracts GPS data.
// Runs in the calling goroutine. Use go p.ProcessDirectory(...) for background.
// throttleMs controls the sleep between files (0 = no throttle).
// onProgress is called periodically with status updates (can be nil).
func (p *Processor) ProcessDirectory(ctx context.Context, clipsDir string, throttleMs int, onProgress func(current, total int)) (*ProcessResult, error) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil, fmt.Errorf("processing already in progress")
	}
	p.running = true
	ctx, cancel := context.WithCancel(ctx)
	p.cancelFunc = cancel
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.running = false
		p.cancelFunc = nil
		p.mu.Unlock()
		cancel()
	}()

	start := time.Now()
	result := &ProcessResult{}

	// Discover front camera files
	allFiles, err := discoverFrontCameraFiles(clipsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}
	result.FilesScanned = len(allFiles)

	// Filter out already-processed files
	// ProcessedSet indexes both original and normalized paths to handle mixed separators
	processedSet := p.store.ProcessedSet()
	var newFiles []fileInfo
	for _, f := range allFiles {
		if !processedSet[f.relativePath] {
			newFiles = append(newFiles, f)
		}
	}
	result.FilesNew = len(newFiles)

	if len(newFiles) == 0 {
		result.Duration = time.Since(start).Round(time.Millisecond).String()
		return result, nil
	}

	log.Printf("[drives] Processing %d new files from %s", len(newFiles), clipsDir)

	saveInterval := 50
	throttle := time.Duration(throttleMs) * time.Millisecond
	if throttleMs <= 0 {
		// Default: 10ms between files on Pi to avoid CPU hogging
		throttle = 10 * time.Millisecond
	}

	for i, f := range newFiles {
		// Check for cancellation
		select {
		case <-ctx.Done():
			_ = p.store.Save()
			result.Duration = time.Since(start).Round(time.Millisecond).String()
			return result, ctx.Err()
		default:
		}

		points, gears, apStates, speeds, accelPositions, err := ExtractGPSFromFile(f.fullPath)
		if err != nil {
			result.Errors++
			if len(result.ErrorMessages) < 10 {
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %v", f.relativePath, err))
			}
			p.store.MarkProcessed(f.relativePath)
		} else {
			// Count raw park frames before dedup for accurate park time estimation
			rawFrameCount := len(gears)
			rawParkCount := 0
			for _, g := range gears {
				if g == GearPark {
					rawParkCount++
				}
			}

			// Compute gear runs from raw data for intra-clip drive splitting
			gearRuns := computeGearRuns(gears)

			// Deduplicate consecutive identical points
			deduped, dedupedGears, dedupedAP, dedupedSpeeds, dedupedAccel := deduplicatePoints(points, gears, apStates, speeds, accelPositions)
			if len(deduped) > 0 {
				p.store.AddRoute(f.relativePath, f.dateDir, deduped, dedupedGears, dedupedAP, dedupedSpeeds, dedupedAccel, rawParkCount, rawFrameCount, gearRuns)
				result.FilesWithGPS++
				result.TotalPoints += len(deduped)
			} else {
				p.store.MarkProcessed(f.relativePath)
			}
		}

		// Periodic save
		if (i+1)%saveInterval == 0 {
			if err := p.store.Save(); err != nil {
				log.Printf("[drives] Warning: failed to save progress: %v", err)
			}
		}

		// Progress callback
		if onProgress != nil && ((i+1)%10 == 0 || i == len(newFiles)-1) {
			onProgress(i+1, len(newFiles))
		}

		// Throttle to be kind to the Pi's CPU
		if throttle > 0 {
			time.Sleep(throttle)
		}
		// Yield to other goroutines
		runtime.Gosched()
	}

	// Final save
	if err := p.store.Save(); err != nil {
		log.Printf("[drives] Error saving final data: %v", err)
	}

	// Sync drive data to archive mount while we're still "running" so the
	// post-archive shell script keeps the archive mounted.  If the archive
	// is not mounted this is a harmless no-op.
	if err := p.store.SyncToArchive(); err != nil {
		log.Printf("[drives] Warning: failed to sync to archive: %v", err)
	}

	result.Duration = time.Since(start).Round(time.Millisecond).String()

	// Count drives
	routes := p.store.GetRoutes()
	drives := GroupIntoDrives(routes)
	result.DrivesFound = len(drives)

	log.Printf("[drives] Done: %d files, %d with GPS, %d points, %d drives, %d errors in %s",
		result.FilesNew, result.FilesWithGPS, result.TotalPoints, result.DrivesFound, result.Errors, result.Duration)

	return result, nil
}

// fileInfo holds discovered file metadata.
type fileInfo struct {
	relativePath string
	fullPath     string
	dateDir      string
}

// discoverFrontCameraFiles finds all *-front.mp4 files organized in date directories.
func discoverFrontCameraFiles(clipsDir string) ([]fileInfo, error) {
	var files []fileInfo

	entries, err := os.ReadDir(clipsDir)
	if err != nil {
		return nil, err
	}

	// Sort date directories
	var dateDirs []string
	for _, e := range entries {
		if e.IsDir() {
			dateDirs = append(dateDirs, e.Name())
		}
	}
	sort.Strings(dateDirs)

	for _, dateDir := range dateDirs {
		dirPath := filepath.Join(clipsDir, dateDir)
		mp4s, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}

		for _, mp4 := range mp4s {
			name := mp4.Name()
			if !mp4.IsDir() && strings.HasSuffix(name, "-front.mp4") {
				relPath := filepath.Join(dateDir, name)
				files = append(files, fileInfo{
					relativePath: relPath,
					fullPath:     filepath.Join(dirPath, name),
					dateDir:      dateDir,
				})
			}
		}
	}

	return files, nil
}

// computeGearRuns computes contiguous runs of gear states from raw frame data.
// e.g. [Drive,Drive,Drive,Park,Park,Drive] → [{Drive,3},{Park,2},{Drive,1}]
func computeGearRuns(gears []uint8) []GearRun {
	if len(gears) == 0 {
		return nil
	}
	var runs []GearRun
	currentGear := gears[0]
	count := 1
	for i := 1; i < len(gears); i++ {
		if gears[i] == currentGear {
			count++
		} else {
			runs = append(runs, GearRun{Gear: currentGear, Frames: count})
			currentGear = gears[i]
			count = 1
		}
	}
	runs = append(runs, GearRun{Gear: currentGear, Frames: count})
	return runs
}

// deduplicatePoints removes consecutive identical GPS points, keeping all parallel slices in sync.
func deduplicatePoints(points []GPSPoint, gears []uint8, apStates []uint8, speeds []float32, accelPositions []float32) ([]GPSPoint, []uint8, []uint8, []float32, []float32) {
	if len(points) == 0 {
		return nil, nil, nil, nil, nil
	}
	hasGears := len(gears) == len(points)
	hasAP := len(apStates) == len(points)
	hasSpeeds := len(speeds) == len(points)
	hasAccel := len(accelPositions) == len(points)

	deduped := []GPSPoint{points[0]}
	var dedupedGears []uint8
	var dedupedAP []uint8
	var dedupedSpeeds []float32
	var dedupedAccel []float32
	if hasGears {
		dedupedGears = []uint8{gears[0]}
	}
	if hasAP {
		dedupedAP = []uint8{apStates[0]}
	}
	if hasSpeeds {
		dedupedSpeeds = []float32{speeds[0]}
	}
	if hasAccel {
		dedupedAccel = []float32{accelPositions[0]}
	}
	for i := 1; i < len(points); i++ {
		if points[i][0] != points[i-1][0] || points[i][1] != points[i-1][1] {
			deduped = append(deduped, points[i])
			if hasGears {
				dedupedGears = append(dedupedGears, gears[i])
			}
			if hasAP {
				dedupedAP = append(dedupedAP, apStates[i])
			}
			if hasSpeeds {
				dedupedSpeeds = append(dedupedSpeeds, speeds[i])
			}
			if hasAccel {
				dedupedAccel = append(dedupedAccel, accelPositions[i])
			}
		}
	}
	return deduped, dedupedGears, dedupedAP, dedupedSpeeds, dedupedAccel
}
