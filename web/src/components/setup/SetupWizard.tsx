import { useState, useCallback } from "react"
import { ChevronLeft, ChevronRight, Check, Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
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

export function SetupWizard({ initialData, onClose }: SetupWizardProps) {
  const [currentStep, setCurrentStep] = useState(0)
  const [formData, setFormData] = useState<SetupFormData>(initialData ?? {})
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const handleChange = useCallback((key: string, value: string) => {
    setFormData((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleBatchChange = useCallback((updates: Record<string, string>) => {
    setFormData((prev) => ({ ...prev, ...updates }))
  }, [])

  const StepComponent = steps[currentStep].component

  async function handleApply() {
    setSaving(true)
    setSaveError(null)
    try {
      // Strip internal UI-only fields (prefixed with _) before saving
      const configData = Object.fromEntries(
        Object.entries(formData).filter(([k, v]) => !k.startsWith("_") && v !== "")
      )
      const res = await fetch("/api/setup/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(configData),
      })
      if (!res.ok) throw new Error("Failed to save configuration")

      // Optionally trigger setup
      const runRes = await fetch("/api/setup/run", { method: "POST" })
      if (!runRes.ok) throw new Error("Failed to start setup")

      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Unknown error")
    } finally {
      setSaving(false)
    }
  }

  const isLast = currentStep === steps.length - 1
  const isFirst = currentStep === 0

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
                onClick={() => setCurrentStep(i)}
                className="group flex-1"
                title={step.title}
              >
                <div
                  className={cn(
                    "h-1 rounded-full transition-all",
                    i < currentStep
                      ? "bg-blue-500"
                      : i === currentStep
                        ? "bg-blue-400"
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
                className="flex items-center gap-1.5 rounded-lg bg-blue-500/20 px-4 py-2 text-sm font-medium text-blue-400 transition-colors hover:bg-blue-500/30"
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
