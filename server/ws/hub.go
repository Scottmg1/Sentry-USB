package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client connected (%d total)", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected (%d total)", len(h.clients))

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) Broadcast(msgType string, data interface{}) {
	msg := Message{Type: msgType, Data: data}
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal broadcast message: %v", err)
		return
	}
	h.broadcast <- b
}

func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	s := websocket.Server{
		Handler: func(conn *websocket.Conn) {
			client := &Client{
				conn: conn,
				send: make(chan []byte, 256),
			}
			hub.register <- client

			done := make(chan struct{})

			// Writer goroutine — also sends pings every 30s
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case msg, ok := <-client.send:
						if !ok {
							return
						}
						if _, err := conn.Write(msg); err != nil {
							return
						}
					case <-ticker.C:
						ping, _ := json.Marshal(Message{Type: "ping", Data: nil})
						if _, err := conn.Write(ping); err != nil {
							return
						}
					case <-done:
						return
					}
				}
			}()

			// Reader goroutine — resets 60s read deadline on every message (including pong)
			buf := make([]byte, 512)
			for {
				conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				_, err := conn.Read(buf)
				if err != nil {
					break
				}
			}

			close(done)
			hub.unregister <- client
			conn.Close()
		},
	}
	s.ServeHTTP(w, r)
}
