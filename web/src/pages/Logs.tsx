import { useState, useEffect, useRef, useCallback } from "react"
import { ScrollText, Download, RefreshCw, ArrowDown } from "lucide-react"
import { cn } from "@/lib/utils"

const logTabs = [
  { id: "archiveloop", label: "Archive Loop", url: "/api/logs/archiveloop" },
  { id: "setup", label: "Setup Log", url: "/api/logs/setup" },
  { id: "diagnostics", label: "Diagnostics", url: "/api/logs/diagnostics" },
]

const SCROLL_THRESHOLD = 60

export default function Logs() {
  const [activeTab, setActiveTab] = useState("archiveloop")
  const [content, setContent] = useState<string>("Loading...")
  const [loading, setLoading] = useState(false)
  const [showScrollBtn, setShowScrollBtn] = useState(false)
  const preRef = useRef<HTMLPreElement>(null)
  const followRef = useRef(true)

  const activeLog = logTabs.find((t) => t.id === activeTab)!

  const handleScroll = useCallback(() => {
    const el = preRef.current
    if (!el) return
    const atBottom =
      el.scrollHeight - el.scrollTop - el.clientHeight < SCROLL_THRESHOLD
    followRef.current = atBottom
    setShowScrollBtn(!atBottom)
  }, [])

  function scrollToBottom() {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight
      followRef.current = true
      setShowScrollBtn(false)
    }
  }

  useEffect(() => {
    followRef.current = true
    setShowScrollBtn(false)
  }, [activeTab])

  useEffect(() => {
    let mounted = true
    let isFirstFetch = true
    setLoading(true)
    setContent("Loading...")

    async function fetchLog() {
      try {
        const url =
          activeTab === "diagnostics"
            ? "/api/diagnostics?" + Math.random()
            : activeLog.url + "?" + Math.random()
        const res = await fetch(url)
        const text = await res.text()
        if (mounted) {
          if (!res.ok && activeTab !== "diagnostics") {
            setContent("Log file not available. It may not exist yet.")
          } else {
            setContent(text || "(empty)")
          }
          setLoading(false)
          requestAnimationFrame(() => {
            if (preRef.current && (followRef.current || isFirstFetch)) {
              preRef.current.scrollTop = preRef.current.scrollHeight
            }
          })
          isFirstFetch = false
        }
      } catch {
        if (mounted) {
          setContent("Unable to connect to Sentry USB. Is the device online?")
          setLoading(false)
        }
      }
    }

    fetchLog()

    const interval =
      activeTab !== "diagnostics" ? setInterval(fetchLog, 2000) : undefined

    return () => {
      mounted = false
      if (interval) clearInterval(interval)
    }
  }, [activeLog.url, activeTab])

  function handleDownload() {
    const blob = new Blob([content], { type: "text/plain" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `${activeTab}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  async function handleRefreshDiagnostics() {
    setLoading(true)
    setContent("Generating diagnostics...")
    try {
      await fetch("/api/diagnostics/refresh", { method: "POST" })
      await new Promise((r) => setTimeout(r, 3000))
      const res = await fetch("/api/logs/diagnostics?" + Math.random())
      const text = await res.text()
      setContent(text || "(empty)")
    } catch {
      setContent("Failed to generate diagnostics")
    }
    setLoading(false)
  }

  return (
    <div className="flex h-[calc(100vh-120px)] flex-col space-y-4 md:h-[calc(100vh-96px)]">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Logs</h1>
          <p className="mt-1 text-sm text-slate-500">
            System logs and diagnostics
          </p>
        </div>
        <div className="flex gap-2">
          {activeTab === "diagnostics" && (
            <button
              onClick={handleRefreshDiagnostics}
              disabled={loading}
              className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200 disabled:opacity-50"
            >
              <RefreshCw
                className={cn("h-4 w-4", loading && "animate-spin")}
              />
              Refresh
            </button>
          )}
          <button
            onClick={handleDownload}
            className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200"
          >
            <Download className="h-4 w-4" />
            Download
          </button>
        </div>
      </div>

      {/* Tab bar */}
      <div className="flex gap-1">
        {logTabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={cn(
              "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
              activeTab === tab.id
                ? "bg-blue-500/15 text-blue-400"
                : "text-slate-500 hover:bg-white/5 hover:text-slate-300"
            )}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Log output */}
      <div className="glass-card relative flex-1 overflow-hidden">
        <pre
          ref={preRef}
          onScroll={handleScroll}
          className="h-full overflow-auto p-4 font-mono text-xs leading-relaxed text-slate-300"
        >
          {content || (
            <span className="flex items-center gap-2 text-slate-600">
              <ScrollText className="h-4 w-4" />
              No log content
            </span>
          )}
        </pre>
        {showScrollBtn && (
          <button
            onClick={scrollToBottom}
            className="absolute bottom-4 right-6 flex items-center gap-1.5 rounded-full bg-blue-500/90 px-3 py-1.5 text-xs font-medium text-white shadow-lg backdrop-blur transition-opacity hover:bg-blue-500"
          >
            <ArrowDown className="h-3.5 w-3.5" />
            Follow
          </button>
        )}
      </div>
    </div>
  )
}
