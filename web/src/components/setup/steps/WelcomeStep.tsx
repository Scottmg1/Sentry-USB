import { Shield } from "lucide-react"
import type { StepProps } from "../SetupWizard"

export function WelcomeStep(_props: StepProps) {
  return (
    <div className="flex flex-col items-center py-8 text-center">
      <div className="mb-6 flex h-20 w-20 items-center justify-center rounded-2xl bg-blue-500/15">
        <Shield className="h-10 w-10 text-blue-400" />
      </div>
      <h2 className="text-2xl font-bold text-slate-100">
        Welcome to SentryUSB Setup
      </h2>
      <p className="mt-3 max-w-md text-sm leading-relaxed text-slate-400">
        This wizard will guide you through configuring your SentryUSB device.
        You&apos;ll set up WiFi, storage, archive destinations, notifications,
        and more — all from this interface.
      </p>
      <div className="mt-8 grid w-full max-w-md gap-3 text-left">
        <InfoCard
          title="No SSH Required"
          desc="Everything is configured right here in your browser."
        />
        <InfoCard
          title="Safe to Re-run"
          desc="You can re-run this wizard anytime to change settings."
        />
        <InfoCard
          title="Preserves Comments"
          desc="Your existing config file comments are preserved."
        />
      </div>
      <p className="mt-8 text-xs text-slate-600">
        Click <span className="text-slate-400">Next</span> to begin.
      </p>
    </div>
  )
}

function InfoCard({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="rounded-lg border border-white/5 bg-white/[0.02] px-4 py-3">
      <p className="text-sm font-medium text-slate-200">{title}</p>
      <p className="mt-0.5 text-xs text-slate-500">{desc}</p>
    </div>
  )
}
