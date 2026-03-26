import { useEffect, useState, useCallback, useRef } from "react"
import { Search, Upload, Download, Paintbrush, ChevronLeft, ChevronRight, Loader2, CheckCircle, AlertCircle, Trash2, Pencil, Shield, X } from "lucide-react"
import GodotRenderer, { type GodotRendererHandle } from "../components/wraps/GodotRenderer"

const API_BASE = "/api"

const TESLA_MODELS = [
  "Cybertruck",
  "Model 3",
  "Model 3 (2024+) Standard & Premium",
  "Model 3 (2024+) Performance",
  "Model S",
  "Model X",
  "Model Y",
  "Model Y (2025+) Standard",
  "Model Y (2025+) Premium",
  "Model Y (2025+) Performance",
  "Model Y L",
]

const FILTER_MODELS = ["All", ...TESLA_MODELS]

// Maps display names to Godot scene IDs from Tesla Wrap Studio
// Models without a Godot 3D counterpart (Model S, Model X) are omitted — no 3D preview for those
const MODEL_TO_GODOT_ID: Record<string, string> = {
  "Cybertruck": "cybertruck",
  "Model 3": "model3",
  "Model 3 (2024+) Standard & Premium": "model3-2024-base",
  "Model 3 (2024+) Performance": "model3-2024-performance",
  "Model Y": "modely",
  "Model Y (2025+) Standard": "modely-2025-base",
  "Model Y (2025+) Premium": "modely-2025-premium",
  "Model Y (2025+) Performance": "modely-2025-performance",
  "Model Y L": "modely-l",
}

type SortOption = "newest" | "oldest" | "popular" | "name"

interface CommunityWrap {
  code: string
  name: string
  tesla_model: string
  download_count: number
  created_at: string
  fingerprint?: string
  has_preview?: boolean
}

interface LibraryResponse {
  wraps: CommunityWrap[]
  total: number
  page: number
}

type Tab = "browse" | "upload"

