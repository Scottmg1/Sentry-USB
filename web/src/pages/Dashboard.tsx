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
} from "lucide-react"
import { api } from "@/lib/api"
import type { PiStatus, DriveStats } from "@/lib/api"
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
  const [archiveProgress, setArchiveProgress] = useState<ProcessProgress | null>(null)
  const [processing, setProcessing] = useState(false)
  const [processProgress, setProcessProgress] = useState<ProcessProgress | null>(null)
  const [metric, setMetric] = useState(false)

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
        if (mounted) setError("Unable to connect to SentryUSB")
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

    fetchStatus()
    fetchDriveStats()
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
      })
      .catch(() => {})
    const statusInterval = setInterval(fetchStatus, 4000)
    const statsInterval = setInterval(fetchDriveStats, 5000)

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
          Make sure the SentryUSB API server is running
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
          value={cpuTemp > 0 ? formatTemp(cpuTemp) : "N/A"}
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
            {formatBytes(freeSpace)} free
          </span>
        </div>
        <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-800">
          <div
            className="h-full rounded-full bg-gradient-to-r from-blue-500 to-blue-400 transition-all duration-500"
            style={{ width: `${usedPercent}%` }}
          />
        </div>
      </div>

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
            <div className="mb-4 grid grid-cols-3 gap-3">
              <div>
                <p className="text-xs text-slate-500">Clips Processed</p>
                <p className="mt-0.5 text-lg font-semibold text-slate-100">
                  {driveStats.processed_count.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500">Drives Found</p>
                <p className="mt-0.5 text-lg font-semibold text-slate-100">
                  {driveStats.drives_count.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500">Distance</p>
                <p className="mt-0.5 text-lg font-semibold text-slate-100">
                  {metric ? driveStats.total_distance_km.toFixed(1) : driveStats.total_distance_mi.toFixed(1)}{" "}
                  <span className="text-sm font-normal text-slate-400">{metric ? "km" : "mi"}</span>
                </p>
              </div>
            </div>

            {driveStats.fsd_engaged_ms > 0 && (
              <div className="mb-4 grid grid-cols-3 gap-3 rounded-lg border border-emerald-500/10 bg-emerald-500/5 p-3">
                <div>
                  <p className="text-xs text-emerald-400/70">FSD Usage</p>
                  <p className="mt-0.5 text-lg font-semibold text-emerald-400">
                    {driveStats.fsd_percent}%
                  </p>
                </div>
                <div>
                  <p className="text-xs text-red-400/70">Disengagements</p>
                  <p className="mt-0.5 text-lg font-semibold text-red-400">
                    {driveStats.fsd_disengagements}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-emerald-400/70">FSD Distance</p>
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
