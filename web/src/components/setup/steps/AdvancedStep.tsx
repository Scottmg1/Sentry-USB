import { Cog, Thermometer, MapPin } from "lucide-react"
import type { StepProps } from "../SetupWizard"

function Field({ label, field, type = "text", placeholder, data, onChange, hint }: {
  label: string; field: string; type?: string; placeholder?: string
  data: StepProps["data"]; onChange: StepProps["onChange"]; hint?: string
}) {
  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-slate-300">{label}</label>
      <input type={type} value={data[field] ?? ""} onChange={(e) => onChange(field, e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25" />
      {hint && <p className="mt-1 text-xs text-slate-600">{hint}</p>}
    </div>
  )
}

export function AdvancedStep({ data, onChange }: StepProps) {
  return (
    <div className="space-y-6">
      {/* Timezone */}
      <Field label="Time Zone" field="TIME_ZONE" placeholder="America/Los_Angeles or auto"
        data={data} onChange={onChange} hint='Use "auto" for automatic detection, or a tz database name like America/New_York' />

      {/* Archive tuning */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <Cog className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            Archive Tuning
          </h3>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Archive Delay (seconds)" field="ARCHIVE_DELAY" placeholder="20"
            data={data} onChange={onChange} hint="Delay between WiFi connect and archiving start" />
          <Field label="Snapshot Interval (seconds)" field="SNAPSHOT_INTERVAL" placeholder="default"
            data={data} onChange={onChange} hint="Set ~2 min shorter than car's RecentClips retention" />
        </div>
      </div>

      {/* Temperature monitoring */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <Thermometer className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            Temperature Monitoring
          </h3>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Warning Threshold (milli-°C)" field="TEMPERATURE_WARNING" placeholder="68000"
            data={data} onChange={onChange} hint="e.g. 68000 = 68.0°C" />
          <Field label="Caution Threshold (milli-°C)" field="TEMPERATURE_CAUTION" placeholder="55000"
            data={data} onChange={onChange} hint="e.g. 55000 = 55.0°C" />
          <Field label="Log Interval (minutes)" field="TEMPERATURE_INTERVAL" placeholder="60"
            data={data} onChange={onChange} hint="How often to log temperature readings" />
        </div>
        <label className="mt-3 flex cursor-pointer items-center gap-2">
          <input type="checkbox" checked={(data.TEMPERATURE_POSTARCHIVE ?? "true") === "true"}
            onChange={(e) => onChange("TEMPERATURE_POSTARCHIVE", e.target.checked ? "true" : "false")}
            className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500" />
          <span className="text-sm text-slate-300">Log temperature after each archive</span>
        </label>
      </div>

      {/* System tuning */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <Cog className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            System Tuning
          </h3>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Increase Root Size" field="INCREASE_ROOT_SIZE" placeholder="e.g. 500M or 2G"
            data={data} onChange={onChange} hint="Extra space for packages. Only works during initial setup." />
          <Field label="Additional Packages" field="INSTALL_USER_REQUESTED_PACKAGES" placeholder="iftop mosh sysstat"
            data={data} onChange={onChange} hint="Space-separated list of apt packages" />
          <Field label="CPU Governor" field="CPU_GOVERNOR" placeholder="conservative"
            data={data} onChange={onChange} hint="Leave empty for teslausb defaults" />
          <Field label="Dirty Background Bytes" field="DIRTY_BACKGROUND_BYTES" placeholder="65536"
            data={data} onChange={onChange} hint="VM write-back tuning. Leave empty for defaults." />
        </div>
      </div>

      {/* Drive Map */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <MapPin className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            Drive Map
          </h3>
        </div>
        <p className="mb-3 text-xs text-slate-500">
          Automatically extract GPS data from dashcam clips after archiving and build a map of all your drives.
        </p>
        <label className="flex cursor-pointer items-center gap-2">
          <input type="checkbox" checked={(data.DRIVE_MAP_ENABLED ?? "false") === "true"}
            onChange={(e) => onChange("DRIVE_MAP_ENABLED", e.target.checked ? "true" : "false")}
            className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500" />
          <span className="text-sm text-slate-300">Enable drive map processing after archive</span>
        </label>
      </div>

      {/* Source */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <Cog className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            Update Source
          </h3>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="GitHub Repo" field="REPO" placeholder="Scottmg1"
            data={data} onChange={onChange} hint="GitHub user/org for update scripts" />
          <Field label="Branch" field="BRANCH" placeholder="main-dev"
            data={data} onChange={onChange} />
        </div>
      </div>
    </div>
  )
}
