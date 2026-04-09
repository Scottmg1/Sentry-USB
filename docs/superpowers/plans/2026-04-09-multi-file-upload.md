# Multi-File Drag-and-Drop Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add drag-and-drop multi-file upload with thumbnail grid, inline editing, and sequential upload with progress to both Community Wraps and Community Chimes.

**Architecture:** A shared `MultiFileUploader` component handles drag-drop, file grid, expand-to-edit, remove, and sequential upload orchestration. Wraps and Chimes pass in their own validation, preview renderers, and form fields via props. No backend changes.

**Tech Stack:** React 19, TypeScript, Tailwind CSS, Lucide React icons, Web Audio API (for WAV duration validation)

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `web/src/components/upload/MultiFileUploader.tsx` | Shared multi-file uploader: drag-drop zone, thumbnail grid, inline editor, upload orchestration |
| Modify | `web/src/pages/CommunityWraps.tsx` | Replace single-file UploadTab with MultiFileUploader integration |
| Modify | `web/src/pages/LockChime.tsx` | Replace single-file CommunityUpload with MultiFileUploader integration |

---

### Task 1: Create MultiFileUploader — Types and Shell

**Files:**
- Create: `web/src/components/upload/MultiFileUploader.tsx`

- [ ] **Step 1: Create the component file with types and empty shell**

Create `web/src/components/upload/MultiFileUploader.tsx`:

```tsx
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

  return <div>TODO</div>
}
```

- [ ] **Step 2: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No errors related to `MultiFileUploader.tsx` (other pre-existing errors are OK).

- [ ] **Step 3: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx
git commit -m "feat: add MultiFileUploader component shell with types"
```

---

### Task 2: Implement File Addition and Validation

**Files:**
- Modify: `web/src/components/upload/MultiFileUploader.tsx`

- [ ] **Step 1: Add the addFiles handler**

Inside the `MultiFileUploader` function, after the state declarations and before the return, add:

```tsx
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
        // Auto-expand first new file if nothing is expanded
        if (!expandedId && validated.length > 0) {
          setExpandedId(validated[0].id)
        }
        return next
      })
    }
  }, [files.length, maxFiles, validateFile, expandedId])
```

- [ ] **Step 2: Add the drag-drop and click handlers**

After `addFiles`, add:

```tsx
  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    const dropped = Array.from(e.dataTransfer.files)
    if (dropped.length > 0) addFiles(dropped)
  }, [addFiles])

  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = Array.from(e.target.files || [])
    if (selected.length > 0) addFiles(selected)
    // Reset input so re-selecting same file works
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
```

- [ ] **Step 3: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx
git commit -m "feat: add file validation, drag-drop, and removal handlers"
```

---

### Task 3: Implement the Drop Zone UI

**Files:**
- Modify: `web/src/components/upload/MultiFileUploader.tsx`

- [ ] **Step 1: Replace the return statement with the full drop zone UI**

Replace `return <div>TODO</div>` with:

```tsx
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

      {/* Drop zone — full size when no files, compact strip when files exist */}
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
```

