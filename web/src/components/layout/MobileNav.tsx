import { NavLink } from "react-router-dom"
import {
  LayoutDashboard,
  Video,
  FolderOpen,
  ScrollText,
  Settings,
  X,
  Shield,
} from "lucide-react"
import { cn } from "@/lib/utils"

interface MobileNavProps {
  open: boolean
  onClose: () => void
}

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/viewer", icon: Video, label: "Viewer" },
  { to: "/files", icon: FolderOpen, label: "Files" },
  { to: "/logs", icon: ScrollText, label: "Logs" },
  { to: "/settings", icon: Settings, label: "Settings" },
]

export function MobileNav({ open, onClose }: MobileNavProps) {
  if (!open) return null

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/60"
        onClick={onClose}
      />

      {/* Drawer */}
      <div className="glass-sidebar fixed left-0 top-0 z-50 flex h-full w-64 flex-col">
        <div className="flex h-16 items-center justify-between px-4">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-500/20">
              <Shield className="h-5 w-5 text-blue-400" />
            </div>
            <span className="text-lg font-semibold tracking-tight text-slate-100">
              SentryUSB
            </span>
          </div>
          <button
            onClick={onClose}
            className="rounded-lg p-1.5 text-slate-500 hover:bg-white/5 hover:text-slate-300"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <nav className="flex-1 space-y-1 px-2 py-4">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              onClick={onClose}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-blue-500/15 text-blue-400"
                    : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
                )
              }
            >
              <item.icon className="h-5 w-5 shrink-0" />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>
      </div>
    </>
  )
}
