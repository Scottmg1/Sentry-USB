import { useState } from "react"
import { Cog, Thermometer, MapPin, Search } from "lucide-react"
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

const TIMEZONES = [
  "auto",
  "US/Eastern", "US/Central", "US/Mountain", "US/Pacific", "US/Alaska", "US/Hawaii",
  "America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles",
  "America/Anchorage", "America/Phoenix", "America/Toronto", "America/Vancouver",
  "America/Mexico_City", "America/Sao_Paulo", "America/Buenos_Aires", "America/Bogota",
  "Europe/London", "Europe/Paris", "Europe/Berlin", "Europe/Madrid", "Europe/Rome",
  "Europe/Amsterdam", "Europe/Brussels", "Europe/Zurich", "Europe/Vienna", "Europe/Stockholm",
  "Europe/Oslo", "Europe/Copenhagen", "Europe/Helsinki", "Europe/Dublin", "Europe/Lisbon",
  "Europe/Warsaw", "Europe/Prague", "Europe/Budapest", "Europe/Bucharest", "Europe/Athens",
  "Europe/Moscow", "Europe/Istanbul",
  "Asia/Tokyo", "Asia/Seoul", "Asia/Shanghai", "Asia/Hong_Kong", "Asia/Taipei",
  "Asia/Singapore", "Asia/Kolkata", "Asia/Mumbai", "Asia/Dubai", "Asia/Jerusalem",
  "Asia/Bangkok", "Asia/Jakarta", "Asia/Manila", "Asia/Kuala_Lumpur",
  "Australia/Sydney", "Australia/Melbourne", "Australia/Brisbane", "Australia/Perth",
  "Australia/Adelaide", "Australia/Hobart",
  "Pacific/Auckland", "Pacific/Fiji",
  "Africa/Johannesburg", "Africa/Cairo", "Africa/Lagos", "Africa/Nairobi",
]

function TempInput({
  label,
  field,
  data,
  onChange,
  placeholder,
  useFahrenheit,
}: {
  label: string
  field: string
  data: StepProps["data"]
  onChange: StepProps["onChange"]
  placeholder: string
  useFahrenheit: boolean
}) {
  // Convert from milli-°C stored value to display value
  const raw = data[field] ?? ""
  let displayVal = raw
  // If value looks like milli-°C (>=1000), convert for display
  if (raw && parseInt(raw) >= 1000) {
    const celsius = parseInt(raw) / 1000
    displayVal = useFahrenheit
      ? ((celsius * 9 / 5) + 32).toFixed(1)
      : celsius.toFixed(1)
  } else if (raw && useFahrenheit && parseFloat(raw) > 0) {
    // Already in °C display format, convert to °F
    displayVal = ((parseFloat(raw) * 9 / 5) + 32).toFixed(1)
  }

  const unit = useFahrenheit ? "°F" : "°C"

  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-slate-300">{label}</label>
      <div className="relative">
        <input
          type="text"
          inputMode="decimal"
          value={displayVal}
          onChange={(e) => {
            const v = e.target.value.replace(/[^0-9.]/g, "")
            // Store as °C (will be converted to milli-°C on save)
            if (useFahrenheit && v) {
              const f = parseFloat(v)
              if (!isNaN(f)) {
                onChange(field, ((f - 32) * 5 / 9).toFixed(1))
                return
              }
            }
            onChange(field, v)
          }}
          placeholder={placeholder}
          className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 pr-10 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
        />
        <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs font-medium text-slate-500">{unit}</span>
      </div>
    </div>
  )
}

export function AdvancedStep({ data, onChange }: StepProps) {
  const [tzSearch, setTzSearch] = useState("")
  const [useFahrenheit, setUseFahrenheit] = useState(false)

  const filteredTz = tzSearch
    ? TIMEZONES.filter(tz => tz.toLowerCase().includes(tzSearch.toLowerCase()))
    : TIMEZONES

  return (
    <div className="space-y-6">
      {/* Timezone */}
      <div>
        <label className="mb-1 block text-sm font-medium text-slate-300">Time Zone</label>
        <div className="relative mb-2">
          <Search className="absolute left-3 top-2.5 h-3.5 w-3.5 text-slate-600" />
          <input
            type="text"
            value={tzSearch}
            onChange={(e) => setTzSearch(e.target.value)}
            placeholder="Search timezones..."
            className="w-full rounded-lg border border-white/10 bg-white/5 py-2 pl-9 pr-3 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
          />
        </div>
        <select
          value={data.TIME_ZONE ?? "auto"}
          onChange={(e) => onChange("TIME_ZONE", e.target.value)}
          size={6}
          className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
        >
          {filteredTz.map(tz => (
            <option key={tz} value={tz}>{tz === "auto" ? "auto (detect automatically)" : tz}</option>
          ))}
        </select>
        <p className="mt-1 text-xs text-slate-600">
          Selected: <span className="font-medium text-blue-400">{data.TIME_ZONE || "auto"}</span>
        </p>
      </div>

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
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Thermometer className="h-4 w-4 text-blue-400" />
            <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
              Temperature Monitoring
            </h3>
          </div>
          <div className="flex overflow-hidden rounded-lg border border-white/10">
            <button type="button" onClick={() => setUseFahrenheit(false)}
              className={`px-2.5 py-1 text-xs font-medium transition-colors ${!useFahrenheit ? "bg-blue-500 text-white" : "text-slate-500 hover:text-slate-300"}`}>
              °C
            </button>
            <button type="button" onClick={() => setUseFahrenheit(true)}
              className={`px-2.5 py-1 text-xs font-medium transition-colors ${useFahrenheit ? "bg-blue-500 text-white" : "text-slate-500 hover:text-slate-300"}`}>
              °F
            </button>
          </div>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <TempInput label="Warning Threshold" field="TEMPERATURE_WARNING"
            placeholder={useFahrenheit ? "154.4" : "68.0"} data={data} onChange={onChange} useFahrenheit={useFahrenheit} />
          <TempInput label="Caution Threshold" field="TEMPERATURE_CAUTION"
            placeholder={useFahrenheit ? "131.0" : "55.0"} data={data} onChange={onChange} useFahrenheit={useFahrenheit} />
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
            data={data} onChange={onChange} hint="Leave empty for SentryUSB defaults" />
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
          <input type="checkbox" checked={(data.DRIVE_MAP_ENABLED ?? "true") === "true"}
            onChange={(e) => onChange("DRIVE_MAP_ENABLED", e.target.checked ? "true" : "false")}
            className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500" />
          <span className="text-sm text-slate-300">Enable drive map processing after archive</span>
        </label>
        <div className="mt-3">
          <label className="mb-1 block text-sm font-medium text-slate-300">Distance Unit</label>
          <div className="flex overflow-hidden rounded-lg border border-white/10 w-fit">
            <button type="button" onClick={() => onChange("DRIVE_MAP_UNIT", "mi")}
              className={`px-3 py-1.5 text-xs font-medium transition-colors ${(data.DRIVE_MAP_UNIT ?? "mi") === "mi" ? "bg-blue-500 text-white" : "text-slate-500 hover:text-slate-300"}`}>
              Miles
            </button>
            <button type="button" onClick={() => onChange("DRIVE_MAP_UNIT", "km")}
              className={`px-3 py-1.5 text-xs font-medium transition-colors ${data.DRIVE_MAP_UNIT === "km" ? "bg-blue-500 text-white" : "text-slate-500 hover:text-slate-300"}`}>
              Kilometers
            </button>
          </div>
        </div>
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