- [ ] **Step 2: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx
git commit -m "feat: add drop zone UI with rate limit banner"
```

---

### Task 4: Implement the Thumbnail Grid

**Files:**
- Modify: `web/src/components/upload/MultiFileUploader.tsx`

- [ ] **Step 1: Replace the thumbnail grid placeholder comment**

Replace `{/* Thumbnail grid — Task 4 */}` with:

```tsx
      {/* Thumbnail grid */}
      {hasFiles && (
        <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
          {files.map((entry) => {
            const isExpanded = expandedId === entry.id
            const isUploading = entry.status === "uploading"
            const isDone = entry.status === "done"
            const isError = entry.status === "error"

            return (
              <div
                key={entry.id}
                className={`group relative aspect-square overflow-hidden rounded-lg border-2 cursor-pointer transition-colors ${
                  isDone
                    ? "border-emerald-500/40"
                    : isError
                    ? "border-red-500/40"
                    : isExpanded
                    ? `${accent.border}`
                    : "border-white/10 hover:border-white/20"
                }`}
                onClick={() => !isUploading && setExpandedId(isExpanded ? null : entry.id)}
              >
                {/* Preview content */}
                <div className={`flex h-full w-full items-center justify-center bg-white/[0.02] ${
                  isUploading ? "opacity-50" : ""
                }`}>
                  {renderPreview(entry.file)}
                </div>

                {/* Uploading spinner overlay */}
                {isUploading && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/40">
                    <Loader2 className="h-6 w-6 animate-spin text-white" />
                  </div>
                )}

                {/* Done checkmark overlay */}
                {isDone && (
                  <div className="absolute inset-0 flex items-center justify-center bg-emerald-500/20">
                    <CheckCircle className="h-8 w-8 text-emerald-400" />
                  </div>
                )}

                {/* Error overlay */}
                {isError && (
                  <div className="absolute inset-0 flex items-center justify-center bg-red-500/10">
                    <AlertCircle className="h-6 w-6 text-red-400" />
                  </div>
                )}

                {/* Remove button (hover, hidden during upload or after done) */}
                {!isUploading && !isDone && !uploadingAll && (
                  <button
                    className="absolute top-1 right-1 flex h-5 w-5 items-center justify-center rounded-full bg-black/60 text-slate-400 opacity-0 transition-opacity group-hover:opacity-100 hover:text-white"
                    onClick={(e) => {
                      e.stopPropagation()
                      removeFile(entry.id)
                    }}
                  >
                    <X className="h-3 w-3" />
                  </button>
                )}

                {/* Filename label */}
                <div className="absolute bottom-0 left-0 right-0 truncate bg-gradient-to-t from-black/70 to-transparent px-1.5 pb-1 pt-4">
                  <p className="truncate text-[10px] text-white/80">{entry.name || entry.file.name}</p>
                </div>
              </div>
            )
          })}
        </div>
      )}
```

- [ ] **Step 2: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx
git commit -m "feat: add thumbnail grid with status overlays and remove button"
```

---

### Task 5: Implement the Inline Editor

**Files:**
- Modify: `web/src/components/upload/MultiFileUploader.tsx`

- [ ] **Step 1: Replace the inline editor placeholder comment**

Replace `{/* Inline editor — Task 5 */}` with:

```tsx
      {/* Inline editor */}
      {expandedId && (() => {
        const entry = files.find((f) => f.id === expandedId)
        if (!entry) return null
        const isUploading = entry.status === "uploading"
        const isDone = entry.status === "done"
        const isError = entry.status === "error"

        return (
          <div className={`rounded-xl border p-4 space-y-4 ${
            isError
              ? "border-red-500/30 bg-red-500/[0.04]"
              : `${accent.border.replace("/60", "/30")} bg-white/[0.02]`
          }`}>
            {/* Full preview */}
            <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/50">
              <div className="flex h-48 items-center justify-center">
                {renderPreview(entry.file)}
              </div>
            </div>

            {/* Caller-provided form fields */}
            {!isDone && renderFields(entry, (updates) => updateEntry(entry.id, updates))}

            {/* Upload step progress */}
            {isUploading && currentStep && (
              <div className="flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin text-blue-400 shrink-0" />
                <span className="text-sm text-blue-300">{currentStep}</span>
              </div>
            )}

            {/* Error message */}
            {isError && entry.error && (
              <div className="flex items-center gap-2 text-sm text-red-400">
                <AlertCircle className="h-4 w-4 shrink-0" />
                {entry.error}
              </div>
            )}

            {/* Done message */}
            {isDone && (
              <div className="flex items-center gap-2 text-sm text-emerald-400">
                <CheckCircle className="h-4 w-4 shrink-0" />
                Uploaded successfully
              </div>
            )}

            {/* Individual upload button */}
            {!isDone && !uploadingAll && (
              <button
                onClick={() => uploadSingle(entry.id)}
                disabled={isUploading || !isEntryReady(entry)}
                className={`flex w-full items-center justify-center gap-2 rounded-lg py-2 text-sm font-medium text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                  accentColor === "blue"
                    ? "bg-blue-600 hover:bg-blue-500"
                    : "bg-violet-600 hover:bg-violet-500"
                }`}
              >
                {isUploading ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Upload className="h-4 w-4" />
                )}
                {isUploading ? "Uploading..." : "Upload"}
              </button>
            )}
          </div>
        )
      })()}
```

