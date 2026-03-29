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
  Timer,
  LogOut,
  Paintbrush,
  Wifi,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { useAwayMode } from "@/hooks/useAwayMode"
import { useKeepAwake } from "@/hooks/useKeepAwake"
import { useUpdateAvailable } from "@/hooks/useUpdateAvailable"
import { useConnectionStatus } from "@/hooks/useConnectionStatus"
import { useAuth } from "@/hooks/useAuth"

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
  { to: "/community-wraps", icon: Paintbrush, label: "Community Wraps" },
  { to: "/support", icon: MessageCircle, label: "Support" },
  { to: "/terminal", icon: TerminalSquare, label: "Terminal" },
  { to: "/settings", icon: Settings, label: "Settings" },
]

export function Sidebar({ collapsed, onToggle }: SidebarProps) {
  const { status: awayModeStatus } = useAwayMode()
  const { status } = useKeepAwake()
  const isAwake = status.state === "active" || status.state === "pending"
  const { available: updateAvailable } = useUpdateAvailable()
  const { state: connState } = useConnectionStatus()
  const { authRequired, logout } = useAuth()

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
          <span className="text-lg font-semibold tracking-tight text-slate-100" style={{ fontFamily: '"Sora", "DM Sans", system-ui, sans-serif' }}>
            Sentry USB
          </span>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 px-2 py-4">
        {navItems.map((item) => {
          const showBadge = updateAvailable && item.to === "/settings"
          return (
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
              <span className="relative shrink-0">
                <item.icon className="h-5 w-5" />
                {showBadge && (
                  <span className="absolute -right-1 -top-1 h-2 w-2 rounded-full bg-amber-400" />
                )}
              </span>
              {!collapsed && (
                <span className="flex flex-1 items-center justify-between">
                  {item.label}
                  {showBadge && (
                    <span className="rounded-full bg-amber-500/20 px-1.5 py-0.5 text-[10px] font-medium text-amber-400">
                      Update
                    </span>
                  )}
                </span>
              )}
            </NavLink>
          )
        })}
      </nav>

      {/* Connection status */}
      <div className={cn(
        "mx-2 mb-1 flex items-center gap-2 rounded-lg px-3 py-2 text-xs",
        connState === "connected" ? "text-emerald-400" : connState === "reconnecting" ? "text-amber-400" : "text-red-400"
      )}>
        <span className={cn(
          "h-2 w-2 shrink-0 rounded-full",
          connState === "connected" ? "bg-emerald-400" : connState === "reconnecting" ? "bg-amber-400 animate-pulse" : "bg-red-400"
        )} />
        {!collapsed && (
          <span className="opacity-70">
            {connState === "connected" ? "Connected" : connState === "reconnecting" ? "Reconnecting" : "Offline"}
          </span>
        )}
      </div>

      {/* Away Mode indicator */}
      {awayModeStatus.state === "active" && (
        <div className="mx-2 mb-1 flex items-center gap-2 rounded-lg px-3 py-2 text-xs text-blue-400">
          <Wifi className="h-3.5 w-3.5 animate-pulse" />
          {!collapsed && (
            <span className="opacity-70">Away Mode</span>
          )}
        </div>
      )}

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
            <Timer className="h-3.5 w-3.5 animate-pulse" />
          )}
          {!collapsed && (
            <span className="opacity-70">
              {status.state === "active" ? "Keeping awake" : "Waiting for archive..."}
            </span>
          )}
        </div>
      )}

      {/* Logout */}
      {authRequired && (
        <button
          onClick={logout}
          className="mx-2 mb-1 flex items-center gap-2 rounded-lg px-3 py-2 text-xs text-slate-600 transition-colors hover:bg-white/5 hover:text-slate-400"
        >
          <LogOut className="h-3.5 w-3.5 shrink-0" />
          {!collapsed && <span>Logout</span>}
        </button>
      )}

      {/* Collapse toggle */}
      <button
        onClick={onToggle}
        className="mx-2 mb-4 flex items-center justify-center rounded-lg p-2 text-slate-600 transition-colors hover:bg-white/5 hover:text-slate-400"
      >
        {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
      </button>
    </aside>
  )
}
