import { useState, useEffect } from "react"
import { Wifi, Loader2, CheckCircle, AlertCircle } from "lucide-react"
import { cn } from "@/lib/utils"

type MQTTFormState = "idle" | "testing" | "saving" | "success" | "error"

interface MQTTFormData {
  enabled: boolean
  host: string
  port: string
  username: string
  password: string
  base_topic: string
}

export function MQTTSettings() {
  const [formState, setFormState] = useState<MQTTFormState>("idle")
  const [error, setError] = useState("")
  const [successMsg, setSuccessMsg] = useState("")
  const [form, setForm] = useState<MQTTFormData>({
    enabled: false,
    host: "",
    port: "1883",
    username: "",
    password: "",
    base_topic: "sentryusb",
  })
  const [connectionStatus, setConnectionStatus] = useState<{
    connected: boolean
    host: string
    port: number
  } | null>(null)

  useEffect(() => {
    loadConfig()
    checkStatus()
  }, [])

  async function loadConfig() {
    try {
      const response = await fetch("/api/mqtt/config")
      const config = await response.json()
      setForm({
        enabled: config.enabled ?? false,
        host: config.host ?? "",
        port: (config.port ?? 1883).toString(),
        username: config.username ?? "",
        password: config.password ?? "",
        base_topic: config.base_topic ?? "sentryusb",
      })
    } catch (err) {
      console.error("Failed to load MQTT config:", err)
    }
  }

  async function checkStatus() {
    try {
      const response = await fetch("/api/mqtt/status")
      const status = await response.json()
      setConnectionStatus(status)
    } catch (err) {
      console.error("Failed to check MQTT status:", err)
    }
  }

  async function handleTest() {
    setFormState("testing")
    setError("")
    setSuccessMsg("")

    try {
      const response = await fetch("/api/mqtt/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          enabled: true,
          host: form.host,
          port: parseInt(form.port),
          username: form.username,
          password: form.password,
          base_topic: form.base_topic,
        }),
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.message || "Connection test failed")
      }

      setFormState("success")
      setSuccessMsg("Successfully connected to MQTT broker!")
      setTimeout(() => setFormState("idle"), 3000)
    } catch (err) {
      setFormState("error")
      setError(err instanceof Error ? err.message : "Connection test failed")
      setTimeout(() => setFormState("idle"), 5000)
    }
  }

  async function handleSave() {
    setFormState("saving")
    setError("")
    setSuccessMsg("")

    // Validate required fields
    if (form.enabled) {
      if (!form.host.trim()) {
        setFormState("error")
        setError("MQTT Host is required")
        setTimeout(() => setFormState("idle"), 3000)
        return
      }

      const port = parseInt(form.port)
      if (isNaN(port) || port < 1 || port > 65535) {
        setFormState("error")
        setError("MQTT Port must be between 1 and 65535")
        setTimeout(() => setFormState("idle"), 3000)
        return
      }

      if (!form.base_topic.trim()) {
        setFormState("error")
        setError("MQTT Base Topic is required")
        setTimeout(() => setFormState("idle"), 3000)
        return
      }
    }

    try {
      const response = await fetch("/api/mqtt/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          enabled: form.enabled,
          host: form.host,
          port: parseInt(form.port),
          username: form.username,
          password: form.password,
          base_topic: form.base_topic,
        }),
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.message || "Failed to save configuration")
      }

      setFormState("success")
      setSuccessMsg("MQTT configuration saved!")
      setTimeout(() => {
        setFormState("idle")
        checkStatus()
      }, 2000)
    } catch (err) {
      setFormState("error")
      setError(err instanceof Error ? err.message : "Failed to save configuration")
      setTimeout(() => setFormState("idle"), 5000)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Wifi className="h-5 w-5 text-blue-400" />
          <h3 className="text-lg font-semibold text-slate-200">MQTT / Home Assistant</h3>
        </div>
        {connectionStatus && (
          <div className="flex items-center gap-2">
            <span
              className={cn(
                "inline-block h-2.5 w-2.5 rounded-full",
                connectionStatus.connected ? "bg-emerald-400" : "bg-slate-500"
              )}
            />
            <span className="text-xs text-slate-400">
              {connectionStatus.connected
                ? `Connected to ${connectionStatus.host}:${connectionStatus.port}`
                : "Not connected"}
            </span>
          </div>
        )}
      </div>

      <div className="rounded-lg border border-white/10 bg-white/[0.02] p-4 space-y-4">
        {/* Enable toggle */}
        <label className="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
            className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500"
          />
          <span className="text-sm font-medium text-slate-300">Enable MQTT Integration</span>
        </label>

        {form.enabled && (
          <div className="space-y-3 rounded-lg border border-blue-500/20 bg-blue-500/5 p-3 mt-4">
            {/* Host */}
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-300">
                MQTT Broker Host <span className="text-red-400">*</span>
              </label>
              <input
                type="text"
                value={form.host}
                onChange={(e) => setForm({ ...form, host: e.target.value })}
                placeholder="192.168.1.100"
                className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
              />
              <p className="mt-0.5 text-xs text-slate-600">IP address or hostname of your MQTT broker</p>
            </div>

            {/* Port */}
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-300">
                Port <span className="text-red-400">*</span>
              </label>
              <input
                type="number"
                value={form.port}
                onChange={(e) => setForm({ ...form, port: e.target.value })}
                placeholder="1883"
                min="1"
                max="65535"
                className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
              />
              <p className="mt-0.5 text-xs text-slate-600">Default: 1883 (unencrypted), 8883 (encrypted)</p>
            </div>

            {/* Username */}
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-300">Username (Optional)</label>
              <input
                type="text"
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                placeholder="sentryusb"
                className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
              />
              <p className="mt-0.5 text-xs text-slate-600">Leave blank if your broker doesn't require authentication</p>
            </div>

            {/* Password */}
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-300">Password (Optional)</label>
              <input
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                placeholder="••••••••"
                className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
              />
              <p className="mt-0.5 text-xs text-slate-600">Leave blank if your broker doesn't require authentication</p>
            </div>

            {/* Base Topic */}
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-300">
                Base Topic <span className="text-red-400">*</span>
              </label>
              <input
                type="text"
                value={form.base_topic}
                onChange={(e) => setForm({ ...form, base_topic: e.target.value })}
                placeholder="sentryusb"
                className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
              />
              <p className="mt-0.5 text-xs text-slate-600">
                Topics will be published under this prefix (e.g., sentryusb/uptime)
              </p>
            </div>

            {/* Info box */}
            <div className="rounded-lg border border-green-500/20 bg-green-500/5 p-3 mt-4">
              <p className="text-xs text-slate-400">
                <strong className="text-green-400">📡 Home Assistant:</strong> Once enabled, your device will be
                automatically discovered by Home Assistant. Sensors include temperature, storage, network info, and
                archive progress. You can also trigger archiving from Home Assistant automations.
              </p>
            </div>
          </div>
        )}

        {/* Status messages */}
        {error && (
          <div className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/5 p-3">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-red-400" />
            <p className="text-xs text-red-300">{error}</p>
          </div>
        )}

        {successMsg && (
          <div className="flex items-start gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3">
            <CheckCircle className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />
            <p className="text-xs text-emerald-300">{successMsg}</p>
          </div>
        )}

        {/* Action buttons */}
        {form.enabled && (
          <div className="flex gap-2 pt-2">
            <button
              onClick={handleTest}
              disabled={formState === "testing" || formState === "saving"}
              className={cn(
                "flex items-center gap-2 rounded-lg px-3 py-2 text-xs font-medium transition-colors",
                formState === "testing"
                  ? "bg-blue-500/15 text-blue-400"
                  : "bg-white/5 text-slate-300 hover:bg-white/10"
              )}
            >
              {formState === "testing" && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {formState === "testing" ? "Testing..." : "Test Connection"}
            </button>

            <button
              onClick={handleSave}
              disabled={formState === "saving" || formState === "testing"}
              className={cn(
                "flex items-center gap-2 rounded-lg px-3 py-2 text-xs font-medium transition-colors",
                formState === "saving"
                  ? "bg-blue-500/15 text-blue-400"
                  : "bg-blue-500/15 text-blue-300 hover:bg-blue-500/25"
              )}
            >
              {formState === "saving" && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {formState === "success" && <CheckCircle className="h-3.5 w-3.5" />}
              {formState === "saving" ? "Saving..." : formState === "success" ? "Saved!" : "Save Changes"}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