- [ ] **Step 2: Add the uploadSingle function**

After the `updateEntry` function, add:

```tsx
  const uploadSingle = useCallback(async (id: string) => {
    const entry = files.find((f) => f.id === id)
    if (!entry || entry.status !== "pending") return

    setFiles((prev) => prev.map((f) => f.id === id ? { ...f, status: "uploading" } : f))
    setExpandedId(id)
    setCurrentStep(null)

    try {
      const result = await onUpload(entry, (step) => setCurrentStep(step))
      setFiles((prev) =>
        prev.map((f) =>
          f.id === id
            ? { ...f, status: result.success ? "done" : "error", error: result.success ? undefined : result.message }
            : f
        )
      )
      // Auto-advance to next pending file
      if (result.success) {
        const nextPending = files.find((f) => f.id !== id && f.status === "pending")
        setExpandedId(nextPending?.id ?? null)
      }
    } catch (err: any) {
      setFiles((prev) =>
        prev.map((f) => f.id === id ? { ...f, status: "error", error: err.message || "Upload failed" } : f)
      )
    } finally {
      setCurrentStep(null)
    }
  }, [files, onUpload])
```

- [ ] **Step 3: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx
git commit -m "feat: add inline editor with individual upload and step progress"
```

---

### Task 6: Implement Upload All and Summary

**Files:**
- Modify: `web/src/components/upload/MultiFileUploader.tsx`

- [ ] **Step 1: Add the uploadAll function**

After the `uploadSingle` function, add:

```tsx
  const uploadAll = useCallback(async () => {
    const toUpload = files.filter((f) => f.status === "pending" && isEntryReady(f))
    if (toUpload.length === 0) return

    setUploadingAll(true)
    setUploadProgress({ current: 0, total: toUpload.length })

    for (let i = 0; i < toUpload.length; i++) {
      const entry = toUpload[i]
      setUploadProgress({ current: i + 1, total: toUpload.length })
      setFiles((prev) => prev.map((f) => f.id === entry.id ? { ...f, status: "uploading" } : f))
      setExpandedId(entry.id)
      setCurrentStep(null)

      try {
        const result = await onUpload(entry, (step) => setCurrentStep(step))
        setFiles((prev) =>
          prev.map((f) =>
            f.id === entry.id
              ? { ...f, status: result.success ? "done" : "error", error: result.success ? undefined : result.message }
              : f
          )
        )
      } catch (err: any) {
        setFiles((prev) =>
          prev.map((f) =>
            f.id === entry.id ? { ...f, status: "error", error: err.message || "Upload failed" } : f
          )
        )
      }

      setCurrentStep(null)
    }

    setUploadingAll(false)
    setUploadProgress(null)
    setExpandedId(null)
  }, [files, isEntryReady, onUpload])

  const clearAll = useCallback(() => {
    setFiles([])
    setExpandedId(null)
    setErrors([])
    setUploadProgress(null)
  }, [])
