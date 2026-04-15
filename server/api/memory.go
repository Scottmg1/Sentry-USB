package api

import (
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
)

// GET /api/memory — human-readable memory breakdown
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

		// OS memory (what the OS sees as RSS, roughly)
		"total_alloc_mb":     round2(float64(m.TotalAlloc) / 1048576),
		"heap_released_mb":   round2(float64(m.HeapReleased) / 1048576),
		"heap_idle_mb":       round2(float64(m.HeapIdle) / 1048576),
		"gc_cpu_percent":     round2(m.GCCPUFraction * 100),
		"num_gc":             m.NumGC,
		"num_goroutines":     runtime.NumGoroutine(),
		"heap_objects":       m.HeapObjects,
		"mallocs":            m.Mallocs,
		"frees":              m.Frees,
		"go_version":         goVersion,

		// Breakdown
		"mspan_in_use_mb":    round2(float64(m.MSpanInuse) / 1048576),
		"mcache_in_use_mb":   round2(float64(m.MCacheInuse) / 1048576),
		"buck_hash_sys_mb":   round2(float64(m.BuckHashSys) / 1048576),
		"gc_sys_mb":          round2(float64(m.GCSys) / 1048576),
		"other_sys_mb":       round2(float64(m.OtherSys) / 1048576),

		// Explanation
		"_help": map[string]string{
			"heap_in_use_mb":  "Memory currently holding live Go objects",
			"heap_sys_mb":     "Total memory obtained from OS for heap",
			"heap_idle_mb":    "Heap memory not in use (some released to OS, some retained)",
			"heap_released_mb": "Memory returned to OS (not counted in RSS)",
			"total_sys_mb":    "Total memory obtained from OS (heap + stack + GC metadata)",
			"stack_in_use_mb": "Memory used by goroutine stacks",
			"heap_objects":    "Number of live objects on the heap",
		},
	})
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
  table { border-collapse: collapse; width: 100%; max-width: 600px; }
  td { padding: 6px 12px; border-bottom: 1px solid #333; }
  td:first-child { color: #888; }
  td:last-child { text-align: right; font-weight: bold; }
  .big { font-size: 1.2em; color: #f59e0b; }
  .section { color: #3b82f6; padding-top: 16px; font-weight: bold; border: none; }
  button { background: #3b82f6; color: #fff; border: none; padding: 8px 16px; border-radius: 4px; cursor: pointer; font-family: monospace; margin-top: 12px; }
  button:hover { background: #2563eb; }
  #updated { color: #666; font-size: 0.85em; margin-top: 8px; }
</style></head><body>
<h1>SentryUSB Memory Usage</h1>
<table id="stats"></table>
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
    rows += s('section', 'OTHER', '');
    rows += s('', 'GC Metadata', fmt(d.gc_sys_mb));
    rows += s('', 'MSpan', fmt(d.mspan_in_use_mb));
    rows += s('', 'BuckHash', fmt(d.buck_hash_sys_mb));
    rows += s('', 'Other Sys', fmt(d.other_sys_mb));
    document.getElementById('stats').innerHTML = rows;
    document.getElementById('updated').textContent = 'Updated: ' + new Date().toLocaleTimeString();
  });
}
load();
</script></body></html>`)
}
