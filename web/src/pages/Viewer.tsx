import { useState, useEffect, useRef, useCallback } from "react"
import { Video, Play, Pause, SkipBack, SkipForward, Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"

interface ClipEntry {
  date: string
  path: string
  files: string[]
}

interface ClipGroup {
  name: string
  clips: ClipEntry[]
}

interface ClipSet {
  timestamp: string
  cameras: Record<string, string>
}

const CAMERAS = ["front", "back", "left_repeater", "right_repeater", "left_pillar", "right_pillar"]
const CAMERA_LABELS: Record<string, string> = {
  front: "Front",
  back: "Rear",
  left_repeater: "Left",
  right_repeater: "Right",
  left_pillar: "Left Pillar",
  right_pillar: "Right Pillar",
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

export default function Viewer() {
  const [groups, setGroups] = useState<ClipGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [activeCategory, setActiveCategory] = useState("RecentClips")
  const [selectedClip, setSelectedClip] = useState<ClipEntry | null>(null)
  const [clipSets, setClipSets] = useState<ClipSet[]>([])
  const [currentSetIdx, setCurrentSetIdx] = useState(0)
  const [playing, setPlaying] = useState(false)
  const videoRefs = useRef<(HTMLVideoElement | null)[]>([])

  useEffect(() => {
    fetch("/api/clips")
      .then((r) => r.json())
      .then((data: ClipGroup[]) => {
        setGroups(data)
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }, [])

  const activeGroup = groups.find((g) => g.name === activeCategory)

  useEffect(() => {
    if (selectedClip) {
      const sets = groupByTimestamp(selectedClip.files, selectedClip.path)
      setClipSets(sets)
      setCurrentSetIdx(0)
      setPlaying(false)
    }
  }, [selectedClip])

  const currentSet = clipSets[currentSetIdx]

  const togglePlay = useCallback(() => {
    videoRefs.current.forEach((v) => {
      if (!v) return
      if (playing) v.pause()
      else v.play().catch(() => {})
    })
    setPlaying(!playing)
  }, [playing])

  function handlePrev() {
    if (currentSetIdx > 0) {
      setCurrentSetIdx(currentSetIdx - 1)
      setPlaying(false)
    }
  }

  function handleNext() {
    if (currentSetIdx < clipSets.length - 1) {
      setCurrentSetIdx(currentSetIdx + 1)
      setPlaying(false)
    }
  }

  return (
    <div className="flex h-[calc(100vh-120px)] flex-col space-y-3 md:h-[calc(100vh-96px)]">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Viewer</h1>
          <p className="mt-1 text-sm text-slate-500">Multi-camera clip viewer</p>
        </div>
      </div>

      {/* Category tabs */}
      <div className="flex gap-1">
        {["RecentClips", "SavedClips", "SentryClips"].map((cat) => (
          <button
            key={cat}
            onClick={() => { setActiveCategory(cat); setSelectedClip(null) }}
            className={cn(
              "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
              activeCategory === cat
                ? "bg-blue-500/15 text-blue-400"
                : "text-slate-500 hover:bg-white/5 hover:text-slate-300"
            )}
          >
            {cat.replace("Clips", " Clips")}
          </button>
        ))}
      </div>

      <div className="flex min-h-0 flex-1 gap-3">
        {/* Clip list sidebar */}
        <div className="glass-card w-48 shrink-0 overflow-y-auto p-2">
          {loading ? (
            <div className="flex items-center justify-center p-4">
              <Loader2 className="h-5 w-5 animate-spin text-slate-500" />
            </div>
          ) : activeGroup && activeGroup.clips.length > 0 ? (
            activeGroup.clips.map((clip) => (
              <button
                key={clip.date}
                onClick={() => setSelectedClip(clip)}
                className={cn(
                  "w-full rounded-md px-2 py-1.5 text-left text-sm transition-colors",
                  selectedClip?.date === clip.date
                    ? "bg-blue-500/15 text-blue-400"
                    : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
                )}
              >
                <span className="font-mono text-xs">{clip.date}</span>
                <span className="ml-1 text-xs text-slate-600">
                  ({clip.files.length} files)
                </span>
              </button>
            ))
          ) : (
            <p className="p-2 text-center text-xs text-slate-600">No clips</p>
          )}
        </div>

        {/* Video area */}
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          {currentSet ? (
            <>
              <div className="grid min-h-0 flex-1 grid-cols-3 grid-rows-2 gap-1">
                {CAMERAS.map((cam, i) => (
                  <div key={cam} className="relative overflow-hidden rounded-md bg-black">
                    {currentSet.cameras[cam] ? (
                      <video
                        ref={(el) => { videoRefs.current[i] = el }}
                        src={currentSet.cameras[cam]}
                        className="h-full w-full object-contain"
                        muted
                        playsInline
                        preload="metadata"
                      />
                    ) : (
                      <div className="flex h-full items-center justify-center">
                        <Video className="h-6 w-6 text-slate-700" />
                      </div>
                    )}
                    <span className="absolute bottom-1 left-1 rounded bg-black/60 px-1 py-0.5 text-[10px] text-slate-400">
                      {CAMERA_LABELS[cam]}
                    </span>
                  </div>
                ))}
              </div>

              {/* Transport controls */}
              <div className="glass-card flex items-center justify-center gap-4 p-2">
                <button
                  onClick={handlePrev}
                  disabled={currentSetIdx === 0}
                  className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-white/5 hover:text-slate-200 disabled:opacity-30"
                >
                  <SkipBack className="h-4 w-4" />
                </button>
                <button
                  onClick={togglePlay}
                  className="flex h-9 w-9 items-center justify-center rounded-full bg-blue-500/20 text-blue-400 transition-colors hover:bg-blue-500/30"
                >
                  {playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
                </button>
                <button
                  onClick={handleNext}
                  disabled={currentSetIdx >= clipSets.length - 1}
                  className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-white/5 hover:text-slate-200 disabled:opacity-30"
                >
                  <SkipForward className="h-4 w-4" />
                </button>
                <span className="text-xs tabular-nums text-slate-500">
                  {currentSetIdx + 1} / {clipSets.length} clips
                </span>
                <span className="text-xs text-slate-600">
                  {currentSet.timestamp}
                </span>
              </div>
            </>
          ) : (
            <div className="glass-card flex flex-1 items-center justify-center">
              <div className="text-center">
                <Video className="mx-auto mb-3 h-12 w-12 text-slate-600" />
                <p className="text-sm text-slate-500">
                  {selectedClip ? "No video files found" : "Select a clip to begin playback"}
                </p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