```

- [ ] **Step 2: Replace the upload controls placeholder comment**

Replace `{/* Upload controls — Task 6 */}` with:

```tsx
      {/* Upload All / Summary */}
      {hasFiles && !allDone && !uploadingAll && pendingFiles.length > 1 && (
        <button
          onClick={uploadAll}
          disabled={!allReady}
          className={`flex w-full items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
            accentColor === "blue"
              ? "bg-blue-600 hover:bg-blue-500"
              : "bg-violet-600 hover:bg-violet-500"
          }`}
        >
          <Upload className="h-4 w-4" />
          Upload All ({pendingFiles.length} files)
        </button>
      )}

      {/* Upload All progress */}
      {uploadingAll && uploadProgress && (
        <div className="flex items-center justify-center gap-2 rounded-lg border border-white/10 bg-white/[0.02] py-2.5">
          <Loader2 className="h-4 w-4 animate-spin text-blue-400" />
          <span className="text-sm text-slate-300">
            Uploading {uploadProgress.current} of {uploadProgress.total}...
          </span>
        </div>
      )}

      {/* Completion summary */}
      {allDone && (
        <div className="space-y-3">
          <div className={`flex items-center gap-2 rounded-lg px-4 py-3 text-sm ${
            errorCount === 0
              ? "bg-emerald-500/10 text-emerald-400 border border-emerald-500/20"
              : "bg-amber-500/10 text-amber-300 border border-amber-500/20"
          }`}>
            {errorCount === 0 ? (
              <CheckCircle className="h-4 w-4 shrink-0" />
            ) : (
              <AlertCircle className="h-4 w-4 shrink-0" />
            )}
            {errorCount === 0
              ? `All ${doneCount} file${doneCount !== 1 ? "s" : ""} uploaded!`
              : `${doneCount} of ${files.length} uploaded successfully`
            }
          </div>
          <button
            onClick={clearAll}
            className="flex w-full items-center justify-center gap-2 rounded-lg border border-white/10 py-2 text-sm text-slate-400 transition-colors hover:bg-white/5"
          >
            <X className="h-4 w-4" />
            Clear All
          </button>
        </div>
      )}
```

- [ ] **Step 3: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx
git commit -m "feat: add Upload All with sequential progress and completion summary"
```

---

### Task 7: Integrate MultiFileUploader into CommunityWraps UploadTab

**Files:**
- Modify: `web/src/pages/CommunityWraps.tsx:608-926` (the `UploadTab` function)

- [ ] **Step 1: Add the import**

At the top of `CommunityWraps.tsx`, add after the existing imports:

```tsx
import MultiFileUploader, { type FileEntry } from "../components/upload/MultiFileUploader"
```

- [ ] **Step 2: Rewrite the UploadTab function**

Replace the entire `UploadTab` function (lines 608-926) with:

