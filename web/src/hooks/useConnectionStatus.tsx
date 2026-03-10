import { createContext, useContext, useEffect, useRef, useState } from "react"
import { wsClient } from "@/lib/ws"

export type ConnectionState = "connected" | "reconnecting" | "disconnected"

interface ConnectionContextValue {
  state: ConnectionState
  retry: () => void
}

const ConnectionContext = createContext<ConnectionContextValue>({
  state: "connected",
  retry: () => {},
})

export function useConnectionStatus() {
  return useContext(ConnectionContext)
}

export function ConnectionProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<ConnectionState>("connected")
  const disconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const httpOk = useRef(true)
  const wsOk = useRef(wsClient.isConnected)

  function evaluate() {
    if (wsOk.current && httpOk.current) {
      if (disconnectTimer.current) {
        clearTimeout(disconnectTimer.current)
        disconnectTimer.current = null
      }
      setState("connected")
    } else if (!wsOk.current && !httpOk.current) {
      setState("disconnected")
    } else {
      // One is up, one is down — transitional
      setState("reconnecting")
    }
  }

  // WebSocket state tracking
  useEffect(() => {
    // Ensure WS is connecting
    wsClient.connect()

    const unsub = wsClient.onStatusChange((connected) => {
      wsOk.current = connected
      if (!connected) {
        // Give it time before showing "disconnected"
        if (!disconnectTimer.current) {
          setState("reconnecting")
          disconnectTimer.current = setTimeout(() => {
            disconnectTimer.current = null
            evaluate()
          }, 10_000)
        }
      } else {
        evaluate()
      }
    })

    // Sync initial state
    wsOk.current = wsClient.isConnected

    return () => {
      unsub()
      if (disconnectTimer.current) clearTimeout(disconnectTimer.current)
    }
  }, [])

  // HTTP heartbeat poll
  useEffect(() => {
    let mounted = true

    async function poll() {
      try {
        const controller = new AbortController()
        const timeout = setTimeout(() => controller.abort(), 5000)
        const res = await fetch("/api/status", { signal: controller.signal })
        clearTimeout(timeout)
        if (mounted) {
          httpOk.current = res.ok
          evaluate()
        }
      } catch {
        if (mounted) {
          httpOk.current = false
          evaluate()
        }
      }
    }

    poll()
    const iv = setInterval(poll, 8000)
    return () => { mounted = false; clearInterval(iv) }
  }, [])

  function retry() {
    wsClient.reconnect()
    // Immediate HTTP check
    fetch("/api/status")
      .then((res) => {
        httpOk.current = res.ok
        evaluate()
      })
      .catch(() => {
        httpOk.current = false
        evaluate()
      })
    setState("reconnecting")
  }

  return (
    <ConnectionContext.Provider value={{ state, retry }}>
      {children}
    </ConnectionContext.Provider>
  )
}
