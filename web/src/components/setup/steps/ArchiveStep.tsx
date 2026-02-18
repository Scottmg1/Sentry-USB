import { Archive } from "lucide-react"
import type { StepProps } from "../SetupWizard"
import { SecretInput } from "../SecretInput"
import { cn } from "@/lib/utils"

const archiveSystems = [
  { id: "cifs", label: "CIFS / SMB", desc: "Windows/Mac file sharing" },
  { id: "rsync", label: "rsync", desc: "SSH-based file sync" },
  { id: "rclone", label: "rclone", desc: "Cloud storage (Google Drive, S3, etc.)" },
  { id: "nfs", label: "NFS", desc: "Network File System" },
  { id: "none", label: "None", desc: "No archiving, local storage only" },
]

function Field({
  label,
  field,
  type = "text",
  placeholder,
  data,
  onChange,
  hint,
}: {
  label: string
  field: string
  type?: string
  placeholder?: string
  data: StepProps["data"]
  onChange: StepProps["onChange"]
  hint?: string
}) {
  const inputCls = "w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-slate-300">
        {label}
      </label>
      {type === "password" ? (
        <SecretInput
          value={data[field] ?? ""}
          onChange={(v) => onChange(field, v)}
          placeholder={placeholder}
          className={cn(inputCls, "pr-8")}
        />
      ) : (
        <input
          type={type}
          value={data[field] ?? ""}
          onChange={(e) => onChange(field, e.target.value)}
          placeholder={placeholder}
          className={inputCls}
        />
      )}
      {hint && <p className="mt-1 text-xs text-slate-600">{hint}</p>}
    </div>
  )
}

export function ArchiveStep({ data, onChange }: StepProps) {
  const system = data.ARCHIVE_SYSTEM ?? "cifs"

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Archive className="h-4 w-4 text-blue-400" />
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
          Archive System
        </h3>
      </div>

      <p className="text-xs text-slate-500">
        Choose how recorded clips are automatically backed up when you connect
        to WiFi.
      </p>

      {/* System selector */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {archiveSystems.map((s) => (
          <button
            key={s.id}
            onClick={() => onChange("ARCHIVE_SYSTEM", s.id)}
            className={cn(
              "rounded-lg border p-3 text-left transition-colors",
              system === s.id
                ? "border-blue-500/40 bg-blue-500/10"
                : "border-white/5 bg-white/[0.02] hover:border-white/10 hover:bg-white/[0.04]"
            )}
          >
            <p
              className={cn(
                "text-sm font-medium",
                system === s.id ? "text-blue-400" : "text-slate-300"
              )}
            >
              {s.label}
            </p>
            <p className="mt-0.5 text-xs text-slate-600">{s.desc}</p>
          </button>
        ))}
      </div>

      {/* Dynamic fields per archive system */}
      {system === "cifs" && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Archive Server" field="ARCHIVE_SERVER" placeholder="hostname or IP" data={data} onChange={onChange} />
          <Field label="Share Name" field="SHARE_NAME" placeholder="share/path" data={data} onChange={onChange} />
          <Field label="Username" field="SHARE_USER" placeholder="username" data={data} onChange={onChange} />
          <Field label="Password" field="SHARE_PASSWORD" type="password" placeholder="password" data={data} onChange={onChange} />
          <Field label="Domain" field="SHARE_DOMAIN" placeholder="optional" data={data} onChange={onChange} hint="Usually not needed" />
          <Field label="CIFS Version" field="CIFS_VERSION" placeholder="3.0" data={data} onChange={onChange} hint="Usually not needed" />
        </div>
      )}

      {system === "rsync" && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Server" field="RSYNC_SERVER" placeholder="hostname or IP" data={data} onChange={onChange} />
          <Field label="Username" field="RSYNC_USER" placeholder="username" data={data} onChange={onChange} />
          <Field label="Remote Path" field="RSYNC_PATH" placeholder="/path/on/server" data={data} onChange={onChange} />
        </div>
      )}

      {system === "rclone" && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Remote Name" field="RCLONE_DRIVE" placeholder="remotename" data={data} onChange={onChange} />
          <Field label="Remote Path" field="RCLONE_PATH" placeholder="remotepath" data={data} onChange={onChange} />
          <Field label="Archive Server" field="ARCHIVE_SERVER" placeholder="8.8.8.8" data={data} onChange={onChange} hint="For connectivity checks" />
        </div>
      )}

      {system === "nfs" && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="NFS Server" field="ARCHIVE_SERVER" placeholder="hostname or IP" data={data} onChange={onChange} />
          <Field label="Export Path" field="SHARE_NAME" placeholder="/volume1/TeslaCam" data={data} onChange={onChange} hint="Exact export path on the NAS" />
        </div>
      )}

      {/* Archive options */}
      {system !== "none" && (
        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-slate-500">
            What to Archive
          </p>
          {[
            { field: "ARCHIVE_SAVEDCLIPS", label: "Saved Clips", def: "true" },
            { field: "ARCHIVE_SENTRYCLIPS", label: "Sentry Clips", def: "true" },
            { field: "ARCHIVE_RECENTCLIPS", label: "Recent Clips", def: "true" },
            { field: "ARCHIVE_TRACKMODECLIPS", label: "Track Mode Clips", def: "true" },
          ].map(({ field, label, def }) => (
            <label key={field} className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={(data[field] ?? def) === "true"}
                onChange={(e) => onChange(field, e.target.checked ? "true" : "false")}
                className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500"
              />
              <span className="text-sm text-slate-300">{label}</span>
            </label>
          ))}
        </div>
      )}
    </div>
  )
}
