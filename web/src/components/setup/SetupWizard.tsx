import { useState, useCallback, useEffect, useRef } from "react"
import { ChevronLeft, ChevronRight, Check, Loader2, AlertCircle, CheckCircle } from "lucide-react"
import { cn } from "@/lib/utils"
import { SetupProgress } from "./SetupProgress"
import { WelcomeStep } from "./steps/WelcomeStep"
import { NetworkStep } from "./steps/NetworkStep"
import { StorageStep } from "./steps/StorageStep"
import { ArchiveStep } from "./steps/ArchiveStep"
import { KeepAwakeStep } from "./steps/KeepAwakeStep"
import { NotificationsStep } from "./steps/NotificationsStep"
import { SecurityStep } from "./steps/SecurityStep"
import { AdvancedStep } from "./steps/AdvancedStep"
import { ReviewStep } from "./steps/ReviewStep"

export interface SetupFormData {
  [key: string]: string
}

interface StepDef {
  id: string
  title: string
  component: React.ComponentType<StepProps>
}

export interface StepProps {
  data: SetupFormData
  onChange: (key: string, value: string) => void
  onBatchChange: (updates: Record<string, string>) => void
}

function networkError(data: SetupFormData): string | null {
  if (data.AP_SSID && (data.AP_PASS ?? "").length < 8)
    return "WiFi Access Point password must be at least 8 characters."
  return null
}

function storageError(data: SetupFormData): string | null {
  const musicSize = (data.MUSIC_SIZE ?? "").replace(/[^0-9]/g, "")
  if (musicSize && !(data.MUSIC_SHARE_NAME ?? "").trim())
    return "Music Share Name is required when a music drive size is set."
  return null
}

function archiveError(data: SetupFormData): string | null {
  const system = data.ARCHIVE_SYSTEM ?? "cifs"
  if (system === "none") return null
  if (system === "cifs") {
    if (!data.ARCHIVE_SERVER?.trim()) return "Archive Server is required."
    if (!data.SHARE_NAME?.trim()) return "Share Name is required."
    if (!data.SHARE_USER?.trim()) return "Username is required."
    if (!data.SHARE_PASSWORD?.trim()) return "Password is required."
  } else if (system === "rsync") {
    if (!data.RSYNC_SERVER?.trim()) return "Server is required."
    if (!data.RSYNC_USER?.trim()) return "Username is required."
    if (!data.RSYNC_PATH?.trim()) return "Remote Path is required."
  } else if (system === "rclone") {
    if (!data.RCLONE_DRIVE?.trim()) return "Remote Name is required."
    if (!data.RCLONE_PATH?.trim()) return "Remote Path is required."
  } else if (system === "nfs") {
    if (!data.ARCHIVE_SERVER?.trim()) return "NFS Server is required."
    if (!data.SHARE_NAME?.trim()) return "Export Path is required."
  }
  return null
}

function keepAwakeError(data: SetupFormData): string | null {
  const method = data._KEEP_AWAKE_METHOD
    || (data.TESLA_BLE_VIN ? "ble"
      : data.TESLAFI_API_TOKEN ? "teslafi"
        : data.TESSIE_API_TOKEN ? "tessie"
          : data.KEEP_AWAKE_WEBHOOK_URL ? "webhook"
            : "none")
  if (method === "none") return null
  if (method === "ble" && !data.TESLA_BLE_VIN?.trim()) return "Vehicle VIN is required for Bluetooth LE."
  if (method === "teslafi" && !data.TESLAFI_API_TOKEN?.trim()) return "TeslaFi API Token is required."
  if (method === "tessie" && !data.TESSIE_API_TOKEN?.trim()) return "Tessie API Token is required."
  if (method === "tessie" && !data.TESSIE_VIN?.trim()) return "Vehicle VIN is required for Tessie."
  if (method === "webhook" && !data.KEEP_AWAKE_WEBHOOK_URL?.trim()) return "Webhook URL is required."
  if (!data.SENTRY_CASE) return "Sentry Mode behavior must be selected."
  return null
}

