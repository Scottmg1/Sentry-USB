import { useEffect, useState } from "react"
import { wsClient } from "@/lib/ws"

/**
 * Shape of /api/drives/migration-status and of the
 * "drives.migration.progress" WS broadcast. Kept identical so the hook
 * can merge both sources without branching on origin.
 */
export interface MigrationStatus {
  active: boolean
  done: number
  total: number
  pct: number
  error: string
  disk_full: boolean
}

const initialStatus: MigrationStatus = {
  active: false,
  done: 0,
  total: 0,
  pct: 0,
  error: "",
  disk_full: false,
}

/**
 * useMigrationStatus wires the one-shot aggregate backfill state into
 * React. Polls /api/drives/migration-status every 3 seconds and also
 * listens for "drives.migration.progress" WebSocket broadcasts; WS
 * events win when they arrive faster.
 *
 * While the migration is inactive the hook short-polls at 10s so we
 * catch a backfill that was kicked off by an upload-restore after page
 * load without spamming the endpoint.
 *
 * The backend drives this hook, not the other way around -- there is
 * no client-initiated "start migration" call; the server starts it
 * automatically when it detects NULL aggregate columns during Load().
 */
export function useMigrationStatus(): MigrationStatus {
  const [status, setStatus] = useState<MigrationStatus>(initialStatus)

  useEffect(() => {
    let cancelled = false
    let pollTimer: ReturnType<typeof setTimeout> | null = null

    const poll = async () => {
      try {
        const res = await fetch("/api/drives/migration-status")
        if (!res.ok) throw new Error("migration-status HTTP " + res.status)
        const data = (await res.json()) as MigrationStatus
        if (!cancelled) setStatus(data)
      } catch {
        // Silent: the banner just stays hidden if we can't reach the
        // backend. ConnectionBanner handles the user-visible "offline"
        // state separately.
      }
      if (cancelled) return
      // Poll faster while active so the progress bar feels live; slow
      // down to a heartbeat otherwise.
      pollTimer = setTimeout(poll, status.active ? 3000 : 10000)
    }

    poll()

    // WS fast path. The backend broadcasts on every batch commit, so
    // during an active migration the user sees sub-second updates.
    const unsub = wsClient.subscribe("drives.migration.progress", (data) => {
      if (cancelled) return
      const d = data as Partial<MigrationStatus>
      setStatus((prev) => ({
        active: d.active ?? prev.active,
        done: d.done ?? prev.done,
        total: d.total ?? prev.total,
        pct: computePct(d.done ?? prev.done, d.total ?? prev.total),
        error: d.error ?? prev.error,
        disk_full: d.disk_full ?? prev.disk_full,
      }))
    })

    return () => {
      cancelled = true
      if (pollTimer) clearTimeout(pollTimer)
      unsub()
    }
    // status.active intentionally excluded: we read it inside poll() to
    // pick the next delay, but we don't want to re-subscribe on every
    // transition. Re-subscribing would drop in-flight WS handlers.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return status
}

function computePct(done: number, total: number): number {
  if (total <= 0) return 0
  const pct = (100 * done) / total
  if (pct > 100) return 100
  if (pct < 0) return 0
  return pct
}