export default function CommunityWraps() {
  const [tab, setTab] = useState<Tab>("browse")
  const [adminPasscode, setAdminPasscode] = useState<string | null>(null)
  const [showPasscodePrompt, setShowPasscodePrompt] = useState(false)
  const clickCountRef = useRef(0)
  const clickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Godot 3D engine state — mounted at page level so it starts loading immediately
  const godotReadyRef = useRef(false)
  const godotRef = useRef<GodotRendererHandle>(null)
  const carLoadedRef = useRef(false)

  const handleHeadingClick = () => {
    if (adminPasscode) {
      // Already in admin mode — 5 clicks exits
      clickCountRef.current++
      if (clickTimerRef.current) clearTimeout(clickTimerRef.current)
      clickTimerRef.current = setTimeout(() => { clickCountRef.current = 0 }, 2000)
      if (clickCountRef.current >= 5) {
        clickCountRef.current = 0
        setAdminPasscode(null)
      }
      return
    }

    clickCountRef.current++
    if (clickTimerRef.current) clearTimeout(clickTimerRef.current)
    clickTimerRef.current = setTimeout(() => { clickCountRef.current = 0 }, 2000)
    if (clickCountRef.current >= 5) {
      clickCountRef.current = 0
      setShowPasscodePrompt(true)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-500/20">
            <Paintbrush className="h-5 w-5 text-amber-400" />
          </div>
          <div>
            <h1
              className="cursor-default select-none text-xl font-semibold text-slate-100"
              onClick={handleHeadingClick}
            >
              Community Wraps
            </h1>
            <p className="text-xs text-slate-500">Browse and share Tesla wraps</p>
          </div>
        </div>
      </div>

      {/* Tab selector */}
      <div className="flex items-center gap-2">
        <button
          onClick={() => setTab("browse")}
          className={`rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
            tab === "browse"
              ? "bg-blue-500/15 text-blue-400"
              : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
          }`}
        >
          Browse
        </button>
        <button
          onClick={() => setTab("upload")}
          className={`rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
            tab === "upload"
              ? "bg-blue-500/15 text-blue-400"
              : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
          }`}
        >
          Upload
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

      {/* Hidden Godot renderer — starts loading 283MB .pck immediately */}
      <GodotRenderer
        ref={godotRef}
        onReady={() => { godotReadyRef.current = true }}
        onCapture={() => {}}
        onError={() => {}}
        onCarLoaded={() => { carLoadedRef.current = true }}
      />

      {tab === "browse" ? (
        <BrowseTab adminPasscode={adminPasscode} onAdminExit={() => setAdminPasscode(null)} />
      ) : (
        <UploadTab godotReadyRef={godotReadyRef} godotRef={godotRef} carLoadedRef={carLoadedRef} />
      )}

      {/* Passcode prompt modal */}
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

function PasscodeModal({ onSuccess, onClose }: { onSuccess: (passcode: string) => void; onClose: () => void }) {
  const [input, setInput] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)

  const handleValidate = async () => {
    if (!input.trim()) return
    setValidating(true)
    setError(null)
    try {
      const res = await fetch(`${API_BASE}/wraps/admin/validate`, {
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
          className="mt-4 w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-blue-500/50 focus:outline-none"
        />
        {error && (
          <p className="mt-2 text-xs text-red-400">{error}</p>
        )}
        <div className="mt-4 flex gap-3">
          <button
            onClick={handleValidate}
            disabled={!input.trim() || validating}
            className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-50"
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

function BrowseTab({ adminPasscode, onAdminExit }: { adminPasscode: string | null; onAdminExit: () => void }) {
  const [wraps, setWraps] = useState<CommunityWrap[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const [model, setModel] = useState("All")
  const [sort, setSort] = useState<SortOption>("newest")
  const [selectedWrap, setSelectedWrap] = useState<CommunityWrap | null>(null)
  const [downloading, setDownloading] = useState<string | null>(null)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)
  const [editingWrap, setEditingWrap] = useState<CommunityWrap | null>(null)
  const [deletingWrap, setDeletingWrap] = useState<CommunityWrap | null>(null)
  const limit = 20

  const fetchWraps = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams({ page: String(page), limit: String(limit) })
      if (model !== "All") params.set("model", model)
      if (search.trim()) params.set("search", search.trim())
      if (sort !== "newest") params.set("sort", sort)

      const headers: HeadersInit = {}
      if (adminPasscode) headers["x-passcode"] = adminPasscode

      const res = await fetch(`${API_BASE}/wraps/library?${params}`, { headers })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data: LibraryResponse = await res.json()
      setWraps(data.wraps || [])
      setTotal(data.total || 0)
    } catch (err: any) {
      setError(err.message || "Failed to load wraps")
    } finally {
      setLoading(false)
    }
  }, [page, model, search, sort, adminPasscode])

  useEffect(() => {
    const timer = setTimeout(fetchWraps, search ? 300 : 0)
    return () => clearTimeout(timer)
  }, [fetchWraps])

  useEffect(() => { setPage(1) }, [model, search, sort])

  const totalPages = Math.ceil(total / limit)

  const handleDownload = async (wrap: CommunityWrap) => {
    setDownloading(wrap.code)
    try {
      const res = await fetch(`${API_BASE}/wraps/download/${wrap.code}`, { method: "POST" })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `HTTP ${res.status}`)
      }
      setToast({ message: `"${wrap.name}" added to your Wraps folder!`, type: "success" })
      setSelectedWrap(null)
    } catch (err: any) {
      setToast({ message: err.message || "Download failed", type: "error" })
    } finally {
      setDownloading(null)
    }
  }

  const handleEdit = async (code: string, name: string, tesla_model: string) => {
    if (!adminPasscode) return
    try {
      const res = await fetch(`${API_BASE}/wraps/admin/edit/${code}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json", "x-passcode": adminPasscode },
        body: JSON.stringify({ name, tesla_model }),
      })
      if (res.status === 401) {
        onAdminExit()
        setToast({ message: "Passcode expired — admin mode deactivated", type: "error" })
        setEditingWrap(null)
        return
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `HTTP ${res.status}`)
      }
      setToast({ message: "Wrap updated", type: "success" })
      setEditingWrap(null)
      setSelectedWrap(null)
      fetchWraps()
    } catch (err: any) {
      setToast({ message: err.message || "Edit failed", type: "error" })
    }
  }

  const handleDelete = async (code: string) => {
    if (!adminPasscode) return
    try {
      const res = await fetch(`${API_BASE}/wraps/admin/delete/${code}`, {
        method: "DELETE",
        headers: { "x-passcode": adminPasscode },
      })
      if (res.status === 401) {
        onAdminExit()
        setToast({ message: "Passcode expired — admin mode deactivated", type: "error" })
        setDeletingWrap(null)
        return
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `HTTP ${res.status}`)
      }
      setToast({ message: "Wrap deleted", type: "success" })
      setDeletingWrap(null)
      setSelectedWrap(null)
      fetchWraps()
    } catch (err: any) {
      setToast({ message: err.message || "Delete failed", type: "error" })
    }
  }

  useEffect(() => {
    if (!toast) return
    const timer = setTimeout(() => setToast(null), 4000)
    return () => clearTimeout(timer)
  }, [toast])

  return (
    <>
      {/* Toast notification */}
      {toast && (
        <div className={`fixed right-4 top-4 z-50 flex items-center gap-2 rounded-lg px-4 py-3 text-sm font-medium shadow-lg ${
          toast.type === "success" ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30" : "bg-red-500/20 text-red-400 border border-red-500/30"
        }`}>
          {toast.type === "success" ? <CheckCircle className="h-4 w-4" /> : <AlertCircle className="h-4 w-4" />}
          {toast.message}
        </div>
      )}

      {/* Filters */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search wraps..."
            className="w-full rounded-lg border border-white/10 bg-white/[0.03] py-2 pl-10 pr-4 text-sm text-slate-200 placeholder:text-slate-600 focus:border-blue-500/50 focus:outline-none"
          />
        </div>
        <select
          value={model}
          onChange={(e) => setModel(e.target.value)}
          className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
        >
          {FILTER_MODELS.map((m) => (
            <option key={m} value={m} className="bg-slate-900">{m}</option>
          ))}
        </select>
        <select
          value={sort}
          onChange={(e) => setSort(e.target.value as SortOption)}
          className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
        >
          <option value="newest" className="bg-slate-900">Newest</option>
          <option value="oldest" className="bg-slate-900">Oldest</option>
          <option value="popular" className="bg-slate-900">Most Popular</option>
          <option value="name" className="bg-slate-900">Name (A-Z)</option>
        </select>
      </div>

      {/* Results */}
      {loading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="h-6 w-6 animate-spin text-blue-400" />
        </div>
      ) : error ? (
        <div className="flex flex-col items-center justify-center py-20 text-slate-500">
          <AlertCircle className="mb-2 h-8 w-8" />
          <p className="text-sm">{error}</p>
          <button onClick={fetchWraps} className="mt-3 text-xs text-blue-400 hover:text-blue-300">Retry</button>
        </div>
      ) : wraps.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-slate-500">
          <Paintbrush className="mb-2 h-8 w-8" />
          <p className="text-sm">No wraps found</p>
        </div>
      ) : (
        <>
          {/* Grid */}
          <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {wraps.map((wrap) => (
              <div
                key={wrap.code}
                className="group relative overflow-hidden rounded-lg border border-white/5 bg-white/[0.02] transition-colors hover:border-white/10 hover:bg-white/[0.04]"
              >
                <button
                  onClick={() => setSelectedWrap(wrap)}
                  className="w-full text-left"
                >
                  <div className="aspect-square overflow-hidden bg-slate-800/50">
                    <img
                      src={`${API_BASE}/wraps/${wrap.has_preview ? 'preview' : 'thumbnail'}/${wrap.code}`}
                      alt={wrap.name}
                      className="h-full w-full object-cover transition-transform group-hover:scale-105"
                      loading="lazy"
                    />
                  </div>
                  <div className="p-2">
                    <p className="truncate text-xs font-medium text-slate-200">{wrap.name}</p>
                    <div className="mt-1 flex items-center justify-between">
                      <span className="rounded bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium text-blue-400">
                        {wrap.tesla_model}
                      </span>
                      <span className="flex items-center gap-1 text-[10px] text-slate-600">
                        <Download className="h-3 w-3" />
                        {wrap.download_count}
                      </span>
                    </div>
                    {adminPasscode && wrap.fingerprint && (
                      <p className="mt-1 truncate font-mono text-[9px] text-slate-600">
                        {wrap.fingerprint.slice(0, 12)}...
                      </p>
                    )}
                  </div>
                </button>

                {/* Admin action icons */}
                {adminPasscode && (
                  <div className="absolute right-1 top-1 flex gap-1">
                    <button
                      onClick={(e) => { e.stopPropagation(); setEditingWrap(wrap) }}
                      className="rounded bg-black/60 p-1 text-blue-400 opacity-0 transition-opacity hover:bg-black/80 hover:text-blue-300 group-hover:opacity-100"
                      title="Edit"
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                    <button
                      onClick={(e) => { e.stopPropagation(); setDeletingWrap(wrap) }}
                      className="rounded bg-black/60 p-1 text-red-400 opacity-0 transition-opacity hover:bg-black/80 hover:text-red-300 group-hover:opacity-100"
                      title="Delete"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-center gap-3">
              <button
                onClick={() => setPage(Math.max(1, page - 1))}
                disabled={page === 1}
                className="rounded-lg border border-white/10 p-2 text-slate-400 transition-colors hover:bg-white/5 disabled:opacity-30"
              >
                <ChevronLeft className="h-4 w-4" />
              </button>
              <span className="text-sm text-slate-400">{page} / {totalPages}</span>
              <button
                onClick={() => setPage(Math.min(totalPages, page + 1))}
                disabled={page >= totalPages}
                className="rounded-lg border border-white/10 p-2 text-slate-400 transition-colors hover:bg-white/5 disabled:opacity-30"
              >
                <ChevronRight className="h-4 w-4" />
              </button>
            </div>
          )}
        </>
      )}

      {/* Detail modal */}
      {selectedWrap && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => setSelectedWrap(null)}>
          <div
            className="w-full max-w-md overflow-hidden rounded-2xl border border-white/10 bg-slate-900"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="aspect-square overflow-hidden bg-slate-800">
              <img
                src={`${API_BASE}/wraps/${selectedWrap.has_preview ? 'preview' : 'thumbnail'}/${selectedWrap.code}`}
                alt={selectedWrap.name}
                className="h-full w-full object-cover"
              />
            </div>
            <div className="p-5">
              <h3 className="text-lg font-semibold text-slate-100">{selectedWrap.name}</h3>
              <div className="mt-2 flex items-center gap-3">
                <span className="rounded bg-blue-500/15 px-2 py-1 text-xs font-medium text-blue-400">
                  {selectedWrap.tesla_model}
                </span>
                <span className="flex items-center gap-1 text-xs text-slate-500">
                  <Download className="h-3 w-3" />
                  {selectedWrap.download_count} downloads
                </span>
              </div>
              {adminPasscode && selectedWrap.fingerprint && (
                <p className="mt-2 break-all font-mono text-[10px] text-slate-600">
                  Fingerprint: {selectedWrap.fingerprint}
                </p>
              )}
              <div className="mt-4 flex gap-3">
                <button
                  onClick={() => handleDownload(selectedWrap)}
                  disabled={downloading === selectedWrap.code}
                  className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-50"
                >
                  {downloading === selectedWrap.code ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Download className="h-4 w-4" />
                  )}
                  Download to Pi
                </button>
                {adminPasscode && (
                  <>
                    <button
                      onClick={() => setEditingWrap(selectedWrap)}
                      className="rounded-lg border border-blue-500/30 px-3 py-2.5 text-blue-400 transition-colors hover:bg-blue-500/10"
                      title="Edit"
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => setDeletingWrap(selectedWrap)}
                      className="rounded-lg border border-red-500/30 px-3 py-2.5 text-red-400 transition-colors hover:bg-red-500/10"
                      title="Delete"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </>
                )}
                <button
                  onClick={() => setSelectedWrap(null)}
                  className="rounded-lg border border-white/10 px-4 py-2.5 text-sm text-slate-400 transition-colors hover:bg-white/5"
                >
                  Close
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Edit modal */}
      {editingWrap && (
        <EditWrapModal
          wrap={editingWrap}
          onSave={handleEdit}
          onClose={() => setEditingWrap(null)}
        />
      )}

      {/* Delete confirmation modal */}
      {deletingWrap && (
        <DeleteWrapModal
          wrap={deletingWrap}
          onDelete={handleDelete}
          onClose={() => setDeletingWrap(null)}
        />
      )}
    </>
  )
}

function EditWrapModal({ wrap, onSave, onClose }: {
  wrap: CommunityWrap
  onSave: (code: string, name: string, model: string) => Promise<void>
  onClose: () => void
}) {
  const [name, setName] = useState(wrap.name)
  const [model, setModel] = useState(wrap.tesla_model)
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    if (!name.trim() || !model) return
    setSaving(true)
    await onSave(wrap.code, name.trim(), model)
    setSaving(false)
  }

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="w-full max-w-sm overflow-hidden rounded-2xl border border-white/10 bg-slate-900 p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="text-lg font-semibold text-slate-100">Edit Wrap</h3>
        <p className="mt-1 font-mono text-[10px] text-slate-600">{wrap.code}</p>

        <div className="mt-4 space-y-4">
          <div>
            <label className="mb-1.5 block text-sm font-medium text-slate-300">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value.slice(0, 50))}
              className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
            />
          </div>
          <div>
            <label className="mb-1.5 block text-sm font-medium text-slate-300">Tesla Model</label>
            <select
              value={model}
              onChange={(e) => setModel(e.target.value)}
              className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
            >
              {TESLA_MODELS.map((m) => (
                <option key={m} value={m} className="bg-slate-900">{m}</option>
              ))}
            </select>
          </div>
        </div>

        <div className="mt-5 flex gap-3">
          <button
            onClick={handleSave}
            disabled={!name.trim() || !model || saving}
            className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-50"
          >
            {saving && <Loader2 className="h-4 w-4 animate-spin" />}
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

function DeleteWrapModal({ wrap, onDelete, onClose }: {
  wrap: CommunityWrap
  onDelete: (code: string) => Promise<void>
  onClose: () => void
}) {
  const [deleting, setDeleting] = useState(false)

  const handleDelete = async () => {
    setDeleting(true)
    await onDelete(wrap.code)
    setDeleting(false)
  }

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="w-full max-w-sm overflow-hidden rounded-2xl border border-white/10 bg-slate-900 p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="text-lg font-semibold text-red-400">Delete Wrap</h3>
        <p className="mt-2 text-sm text-slate-400">
          Permanently delete <span className="font-medium text-slate-200">"{wrap.name}"</span>? This cannot be undone.
        </p>
        <div className="mt-5 flex gap-3">
          <button
            onClick={handleDelete}
            disabled={deleting}
            className="flex flex-1 items-center justify-center gap-2 rounded-lg bg-red-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-red-500 disabled:opacity-50"
          >
            {deleting && <Loader2 className="h-4 w-4 animate-spin" />}
            Delete
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

interface UploadTabProps {
  godotReadyRef: React.MutableRefObject<boolean>
  godotRef: React.RefObject<GodotRendererHandle | null>
  carLoadedRef: React.MutableRefObject<boolean>
}

function UploadTab({ godotReadyRef, godotRef, carLoadedRef }: UploadTabProps) {
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<string | null>(null)
  const [name, setName] = useState("")
  const [model, setModel] = useState("")
  const [uploading, setUploading] = useState(false)
  const [uploadStatus, setUploadStatus] = useState<string | null>(null)
  const [result, setResult] = useState<{ success: boolean; message: string } | null>(null)

  // Wait for Godot engine to finish loading (polls the ref)
  const waitForGodotReady = useCallback((timeoutMs: number): Promise<boolean> => {
    return new Promise((resolve) => {
      if (godotReadyRef.current) { resolve(true); return }
      const start = Date.now()
      const check = setInterval(() => {
        if (godotReadyRef.current) { clearInterval(check); resolve(true) }
        else if (Date.now() - start > timeoutMs) { clearInterval(check); resolve(false) }
      }, 500)
    })
  }, [godotReadyRef])

  // Generate a 3D preview by sending commands to Godot and capturing the result
  const generate3DPreview = useCallback((imageFile: File, godotId: string): Promise<string> => {
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        window.removeEventListener("message", handler)
        reject(new Error("Preview capture timeout"))
      }, 15000)

      const handler = (e: MessageEvent) => {
        if (e.data?.type === "capture_result" && e.data.dataUrl) {
          clearTimeout(timeout)
          window.removeEventListener("message", handler)
          resolve(e.data.dataUrl)
        } else if (e.data?.type === "capture_error") {
          clearTimeout(timeout)
          window.removeEventListener("message", handler)
          reject(new Error(e.data.error || "Capture failed"))
        }
      }
      window.addEventListener("message", handler)

      // Read the file as data URL, then send to Godot
      const reader = new FileReader()
      reader.onload = () => {
        const dataUrl = reader.result as string
        carLoadedRef.current = false

        // Skip loadScene if model is already the default (modely) — saves time
        const isDefaultModel = godotId === "modely"
        if (!isDefaultModel) {
          godotRef.current?.loadScene(godotId)
        }

        // Wait for car model to load (or timeout), then apply texture and capture
        const timeout = isDefaultModel ? 1000 : 8000
        const start = Date.now()
        const check = setInterval(() => {
          if (carLoadedRef.current || Date.now() - start > timeout) {
            clearInterval(check)
            godotRef.current?.setTexture(dataUrl)
            // Extra wait for non-default models to fully settle after scene switch
            const captureDelay = isDefaultModel ? 1000 : 2500
            setTimeout(() => godotRef.current?.capture(), captureDelay)
          }
        }, 200)
      }
      reader.readAsDataURL(imageFile)
    })
  }, [godotRef, carLoadedRef])

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (!f) return

    if (f.type !== "image/png") {
      setResult({ success: false, message: "Only PNG files are supported." })
      return
    }
    if (f.size > 1024 * 1024) {
      setResult({ success: false, message: "File must be under 1 MB." })
      return
    }

    setFile(f)
    setResult(null)
    const url = URL.createObjectURL(f)
    setPreview(url)
  }

  const handleSubmit = async () => {
    if (!file || !name.trim() || !model) return

    setUploading(true)
    setResult(null)

    try {
      let previewDataUrl: string | null = null
      const godotId = MODEL_TO_GODOT_ID[model]

      // Generate 3D preview if model has a Godot counterpart
      if (godotId && godotRef.current) {
        // Wait for Godot to be ready (may still be downloading the 283MB .pck)
        if (!godotReadyRef.current) {
          setUploadStatus("Loading 3D engine...")
          const ready = await waitForGodotReady(60000)
          if (!ready) {
            // Godot didn't load in time — continue without preview
            setUploadStatus("Uploading...")
          }
        }

        if (godotReadyRef.current) {
          setUploadStatus("Generating 3D preview...")
          try {
            previewDataUrl = await generate3DPreview(file, godotId)
          } catch (previewErr) {
            console.warn("[WRAPS] 3D preview generation failed:", previewErr)
          }
        }
      }

      setUploadStatus("Uploading...")

      const formData = new FormData()
      formData.append("image", file)
      formData.append("name", name.trim())
      formData.append("tesla_model", model)

      if (previewDataUrl) {
        const previewBlob = await (await fetch(previewDataUrl)).blob()
        formData.append("preview", previewBlob, "preview.png")
      }

      const res = await fetch(`${API_BASE}/wraps/upload`, {
        method: "POST",
        body: formData,
      })

      const data = await res.json()

      if (!res.ok) {
        throw new Error(data.error || `HTTP ${res.status}`)
      }

      setResult({ success: true, message: data.message || "Wrap submitted! It will appear in the library once reviewed." })
      setFile(null)
      setPreview(null)
      setName("")
      setModel("")
    } catch (err: any) {
      setResult({ success: false, message: err.message || "Upload failed" })
    } finally {
      setUploading(false)
      setUploadStatus(null)
    }
  }

  return (
    <div className="mx-auto max-w-lg space-y-5">
      {/* File picker */}
      <div>
        <label className="mb-1.5 block text-sm font-medium text-slate-300">Wrap Image</label>
        <div className="relative">
          <input
            type="file"
            accept=".png,image/png"
            onChange={handleFileChange}
            className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 file:mr-3 file:rounded file:border-0 file:bg-blue-500/20 file:px-3 file:py-1 file:text-xs file:font-medium file:text-blue-400"
          />
        </div>
        <p className="mt-1 text-xs text-slate-600">PNG, 512x512 to 1024x1024, max 1 MB</p>
      </div>

      {/* Flat preview */}
      {preview && (
        <div className="overflow-hidden rounded-xl border border-white/10">
          <img src={preview} alt="Preview" className="h-48 w-full object-contain bg-slate-800/50" />
        </div>
      )}

      {/* Name */}
      <div>
        <label className="mb-1.5 block text-sm font-medium text-slate-300">Wrap Name</label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value.slice(0, 50))}
          placeholder="e.g. Red Carbon Fiber"
          className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-blue-500/50 focus:outline-none"
        />
        <p className="mt-1 text-xs text-slate-600">{name.length}/50 characters. Letters, numbers, spaces, hyphens only.</p>
      </div>

      {/* Tesla Model */}
      <div>
        <label className="mb-1.5 block text-sm font-medium text-slate-300">Tesla Model</label>
        <select
          value={model}
          onChange={(e) => setModel(e.target.value)}
          className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
        >
          <option value="" className="bg-slate-900">Select model...</option>
          {TESLA_MODELS.map((m) => (
            <option key={m} value={m} className="bg-slate-900">{m}</option>
          ))}
        </select>
      </div>

      {/* Submit */}
      <button
        onClick={handleSubmit}
        disabled={!file || !name.trim() || !model || uploading}
        className="flex w-full items-center justify-center gap-2 rounded-lg bg-blue-600 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed"
      >
        {uploading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <Upload className="h-4 w-4" />
        )}
        {uploading ? (uploadStatus || "Uploading...") : "Submit Wrap"}
      </button>

      {/* Result message */}
      {result && (
        <div className={`flex items-center gap-2 rounded-lg px-4 py-3 text-sm ${
          result.success
            ? "bg-emerald-500/10 text-emerald-400 border border-emerald-500/20"
            : "bg-red-500/10 text-red-400 border border-red-500/20"
        }`}>
          {result.success ? <CheckCircle className="h-4 w-4 shrink-0" /> : <AlertCircle className="h-4 w-4 shrink-0" />}
          {result.message}
        </div>
      )}
    </div>
  )
}