```tsx
function UploadTab({ godotReadyRef, godotRef, adminPasscode }: UploadTabProps) {
  const [defaultModel, setDefaultModel] = useState("")

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

  const generate3DPreview = useCallback((imageFile: File, godotId: string): Promise<string> => {
    return new Promise((resolve, reject) => {
      let textureDataUrl: string | null = null
      let phase: "loading_scene" | "applying_texture" | "capturing" = "loading_scene"

      const abortTimer = setTimeout(() => {
        cleanup()
        reject(new Error("Preview capture timeout"))
      }, 30000)

      const cleanup = () => {
        clearTimeout(abortTimer)
        window.removeEventListener("message", handler)
      }

      const handler = (e: MessageEvent) => {
        if (!e.data?.type) return

        if ((e.data.type === "car_loaded" || e.data.type === "scene_loaded") && phase === "loading_scene") {
          phase = "applying_texture"
          if (textureDataUrl) {
            setTimeout(() => {
              godotRef.current?.setTexture(textureDataUrl!)
              phase = "capturing"
              const camDistance = GODOT_CAMERA_DISTANCE[godotId]
              setTimeout(() => godotRef.current?.capture(camDistance), 3000)
            }, 2000)
          }
        }

        if (e.data.type === "capture_result" && e.data.dataUrl) {
          cleanup()
          const img = new Image()
          img.onload = () => {
            const size = Math.min(img.width, img.height)
            const sx = (img.width - size) / 2
            const sy = (img.height - size) / 2
            const canvas = document.createElement("canvas")
            canvas.width = 1024
            canvas.height = 1024
            const ctx = canvas.getContext("2d")!
            ctx.drawImage(img, sx, sy, size, size, 0, 0, 1024, 1024)
            resolve(canvas.toDataURL("image/png"))
          }
          img.onerror = () => resolve(e.data.dataUrl)
          img.src = e.data.dataUrl
        } else if (e.data.type === "capture_error") {
          cleanup()
          reject(new Error(e.data.error || "Capture failed"))
        }
      }
      window.addEventListener("message", handler)

      const reader = new FileReader()
      reader.onload = () => {
        textureDataUrl = reader.result as string
        godotRef.current?.loadScene(godotId)
        setTimeout(() => {
          if (phase === "loading_scene") {
            console.warn("Scene load event not received, applying texture anyway")
            phase = "applying_texture"
            setTimeout(() => {
              godotRef.current?.setTexture(textureDataUrl!)
              phase = "capturing"
              const camDistance = GODOT_CAMERA_DISTANCE[godotId]
              setTimeout(() => godotRef.current?.capture(camDistance), 3000)
            }, 2000)
          }
        }, 10000)
      }
      reader.readAsDataURL(imageFile)
    })
  }, [godotRef])

  const validateFile = useCallback(async (file: File) => {
    if (file.type !== "image/png") {
      return { ok: false, error: "Only PNG files are supported" }
    }
    if (file.size > 1024 * 1024) {
      return { ok: false, error: "File must be under 1 MB" }
    }
    return { ok: true }
  }, [])

  const handleUpload = useCallback(async (
    entry: FileEntry,
    onStep: (step: string) => void
  ): Promise<{ success: boolean; message: string }> => {
    const model = entry.fields.tesla_model || defaultModel
    if (!model) return { success: false, message: "No Tesla model selected" }

    let previewDataUrl: string | null = null
    const godotId = MODEL_TO_GODOT_ID[model]
    const willGeneratePreview = !!(godotId && godotRef.current)

    if (willGeneratePreview) {
      onStep("Generating 3D preview...")
      if (!godotReadyRef.current) {
        const ready = await waitForGodotReady(60000)
        if (!ready) {
          onStep("Uploading wrap...")
        }
      }
      if (godotReadyRef.current) {
        try {
          previewDataUrl = await generate3DPreview(entry.file, godotId!)
        } catch (previewErr) {
          console.warn("[WRAPS] 3D preview generation failed:", previewErr)
        }
      }
    }

    onStep("Uploading wrap...")

    const formData = new FormData()
    formData.append("image", entry.file)
    formData.append("name", entry.name.trim())
    formData.append("tesla_model", model)

    if (previewDataUrl) {
      const previewBlob = await (await fetch(previewDataUrl)).blob()
      formData.append("preview", previewBlob, "preview.png")
    }

    const headers: Record<string, string> = {}
    if (adminPasscode) headers["x-passcode"] = adminPasscode

    const res = await fetch(`${API_BASE}/wraps/upload`, {
      method: "POST",
      headers,
      body: formData,
    })

    const data = await res.json()
    if (!res.ok) {
      return { success: false, message: data.error || `HTTP ${res.status}` }
    }

    return { success: true, message: data.message || "Wrap submitted!" }
  }, [defaultModel, godotRef, godotReadyRef, waitForGodotReady, generate3DPreview, adminPasscode])

  return (
    <div className="mx-auto max-w-lg space-y-5">
      {/* Default Tesla model selector */}
      <div>
        <label className="mb-1.5 block text-sm font-medium text-slate-300">Default Tesla Model</label>
        <select
          value={defaultModel}
          onChange={(e) => setDefaultModel(e.target.value)}
          className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
        >
          <option value="" className="bg-slate-900">Select model...</option>
          {TESLA_MODELS.map((m) => (
            <option key={m} value={m} className="bg-slate-900">{m}</option>
          ))}
        </select>
        <p className="mt-1 text-xs text-slate-600">Applied to all files unless overridden per file</p>
      </div>

      <MultiFileUploader
        accept=".png,image/png"
        maxFiles={10}
        rateLimitText="Up to 10 wraps per hour. Submissions are reviewed before appearing in the library."
        accentColor="blue"
        validateFile={validateFile}
        renderPreview={(file) => (
          <img
            src={URL.createObjectURL(file)}
            alt={file.name}
            className="h-full w-full object-cover"
          />
        )}
        renderFields={(entry, onChange) => (
          <div className="space-y-3">
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-400">Wrap Name</label>
              <input
                type="text"
                value={entry.name}
                onChange={(e) => onChange({ name: e.target.value.slice(0, 50) })}
                placeholder="e.g. Red Carbon Fiber"
                className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-blue-500/50 focus:outline-none"
              />
              <p className="mt-1 text-xs text-slate-600">{entry.name.length}/50</p>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-400">Tesla Model</label>
              <select
                value={entry.fields.tesla_model || defaultModel}
                onChange={(e) => onChange({ fields: { tesla_model: e.target.value } })}
                className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 focus:border-blue-500/50 focus:outline-none"
              >
                <option value="" className="bg-slate-900">Select model...</option>
                {TESLA_MODELS.map((m) => (
                  <option key={m} value={m} className="bg-slate-900">{m}</option>
                ))}
              </select>
            </div>
          </div>
        )}
        isEntryReady={(entry) =>
          entry.name.trim().length > 0 &&
          !!(entry.fields.tesla_model || defaultModel)
        }
        onUpload={handleUpload}
      />
    </div>
  )
}
```

