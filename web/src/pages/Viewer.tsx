import { useState, useEffect, useRef, useCallback } from "react"
import {
  Video, Play, Pause, SkipBack, SkipForward, Loader2,
  Maximize, Minimize, Trash2,
  Download, ChevronLeft, ChevronRight, AlertTriangle,
  Zap, Eye, Car, Hand, ExternalLink, X,
} from "lucide-react"
import { cn } from "@/lib/utils"
import type { ClipEntry, ClipGroup, EventMeta } from "@/lib/api"

interface ClipSet {
  timestamp: string
  cameras: Record<string, string>
}

// Camera grid layout: pillars top, repeaters bottom (matches Sentry Six)
const CAMERAS_GRID = ["left_pillar", "front", "right_pillar", "left_repeater", "back", "right_repeater"]
const CAMERA_LABELS: Record<string, string> = {
  front: "Front",
  back: "Rear",
  left_repeater: "Left Repeater",
  right_repeater: "Right Repeater",
  left_pillar: "Left Pillar",
  right_pillar: "Right Pillar",
}
const CAMERA_SHORT: Record<string, string> = {
  front: "Front",
  back: "Rear",
  left_repeater: "L. Rep",
  right_repeater: "R. Rep",
  left_pillar: "L. Pillar",
  right_pillar: "R. Pillar",
}

const SPEED_OPTIONS = [0.5, 1, 1.5, 2, 4]

const EVENT_REASONS: Record<string, { label: string; icon: typeof Zap }> = {
  sentry_aware_object_detection: { label: "Object Detected", icon: Eye },
  vehicle_auto_emergency_braking: { label: "Emergency Braking", icon: AlertTriangle },
  user_interaction_dashcam_icon_tapped: { label: "Manual Save", icon: Hand },
  user_interaction_dashcam_panel_save: { label: "Manual Save", icon: Hand },
  user_interaction_dashcam_launcher_action_tapped: { label: "Manual Save", icon: Hand },
  user_interaction_honk: { label: "Honk", icon: Zap },
  sentry_aware_accel: { label: "Acceleration", icon: Zap },
  collision: { label: "Collision", icon: AlertTriangle },
  user_interaction_dashcam: { label: "Manual Save", icon: Hand },
}

function formatEventReason(reason: string): { label: string; Icon: typeof Zap } {
  const mapped = EVENT_REASONS[reason]
  if (mapped) return { label: mapped.label, Icon: mapped.icon }
  return {
    label: reason.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()),
    Icon: Zap,
  }
}

function groupByTimestamp(files: string[], basePath: string): ClipSet[] {
  const map = new Map<string, Record<string, string>>()
  for (const f of files) {
    const match = f.match(/^(.+)-(front|back|left_repeater|right_repeater|left_pillar|right_pillar)\.mp4$/)
    if (!match) continue
    const [, ts, cam] = match
    if (!map.has(ts)) map.set(ts, {})
    map.get(ts)![cam] = `${basePath}/${f}`
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([timestamp, cameras]) => ({ timestamp, cameras }))
}

function formatTime(s: number): string {
  if (!Number.isFinite(s) || s < 0) return "0:00"
  const m = Math.floor(s / 60)
  const sec = Math.floor(s % 60)
  return `${m}:${sec.toString().padStart(2, "0")}`
}

function formatClipDate(date: string): string {
  // Tesla format: 2025-02-22_17-58-00 → Feb 22, 5:58 PM
  const match = date.match(/^(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{2})$/)
  if (!match) return date
  const [, y, mo, d, h, mi] = match
  const dt = new Date(+y, +mo - 1, +d, +h, +mi)
  return dt.toLocaleDateString("en-US", { month: "short", day: "numeric" }) +
    ", " + dt.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" })
}

