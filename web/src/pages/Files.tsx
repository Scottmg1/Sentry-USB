import { useState, useEffect, useRef } from "react"
import {
  FolderOpen,
  Upload,
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
  { id: "Wraps", base: "/var/www/html/fs/Wraps", icon: "wrap" },
  { id: "License Plates", base: "/var/www/html/fs/LicensePlate", icon: "plate" },
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

export default function Files() {
  const [drives, setDrives] = useState<DriveTab[]>([])
  const [activeDrive, setActiveDrive] = useState<DriveTab | null>(null)
  const [currentPath, setCurrentPath] = useState("")
  const [files, setFiles] = useState<FileEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const uploadRef = useRef<HTMLInputElement>(null)

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
        const data = await res.json()
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
    if (!activeDrive || currentPath === activeDrive.base) return
    const parent = currentPath.split("/").slice(0, -1).join("/")
    setCurrentPath(parent || activeDrive.base)
  }

  function switchDrive(drive: DriveTab) {
    setActiveDrive(drive)
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

  async function handleUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const fileList = e.target.files
    if (!fileList) return
    for (const file of Array.from(fileList)) {
      const form = new FormData()
      form.append("file", file)
      form.append("path", currentPath)
      await fetch("/api/files/upload", { method: "POST", body: form })
    }
    fetchFiles(currentPath)
    if (uploadRef.current) uploadRef.current.value = ""
  }

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

  const relativePath = currentPath.replace(activeDrive.base, "") || "/"

  return (
    <div className="flex h-[calc(100vh-120px)] flex-col space-y-4 md:h-[calc(100vh-96px)]">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Files</h1>
          <p className="mt-1 text-sm text-slate-500">
            Manage dashcam clips and media files
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={handleNewFolder}
            className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200"
          >
            <FolderPlus className="h-4 w-4" />
            New Folder
          </button>
          <button
            onClick={() => uploadRef.current?.click()}
            className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200"
          >
            <Upload className="h-4 w-4" />
            Upload
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

      {/* File list */}
      <div className="glass-card flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="flex items-center justify-between border-b border-white/5 px-3 py-2">
          <div className="flex items-center gap-2">
            {currentPath !== activeDrive.base && (
              <button
                onClick={goUp}
                className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-slate-300"
              >
                <ArrowLeft className="h-4 w-4" />
              </button>
            )}
            <p className="font-mono text-sm text-slate-400">{relativePath}</p>
          </div>
          <div className="flex gap-1">
            {selected.size > 0 && (
              <button
                onClick={handleDelete}
                className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-red-400"
              >
                <Trash2 className="h-4 w-4" />
              </button>
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
                    <td className="px-3 py-2">
                      {f.is_dir ? (
                        <Folder className="h-4 w-4 text-blue-400" />
                      ) : (
                        <File className="h-4 w-4 text-slate-500" />
                      )}
                    </td>
                    <td className="py-2 text-slate-300">{f.name}</td>
                    <td className="px-3 py-2 text-right text-xs text-slate-600">
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
