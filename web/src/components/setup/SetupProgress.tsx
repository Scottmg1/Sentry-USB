import { useEffect, useRef, useState } from "react"
import { Loader2, Terminal } from "lucide-react"

// Keywords from setup-sentryusb stages used to estimate progress
const STAGE_MARKERS: [RegExp, number][] = [
  [/SENTRYUSB_SETUP_STARTED|rc\.local/, 5],
  [/Downloading common runtime/, 10],
  [/Updating package index/, 20],
  [/Upgrading installed packages/, 30],
  [/cmdline\.txt/, 40],
  [/Configuring the hostname/, 45],
  [/Mounting.*backing/, 50],
  [/Creating backing disk/, 60],
  [/create-backingfiles/, 65],
  [/archiveloop|archive/, 75],
  [/rebooting for changes/, 85],
  [/SETUP_FINISHED|setup completed/i, 100],
]

function estimateProgress(logText: string): number {
  let highest = 0
  for (const [pattern, pct] of STAGE_MARKERS) {
    if (pattern.test(logText)) {
      highest = Math.max(highest, pct)
    }
  }
  return highest
}

interface SetupProgressProps {
  /** If true, setup has finished successfully */
  complete?: boolean
}

export function SetupProgress({ complete }: SetupProgressProps) {
  const [logLines, setLogLines] = useState<string[]>([])
  const [progress, setProgress] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  const prevLenRef = useRef(0)

  useEffect(() => {
    if (complete) {
      setProgress(100)
      return
    }

    let cancelled = false
    async function poll() {
      try {
        const res = await fetch("/api/logs/setup")
        if (!res.ok) return
        const text = await res.text()
        if (cancelled) return
        const lines = text.split("\n").filter(Boolean)
        setLogLines(lines)
        setProgress(estimateProgress(text))
      } catch {
        // server unreachable during reboot — expected
      }
    }

    poll()
    const id = setInterval(poll, 3000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [complete])

  // Auto-scroll when new lines appear
  useEffect(() => {
    if (logLines.length > prevLenRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
    prevLenRef.current = logLines.length
  }, [logLines])

  const pct = complete ? 100 : progress

  return (
    <div className="w-full space-y-4">
      {/* Progress bar */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between text-xs">
          <span className="font-medium text-slate-400">
            {pct >= 100 ? "Complete" : "Setting up..."}
          </span>
          <span className="tabular-nums text-slate-500">{pct}%</span>
        </div>
        <div className="h-2 w-full overflow-hidden rounded-full bg-white/5">
          <div
            className="h-full rounded-full transition-all duration-700 ease-out"
            style={{
              width: `${pct}%`,
              background: pct >= 100
                ? "rgb(52, 211, 153)"
                : "linear-gradient(90deg, rgb(59,130,246), rgb(99,102,241))",
            }}
          />
        </div>
      </div>

      {/* Log journal */}
      <div className="overflow-hidden rounded-lg border border-white/10 bg-black/40">
        <div className="flex items-center gap-2 border-b border-white/5 px-3 py-2">
          <Terminal className="h-3.5 w-3.5 text-slate-500" />
          <span className="text-xs font-medium text-slate-500">Setup Log</span>
          {logLines.length > 0 && (
            <span className="ml-auto text-[10px] tabular-nums text-slate-600">
              {logLines.length} lines
            </span>
          )}
        </div>
        <div
          ref={scrollRef}
          className="max-h-48 overflow-y-auto p-3 font-mono text-[11px] leading-relaxed text-slate-400"
        >
          {logLines.length === 0 ? (
            <div className="flex items-center gap-2 text-slate-600">
              <Loader2 className="h-3 w-3 animate-spin" />
              Waiting for setup log...
            </div>
          ) : (
            logLines.map((line, i) => (
              <div key={i} className="whitespace-pre-wrap break-all">
                {line}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
