import { useState } from "react"
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
} from "lucide-react"
import { cn } from "@/lib/utils"
import { SetupWizard } from "@/components/setup/SetupWizard"

function ActionButton({
  icon: Icon,
  label,
  description,
  variant = "default",
  onClick,
}: {
  icon: React.ElementType
  label: string
  description: string
  variant?: "default" | "danger"
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className="glass-card glass-card-hover flex items-start gap-3 p-4 text-left transition-colors"
    >
      <div
        className={cn(
          "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg",
          variant === "danger"
            ? "bg-red-500/15 text-red-400"
            : "bg-blue-500/15 text-blue-400"
        )}
      >
        <Icon className="h-5 w-5" />
      </div>
      <div>
        <p className="text-sm font-medium text-slate-200">{label}</p>
        <p className="mt-0.5 text-xs text-slate-500">{description}</p>
      </div>
    </button>
  )
}

type UpdateStatus = "idle" | "checking" | "downloading" | "installing" | "restarting" | "done" | "error"

export default function Settings() {
  const [confirmReboot, setConfirmReboot] = useState(false)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus>("idle")
  const [updateError, setUpdateError] = useState<string | null>(null)

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
      }, 5000)
    } catch (err) {
      setUpdateStatus("error")
      setUpdateError(err instanceof Error ? err.message : "Update failed")
    }
  }

  function handleReboot() {
    if (!confirmReboot) {
      setConfirmReboot(true)
      setTimeout(() => setConfirmReboot(false), 10000)
      return
    }
    fetch("/api/system/reboot", { method: "POST" })
    setConfirmReboot(false)
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
          <button
            onClick={() => setWizardOpen(true)}
            className="shrink-0 rounded-lg bg-blue-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-600"
          >
            Open Wizard
          </button>
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
            onClick={() => fetch("/api/system/toggle-drives", { method: "POST" })}
          />
          <ActionButton
            icon={RefreshCw}
            label="Trigger Archive Sync"
            description="Start archiving recorded clips now"
            onClick={() => fetch("/api/system/trigger-sync", { method: "POST" })}
          />
          <ActionButton
            icon={Gauge}
            label="Network Speed Test"
            description="Test the network throughput"
            onClick={async () => {
              const start = Date.now()
              try {
                const res = await fetch("/api/system/speedtest")
                const blob = await res.blob()
                const elapsed = (Date.now() - start) / 1000
                const mbps = ((blob.size * 8) / elapsed / 1_000_000).toFixed(1)
                alert(`Download: ${mbps} Mbps\n${(blob.size / 1_000_000).toFixed(1)} MB in ${elapsed.toFixed(1)}s`)
              } catch {
                alert("Speed test failed. Check connection.")
              }
            }}
          />
          <ActionButton
            icon={Bluetooth}
            label="Pair BLE with Car"
            description="Initiate Bluetooth Low Energy pairing"
            onClick={() => fetch("/api/system/ble-pair", { method: "POST" })}
          />
        </div>
      </div>

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
            {(updateStatus === "checking" || updateStatus === "downloading" || updateStatus === "installing" || updateStatus === "restarting") && (
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
            description="Edit raw configuration variables"
            onClick={() => {}}
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
      </div>

      {/* Setup Wizard Modal */}
      {wizardOpen && (
        <SetupWizard onClose={() => setWizardOpen(false)} />
      )}
    </div>
  )
}
