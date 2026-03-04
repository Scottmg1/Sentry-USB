import { NavLink } from "react-router-dom"
import {
  LayoutDashboard,
  Video,
  FolderOpen,
  ScrollText,
  MapPin,
  MessageCircle,
  Settings,
  ChevronLeft,
  ChevronRight,
  Shield,
  TerminalSquare,
  HeartPulse,
  Clock,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { useKeepAwake } from "@/hooks/useKeepAwake"

interface SidebarProps {
  collapsed: boolean
  onToggle: () => void
}

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/viewer", icon: Video, label: "Viewer" },
  { to: "/files", icon: FolderOpen, label: "Files" },
  { to: "/logs", icon: ScrollText, label: "Logs" },
  { to: "/drives", icon: MapPin, label: "Drives" },
  { to: "/support", icon: MessageCircle, label: "Support" },
  { to: "/terminal", icon: TerminalSquare, label: "Terminal" },
  { to: "/settings", icon: Settings, label: "Settings" },
]

export function Sidebar({ collapsed, onToggle }: SidebarProps) {
  const { status } = useKeepAwake()
  const isAwake = status.state === "active" || status.state === "pending"

  return (
    <aside
      className={cn(
        "glass-sidebar fixed left-0 top-0 z-30 flex h-full flex-col transition-all duration-300",
        collapsed ? "w-16" : "w-56"
      )}
    >
      {/* Logo */}
      <div className="flex h-16 items-center gap-3 px-4">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-500/20">
          <Shield className="h-5 w-5 text-blue-400" />
        </div>
        {!collapsed && (
          <span className="text-lg font-semibold tracking-tight text-slate-100">
            Sentry USB
          </span>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 px-2 py-4">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === "/"}
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
            {!collapsed && <span>{item.label}</span>}
          </NavLink>
        ))}
      </nav>

      {/* Keep-awake indicator */}
      {isAwake && (
        <div className={cn(
          "mx-2 mb-2 flex items-center gap-2 rounded-lg px-3 py-2 text-xs",
          status.state === "active"
            ? "text-rose-400"
            : "text-amber-400"
        )}>
          {status.state === "active" ? (
            <HeartPulse className="h-3.5 w-3.5 animate-pulse" />
          ) : (
            <Clock className="h-3.5 w-3.5 animate-pulse" />
          )}
          {!collapsed && (
            <span className="opacity-70">
              {status.state === "active" ? "Keeping awake" : "Waiting..."}
            </span>
          )}
        </div>
      )}

      {/* Collapse toggle */}
      <button
        onClick={onToggle}
        className="mx-2 mb-4 flex items-center justify-center rounded-lg p-2 text-slate-500 transition-colors hover:bg-white/5 hover:text-slate-300"
      >
        {collapsed ? (
          <ChevronRight className="h-4 w-4" />
        ) : (
          <ChevronLeft className="h-4 w-4" />
        )}
      </button>
    </aside>
  )
}