function notificationsError(data: SetupFormData): string | null {
  const requiredPerProvider: [string, string, string[]][] = [
    ["Pushover", "PUSHOVER_ENABLED", ["PUSHOVER_USER_KEY", "PUSHOVER_APP_KEY"]],
    ["Gotify", "GOTIFY_ENABLED", ["GOTIFY_DOMAIN", "GOTIFY_APP_TOKEN"]],
    ["Discord", "DISCORD_ENABLED", ["DISCORD_WEBHOOK_URL"]],
    ["Telegram", "TELEGRAM_ENABLED", ["TELEGRAM_CHAT_ID", "TELEGRAM_BOT_TOKEN"]],
    ["IFTTT", "IFTTT_ENABLED", ["IFTTT_EVENT_NAME", "IFTTT_KEY"]],
    ["Slack", "SLACK_ENABLED", ["SLACK_WEBHOOK_URL"]],
    ["Signal", "SIGNAL_ENABLED", ["SIGNAL_URL", "SIGNAL_FROM_NUM", "SIGNAL_TO_NUM"]],
    ["Matrix", "MATRIX_ENABLED", ["MATRIX_SERVER_URL", "MATRIX_USERNAME", "MATRIX_PASSWORD", "MATRIX_ROOM"]],
    ["AWS SNS", "SNS_ENABLED", ["AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SNS_TOPIC_ARN"]],
    ["Webhook", "WEBHOOK_ENABLED", ["WEBHOOK_URL"]],
  ]
  for (const [label, enableField, fields] of requiredPerProvider) {
    if (data[enableField] === "true" && fields.some(f => !data[f]?.trim()))
      return `Complete all required fields for ${label}.`
  }
  return null
}

function securityError(data: SetupFormData): string | null {
  if (data.WEB_USERNAME?.trim() && !data.WEB_PASSWORD?.trim())
    return "Web Password is required when a Web Username is set."
  return null
}

function getStepError(stepIdx: number, data: SetupFormData): string | null {
  switch (stepIdx) {
    case 1: return networkError(data)
    case 2: return storageError(data)
    case 3: return archiveError(data)
    case 4: return keepAwakeError(data)
    case 5: return notificationsError(data)
    case 6: return securityError(data)
    default: return null
  }
}

const steps: StepDef[] = [
  { id: "welcome", title: "Welcome", component: WelcomeStep },
  { id: "network", title: "Network", component: NetworkStep },
  { id: "storage", title: "Storage", component: StorageStep },
  { id: "archive", title: "Archive", component: ArchiveStep },
  { id: "keepawake", title: "Keep Awake", component: KeepAwakeStep },
  { id: "notifications", title: "Notifications", component: NotificationsStep },
  { id: "security", title: "Security", component: SecurityStep },
  { id: "advanced", title: "Advanced", component: AdvancedStep },
  { id: "review", title: "Review", component: ReviewStep },
]

interface SetupWizardProps {
  initialData?: SetupFormData
  onClose: () => void
}

type SetupPhase = "wizard" | "applying" | "running" | "rebooting" | "finalizing" | "complete" | "error"

