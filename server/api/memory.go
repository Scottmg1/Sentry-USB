package api

import (
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

// GET /api/memory — memory breakdown with per-feature allocation info
func (h *handlers) memoryStats(w http.ResponseWriter, r *http.Request) {
	// Force a GC so numbers reflect actual live memory, not pending garbage
	runtime.GC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	bi, _ := debug.ReadBuildInfo()
	goVersion := ""
	if bi != nil {
		goVersion = bi.GoVersion
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		// Headline numbers
		"heap_in_use_mb":  round2(float64(m.HeapInuse) / 1048576),
		"heap_alloc_mb":   round2(float64(m.HeapAlloc) / 1048576),
		"heap_sys_mb":     round2(float64(m.HeapSys) / 1048576),
		"stack_in_use_mb": round2(float64(m.StackInuse) / 1048576),
		"total_sys_mb":    round2(float64(m.Sys) / 1048576),

		// OS memory
		"total_alloc_mb":   round2(float64(m.TotalAlloc) / 1048576),
		"heap_released_mb": round2(float64(m.HeapReleased) / 1048576),
		"heap_idle_mb":     round2(float64(m.HeapIdle) / 1048576),
		"gc_cpu_percent":   round2(m.GCCPUFraction * 100),
		"num_gc":           m.NumGC,
		"num_goroutines":   runtime.NumGoroutine(),
		"heap_objects":     m.HeapObjects,
		"go_version":       goVersion,

		// Breakdown
		"gc_sys_mb":    round2(float64(m.GCSys) / 1048576),
		"other_sys_mb": round2(float64(m.OtherSys) / 1048576),

		// Per-feature allocation breakdown
		"features": getFeatureBreakdown(),
	})
}

// featureEntry represents one feature's memory usage
type featureEntry struct {
	Name      string  `json:"name"`
	AllocMB   float64 `json:"alloc_mb"`
	Objects   int64   `json:"objects"`
	Source    string  `json:"source"`
}

// getFeatureBreakdown reads the heap profile and groups allocations by
// feature/package so you can see what's actually using memory.
func getFeatureBreakdown() []featureEntry {
	// Get memory profile records
	n, _ := runtime.MemProfile(nil, true)
	records := make([]runtime.MemProfileRecord, n+50)
	n, ok := runtime.MemProfile(records, true)
	if !ok {
		records = make([]runtime.MemProfileRecord, n+100)
		n, _ = runtime.MemProfile(records, true)
	}
	records = records[:n]

	// Map package paths to friendly feature names
	featureMap := map[string]string{
		"drives":        "Drive Map",
		"encoding/json": "JSON Parsing",
		"ws":            "WebSocket",
		"api":           "API Handlers",
		"embed":         "Embedded Files",
		"net/http":      "HTTP Server",
		"crypto":        "TLS/Crypto",
		"bufio":         "I/O Buffers",
		"compress":      "Compression",
		"os":            "OS/File I/O",
		"reflect":       "Reflection",
		"fmt":           "Formatting",
		"regexp":        "Regex",
		"sync":          "Sync Primitives",
	}

	// Accumulate bytes by feature
	type accumulator struct {
		bytes   int64
		objects int64
		source  string // most significant source function
	}
	features := make(map[string]*accumulator)

	for _, rec := range records {
		if rec.InUseBytes() <= 0 {
			continue
		}

		// Walk the call stack to find the owning feature
		feature := "Go Runtime"
		source := ""
		frames := runtime.CallersFrames(rec.Stack())
		for {
			frame, more := frames.Next()
			fn := frame.Function
			if fn == "" {
				if !more {
					break
				}
				continue
			}

			// Try to match to a feature
			matched := ""
			for prefix, name := range featureMap {
				if strings.Contains(fn, prefix) {
					matched = name
					break
				}
			}
			if matched != "" {
				feature = matched
				// Get short function name for source
				parts := strings.Split(fn, "/")
				source = parts[len(parts)-1]
				break
			}

			if !more {
				break
			}
		}

		acc, ok := features[feature]
		if !ok {
			acc = &accumulator{}
			features[feature] = acc
		}
		acc.bytes += rec.InUseBytes()
		acc.objects += rec.InUseObjects()
		if acc.source == "" {
			acc.source = source
		}
	}

	// Convert to sorted slice
	var result []featureEntry
	for name, acc := range features {
		result = append(result, featureEntry{
			Name:    name,
			AllocMB: round2(float64(acc.bytes) / 1048576),
			Objects: acc.objects,
			Source:  acc.source,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].AllocMB > result[j].AllocMB
	})

	return result
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}

