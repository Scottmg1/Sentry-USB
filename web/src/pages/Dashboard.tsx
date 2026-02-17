import { useEffect, useState } from "react"
import {
  Thermometer,
  HardDrive,
  Wifi,
  Clock,
  Camera,
  Activity,
  Cable,
} from "lucide-react"
import { api } from "@/lib/api"
import type { PiStatus } from "@/lib/api"
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

export default function Dashboard() {
  const [status, setStatus] = useState<PiStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [uptime, setUptime] = useState(0)

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

    fetchStatus()
    const statusInterval = setInterval(fetchStatus, 4000)

    uptimeInterval = setInterval(() => {
      setUptime((prev) => prev + 1)
    }, 1000)

    return () => {
      mounted = false
      clearInterval(statusInterval)
      clearInterval(uptimeInterval)
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
    </div>
  )
}