export function SetupWizard({ initialData, onClose }: SetupWizardProps) {
  const [currentStep, setCurrentStep] = useState(0)
  // Defaults for fields that appear pre-selected in the UI but may not exist
  // in the config file yet. Without this, untouched defaults never get saved.
  const defaults: SetupFormData = {
    CAM_SIZE: "40",
    ARCHIVE_SYSTEM: "cifs",
    TEMPERATURE_UNIT: "C",
    ARCHIVE_SAVEDCLIPS: "true",
    ARCHIVE_SENTRYCLIPS: "true",
    ARCHIVE_RECENTCLIPS: "true",
    ARCHIVE_TRACKMODECLIPS: "true",
    DRIVE_MAP_ENABLED: "true",
    DRIVE_MAP_WHILE_AWAY: "true",
    DRIVE_MAP_UNIT: "mi",
    TEMPERATURE_POSTARCHIVE: "true",
    USE_EXFAT: "true",
  }
  const [formData, setFormData] = useState<SetupFormData>({ ...defaults, ...(initialData ?? {}) })
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [phase, setPhase] = useState<SetupPhase>("wizard")
  const [setupMessage, setSetupMessage] = useState("")
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const handleChange = useCallback((key: string, value: string) => {
    setFormData((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleBatchChange = useCallback((updates: Record<string, string>) => {
    setFormData((prev) => ({ ...prev, ...updates }))
  }, [])

  // Poll setup status while running
  useEffect(() => {
    if (phase !== "running" && phase !== "rebooting") return
    pollRef.current = setInterval(async () => {
      try {
        const res = await fetch("/api/setup/status")
        const data = await res.json()
        if (data.setup_finished) {
          // Setup scripts are done — the Pi will do a final reboot.
          // Transition to "finalizing" which keeps the spinner and
          // waits for the server to come back before showing dashboard.
          setPhase("finalizing")
          setSetupMessage("Sentry USB has finished setting up. The device is now rebooting one last time...")
          if (pollRef.current) clearInterval(pollRef.current)
        } else if (data.setup_running && phase === "rebooting") {
          // Server is back and setup is still going — restore live progress view.
          // This recovers from transient blips (service restart, heavy I/O, etc.)
          // that previously left the UI permanently stuck in "rebooting".
          setPhase("running")
          setSetupMessage("Setup is running. The device will reboot several times during this process — this is normal.")
        } else if (!data.setup_running && phase === "running") {
          setPhase("rebooting")
          setSetupMessage("System is rebooting to continue setup. This page will reconnect automatically.")
        }
      } catch {
        // Server unreachable — likely rebooting, which is expected
        if (phase !== "rebooting") {
          setPhase("rebooting")
          setSetupMessage("Waiting for device to come back online after reboot...")
        }
      }
    }, 3000)
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [phase])

  // Poll during finalizing — wait for server to go DOWN then come back UP.
  // Without the wentDown gate, the first poll can succeed while the Pi is
  // still shutting down (exec reboot takes a few seconds to kill the server),
  // causing a premature "Setup Complete!" before the Pi has actually rebooted.
  useEffect(() => {
    if (phase !== "finalizing") return
    let wentDown = false
    const poll = setInterval(async () => {
      try {
        const res = await fetch("/api/setup/status")
        if (res.ok && wentDown) {
          // Server is back up after confirmed reboot
          setPhase("complete")
          setSetupMessage("Setup completed successfully! Your device is ready.")
          clearInterval(poll)
        }
      } catch {
        // Server unreachable — Pi is rebooting
        wentDown = true
        setSetupMessage("Waiting for Sentry USB to come back online after final reboot...")
      }
    }, 3000)
    return () => clearInterval(poll)
  }, [phase])

  // Also listen to WebSocket for real-time updates (auto-reconnect on drop)
  useEffect(() => {
    if (phase !== "running" && phase !== "applying" && phase !== "rebooting") return
    let ws: WebSocket | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let backoff = 2000
    let cancelled = false

    function connect() {
      if (cancelled) return
      try {
        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
        ws = new WebSocket(`${protocol}//${window.location.host}/api/ws`)
        ws.onopen = () => { backoff = 2000 }
        ws.onmessage = (event) => {
          try {
            const msg = JSON.parse(event.data)
            if (msg.type === "setup_status") {
              const d = msg.data
              if (d.status === "starting" || d.status === "downloading_scripts") {
                setPhase("running")
                setSetupMessage("Downloading setup scripts...")
              } else if (d.status === "running") {
                setSetupMessage("Running setup... This may take several minutes.")
              } else if (d.status === "complete") {
                setPhase("finalizing")
                setSetupMessage("Sentry USB has finished setting up. The device is now rebooting one last time...")
              } else if (d.status === "rebooting") {
                setPhase("rebooting")
                setSetupMessage(d.message || "System is rebooting to continue setup...")
              } else if (d.status === "error") {
                setPhase("error")
                setSetupMessage(d.error || "Setup failed. Check logs for details.")
              }
            }
          } catch { /* ignore parse errors */ }
        }
        ws.onclose = () => {
          if (cancelled) return
          reconnectTimer = setTimeout(() => {
            backoff = Math.min(backoff * 1.5, 15000)
            connect()
          }, backoff)
        }
      } catch { /* ws not available */ }
    }

    connect()
    return () => {
      cancelled = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      ws?.close()
    }
  }, [phase])

  const StepComponent = steps[currentStep].component
  const currentStepError = getStepError(currentStep, formData)

  async function handleApply() {
    const firstInvalidIdx = steps.findIndex((_, i) => getStepError(i, formData) !== null)
    if (firstInvalidIdx !== -1) {
      setCurrentStep(firstInvalidIdx)
      setSaveError(getStepError(firstInvalidIdx, formData))
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      // Strip internal UI-only fields (prefixed with _) before saving
      const sizeFields = new Set(["CAM_SIZE", "MUSIC_SIZE", "LIGHTSHOW_SIZE", "BOOMBOX_SIZE"])
      const configData = Object.fromEntries(
        Object.entries(formData)
          .filter(([k, v]) => !k.startsWith("_") && v !== "")
          .map(([k, v]) => {
            // Append G suffix to size fields if it's a plain number
            if (sizeFields.has(k) && /^\d+$/.test(v)) {
              return [k, v + "G"]
            }
            // Convert temperature fields from °C to milli-°C for the config
            if ((k === "TEMPERATURE_WARNING" || k === "TEMPERATURE_CAUTION") && v && !v.includes("000")) {
              const num = parseFloat(v)
              if (!isNaN(num)) return [k, String(Math.round(num * 1000))]
            }
            return [k, v]
          })
      )
      const res = await fetch("/api/setup/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(configData),
      })
      if (!res.ok) throw new Error("Failed to save configuration")

      setPhase("applying")
      setSetupMessage("Configuration saved. Starting setup...")

      // Trigger setup
      const runRes = await fetch("/api/setup/run", { method: "POST" })
      if (!runRes.ok) {
        const err = await runRes.json()
        throw new Error(err.error || "Failed to start setup")
      }

      setPhase("running")
      setSetupMessage("Setup is running. The device will reboot several times during this process — this is normal.")
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Unknown error")
      setPhase("wizard")
    } finally {
      setSaving(false)
    }
  }

  const isLast = currentStep === steps.length - 1
  const isFirst = currentStep === 0

  // ── Progress screen (shown after Apply) ──
  if (phase !== "wizard") {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
        <div className="glass-card flex w-full max-w-lg flex-col items-center gap-6 p-10 text-center">
          {phase === "applying" || phase === "running" || phase === "rebooting" || phase === "finalizing" ? (
            <>
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-blue-500/20">
                <Loader2 className="h-8 w-8 animate-spin text-blue-400" />
              </div>
              <div>
                <h2 className="text-xl font-semibold text-slate-100">
                  {phase === "finalizing" ? "Almost Done!" : "Setting Up Sentry USB"}
                </h2>
                <p className="mt-2 text-sm text-slate-400">{setupMessage}</p>
                {phase !== "finalizing" && (
                  <p className="mt-4 text-xs text-slate-600">
                    This process creates disk images, configures archiving, and sets up USB gadget mode.
                    The device will reboot multiple times — this is completely normal.
                    Setup continues automatically after each reboot. Do not power off the device.
                    The full process may take 10-20 minutes.
                  </p>
                )}
                {phase === "finalizing" && (
                  <p className="mt-4 text-xs text-slate-600">
                    Sentry USB is performing its final reboot. This page will automatically
                    redirect you to the dashboard once the device is back online.
                  </p>
                )}
              </div>
              <SetupProgress />
            </>
          ) : phase === "complete" ? (
            <>
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-emerald-500/20">
                <CheckCircle className="h-8 w-8 text-emerald-400" />
              </div>
              <div>
                <h2 className="text-xl font-semibold text-slate-100">Setup Complete!</h2>
                <p className="mt-2 text-sm text-slate-400">{setupMessage}</p>
              </div>
              <button
                onClick={onClose}
                className="rounded-lg bg-blue-500 px-6 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-600"
              >
                Go to Dashboard
              </button>
            </>
          ) : (
            <>
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-red-500/20">
                <AlertCircle className="h-8 w-8 text-red-400" />
              </div>
              <div>
                <h2 className="text-xl font-semibold text-slate-100">Setup Error</h2>
                <p className="mt-2 text-sm text-red-400">{setupMessage}</p>
              </div>
              <div className="flex gap-3">
                <button
                  onClick={() => { setPhase("wizard"); setCurrentStep(steps.length - 1) }}
                  className="rounded-lg border border-white/10 bg-white/5 px-4 py-2 text-sm font-medium text-slate-300 transition-colors hover:bg-white/10"
                >
                  Back to Wizard
                </button>
                <button
                  onClick={handleApply}
                  className="rounded-lg bg-blue-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-600"
                >
                  Retry
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    )
  }

  // ── Wizard steps ──
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="glass-card relative flex h-[90vh] w-full max-w-3xl flex-col overflow-hidden">
        {/* Header with step indicator */}
        <div className="shrink-0 border-b border-white/5 px-6 py-4">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-lg font-semibold text-slate-100">
              Setup Wizard
            </h2>
            <button
              onClick={onClose}
              className="rounded-lg px-3 py-1 text-sm text-slate-500 hover:bg-white/5 hover:text-slate-300"
            >
              Cancel
            </button>
          </div>

          {/* Step progress bar */}
          <div className="flex gap-1">
            {steps.map((step, i) => (
              <button
                key={step.id}
                onClick={() => {
                  if (i > currentStep) {
                    for (let s = 0; s < i; s++) {
                      if (getStepError(s, formData) !== null) {
                        setCurrentStep(s)
                        return
                      }
                    }
                  }
                  setCurrentStep(i)
                }}
                className="group flex-1"
                title={step.title}
              >
                <div
                  className={cn(
                    "h-1 rounded-full transition-all",
                    i === currentStep
                      ? "bg-blue-400"
                      : i < currentStep && getStepError(i, formData) !== null
                        ? "bg-red-500/70"
                        : i < currentStep
                          ? "bg-blue-500"
                          : "bg-slate-800"
                  )}
                />
                <p
                  className={cn(
                    "mt-1 hidden text-[10px] font-medium sm:block",
                    i <= currentStep ? "text-slate-400" : "text-slate-700"
                  )}
                >
                  {step.title}
                </p>
              </button>
            ))}
          </div>
        </div>

        {/* Step content */}
        <div className="flex-1 overflow-y-auto px-6 py-5">
          <StepComponent
            data={formData}
            onChange={handleChange}
            onBatchChange={handleBatchChange}
          />
        </div>

        {/* Footer navigation */}
        <div className="shrink-0 border-t border-white/5 px-6 py-4">
          {saveError && (
            <p className="mb-2 text-sm text-red-400">{saveError}</p>
          )}
          {currentStepError && (
            <p className="mb-2 text-sm text-red-400">{currentStepError}</p>
          )}
          <div className="flex items-center justify-between">
            <button
              onClick={() => setCurrentStep((s) => s - 1)}
              disabled={isFirst}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors",
                isFirst
                  ? "text-slate-700"
                  : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
              )}
            >
              <ChevronLeft className="h-4 w-4" />
              Back
            </button>

            <span className="text-xs text-slate-600">
              {currentStep + 1} / {steps.length}
            </span>

            {isLast ? (
              <button
                onClick={handleApply}
                disabled={saving}
                className="flex items-center gap-1.5 rounded-lg bg-blue-500 px-5 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-600 disabled:opacity-50"
              >
                {saving ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Check className="h-4 w-4" />
                )}
                Apply & Run Setup
              </button>
            ) : (
              <button
                onClick={() => setCurrentStep((s) => s + 1)}
                disabled={!!currentStepError}
                className="flex items-center gap-1.5 rounded-lg bg-blue-500/20 px-4 py-2 text-sm font-medium text-blue-400 transition-colors hover:bg-blue-500/30 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                Next
                <ChevronRight className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