- [ ] **Step 3: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 4: Manually test in browser**

1. Navigate to Community → Wraps → Upload tab
2. Test drag-and-drop with multiple PNG files
3. Test click-to-browse with multiple selections
4. Verify thumbnails appear in grid
5. Click a thumbnail — verify inline editor shows with name + model fields
6. Test individual upload and Upload All
7. Verify 3D preview generation still works per file
8. Verify checkmarks appear on completed files

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/CommunityWraps.tsx
git commit -m "feat: integrate MultiFileUploader into CommunityWraps UploadTab"
```

---

### Task 8: Integrate MultiFileUploader into LockChime CommunityUpload

**Files:**
- Modify: `web/src/pages/LockChime.tsx:1523-1678` (the `CommunityUpload` function)

- [ ] **Step 1: Add the import**

At the top of `LockChime.tsx`, add after the existing imports:

```tsx
import MultiFileUploader, { type FileEntry } from "../components/upload/MultiFileUploader"
```

- [ ] **Step 2: Rewrite the CommunityUpload function**

Replace the entire `CommunityUpload` function (lines 1523-1678) with:

```tsx
function CommunityUpload({ adminPasscode }: { adminPasscode: string | null }) {
  const { showToast, ToastView } = useToast()

  const validateFile = useCallback(async (file: File) => {
    if (!file.name.toLowerCase().endsWith(".wav")) {
      return { ok: false, error: "Only .wav files are supported" }
    }
    if (file.size > MAX_FILE_BYTES) {
      return { ok: false, error: "File is too large (max 5 MB)" }
    }
    try {
      const duration = await getWavDuration(file)
      if (duration > MAX_DURATION_SECONDS) {
        return { ok: false, error: `Sound is ${duration.toFixed(1)}s — max ${MAX_DURATION_SECONDS}s` }
      }
    } catch {
      return { ok: false, error: "Could not read WAV file" }
    }
    return { ok: true }
  }, [])

  const handleUpload = useCallback(async (
    entry: FileEntry,
    onStep: (step: string) => void
  ): Promise<{ success: boolean; message: string }> => {
    if (!entry.name.trim()) return { success: false, message: "Name is required" }
    if (entry.name.trim().toLowerCase() === "lockchime") {
      return { success: false, message: 'Sound name cannot be "lockchime"' }
    }

    onStep("Uploading sound...")

    const form = new FormData()
    form.append("sound", entry.file)
    form.append("name", entry.name.trim())

    const headers: HeadersInit = {}
    if (adminPasscode) headers["x-passcode"] = adminPasscode

    const res = await fetch(`${API_BASE}/lockchime/community/upload`, {
      method: "POST",
      headers,
      body: form,
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) {
      return { success: false, message: data.error || `HTTP ${res.status}` }
    }
    return { success: true, message: "Sound submitted!" }
  }, [adminPasscode])

  return (
    <div className="space-y-5">
      <div className="rounded-xl border border-white/10 bg-white/[0.02] p-5 space-y-4">
        <h3 className="text-sm font-medium text-slate-200">Share Lock Sounds</h3>
        <p className="text-xs text-slate-500">
          Upload .wav files to share with the Sentry USB community. Submissions are reviewed before appearing in the library.
        </p>

        <MultiFileUploader
          accept=".wav,audio/wav,audio/x-wav"
          maxFiles={5}
          rateLimitText="Up to 5 sounds per hour. Submissions are reviewed before appearing in the library."
          accentColor="violet"
          validateFile={validateFile}
          renderPreview={(file) => <AudioPreview file={file} />}
          renderFields={(entry, onChange) => (
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-400">Sound Name</label>
              <input
                type="text"
                value={entry.name}
                onChange={(e) => onChange({ name: e.target.value.slice(0, 50) })}
                placeholder="e.g. Sci-Fi Beep"
                className="w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 focus:border-violet-500/50 focus:outline-none"
              />
              <p className="mt-1 text-xs text-slate-600">
                Will be saved as <code className="text-slate-400">{entry.name.trim() || "..."}.wav</code> · {entry.name.length}/50
              </p>
            </div>
          )}
          isEntryReady={(entry) => entry.name.trim().length > 0}
          onUpload={handleUpload}
        />
      </div>

      {ToastView}
    </div>
  )
}
```

- [ ] **Step 3: Add the AudioPreview component**

After the `CommunityUpload` function, add:

```tsx
function AudioPreview({ file }: { file: File }) {
  const [playing, setPlaying] = useState(false)
  const audioRef = useRef<HTMLAudioElement>(null)
  const [url] = useState(() => URL.createObjectURL(file))

  const togglePlay = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    const audio = audioRef.current
    if (!audio) return
    if (playing) {
      audio.pause()
      audio.currentTime = 0
      setPlaying(false)
    } else {
      audio.play()
      setPlaying(true)
    }
  }, [playing])

  return (
    <div className="flex flex-col items-center justify-center gap-1.5 p-2">
      <button
        onClick={togglePlay}
        className="flex h-10 w-10 items-center justify-center rounded-full bg-violet-500/20 text-violet-400 transition-colors hover:bg-violet-500/30"
      >
        {playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
      </button>
      <audio
        ref={audioRef}
        src={url}
        onEnded={() => setPlaying(false)}
      />
    </div>
  )
}
```

- [ ] **Step 4: Add the missing icon imports**

In the existing import from `lucide-react` at the top of `LockChime.tsx`, add `Play` and `Pause` if not already present. The current import list is:

```tsx
import {
  Music, Upload, Play, Pause, CheckCircle2, Trash2, Volume2, X, AlertCircle,
  AlertTriangle, Shuffle, Clock, Zap, Download, Search, ChevronDown, ChevronLeft,
  ChevronRight, Loader2, Shield, Pencil, Unplug,
} from "lucide-react"
```

`Play` and `Pause` are already imported. No change needed.

- [ ] **Step 5: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 6: Manually test in browser**

1. Navigate to Community → Chimes → Community tab → Share a Sound
2. Test drag-and-drop with multiple WAV files
3. Test click-to-browse with multiple selections
4. Verify thumbnails show mini play/pause buttons
5. Click play on a thumbnail — verify audio plays
6. Click a thumbnail — verify inline editor with name input
7. Test individual upload and Upload All
8. Verify checkmarks appear on completed files
9. Verify invalid files (too long, too big, non-WAV) are rejected with error messages

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/LockChime.tsx
git commit -m "feat: integrate MultiFileUploader into LockChime CommunityUpload with audio preview"
```

