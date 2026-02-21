const API_BASE = "/api"

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
    ...options,
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json() as Promise<T>
}

export interface PiStatus {
  cpu_temp: string
  num_snapshots: string
  snapshot_oldest: string
  snapshot_newest: string
  total_space: string
  free_space: string
  uptime: string
  drives_active: string
  wifi_ssid: string
  wifi_freq: string
  wifi_strength: string
  wifi_ip: string
  ether_ip: string
  ether_speed: string
}

export interface PiConfig {
  has_cam: string
  has_music: string
  has_lightshow: string
  has_boombox: string
  uses_ble: string
}

export interface SetupConfig {
  [key: string]: string
}

export interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  modified: string
}

export interface DriveStats {
  drives_count: number
  routes_count: number
  processed_count: number
  total_distance_km: number
  total_distance_mi: number
  total_duration_ms: number
}

export interface DriveStatus {
  running: boolean
  routes_count: number
  processed_count: number
  phase?: string
  current?: number
  total?: number
}

export interface ClipGroup {
  name: string
  clips: ClipEntry[]
}

export interface ClipEntry {
  date: string
  path: string
  files: string[]
}

export const api = {
  getStatus: () => request<PiStatus>("/status"),
  getConfig: () => request<PiConfig>("/config"),
  getSetupConfig: () => request<SetupConfig>("/setup/config"),
  saveSetupConfig: (config: SetupConfig) =>
    request<{ success: boolean }>("/setup/config", {
      method: "PUT",
      body: JSON.stringify(config),
    }),
  runSetup: () =>
    request<{ success: boolean }>("/setup/run", { method: "POST" }),
  getDriveStats: () => request<DriveStats>("/drives/stats"),
  getDriveStatus: () => request<DriveStatus>("/drives/status"),
  getClips: () => request<ClipGroup[]>("/clips"),
  listFiles: (path: string) =>
    request<FileEntry[]>(`/files/ls?path=${encodeURIComponent(path)}`),
  deleteFile: (path: string) =>
    request<{ success: boolean }>(`/files?path=${encodeURIComponent(path)}`, {
      method: "DELETE",
    }),
  createDir: (path: string) =>
    request<{ success: boolean }>("/files/mkdir", {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  reboot: () => request<{ success: boolean }>("/system/reboot", { method: "POST" }),
  toggleDrives: () =>
    request<{ success: boolean }>("/system/toggle-drives", { method: "POST" }),
  triggerSync: () =>
    request<{ success: boolean }>("/system/trigger-sync", { method: "POST" }),
  blePair: () =>
    request<{ success: boolean }>("/system/ble-pair", { method: "POST" }),
  bleStatus: () => request<{ status: string }>("/system/ble-status"),
  refreshDiagnostics: () =>
    request<{ success: boolean }>("/diagnostics/refresh", { method: "POST" }),
}