export default function Viewer() {
  const [groups, setGroups] = useState<ClipGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [activeCategory, setActiveCategory] = useState("RecentClips")
  const [selectedClip, setSelectedClip] = useState<ClipEntry | null>(null)
  const [clipSets, setClipSets] = useState<ClipSet[]>([])
  const [currentSetIdx, setCurrentSetIdx] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [focusedCamera, setFocusedCamera] = useState<string | null>(null)
  const [playbackSpeed, setPlaybackSpeed] = useState(1)
  const [currentTime, setCurrentTime] = useState(0)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => window.innerWidth < 768)
  const [showPromo, setShowPromo] = useState(true)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [segmentDurations, setSegmentDurations] = useState<number[]>([])

  const videoRefs = useRef<Map<string, HTMLVideoElement>>(new Map())
  const masterVideoRef = useRef<HTMLVideoElement | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const seekBarRef = useRef<HTMLDivElement>(null)
  const animFrameRef = useRef<number>(0)
  const pendingSeekRef = useRef<number | null>(null)

  // Continuous timeline across all segments
  const priorSegmentsTime = segmentDurations.slice(0, currentSetIdx).reduce((a, b) => a + b, 0)
  const globalTime = priorSegmentsTime + currentTime
  const totalDuration = segmentDurations.reduce((a, b) => a + b, 0)

  // Fetch clips
  useEffect(() => {
    fetch("/api/clips")
      .then((r) => r.json())
      .then((data: ClipGroup[]) => { setGroups(data); setLoading(false) })
      .catch(() => setLoading(false))
  }, [])

  const activeGroup = groups.find((g) => g.name === activeCategory)

  // When clip changes, build clip sets
  useEffect(() => {
    if (selectedClip) {
      const sets = groupByTimestamp(selectedClip.files, selectedClip.path)
      setClipSets(sets)
      setCurrentSetIdx(0)
      setPlaying(false)
      setFocusedCamera(null)
      pendingSeekRef.current = null
      setCurrentTime(0)
    }
  }, [selectedClip])

  // Preload segment durations for continuous timeline
  useEffect(() => {
    if (!clipSets.length) { setSegmentDurations([]); return }
    const durations = new Array(clipSets.length).fill(60)
    setSegmentDurations([...durations])
    const cleanups: (() => void)[] = []
    clipSets.forEach((set, i) => {
      const url = set.cameras["front"] || Object.values(set.cameras)[0]
      if (!url) return
      const v = document.createElement("video")
      v.preload = "metadata"
      v.src = url
      v.onloadedmetadata = () => {
        if (Number.isFinite(v.duration)) {
          durations[i] = v.duration
          setSegmentDurations([...durations])
        }
      }
      cleanups.push(() => { v.src = ""; v.remove() })
    })
    return () => cleanups.forEach((c) => c())
  }, [clipSets])

  const currentSet = clipSets[currentSetIdx]

  // Set master video ref (front camera preferred)
  useEffect(() => {
    if (!currentSet) { masterVideoRef.current = null; return }
    const front = videoRefs.current.get("front")
    if (front) { masterVideoRef.current = front; return }
    // Fallback to first available
    for (const v of videoRefs.current.values()) {
      if (v) { masterVideoRef.current = v; return }
    }
    masterVideoRef.current = null
  }, [currentSet, currentSetIdx])

  // Time update animation loop
  useEffect(() => {
    function tick() {
      const master = masterVideoRef.current
      if (master) {
        setCurrentTime(master.currentTime)
      }
      animFrameRef.current = requestAnimationFrame(tick)
    }
    animFrameRef.current = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(animFrameRef.current)
  }, [])

  // Apply playback speed to all videos
  useEffect(() => {
    videoRefs.current.forEach((v) => { if (v) v.playbackRate = playbackSpeed })
  }, [playbackSpeed, currentSetIdx])


  // Auto-advance to next clip set
  const handleVideoEnded = useCallback(() => {
    if (currentSetIdx < clipSets.length - 1) {
      setCurrentSetIdx((i) => i + 1)
      // Will auto-play via the effect below
    } else {
      setPlaying(false)
    }
  }, [currentSetIdx, clipSets.length])

  // Auto-play when advancing clip sets
  useEffect(() => {
    if (!currentSet || !playing) return
    const timer = setTimeout(() => {
      videoRefs.current.forEach((v) => {
        if (v) v.play().catch(() => { })
      })
    }, 100)
    return () => clearTimeout(timer)
  }, [currentSetIdx]) // eslint-disable-line react-hooks/exhaustive-deps

  // Sync all videos to master on seek
  const syncVideos = useCallback((time: number) => {
    videoRefs.current.forEach((v) => {
      if (v) v.currentTime = time
    })
  }, [])

  const togglePlay = useCallback(() => {
    const wasPlaying = playing
    videoRefs.current.forEach((v) => {
      if (!v) return
      if (wasPlaying) v.pause()
      else v.play().catch(() => { })
    })
    setPlaying(!wasPlaying)
  }, [playing])

  const seekToGlobal = useCallback((globalT: number) => {
    const clamped = Math.max(0, Math.min(globalT, totalDuration))
    let remaining = clamped
    for (let i = 0; i < segmentDurations.length; i++) {
      if (remaining <= segmentDurations[i] + 0.05 || i === segmentDurations.length - 1) {
        const offset = Math.min(remaining, segmentDurations[i])
        if (i !== currentSetIdx) {
          pendingSeekRef.current = offset
          setCurrentSetIdx(i)
        } else {
          syncVideos(offset)
        }
        return
      }
      remaining -= segmentDurations[i]
    }
  }, [segmentDurations, totalDuration, currentSetIdx, syncVideos])

  const skip = useCallback((seconds: number) => {
    seekToGlobal(globalTime + seconds)
  }, [globalTime, seekToGlobal])

  const handleSeek = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const bar = seekBarRef.current
    if (!bar || totalDuration <= 0) return
    const rect = bar.getBoundingClientRect()
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
    seekToGlobal(pct * totalDuration)
  }, [totalDuration, seekToGlobal])

  // Fullscreen toggle
  const toggleFullscreen = useCallback(() => {
    if (!containerRef.current) return
    if (document.fullscreenElement) {
      document.exitFullscreen()
    } else {
      containerRef.current.requestFullscreen()
    }
  }, [])

  useEffect(() => {
    const onFS = () => setIsFullscreen(!!document.fullscreenElement)
    document.addEventListener("fullscreenchange", onFS)
    return () => document.removeEventListener("fullscreenchange", onFS)
  }, [])

  // Keyboard shortcuts
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
      switch (e.key) {
        case " ":
          e.preventDefault()
          togglePlay()
          break
        case "ArrowLeft":
          e.preventDefault()
          skip(e.shiftKey ? -15 : -5)
          break
        case "ArrowRight":
          e.preventDefault()
          skip(e.shiftKey ? 15 : 5)
          break
        case "f":
          e.preventDefault()
          toggleFullscreen()
          break
        case "Escape":
          if (focusedCamera) { e.preventDefault(); setFocusedCamera(null) }
          break
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [togglePlay, skip, toggleFullscreen, focusedCamera]) // eslint-disable-line react-hooks/exhaustive-deps

  // Delete clip
  async function handleDeleteClip(clip: ClipEntry) {
    try {
      const fullPath = `/mutable/TeslaCam/${activeCategory}/${clip.date}`
      await fetch(`/api/files?path=${encodeURIComponent(fullPath)}`, { method: "DELETE" })
      setGroups((prev) =>
        prev.map((g) =>
          g.name === activeCategory
            ? { ...g, clips: g.clips.filter((c) => c.date !== clip.date) }
            : g
        )
      )
      if (selectedClip?.date === clip.date) {
        setSelectedClip(null)
        setClipSets([])
      }
      setDeleteConfirm(null)
    } catch { /* ignore */ }
  }

  // Download clip set as zip
  function handleDownload() {
    if (!selectedClip) return
    const fullPath = `/mutable/TeslaCam/${activeCategory}/${selectedClip.date}`
    window.open(`/api/files/download-zip?path=${encodeURIComponent(fullPath)}`, "_blank")
  }

  // Register video ref
  const setVideoRef = useCallback((cam: string) => (el: HTMLVideoElement | null) => {
    if (el) {
      videoRefs.current.set(cam, el)
      el.playbackRate = playbackSpeed
    } else {
      videoRefs.current.delete(cam)
    }
  }, [playbackSpeed])

  const progress = totalDuration > 0 ? (globalTime / totalDuration) * 100 : 0

  // Event metadata
  const eventMeta = selectedClip?.event
  const triggeredCamera = eventMeta?.camera

  // Camera list for rendering
  const camerasToShow = focusedCamera ? [focusedCamera] : CAMERAS_GRID

  const categoryLabels: Record<string, string> = {
    RecentClips: "Recent",
    SavedClips: "Saved",
    SentryClips: "Sentry",
  }
  const categoryCounts = groups.reduce<Record<string, number>>((acc, g) => {
    acc[g.name] = g.clips.length
    return acc
  }, {})

  return (
    <div
      ref={containerRef}
      className={cn(
        "flex flex-col",
        isFullscreen ? "h-screen bg-slate-950 p-2" : "h-[calc(100vh-120px)] md:h-[calc(100vh-96px)]"
      )}
    >
      {/* Header */}
      {!isFullscreen && (
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Viewer</h1>
            <p className="mt-0.5 text-sm text-slate-500">
              Multi-camera clip viewer
              <span className="ml-2 hidden text-[10px] text-slate-600 md:inline">
                Space: play &middot; ←→: seek &middot; F: fullscreen
              </span>
            </p>
          </div>
        </div>
      )}

      {/* Category tabs */}
      <div className={cn("mb-2 flex items-center gap-1", isFullscreen && "mb-1")}>
        {["RecentClips", "SavedClips", "SentryClips"].map((cat) => (
          <button
            key={cat}
            onClick={() => { setActiveCategory(cat); setSelectedClip(null); setClipSets([]) }}
            className={cn(
              "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
              activeCategory === cat
                ? "bg-blue-500/15 text-blue-400"
                : "text-slate-500 hover:bg-white/5 hover:text-slate-300"
            )}
          >
            {categoryLabels[cat]}
            {(categoryCounts[cat] ?? 0) > 0 && (
              <span className="ml-1.5 rounded-full bg-white/5 px-1.5 py-0.5 text-[10px] tabular-nums text-slate-500">
                {categoryCounts[cat]}
              </span>
            )}
          </button>
        ))}

        {/* Sidebar toggle */}
        <button
          onClick={() => setSidebarCollapsed((c) => !c)}
          className="ml-auto rounded-lg p-1.5 text-slate-500 transition-colors hover:bg-white/5 hover:text-slate-300"
          title={sidebarCollapsed ? "Show clip browser" : "Hide clip browser"}
        >
          {sidebarCollapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
        </button>
      </div>

      <div className="flex min-h-0 flex-1 gap-2">
        {/* Clip browser sidebar */}
        {!sidebarCollapsed && (
          <div className="glass-card flex w-56 shrink-0 flex-col overflow-hidden">
            {/* Event info for selected clip */}
            {selectedClip && eventMeta?.reason && (
              <div className="border-b border-white/5 p-2">
                <EventBadge event={eventMeta} />
              </div>
            )}

            {/* Clip list */}
            <div className="flex-1 overflow-y-auto p-1.5">
              {loading ? (
                <div className="flex items-center justify-center p-8">
                  <Loader2 className="h-5 w-5 animate-spin text-slate-500" />
                </div>
              ) : activeGroup && activeGroup.clips.length > 0 ? (
                activeGroup.clips.map((clip) => {
                  const isSelected = selectedClip?.date === clip.date
                  const eventInfo = clip.event
                  const { label: reasonLabel } = eventInfo?.reason
                    ? formatEventReason(eventInfo.reason) : { label: "" }
                  return (
                    <div key={clip.date} className="group relative">
                      <button
                        onClick={() => setSelectedClip(clip)}
                        className={cn(
                          "w-full rounded-lg px-2.5 py-2 text-left transition-colors",
                          isSelected
                            ? "bg-blue-500/15 text-blue-400"
                            : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
                        )}
                      >
                        <div className="text-xs font-medium">{formatClipDate(clip.date)}</div>
                        <div className="mt-0.5 flex items-center gap-1.5">
                          <span className="text-[10px] text-slate-600">
                            {clip.files.length} files
                          </span>
                          {reasonLabel && (
                            <span className={cn(
                              "rounded px-1 py-0.5 text-[9px] font-medium",
                              activeCategory === "SentryClips"
                                ? "bg-red-500/15 text-red-400"
                                : "bg-amber-500/15 text-amber-400"
                            )}>
                              {reasonLabel}
                            </span>
                          )}
                        </div>
                        {eventInfo?.city && (
                          <div className="mt-0.5 truncate text-[10px] text-slate-600">
                            {eventInfo.city}
                          </div>
                        )}
                      </button>
                      {/* Delete button */}
                      <button
                        onClick={(e) => { e.stopPropagation(); setDeleteConfirm(clip.date) }}
                        className="absolute right-1 top-1 hidden rounded p-0.5 text-slate-600 transition-colors hover:bg-red-500/15 hover:text-red-400 group-hover:block"
                        title="Delete clip"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                      {/* Delete confirmation */}
                      {deleteConfirm === clip.date && (
                        <div className="mx-1 mb-1 flex items-center gap-1 rounded-md bg-red-500/10 px-2 py-1.5">
                          <span className="flex-1 text-[10px] text-red-400">Delete this clip?</span>
                          <button
                            onClick={() => handleDeleteClip(clip)}
                            className="rounded bg-red-500/20 px-2 py-0.5 text-[10px] font-medium text-red-400 hover:bg-red-500/30"
                          >
                            Yes
                          </button>
                          <button
                            onClick={() => setDeleteConfirm(null)}
                            className="rounded bg-white/5 px-2 py-0.5 text-[10px] text-slate-400 hover:bg-white/10"
                          >
                            No
                          </button>
                        </div>
                      )}
                    </div>
                  )
                })
              ) : (
                <div className="flex flex-col items-center justify-center py-8 text-center">
                  <Video className="mb-2 h-8 w-8 text-slate-700" />
                  <p className="text-xs text-slate-600">No {categoryLabels[activeCategory]?.toLowerCase()} clips</p>
                </div>
              )}
            </div>

            {/* Sentry Six promo */}
            {showPromo && (
              <div className="border-t border-white/5 p-2">
                <div className="relative rounded-lg bg-gradient-to-r from-blue-500/10 to-purple-500/10 p-2.5">
                  <button
                    onClick={() => setShowPromo(false)}
                    className="absolute right-1 top-1 rounded p-0.5 text-slate-600 hover:text-slate-400"
                  >
                    <X className="h-3 w-3" />
                  </button>
                  <div className="flex items-start gap-2">
                    <Car className="mt-0.5 h-4 w-4 shrink-0 text-blue-400" />
                    <div>
                      <p className="text-[11px] font-medium text-slate-300">
                        Want more? Try Sentry Six
                      </p>
                      <p className="mt-0.5 text-[10px] leading-tight text-slate-500">
                        SEI telemetry, GPS maps, export with overlays, and more.
                      </p>
                      <a
                        href="https://github.com/ChadR23/Sentry-Six"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="mt-1.5 inline-flex items-center gap-1 text-[10px] font-medium text-blue-400 transition-colors hover:text-blue-300"
                      >
                        Learn more <ExternalLink className="h-2.5 w-2.5" />
                      </a>
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Video area */}
        <div className="flex min-h-0 flex-1 flex-col">
          {currentSet ? (
            <>
              {/* Camera grid */}
              <div
                className={cn(
                  "relative min-h-0 flex-1",
                  focusedCamera ? "" : "grid grid-cols-2 grid-rows-3 gap-0.5 md:grid-cols-3 md:grid-rows-2"
                )}
              >
                {camerasToShow.map((cam) => {
                  const isTriggered = triggeredCamera === cam
                  const hasFocus = focusedCamera === cam
                  return (
                    <div
                      key={cam}
                      className={cn(
                        "relative cursor-pointer overflow-hidden rounded-md bg-black transition-all",
                        hasFocus && "h-full w-full",
                        isTriggered && !hasFocus && "ring-1 ring-amber-500/60",
                      )}
                      onClick={() => setFocusedCamera(hasFocus ? null : cam)}
                    >
                      {currentSet.cameras[cam] ? (
                        <video
                          ref={setVideoRef(cam)}
                          key={`${currentSetIdx}-${cam}`}
                          src={currentSet.cameras[cam]}
                          className="h-full w-full object-contain"
                          muted
                          playsInline
                          preload="auto"
                          onEnded={cam === "front" ? handleVideoEnded : undefined}
                          onLoadedData={(e) => {
                            const v = e.currentTarget
                            v.playbackRate = playbackSpeed
                            if (pendingSeekRef.current !== null) {
                              v.currentTime = pendingSeekRef.current
                              if (cam === "front" || !currentSet.cameras["front"]) pendingSeekRef.current = null
                            }
                            if (playing) v.play().catch(() => { })
                          }}
                        />
                      ) : (
                        <div className="flex h-full items-center justify-center">
                          <Video className="h-6 w-6 text-slate-700" />
                        </div>
                      )}
                      {/* Camera label */}
                      <span
                        className={cn(
                          "absolute bottom-1 left-1 rounded px-1.5 py-0.5 text-[10px] font-medium",
                          isTriggered
                            ? "bg-amber-500/80 text-black"
                            : "bg-black/60 text-slate-400"
                        )}
                      >
                        {hasFocus ? CAMERA_LABELS[cam] : CAMERA_SHORT[cam]}
                        {isTriggered && " ⚡"}
                      </span>
                      {/* Focus hint */}
                      {hasFocus && (
                        <span className="absolute right-1 top-1 rounded bg-black/60 px-1.5 py-0.5 text-[10px] text-slate-500">
                          Click to exit &middot; ESC
                        </span>
                      )}
                    </div>
                  )
                })}
              </div>

              {/* Transport bar */}
              <div className="glass-card mt-1 p-2">
                {/* Seek bar */}
                <div
                  ref={seekBarRef}
                  className="group mb-2 h-1.5 cursor-pointer rounded-full bg-white/10 transition-all hover:h-2.5"
                  onClick={handleSeek}
                  onMouseDown={(e) => {
                    handleSeek(e)
                    const onMove = (ev: MouseEvent) => {
                      const bar = seekBarRef.current
                      if (!bar || totalDuration <= 0) return
                      const rect = bar.getBoundingClientRect()
                      const pct = Math.max(0, Math.min(1, (ev.clientX - rect.left) / rect.width))
                      seekToGlobal(pct * totalDuration)
                    }
                    const onUp = () => {
                      document.removeEventListener("mousemove", onMove)
                      document.removeEventListener("mouseup", onUp)
                    }
                    document.addEventListener("mousemove", onMove)
                    document.addEventListener("mouseup", onUp)
                  }}
                >
                  <div className="relative h-full w-full">
                    <div
                      className="h-full rounded-full bg-blue-500 transition-all"
                      style={{ width: `${progress}%` }}
                    >
                      <div className="absolute -right-1 -top-0.5 hidden h-3 w-3 rounded-full bg-blue-400 shadow-lg group-hover:block" />
                    </div>
                    {segmentDurations.length > 1 && segmentDurations.map((_, i) => {
                      if (i === 0 || totalDuration <= 0) return null
                      const pos = segmentDurations.slice(0, i).reduce((a, b) => a + b, 0) / totalDuration * 100
                      return <div key={i} className="absolute top-0 h-full w-px bg-white/20" style={{ left: `${pos}%` }} />
                    })}
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {/* Time display */}
                  <span className="w-28 text-xs tabular-nums text-slate-400">
                    {formatTime(globalTime)} / {formatTime(totalDuration)}
                  </span>

                  {/* Skip back */}
                  <button
                    onClick={() => skip(-5)}
                    className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-white/5 hover:text-slate-200"
                    title="Back 5s (← or Shift+← for 15s)"
                  >
                    <SkipBack className="h-3.5 w-3.5" />
                  </button>

                  {/* Play/Pause */}
                  <button
                    onClick={togglePlay}
                    className="flex h-8 w-8 items-center justify-center rounded-full bg-blue-500/20 text-blue-400 transition-colors hover:bg-blue-500/30"
                    title="Play/Pause (Space)"
                  >
                    {playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4 translate-x-px" />}
                  </button>

                  {/* Skip forward */}
                  <button
                    onClick={() => skip(5)}
                    className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-white/5 hover:text-slate-200"
                    title="Forward 5s (→ or Shift+→ for 15s)"
                  >
                    <SkipForward className="h-3.5 w-3.5" />
                  </button>

                  <div className="flex-1" />

                  {/* Speed selector */}
                  <div className="hidden items-center gap-0.5 sm:flex">
                    {SPEED_OPTIONS.map((s) => (
                      <button
                        key={s}
                        onClick={() => setPlaybackSpeed(s)}
                        className={cn(
                          "rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors",
                          playbackSpeed === s
                            ? "bg-blue-500/20 text-blue-400"
                            : "text-slate-600 hover:bg-white/5 hover:text-slate-400"
                        )}
                      >
                        {s}x
                      </button>
                    ))}
                  </div>


                  {/* Download */}
                  <button
                    onClick={handleDownload}
                    className="rounded-lg p-1.5 text-slate-500 transition-colors hover:bg-white/5 hover:text-slate-300"
                    title="Download clip folder"
                  >
                    <Download className="h-3.5 w-3.5" />
                  </button>

                  {/* Fullscreen */}
                  <button
                    onClick={toggleFullscreen}
                    className="rounded-lg p-1.5 text-slate-500 transition-colors hover:bg-white/5 hover:text-slate-300"
                    title="Fullscreen (F)"
                  >
                    {isFullscreen ? <Minimize className="h-3.5 w-3.5" /> : <Maximize className="h-3.5 w-3.5" />}
                  </button>
                </div>
              </div>
            </>
          ) : (
            <div className="glass-card flex flex-1 items-center justify-center">
              <div className="text-center">
                <Video className="mx-auto mb-3 h-16 w-16 text-slate-700" />
                <p className="text-sm font-medium text-slate-400">
                  {selectedClip ? "No video files found" : "Select a clip to begin playback"}
                </p>
                <p className="mt-1 text-xs text-slate-600">
                  Choose a clip from the sidebar to view all cameras simultaneously
                </p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// Event badge component
function EventBadge({ event }: { event: EventMeta }) {
  if (!event.reason) return null
  const { label, Icon } = formatEventReason(event.reason)
  return (
    <div className="flex items-start gap-2">
      <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-amber-500/15">
        <Icon className="h-3 w-3 text-amber-400" />
      </div>
      <div className="min-w-0">
        <p className="text-xs font-medium text-slate-300">{label}</p>
        {event.camera && (
          <p className="text-[10px] text-slate-500">
            Triggered: {CAMERA_LABELS[event.camera] || event.camera}
          </p>
        )}
        {event.city && (
          <p className="truncate text-[10px] text-slate-600">{event.city}</p>
        )}
      </div>
    </div>
  )
}
