import { useEffect } from "react"
import { Database, AlertTriangle, Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { useMigrationStatus } from "@/hooks/useMigrationStatus"
import { useKeepAwake } from "@/hooks/useKeepAwake"

/**
 * MigrationBanner shows a top-of-page banner with a live progress bar
 * while the server is running the one-shot aggregate backfill
 * (first-boot-after-upgrade migration from pre-v2 route rows).
 *
 * Invisible when not migrating. Sticky/prominent when migrating so the
 * user knows why the Drives page may show partial data and doesn't try
 * to force-kill the server.
 *
 * While visible, the banner also reinforces keep-awake from the client
 * side (the backend already pins the car awake, but a mobile browser
 * left open on the car's screen is another way to prevent sleep, so we
 * belt-and-suspenders it).
 */
export function MigrationBanner() {
  const status = useMigrationStatus()
  const { start: keepAwakeStart } = useKeepAwake()

  // Backend already nudges keep-awake every 5 minutes while migration
  // runs. The client side only kicks in if the user's UI session is
  // active -- one short 15-minute request is enough; the user can
  // refresh to re-arm if the migration is unusually long.
  useEffect(() => {
    if (status.active) {
      keepAwakeStart(15).catch(() => {
        // Best-effort. If keep-awake is already active or the call
        // fails, the banner still works.
      })
    }
    // Only fire on transition to active=true; subsequent progress
    // updates shouldn't re-request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status.active])

  if (!status.active && !status.disk_full && !status.error) {
    return null
  }

  // Error/disk-full state: show a persistent warning banner instead of
  // the progress bar.
  if (!status.active && (status.disk_full || status.error)) {
    return (
      <div className="mb-4 flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-200">
        <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
        <div className="flex-1 space-y-1">
          <p className="font-medium">
            {status.disk_full
              ? "Drive data migration paused: disk full"
              : "Drive data migration failed"}
          </p>
          <p className="text-xs text-red-300/80">
            {status.disk_full
              ? "Free space on /mutable, then reboot the Pi. Your drive data is safe -- the migration will resume automatically on the next boot."
              : status.error ||
                "The aggregate backfill ran into an error. It will retry automatically on the next boot."}
          </p>
        </div>
      </div>
    )
  }

  const pct = Math.round(status.pct * 10) / 10
  const pctLabel = pct.toFixed(1)

  return (
    <div
      className={cn(
        "mb-4 overflow-hidden rounded-lg border border-sky-500/30 bg-sky-500/10",
      )}
    >
      <div className="flex items-start gap-3 px-4 py-3 text-sm text-sky-100">
        <div className="relative">
          <Database className="h-4 w-4 shrink-0" />
          <Loader2 className="absolute -right-1 -top-1 h-3 w-3 animate-spin text-sky-300" />
        </div>
        <div className="flex-1 space-y-1.5">
          <p className="font-medium">
            Upgrading drive data — {status.done.toLocaleString()} of{" "}
            {status.total.toLocaleString()} routes ({pctLabel}%)
          </p>
          <p className="text-xs text-sky-200/80">
            First boot after upgrade — your Drives page may show partial
            data until this finishes. Keeping the Tesla awake so the Pi
            stays powered. Safe to leave this tab open or close it; the
            migration runs in the background either way.
          </p>
          <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-sky-900/40">
            <div
              className="h-full bg-sky-400 transition-all duration-500 ease-out"
              style={{ width: `${pct}%` }}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
