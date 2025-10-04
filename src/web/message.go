package web

import (
	"sloubi/src/chat"
	"sync"

	"github.com/gophre/websocket"
)

type Hub struct {
	mu        sync.RWMutex
	clients   map[*websocket.Conn]bool
	Broadcast chan chat.Message
}

func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*websocket.Conn]bool),
		Broadcast: make(chan chat.Message, 256),
	}
}

func (h *Hub) Run() {
	for msg := range h.Broadcast {
		h.mu.RLock()
		for c := range h.clients {
			_ = c.WriteJSON(msg)
		}
		h.mu.RUnlock()
	}
}