---

### Task 9: Fix Object URL Memory Leaks

**Files:**
- Modify: `web/src/components/upload/MultiFileUploader.tsx`
- Modify: `web/src/pages/CommunityWraps.tsx`

The `renderPreview` callbacks in both integrations create object URLs via `URL.createObjectURL()` on every render but never revoke them. This causes memory leaks with multiple files.

- [ ] **Step 1: Add a URL-caching preview wrapper to MultiFileUploader**

At the top of `MultiFileUploader.tsx`, after the imports, add:

```tsx
import { useEffect } from "react"
```

Update the import line to include `useEffect`. Then, after the `MultiFileUploader` component export, add a helper:

```tsx
export function useObjectUrl(file: File): string {
  const [url, setUrl] = useState("")
  useEffect(() => {
    const u = URL.createObjectURL(file)
    setUrl(u)
    return () => URL.revokeObjectURL(u)
  }, [file])
  return url
}
```

- [ ] **Step 2: Update the CommunityWraps renderPreview to use the hook**

In `CommunityWraps.tsx`, create a small wrapper component and use it in `renderPreview`. Add after the imports:

```tsx
function WrapPreview({ file }: { file: File }) {
  const url = useObjectUrl(file)
  if (!url) return null
  return <img src={url} alt={file.name} className="h-full w-full object-cover" />
}
```

