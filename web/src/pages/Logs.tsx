import { useState, useEffect, useRef } from "react"
import { ScrollText, Download, RefreshCw } from "lucide-react"
import { cn } from "@/lib/utils"

const logTabs = [
  { id: "archiveloop", label: "Archive Loop", url: "/archiveloop.log" },
  { id: "setup", label: "Setup Log", url: "/teslausb-headless-setup.log" },
  { id: "diagnostics", label: "Diagnostics", url: "/diagnostics.txt" },
]

export default function Logs() {
  const [activeTab, setActiveTab] = useState("archiveloop")
  const [content, setContent] = useState<string>("Loading...")
  const [loading, setLoading] = useState(false)
  const preRef = useRef<HTMLPreElement>(null)

  const activeLog = logTabs.find((t) => t.id === activeTab)!

  useEffect(() => {
    let mounted = true
    setLoading(true)
    setContent("Loading...")

    async function fetchLog() {
      try {
        const res = await fetch(activeLog.url + "?" + Math.random())
        if (!res.ok) throw new Error("Failed to fetch")
        const text = await res.text()
        if (mounted) {
          setContent(text || "(empty)")
          setLoading(false)
          // Auto-scroll to bottom
          requestAnimationFrame(() => {
            if (preRef.current) {
              preRef.current.scrollTop = preRef.current.scrollHeight
            }
          })
        }
      } catch {
        if (mounted) {
          setContent("Unable to load log file. Connect to SentryUSB.")
          setLoading(false)
        }
      }
    }

    fetchLog()

    // Tail the log every 2s
    const interval = setInterval(fetchLog, 2000)

    return () => {
      mounted = false
      clearInterval(interval)
    }
  }, [activeLog.url])

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
      await fetch("/cgi-bin/diagnose.sh?" + Math.random())
      // Wait a moment for diagnostics to generate
      await new Promise((r) => setTimeout(r, 2000))
      const res = await fetch("/diagnostics.txt?" + Math.random())
      const text = await res.text()
      setContent(text || "(empty)")
    } catch {
      setContent("Failed to generate diagnostics")
    }
    setLoading(false)
  }

  return (
    <div className="flex h-[calc(100vh-120px)] flex-col space-y-4 md:h-[calc(100vh-96px)]">
      <div className="flex items-center justify-between">
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
              <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
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
      <div className="glass-card flex-1 overflow-hidden">
        <pre
          ref={preRef}
          className="h-full overflow-auto p-4 font-mono text-xs leading-relaxed text-slate-300"
        >
          {content || (
            <span className="flex items-center gap-2 text-slate-600">
              <ScrollText className="h-4 w-4" />
              No log content
            </span>
          )}
        </pre>
      </div>
    </div>
  )
}
