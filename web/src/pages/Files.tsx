import { useState, useEffect, useRef, useCallback } from "react"
import {
  FolderOpen,
  Upload,
  Download,
  FolderPlus,
  Trash2,
  File,
  Folder,
  ArrowLeft,
  Loader2,
  Music,
  Video,
  Paintbrush,
  RectangleHorizontal,
  CheckCircle,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"

interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  modified: string
}

interface DriveTab {
  id: string
  base: string
  icon: "cam" | "media" | "wrap" | "plate"
}

const ALL_DRIVES: DriveTab[] = [
  { id: "TeslaCam", base: "/mutable/TeslaCam", icon: "cam" },
  { id: "Wraps", base: "/mutable/Wraps", icon: "wrap" },
  { id: "License Plates", base: "/mutable/LicensePlate", icon: "plate" },
  { id: "Music", base: "/var/www/html/fs/Music", icon: "media" },
  { id: "LightShow", base: "/var/www/html/fs/LightShow", icon: "media" },
  { id: "Boombox", base: "/var/www/html/fs/Boombox", icon: "media" },
]

const TAB_ICONS: Record<DriveTab["icon"], React.ComponentType<{ className?: string }>> = {
  cam: Video,
  media: Music,
  wrap: Paintbrush,
  plate: RectangleHorizontal,
}

