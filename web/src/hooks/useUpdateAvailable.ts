import { useEffect, useState } from "react"

export function useUpdateAvailable(): boolean {
  const [updateAvailable, setUpdateAvailable] = useState(false)

  useEffect(() => {
    function check() {
      fetch("/api/system/update-status")
        .then((r) => r.json())
        .then((data) => setUpdateAvailable(!!data.update_available))
        .catch(() => {})
    }

    check()
    const interval = setInterval(check, 5 * 60 * 1000)
    return () => clearInterval(interval)
  }, [])

  return updateAvailable
}