Update the import to include `useObjectUrl`:

```tsx
import MultiFileUploader, { type FileEntry, useObjectUrl } from "../components/upload/MultiFileUploader"
```

Then change the `renderPreview` prop from the inline arrow to:

```tsx
renderPreview={(file) => <WrapPreview file={file} />}
```

- [ ] **Step 3: Update the LockChime AudioPreview**

The `AudioPreview` component already creates the URL once via `useState`, which is fine — it doesn't re-create on re-render. But it never revokes it. Update the `AudioPreview` component to use `useObjectUrl`:

Add `useObjectUrl` to the import:

```tsx
import MultiFileUploader, { type FileEntry, useObjectUrl } from "../components/upload/MultiFileUploader"
```

Then in `AudioPreview`, replace:

```tsx
  const [url] = useState(() => URL.createObjectURL(file))
```

with:

```tsx
  const url = useObjectUrl(file)
```

- [ ] **Step 4: Verify the file compiles**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npx tsc --noEmit --pretty 2>&1 | head -20`

Expected: No new errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/upload/MultiFileUploader.tsx web/src/pages/CommunityWraps.tsx web/src/pages/LockChime.tsx
git commit -m "fix: revoke object URLs to prevent memory leaks in upload previews"
```

---

### Task 10: End-to-End Smoke Test

**Files:** None (testing only)

- [ ] **Step 1: Start the dev server**

Run: `cd "/Users/jhoan/Nextcloud/Documents/Visual Studio Code/Sentry-USB/web" && npm run dev`

- [ ] **Step 2: Test Wraps multi-file upload**

1. Go to Community → Wraps → Upload
2. Select a default Tesla model
3. Drag 3 PNG files onto the drop zone
4. Verify: 3 thumbnails appear in grid, rate limit banner shows "Up to 10 wraps per hour"
5. Click thumbnail → verify editor opens with name + model fields, model pre-filled with default
6. Change the model on one file to verify per-file override
7. Hover a thumbnail → verify X button appears, click to remove
8. Click "Upload All" → verify sequential processing with 3D preview step + upload step per file
9. Verify checkmarks appear as each file completes
10. Verify completion summary shows

- [ ] **Step 3: Test Chimes multi-file upload**

1. Go to Community → Chimes → Community → Share a Sound
2. Drop 2 WAV files onto the drop zone
3. Verify: thumbnails show play/pause buttons, rate limit banner shows "Up to 5 sounds per hour"
4. Click play on a thumbnail → verify audio plays
5. Click a thumbnail → verify editor with name input
6. Upload one file individually → verify checkmark
7. Upload the second → verify completion summary

- [ ] **Step 4: Test edge cases**

1. Try dropping more files than the limit (e.g. 12 PNGs for wraps) → verify only 10 are added with warning
2. Try dropping invalid files (JPGs for wraps, MP3s for chimes) → verify error messages
3. Try uploading with empty name → verify Upload button is disabled
4. Try adding files after some are already uploaded → verify "Add more" strip works

- [ ] **Step 5: Commit if any fixes were needed**

If any bugs were found and fixed during testing, commit those fixes.
