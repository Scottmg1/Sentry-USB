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

export interface DriveStats {
  drives_count: number
  routes_count: number
  processed_count: number
  total_distance_km: number
  total_distance_mi: number
  total_duration_ms: number
  fsd_engaged_ms: number
  fsd_distance_km: number
  fsd_distance_mi: number
  fsd_percent: number
  fsd_disengagements: number
  fsd_accel_pushes: number
}

export interface DriveStatus {
  running: boolean
  routes_count: number
  processed_count: number
  phase?: string
  current?: number
  total?: number
  archiving?: boolean
}

export interface EventMeta {
  timestamp?: string
  city?: string
  reason?: string
  camera?: string
  latitude?: string
  longitude?: string
}

export interface ClipGroup {
  name: string
  clips: ClipEntry[]
}

export interface ClipEntry {
  date: string
  path: string
  files: string[]
  event?: EventMeta
}

export interface StorageBreakdown {
  cam_size: number
  music_size: number
  lightshow_size: number
  boombox_size: number
  wraps_size: number
  snapshots_size: number
  total_space: number
  free_space: number
}

export const api = {
  getStatus: () => request<PiStatus>("/status"),
  getStorageBreakdown: () => request<StorageBreakdown>("/status/storage"),
  getDriveStats: () => request<DriveStats>("/drives/stats"),
  getDriveStatus: () => request<DriveStatus>("/drives/status"),
}
