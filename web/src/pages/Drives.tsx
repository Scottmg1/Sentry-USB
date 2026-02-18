import { useCallback, useEffect, useRef, useState } from "react"
import L from "leaflet"
import "leaflet/dist/leaflet.css"
import {
  MapPin, Navigation, Clock, Gauge, Play,
  Download, Upload, Loader2, ChevronLeft, Search, List, X,
} from "lucide-react"
import { cn } from "@/lib/utils"

// ── Types ──────────────────────────────────────────────────────────

interface DriveSummary {
  id: number
  date: string
  startTime: string
  endTime: string
  durationMs: number
  distanceMi: number
  distanceKm: number
  avgSpeedMph: number
  maxSpeedMph: number
  avgSpeedKmh: number
  maxSpeedKmh: number
  clipCount: number
  pointCount: number
  startPoint: [number, number] | null
  endPoint: [number, number] | null
}

interface DriveDetail extends Omit<DriveSummary, "startPoint" | "endPoint"> {
  points: [number, number, number, number][] // [lat, lng, timeMs, speedMps]
}

interface RouteOverview {
  id: number
  points: [number, number][]
}

interface DriveStats {
  drives_count: number
  routes_count: number
  processed_count: number
  total_distance_km: number
  total_distance_mi: number
  total_duration_ms: number
}

// ── Helpers ────────────────────────────────────────────────────────

