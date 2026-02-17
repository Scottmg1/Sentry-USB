type MessageHandler = (data: unknown) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private handlers: Map<string, Set<MessageHandler>> = new Map()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private url: string

  constructor() {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
    this.url = `${protocol}//${window.location.host}/api/ws`
  }

  connect() {
    if (this.ws?.readyState === WebSocket.OPEN) return

    this.ws = new WebSocket(this.url)

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data) as { type: string; data: unknown }
        const handlers = this.handlers.get(msg.type)
        if (handlers) {
          handlers.forEach((handler) => handler(msg.data))
        }
      } catch {
        // ignore malformed messages
      }
    }

    this.ws.onclose = () => {
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, 3000)
  }

  subscribe(type: string, handler: MessageHandler): () => void {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, new Set())
    }
    this.handlers.get(type)!.add(handler)

    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this.connect()
    }

    return () => {
      this.handlers.get(type)?.delete(handler)
    }
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws?.close()
    this.ws = null
  }
}

export const wsClient = new WebSocketClient()
