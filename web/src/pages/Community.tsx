import { useState, useRef, useCallback } from "react"
import { Users, Paintbrush, Volume2 } from "lucide-react"
import CommunityWraps from "./CommunityWraps"
import LockChime from "./LockChime"

type CommunityView = "wraps" | "chimes"

export default function Community() {
  const [view, setView] = useState<CommunityView>("wraps")
  const headingClickRef = useRef<(() => void) | null>(null)
  const registerHeadingClick = useCallback((fn: () => void) => { headingClickRef.current = fn }, [])

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-500/20">
            <Users className="h-5 w-5 text-blue-400" />
          </div>
          <div>
            <h1
              className="cursor-default select-none text-xl font-semibold text-slate-100"
              onClick={() => headingClickRef.current?.()}
            >Community</h1>
            <p className="text-xs text-slate-500">Wraps & Chimes</p>
          </div>
        </div>
      </div>

      {/* Toggle Switch */}
      <div className="flex items-center gap-1 rounded-lg bg-white/[0.03] border border-white/10 p-1 w-fit">
        <button
          onClick={() => setView("wraps")}
          className={`flex items-center gap-2 rounded-md px-4 py-2 text-sm font-medium transition-colors ${
            view === "wraps"
              ? "bg-blue-500/15 text-blue-400"
              : "text-slate-400 hover:text-slate-200"
          }`}
        >
          <Paintbrush className="h-4 w-4" />
          Wraps
        </button>
        <button
          onClick={() => setView("chimes")}
          className={`flex items-center gap-2 rounded-md px-4 py-2 text-sm font-medium transition-colors ${
            view === "chimes"
              ? "bg-blue-500/15 text-blue-400"
              : "text-slate-400 hover:text-slate-200"
          }`}
        >
          <Volume2 className="h-4 w-4" />
          Chimes
        </button>
      </div>

      {/* Content */}
      {view === "wraps" ? <CommunityWraps onRegisterHeadingClick={registerHeadingClick} /> : <LockChime />}
    </div>
  )
}
