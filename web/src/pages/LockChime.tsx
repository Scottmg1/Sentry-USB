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
  AlertTriangle,
  Shuffle,
  Clock,
  Zap,
  Download,
  Search,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Loader2,
  Shield,
  Pencil,
  Unplug,
} from "lucide-react"

const API_BASE = "/api"
const MAX_DURATION_SECONDS = 7
const MAX_FILE_BYTES = 5 * 1024 * 1024 // 5 MB
const COMMUNITY_PAGE_SIZE = 20
const LIBRARY_PAGE_SIZE = 15

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
  hour: number
  day: number
  has_rtc: boolean
  has_ble: boolean
}

interface CommunitySound {
  code: string
  name: string
  download_count: number
  duration: number
  created_at: string
  fingerprint?: string
}

interface CommunityLibraryResponse {
  sounds: CommunitySound[]
  total: number
  page: number
}

type Tab = "library" | "community"
type CommunitySubTab = "browse" | "upload"
type SortOption = "newest" | "oldest" | "popular" | "name"

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatDuration(seconds: number): string {
  return `${seconds.toFixed(1)}s`
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

// ─────────────────────────────────────────────────────────────
// Main component
// ─────────────────────────────────────────────────────────────

export default function LockChime() {
  const [tab, setTab] = useState<Tab>("library")
  const [adminPasscode, setAdminPasscode] = useState<string | null>(null)
  const [showPasscodePrompt, setShowPasscodePrompt] = useState(false)
  const [volume, setVolume] = useState(() => {
    const saved = localStorage.getItem("lockchime-preview-volume")
    return saved !== null ? Number(saved) : 0.5
  })
  function handleVolumeChange(v: number) {
    setVolume(v)
    localStorage.setItem("lockchime-preview-volume", String(v))
  }

  return (
    <div className="space-y-6">
      {/* Tabs */}
      <div className="flex items-center gap-2">
        <button
          onClick={() => setTab("library")}
          className={`rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
            tab === "library"
              ? "bg-violet-500/15 text-violet-400"
              : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
          }`}
        >
          My Library
        </button>
        <button
          onClick={() => setTab("community")}
          className={`rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
            tab === "community"
              ? "bg-violet-500/15 text-violet-400"
              : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
          }`}
        >
          Community
        </button>

        {adminPasscode && (
          <div className="ml-auto flex items-center gap-1.5 rounded bg-red-500/10 border border-red-500/20 px-2.5 py-1 text-xs text-red-400">
            <Shield className="h-3 w-3" />
            Admin Mode
            <button onClick={() => setAdminPasscode(null)} className="ml-1 hover:text-red-300">
              <X className="h-3 w-3" />
            </button>
          </div>
        )}
      </div>

      {/* USB Disconnect Notice */}
      {tab === "library" && (
        <div className="rounded-xl border border-amber-500/20 bg-amber-500/[0.06] p-3 flex items-start gap-3">
          <Unplug className="h-4 w-4 shrink-0 mt-0.5 text-amber-400" />
          <p className="text-xs text-amber-200/80 leading-relaxed">
            Changing or clearing the active lock chime requires a brief USB disconnect (~5 seconds).
            Tesla will temporarily lose access to the drives during this time.
          </p>
        </div>
      )}

      {/* Preview Volume */}
      <div className="flex items-center gap-3 rounded-xl border border-white/10 bg-white/[0.02] px-4 py-3">
        <Volume2 className="h-4 w-4 shrink-0 text-slate-400" />
        <span className="text-xs font-medium text-slate-400 whitespace-nowrap">Preview Volume</span>
        <input
          type="range"
          min={0}
          max={1}
          step={0.01}
          value={volume}
          onChange={(e) => handleVolumeChange(Number(e.target.value))}
          className="flex-1 h-1.5 accent-violet-500 cursor-pointer"
        />
        <span className="text-xs text-slate-500 tabular-nums w-8 text-right">{Math.round(volume * 100)}%</span>
      </div>

      {tab === "library" ? (
        <MyLibraryTab volume={volume} />
      ) : (
        <CommunityTab adminPasscode={adminPasscode} volume={volume} />
      )}

      {/* Passcode modal */}
      {showPasscodePrompt && (
        <PasscodeModal
          onSuccess={(passcode) => {
            setAdminPasscode(passcode)
            setShowPasscodePrompt(false)
          }}
          onClose={() => setShowPasscodePrompt(false)}
        />
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────
// Passcode modal (reuses the same pattern as Community Wraps)
// ─────────────────────────────────────────────────────────────

function PasscodeModal({ onSuccess, onClose }: { onSuccess: (passcode: string) => void; onClose: () => void }) {
  const [input, setInput] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)

  const handleValidate = async () => {
    if (!input.trim()) return
    setValidating(true)
    setError(null)
    try {
      const res = await fetch(`${API_BASE}/lockchime/community/admin/validate`, {
        method: "POST",
        headers: { "x-passcode": input.trim() },
      })
      if (res.ok) {
        onSuccess(input.trim())
      } else {
        setError("Invalid passcode")
        setInput("")
      }
    } catch {
      setError("Connection failed")
    } finally {
      setValidating(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="w-full max-w-sm overflow-hidden rounded-2xl border border-white/10 bg-slate-900 p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="text-lg font-semibold text-slate-100">Admin Access</h3>
        <p className="mt-1 text-xs text-slate-500">Enter the admin passcode to continue</p>
        <input
          type="password"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleValidate()}
          placeholder="Passcode"
          autoFocus
          className="mt-4 w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-violet-500/50 focus:outline-none"
        />
        {error && <p className="mt-2 text-xs text-red-400">{error}</p>}
        <div className="mt-4 flex gap-3">
          <button
            onClick={handleValidate}
            disabled={!input.trim() || validating}
            className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-violet-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-violet-500 disabled:opacity-50"
          >
            {validating && <Loader2 className="h-4 w-4 animate-spin" />}
            Validate
          </button>
          <button
            onClick={onClose}
            className="rounded-lg border border-white/10 px-4 py-2.5 text-sm text-slate-400 transition-colors hover:bg-white/5"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────
// Toast component (shared)
// ─────────────────────────────────────────────────────────────

function useToast() {
  const [toast, setToast] = useState<{ msg: string; type: "success" | "error" } | null>(null)

  const showToast = useCallback((msg: string, type: "success" | "error") => {
    setToast({ msg, type })
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 4000)
    return () => clearTimeout(t)
  }, [toast])

  const ToastView = toast ? (
    <div
      className={`fixed bottom-6 right-6 z-50 flex items-center gap-3 rounded-xl px-4 py-3 shadow-xl text-sm font-medium ${
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
  ) : null

  return { showToast, ToastView }
}

// ─────────────────────────────────────────────────────────────
// My Library tab
// ─────────────────────────────────────────────────────────────

function MyLibraryTab({ volume }: { volume: number }) {
  const [sounds, setSounds] = useState<SoundEntry[]>([])
  const [activeName, setActiveName] = useState("")
  const [activeSet, setActiveSet] = useState(false)
  const [loading, setLoading] = useState(true)
  const [playingName, setPlayingName] = useState<string | null>(null)
  const [uploadDragging, setUploadDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [activating, setActivating] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [clearing, setClearing] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [libPage, setLibPage] = useState(1)
  const [pendingFile, setPendingFile] = useState<File | null>(null)
  const [pendingName, setPendingName] = useState("")
  const fileInputRef = useRef<HTMLInputElement>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  // Random mode state
  const [randomCfg, setRandomCfg] = useState<RandomConfig>({
    enabled: false,
    mode: "on_connect",
    interval: "daily",
    hour: 3,
    day: 0,
    has_rtc: false,
    has_ble: false,
  })
  const [randomLoading, setRandomLoading] = useState(true)
  const [savingRandom, setSavingRandom] = useState(false)
  const [randomizing, setRandomizing] = useState(false)
  const [randomExpanded, setRandomExpanded] = useState(false)
  const [showRandomDisclaimer, setShowRandomDisclaimer] = useState(false)
  const [pendingRandomCfg, setPendingRandomCfg] = useState<RandomConfig | null>(null)
  const [bleTestLoading, setBleTestLoading] = useState(false)
  const [bleTestResult, setBleTestResult] = useState<{ success: boolean; label?: string; shift_state?: string; error?: string } | null>(null)

  const { showToast, ToastView } = useToast()

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
      // Random config is optional — silently fail
    } finally {
      setRandomLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchSounds()
    fetchRandomConfig()
  }, [fetchSounds, fetchRandomConfig])

  useEffect(() => {
    return () => { audioRef.current?.pause() }
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
    audio.volume = volume
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
        showToast(`Sound is ${duration.toFixed(1)}s — Tesla requires ${MAX_DURATION_SECONDS}s or less`, "error")
        return
      }
    } catch {
      showToast("Could not read WAV file — is it a valid .wav?", "error")
      return
    }
    // Show rename dialog before uploading
    setPendingFile(file)
    setPendingName(file.name.replace(/\.wav$/i, ""))
  }

  async function handleUploadConfirm() {
    if (!pendingFile || !pendingName.trim()) return
    const cleanName = pendingName.trim()
    if (cleanName.toLowerCase() === "lockchime") {
      showToast("Name cannot be \"lockchime\" — please choose a different name", "error")
      return
    }
    setUploading(true)
    setPendingFile(null)
    setPendingName("")
    try {
      const renamedFile = new File([pendingFile], cleanName + ".wav", { type: pendingFile.type })
      const form = new FormData()
      form.append("file", renamedFile)
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
      const res = await fetch(`${API_BASE}/lockchime/activate/${encodeURIComponent(name)}`, { method: "POST" })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      showToast(
        data.usb_rebound
          ? `"${name}" activated — USB re-enumerated, Tesla will use the new sound`
          : `"${name}" is now your active lock sound`,
        "success"
      )
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
      const res = await fetch(`${API_BASE}/lockchime/clear-active`, { method: "POST" })
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
      const res = await fetch(`${API_BASE}/lockchime/${encodeURIComponent(name)}`, { method: "DELETE" })
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
        body: JSON.stringify({ enabled: newCfg.enabled, mode: newCfg.mode, interval: newCfg.interval }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      setRandomCfg((prev) => ({ ...prev, ...newCfg, enabled: data.enabled }))
      showToast(newCfg.enabled ? "Random mode enabled" : "Random mode disabled", "success")
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
      showToast(
        data.usb_rebound
          ? `Randomly selected "${data.active}" — USB re-enumerated`
          : `Randomly selected "${data.active}"`,
        "success"
      )
      await fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Failed to randomize", "error")
    } finally {
      setRandomizing(false)
    }
  }

  return (
    <div className="space-y-5">
      {/* Active Sound Banner */}
      <div
        className={`rounded-xl border p-4 flex items-center justify-between gap-4 ${
          activeSet ? "border-violet-500/30 bg-violet-500/[0.08]" : "border-white/10 bg-white/[0.03]"
        }`}
      >
        <div className="flex items-center gap-3 min-w-0">
          <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${
            activeSet ? "bg-violet-500/20" : "bg-white/5"
          }`}>
            <Volume2 className={`h-4.5 w-4.5 ${activeSet ? "text-violet-400" : "text-slate-600"}`} />
          </div>
          <div className="min-w-0">
            <p className="text-sm font-medium text-slate-200">
              {activeSet ? "Active lock sound" : "No lock sound set"}
            </p>
            {activeSet && activeName ? (
              <p className="text-xs text-slate-400 truncate">{activeName}</p>
            ) : !activeSet ? (
              <p className="text-xs text-slate-500">Tesla will use its default chime</p>
            ) : null}
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

      {/* Sound library */}
      <div>
        <h3 className="mb-3 text-xs font-medium text-slate-500 uppercase tracking-wider">
          Sounds on this Pi
        </h3>

        {loading && (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-5 w-5 animate-spin text-violet-400" />
          </div>
        )}

        {!loading && sounds.length === 0 && (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-white/10 bg-white/[0.02] py-12 text-center">
            <Music className="h-10 w-10 text-slate-700" />
            <div>
              <p className="text-sm font-medium text-slate-400">No sounds yet</p>
              <p className="mt-1 text-xs text-slate-600">Upload a .wav file or download from the Community tab</p>
            </div>
          </div>
        )}

        {!loading && sounds.length > 0 && (() => {
          const libTotalPages = Math.ceil(sounds.length / LIBRARY_PAGE_SIZE)
          const safePage = Math.min(libPage, libTotalPages)
          const startIdx = (safePage - 1) * LIBRARY_PAGE_SIZE
          const pageSounds = sounds.slice(startIdx, startIdx + LIBRARY_PAGE_SIZE)

          return (
            <>
              <div className="space-y-2">
                {pageSounds.map((sound) => {
                  const isPlaying = playingName === sound.name
                  const isActive = activeSet && activeName === sound.name
                  const isActivating = activating === sound.name
                  const isDeleting = deleting === sound.name

                  return (
                    <div
                      key={sound.name}
                      className={`group flex items-center gap-3 rounded-xl border px-4 py-3 transition-colors ${
                        isActive
                          ? "border-violet-500/40 bg-violet-500/[0.08]"
                          : "border-white/10 bg-white/[0.02] hover:bg-white/[0.04]"
                      }`}
                    >
                      <button
                        onClick={() => togglePlay(sound.name)}
                        className={`shrink-0 flex h-9 w-9 items-center justify-center rounded-full transition-colors ${
                          isPlaying
                            ? "bg-violet-500/20 text-violet-300"
                            : "bg-white/5 text-slate-400 hover:bg-white/10 hover:text-white"
                        }`}
                        title={isPlaying ? "Pause" : "Play"}
                      >
                        {isPlaying ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4 translate-x-0.5" />}
                      </button>

                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium text-slate-200">{sound.name}</p>
                        <p className="text-xs text-slate-500">{formatSize(sound.size)}</p>
                      </div>

                      {isActive && (
                        <span className="shrink-0 flex items-center gap-1 rounded-full bg-violet-500/20 px-2 py-0.5 text-xs font-medium text-violet-300">
                          <CheckCircle2 className="h-3 w-3" />
                          Active
                        </span>
                      )}

                      {!isActive && (
                        <button
                          onClick={() => handleActivate(sound.name)}
                          disabled={isActivating || isDeleting}
                          className="shrink-0 rounded-lg border border-white/10 px-3 py-1.5 text-xs text-slate-400 transition-colors hover:border-violet-500/40 hover:text-violet-300 disabled:opacity-50"
                        >
                          {isActivating ? "Setting..." : "Set Active"}
                        </button>
                      )}

                      {deleteConfirm === sound.name ? (
                        <div className="shrink-0 flex items-center gap-1">
                          <button
                            onClick={() => handleDelete(sound.name)}
                            disabled={isDeleting}
                            className="rounded-lg bg-red-500/20 px-2.5 py-1.5 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/30 disabled:opacity-50"
                          >
                            {isDeleting ? "..." : "Confirm"}
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

              {libTotalPages > 1 && (
                <div className="flex items-center justify-center gap-3 pt-2">
                  <button
                    onClick={() => setLibPage((p) => Math.max(1, p - 1))}
                    disabled={safePage <= 1}
                    className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-white/5 hover:text-white disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-slate-400"
                  >
                    <ChevronLeft className="h-4 w-4" />
                  </button>
                  <span className="text-xs tabular-nums text-slate-500">
                    Page {safePage} of {libTotalPages}
                  </span>
                  <button
                    onClick={() => setLibPage((p) => Math.min(libTotalPages, p + 1))}
                    disabled={safePage >= libTotalPages}
                    className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-white/5 hover:text-white disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-slate-400"
                  >
                    <ChevronRight className="h-4 w-4" />
                  </button>
                </div>
              )}
            </>
          )
        })()}
      </div>

      {/* Random Mode */}
      {!randomLoading && (
        <div className="rounded-xl border border-white/10 bg-white/[0.02] p-4 space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5 min-w-0">
              {/* Clickable header to expand/collapse when enabled */}
              <div
                onClick={() => randomCfg.enabled && setRandomExpanded((v) => !v)}
                className={`flex items-center gap-2.5 min-w-0 ${randomCfg.enabled ? "cursor-pointer" : "cursor-default"}`}
              >
                <Shuffle className={`h-4 w-4 shrink-0 ${randomCfg.enabled ? "text-amber-400" : "text-slate-500"}`} />
                <h2 className="text-sm font-medium text-slate-200">Random Mode</h2>
                {randomCfg.enabled && (
                  <>
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-md ${
                      randomCfg.mode === "smart"
                        ? "bg-emerald-500/15 text-emerald-400"
                        : "bg-amber-500/15 text-amber-400"
                    }`}>
                      {randomCfg.mode === "on_connect" ? "On Connect" : randomCfg.mode === "scheduled" ? "Scheduled" : "Smart"}
                    </span>
                    {!randomExpanded && sounds.length >= 2 && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          handleRandomizeNow()
                        }}
                        disabled={randomizing}
                        className="flex items-center gap-1.5 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-1 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-50"
                      >
                        <Shuffle className="h-3 w-3" />
                        {randomizing ? "..." : "Randomize"}
                      </button>
                    )}
                    <ChevronDown className={`h-3.5 w-3.5 text-slate-500 transition-transform ${randomExpanded ? "rotate-180" : ""}`} />
                  </>
                )}
              </div>
            </div>
            <button
              onClick={(e) => {
                e.stopPropagation()
                if (!randomCfg.enabled) {
                  // Enabling — show disclaimer first
                  setPendingRandomCfg({ ...randomCfg, enabled: true })
                  setShowRandomDisclaimer(true)
                } else {
                  // Disabling — no disclaimer needed, collapse too
                  setRandomExpanded(false)
                  handleSaveRandomConfig({ ...randomCfg, enabled: false })
                }
              }}
              disabled={savingRandom || sounds.length < 2}
              className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors ${
                randomCfg.enabled ? "bg-amber-500" : "bg-white/10"
              } ${sounds.length < 2 ? "opacity-40 cursor-not-allowed" : "cursor-pointer"}`}
              title={sounds.length < 2 ? "Upload at least 2 sounds to use random mode" : ""}
            >
              <span className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${
                randomCfg.enabled ? "translate-x-6" : "translate-x-1"
              }`} />
            </button>
          </div>

          {sounds.length < 2 && (
            <p className="text-xs text-slate-500">Upload at least 2 sounds to use random mode.</p>
          )}

          {randomCfg.enabled && randomExpanded && sounds.length >= 2 && (
            <div className="space-y-3">
              <div className="flex gap-2">
                <button
                  onClick={() => handleSaveRandomConfig({ ...randomCfg, mode: "on_connect" })}
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
                    <p className="mt-0.5 text-[10px] opacity-60">Random sound each time Tesla connects</p>
                  </div>
                </button>

                <button
                  onClick={() => {
                    if (!randomCfg.has_rtc) {
                      showToast("Scheduled mode requires a Pi with RTC (real-time clock)", "error")
                      return
                    }
                    if (randomCfg.mode !== "scheduled") {
                      setPendingRandomCfg({ ...randomCfg, mode: "scheduled", interval: randomCfg.interval || "daily" })
                      setShowRandomDisclaimer(true)
                      return
                    }
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
                      {randomCfg.has_rtc ? "Change on a time schedule" : "Requires RTC hardware"}
                    </p>
                  </div>
                </button>

                <button
                  onClick={() => {
                    if (!randomCfg.has_rtc || !randomCfg.has_ble) {
                      showToast(
                        !randomCfg.has_ble
                          ? "Smart mode requires a paired BLE key — pair your Pi in Settings first"
                          : "Smart mode requires a Pi with RTC (real-time clock)",
                        "error"
                      )
                      return
                    }
                    if (randomCfg.mode !== "smart") {
                      setPendingRandomCfg({ ...randomCfg, mode: "smart", interval: randomCfg.interval || "daily" })
                      setShowRandomDisclaimer(true)
                      return
                    }
                  }}
                  disabled={savingRandom}
                  className={`flex-1 flex items-center gap-2 rounded-lg border px-3 py-2.5 text-xs transition-colors ${
                    randomCfg.mode === "smart"
                      ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-300"
                      : !randomCfg.has_rtc || !randomCfg.has_ble
                        ? "border-white/5 text-slate-600 cursor-not-allowed"
                        : "border-white/10 text-slate-400 hover:border-white/20"
                  }`}
                >
                  <Shield className="h-3.5 w-3.5 shrink-0" />
                  <div className="text-left">
                    <p className="font-medium">Smart</p>
                    <p className="mt-0.5 text-[10px] opacity-60">
                      {!randomCfg.has_ble
                        ? "Requires BLE pairing"
                        : !randomCfg.has_rtc
                          ? "Requires RTC hardware"
                          : "Only changes while parked via BLE"}
                    </p>
                  </div>
                </button>
              </div>

              {randomCfg.mode === "smart" && (
                <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/[0.06] p-3 space-y-3">
                  <p className="text-xs text-emerald-200/80 leading-relaxed">
                    Smart mode uses your BLE key to check if the vehicle is in Park before changing the lock sound.
                    If the car is in Drive, Reverse, or Neutral the change is skipped and retried later.
                    Sentry footage recording during Park may still be briefly interrupted (~5 seconds) during the USB reconnect.
                  </p>
                  <div className="flex items-center gap-3">
                    <button
                      onClick={async () => {
                        setBleTestLoading(true)
                        setBleTestResult(null)
                        try {
                          const res = await fetch(`${API_BASE}/lockchime/ble-shift-state`)
                          const data = await res.json()
                          setBleTestResult(data)
                        } catch {
                          setBleTestResult({ success: false, error: "Request failed — is the server running?" })
                        } finally {
                          setBleTestLoading(false)
                        }
                      }}
                      disabled={bleTestLoading}
                      className="flex items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-1.5 text-xs font-medium text-emerald-300 transition-colors hover:bg-emerald-500/20 disabled:opacity-50"
                    >
                      {bleTestLoading ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Shield className="h-3.5 w-3.5" />
                      )}
                      {bleTestLoading ? "Querying..." : "Test BLE"}
                    </button>
                    {bleTestResult && (
                      <span className={`text-xs font-medium ${bleTestResult.success ? "text-emerald-300" : "text-red-400"}`}>
                        {bleTestResult.success
                          ? `Vehicle is in ${bleTestResult.label} (${bleTestResult.shift_state})`
                          : bleTestResult.error}
                      </span>
                    )}
                  </div>
                </div>
              )}

              {/* Scheduled mode: time-based schedule with hour/day pickers */}
              {randomCfg.mode === "scheduled" && randomCfg.has_rtc && (
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-slate-400">Change every:</span>
                    {(["hourly", "daily", "weekly"] as const).map((int) => (
                      <button
                        key={int}
                        onClick={() => handleSaveRandomConfig({ ...randomCfg, interval: int })}
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

                  {/* Day picker for weekly */}
                  {randomCfg.interval === "weekly" && (
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-slate-400">On:</span>
                      {(["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const).map((dayName, i) => (
                        <button
                          key={dayName}
                          onClick={() => handleSaveRandomConfig({ ...randomCfg, day: i })}
                          disabled={savingRandom}
                          className={`rounded-md px-2 py-1 text-xs transition-colors ${
                            randomCfg.day === i
                              ? "bg-amber-500/20 text-amber-300 font-medium"
                              : "text-slate-500 hover:text-slate-300"
                          }`}
                        >
                          {dayName}
                        </button>
                      ))}
                    </div>
                  )}

                  {/* Hour picker for daily and weekly */}
                  {(randomCfg.interval === "daily" || randomCfg.interval === "weekly") && (
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-slate-400">At:</span>
                      <select
                        value={randomCfg.hour}
                        onChange={(e) => handleSaveRandomConfig({ ...randomCfg, hour: Number(e.target.value) })}
                        disabled={savingRandom}
                        className="rounded-md border border-white/10 bg-white/[0.03] px-2 py-1 text-xs text-slate-300 focus:border-violet-500/50 focus:outline-none"
                      >
                        {Array.from({ length: 24 }, (_, h) => {
                          const ampm = h === 0 ? "12 AM" : h < 12 ? `${h} AM` : h === 12 ? "12 PM" : `${h - 12} PM`
                          return (
                            <option key={h} value={h}>
                              {ampm}
                            </option>
                          )
                        })}
                      </select>
                    </div>
                  )}
                </div>
              )}

              {/* Smart mode: same schedule options as scheduled but with emerald accent */}
              {randomCfg.mode === "smart" && (
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-slate-400">Change every:</span>
                    {(["hourly", "daily", "weekly"] as const).map((int) => (
                      <button
                        key={int}
                        onClick={() => handleSaveRandomConfig({ ...randomCfg, interval: int })}
                        disabled={savingRandom}
                        className={`rounded-md px-2.5 py-1 text-xs transition-colors ${
                          randomCfg.interval === int
                            ? "bg-emerald-500/20 text-emerald-300 font-medium"
                            : "text-slate-500 hover:text-slate-300"
                        }`}
                      >
                        {int.charAt(0).toUpperCase() + int.slice(1)}
                      </button>
                    ))}
                  </div>

                  {randomCfg.interval === "weekly" && (
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-slate-400">On:</span>
                      {(["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const).map((dayName, i) => (
                        <button
                          key={dayName}
                          onClick={() => handleSaveRandomConfig({ ...randomCfg, day: i })}
                          disabled={savingRandom}
                          className={`rounded-md px-2 py-1 text-xs transition-colors ${
                            randomCfg.day === i
                              ? "bg-emerald-500/20 text-emerald-300 font-medium"
                              : "text-slate-500 hover:text-slate-300"
                          }`}
                        >
                          {dayName}
                        </button>
                      ))}
                    </div>
                  )}

                  {(randomCfg.interval === "daily" || randomCfg.interval === "weekly") && (
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-slate-400">At:</span>
                      <select
                        value={randomCfg.hour}
                        onChange={(e) => handleSaveRandomConfig({ ...randomCfg, hour: Number(e.target.value) })}
                        disabled={savingRandom}
                        className="rounded-md border border-white/10 bg-white/[0.03] px-2 py-1 text-xs text-slate-300 focus:border-violet-500/50 focus:outline-none"
                      >
                        {Array.from({ length: 24 }, (_, h) => {
                          const ampm = h === 0 ? "12 AM" : h < 12 ? `${h} AM` : h === 12 ? "12 PM" : `${h - 12} PM`
                          return <option key={h} value={h}>{ampm}</option>
                        })}
                      </select>
                    </div>
                  )}
                </div>
              )}

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

      {/* Upload area / rename-and-confirm */}
      {pendingFile ? (
        <div className="rounded-xl border-2 border-violet-500/40 bg-violet-500/[0.06] p-4 space-y-3">
          <div className="flex items-start gap-3 rounded-lg bg-white/[0.04] border border-white/10 px-3 py-2.5">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
            <p className="text-xs text-slate-400">
              This name will be the file name on your Pi and what shows in the community if you share it.
            </p>
          </div>
          <div>
            <label className="text-xs font-medium text-slate-500">Sound Name</label>
            <input
              type="text"
              value={pendingName}
              onChange={(e) => setPendingName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleUploadConfirm()}
              maxLength={50}
              autoFocus
              className="mt-1 w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-violet-500/50 focus:outline-none"
            />
            <p className="mt-1 text-xs text-slate-600">Will be saved as <code className="text-slate-400">{pendingName.trim() || "…"}.wav</code></p>
          </div>
          <div className="flex gap-2">
            <button
              onClick={handleUploadConfirm}
              disabled={!pendingName.trim() || uploading}
              className="flex items-center gap-1.5 rounded-lg bg-violet-600 px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-violet-500 disabled:opacity-50"
            >
              {uploading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
              {uploading ? "Uploading..." : "Upload"}
            </button>
            <button
              onClick={() => { setPendingFile(null); setPendingName(""); if (fileInputRef.current) fileInputRef.current.value = "" }}
              className="rounded-lg border border-white/10 px-3 py-2 text-xs text-slate-400 transition-colors hover:bg-white/5"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <div
          className={`relative rounded-xl border-2 border-dashed transition-colors cursor-pointer ${
            uploadDragging
              ? "border-violet-500/60 bg-violet-500/10"
              : "border-white/10 hover:border-white/20 bg-white/[0.02]"
          }`}
          onDragOver={(e) => { e.preventDefault(); setUploadDragging(true) }}
          onDragLeave={() => setUploadDragging(false)}
          onDrop={(e) => {
            e.preventDefault()
            setUploadDragging(false)
            const file = e.dataTransfer.files[0]
            if (file) handleFileSelected(file)
          }}
          onClick={() => !uploading && fileInputRef.current?.click()}
        >
          <div className="flex flex-col items-center gap-3 py-8 px-4 text-center">
            {uploading ? (
              <>
                <Loader2 className="h-5 w-5 animate-spin text-violet-400" />
                <p className="text-sm text-slate-400">Uploading...</p>
              </>
            ) : (
              <>
                <Upload className="h-8 w-8 text-slate-600" />
                <div>
                  <p className="text-sm font-medium text-slate-300">Drop a .wav file or click to browse</p>
                  <p className="mt-1 text-xs text-slate-500">WAV only · max {MAX_DURATION_SECONDS}s · max 5 MB</p>
                </div>
              </>
            )}
          </div>
        </div>
      )}
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

      {/* Info */}
      <div className="flex items-start gap-3 rounded-xl border border-white/10 bg-white/[0.02] p-4">
        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-slate-500" />
        <p className="text-xs text-slate-500 leading-relaxed">
          Tesla reads <code className="text-slate-400">LockChime.wav</code> from the root of the USB drive.
          Only one lock sound can be active at a time. Tesla supports WAV format only, max {MAX_DURATION_SECONDS} seconds.
          Random mode selects from your library automatically — "On Connect" works on all Pis,
          "Scheduled" requires a Pi with a real-time clock (RTC), and
          "Smart" uses BLE to only change while parked (requires RTC + BLE pairing).
        </p>
      </div>

      {/* Random Mode Disclaimer Modal */}
      {showRandomDisclaimer && pendingRandomCfg && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => setShowRandomDisclaimer(false)}>
          <div
            className="w-full max-w-md overflow-hidden rounded-2xl border border-white/10 bg-slate-900 p-6"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-500/15">
                <AlertTriangle className="h-5 w-5 text-amber-400" />
              </div>
              <h3 className="text-lg font-semibold text-slate-100">Potential Recording Loss</h3>
            </div>

            <div className="mt-4 space-y-3">
              <p className="text-sm text-slate-300 leading-relaxed">
                Random and Scheduled modes require a brief USB disconnect (~5 seconds) each time the lock
                sound is changed. During this window, Tesla cannot write dashcam or sentry footage.
              </p>
              <div className="rounded-lg border border-amber-500/20 bg-amber-500/[0.06] p-3">
                <p className="text-xs text-amber-200/80 leading-relaxed">
                  <strong className="text-amber-300">By enabling this feature you acknowledge:</strong>{" "}
                  Any dashcam or sentry clips in progress during the USB reconnect may be lost.
                  The Sentry Six team is not responsible for any data loss while this feature is active.
                </p>
              </div>
              <ul className="space-y-1.5 text-xs text-slate-400">
                <li className="flex items-start gap-2">
                  <Zap className="h-3.5 w-3.5 shrink-0 mt-0.5 text-slate-500" />
                  <span><strong className="text-slate-300">On Connect</strong> — changes the sound when the Pi reconnects to Tesla (during normal archive cycles)</span>
                </li>
                <li className="flex items-start gap-2">
                  <Clock className="h-3.5 w-3.5 shrink-0 mt-0.5 text-slate-500" />
                  <span><strong className="text-slate-300">Scheduled</strong> — changes on a timer which may disconnect USB at any time, including while driving</span>
                </li>
                <li className="flex items-start gap-2">
                  <Shield className="h-3.5 w-3.5 shrink-0 mt-0.5 text-slate-500" />
                  <span><strong className="text-slate-300">Smart</strong> — uses BLE to check if parked before changing. Only sentry/recent clips may be affected, never while driving</span>
                </li>
              </ul>
            </div>

            <div className="mt-5 flex gap-3">
              <button
                onClick={() => {
                  setShowRandomDisclaimer(false)
                  handleSaveRandomConfig(pendingRandomCfg)
                  setPendingRandomCfg(null)
                }}
                className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-amber-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-amber-500"
              >
                I Understand, Enable
              </button>
              <button
                onClick={() => {
                  setShowRandomDisclaimer(false)
                  setPendingRandomCfg(null)
                }}
                className="rounded-lg border border-white/10 px-4 py-2.5 text-sm text-slate-400 transition-colors hover:bg-white/5"
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {ToastView}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────
// Community tab
// ─────────────────────────────────────────────────────────────

function CommunityTab({ adminPasscode, volume }: { adminPasscode: string | null; volume: number }) {
  const [subTab, setSubTab] = useState<CommunitySubTab>("browse")

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-2">
        <button
          onClick={() => setSubTab("browse")}
          className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
            subTab === "browse"
              ? "bg-white/10 text-slate-200"
              : "text-slate-500 hover:text-slate-300"
          }`}
        >
          Browse
        </button>
        <button
          onClick={() => setSubTab("upload")}
          className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
            subTab === "upload"
              ? "bg-white/10 text-slate-200"
              : "text-slate-500 hover:text-slate-300"
          }`}
        >
          Share a Sound
        </button>
      </div>

      {subTab === "browse" ? (
        <CommunityBrowse adminPasscode={adminPasscode} volume={volume} />
      ) : (
        <CommunityUpload adminPasscode={adminPasscode} />
      )}
    </div>
  )
}

function CommunityBrowse({ adminPasscode, volume }: { adminPasscode: string | null; volume: number }) {
  const [sounds, setSounds] = useState<CommunitySound[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const [sort, setSort] = useState<SortOption>("newest")
  const [downloadingCode, setDownloadingCode] = useState<string | null>(null)
  const [playingCode, setPlayingCode] = useState<string | null>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [editingSound, setEditingSound] = useState<CommunitySound | null>(null)
  const [deletingSound, setDeletingSound] = useState<CommunitySound | null>(null)
  const { showToast, ToastView } = useToast()

  useEffect(() => {
    return () => { audioRef.current?.pause() }
  }, [])

  function togglePreview(code: string) {
    if (playingCode === code) {
      audioRef.current?.pause()
      setPlayingCode(null)
      return
    }
    audioRef.current?.pause()
    const url = `${API_BASE}/lockchime/community/stream/${code}`
    const audio = new Audio(url)
    audio.volume = volume
    audioRef.current = audio
    audio.onended = () => setPlayingCode(null)
    audio.onerror = () => {
      setPlayingCode(null)
      showToast("Could not play preview", "error")
    }
    audio.play().catch(() => {
      setPlayingCode(null)
      showToast("Could not play preview", "error")
    })
    setPlayingCode(code)
  }

  const totalPages = Math.ceil(total / COMMUNITY_PAGE_SIZE)

  const fetchSounds = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      params.set("page", String(page))
      params.set("limit", String(COMMUNITY_PAGE_SIZE))
      if (search) params.set("search", search)
      if (sort !== "newest") params.set("sort", sort)

      const headers: HeadersInit = {}
      if (adminPasscode) headers["x-passcode"] = adminPasscode

      const res = await fetch(`${API_BASE}/lockchime/community/library?${params}`, { headers })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data: CommunityLibraryResponse = await res.json()
      setSounds(data.sounds ?? [])
      setTotal(data.total ?? 0)
    } catch {
      setError("Community lock chimes are not available yet. Check back soon!")
      setSounds([])
    } finally {
      setLoading(false)
    }
  }, [page, search, sort, adminPasscode])

  useEffect(() => {
    const timer = setTimeout(fetchSounds, search ? 300 : 0)
    return () => clearTimeout(timer)
  }, [fetchSounds])

  async function handleDownload(sound: CommunitySound) {
    setDownloadingCode(sound.code)
    try {
      const res = await fetch(`${API_BASE}/lockchime/community/download/${sound.code}`, { method: "POST" })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      showToast(`Downloaded "${sound.name}" to your library`, "success")
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Download failed", "error")
    } finally {
      setDownloadingCode(null)
    }
  }

  async function handleAdminEdit(code: string, name: string) {
    if (!adminPasscode) return
    try {
      const res = await fetch(`${API_BASE}/lockchime/community/admin/edit/${code}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json", "x-passcode": adminPasscode },
        body: JSON.stringify({ name }),
      })
      if (!res.ok) throw new Error("Edit failed")
      showToast("Sound updated", "success")
      setEditingSound(null)
      fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Edit failed", "error")
    }
  }

  async function handleAdminDelete(code: string) {
    if (!adminPasscode) return
    try {
      const res = await fetch(`${API_BASE}/lockchime/community/admin/delete/${code}`, {
        method: "DELETE",
        headers: { "x-passcode": adminPasscode },
      })
      if (!res.ok) throw new Error("Delete failed")
      showToast("Sound deleted from community", "success")
      setDeletingSound(null)
      fetchSounds()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Delete failed", "error")
    }
  }

  return (
    <div className="space-y-4">
      {/* Search & sort */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1) }}
            placeholder="Search community sounds..."
            className="w-full rounded-lg border border-white/10 bg-white/[0.03] py-2 pl-10 pr-3 text-sm text-slate-200 placeholder:text-slate-600 focus:border-violet-500/50 focus:outline-none"
          />
        </div>
        <select
          value={sort}
          onChange={(e) => { setSort(e.target.value as SortOption); setPage(1) }}
          className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-300 focus:border-violet-500/50 focus:outline-none"
        >
          <option value="newest">Newest</option>
          <option value="oldest">Oldest</option>
          <option value="popular">Most Downloaded</option>
          <option value="name">Name A–Z</option>
        </select>
      </div>

      {/* Grid */}
      {loading && (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-6 w-6 animate-spin text-violet-400" />
        </div>
      )}

      {!loading && error && (
        <div className="flex flex-col items-center gap-3 rounded-xl border border-white/10 bg-white/[0.02] py-16 text-center">
          <Music className="h-10 w-10 text-slate-700" />
          <p className="text-sm text-slate-400">{error}</p>
        </div>
      )}

      {!loading && !error && sounds.length === 0 && (
        <div className="flex flex-col items-center gap-3 rounded-xl border border-white/10 bg-white/[0.02] py-16 text-center">
          <Music className="h-10 w-10 text-slate-700" />
          <div>
            <p className="text-sm font-medium text-slate-400">No community sounds yet</p>
            <p className="mt-1 text-xs text-slate-600">Be the first to share a lock chime!</p>
          </div>
        </div>
      )}

      {!loading && !error && sounds.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {sounds.map((sound) => (
            <div
              key={sound.code}
              className="group relative rounded-xl border border-white/10 bg-white/[0.02] p-4 transition-colors hover:bg-white/[0.04]"
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-slate-200">{sound.name}</p>
                  <div className="mt-1 flex items-center gap-3 text-xs text-slate-500">
                    {sound.duration > 0 && <span>{formatDuration(sound.duration)}</span>}
                    <span>{sound.download_count} downloads</span>
                  </div>
                </div>
                <button
                  onClick={() => togglePreview(sound.code)}
                  className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg transition-colors ${
                    playingCode === sound.code
                      ? "bg-violet-500/20 text-violet-300"
                      : "bg-violet-500/10 text-violet-400 hover:bg-violet-500/20"
                  }`}
                  title={playingCode === sound.code ? "Stop" : "Preview"}
                >
                  {playingCode === sound.code ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4 translate-x-0.5" />}
                </button>
              </div>

              <div className="mt-3 flex items-center gap-2">
                <button
                  onClick={() => handleDownload(sound)}
                  disabled={downloadingCode === sound.code}
                  className="flex flex-1 items-center justify-center gap-1.5 rounded-lg bg-violet-600/80 px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-violet-500 disabled:opacity-50"
                >
                  {downloadingCode === sound.code ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Download className="h-3.5 w-3.5" />
                  )}
                  {downloadingCode === sound.code ? "Downloading..." : "Download to Pi"}
                </button>

                {adminPasscode && (
                  <>
                    <button
                      onClick={() => setEditingSound(sound)}
                      className="rounded-lg border border-white/10 p-2 text-slate-500 transition-colors hover:text-slate-300"
                    >
                      <Pencil className="h-3 w-3" />
                    </button>
                    <button
                      onClick={() => setDeletingSound(sound)}
                      className="rounded-lg border border-white/10 p-2 text-slate-500 transition-colors hover:text-red-400"
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Pagination */}
      {!loading && totalPages > 1 && (
        <div className="flex items-center justify-center gap-3">
          <button
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={page <= 1}
            className="rounded-lg border border-white/10 p-2 text-slate-400 transition-colors hover:bg-white/5 disabled:opacity-30"
          >
            <ChevronLeft className="h-4 w-4" />
          </button>
          <span className="text-xs text-slate-500">
            Page {page} of {totalPages}
          </span>
          <button
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            className="rounded-lg border border-white/10 p-2 text-slate-400 transition-colors hover:bg-white/5 disabled:opacity-30"
          >
            <ChevronRight className="h-4 w-4" />
          </button>
        </div>
      )}

      {/* Edit modal */}
      {editingSound && (
        <EditSoundModal
          sound={editingSound}
          onSave={(name) => handleAdminEdit(editingSound.code, name)}
          onClose={() => setEditingSound(null)}
        />
      )}

      {/* Delete confirm modal */}
      {deletingSound && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => setDeletingSound(null)}>
          <div className="w-full max-w-sm rounded-2xl border border-white/10 bg-slate-900 p-6" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold text-slate-100">Delete Sound</h3>
            <p className="mt-2 text-sm text-slate-400">
              Delete <strong className="text-slate-200">{deletingSound.name}</strong> from the community library?
              This cannot be undone.
            </p>
            <div className="mt-4 flex gap-3">
              <button
                onClick={() => handleAdminDelete(deletingSound.code)}
                className="flex-1 rounded-lg bg-red-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-red-500"
              >
                Delete
              </button>
              <button
                onClick={() => setDeletingSound(null)}
                className="rounded-lg border border-white/10 px-4 py-2.5 text-sm text-slate-400 transition-colors hover:bg-white/5"
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {ToastView}
    </div>
  )
}

function EditSoundModal({ sound, onSave, onClose }: { sound: CommunitySound; onSave: (name: string) => void; onClose: () => void }) {
  const [name, setName] = useState(sound.name)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-2xl border border-white/10 bg-slate-900 p-6" onClick={(e) => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-slate-100">Edit Sound</h3>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={50}
          className="mt-4 w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-violet-500/50 focus:outline-none"
        />
        <div className="mt-4 flex gap-3">
          <button
            onClick={() => onSave(name)}
            disabled={!name.trim()}
            className="flex-1 rounded-lg bg-violet-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-violet-500 disabled:opacity-50"
          >
            Save
          </button>
          <button
            onClick={onClose}
            className="rounded-lg border border-white/10 px-4 py-2.5 text-sm text-slate-400 transition-colors hover:bg-white/5"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}

function CommunityUpload({ adminPasscode }: { adminPasscode: string | null }) {
  const [file, setFile] = useState<File | null>(null)
  const [name, setName] = useState("")
  const [uploading, setUploading] = useState(false)
  const [duration, setDuration] = useState<number | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const { showToast, ToastView } = useToast()

  async function handleFile(f: File) {
    if (!f.name.toLowerCase().endsWith(".wav")) {
      showToast("Only .wav files are supported", "error")
      return
    }
    if (f.size > MAX_FILE_BYTES) {
      showToast("File is too large (max 5 MB)", "error")
      return
    }
    try {
      const dur = await getWavDuration(f)
      if (dur > MAX_DURATION_SECONDS) {
        showToast(`Sound is ${dur.toFixed(1)}s — Tesla requires ${MAX_DURATION_SECONDS}s or less`, "error")
        return
      }
      setDuration(dur)
    } catch {
      showToast("Could not read WAV file", "error")
      return
    }
    setFile(f)
    setName(f.name.replace(/\.wav$/i, ""))
  }

  async function handleSubmit() {
    if (!file || !name.trim()) return
    if (name.trim().toLowerCase().replace(/\.wav$/i, "") === "lockchime") {
      showToast("Sound name cannot be \"lockchime\" — please choose a different name", "error")
      return
    }
    setUploading(true)
    try {
      const form = new FormData()
      form.append("sound", file)
      form.append("name", name.trim())

      const headers: HeadersInit = {}
      if (adminPasscode) headers["x-passcode"] = adminPasscode

      const res = await fetch(`${API_BASE}/lockchime/community/upload`, {
        method: "POST",
        headers,
        body: form,
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
      showToast("Sound submitted! It will appear in the library after review.", "success")
      setFile(null)
      setName("")
      setDuration(null)
      if (fileInputRef.current) fileInputRef.current.value = ""
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : "Upload failed", "error")
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="space-y-5">
      <div className="rounded-xl border border-white/10 bg-white/[0.02] p-5 space-y-4">
        <h3 className="text-sm font-medium text-slate-200">Share a Lock Sound</h3>
        <p className="text-xs text-slate-500">
          Upload a .wav file to share with the Sentry USB community. Submissions are reviewed before appearing in the library.
        </p>

        {/* File selection */}
        {!file ? (
          <div
            className="rounded-xl border-2 border-dashed border-white/10 hover:border-white/20 bg-white/[0.01] cursor-pointer transition-colors"
            onClick={() => fileInputRef.current?.click()}
          >
            <div className="flex flex-col items-center gap-2 py-10 text-center">
              <Upload className="h-6 w-6 text-slate-600" />
              <p className="text-sm text-slate-400">Click to select a .wav file</p>
              <p className="text-xs text-slate-600">Max {MAX_DURATION_SECONDS}s · max 5 MB</p>
            </div>
          </div>
        ) : (
          <div className="rounded-xl border border-violet-500/30 bg-violet-500/[0.06] p-4">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-violet-500/20">
                <Music className="h-5 w-5 text-violet-400" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="truncate text-sm font-medium text-slate-200">{file.name}</p>
                <p className="text-xs text-slate-500">
                  {formatSize(file.size)}{duration !== null ? ` · ${formatDuration(duration)}` : ""}
                </p>
              </div>
              <button
                onClick={() => { setFile(null); setDuration(null); if (fileInputRef.current) fileInputRef.current.value = "" }}
                className="text-slate-500 hover:text-slate-300"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>
        )}
        <input
          ref={fileInputRef}
          type="file"
          accept=".wav,audio/wav,audio/x-wav"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) handleFile(f)
          }}
        />

        {/* Name */}
        <div>
          <label className="text-xs font-medium text-slate-400">Sound name</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={50}
            placeholder="e.g. Sci-Fi Beep"
            className="mt-1 w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-violet-500/50 focus:outline-none"
          />
          <p className="mt-1 text-xs text-slate-600">{name.length}/50 characters</p>
        </div>

        {/* Submit */}
        <button
          onClick={handleSubmit}
          disabled={!file || !name.trim() || uploading}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-violet-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-violet-500 disabled:opacity-50"
        >
          {uploading ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" />
              Submitting...
            </>
          ) : (
            <>
              <Upload className="h-4 w-4" />
              Submit Sound
            </>
          )}
        </button>
      </div>

      {ToastView}
    </div>
  )
}
