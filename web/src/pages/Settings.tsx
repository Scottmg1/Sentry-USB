import { useState, useEffect, useRef } from "react"
import {
  Settings as SettingsIcon,
  RotateCcw,
  Unplug,
  RefreshCw,
  Bluetooth,
  Gauge,
  Wand2,
  Download,
  Loader2,
  CheckCircle,
  AlertCircle,
  Stethoscope,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  XCircle,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { SetupWizard } from "@/components/setup/SetupWizard"
import { wsClient } from "@/lib/ws"

type ActionState = "idle" | "loading" | "success" | "error"

function ActionButton({
  icon: Icon,
  label,
  description,
  variant = "default",
  onClick,
  successMessage = "Done!",
  errorMessage = "Failed",
}: {
  icon: React.ElementType
  label: string
  description: string
  variant?: "default" | "danger"
  onClick: () => void | string | Promise<void | string>
  successMessage?: string
  errorMessage?: string
}) {
  const [state, setState] = useState<ActionState>("idle")
  const [msg, setMsg] = useState("")

  async function handleClick() {
    if (state === "loading") return
    setState("loading")
    setMsg("")
    try {
      const result = await onClick()
      if (result === "confirm") {
        // Special case: action needs confirmation, don't show success
        setState("idle")
        setMsg("")
        return
      }
      setState("success")
      setMsg(typeof result === "string" ? result : successMessage)
      setTimeout(() => { setState("idle"); setMsg("") }, 5000)
    } catch (err) {
      setState("error")
      setMsg(err instanceof Error ? err.message : errorMessage)
      setTimeout(() => { setState("idle"); setMsg("") }, 5000)
    }
  }

  return (
    <button
      onClick={handleClick}
      disabled={state === "loading"}
      className="glass-card glass-card-hover flex items-start gap-3 p-4 text-left transition-colors disabled:opacity-70"
    >
      <div
        className={cn(
          "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors",
          state === "loading" ? "bg-blue-500/15 text-blue-400" :
          state === "success" ? "bg-emerald-500/15 text-emerald-400" :
          state === "error" ? "bg-red-500/15 text-red-400" :
          variant === "danger"
            ? "bg-red-500/15 text-red-400"
            : "bg-blue-500/15 text-blue-400"
        )}
      >
        {state === "loading" ? (
          <Loader2 className="h-5 w-5 animate-spin" />
        ) : state === "success" ? (
          <CheckCircle className="h-5 w-5" />
        ) : state === "error" ? (
          <AlertCircle className="h-5 w-5" />
        ) : (
          <Icon className="h-5 w-5" />
        )}
      </div>
      <div>
        <p className="text-sm font-medium text-slate-200">{label}</p>
        <p className={cn(
          "mt-0.5 text-xs",
          state === "success" ? "text-emerald-400" :
          state === "error" ? "text-red-400" :
          "text-slate-500"
        )}>
          {msg || description}
        </p>
      </div>
    </button>
  )
}

type BleState = "idle" | "initiating" | "waiting" | "polling" | "paired" | "error"

function BlePairButton() {
  const [bleState, setBleState] = useState<BleState>("idle")
  const [bleMsg, setBleMsg] = useState("")
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Check if already paired on mount (quick check, no BLE probe)
  useEffect(() => {
    fetch("/api/system/ble-status?quick=true")
      .then(r => r.json())
      .then(data => {
        if (data.status === "paired" || data.status === "keys_generated") {
          setBleState("paired")
          setBleMsg("Paired — click to re-pair")
        }
      })
      .catch(() => {})
  }, [])

  // Subscribe to WebSocket ble_status messages
  useEffect(() => {
    const unsub = wsClient.subscribe("ble_status", (data: unknown) => {
      const d = data as { status: string; error?: string; output?: string }
      if (d.status === "pairing") {
        setBleState("initiating")
        setBleMsg("Sending pairing request to car...")
      } else if (d.status === "error") {
        setBleState("error")
        const errMsg = d.error || "Unknown error"
        if (errMsg.includes("maximum number of BLE")) {
          setBleMsg("Too many BLE devices active. Turn off Bluetooth on nearby phone keys and try again.")
        } else if (errMsg.includes("timed out")) {
          setBleMsg("BLE connection timed out. Make sure the Pi is near the car and try again.")
        } else {
          setBleMsg(errMsg)
        }
        cleanup()
      } else if (d.status === "waiting") {
        setBleState("waiting")
        setBleMsg("Tap your keycard on the center console to confirm pairing.")
        startPolling()
      }
    })
    return () => {
      unsub()
      cleanup()
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function cleanup() {
    if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
    if (timeoutRef.current) { clearTimeout(timeoutRef.current); timeoutRef.current = null }
  }

  function startPolling() {
    cleanup()
    let count = 0
    pollRef.current = setInterval(async () => {
      count++
      try {
        const res = await fetch("/api/system/ble-status")
        if (res.ok) {
          const data = await res.json()
          if (data.status === "paired") {
            setBleState("paired")
            setBleMsg("Successfully paired with car!")
            cleanup()
            return
          }
        }
      } catch { /* ignore fetch errors during polling */ }
      if (count >= 12) {
        setBleState("error")
        setBleMsg("Pairing timed out. Make sure you tapped your keycard on the center console, then try again.")
        cleanup()
      }
    }, 5000)
    // Safety timeout at 65 seconds
    timeoutRef.current = setTimeout(() => {
      if (bleState !== "paired" && bleState !== "error") {
        setBleState("error")
        setBleMsg("Pairing timed out. Please try again.")
        cleanup()
      }
    }, 65000)
  }

  async function handlePair() {
    setBleState("initiating")
    setBleMsg("Sending pairing request...")
    try {
      const res = await fetch("/api/system/ble-pair", { method: "POST" })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || "Failed to initiate BLE pairing")
      }
    } catch (err) {
      setBleState("error")
      setBleMsg(err instanceof Error ? err.message : "Failed to initiate pairing")
    }
  }

  function handleReset() {
    cleanup()
    setBleState("idle")
    setBleMsg("")
  }

  function handlePairedClick() {
    // Allow re-pairing from paired state
    handlePair()
  }

  const isActive = bleState !== "idle" && bleState !== "paired" && bleState !== "error"

  return (
    <button
      onClick={bleState === "idle" ? handlePair : bleState === "paired" ? handlePairedClick : bleState === "error" ? handleReset : undefined}
      disabled={isActive}
      className="glass-card glass-card-hover flex items-start gap-3 p-4 text-left transition-colors disabled:opacity-70"
    >
      <div
        className={cn(
          "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors",
          bleState === "paired" ? "bg-emerald-500/15 text-emerald-400" :
          bleState === "error" ? "bg-red-500/15 text-red-400" :
          isActive ? "bg-amber-500/15 text-amber-400" :
          "bg-blue-500/15 text-blue-400"
        )}
      >
        {isActive ? (
          <Loader2 className="h-5 w-5 animate-spin" />
        ) : bleState === "paired" ? (
          <CheckCircle className="h-5 w-5" />
        ) : bleState === "error" ? (
          <AlertCircle className="h-5 w-5" />
        ) : (
          <Bluetooth className="h-5 w-5" />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-slate-200">
          {bleState === "paired" ? "BLE Paired" : bleState === "error" ? "BLE Pairing Failed" : isActive ? "Pairing..." : "Pair BLE"}
        </p>
        <p className={cn(
          "mt-0.5 text-xs",
          bleState === "paired" ? "text-emerald-400" :
          bleState === "error" ? "text-red-400" :
          bleState === "waiting" ? "text-amber-400 font-medium" :
          "text-slate-500"
        )}>
          {bleMsg || "Initiate Bluetooth Low Energy pairing with your car"}
        </p>
      </div>
    </button>
  )
}

type HealthItem = { name: string; status: "pass" | "warn" | "fail"; detail?: string }
type HealthCategory = { name: string; items: HealthItem[] }
type HealthReport = { summary: string; categories: HealthCategory[] }

function HealthCheckButton() {
  const [loading, setLoading] = useState(false)
  const [report, setReport] = useState<HealthReport | null>(null)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  async function runCheck() {
    setLoading(true)
    setReport(null)
    try {
      const res = await fetch("/api/system/health-check")
      if (!res.ok) throw new Error("Health check failed")
      const data = await res.json()
      setReport(data)
      // Auto-expand categories with issues
      const exp: Record<string, boolean> = {}
      for (const cat of data.categories) {
        if (cat.items.some((i: HealthItem) => i.status !== "pass")) exp[cat.name] = true
      }
      setExpanded(exp)
    } catch { setReport(null) }
    setLoading(false)
  }

  const statusIcon = (s: string) => {
    if (s === "pass") return <CheckCircle className="h-3.5 w-3.5 text-emerald-400" />
    if (s === "warn") return <AlertTriangle className="h-3.5 w-3.5 text-amber-400" />
    return <XCircle className="h-3.5 w-3.5 text-red-400" />
  }

  if (!report) {
    return (
      <button
        onClick={runCheck}
        disabled={loading}
        className="glass-card glass-card-hover flex items-start gap-3 p-4 text-left transition-colors disabled:opacity-70"
      >
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-blue-500/15 text-blue-400">
          {loading ? <Loader2 className="h-5 w-5 animate-spin" /> : <Stethoscope className="h-5 w-5" />}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-slate-200">{loading ? "Running..." : "System Health Check"}</p>
          <p className="mt-0.5 text-xs text-slate-500">Verify all files, services, and config are intact</p>
        </div>
      </button>
    )
  }

  const failCount = report.categories.reduce((n, c) => n + c.items.filter(i => i.status === "fail").length, 0)
  const warnCount = report.categories.reduce((n, c) => n + c.items.filter(i => i.status === "warn").length, 0)

  return (
    <div className="glass-card col-span-full overflow-hidden">
      <div className="flex items-center justify-between border-b border-white/5 px-4 py-3">
        <div className="flex items-center gap-2">
          <Stethoscope className={cn("h-5 w-5", failCount > 0 ? "text-red-400" : warnCount > 0 ? "text-amber-400" : "text-emerald-400")} />
          <span className="text-sm font-medium text-slate-200">Health Check</span>
          <span className={cn(
            "rounded-full px-2 py-0.5 text-xs font-medium",
            failCount > 0 ? "bg-red-500/15 text-red-400" : warnCount > 0 ? "bg-amber-500/15 text-amber-400" : "bg-emerald-500/15 text-emerald-400"
          )}>{report.summary}</span>
        </div>
        <div className="flex gap-2">
          <button onClick={runCheck} disabled={loading}
            className="rounded-lg px-3 py-1 text-xs text-slate-400 hover:bg-white/5 hover:text-slate-200 disabled:opacity-50">
            {loading ? "Running..." : "Re-run"}
          </button>
          <button onClick={() => setReport(null)}
            className="rounded-lg px-3 py-1 text-xs text-slate-500 hover:bg-white/5 hover:text-slate-300">Close</button>
        </div>
      </div>
      <div className="max-h-[60vh] overflow-y-auto px-4 py-2">
        {report.categories.map(cat => {
          const isOpen = expanded[cat.name] ?? false
          const catFails = cat.items.filter(i => i.status === "fail").length
          const catWarns = cat.items.filter(i => i.status === "warn").length
          return (
            <div key={cat.name} className="border-b border-white/5 last:border-0">
              <button
                onClick={() => setExpanded(p => ({ ...p, [cat.name]: !isOpen }))}
                className="flex w-full items-center gap-2 py-2 text-left"
              >
                {isOpen ? <ChevronDown className="h-3.5 w-3.5 text-slate-500" /> : <ChevronRight className="h-3.5 w-3.5 text-slate-500" />}
                <span className="flex-1 text-xs font-medium text-slate-300">{cat.name}</span>
                {catFails > 0 && <span className="rounded bg-red-500/15 px-1.5 py-0.5 text-[10px] text-red-400">{catFails} fail</span>}
                {catWarns > 0 && <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-400">{catWarns} warn</span>}
                {catFails === 0 && catWarns === 0 && <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] text-emerald-400">all pass</span>}
              </button>
              {isOpen && (
                <div className="mb-2 space-y-0.5 pl-5">
                  {cat.items.map((item, i) => (
                    <div key={i} className="flex items-start gap-2 py-0.5">
                      {statusIcon(item.status)}
                      <span className="text-xs text-slate-300">{item.name}</span>
                      {item.detail && <span className="text-xs text-slate-600">— {item.detail}</span>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function SpeedTestButton() {
  const [running, setRunning] = useState(false)
  const [results, setResults] = useState<string[]>([])
  const cancelRef = useRef(false)

  async function runOnce(): Promise<string> {
    const start = Date.now()
    const res = await fetch("/api/system/speedtest")
    if (!res.ok) throw new Error("Speed test failed")
    const blob = await res.blob()
    const elapsed = (Date.now() - start) / 1000
    const mbps = ((blob.size * 8) / elapsed / 1_000_000).toFixed(1)
    return `${mbps} Mbps (${(blob.size / 1_000_000).toFixed(1)} MB in ${elapsed.toFixed(1)}s)`
  }

  async function startTest() {
    setRunning(true)
    cancelRef.current = false
    setResults([])
    let round = 0
    while (!cancelRef.current) {
      round++
      try {
        const result = await runOnce()
        if (cancelRef.current) break
        setResults(prev => [...prev.slice(-4), `#${round}: ${result}`])
      } catch {
        if (cancelRef.current) break
        setResults(prev => [...prev.slice(-4), `#${round}: Error`])
        break
      }
    }
    setRunning(false)
  }

  function stopTest() {
    cancelRef.current = true
  }

  return (
    <button
      onClick={running ? stopTest : startTest}
      className="glass-card glass-card-hover flex items-start gap-3 p-4 text-left transition-colors"
    >
      <div className={cn(
        "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors",
        running ? "bg-amber-500/15 text-amber-400" : "bg-blue-500/15 text-blue-400"
      )}>
        {running ? <Loader2 className="h-5 w-5 animate-spin" /> : <Gauge className="h-5 w-5" />}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-slate-200">
          {running ? "Stop Speed Test" : "Network Speed Test"}
        </p>
        {results.length > 0 ? (
          <div className="mt-0.5 space-y-0.5">
            {results.map((r, i) => (
              <p key={i} className={cn("text-xs", i === results.length - 1 ? "text-blue-400 font-medium" : "text-slate-600")}>{r}</p>
            ))}
          </div>
        ) : (
          <p className="mt-0.5 text-xs text-slate-500">
            {running ? "Running..." : "Runs continuously until stopped"}
          </p>
        )}
      </div>
    </button>
  )
}

type UpdateStatus = "idle" | "checking" | "downloading" | "installing" | "updating_scripts" | "restarting" | "done" | "error"

interface RawConfigEntry {
  value: string
  active: boolean
}

function RawConfigEditor({ config, onClose }: { config: Record<string, RawConfigEntry>; onClose: () => void }) {
  const [entries, setEntries] = useState<Record<string, { value: string; active: boolean }>>(() => {
    const e: Record<string, { value: string; active: boolean }> = {}
    for (const [k, v] of Object.entries(config)) {
      e[k] = { value: v.value, active: v.active }
    }
    return e
  })
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)
  const [newKey, setNewKey] = useState("")
  const [newVal, setNewVal] = useState("")

  const sortedKeys = Object.keys(entries).sort()

  async function handleSave() {
    setSaving(true)
    setSaveMsg(null)
    try {
      const configData: Record<string, string> = {}
      for (const [k, v] of Object.entries(entries)) {
        if (v.active) configData[k] = v.value
      }
      const res = await fetch("/api/setup/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(configData),
      })
      if (!res.ok) throw new Error("Failed to save")
      setSaveMsg("Saved successfully")
      setTimeout(() => setSaveMsg(null), 3000)
    } catch (err) {
      setSaveMsg(err instanceof Error ? err.message : "Save failed")
    } finally {
      setSaving(false)
    }
  }

  function addEntry() {
    if (!newKey.trim()) return
    setEntries(prev => ({ ...prev, [newKey.trim()]: { value: newVal, active: true } }))
    setNewKey("")
    setNewVal("")
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="glass-card relative flex h-[85vh] w-full max-w-3xl flex-col overflow-hidden">
        <div className="flex shrink-0 items-center justify-between border-b border-white/5 px-6 py-4">
          <h2 className="text-lg font-semibold text-slate-100">Raw Configuration</h2>
          <div className="flex gap-2">
            {saveMsg && <span className={cn("text-xs self-center", saveMsg.includes("success") ? "text-emerald-400" : "text-red-400")}>{saveMsg}</span>}
            <button onClick={handleSave} disabled={saving}
              className="rounded-lg bg-blue-500 px-4 py-1.5 text-sm font-medium text-white hover:bg-blue-600 disabled:opacity-50">
              {saving ? "Saving..." : "Save"}
            </button>
            <button onClick={onClose}
              className="rounded-lg px-3 py-1.5 text-sm text-slate-500 hover:bg-white/5 hover:text-slate-300">Close</button>
          </div>
        </div>
        <div className="flex-1 overflow-y-auto px-6 py-4">
          <div className="space-y-1">
            {sortedKeys.map(key => (
              <div key={key} className="flex items-center gap-2 rounded-lg border border-white/5 bg-white/[0.02] px-3 py-2">
                <input type="checkbox" checked={entries[key].active}
                  onChange={e => setEntries(prev => ({ ...prev, [key]: { ...prev[key], active: e.target.checked } }))}
                  className="accent-blue-500" />
                <span className={cn("w-48 shrink-0 truncate font-mono text-xs", entries[key].active ? "text-blue-400" : "text-slate-600")}>{key}</span>
                <input type="text" value={entries[key].value}
                  onChange={e => setEntries(prev => ({ ...prev, [key]: { ...prev[key], value: e.target.value } }))}
                  className="flex-1 rounded border border-white/10 bg-white/5 px-2 py-1 font-mono text-xs text-slate-200 outline-none focus:border-blue-500/50" />
                <button onClick={() => setEntries(prev => { const n = { ...prev }; delete n[key]; return n })}
                  className="text-xs text-slate-600 hover:text-red-400">✕</button>
              </div>
            ))}
          </div>
          <div className="mt-4 flex items-center gap-2">
            <input type="text" value={newKey} onChange={e => setNewKey(e.target.value)}
              placeholder="NEW_KEY" className="w-48 rounded border border-white/10 bg-white/5 px-2 py-1 font-mono text-xs text-slate-200 placeholder-slate-600 outline-none focus:border-blue-500/50" />
            <input type="text" value={newVal} onChange={e => setNewVal(e.target.value)}
              placeholder="value" className="flex-1 rounded border border-white/10 bg-white/5 px-2 py-1 font-mono text-xs text-slate-200 placeholder-slate-600 outline-none focus:border-blue-500/50" />
            <button onClick={addEntry}
              className="rounded bg-blue-500/20 px-3 py-1 text-xs font-medium text-blue-400 hover:bg-blue-500/30">Add</button>
          </div>
        </div>
      </div>
    </div>
  )
}

export default function Settings() {
  const [confirmReboot, setConfirmReboot] = useState(false)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardInitialData, setWizardInitialData] = useState<Record<string, string> | undefined>(undefined)
  const [rawConfigOpen, setRawConfigOpen] = useState(false)
  const [rawConfig, setRawConfig] = useState<Record<string, RawConfigEntry> | null>(null)
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus>("idle")
  const [updateError, setUpdateError] = useState<string | null>(null)
  const [version, setVersion] = useState<string | null>(null)
  const [piConfig, setPiConfig] = useState<{ uses_ble: string } | null>(null)
  const [updateAvailable, setUpdateAvailable] = useState<{ latest_version: string; release_url: string; release_notes: string } | null>(null)
  const [autoUpdateEnabled, setAutoUpdateEnabled] = useState(true)

  useEffect(() => {
    fetch("/api/system/version")
      .then(r => r.json())
      .then(data => setVersion(data.version || "unknown"))
      .catch(() => setVersion("unknown"))
  }, [updateStatus])

  useEffect(() => {
    fetch("/api/config")
      .then(r => r.json())
      .then(data => setPiConfig(data))
      .catch(() => {})
    // Check for cached update status
    fetch("/api/system/update-status")
      .then(r => r.json())
      .then(data => {
        if (data.update_available) {
          setUpdateAvailable({ latest_version: data.latest_version, release_url: data.release_url, release_notes: data.release_notes })
        }
      })
      .catch(() => {})
    // Load auto-update preference
    fetch("/api/config/preference?key=auto_update_check")
      .then(r => r.json())
      .then(data => setAutoUpdateEnabled(data.value !== "disabled"))
      .catch(() => {})
  }, [])

  async function handleUpdate() {
    setUpdateStatus("checking")
    setUpdateError(null)

    try {
      // Check internet first
      const checkRes = await fetch("/api/system/check-internet")
      const checkData = await checkRes.json()
      if (!checkData.connected) {
        setUpdateStatus("error")
        setUpdateError("No internet connection. Connect to WiFi first.")
        return
      }

      setUpdateStatus("downloading")

      // Trigger the update
      const res = await fetch("/api/system/update", { method: "POST" })
      if (!res.ok) throw new Error("Failed to start update")

      // Poll for completion (the server will restart, so we watch for disconnect)
      setUpdateStatus("installing")
      setTimeout(() => {
        setUpdateStatus("updating_scripts")
      }, 5000)
      setTimeout(() => {
        setUpdateStatus("restarting")
        // After restart, poll until the server comes back
        const pollInterval = setInterval(async () => {
          try {
            const r = await fetch("/api/system/version")
            if (r.ok) {
              clearInterval(pollInterval)
              setUpdateStatus("done")
              setTimeout(() => setUpdateStatus("idle"), 5000)
            }
          } catch {
            // Still restarting, keep polling
          }
        }, 3000)
        // Give up after 2 minutes
        setTimeout(() => clearInterval(pollInterval), 120000)
      }, 15000)
    } catch (err) {
      setUpdateStatus("error")
      setUpdateError(err instanceof Error ? err.message : "Update failed")
    }
  }

  function handleReboot() {
    if (!confirmReboot) {
      setConfirmReboot(true)
      setTimeout(() => setConfirmReboot(false), 10000)
      return "confirm"
    }
    fetch("/api/system/reboot", { method: "POST" })
    setConfirmReboot(false)
    return "Rebooting..."
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-100">Settings</h1>
        <p className="mt-1 text-sm text-slate-500">
          System actions and configuration
        </p>
      </div>

      {/* Setup Wizard CTA */}
      <div className="glass-card overflow-hidden">
        <div className="flex items-center gap-4 p-5">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-blue-500/20">
            <Wand2 className="h-6 w-6 text-blue-400" />
          </div>
          <div className="flex-1">
            <h2 className="text-lg font-semibold text-slate-100">
              Setup Wizard
            </h2>
            <p className="mt-0.5 text-sm text-slate-400">
              Configure WiFi, archive, notifications, and more through a guided
              setup experience.
            </p>
          </div>
          <div className="flex shrink-0 gap-2">
            <button
              onClick={async () => {
                try {
                  const res = await fetch("/api/setup/config")
                  if (!res.ok) throw new Error("Failed")
                  const data = await res.json()
                  // Build conf file content
                  let content = "# sentryusb.conf - exported from SentryUSB UI\n"
                  for (const [k, v] of Object.entries(data)) {
                    const entry = v as { value: string; active: boolean }
                    if (entry.active) {
                      content += `export ${k}='${entry.value}'\n`
                    } else {
                      content += `# export ${k}='${entry.value}'\n`
                    }
                  }
                  const blob = new Blob([content], { type: "text/plain" })
                  const url = URL.createObjectURL(blob)
                  const a = document.createElement("a")
                  a.href = url
                  a.download = "sentryusb.conf"
                  a.click()
                  URL.revokeObjectURL(url)
                } catch { /* ignore */ }
              }}
              className="shrink-0 rounded-lg border border-white/10 bg-white/5 px-4 py-2 text-sm font-medium text-slate-300 transition-colors hover:bg-white/10"
            >
              <Download className="mr-1.5 inline h-3.5 w-3.5" />
              Export Config
            </button>
            <button
              onClick={async () => {
                try {
                  const res = await fetch("/api/setup/config")
                  if (res.ok) {
                    const data = await res.json()
                    const flat: Record<string, string> = {}
                    for (const [k, v] of Object.entries(data)) {
                      const entry = v as { value: string; active: boolean }
                      if (entry.active) flat[k] = entry.value
                    }
                    setWizardInitialData(flat)
                  }
                } catch { /* use empty data */ }
                setWizardOpen(true)
              }}
              className="shrink-0 rounded-lg bg-blue-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-600"
            >
              Open Wizard
            </button>
          </div>
        </div>
      </div>

      {/* Quick Actions */}
      <div>
        <h2 className="mb-3 text-sm font-medium uppercase tracking-wider text-slate-500">
          Quick Actions
        </h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <ActionButton
            icon={Unplug}
            label="Toggle USB Drives"
            description="Connect or disconnect drives from the host"
            successMessage="Drives toggled successfully"
            onClick={async () => {
              const res = await fetch("/api/system/toggle-drives", { method: "POST" })
              if (!res.ok) throw new Error("Failed to toggle drives")
            }}
          />
          <ActionButton
            icon={RefreshCw}
            label="Trigger Archive Sync"
            description="Start archiving recorded clips now"
            successMessage="Archive sync started"
            onClick={async () => {
              const res = await fetch("/api/system/trigger-sync", { method: "POST" })
              if (!res.ok) throw new Error("Failed to trigger sync")
            }}
          />
          <SpeedTestButton />
          {piConfig?.uses_ble === "yes" && <BlePairButton />}
          <HealthCheckButton />
        </div>
      </div>

      {/* Update available banner */}
      {updateAvailable && updateStatus === "idle" && (
        <div className="glass-card overflow-hidden border border-amber-500/20 bg-amber-500/5">
          <div className="flex items-center gap-4 p-5">
            <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-amber-500/20">
              <Download className="h-6 w-6 text-amber-400" />
            </div>
            <div className="flex-1">
              <h2 className="text-lg font-semibold text-amber-200">
                Update Available: {updateAvailable.latest_version}
              </h2>
              <p className="mt-0.5 text-sm text-slate-400">
                A new version of SentryUSB is ready to install.
                {" "}
                <a href={updateAvailable.release_url} target="_blank" rel="noopener noreferrer"
                  className="text-blue-400 hover:text-blue-300 underline">View on GitHub</a>
              </p>
            </div>
            <button
              onClick={handleUpdate}
              className="shrink-0 rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-amber-600"
            >
              Install Update
            </button>
          </div>
        </div>
      )}

      {/* Update SentryUSB */}
      <div className="glass-card overflow-hidden">
        <div className="flex items-center gap-4 p-5">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-emerald-500/20">
            <Download className="h-6 w-6 text-emerald-400" />
          </div>
          <div className="flex-1">
            <h2 className="text-lg font-semibold text-slate-100">
              Update SentryUSB
            </h2>
            <p className="mt-0.5 text-sm text-slate-400">
              {updateStatus === "idle" && "Check for and install the latest version."}
              {updateStatus === "checking" && "Checking internet connection..."}
              {updateStatus === "downloading" && "Downloading latest release..."}
              {updateStatus === "installing" && "Installing update..."}
              {updateStatus === "updating_scripts" && "Updating shell scripts..."}
              {updateStatus === "restarting" && "Restarting service..."}
              {updateStatus === "done" && "Update complete!"}
              {updateStatus === "error" && (updateError || "Update failed.")}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {updateStatus === "error" && (
              <AlertCircle className="h-5 w-5 text-red-400" />
            )}
            {updateStatus === "done" && (
              <CheckCircle className="h-5 w-5 text-emerald-400" />
            )}
            {(updateStatus === "checking" || updateStatus === "downloading" || updateStatus === "installing" || updateStatus === "updating_scripts" || updateStatus === "restarting") && (
              <Loader2 className="h-5 w-5 animate-spin text-blue-400" />
            )}
            <button
              onClick={handleUpdate}
              disabled={updateStatus !== "idle" && updateStatus !== "error" && updateStatus !== "done"}
              className="rounded-lg bg-emerald-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-emerald-600 disabled:opacity-50"
            >
              {updateStatus === "idle" || updateStatus === "done" || updateStatus === "error"
                ? "Check for Updates"
                : "Updating..."}
            </button>
          </div>
        </div>
        {/* Auto-update check toggle */}
        <div className="border-t border-white/5 px-5 py-3">
          <label className="flex cursor-pointer items-center justify-between">
            <span className="text-sm text-slate-400">Automatically check for updates after each archive</span>
            <input
              type="checkbox"
              checked={autoUpdateEnabled}
              onChange={async (e) => {
                const enabled = e.target.checked
                setAutoUpdateEnabled(enabled)
                await fetch("/api/config/preference", {
                  method: "PUT",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ key: "auto_update_check", value: enabled ? "enabled" : "disabled" }),
                }).catch(() => {})
              }}
              className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500"
            />
          </label>
        </div>
      </div>

      {/* System */}
      <div>
        <h2 className="mb-3 text-sm font-medium uppercase tracking-wider text-slate-500">
          System
        </h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <ActionButton
            icon={RotateCcw}
            label={confirmReboot ? "Press again to confirm" : "Restart Raspberry Pi"}
            description={
              confirmReboot
                ? "This will reboot the device immediately"
                : "Reboot the SentryUSB device"
            }
            variant={confirmReboot ? "danger" : "default"}
            onClick={handleReboot}
          />
          <ActionButton
            icon={SettingsIcon}
            label="Advanced Settings"
            description="View and edit raw configuration file"
            onClick={async () => {
              const res = await fetch("/api/setup/config")
              if (!res.ok) throw new Error("Failed to load config")
              const data = await res.json()
              setRawConfig(data)
              setRawConfigOpen(true)
              return "confirm"
            }}
          />
        </div>
      </div>

      {/* About */}
      <div className="glass-card p-4">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wider text-slate-500">
          About
        </h2>
        <div className="space-y-1 text-sm">
          <p className="text-slate-300">
            <span className="text-slate-500">Version:</span>{" "}
            {version || "loading..."}
          </p>
          <p className="text-slate-300">
            <span className="text-slate-500">Project:</span> SentryUSB
          </p>
          <p className="text-slate-300">
            <span className="text-slate-500">Based on:</span>{" "}
            <a
              href="https://github.com/Scottmg1/Sentry-USB"
              target="_blank"
              rel="noopener noreferrer"
              className="text-blue-400 hover:text-blue-300"
            >
              TeslaUSB (original)
            </a>{" "}
            (MIT License)
          </p>
          <p className="text-slate-300">
            <span className="text-slate-500">License:</span> MIT
          </p>
        </div>
        <a
          href="https://discord.gg/9QZEzVwdnt"
          target="_blank"
          rel="noopener noreferrer"
          className="mt-4 inline-flex items-center gap-2 rounded-lg bg-[#5865F2] px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-[#4752c4]"
        >
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
            <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028c.462-.63.874-1.295 1.226-1.994a.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z" />
          </svg>
          Join Discord Server
        </a>
      </div>

      {/* Setup Wizard Modal */}
      {wizardOpen && (
        <SetupWizard initialData={wizardInitialData} onClose={() => { setWizardOpen(false); setWizardInitialData(undefined) }} />
      )}

      {/* Raw Config Editor Modal */}
      {rawConfigOpen && rawConfig && (
        <RawConfigEditor config={rawConfig} onClose={() => { setRawConfigOpen(false); setRawConfig(null) }} />
      )}
    </div>
  )
}
