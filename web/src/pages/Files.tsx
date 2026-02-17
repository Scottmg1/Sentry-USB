import { FolderOpen, Upload, FolderPlus, Download, Trash2 } from "lucide-react"

export default function Files() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Files</h1>
          <p className="mt-1 text-sm text-slate-500">
            Manage Music, LightShow, and Boombox files
          </p>
        </div>
        <div className="flex gap-2">
          <button className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200">
            <FolderPlus className="h-4 w-4" />
            New Folder
          </button>
          <button className="glass-card glass-card-hover flex items-center gap-1.5 px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200">
            <Upload className="h-4 w-4" />
            Upload
          </button>
        </div>
      </div>

      {/* Drive selector */}
      <div className="flex gap-2">
        {["Music", "LightShow", "Boombox"].map((drive) => (
          <button
            key={drive}
            className="glass-card glass-card-hover px-3 py-1.5 text-sm text-slate-400 transition-colors hover:text-slate-200"
          >
            {drive}
          </button>
        ))}
      </div>

      {/* File browser layout */}
      <div className="flex gap-4" style={{ height: "calc(100vh - 280px)" }}>
        {/* Tree panel */}
        <div className="glass-card w-56 shrink-0 overflow-y-auto p-3">
          <p className="mb-2 text-xs font-medium uppercase tracking-wider text-slate-500">
            Folders
          </p>
          <div className="space-y-1">
            <div className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-slate-300 hover:bg-white/5">
              <FolderOpen className="h-4 w-4 text-blue-400" />
              <span>/</span>
            </div>
          </div>
        </div>

        {/* File list panel */}
        <div className="glass-card flex flex-1 flex-col overflow-hidden p-3">
          <div className="mb-3 flex items-center justify-between">
            <p className="text-sm text-slate-400">/</p>
            <div className="flex gap-1">
              <button className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-slate-300">
                <Download className="h-4 w-4" />
              </button>
              <button className="rounded p-1 text-slate-500 hover:bg-white/5 hover:text-red-400">
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </div>
          <div className="flex flex-1 items-center justify-center">
            <div className="text-center">
              <FolderOpen className="mx-auto mb-3 h-12 w-12 text-slate-700" />
              <p className="text-sm text-slate-500">
                Connect to SentryUSB to browse files
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
