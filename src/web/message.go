package web

import (
	"encoding/json"
	"log"
	"sloubi/src/chat"
	"sync"
	"time"

	"github.com/gophre/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10
	sendBuffer = 256 // doit contenir le backlog (100) + le post-it + de la marge
)

// client : une connexion WS. Seul writePump écrit sur conn (gorilla ne supporte
// pas les écritures concurrentes) ; les autres passent par send.
type client struct {
	conn    *websocket.Conn
	channel string
	send    chan []byte
}

// Hub : un ensemble de clients par salon.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[*client]struct{}
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*client]struct{})}
}

// Événements WS. Les champs du message restent à plat (l'embed est aplati par
// encoding/json) pour que les anciens clients en cache affichent toujours le chat.
type messageEvent struct {
	Type string `json:"type"`
	chat.Message
}

type postitEvent struct {
	Type string `json:"type"`
	chat.Postit
}

func messageJSON(m chat.Message) []byte { return mustJSON(messageEvent{"message", m}) }
func postitJSON(p chat.Postit) []byte   { return mustJSON(postitEvent{"postit", p}) }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("ws: marshal: %v", err)
		return nil
	}
	return b
}

// join enregistre le client et lui pousse ses premiers événements sous le verrou :
// tout ce qui sera diffusé ensuite arrive après. Un message déjà dans le backlog
// peut arriver une seconde fois ; le client dédoublonne par id.
func (h *Hub) join(c *client, initial func() [][]byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, b := range initial() {
		c.send <- b
	}
	room := h.rooms[c.channel]
	if room == nil {
		room = make(map[*client]struct{})
		h.rooms[c.channel] = room
	}
	room[c] = struct{}{}
}

func (h *Hub) leave(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(c)
}

func (h *Hub) dropLocked(c *client) {
	room := h.rooms[c.channel]
	if _, ok := room[c]; !ok {
		return
	}
	delete(room, c)
	close(c.send)
	if len(room) == 0 {
		delete(h.rooms, c.channel)
	}
}

// Broadcast n'attend jamais un client lent : buffer plein = client déconnecté.
func (h *Hub) Broadcast(channel string, b []byte) {
	if b == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.rooms[channel] {
		select {
		case c.send <- b:
		default:
			h.dropLocked(c)
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case b, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump ne sert qu'à détecter la fermeture et à recevoir les pongs :
// le client n'a rien à envoyer par le WS.
func (c *client) readPump(h *Hub) {
	defer func() {
		h.leave(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}
