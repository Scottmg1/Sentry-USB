import { useState, useRef, useCallback } from "react"
import { Upload, X, CheckCircle, Loader2, AlertCircle, Plus } from "lucide-react"

export interface FileEntry {
  id: string
  file: File
  name: string
  fields: Record<string, string>
  status: "pending" | "uploading" | "done" | "error"
  error?: string
}

export interface MultiFileUploaderProps {
  accept: string
  maxFiles: number
  rateLimitText: string
  accentColor: "blue" | "violet"
  validateFile: (file: File) => Promise<{ ok: boolean; error?: string }>
  renderPreview: (file: File) => React.ReactNode
  renderFields: (
    entry: FileEntry,
    onChange: (updates: Partial<Pick<FileEntry, "name" | "fields">>) => void
  ) => React.ReactNode
  isEntryReady: (entry: FileEntry) => boolean
  onUpload: (
    entry: FileEntry,
    onStep: (step: string) => void
  ) => Promise<{ success: boolean; message: string }>
}

export default function MultiFileUploader({
  accept,
  maxFiles,
  rateLimitText,
  accentColor,
  validateFile,
  renderPreview,
  renderFields,
  isEntryReady,
  onUpload,
}: MultiFileUploaderProps) {
  const [files, setFiles] = useState<FileEntry[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const [uploadingAll, setUploadingAll] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<{ current: number; total: number } | null>(null)
  const [currentStep, setCurrentStep] = useState<string | null>(null)
  const [errors, setErrors] = useState<string[]>([])
  const inputRef = useRef<HTMLInputElement>(null)

  const addFiles = useCallback(async (incoming: File[]) => {
    setErrors([])
    const currentCount = files.length
    const available = maxFiles - currentCount
    if (available <= 0) {
      setErrors([`Maximum ${maxFiles} files allowed`])
      return
    }
    const toProcess = incoming.slice(0, available)
    if (incoming.length > available) {
      setErrors([`Only ${available} more file(s) can be added (limit: ${maxFiles})`])
    }

    const validated: FileEntry[] = []
    const validationErrors: string[] = []

    for (const f of toProcess) {
      const result = await validateFile(f)
      if (result.ok) {
        validated.push({
          id: crypto.randomUUID(),
          file: f,
          name: f.name.replace(/\.[^/.]+$/, ""),
          fields: {},
          status: "pending",
        })
      } else {
        validationErrors.push(`${f.name}: ${result.error}`)
      }
    }

    if (validationErrors.length > 0) {
      setErrors((prev) => [...prev, ...validationErrors])
    }
    if (validated.length > 0) {
      setFiles((prev) => {
        const next = [...prev, ...validated]
        if (!expandedId && validated.length > 0) {
          setExpandedId(validated[0].id)
        }
        return next
      })
    }
  }, [files.length, maxFiles, validateFile, expandedId])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    const dropped = Array.from(e.dataTransfer.files)
    if (dropped.length > 0) addFiles(dropped)
  }, [addFiles])

  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = Array.from(e.target.files || [])
    if (selected.length > 0) addFiles(selected)
    e.target.value = ""
  }, [addFiles])

  const removeFile = useCallback((id: string) => {
    setFiles((prev) => prev.filter((f) => f.id !== id))
    if (expandedId === id) {
      setExpandedId(null)
    }
  }, [expandedId])

  const updateEntry = useCallback((id: string, updates: Partial<Pick<FileEntry, "name" | "fields">>) => {
    setFiles((prev) =>
      prev.map((f) =>
        f.id === id
          ? { ...f, ...updates, fields: updates.fields ? { ...f.fields, ...updates.fields } : f.fields }
          : f
      )
    )
  }, [])

  const accent = accentColor === "blue"
    ? { border: "border-blue-500/60", bg: "bg-blue-500/10", text: "text-blue-400" }
    : { border: "border-violet-500/60", bg: "bg-violet-500/10", text: "text-violet-400" }

  const hasFiles = files.length > 0
  const allDone = files.length > 0 && files.every((f) => f.status === "done" || f.status === "error")
  const doneCount = files.filter((f) => f.status === "done").length
  const errorCount = files.filter((f) => f.status === "error").length
  const pendingFiles = files.filter((f) => f.status === "pending")
  const allReady = pendingFiles.length > 0 && pendingFiles.every((f) => isEntryReady(f))

  return (
    <div className="space-y-4">
      {/* Rate limit banner */}
      <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-white/[0.02] px-3 py-2">
        <AlertCircle className="h-3.5 w-3.5 shrink-0 text-slate-500" />
        <p className="text-xs text-slate-500">{rateLimitText}</p>
      </div>

      {/* Validation errors */}
      {errors.length > 0 && (
        <div className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 space-y-1">
          {errors.map((err, i) => (
            <p key={i} className="text-xs text-red-400">{err}</p>
          ))}
        </div>
      )}

      {/* Drop zone */}
      {!hasFiles ? (
        <div
          className={`relative rounded-xl border-2 border-dashed transition-colors cursor-pointer ${
            dragging
              ? `${accent.border} ${accent.bg}`
              : "border-white/10 hover:border-white/20 bg-white/[0.02]"
          }`}
          onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => inputRef.current?.click()}
        >
          <div className="flex flex-col items-center gap-3 py-10 px-4 text-center">
            <Upload className="h-8 w-8 text-slate-600" />
            <div>
              <p className="text-sm font-medium text-slate-300">Drop files or click to browse</p>
              <p className="mt-1 text-xs text-slate-500">Up to {maxFiles} files</p>
            </div>
          </div>
        </div>
      ) : !uploadingAll && !allDone ? (
        <div
          className={`flex items-center justify-center gap-2 rounded-lg border-2 border-dashed transition-colors cursor-pointer py-2.5 ${
            dragging
              ? `${accent.border} ${accent.bg}`
              : "border-white/10 hover:border-white/20 bg-white/[0.02]"
          }`}
          onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => files.length < maxFiles && inputRef.current?.click()}
        >
          <Plus className="h-4 w-4 text-slate-500" />
          <span className="text-xs text-slate-500">
            Add more ({files.length}/{maxFiles})
          </span>
        </div>
      ) : null}

      <input
        ref={inputRef}
        type="file"
        accept={accept}
        multiple
        className="hidden"
        onChange={handleInputChange}
      />

      {/* Thumbnail grid — Task 4 */}

      {/* Inline editor — Task 5 */}

      {/* Upload controls — Task 6 */}
    </div>
  )
}