// GET /memory — serves a simple HTML page showing memory stats
func MemoryPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>SentryUSB Memory</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  body { background: #111; color: #eee; font-family: monospace; padding: 20px; margin: 0; }
  h1 { color: #3b82f6; font-size: 1.4em; }
  h2 { color: #3b82f6; font-size: 1.1em; margin-top: 24px; }
  table { border-collapse: collapse; width: 100%; max-width: 700px; margin-bottom: 16px; }
  td, th { padding: 6px 12px; border-bottom: 1px solid #333; text-align: left; }
  th { color: #666; font-weight: normal; font-size: 0.85em; }
  td:first-child { color: #888; }
  td:nth-child(2) { text-align: right; font-weight: bold; }
  .big { font-size: 1.2em; color: #f59e0b; }
  .section { color: #3b82f6; padding-top: 16px; font-weight: bold; border: none; }
  .bar-cell { width: 120px; }
  .bar { height: 14px; background: #3b82f6; border-radius: 2px; min-width: 2px; }
  .source { color: #555; font-size: 0.8em; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  button { background: #3b82f6; color: #fff; border: none; padding: 8px 16px; border-radius: 4px; cursor: pointer; font-family: monospace; margin-top: 12px; }
  button:hover { background: #2563eb; }
  #updated { color: #666; font-size: 0.85em; margin-top: 8px; }
</style></head><body>
<h1>SentryUSB Memory Usage</h1>
<table id="stats"></table>
<h2>Memory by Feature</h2>
<table id="features"><tr><th>Feature</th><th>Size</th><th>Objects</th><th class="bar-cell"></th><th>Top Source</th></tr></table>
<button onclick="load()">Refresh</button>
<div id="updated"></div>
<script>
function fmt(mb) { return mb >= 1 ? mb.toFixed(1) + ' MB' : (mb * 1024).toFixed(0) + ' KB'; }
function load() {
  fetch('/api/memory').then(r => r.json()).then(d => {
    let rows = '';
    const s = (cls, label, val) => '<tr><td'+(cls?' class="'+cls+'"':'')+'>'+label+'</td><td'+(cls?' class="'+cls+'"':'')+'>'+val+'</td></tr>';
    rows += s('section', 'HEADLINE', '');
    rows += s('big', 'Heap In Use', fmt(d.heap_in_use_mb));
    rows += s('big', 'Total From OS', fmt(d.total_sys_mb));
    rows += s('', 'Heap Alloc', fmt(d.heap_alloc_mb));
    rows += s('', 'Stack In Use', fmt(d.stack_in_use_mb));
    rows += s('section', 'HEAP DETAIL', '');
    rows += s('', 'Heap Sys (obtained)', fmt(d.heap_sys_mb));
    rows += s('', 'Heap Idle', fmt(d.heap_idle_mb));
    rows += s('', 'Heap Released to OS', fmt(d.heap_released_mb));
    rows += s('', 'Live Objects', d.heap_objects.toLocaleString());
    rows += s('section', 'RUNTIME', '');
    rows += s('', 'Goroutines', d.num_goroutines);
    rows += s('', 'GC Runs', d.num_gc);
    rows += s('', 'GC CPU %', d.gc_cpu_percent.toFixed(2) + '%');
    rows += s('', 'Go Version', d.go_version);
    document.getElementById('stats').innerHTML = rows;

    // Feature breakdown
    let frows = '<tr><th>Feature</th><th>Size</th><th>Objects</th><th class="bar-cell"></th><th>Top Source</th></tr>';
    const maxMB = d.features.length > 0 ? d.features[0].alloc_mb : 1;
    for (const f of d.features) {
      const pct = maxMB > 0 ? (f.alloc_mb / maxMB * 100) : 0;
      frows += '<tr>';
      frows += '<td>' + f.name + '</td>';
      frows += '<td style="text-align:right;font-weight:bold">' + fmt(f.alloc_mb) + '</td>';
      frows += '<td style="text-align:right">' + f.objects.toLocaleString() + '</td>';
      frows += '<td class="bar-cell"><div class="bar" style="width:' + pct + '%"></div></td>';
      frows += '<td class="source">' + (f.source || '') + '</td>';
      frows += '</tr>';
    }
    document.getElementById('features').innerHTML = frows;
    document.getElementById('updated').textContent = 'Updated: ' + new Date().toLocaleTimeString();
  });
}
load();
</script></body></html>`)
}
