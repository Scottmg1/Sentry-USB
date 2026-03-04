import { useEffect, useState } from "react"
import {
  Thermometer,
  HardDrive,
  Wifi,
  Clock,
  Camera,
  Activity,
  Cable,
  Archive,
  HeartPulse,
  Timer,
} from "lucide-react"
import { api } from "@/lib/api"
import { useKeepAwake } from "@/hooks/useKeepAwake"
import type { PiStatus, DriveStats, StorageBreakdown } from "@/lib/api"
import { wsClient } from "@/lib/ws"
import { formatUptime, formatBytes, formatTemp } from "@/lib/utils"

function StatCard({
  icon: Icon,
  label,
  value,
  sub,
  color = "blue",
}: {
  icon: React.ElementType
  label: string
  value: string
  sub?: string
  color?: "blue" | "emerald" | "amber" | "red" | "purple"
}) {
  const colorMap = {
    blue: "text-blue-400 bg-blue-500/15",
    emerald: "text-emerald-400 bg-emerald-500/15",
    amber: "text-amber-400 bg-amber-500/15",
    red: "text-red-400 bg-red-500/15",
    purple: "text-purple-400 bg-purple-500/15",
  }

  return (
    <div className="glass-card p-4">
      <div className="flex items-start gap-3">
        <div
          className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${colorMap[color]}`}
        >
          <Icon className="h-5 w-5" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-xs font-medium uppercase tracking-wider text-slate-500">
            {label}
          </p>
          <p className="mt-1 text-lg font-semibold text-slate-100">{value}</p>
          {sub && <p className="mt-0.5 text-xs text-slate-500">{sub}</p>}
        </div>
      </div>
    </div>
  )
}

function getTempColor(milliC: number): "emerald" | "amber" | "red" {
  if (milliC < 55000) return "emerald"
  if (milliC < 70000) return "amber"
  return "red"
}

function getWifiStrengthBars(strength: string): number {
  if (!strength) return 0
  const parts = strength.split("/")
  if (parts.length !== 2) return 0
  const ratio = parseInt(parts[0]) / parseInt(parts[1])
  if (ratio > 0.75) return 4
  if (ratio > 0.5) return 3
  if (ratio > 0.25) return 2
  return 1
}

interface ProcessProgress {
  current: number
  total: number
}

export default function Dashboard() {
  const [status, setStatus] = useState<PiStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [uptime, setUptime] = useState(0)
  const [driveStats, setDriveStats] = useState<DriveStats | null>(null)
  const [storageBreakdown, setStorageBreakdown] = useState<StorageBreakdown | null>(null)
  const [archiveProgress, setArchiveProgress] = useState<ProcessProgress | null>(null)
  const [processing, setProcessing] = useState(false)
  const [processProgress, setProcessProgress] = useState<ProcessProgress | null>(null)
  const [metric, setMetric] = useState(false)
  const [useFahrenheit, setUseFahrenheit] = useState(false)

  const active = archiveProgress !== null || processing

  useEffect(() => {
    let mounted = true
    let uptimeInterval: ReturnType<typeof setInterval>

    async function fetchStatus() {
      try {
        const data = await api.getStatus()
        if (!mounted) return
        setStatus(data)
        setUptime(parseFloat(data.uptime))
        setError(null)
      } catch {
        if (mounted) setError("Unable to connect to Sentry USB")
      }
    }

    async function fetchDriveStats() {
      try {
        const [stats, driveStatus] = await Promise.all([
          api.getDriveStats(),
          api.getDriveStatus(),
        ])
        if (!mounted) return
        setDriveStats(stats)
        setProcessing(driveStatus.running)
        if (!driveStatus.running) setProcessProgress(null)

        if (driveStatus.phase === "archiving" && driveStatus.total != null) {
          setArchiveProgress({
            current: driveStatus.current ?? 0,
            total: driveStatus.total,
          })
        } else {
          setArchiveProgress(null)
        }
      } catch {
        // non-critical — drive stats may not be available
      }
    }

    async function fetchStorageBreakdown() {
      try {
        const data = await api.getStorageBreakdown()
        if (mounted) setStorageBreakdown(data)
      } catch { /* non-critical */ }
    }

    fetchStatus()
    fetchDriveStats()
    fetchStorageBreakdown()
    fetch("/api/setup/config")
      .then((r) => r.json())
      .then((cfg) => {
        const entry = cfg.DRIVE_MAP_UNIT
        if (entry) {
          const val = typeof entry === "object"
            ? (entry.active ? entry.value : null)
            : entry
          if (val !== null) setMetric(val === "km")
        }
        const tempEntry = cfg.TEMPERATURE_UNIT
        if (tempEntry) {
          const val = typeof tempEntry === "object"
            ? (tempEntry.active ? tempEntry.value : null)
            : tempEntry
          if (val !== null) setUseFahrenheit(val === "F")
        }
      })
      .catch(() => { })
    const statusInterval = setInterval(fetchStatus, 4000)
    const statsInterval = setInterval(fetchDriveStats, 5000)
    const storageInterval = setInterval(fetchStorageBreakdown, 10000)

    uptimeInterval = setInterval(() => {
      setUptime((prev) => prev + 1)
    }, 1000)

    // Subscribe to real-time GPS processing progress via WebSocket
    const unsubscribe = wsClient.subscribe("drive_process", (data) => {
      if (!mounted) return
      const msg = data as { status: string; current?: number; total?: number }
      if (msg.status === "started") {
        setProcessing(true)
        setProcessProgress(null)
      } else if (msg.status === "progress" && msg.current !== undefined && msg.total !== undefined) {
        setProcessing(true)
        setProcessProgress({ current: msg.current, total: msg.total })
      } else if (msg.status === "complete" || msg.status === "error") {
        setProcessing(false)
        setProcessProgress(null)
        fetchDriveStats()
      }
    })

    return () => {
      mounted = false
      clearInterval(statusInterval)
      clearInterval(statsInterval)
      clearInterval(storageInterval)
      clearInterval(uptimeInterval)
      unsubscribe()
    }
  }, [])

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-20">
        <Activity className="mb-4 h-12 w-12 text-slate-600" />
        <p className="text-lg font-medium text-slate-400">{error}</p>
        <p className="mt-1 text-sm text-slate-600">
          Make sure the Sentry USB API server is running
        </p>
      </div>
    )
  }

  if (!status) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-bold text-slate-100">Dashboard</h1>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="glass-card h-24 animate-pulse" />
          ))}
        </div>
      </div>
    )
  }

  const cpuTemp = parseInt(status.cpu_temp)
  const totalSpace = parseInt(status.total_space)
  const freeSpace = parseInt(status.free_space)
  const usedSpace = totalSpace - freeSpace
  const usedPercent = totalSpace > 0 ? ((usedSpace / totalSpace) * 100).toFixed(0) : "0"
  const wifiBars = getWifiStrengthBars(status.wifi_strength)
  const snapshotCount = parseInt(status.num_snapshots)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-100">Dashboard</h1>
        <p className="mt-1 text-sm text-slate-500">
          System overview and status
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <StatCard
          icon={Clock}
          label="Uptime"
          value={formatUptime(uptime)}
          color="blue"
        />

        <StatCard
          icon={Thermometer}
          label="CPU Temperature"
          value={cpuTemp > 0 ? formatTemp(cpuTemp, useFahrenheit) : "N/A"}
          color={cpuTemp > 0 ? getTempColor(cpuTemp) : "blue"}
        />

        <StatCard
          icon={HardDrive}
          label="Storage"
          value={`${formatBytes(usedSpace)} / ${formatBytes(totalSpace)}`}
          sub={`${usedPercent}% used`}
          color={parseInt(usedPercent) > 90 ? "red" : parseInt(usedPercent) > 75 ? "amber" : "emerald"}
        />

        <StatCard
          icon={Wifi}
          label="WiFi"
          value={status.wifi_ssid || "Not connected"}
          sub={
            status.wifi_ssid
              ? `${status.wifi_ip || "No IP"} · ${wifiBars}/4 bars`
              : undefined
          }
          color={status.wifi_ssid ? "emerald" : "red"}
        />

        {status.ether_speed && (
          <StatCard
            icon={Cable}
            label="Ethernet"
            value={
              status.ether_speed === "Unknown!"
                ? "Not connected"
                : status.ether_speed
            }
            sub={status.ether_ip || undefined}
            color={status.ether_speed === "Unknown!" ? "red" : "emerald"}
          />
        )}

        <StatCard
          icon={Camera}
          label="Snapshots"
          value={`${snapshotCount}`}
          sub={
            snapshotCount > 0
              ? `${new Date(parseInt(status.snapshot_oldest) * 1000).toLocaleDateString()} — ${new Date(parseInt(status.snapshot_newest) * 1000).toLocaleDateString()}`
              : "No snapshots"
          }
          color="purple"
        />

        <StatCard
          icon={HardDrive}
          label="USB Drives"
          value={status.drives_active === "yes" ? "Connected" : "Disconnected"}
          sub={
            status.drives_active === "yes"
              ? "Visible to host"
              : "Not visible to host"
          }
          color={status.drives_active === "yes" ? "emerald" : "amber"}
        />
      </div>

      {/* Storage bar */}
      <div className="glass-card p-4">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-sm font-medium text-slate-300">
            Storage Usage
          </span>
          <span className="text-xs text-slate-500">
            {formatBytes(freeSpace)} free of {formatBytes(totalSpace)}
          </span>
        </div>
        {storageBreakdown && storageBreakdown.total_space > 0 ? (() => {
          const segments = [
            { label: "Dashcam", size: storageBreakdown.cam_size, color: "#3b82f6" },
            { label: "Music", size: storageBreakdown.music_size, color: "#a855f7" },
            { label: "Lightshow", size: storageBreakdown.lightshow_size, color: "#f59e0b" },
            { label: "Boombox", size: storageBreakdown.boombox_size, color: "#ec4899" },
            { label: "Wraps", size: storageBreakdown.wraps_size, color: "#14b8a6" },
            { label: "Snapshots", size: storageBreakdown.snapshots_size, color: "#6366f1" },
          ].filter(s => s.size > 0)
          // Give each segment a minimum width so small drives remain visible
          const minPct = 4
          const reservedPct = segments.length * minPct
          const flexPct = Math.max(100 - reservedPct, 0)
          const usedTotal = segments.reduce((acc, s) => acc + s.size, 0)
          return (
            <>
              <div className="h-3 w-full overflow-hidden rounded-full bg-slate-800 flex">
                {segments.map((s) => {
                  const proportional = usedTotal > 0 ? (s.size / usedTotal) * flexPct : 0
                  const pct = minPct + proportional
                  return (
                    <div
                      key={s.label}
                      className="h-full transition-all duration-500 first:rounded-l-full last:rounded-r-full"
                      style={{
                        width: `${pct}%`,
                        backgroundColor: s.color,
                      }}
                      title={`${s.label}: ${formatBytes(s.size)}`}
                    />
                  )
                })}
              </div>
              <div className="mt-2.5 flex flex-wrap gap-x-4 gap-y-1">
                {segments.map((s) => (
                  <div key={s.label} className="flex items-center gap-1.5 text-xs">
                    <span
                      className="inline-block h-2 w-2 rounded-full"
                      style={{ backgroundColor: s.color }}
                    />
                    <span className="text-slate-400">{s.label}</span>
                    <span className="font-medium text-slate-300">{formatBytes(s.size)}</span>
                  </div>
                ))}
                <div className="flex items-center gap-1.5 text-xs">
                  <span className="inline-block h-2 w-2 rounded-full bg-slate-700" />
                  <span className="text-slate-400">Free</span>
                  <span className="font-medium text-slate-300">{formatBytes(storageBreakdown.free_space)}</span>
                </div>
              </div>
            </>
          )
        })() : (
          <div className="h-3 w-full overflow-hidden rounded-full bg-slate-800">
            <div
              className="h-full rounded-full bg-gradient-to-r from-blue-500 to-blue-400 transition-all duration-500"
              style={{ width: `${usedPercent}%` }}
            />
          </div>
        )}
      </div>

      {/* Keep-Awake card */}
      <KeepAwakeCard />

      {/* Archive progress */}
      <div className="glass-card p-4">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Archive className="h-4 w-4 text-slate-400" />
            <span className="text-sm font-medium text-slate-300">
              Clip Archive Progress
            </span>
          </div>
          {active && (
            <span className="flex items-center gap-1.5 text-xs text-emerald-400">
              <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400" />
              {archiveProgress ? "Archiving" : "Processing"}
            </span>
          )}
        </div>

        {driveStats ? (
          <>
            <div className="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div>
                <p className="text-xs text-slate-500">Lifetime Clips Processed</p>
                <p className="mt-0.5 text-lg font-semibold text-slate-100">
                  {driveStats.processed_count.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500">Lifetime Drives Found</p>
                <p className="mt-0.5 text-lg font-semibold text-slate-100">
                  {driveStats.drives_count.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500">Lifetime Distance</p>
                <p className="mt-0.5 text-lg font-semibold text-slate-100">
                  {metric ? driveStats.total_distance_km.toFixed(1) : driveStats.total_distance_mi.toFixed(1)}{" "}
                  <span className="text-sm font-normal text-slate-400">{metric ? "km" : "mi"}</span>
                </p>
              </div>
            </div>

            {driveStats.fsd_engaged_ms > 0 && (
              <div className="mb-4 grid grid-cols-1 gap-3 rounded-lg border border-emerald-500/10 bg-emerald-500/5 p-3 sm:grid-cols-3">
                <div>
                  <p className="text-xs text-emerald-400/70">Lifetime FSD Usage</p>
                  <p className="mt-0.5 text-lg font-semibold text-emerald-400">
                    {driveStats.fsd_percent}%
                  </p>
                </div>
                <div>
                  <p className="text-xs text-red-400/70">Lifetime Disengagements</p>
                  <p className="mt-0.5 text-lg font-semibold text-red-400">
                    {driveStats.fsd_disengagements}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-emerald-400/70">Lifetime FSD Distance</p>
                  <p className="mt-0.5 text-lg font-semibold text-emerald-400">
                    {metric ? driveStats.fsd_distance_km.toFixed(1) : driveStats.fsd_distance_mi.toFixed(1)}{" "}
                    <span className="text-sm font-normal text-emerald-400/60">{metric ? "km" : "mi"}</span>
                  </p>
                </div>
              </div>
            )}

            {archiveProgress && archiveProgress.total > 0 ? (
              <>
                <div className="mb-1.5 flex items-center justify-between text-xs text-slate-500">
                  <span>
                    Archiving: {archiveProgress.current.toLocaleString()} /{" "}
                    {archiveProgress.total.toLocaleString()} files
                  </span>
                  <span>
                    {Math.round(
                      (archiveProgress.current / archiveProgress.total) * 100
                    )}
                    %
                  </span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-slate-800">
                  <div
                    className="h-full rounded-full bg-gradient-to-r from-emerald-500 to-emerald-400 transition-all duration-500"
                    style={{
                      width: `${(archiveProgress.current / archiveProgress.total) * 100}%`,
                    }}
                  />
                </div>
              </>
            ) : processing && processProgress && processProgress.total > 0 ? (
              <>
                <div className="mb-1.5 flex items-center justify-between text-xs text-slate-500">
                  <span>
                    Processing: {processProgress.current.toLocaleString()} /{" "}
                    {processProgress.total.toLocaleString()} files
                  </span>
                  <span>
                    {Math.round(
                      (processProgress.current / processProgress.total) * 100
                    )}
                    %
                  </span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-slate-800">
                  <div
                    className="h-full rounded-full bg-gradient-to-r from-emerald-500 to-emerald-400 transition-all duration-500"
                    style={{
                      width: `${(processProgress.current / processProgress.total) * 100}%`,
                    }}
                  />
                </div>
              </>
            ) : active ? (
              <div className="h-2 w-full overflow-hidden rounded-full bg-slate-800">
                <div className="h-full w-2/5 animate-pulse rounded-full bg-gradient-to-r from-emerald-500 to-emerald-400" />
              </div>
            ) : (
              <div className="h-2 w-full overflow-hidden rounded-full bg-slate-800">
                <div
                  className="h-full rounded-full bg-gradient-to-r from-emerald-500/60 to-emerald-400/60"
                  style={{
                    width: driveStats.processed_count > 0 ? "100%" : "0%",
                  }}
                />
              </div>
            )}
          </>
        ) : (
          <div className="space-y-2">
            <div className="h-4 w-1/2 animate-pulse rounded bg-slate-800" />
            <div className="h-2 w-full animate-pulse rounded-full bg-slate-800" />
          </div>
        )}
      </div>
    </div>
  )
}

const DURATION_OPTIONS = [
  { label: "15 min", value: 15 },
  { label: "30 min", value: 30 },
  { label: "1 hour", value: 60 },
  { label: "2 hours", value: 120 },
]

function KeepAwakeCard() {
  const { status, mode, start, stop } = useKeepAwake()
  const [showDurations, setShowDurations] = useState(false)

  // Don't show the card if user hasn't configured a mode
  if (!mode) return null

  const isActive = status.state === "active"
  const isPending = status.state === "pending"
  const isIdle = status.state === "idle"

  const remainingMin = status.remaining_sec ? Math.ceil(status.remaining_sec / 60) : 0

  return (
    <div className="glass-card p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {isActive ? (
            <HeartPulse className="h-4 w-4 animate-pulse text-rose-400" />
          ) : isPending ? (
            <Timer className="h-4 w-4 animate-pulse text-amber-400" />
          ) : (
            <HeartPulse className="h-4 w-4 text-slate-600" />
          )}
          <span className="text-sm font-medium text-slate-300">Keep Awake</span>
          {isActive && (
            <span className="rounded-full bg-rose-500/15 px-2 py-0.5 text-[10px] font-medium text-rose-400">
              {remainingMin}m remaining
            </span>
          )}
          {isPending && (
            <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium text-amber-400">
              Waiting for archive...
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          {mode === "manual" && isIdle && (
            <div className="relative">
              <button
                onClick={() => setShowDurations(!showDurations)}
                className="rounded-lg bg-blue-500/20 px-3 py-1.5 text-xs font-medium text-blue-400 transition-colors hover:bg-blue-500/30"
              >
                Keep Awake
              </button>
              {showDurations && (
                <div className="absolute right-0 top-full z-10 mt-1 w-32 rounded-lg border border-white/10 bg-slate-900 p-1 shadow-xl">
                  {DURATION_OPTIONS.map((opt) => (
                    <button
                      key={opt.value}
                      onClick={() => { start(opt.value); setShowDurations(false) }}
                      className="w-full rounded-md px-3 py-1.5 text-left text-xs text-slate-300 hover:bg-white/5"
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {mode === "auto" && isIdle && (
            <span className="text-xs text-slate-600">Auto — interact to activate</span>
          )}
          {(isActive || isPending) && (
            <button
              onClick={stop}
              className="rounded-lg bg-red-500/15 px-3 py-1.5 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/25"
            >
              Stop
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
