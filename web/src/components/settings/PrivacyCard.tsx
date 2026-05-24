import { useEffect, useState } from "react"
import { ShieldCheck, Check, X, Loader2 } from "lucide-react"

/**
 * Settings → Privacy card for the legacy Go SentryUSB web UI.
 *
 * Mirrors the Rusty client's PrivacyTab. Lets users review the disclosure
 * and flip the analytics opt-in at any time. This is the Art. 21 right-
 * to-object mechanism — automated, no email needed.
 *
 * Go preferences are stored as map[string]string so the value is the
 * string "true" / "false" rather than the boolean Rusty uses.
 */
export function PrivacyCard() {
  const [choice, setChoice] = useState<boolean | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    fetch("/api/config/preference?key=analytics_opt_in")
      .then((r) => r.json())
      .then((data) => {
        if (data?.value === "true") setChoice(true)
        else if (data?.value === "false") setChoice(false)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }, [])

  async function persist(value: boolean) {
    setSaving(true)
    setError(null)
    try {
      const res = await fetch("/api/config/preference", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ key: "analytics_opt_in", value: value ? "true" : "false" }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setChoice(value)
    } catch (e) {
      setError(`Couldn't save: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="rounded-xl border border-white/10 bg-white/[0.02]">
      <div className="flex items-center gap-3 border-b border-white/5 px-3 py-2.5">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-emerald-500/20">
          <ShieldCheck className="h-4 w-4 text-emerald-400" />
        </div>
        <div className="flex-1">
          <h3 className="text-sm font-semibold text-slate-100">Privacy</h3>
          <p className="text-xs text-slate-500">
            What we send, when, and why — plus your opt-in.
          </p>
        </div>
      </div>

      <div className="space-y-4 p-3">
        {/* Disclosure table — Art. 13 at the point of (ongoing) collection */}
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-slate-400">
            What this device sends
          </p>
          <div className="divide-y divide-white/5 rounded-lg border border-white/5 bg-white/[0.01]">
            <FlowRow
              when="Daily update check"
              what="Version, architecture, board model"
              note="Device identifier only if opted in below."
            />
            <FlowRow
              when="Once per install"
              what="Empty ping (no body, no identifier)"
              note="Anonymous gross-install counter."
            />
            <FlowRow
              when="Wraps / lock chime submissions"
              what="The file + your IP for rate-limiting"
              note="No device fingerprint sent."
            />
            <FlowRow
              when="iOS push pairing"
              what="A random pairing ID"
              note="Not tied to your hardware."
            />
          </div>
        </div>

        {/* Opt-in — explicit affirmative action, no pre-tick */}
        <div>
          <p className="text-sm font-semibold text-slate-200">
            Help us count new installs?
          </p>
          <p className="mt-1 text-xs leading-relaxed text-slate-400">
            If you opt in, daily update checks include a one-way hashed
            device ID so we can count unique installs without double-counting
            reinstalls. Default is opted out.
          </p>

          <div className="mt-3 flex flex-col gap-2 sm:flex-row">
            <button
              type="button"
              disabled={saving || !loaded}
              onClick={() => persist(true)}
              className={
                "flex flex-1 items-center justify-center gap-2 rounded-lg border px-3 py-2 text-xs font-medium transition-colors disabled:opacity-50 " +
                (choice === true
                  ? "border-emerald-400/60 bg-emerald-500/15 text-emerald-200"
                  : "border-white/10 bg-white/[0.02] text-slate-300 hover:border-white/20 hover:bg-white/[0.05]")
              }
            >
              {saving && choice !== true ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Check className="h-3.5 w-3.5" />
              )}
              Opted in
            </button>
            <button
              type="button"
              disabled={saving || !loaded}
              onClick={() => persist(false)}
              className={
                "flex flex-1 items-center justify-center gap-2 rounded-lg border px-3 py-2 text-xs font-medium transition-colors disabled:opacity-50 " +
                (choice === false
                  ? "border-slate-400/60 bg-slate-500/15 text-slate-200"
                  : "border-white/10 bg-white/[0.02] text-slate-300 hover:border-white/20 hover:bg-white/[0.05]")
              }
            >
              {saving && choice !== false ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <X className="h-3.5 w-3.5" />
              )}
              Opted out
            </button>
          </div>

          {choice === null && loaded && !error && (
            <p className="mt-2 text-[11px] text-slate-500">
              Not set. Default is opted out — no choice means no tracking.
            </p>
          )}
          {error && (
            <p className="mt-2 text-[11px] text-rose-400">{error}</p>
          )}
        </div>

        <div className="flex flex-col gap-1 border-t border-white/5 pt-3">
          <a
            href="https://sentry-six.com/privacy"
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-blue-400 hover:text-blue-300"
          >
            Full privacy policy ↗
          </a>
          <a
            href="https://github.com/Scottmg1/Sentry-USB/blob/main/wiki/Privacy.md"
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-blue-400 hover:text-blue-300"
          >
            Wiki: what each flow does ↗
          </a>
        </div>
      </div>
    </div>
  )
}

function FlowRow({
  when,
  what,
  note,
}: {
  when: string
  what: string
  note?: string
}) {
  return (
    <div className="p-2">
      <p className="text-xs font-semibold text-slate-300">{when}</p>
      <p className="mt-0.5 text-[11px] text-slate-400">{what}</p>
      {note && (
        <p className="mt-0.5 text-[11px] italic text-slate-500/80">{note}</p>
      )}
    </div>
  )
}
