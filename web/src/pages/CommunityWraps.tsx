import { useEffect, useState, useCallback } from "react"
import { Search, Upload, Download, Paintbrush, ChevronLeft, ChevronRight, Loader2, CheckCircle, AlertCircle } from "lucide-react"

const API_BASE = "/api"

// Base models for browse filter (uses LIKE prefix matching on server)
const FILTER_MODELS = ["All", "Cybertruck", "Model 3", "Model S", "Model X", "Model Y"]

// Specific models for upload
const UPLOAD_MODELS = [
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

type SortOption = "newest" | "oldest" | "popular" | "name"

interface CommunityWrap {
  code: string
  name: string
  tesla_model: string
  download_count: number
  created_at: string
}

interface LibraryResponse {
  wraps: CommunityWrap[]
  total: number
  page: number
}

type Tab = "browse" | "upload"

export default function CommunityWraps() {
  const [tab, setTab] = useState<Tab>("browse")

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-500/20">
            <Paintbrush className="h-5 w-5 text-amber-400" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-slate-100">Community Wraps</h1>
            <p className="text-xs text-slate-500">Browse and share Tesla wraps</p>
          </div>
        </div>
      </div>

      {/* Tab selector */}
      <div className="flex gap-2">
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
      </div>

      {tab === "browse" ? <BrowseTab /> : <UploadTab />}
    </div>
  )
}

function BrowseTab() {
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
  const limit = 20

  const fetchWraps = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams({ page: String(page), limit: String(limit) })
      if (model !== "All") params.set("model", model)
      if (search.trim()) params.set("search", search.trim())
      if (sort !== "newest") params.set("sort", sort)

      const res = await fetch(`${API_BASE}/wraps/library?${params}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data: LibraryResponse = await res.json()
      setWraps(data.wraps || [])
      setTotal(data.total || 0)
    } catch (err: any) {
      setError(err.message || "Failed to load wraps")
    } finally {
      setLoading(false)
    }
  }, [page, model, search, sort])

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
              <button
                key={wrap.code}
                onClick={() => setSelectedWrap(wrap)}
                className="group overflow-hidden rounded-lg border border-white/5 bg-white/[0.02] transition-colors hover:border-white/10 hover:bg-white/[0.04]"
              >
                <div className="aspect-square overflow-hidden bg-slate-800/50">
                  <img
                    src={`${API_BASE}/wraps/thumbnail/${wrap.code}`}
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
                </div>
              </button>
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
                src={`${API_BASE}/wraps/thumbnail/${selectedWrap.code}`}
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
    </>
  )
}

function UploadTab() {
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<string | null>(null)
  const [name, setName] = useState("")
  const [model, setModel] = useState("")
  const [uploading, setUploading] = useState(false)
  const [result, setResult] = useState<{ success: boolean; message: string } | null>(null)

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
      const formData = new FormData()
      formData.append("image", file)
      formData.append("name", name.trim())
      formData.append("tesla_model", model)

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

      {/* Preview */}
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
          {UPLOAD_MODELS.map((m) => (
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
        {uploading ? "Uploading..." : "Submit Wrap"}
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
