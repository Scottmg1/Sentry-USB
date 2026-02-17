import { HardDrive } from "lucide-react"
import type { StepProps } from "../SetupWizard"

function SizeSlider({
  label,
  field,
  data,
  onChange,
  hint,
  defaultVal,
}: {
  label: string
  field: string
  data: StepProps["data"]
  onChange: StepProps["onChange"]
  hint: string
  defaultVal: string
}) {
  const value = data[field] ?? ""

  return (
    <div className="rounded-lg border border-white/5 bg-white/[0.02] p-4">
      <div className="mb-2 flex items-center justify-between">
        <label className="text-sm font-medium text-slate-300">{label}</label>
        <span className="text-sm font-mono text-blue-400">
          {value || defaultVal}
        </span>
      </div>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(field, e.target.value)}
        placeholder={defaultVal}
        className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
      />
      <p className="mt-1 text-xs text-slate-600">{hint}</p>
    </div>
  )
}

export function StorageStep({ data, onChange }: StepProps) {
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <HardDrive className="h-4 w-4 text-blue-400" />
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-400">
          Drive Sizes
        </h3>
      </div>

      <p className="text-xs text-slate-500">
        Configure the size of each USB drive partition. Use suffixes like G for
        gigabytes, M for megabytes. A 128GB+ SD card is recommended.
      </p>

      <div className="grid gap-3">
        <SizeSlider
          label="Dashcam (CAM_SIZE)"
          field="CAM_SIZE"
          data={data}
          onChange={onChange}
          defaultVal="40G"
          hint="Storage for TeslaCam recordings. 40G recommended. ~7-10GB per hour."
        />
        <SizeSlider
          label="Music (MUSIC_SIZE)"
          field="MUSIC_SIZE"
          data={data}
          onChange={onChange}
          defaultVal=""
          hint="Optional. Leave empty for no music drive."
        />
        <SizeSlider
          label="LightShow (LIGHTSHOW_SIZE)"
          field="LIGHTSHOW_SIZE"
          data={data}
          onChange={onChange}
          defaultVal=""
          hint="Optional. Leave empty for no lightshow drive."
        />
        <SizeSlider
          label="Boombox (BOOMBOX_SIZE)"
          field="BOOMBOX_SIZE"
          data={data}
          onChange={onChange}
          defaultVal=""
          hint="Optional. Leave empty for no boombox drive."
        />
      </div>

      {/* Data Drive */}
      <div>
        <label className="mb-1 block text-sm font-medium text-slate-300">
          External Data Drive (DATA_DRIVE)
        </label>
        <input
          type="text"
          value={data.DATA_DRIVE ?? ""}
          onChange={(e) => onChange("DATA_DRIVE", e.target.value)}
          placeholder="e.g. /dev/sda or /dev/nvme0n1"
          className="w-full rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-100 placeholder-slate-600 outline-none transition focus:border-blue-500/50 focus:ring-1 focus:ring-blue-500/25"
        />
        <p className="mt-1 text-xs text-slate-600">
          Optional. Use an external USB or NVMe drive instead of the SD card.
          WARNING: The drive will be wiped.
        </p>
      </div>

      {/* ExFAT toggle */}
      <label className="flex cursor-pointer items-center gap-2">
        <input
          type="checkbox"
          checked={data.USE_EXFAT === "true"}
          onChange={(e) =>
            onChange("USE_EXFAT", e.target.checked ? "true" : "false")
          }
          className="h-4 w-4 rounded border-white/20 bg-white/5 accent-blue-500"
        />
        <span className="text-sm text-slate-300">Use ExFAT filesystem</span>
        <span className="text-xs text-slate-600">
          (not recommended for prebuilt image)
        </span>
      </label>
    </div>
  )
}
