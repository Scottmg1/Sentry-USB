import { useEffect, useState } from "react"
import { Wifi, Radio, CheckCircle, AlertCircle, RefreshCw, Pencil } from "lucide-react"
import type { StepProps } from "../SetupWizard"
import { SecretInput } from "../SecretInput"
import { cn } from "@/lib/utils"

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
  const inputCls = "w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-slate-300">
        {label}
      </label>
      {type === "password" ? (
        <SecretInput
          value={data[field] ?? ""}
          onChange={(v) => onChange(field, v)}
          placeholder={placeholder}
          className={cn(inputCls, "pr-8")}
        />
      ) : (
        <input
          type={type}
          value={data[field] ?? ""}
          onChange={(e) => onChange(field, e.target.value)}
          placeholder={placeholder}
          className={inputCls}
        />
      )}
      {hint && <p className="mt-1 text-xs text-slate-600">{hint}</p>}
    </div>
  )
}

interface DetectedWifi {
  current: { ssid: string; connected: boolean; source: string }
  config_ssid: string
}

export function NetworkStep({ data, onChange, onBatchChange }: StepProps) {
  const apEnabled = !!data.AP_SSID
  const [detected, setDetected] = useState<DetectedWifi | null>(null)
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetch("/api/wifi")
      .then((r) => r.json())
      .then((d: DetectedWifi) => {
        if (cancelled) return
        setDetected(d)

        // Pre-fill SSID from detected config if the wizard field is empty
        if (!data.SSID) {
          const ssid = d.config_ssid || d.current.ssid
          if (ssid) {
            onChange("SSID", ssid)
          }
        }
      })
      .catch(() => {})
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])

  const detectedSSID = detected?.current.ssid || ""
  const configSSID = detected?.config_ssid || ""
  const isConnected = detected?.current.connected ?? false

  // Show the detected banner if we have a detected SSID and user isn't manually editing
  const showDetectedBanner = !loading && detectedSSID && !editing

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

        {/* Loading state */}
        {loading && (
          <div className="flex items-center gap-2 rounded-lg border border-white/5 bg-white/[0.02] p-3">
            <RefreshCw className="h-4 w-4 animate-spin text-slate-500" />
            <p className="text-sm text-slate-500">Detecting WiFi configuration...</p>
          </div>
        )}

        {/* Detected WiFi banner */}
        {showDetectedBanner && (
          <div className={cn(
            "mb-4 flex items-center justify-between rounded-lg border p-4",
            isConnected
              ? "border-emerald-500/30 bg-emerald-500/10"
              : "border-amber-500/30 bg-amber-500/10"
          )}>
            <div className="flex items-center gap-3">
              {isConnected ? (
                <CheckCircle className="h-5 w-5 text-emerald-400" />
              ) : (
                <AlertCircle className="h-5 w-5 text-amber-400" />
              )}
              <div>
                <p className="text-sm font-medium text-slate-200">
                  {isConnected ? "Connected to WiFi" : "WiFi Configured"}
                </p>
                <p className="text-xs text-slate-400">
                  Network: <span className="font-medium text-slate-300">{detectedSSID}</span>
                  {configSSID && configSSID !== detectedSSID && (
                    <span className="ml-2 text-slate-500">
                      (config: {configSSID})
                    </span>
                  )}
                </p>
              </div>
            </div>
            <button
              onClick={() => setEditing(true)}
              className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 transition-colors hover:bg-white/10"
            >
              <Pencil className="h-3 w-3" />
              Change
            </button>
          </div>
        )}

        {/* WiFi fields — shown if no detected wifi, or user clicks Change, or loading failed */}
        {(!showDetectedBanner || editing) && !loading && (
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
        )}
      </div>

      {/* Hostname & Country */}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label="Hostname"
          field="SENTRYUSB_HOSTNAME"
          placeholder="sentryusb"
          data={data}
          onChange={onChange}
          hint="The device will be accessible at hostname.local"
        />
        <div>
          <label className="mb-1 block text-sm font-medium text-slate-300">
            WiFi Country
          </label>
          <select
            value={data.WPA_COUNTRY ?? "US"}
            onChange={(e) => onChange("WPA_COUNTRY", e.target.value)}
            className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
          >
            <option value="US">US — United States</option>
            <option value="GB">GB — United Kingdom</option>
            <option value="CA">CA — Canada</option>
            <option value="AU">AU — Australia</option>
            <option value="DE">DE — Germany</option>
            <option value="FR">FR — France</option>
            <option value="NL">NL — Netherlands</option>
            <option value="IT">IT — Italy</option>
            <option value="ES">ES — Spain</option>
            <option value="SE">SE — Sweden</option>
            <option value="NO">NO — Norway</option>
            <option value="DK">DK — Denmark</option>
            <option value="FI">FI — Finland</option>
            <option value="CH">CH — Switzerland</option>
            <option value="AT">AT — Austria</option>
            <option value="BE">BE — Belgium</option>
            <option value="IE">IE — Ireland</option>
            <option value="NZ">NZ — New Zealand</option>
            <option value="JP">JP — Japan</option>
            <option value="KR">KR — South Korea</option>
            <option value="SG">SG — Singapore</option>
            <option value="HK">HK — Hong Kong</option>
            <option value="TW">TW — Taiwan</option>
            <option value="IN">IN — India</option>
            <option value="BR">BR — Brazil</option>
            <option value="MX">MX — Mexico</option>
            <option value="IL">IL — Israel</option>
            <option value="AE">AE — UAE</option>
            <option value="ZA">ZA — South Africa</option>
            <option value="CN">CN — China</option>
          </select>
          <p className="mt-1 text-xs text-slate-600">Required for WiFi to work on correct channels</p>
        </div>
      </div>

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
                onBatchChange({ AP_SSID: "", AP_PASS: "", AP_IP: "" })
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
