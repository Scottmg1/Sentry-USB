# Multi-File Drag-and-Drop Upload for Community Wraps & Chimes

## Overview

Enhance both the Community Wraps and Community Chimes upload tabs with drag-and-drop support, multi-file batch uploads, per-file naming/configuration, and sequential upload with progress tracking.

## Requirements

- Drag-and-drop zone accepting multiple files
- Thumbnail grid showing all queued files
- Click-to-expand inline editor for naming and configuring each file
- X button on hover to remove individual files from the batch
- Two upload modes: "Upload All" (sequential) or individual per-file upload
- Checkmark overlays on completed uploads, error states on failures
- Rate limit banner: 10/hr for wraps, 5/hr for chimes
- No backend changes required — existing single-file APIs are called sequentially

## Architecture

### Shared Component: `MultiFileUploader`

**Location:** `web/src/components/upload/MultiFileUploader.tsx`

A generic, reusable component driven by props. Handles drag-drop, file grid, expand-to-edit, remove, and sequential upload orchestration. Wraps and Chimes provide their own validation, preview renderers, and form fields.

**Props:**

| Prop | Type | Description |
|------|------|-------------|
| `accept` | `string` | File input accept attribute (e.g. `".png,image/png"`) |
| `maxFiles` | `number` | Max files per batch |
| `rateLimitText` | `string` | Displayed in info banner (e.g. "10 uploads per hour") |
| `validateFile` | `(file: File) => Promise<{ok: boolean, error?: string}>` | Caller-defined file validation |
| `renderPreview` | `(file: File) => JSX.Element` | Thumbnail content — image for wraps, mini audio player for chimes |
| `renderFields` | `(state: FileEntry, onChange: (fields) => void) => JSX.Element` | Per-file form fields |
| `onUpload` | `(entry: FileEntry) => Promise<{success: boolean, message: string}>` | Handles actual API call (and 3D preview gen for wraps) |

**Internal State:**

```typescript
interface FileEntry {
  id: string           // unique ID for React keys
  file: File           // the raw File object
  name: string         // user-provided name
  fields: Record<string, any>  // extra fields (e.g. tesla_model for wraps)
  status: 'pending' | 'uploading' | 'done' | 'error'
  error?: string
}
```

- `files: FileEntry[]` — the batch queue
- `expandedId: string | null` — which file's editor is open
- `dragging: boolean` — drag-over visual state
- `uploadingAll: boolean` — whether "Upload All" is in progress

### Drag-and-Drop Zone

- **Default state:** Dashed border, upload icon, "Drop files or click to browse" text. Rate limit note underneath.
- **Drag-over state:** Border turns blue (wraps) or violet (chimes) with tinted background, matching existing color schemes.
- **After files added:** Shrinks to a compact "Add more files" strip above the grid.
- **Validation on drop:** Each file validated immediately via `validateFile`. Invalid files show a brief error toast and are not added.
- Hidden `<input multiple>` for click-to-browse. Both paths feed into the same `addFiles()` handler.

### Thumbnail Grid

Responsive grid layout (3-4 columns depending on width).

**Wraps thumbnails:** Actual PNG image from `URL.createObjectURL()`.

**Chimes thumbnails:** Mini audio player with play/pause button and filename.

**Thumbnail states:**

| State | Visual |
|-------|--------|
| Default | Border, preview content |
| Selected/expanded | Blue highlight border |
| Uploading | Spinner overlay, slightly dimmed |
| Done | Green checkmark overlay, green border |
| Error | Red border, small error icon |

**X button:** Top-right corner on hover. Hidden while that file is uploading.

Clicking a thumbnail opens its inline editor below the grid.

### Inline Editor

Appears below the grid when a thumbnail is clicked.

**Contents:**
- Full-size preview (image for wraps, larger audio player for chimes)
- Form fields provided by `renderFields`:
  - **Wraps:** Name input (50 char limit, alphanumeric/hyphens/spaces), Tesla Model dropdown (pre-filled with default, overridable per file)
  - **Chimes:** Name input (50 char limit), ".wav" suffix preview
- Individual "Upload" button for one-at-a-time flow
- Clicking a different thumbnail swaps the editor content
- Editor auto-closes after successful upload, next pending file auto-expands

### Upload Flow

**"Upload All" button** (below the grid):
1. Disables X buttons and "Add more" strip
2. Processes files sequentially in grid order (top-left to bottom-right)
3. For each file:
   - Thumbnail shows spinner, editor auto-expands with step-by-step progress
   - **Wraps:** Generate 3D preview via Godot, then upload (existing multi-step progress UI)
   - **Chimes:** Upload directly
   - On success: green checkmark on thumbnail, auto-advance to next
   - On error: red border, error message in editor, continue to next file
4. Button shows aggregate progress: "Uploading 2 of 5..."

**Individual "Upload" button** (in expanded editor):
- Uploads just that one file
- Same progress/checkmark behavior
- User manually picks next file

**After all files complete:**
- Summary banner: "4 of 5 uploaded successfully" or "All 5 uploaded!"
- Failed files remain in grid with red state for retry or removal
- "Clear All" button appears to reset for a new batch

## Integration

### CommunityWraps.tsx — UploadTab

Replace current single-file picker, preview, name input, model dropdown, and submit button with `<MultiFileUploader>`.

- `accept=".png,image/png"`, `maxFiles={10}`, `rateLimitText="10 uploads per hour"`
- `validateFile`: PNG type check, under 1MB, dimension check (existing logic)
- `renderPreview`: `<img>` tag from `URL.createObjectURL(file)`
- `renderFields`: Name input + Tesla Model dropdown. Default model state lifted to UploadTab level.
- `onUpload`: Existing Godot 3D preview generation + `fetch(/api/wraps/upload)` logic, moved into a per-file function. Godot refs (`godotReadyRef`, `godotRef`) stay in UploadTab, accessed via closure.

### LockChime.tsx — Community Upload Section

Replace current drag-drop zone + rename modal with `<MultiFileUploader>`.

- `accept=".wav,audio/wav,audio/x-wav"`, `maxFiles={5}`, `rateLimitText="5 uploads per hour"`
- `validateFile`: WAV type check, under 5MB, duration under 7 seconds (existing Web Audio API logic)
- `renderPreview`: Mini `<audio>` element with play/pause button
- `renderFields`: Name input with ".wav" suffix display
- `onUpload`: `fetch(/api/lockchime/community/upload)` call

The existing local library upload (non-community) in LockChime stays untouched.

### No Backend Changes

The existing single-file upload APIs (`POST /api/wraps/upload`, `POST /api/lockchime/community/upload`) are called sequentially — one request per file. Rate limiting is enforced server-side on the support server. No new endpoints needed.

## Styling

- Follows existing dark theme: `bg-white/[0.03]`, `border-white/10`, slate/blue/violet accents
- Wraps uses blue accent color, Chimes uses violet — matching current pages
- Tailwind CSS classes throughout, consistent with rest of the app
- Lucide React icons: `Upload`, `X`, `CheckCircle`, `Loader2`, `AlertCircle`, `Play`, `Pause`

## File Structure

```
web/src/components/upload/
  MultiFileUploader.tsx    # Shared component
```

Existing files modified:
- `web/src/pages/CommunityWraps.tsx` — UploadTab refactored to use MultiFileUploader
- `web/src/pages/LockChime.tsx` — Community upload section refactored to use MultiFileUploader
