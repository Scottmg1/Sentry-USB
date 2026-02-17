import { BrowserRouter, Routes, Route } from "react-router-dom"
import { AppShell } from "@/components/layout/AppShell"
import Dashboard from "@/pages/Dashboard"
import Viewer from "@/pages/Viewer"
import Files from "@/pages/Files"
import Logs from "@/pages/Logs"
import Settings from "@/pages/Settings"

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppShell />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/viewer" element={<Viewer />} />
          <Route path="/files" element={<Files />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/settings" element={<Settings />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
