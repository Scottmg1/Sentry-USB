import { useState } from "react"
import { Video, Layout, Play, SkipBack, SkipForward } from "lucide-react"

const layouts = [
  { id: 1, name: "Sides top, rear bottom (mirrored)" },
  { id: 2, name: "Side & rear bottom (mirrored)" },
  { id: 3, name: "Side & rear bottom (looking back)" },
  { id: 4, name: "Mobile (mirrored)" },
  { id: 5, name: "Mobile (looking back)" },
  { id: 6, name: "Pillars on side" },
]

export default function Viewer() {
  const [currentLayout, setCurrentLayout] = useState(3)
  const [showLayoutPicker, setShowLayoutPicker] = useState(false)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Viewer</h1>
          <p className="mt-1 text-sm text-slate-500">
            Multi-camera clip viewer
          </p>
        </div>
        <div className="relative">
          <button
            onClick={() => setShowLayoutPicker(!showLayoutPicker)}
            className="glass-card glass-card-hover flex items-center gap-2 px-3 py-2 text-sm text-slate-300 transition-colors"
          >
            <Layout className="h-4 w-4" />
            Layout
          </button>
          {showLayoutPicker && (
            <div className="glass-card absolute right-0 top-full z-10 mt-1 w-64 p-1">
              {layouts.map((l) => (
                <button
                  key={l.id}
                  onClick={() => {
                    setCurrentLayout(l.id)
                    setShowLayoutPicker(false)
                  }}
                  className={`w-full rounded-lg px-3 py-2 text-left text-sm transition-colors ${
                    currentLayout === l.id
                      ? "bg-blue-500/15 font-medium text-blue-400"
                      : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
                  }`}
                >
                  {l.name}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Clip categories */}
      <div className="flex gap-2">
        {["RecentClips", "SavedClips", "SentryClips"].map((cat) => (
          <button
            key={cat}
            className="glass-card glass-card-hover px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200"
          >
            {cat}
          </button>
        ))}
      </div>

      {/* Video grid placeholder */}
      <div className="glass-card flex aspect-video items-center justify-center">
        <div className="text-center">
          <Video className="mx-auto mb-3 h-12 w-12 text-slate-600" />
          <p className="text-sm text-slate-500">
            Select a clip to begin playback
          </p>
          <p className="mt-1 text-xs text-slate-600">
            Layout {currentLayout} active
          </p>
        </div>
      </div>

      {/* Transport controls */}
      <div className="glass-card flex items-center justify-center gap-4 p-3">
        <button className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-white/5 hover:text-slate-200">
          <SkipBack className="h-5 w-5" />
        </button>
        <button className="flex h-10 w-10 items-center justify-center rounded-full bg-blue-500/20 text-blue-400 transition-colors hover:bg-blue-500/30">
          <Play className="h-5 w-5" />
        </button>
        <button className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-white/5 hover:text-slate-200">
          <SkipForward className="h-5 w-5" />
        </button>
        <div className="flex-1">
          <input
            type="range"
            min="0"
            max="60"
            defaultValue="0"
            className="h-1.5 w-full cursor-pointer appearance-none rounded-full bg-slate-700 accent-blue-500"
          />
        </div>
        <span className="text-xs tabular-nums text-slate-500">00:00 / 01:00</span>
      </div>
    </div>
  )
}
