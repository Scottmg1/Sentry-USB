import { useState, useEffect, useRef, useCallback } from "react"
import {
  Music,
  Upload,
  Play,
  Pause,
  CheckCircle2,
  Trash2,
  Volume2,
  X,
  AlertCircle,
  Bell,
  Shuffle,
  Clock,
  Zap,
} from "lucide-react"

const API_BASE = "/api"
const MAX_DURATION_SECONDS = 7
const MAX_FILE_BYTES = 5 * 1024 * 1024 // 5 MB

interface SoundEntry {
  name: string
  size: number
  active: boolean
}

interface ListResponse {
  sounds: SoundEntry[]
  active_name: string
  active_set: boolean
}

interface RandomConfig {
  enabled: boolean
  mode: string
  interval: string
  has_rtc: boolean
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

async function getWavDuration(file: File): Promise<number> {
  const buffer = await file.arrayBuffer()
  const ctx = new AudioContext()
  try {
    const decoded = await ctx.decodeAudioData(buffer)
    return decoded.duration
  } finally {
    ctx.close()
  }
}

export default function LockChime() {
  const [sounds, setSounds] = useState<SoundEntry[]>([])
  const [activeName, setActiveName] = useState<string>("")
  const [activeSet, setActiveSet] = useState(false)
  const [loading, setLoading] = useState(true)
  const [toast, setToast] = useState<{ msg: string; type: "success" | "error" } | null>(null)
  const [playingName, setPlayingName] = useState<string | null>(null)
  const [uploadDragging, setUploadDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [activating, setActivating] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [clearing, setClearing] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  // Random mode state
  const [randomCfg, setRandomCfg] = useState<RandomConfig>({
    enabled: false,
    mode: "on_connect",
    interval: "daily",
    has_rtc: false,
  })
  const [randomLoading, setRandomLoading] = useState(true)
  const [savingRandom, setSavingRandom] = useState(false)
  const [randomizing, setRandomizing] = useState(false)

  const showToast = useCallback((msg: string, type: "success" | "error") => {
    setToast({ msg, type })
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 4000)
    return () => clearTimeout(t)
  }, [toast])

  const fetchSounds = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/lockchime/list`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data: ListResponse = await res.json()
      setSounds(data.sounds ?? [])
      setActiveName(data.active_name ?? "")
      setActiveSet(data.active_set ?? false)
    } catch {
      showToast("Failed to load sounds", "error")
    } finally {
      setLoading(false)
    }
  }, [showToast])

  const fetchRandomConfig = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/lockchime/random-config`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data: RandomConfig = await res.json()
      setRandomCfg(data)
    } catch {
      // Silently fail — random config is optional
    } finally {
      setRandomLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchSounds()
    fetchRandomConfig()
  }, [fetchSounds, fetchRandomConfig])

  // Stop audio when component unmounts
  useEffect(() => {
    return () => {
      audioRef.current?.pause()
    }
  }, [])

  function togglePlay(name: string) {
    if (playingName === name) {
      audioRef.current?.pause()
      setPlayingName(null)
      return
    }
    audioRef.current?.pause()
    const url = `${API_BASE}/files/download?path=/mutable/LockChime/${encodeURIComponent(name)}`
    const audio = new Audio(url)
    audioRef.current = audio
    audio.onended = () => setPlayingName(null)
    audio.onerror = () => {
      setPlayingName(null)
      showToast("Could not play sound", "error")
    }
    audio.play().catch(() => {
      setPlayingName(null)
      showToast("Could not play sound", "error")
    })
    setPlayingName(name)
  }

  async function handleFileSelected(file: File) {
    if (!file.name.toLowerCase().endsWith(".wav")) {
      showToast("Only .wav files are supported", "error")
      return
    }
    if (file.size > MAX_FILE_BYTES) {
      showToast("File is too large (max 5 MB)", "error")
      return
    }

    try {
      const duration = await getWavDuration(file)
      if (duration > MAX_DURATION_SECONDS) {
        showToast(
          `Sound is ${duration.toFixed(1)}s — Tesla requires ${MAX_DURATION_SECONDS}s or less`,
          "error"
        )
        return
      }
    } catch {
      showToast("Could not read WAV file — is it a valid .wav?", "error")
      return
    }

    setUploading(true)
    try {
      const form = new FormData()
      form.append("file", file)
      const res = await fetch(`${API_BASE}/lockchime/upload`, { method: "POST", body: form })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      showToast(`Uploaded "${data.name}"`, "success")
      await fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Upload failed", "error")
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
    }
  }

  async function handleActivate(name: string) {
    setActivating(name)
    try {
      const res = await fetch(`${API_BASE}/lockchime/activate/${encodeURIComponent(name)}`, {
        method: "POST",
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      showToast(`"${name}" is now your active lock sound`, "success")
      await fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Failed to activate", "error")
    } finally {
      setActivating(null)
    }
  }

  async function handleClear() {
    setClearing(true)
    try {
      const res = await fetch(`${API_BASE}/lockchime/clear`, { method: "DELETE" })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `HTTP ${res.status}`)
      }
      showToast("Active lock sound cleared", "success")
      await fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Failed to clear", "error")
    } finally {
      setClearing(false)
    }
  }

  async function handleDelete(name: string) {
    setDeleting(name)
    setDeleteConfirm(null)
    try {
      const res = await fetch(`${API_BASE}/lockchime/${encodeURIComponent(name)}`, {
        method: "DELETE",
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `HTTP ${res.status}`)
      }
      showToast(`Deleted "${name}"`, "success")
      if (playingName === name) {
        audioRef.current?.pause()
        setPlayingName(null)
      }
      await fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Failed to delete", "error")
    } finally {
      setDeleting(null)
    }
  }

  async function handleSaveRandomConfig(newCfg: RandomConfig) {
    setSavingRandom(true)
    try {
      const res = await fetch(`${API_BASE}/lockchime/random-config`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          enabled: newCfg.enabled,
          mode: newCfg.mode,
          interval: newCfg.interval,
        }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      setRandomCfg((prev) => ({ ...prev, ...newCfg, enabled: data.enabled }))
      showToast(
        newCfg.enabled ? "Random mode enabled" : "Random mode disabled",
        "success"
      )
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Failed to save", "error")
    } finally {
      setSavingRandom(false)
    }
  }

  async function handleRandomizeNow() {
    setRandomizing(true)
    try {
      const res = await fetch(`${API_BASE}/lockchime/randomize`, { method: "POST" })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      showToast(`Randomly selected "${data.active}"`, "success")
      await fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Failed to randomize", "error")
    } finally {
      setRandomizing(false)
    }
  }

  function onDragOver(e: React.DragEvent) {
    e.preventDefault()
    setUploadDragging(true)
  }
  function onDragLeave() {
    setUploadDragging(false)
  }
  function onDrop(e: React.DragEvent) {
    e.preventDefault()
    setUploadDragging(false)
    const file = e.dataTransfer.files[0]
    if (file) handleFileSelected(file)
  }

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-violet-500/20">
          <Bell className="h-5 w-5 text-violet-400" />
        </div>
        <div>
          <h1 className="text-xl font-semibold text-slate-100">Lock Chime</h1>
          <p className="text-sm text-slate-400">
            Custom .wav lock sounds for your Tesla — max {MAX_DURATION_SECONDS}s
          </p>
        </div>
      </div>

      {/* Active Sound Banner */}
      <div
        className={`rounded-xl border p-4 flex items-center justify-between gap-4 ${
          activeSet
            ? "border-violet-500/30 bg-violet-500/10"
            : "border-white/10 bg-white/[0.03]"
        }`}
      >
        <div className="flex items-center gap-3 min-w-0">
          <Volume2
            className={`h-5 w-5 shrink-0 ${activeSet ? "text-violet-400" : "text-slate-600"}`}
          />
          <div className="min-w-0">
            <p className="text-sm font-medium text-slate-200">
              {activeSet ? "Active lock sound" : "No lock sound set"}
            </p>
            {activeSet && activeName && (
              <p className="text-xs text-slate-400 truncate">{activeName}</p>
            )}
            {!activeSet && (
              <p className="text-xs text-slate-500">
                Tesla will use its default chime
              </p>
            )}
          </div>
        </div>
        {activeSet && (
          <button
            onClick={handleClear}
            disabled={clearing}
            className="shrink-0 flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-1.5 text-xs text-slate-400 transition-colors hover:border-red-500/40 hover:text-red-400 disabled:opacity-50"
          >
            <X className="h-3.5 w-3.5" />
            {clearing ? "Clearing..." : "Clear"}
          </button>
        )}
      </div>

      {/* Random Mode Section */}
      {!randomLoading && (
        <div className="rounded-xl border border-white/10 bg-white/[0.02] p-4 space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <Shuffle className="h-4 w-4 text-amber-400" />
              <h2 className="text-sm font-medium text-slate-200">Random Mode</h2>
            </div>
            <button
              onClick={() =>
                handleSaveRandomConfig({
                  ...randomCfg,
                  enabled: !randomCfg.enabled,
                })
              }
              disabled={savingRandom || sounds.length < 2}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                randomCfg.enabled ? "bg-amber-500" : "bg-white/10"
              } ${sounds.length < 2 ? "opacity-40 cursor-not-allowed" : "cursor-pointer"}`}
              title={sounds.length < 2 ? "Upload at least 2 sounds to use random mode" : ""}
            >
              <span
                className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${
                  randomCfg.enabled ? "translate-x-6" : "translate-x-1"
                }`}
              />
            </button>
          </div>

          {sounds.length < 2 && (
            <p className="text-xs text-slate-500">
              Upload at least 2 sounds to use random mode.
            </p>
          )}

          {randomCfg.enabled && sounds.length >= 2 && (
            <div className="space-y-3">
              {/* Mode selection */}
              <div className="flex gap-2">
                <button
                  onClick={() =>
                    handleSaveRandomConfig({ ...randomCfg, mode: "on_connect" })
                  }
                  disabled={savingRandom}
                  className={`flex-1 flex items-center gap-2 rounded-lg border px-3 py-2.5 text-xs transition-colors ${
                    randomCfg.mode === "on_connect"
                      ? "border-amber-500/40 bg-amber-500/10 text-amber-300"
                      : "border-white/10 text-slate-400 hover:border-white/20"
                  }`}
                >
                  <Zap className="h-3.5 w-3.5 shrink-0" />
                  <div className="text-left">
                    <p className="font-medium">On Connect</p>
                    <p className="mt-0.5 text-[10px] opacity-60">
                      Random sound each time Tesla connects
                    </p>
                  </div>
                </button>

                <button
                  onClick={() => {
                    if (!randomCfg.has_rtc) {
                      showToast(
                        "Scheduled mode requires a Pi with RTC (real-time clock)",
                        "error"
                      )
                      return
                    }
                    handleSaveRandomConfig({ ...randomCfg, mode: "scheduled" })
                  }}
                  disabled={savingRandom}
                  className={`flex-1 flex items-center gap-2 rounded-lg border px-3 py-2.5 text-xs transition-colors ${
                    randomCfg.mode === "scheduled"
                      ? "border-amber-500/40 bg-amber-500/10 text-amber-300"
                      : !randomCfg.has_rtc
                        ? "border-white/5 text-slate-600 cursor-not-allowed"
                        : "border-white/10 text-slate-400 hover:border-white/20"
                  }`}
                >
                  <Clock className="h-3.5 w-3.5 shrink-0" />
                  <div className="text-left">
                    <p className="font-medium">Scheduled</p>
                    <p className="mt-0.5 text-[10px] opacity-60">
                      {randomCfg.has_rtc
                        ? "Change on a time schedule"
                        : "Requires RTC hardware"}
                    </p>
                  </div>
                </button>
              </div>

              {/* Schedule interval (only for scheduled mode + RTC) */}
              {randomCfg.mode === "scheduled" && randomCfg.has_rtc && (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-slate-400">Change every:</span>
                  {(["hourly", "daily", "weekly"] as const).map((int) => (
                    <button
                      key={int}
                      onClick={() =>
                        handleSaveRandomConfig({ ...randomCfg, interval: int })
                      }
                      disabled={savingRandom}
                      className={`rounded-md px-2.5 py-1 text-xs transition-colors ${
                        randomCfg.interval === int
                          ? "bg-amber-500/20 text-amber-300 font-medium"
                          : "text-slate-500 hover:text-slate-300"
                      }`}
                    >
                      {int.charAt(0).toUpperCase() + int.slice(1)}
                    </button>
                  ))}
                </div>
              )}

              {/* Randomize Now button */}
              <button
                onClick={handleRandomizeNow}
                disabled={randomizing}
                className="flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-50"
              >
                <Shuffle className="h-3.5 w-3.5" />
                {randomizing ? "Randomizing..." : "Randomize Now"}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Upload Area */}
      <div
        className={`relative rounded-xl border-2 border-dashed transition-colors cursor-pointer ${
          uploadDragging
            ? "border-violet-500/60 bg-violet-500/10"
            : "border-white/10 hover:border-white/20 bg-white/[0.02]"
        }`}
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onDrop={onDrop}
        onClick={() => !uploading && fileInputRef.current?.click()}
      >
        <div className="flex flex-col items-center gap-3 py-8 px-4 text-center">
          {uploading ? (
            <>
              <div className="h-5 w-5 animate-spin rounded-full border-2 border-violet-400 border-t-transparent" />
              <p className="text-sm text-slate-400">Uploading...</p>
            </>
          ) : (
            <>
              <Upload className="h-8 w-8 text-slate-600" />
              <div>
                <p className="text-sm font-medium text-slate-300">
                  Drop a .wav file or click to browse
                </p>
                <p className="mt-1 text-xs text-slate-500">
                  WAV only · max {MAX_DURATION_SECONDS}s · max 5 MB
                </p>
              </div>
            </>
          )}
        </div>
        <input
          ref={fileInputRef}
          type="file"
          accept=".wav,audio/wav,audio/x-wav"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) handleFileSelected(file)
          }}
        />
      </div>

      {/* Sound Library */}
      <div>
        <h2 className="mb-3 text-sm font-medium text-slate-400 uppercase tracking-wider">
          My Library
        </h2>

        {loading && (
          <div className="flex items-center justify-center py-12">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-violet-400 border-t-transparent" />
          </div>
        )}

        {!loading && sounds.length === 0 && (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-white/10 bg-white/[0.02] py-12 text-center">
            <Music className="h-10 w-10 text-slate-700" />
            <div>
              <p className="text-sm font-medium text-slate-400">No sounds yet</p>
              <p className="mt-1 text-xs text-slate-600">Upload a .wav file to get started</p>
            </div>
          </div>
        )}

        {!loading && sounds.length > 0 && (
          <div className="space-y-2">
            {sounds.map((sound) => {
              const isPlaying = playingName === sound.name
              const isActive = activeSet && activeName === sound.name
              const isActivating = activating === sound.name
              const isDeleting = deleting === sound.name

              return (
                <div
                  key={sound.name}
                  className={`flex items-center gap-3 rounded-xl border px-4 py-3 transition-colors ${
                    isActive
                      ? "border-violet-500/40 bg-violet-500/10"
                      : "border-white/10 bg-white/[0.02] hover:bg-white/[0.04]"
                  }`}
                >
                  {/* Play button */}
                  <button
                    onClick={() => togglePlay(sound.name)}
                    className="shrink-0 flex h-8 w-8 items-center justify-center rounded-full bg-white/5 text-slate-300 transition-colors hover:bg-white/10 hover:text-white"
                    title={isPlaying ? "Pause" : "Play"}
                  >
                    {isPlaying ? (
                      <Pause className="h-4 w-4" />
                    ) : (
                      <Play className="h-4 w-4 translate-x-0.5" />
                    )}
                  </button>

                  {/* Name & size */}
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-slate-200">{sound.name}</p>
                    <p className="text-xs text-slate-500">{formatSize(sound.size)}</p>
                  </div>

                  {/* Active badge */}
                  {isActive && (
                    <span className="shrink-0 flex items-center gap-1 rounded-full bg-violet-500/20 px-2 py-0.5 text-xs font-medium text-violet-300">
                      <CheckCircle2 className="h-3 w-3" />
                      Active
                    </span>
                  )}

                  {/* Set as active */}
                  {!isActive && (
                    <button
                      onClick={() => handleActivate(sound.name)}
                      disabled={isActivating || isDeleting}
                      className="shrink-0 rounded-lg border border-white/10 px-3 py-1.5 text-xs text-slate-400 transition-colors hover:border-violet-500/40 hover:text-violet-300 disabled:opacity-50"
                    >
                      {isActivating ? "Setting..." : "Set Active"}
                    </button>
                  )}

                  {/* Delete */}
                  {deleteConfirm === sound.name ? (
                    <div className="shrink-0 flex items-center gap-1">
                      <button
                        onClick={() => handleDelete(sound.name)}
                        disabled={isDeleting}
                        className="rounded-lg bg-red-500/20 px-2.5 py-1.5 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/30 disabled:opacity-50"
                      >
                        {isDeleting ? "Deleting..." : "Confirm"}
                      </button>
                      <button
                        onClick={() => setDeleteConfirm(null)}
                        className="rounded-lg px-2 py-1.5 text-xs text-slate-500 hover:text-slate-300"
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setDeleteConfirm(sound.name)}
                      disabled={isDeleting}
                      className="shrink-0 rounded-lg p-1.5 text-slate-600 transition-colors hover:text-red-400 disabled:opacity-50"
                      title="Delete"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Info note */}
      <div className="flex items-start gap-3 rounded-xl border border-white/10 bg-white/[0.02] p-4">
        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-slate-500" />
        <p className="text-xs text-slate-500 leading-relaxed">
          Tesla reads <code className="text-slate-400">LockChime.wav</code> from the root of the USB
          drive. Only one lock sound can be active at a time. Setting a new active sound replaces the
          previous one. Tesla supports WAV format only with a maximum duration of{" "}
          {MAX_DURATION_SECONDS} seconds. Random mode selects from your library automatically — "On
          Connect" works on all Pis, "Scheduled" requires a Pi with a real-time clock (RTC).
        </p>
      </div>

      {/* Toast */}
      {toast && (
        <div
          className={`fixed bottom-6 right-6 z-50 flex items-center gap-3 rounded-xl px-4 py-3 shadow-xl text-sm font-medium transition-all ${
            toast.type === "success"
              ? "bg-emerald-500/20 border border-emerald-500/30 text-emerald-300"
              : "bg-red-500/20 border border-red-500/30 text-red-300"
          }`}
        >
          {toast.type === "success" ? (
            <CheckCircle2 className="h-4 w-4 shrink-0" />
          ) : (
            <AlertCircle className="h-4 w-4 shrink-0" />
          )}
          {toast.msg}
        </div>
      )}
    </div>
  )
}