function formatSize(bytes: number): string {
  if (bytes === 0) return "—"
  const units = ["B", "KB", "MB", "GB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

interface UploadProgress {
  fileName: string
  loaded: number
  total: number
  done: boolean
  error: boolean
}

export default function Files() {
  const [drives, setDrives] = useState<DriveTab[]>([])
  const [activeDrive, setActiveDrive] = useState<DriveTab | null>(null)
  const [currentPath, setCurrentPath] = useState("")
  const [files, setFiles] = useState<FileEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const uploadRef = useRef<HTMLInputElement>(null)
  const [uploads, setUploads] = useState<UploadProgress[]>([])
  const [uploading, setUploading] = useState(false)
  const [effectiveBase, setEffectiveBase] = useState("")

  // Fetch config to determine which tabs to show
  useEffect(() => {
    async function loadConfig() {
      try {
        const res = await fetch("/api/config")
        const cfg = await res.json()
        const visible: DriveTab[] = []
        // Show TeslaCam tab if cam is configured
        if (cfg.has_cam === "yes") {
          visible.push(ALL_DRIVES.find(d => d.id === "TeslaCam")!)
        }
        // Always show Wraps and License Plates (they're user-uploadable)
        visible.push(ALL_DRIVES.find(d => d.id === "Wraps")!)
        visible.push(ALL_DRIVES.find(d => d.id === "License Plates")!)
        if (cfg.has_music === "yes") visible.push(ALL_DRIVES.find(d => d.id === "Music")!)
        if (cfg.has_lightshow === "yes") visible.push(ALL_DRIVES.find(d => d.id === "LightShow")!)
        if (cfg.has_boombox === "yes") visible.push(ALL_DRIVES.find(d => d.id === "Boombox")!)
        // If nothing is configured (e.g. dev mode), show all
        const result = visible.length > 0 ? visible : ALL_DRIVES
        setDrives(result)
        setActiveDrive(result[0])
        setCurrentPath(result[0].base)
      } catch {
        // Fallback: show all
        setDrives(ALL_DRIVES)
        setActiveDrive(ALL_DRIVES[0])
        setCurrentPath(ALL_DRIVES[0].base)
      }
    }
    loadConfig()
  }, [])

  async function fetchFiles(path: string) {
    setLoading(true)
    setError(null)
    setSelected(new Set())
    try {
      const res = await fetch(`/api/files/ls?path=${encodeURIComponent(path)}`)
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: "Failed to load" }))
        setError(data.error || "Failed to load directory")
        setFiles([])
      } else {
        const raw = await res.json()
        // Server returns { path, entries: [...] } wrapper
        const data: FileEntry[] = Array.isArray(raw) ? raw : (raw.entries ?? [])
        // Auto-navigate into the matching subfolder when at a drive's base path
        // (Music/LightShow/Boombox disk images have a root folder matching the
        // drive name, possibly alongside hidden macOS/Tesla metadata folders)
        if (activeDrive && path === activeDrive.base) {
          const match = data.find(
            (e) => e.is_dir && e.name === activeDrive.id
          )
          if (match) {
            setEffectiveBase(match.path)
            setCurrentPath(match.path)
            return
          }
        }
        setFiles(data)
      }
    } catch {
      setError("Unable to connect")
      setFiles([])
    }
    setLoading(false)
  }

  useEffect(() => {
    if (currentPath) fetchFiles(currentPath)
  }, [currentPath])

  function navigate(entry: FileEntry) {
    if (entry.is_dir) {
      setCurrentPath(entry.path)
    }
  }

  function goUp() {
    const base = effectiveBase || activeDrive?.base
    if (!activeDrive || !base || currentPath === base) return
    const parent = currentPath.split("/").slice(0, -1).join("/")
    if (parent.length < base.length) return
    setCurrentPath(parent || base)
  }

  function switchDrive(drive: DriveTab) {
    setActiveDrive(drive)
    setEffectiveBase("")
    setCurrentPath(drive.base)
  }

  async function handleDelete() {
    if (selected.size === 0) return
    if (!confirm(`Delete ${selected.size} item(s)?`)) return
    for (const path of selected) {
      await fetch(`/api/files?path=${encodeURIComponent(path)}`, { method: "DELETE" })
    }
    fetchFiles(currentPath)
  }

  function uploadFileWithProgress(file: globalThis.File, destPath: string, index: number): Promise<void> {
    return new Promise((resolve) => {
      const form = new FormData()
      form.append("file", file)
      form.append("path", destPath)

      const xhr = new XMLHttpRequest()
      xhr.open("POST", "/api/files/upload")

      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) {
          setUploads((prev) => prev.map((u, i) => i === index ? { ...u, loaded: e.loaded, total: e.total } : u))
        }
      }

      xhr.onload = () => {
        setUploads((prev) => prev.map((u, i) => i === index ? { ...u, done: true, loaded: u.total, error: xhr.status >= 400 } : u))
        resolve()
      }

      xhr.onerror = () => {
        setUploads((prev) => prev.map((u, i) => i === index ? { ...u, done: true, error: true } : u))
        resolve()
      }

      xhr.send(form)
    })
  }

  const handleUpload = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const fileList = e.target.files
    if (!fileList || fileList.length === 0) return

    const fileArr = Array.from(fileList)
    const initial: UploadProgress[] = fileArr.map((f) => ({
      fileName: f.name,
      loaded: 0,
      total: f.size,
      done: false,
      error: false,
    }))

    setUploads(initial)
    setUploading(true)

    // Upload all files in parallel
    await Promise.all(fileArr.map((f, i) => uploadFileWithProgress(f, currentPath, i)))

    // All uploads finished — auto-refresh
    fetchFiles(currentPath)
    if (uploadRef.current) uploadRef.current.value = ""

    // Keep progress visible briefly, then clear
    setTimeout(() => {
      setUploads([])
      setUploading(false)
    }, 2000)
  }, [currentPath])

  async function handleNewFolder() {
    const name = prompt("Folder name:")
    if (!name) return
    await fetch("/api/files/mkdir", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path: `${currentPath}/${name}` }),
    })
    fetchFiles(currentPath)
  }

  if (!activeDrive) {
    return (
      <div className="flex items-center justify-center p-8">
        <Loader2 className="h-5 w-5 animate-spin text-slate-500" />
      </div>
    )
  }

  const base = effectiveBase || activeDrive.base
  const relativePath = currentPath.replace(base, "") || "/"

  return (
    <div className="flex h-[calc(100vh-120px)] flex-col space-y-4 md:h-[calc(100vh-96px)]">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Files</h1>
          <p className="mt-1 text-sm text-slate-500">
            Manage dashcam clips and media files
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            onClick={handleNewFolder}
            className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200"
          >
            <FolderPlus className="h-4 w-4" />
            New Folder
          </button>
          <button
            onClick={() => uploadRef.current?.click()}
            disabled={uploading}
            className={cn(
              "glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors",
              uploading ? "text-slate-600 cursor-not-allowed" : "text-slate-400 hover:text-slate-200"
            )}
          >
            {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
            {uploading ? "Uploading..." : "Upload"}
          </button>
          <input ref={uploadRef} type="file" multiple className="hidden" onChange={handleUpload} />
        </div>
      </div>

      {/* Drive selector */}
      <div className="flex flex-wrap gap-1">
        {drives.map((drive) => (
          <button
            key={drive.id}
            onClick={() => switchDrive(drive)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
              activeDrive.id === drive.id
                ? "bg-blue-500/15 text-blue-400"
                : "text-slate-500 hover:bg-white/5 hover:text-slate-300"
            )}
          >
            {(() => { const Icon = TAB_ICONS[drive.icon]; return <Icon className="h-3.5 w-3.5" /> })()}
            {drive.id}
          </button>
        ))}
      </div>

      {/* Upload progress */}
      {uploads.length > 0 && (
        <div className="glass-card space-y-2 p-3">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-slate-300">
              {uploading ? "Uploading files..." : (
                <span className="flex items-center gap-1.5">
                  <CheckCircle className="h-3.5 w-3.5 text-emerald-400" />
                  Upload complete
                </span>
              )}
            </p>
            {!uploading && (
              <button onClick={() => setUploads([])} className="rounded p-0.5 text-slate-600 hover:text-slate-400">
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
          {uploads.map((u, i) => {
            const pct = u.total > 0 ? Math.round((u.loaded / u.total) * 100) : 0
            return (
              <div key={i} className="space-y-1">
                <div className="flex items-center justify-between text-[11px]">
                  <span className="truncate text-slate-400">{u.fileName}</span>
                  <span className={cn("tabular-nums", u.error ? "text-red-400" : u.done ? "text-emerald-400" : "text-slate-500")}>
                    {u.error ? "Error" : u.done ? "Done" : `${pct}%`}
                  </span>
                </div>
                <div className="h-1 overflow-hidden rounded-full bg-slate-800">
                  <div
                    className={cn(
                      "h-full rounded-full transition-all duration-300",
                      u.error ? "bg-red-500" : u.done ? "bg-emerald-500" : "bg-blue-500"
                    )}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </div>
            )
          })}
          {uploading && uploads.length > 1 && (() => {
            const totalLoaded = uploads.reduce((s, u) => s + u.loaded, 0)
            const totalSize = uploads.reduce((s, u) => s + u.total, 0)
            const totalPct = totalSize > 0 ? Math.round((totalLoaded / totalSize) * 100) : 0
            const doneCount = uploads.filter((u) => u.done).length
            return (
              <div className="border-t border-white/5 pt-2">
                <div className="flex items-center justify-between text-[11px]">
                  <span className="text-slate-500">{doneCount}/{uploads.length} files</span>
                  <span className="tabular-nums text-slate-400">{formatSize(totalLoaded)} / {formatSize(totalSize)} ({totalPct}%)</span>
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-slate-800">
                  <div className="h-full rounded-full bg-blue-500 transition-all duration-300" style={{ width: `${totalPct}%` }} />
                </div>
              </div>
            )
          })()}
        </div>
      )}

      {/* File list */}
      <div className="glass-card flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="flex items-center justify-between border-b border-white/5 px-3 py-2">
          <div className="flex items-center gap-2">
            {currentPath !== base && (
              <button
                onClick={goUp}
                className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-slate-300"
              >
                <ArrowLeft className="h-4 w-4" />
              </button>
            )}
            <p className="font-mono text-sm text-slate-400">{relativePath}</p>
          </div>
          <div className="flex items-center gap-1">
            {selected.size > 0 && (
              <>
                <span className="mr-1 rounded-full bg-blue-500/20 px-2 py-0.5 text-[10px] font-semibold text-blue-400">
                  {selected.size} selected
                </span>
                <button
                  onClick={() => {
                    for (const p of selected) {
                      const a = document.createElement("a")
                      a.href = `/api/files/download?path=${encodeURIComponent(p)}`
                      a.download = ""
                      a.click()
                    }
                  }}
                  className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-blue-400"
                  title="Download selected"
                >
                  <Download className="h-4 w-4" />
                </button>
                <button
                  onClick={handleDelete}
                  className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-red-400"
                  title="Delete selected"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </>
            )}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center p-8">
              <Loader2 className="h-5 w-5 animate-spin text-slate-500" />
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center p-8">
              <FolderOpen className="mb-2 h-10 w-10 text-slate-700" />
              <p className="text-sm text-slate-500">{error}</p>
            </div>
          ) : files.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-8">
              {(() => { const Icon = TAB_ICONS[activeDrive.icon]; return <Icon className="mb-2 h-10 w-10 text-slate-700" /> })()}
              <p className="text-sm text-slate-500">Empty folder</p>
              <p className="mt-1 text-xs text-slate-600">
                {activeDrive.icon === "cam" ? "No clips in this folder" : "Upload files to get started"}
              </p>
            </div>
          ) : (
            <table className="w-full text-sm">
              <tbody>
                {files.map((f) => (
                  <tr
                    key={f.path}
                    className={cn(
                      "cursor-pointer border-b border-white/5 transition-colors hover:bg-white/5",
                      selected.has(f.path) && "bg-blue-500/10"
                    )}
                    onClick={() => {
                      if (f.is_dir) {
                        navigate(f)
                      } else {
                        setSelected((prev) => {
                          const next = new Set(prev)
                          if (next.has(f.path)) next.delete(f.path)
                          else next.add(f.path)
                          return next
                        })
                      }
                    }}
                  >
                    <td className="px-3 py-3">
                      {f.is_dir ? (
                        <Folder className="h-4 w-4 text-blue-400" />
                      ) : (
                        <File className="h-4 w-4 text-slate-500" />
                      )}
                    </td>
                    <td className="min-w-0 truncate py-3 text-slate-300">{f.name}</td>
                    <td className="px-3 py-3 text-right text-xs text-slate-600">
                      {f.is_dir ? "" : formatSize(f.size)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}