function formatDuration(ms: number) {
  const totalMin = Math.floor(ms / 60000)
  const h = Math.floor(totalMin / 60)
  const m = totalMin % 60
  return h > 0 ? `${h}h ${m}m` : `${m} min`
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function formatTimeMs(ms: number) {
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", timeZone: "UTC" })
}

function formatDate(dateStr: string) {
  const d = new Date(dateStr + "T00:00:00")
  return d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric", year: "numeric" })
}

function haversine(lat1: number, lon1: number, lat2: number, lon2: number) {
  const R = 6371000
  const toRad = (d: number) => (d * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a = Math.sin(dLat / 2) ** 2 + Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return R * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
}


// ── Component ──────────────────────────────────────────────────────

export default function Drives() {
  const mapRef = useRef<HTMLDivElement>(null)
  const mapInstance = useRef<L.Map | null>(null)
  const overviewLayers = useRef<L.Polyline[]>([])
  const selectionLayers = useRef<L.Layer[]>([])
  const arrowMarker = useRef<L.Marker | null>(null)

  const [drives, setDrives] = useState<DriveSummary[]>([])
  const [stats, setStats] = useState<DriveStats | null>(null)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [selectedDrive, setSelectedDrive] = useState<DriveDetail | null>(null)
  const [search, setSearch] = useState("")
  const [metric, setMetric] = useState(() => {
    try { return localStorage.getItem("sentryusb_metric") === "true" } catch { return false }
  })
  const [sliderIdx, setSliderIdx] = useState(0)
  const [loading, setLoading] = useState(true)
  const [processing, setProcessing] = useState(false)
  const [processMsg, setProcessMsg] = useState("")
  const [mobileListOpen, setMobileListOpen] = useState(false)
  const [visibleCount, setVisibleCount] = useState(30)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const mobileSentinelRef = useRef<HTMLDivElement>(null)

  const fileInputRef = useRef<HTMLInputElement>(null)

  // ── Init map ──
  useEffect(() => {
    if (!mapRef.current || mapInstance.current) return
    const map = L.map(mapRef.current, { zoomControl: true }).setView([39.8, -98.6], 5)
    L.tileLayer("https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png", {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OSM</a> &copy; <a href="https://carto.com/">CARTO</a>',
      subdomains: "abcd",
      maxZoom: 20,
    }).addTo(map)
    mapInstance.current = map
    return () => { map.remove(); mapInstance.current = null }
  }, [])

  // ── Load data ──
  const loadDrives = useCallback(async () => {
    setLoading(true)
    try {
      const [drivesRes, statsRes] = await Promise.all([
        fetch("/api/drives"),
        fetch("/api/drives/stats"),
      ])
      const drivesData: DriveSummary[] = await drivesRes.json()
      const statsData: DriveStats = await statsRes.json()
      drivesData.sort((a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime())
      setDrives(drivesData)
      setStats(statsData)

      // Draw overview routes
      const routesRes = await fetch("/api/drives/routes")
      const routes: RouteOverview[] = await routesRes.json()
      drawOverview(routes)
    } catch {
      // API may not be available in dev
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadDrives() }, [loadDrives])

  function drawOverview(routes: RouteOverview[]) {
    const map = mapInstance.current
    if (!map) return
    // Clear old
    overviewLayers.current.forEach((l) => map.removeLayer(l))
    overviewLayers.current = []

    for (const r of routes) {
      if (r.points && r.points.length > 1) {
        const line = L.polyline(r.points as L.LatLngExpression[], {
          color: "#3b82f6", weight: 2, opacity: 0.4, smoothFactor: 1.5,
        }).addTo(map)
        ;(line as any)._driveId = r.id
        line.on("click", () => selectDrive(r.id))
        overviewLayers.current.push(line)
      }
    }
    if (overviewLayers.current.length > 0) {
      const group = L.featureGroup(overviewLayers.current)
      map.fitBounds(group.getBounds(), { padding: [40, 40] })
    }
  }

  function clearSelection() {
    const map = mapInstance.current
    if (!map) return
    selectionLayers.current.forEach((l) => map.removeLayer(l))
    selectionLayers.current = []
    if (arrowMarker.current) { map.removeLayer(arrowMarker.current); arrowMarker.current = null }
  }

  async function selectDrive(id: number) {
    setSelectedId(id)
    setSliderIdx(0)
    const map = mapInstance.current
    if (!map) return

    try {
      const res = await fetch(`/api/drives/${id}`)
      const data: DriveDetail = await res.json()
      setSelectedDrive(data)

      clearSelection()
      overviewLayers.current.forEach((l) => map.removeLayer(l))

      const pts = data.points
      if (!pts || pts.length < 2) return
      const latlngs = pts.map((p) => [p[0], p[1]] as L.LatLngExpression)

      const route = L.polyline(latlngs, { color: "#3b82f6", weight: 4, opacity: 1, smoothFactor: 0 }).addTo(map)
      selectionLayers.current.push(route)

      const startM = L.marker(latlngs[0], {
        icon: L.divIcon({ className: "", html: '<div style="width:10px;height:10px;border-radius:50%;background:#22c55e;border:2px solid #fff"></div>', iconSize: [10, 10], iconAnchor: [5, 5] }),
      }).addTo(map)
      const endM = L.marker(latlngs[latlngs.length - 1], {
        icon: L.divIcon({ className: "", html: '<div style="width:10px;height:10px;border-radius:50%;background:#ef4444;border:2px solid #fff"></div>', iconSize: [10, 10], iconAnchor: [5, 5] }),
      }).addTo(map)
      selectionLayers.current.push(startM, endM)

      const arrow = L.marker(latlngs[0], {
        icon: L.divIcon({
          className: "",
          html: '<div style="width:12px;height:12px;border-radius:50%;background:#3b82f6;border:2px solid #fff;box-shadow:0 0 6px rgba(59,130,246,0.6)"></div>',
          iconSize: [12, 12], iconAnchor: [6, 6],
        }),
      }).addTo(map)
      arrowMarker.current = arrow
      selectionLayers.current.push(arrow)

      map.fitBounds(route.getBounds(), { padding: [60, 60] as L.PointExpression })
    } catch {
      // ignore
    }
  }

  function goBack() {
    setSelectedId(null)
    setSelectedDrive(null)
    clearSelection()
    const map = mapInstance.current
    if (!map) return
    overviewLayers.current.forEach((l) => l.addTo(map))
    if (overviewLayers.current.length > 0) {
      map.fitBounds(L.featureGroup(overviewLayers.current).getBounds(), { padding: [40, 40] })
    }
  }

  function handleSlider(idx: number) {
    setSliderIdx(idx)
    if (!selectedDrive || !arrowMarker.current) return
    const pt = selectedDrive.points[idx]
    arrowMarker.current.setLatLng([pt[0], pt[1]])
  }

  // ── Process ──
  async function triggerProcess() {
    setProcessing(true)
    setProcessMsg("Starting...")
    try {
      const res = await fetch("/api/drives/process", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ throttle_ms: 15 }),
      })
      if (!res.ok) {
        const err = await res.json()
        setProcessMsg(`Error: ${err.error}`)
        setProcessing(false)
        return
      }
      setProcessMsg("Processing... check back shortly")
      // Poll status
      const poll = setInterval(async () => {
        try {
          const s = await fetch("/api/drives/status")
          const data = await s.json()
          if (!data.running) {
            clearInterval(poll)
            setProcessing(false)
            setProcessMsg("")
            loadDrives()
          }
        } catch { clearInterval(poll); setProcessing(false) }
      }, 3000)
    } catch {
      setProcessMsg("Failed to start processing")
      setProcessing(false)
    }
  }

  // ── Upload / Download ──
  async function handleUpload(file: File) {
    try {
      const text = await file.text()
      const res = await fetch("/api/drives/data/upload", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: text,
      })
      if (res.ok) loadDrives()
    } catch { /* ignore */ }
  }

  // ── Derived ──
  // Reset visible count when search changes
  useEffect(() => { setVisibleCount(30) }, [search])

  // IntersectionObserver for lazy loading more drives
  useEffect(() => {
    const cb: IntersectionObserverCallback = (entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        setVisibleCount((c) => c + 30)
      }
    }
    const obs = new IntersectionObserver(cb, { rootMargin: "200px" })
    if (sentinelRef.current) obs.observe(sentinelRef.current)
    if (mobileSentinelRef.current) obs.observe(mobileSentinelRef.current)
    return () => obs.disconnect()
  }, [drives, search])

  const filtered = search
    ? drives.filter((d) => d.date.includes(search) || formatDate(d.date).toLowerCase().includes(search.toLowerCase()))
    : drives
  const visible = filtered.slice(0, visibleCount)

  const dist = (d: DriveSummary | DriveDetail) => metric ? `${d.distanceKm} km` : `${d.distanceMi} mi`
  const avgSpd = (d: DriveSummary | DriveDetail) => metric ? `${d.avgSpeedKmh} km/h` : `${d.avgSpeedMph} mph`
  const maxSpd = (d: DriveSummary | DriveDetail) => metric ? `${d.maxSpeedKmh} km/h` : `${d.maxSpeedMph} mph`
  const mpsToDisplay = (mps: number) => metric ? (mps * 3.6).toFixed(1) : (mps * 2.23694).toFixed(1)
  const distUnit = metric ? "km" : "mi"
  const speedUnit = metric ? "km/h" : "mph"

  // Cumulative distances for slider
  const cumDist = selectedDrive
    ? selectedDrive.points.reduce<number[]>((acc, pt, i) => {
        if (i === 0) return [0]
        const prev = selectedDrive.points[i - 1]
        acc.push(acc[i - 1] + haversine(prev[0], prev[1], pt[0], pt[1]))
        return acc
      }, [])
    : []

  const sliderPt = selectedDrive?.points[sliderIdx]
  const sliderDist = cumDist[sliderIdx] ?? 0
  const sliderDistDisplay = metric ? (sliderDist / 1000).toFixed(2) : (sliderDist / 1609.344).toFixed(2)

  const totalDist = stats ? (metric ? stats.total_distance_km.toFixed(1) : stats.total_distance_mi.toFixed(1)) : "0"
  const totalDur = stats ? formatDuration(stats.total_duration_ms) : "0"

  return (
    <div className="flex h-[calc(100vh-5rem)] flex-col gap-4 md:h-[calc(100vh-3rem)]">
      {/* Header bar */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <MapPin className="h-5 w-5 text-blue-400" />
          <h1 className="text-lg font-semibold text-slate-100">Drive Map</h1>
          {stats && (
            <div className="hidden items-center gap-4 text-xs text-slate-500 sm:flex">
              <span>Drives: <span className="font-semibold text-blue-400">{stats.drives_count}</span></span>
              <span>Total: <span className="font-semibold text-blue-400">{totalDist} {distUnit}</span></span>
              <span>Time: <span className="font-semibold text-blue-400">{totalDur}</span></span>
            </div>
          )}
        </div>
        <div className="flex items-center gap-2">
          {/* Unit toggle */}
          <div className="flex overflow-hidden rounded-lg border border-white/10">
            <button onClick={() => { setMetric(false); try { localStorage.setItem("sentryusb_metric", "false") } catch {} fetch("/api/config/preference", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: "unit", value: "mi" }) }).catch(() => {}) }} className={cn("px-2.5 py-1 text-xs font-medium transition-colors", !metric ? "bg-blue-500 text-white" : "text-slate-500 hover:text-slate-300")}>MI</button>
            <button onClick={() => { setMetric(true); try { localStorage.setItem("sentryusb_metric", "true") } catch {} fetch("/api/config/preference", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: "unit", value: "km" }) }).catch(() => {}) }} className={cn("px-2.5 py-1 text-xs font-medium transition-colors", metric ? "bg-blue-500 text-white" : "text-slate-500 hover:text-slate-300")}>KM</button>
          </div>
          {/* Process */}
          <button onClick={triggerProcess} disabled={processing} className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 transition-colors hover:bg-white/10 disabled:opacity-50">
            {processing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
            Process
          </button>
          {/* Download */}
          <a href="/api/drives/data/download" className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 transition-colors hover:bg-white/10">
            <Download className="h-3 w-3" /> Export
          </a>
          {/* Upload */}
          <button onClick={() => fileInputRef.current?.click()} className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 transition-colors hover:bg-white/10">
            <Upload className="h-3 w-3" /> Import
          </button>
          <input ref={fileInputRef} type="file" accept=".json" className="hidden" onChange={(e) => { const f = e.target.files?.[0]; if (f) handleUpload(f) }} />
        </div>
      </div>

      {processMsg && <p className="text-xs text-amber-400">{processMsg}</p>}

      {/* Main content: sidebar + map */}
      <div className="relative flex flex-1 gap-4 overflow-hidden rounded-xl border border-white/5">
        {/* Mobile drive list toggle */}
        <button
          onClick={() => setMobileListOpen(!mobileListOpen)}
          className="absolute left-3 bottom-3 z-[1000] flex items-center gap-1.5 rounded-lg border border-white/10 bg-slate-950/90 px-3 py-1.5 text-xs font-medium text-slate-300 backdrop-blur-sm transition-colors hover:bg-slate-900 md:hidden"
        >
          {mobileListOpen ? <X className="h-3.5 w-3.5" /> : <List className="h-3.5 w-3.5" />}
          {mobileListOpen ? "Hide Drives" : `Drives (${drives.length})`}
        </button>

        {/* Mobile drive list overlay */}
        {mobileListOpen && (
          <div className="absolute inset-0 z-[1000] flex flex-col overflow-hidden bg-slate-950/95 backdrop-blur-sm md:hidden">
            <div className="border-b border-white/5 p-3">
              <div className="relative">
                <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-slate-600" />
                <input
                  type="text"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Filter by date..."
                  className="w-full rounded-lg border border-white/10 bg-white/5 py-1.5 pl-8 pr-3 text-xs text-slate-200 placeholder-slate-600 outline-none focus:border-blue-500/50"
                />
              </div>
            </div>
            <div className="flex-1 overflow-y-auto">
              {loading && <p className="p-4 text-center text-xs text-slate-600">Loading drives...</p>}
              {!loading && filtered.length === 0 && <p className="p-4 text-center text-xs text-slate-600">No drives found</p>}
              {(() => {
                let cd = ""
                return visible.map((d) => {
                  const sh = d.date !== cd
                  cd = d.date
                  return (
                    <div key={d.id}>
                      {sh && (
                        <div className="sticky top-0 z-10 bg-slate-950/90 px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-slate-600">
                          {formatDate(d.date)}
                        </div>
                      )}
                      <button
                        onClick={() => { selectDrive(d.id); setMobileListOpen(false) }}
                        className={cn(
                          "w-full border-b border-white/[0.03] px-3 py-2.5 text-left transition-colors hover:bg-white/[0.04]",
                          selectedId === d.id && "border-l-2 border-l-blue-500 bg-blue-500/10"
                        )}
                      >
                        <p className="text-sm font-medium text-slate-200">
                          {formatTime(d.startTime)} — {formatTime(d.endTime)}
                        </p>
                        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-slate-500">
                          <span>{dist(d)}</span>
                          <span>{formatDuration(d.durationMs)}</span>
                          <span>{avgSpd(d)}</span>
                        </div>
                      </button>
                    </div>
                  )
                })
              })()}
              {visibleCount < filtered.length && <div ref={mobileSentinelRef} className="py-4 text-center text-[10px] text-slate-600">Loading more...</div>}
            </div>
          </div>
        )}

        {/* Desktop Sidebar */}
        <div className="hidden w-72 shrink-0 flex-col overflow-hidden border-r border-white/5 bg-white/[0.02] md:flex">
          <div className="border-b border-white/5 p-3">
            <div className="relative">
              <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-slate-600" />
              <input
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Filter by date..."
                className="w-full rounded-lg border border-white/10 bg-white/5 py-1.5 pl-8 pr-3 text-xs text-slate-200 placeholder-slate-600 outline-none focus:border-blue-500/50"
              />
            </div>
          </div>
          <div className="flex-1 overflow-y-auto">
            {loading && <p className="p-4 text-center text-xs text-slate-600">Loading drives...</p>}
            {!loading && filtered.length === 0 && <p className="p-4 text-center text-xs text-slate-600">No drives found</p>}
            {(() => {
              let currentDate = ""
              return visible.map((d) => {
                const showHeader = d.date !== currentDate
                currentDate = d.date
                return (
                  <div key={d.id}>
                    {showHeader && (
                      <div className="sticky top-0 z-10 bg-slate-950/90 px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-slate-600">
                        {formatDate(d.date)}
                      </div>
                    )}
                    <button
                      onClick={() => selectDrive(d.id)}
                      className={cn(
                        "w-full border-b border-white/[0.03] px-3 py-2.5 text-left transition-colors hover:bg-white/[0.04]",
                        selectedId === d.id && "border-l-2 border-l-blue-500 bg-blue-500/10"
                      )}
                    >
                      <p className="text-sm font-medium text-slate-200">
                        {formatTime(d.startTime)} — {formatTime(d.endTime)}
                      </p>
                      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-slate-500">
                        <span>{dist(d)}</span>
                        <span>{formatDuration(d.durationMs)}</span>
                        <span>{avgSpd(d)}</span>
                      </div>
                    </button>
                  </div>
                )
              })
            })()}
            {visibleCount < filtered.length && <div ref={sentinelRef} className="py-4 text-center text-[10px] text-slate-600">Loading more...</div>}
          </div>
        </div>

        {/* Map */}
        <div className="relative flex-1">
          <div ref={mapRef} className="h-full w-full" />

          {loading && (
            <div className="absolute inset-0 z-[1000] flex items-center justify-center bg-black/70">
              <p className="text-sm text-slate-400">Loading drives...</p>
            </div>
          )}

          {/* Back button */}
          {selectedId !== null && (
            <button
              onClick={goBack}
              className="absolute left-3 top-3 z-[1000] flex items-center gap-1.5 rounded-lg border border-white/10 bg-slate-950/90 px-3 py-1.5 text-xs font-medium text-slate-300 backdrop-blur-sm transition-colors hover:bg-slate-900"
            >
              <ChevronLeft className="h-3.5 w-3.5" /> All Drives
            </button>
          )}

          {/* Detail panel */}
          {selectedDrive && (
            <div className="absolute inset-x-0 bottom-0 z-[1000] border-t border-white/10 bg-slate-950/90 px-4 py-3 backdrop-blur-md">
              <div className="mb-2 flex flex-wrap gap-x-6 gap-y-1">
                <Stat icon={<Navigation className="h-3 w-3" />} label="Distance" value={dist(selectedDrive)} highlight />
                <Stat icon={<Clock className="h-3 w-3" />} label="Duration" value={formatDuration(selectedDrive.durationMs)} />
                <Stat label="Start" value={formatTime(selectedDrive.startTime)} />
                <Stat label="End" value={formatTime(selectedDrive.endTime)} />
                <Stat icon={<Gauge className="h-3 w-3" />} label="Avg" value={avgSpd(selectedDrive)} />
                <Stat label="Max" value={maxSpd(selectedDrive)} highlight />
              </div>

              {/* Slider */}
              <div className="flex items-center gap-3">
                <span className="min-w-[52px] text-[10px] tabular-nums text-slate-500">
                  {selectedDrive.points.length > 0 ? formatTimeMs(selectedDrive.points[0][2]) : "--"}
                </span>
                <input
                  type="range"
                  min={0}
                  max={selectedDrive.points.length - 1}
                  value={sliderIdx}
                  onChange={(e) => handleSlider(parseInt(e.target.value))}
                  className="h-1 flex-1 cursor-pointer appearance-none rounded-full bg-slate-800 accent-blue-500 [&::-webkit-slider-thumb]:h-3.5 [&::-webkit-slider-thumb]:w-3.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-500 [&::-webkit-slider-thumb]:shadow-[0_0_6px_rgba(59,130,246,0.5)]"
                />
                <span className="min-w-[52px] text-right text-[10px] tabular-nums text-slate-500">
                  {selectedDrive.points.length > 0 ? formatTimeMs(selectedDrive.points[selectedDrive.points.length - 1][2]) : "--"}
                </span>
              </div>

              {sliderPt && (
                <div className="mt-1.5 flex justify-center gap-5 text-[11px] text-slate-500">
                  <span>Time: <span className="font-semibold text-blue-400">{formatTimeMs(sliderPt[2])}</span></span>
                  <span>Speed: <span className="font-semibold text-blue-400">{mpsToDisplay(sliderPt[3])} {speedUnit}</span></span>
                  <span>Dist: <span className="font-semibold text-blue-400">{sliderDistDisplay} {distUnit}</span></span>
                  <span>Pt: <span className="font-semibold text-blue-400">{sliderIdx + 1}/{selectedDrive.points.length}</span></span>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function Stat({ icon, label, value, highlight }: { icon?: React.ReactNode; label: string; value: string; highlight?: boolean }) {
  return (
    <div className="flex items-center gap-1.5">
      {icon && <span className="text-slate-600">{icon}</span>}
      <span className="text-[10px] uppercase tracking-wider text-slate-600">{label}</span>
      <span className={cn("text-xs font-semibold", highlight ? "text-blue-400" : "text-slate-300")}>{value}</span>
    </div>
  )
}
