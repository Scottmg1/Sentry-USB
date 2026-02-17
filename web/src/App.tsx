import { useEffect, useState } from "react"
import { BrowserRouter, Routes, Route } from "react-router-dom"
import { AppShell } from "@/components/layout/AppShell"
import Dashboard from "@/pages/Dashboard"
import Viewer from "@/pages/Viewer"
import Files from "@/pages/Files"
import Logs from "@/pages/Logs"
import Settings from "@/pages/Settings"
import Drives from "@/pages/Drives"
import { SetupWizard } from "@/components/setup/SetupWizard"

export default function App() {
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)

  useEffect(() => {
    fetch("/api/setup/status")
      .then((r) => r.json())
      .then((data) => setNeedsSetup(!data.setup_finished))
      .catch(() => setNeedsSetup(false))
  }, [])

  // Still checking — show nothing (brief flash)
  if (needsSetup === null) {
    return (
      <div className="flex h-screen items-center justify-center bg-slate-950">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-blue-500 border-t-transparent" />
      </div>
    )
  }

  // Setup not done — show wizard full screen
  if (needsSetup) {
    return (
      <div className="min-h-screen bg-slate-950 p-4">
        <SetupWizard onClose={() => setNeedsSetup(false)} />
      </div>
    )
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppShell />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/viewer" element={<Viewer />} />
          <Route path="/files" element={<Files />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/drives" element={<Drives />} />
          <Route path="/settings" element={<Settings />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
