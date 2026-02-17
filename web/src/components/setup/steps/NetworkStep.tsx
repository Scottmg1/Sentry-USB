import { Wifi, Radio } from "lucide-react"
import type { StepProps } from "../SetupWizard"

function Field({
  label,
  field,
  type = "text",
  placeholder,
  data,
  onChange,
  hint,
}: {
  label: string
  field: string
  type?: string
  placeholder?: string
  data: StepProps["data"]
  onChange: StepProps["onChange"]
  hint?: string
}) {
  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-slate-300">
        {label}
      </label>
      <input
        type={type}
        value={data[field] ?? ""}
        onChange={(e) => onChange(field, e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
      />
      {hint && <p className="mt-1 text-xs text-slate-600">{hint}</p>}
    </div>
  )
}

export function NetworkStep({ data, onChange }: StepProps) {
  const apEnabled = !!data.AP_SSID

  return (
    <div className="space-y-6">
      {/* WiFi */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <Wifi className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            Home WiFi
          </h3>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field
            label="SSID"
            field="SSID"
            placeholder="Your WiFi network name"
            data={data}
            onChange={onChange}
          />
          <Field
            label="Password"
            field="WIFIPASS"
            type="password"
            placeholder="WiFi password"
            data={data}
            onChange={onChange}
          />
        </div>
      </div>

      {/* Hostname */}
      <Field
        label="Hostname"
        field="TESLAUSB_HOSTNAME"
        placeholder="teslausb"
        data={data}
        onChange={onChange}
        hint="The device will be accessible at hostname.local"
      />

      {/* Access Point */}
      <div>
        <div className="mb-3 flex items-center gap-2">
          <Radio className="h-4 w-4 text-blue-400" />
          <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
            WiFi Access Point
          </h3>
          <span className="text-xs text-slate-600">(optional)</span>
        </div>
        <p className="mb-3 text-xs text-slate-500">
          Create a WiFi hotspot so you can access SentryUSB on the road.
        </p>

        <label className="mb-3 flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            checked={apEnabled}
            onChange={(e) => {
              if (!e.target.checked) {
                onChange("AP_SSID", "")
                onChange("AP_PASS", "")
                onChange("AP_IP", "")
              } else {
                onChange("AP_SSID", "SENTRYUSB WIFI")
              }
            }}
            className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500"
          />
          <span className="text-sm text-slate-300">
            Enable WiFi Access Point
          </span>
        </label>

        {apEnabled && (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field
              label="AP SSID"
              field="AP_SSID"
              placeholder="SENTRYUSB WIFI"
              data={data}
              onChange={onChange}
            />
            <Field
              label="AP Password"
              field="AP_PASS"
              type="password"
              placeholder="Min 8 characters"
              data={data}
              onChange={onChange}
              hint="Must be at least 8 characters"
            />
            <Field
              label="AP IP Address"
              field="AP_IP"
              placeholder="192.168.66.1"
              data={data}
              onChange={onChange}
              hint="Optional, default: 192.168.66.1"
            />
          </div>
        )}
      </div>
    </div>
  )
}
