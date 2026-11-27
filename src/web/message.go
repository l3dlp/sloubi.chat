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

	// appel (calls.go) : protégé par Hub.mu
	pid    string // identifiant public dans l'appel, tiré au hasard à la connexion
	nick   string
	inCall bool
	muted  bool
	listen bool   // spectateur : sans micro
	tag    string // identifiant de notifications du navigateur (push.go), annoncé par « hello »

	presNick string // le pseudo annoncé par « hello » (presence.go), protégé par Hub.mu

	// seau à jetons des messages entrants : seul readPump y touche
	tokens float64
	last   time.Time
}

// Hub : un ensemble de clients par salon.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[*client]struct{}
	calls map[string]map[*client]struct{} // qui est dans l'appel de chaque salon
	stage map[string][]*client            // qui est en live dans chaque appel (maxLive à la fois, dans l'ordre d'arrivée)
	relay map[*client]*client             // passage de relais : la personne sollicitée → la personne en live qui lui propose sa place

	presence  map[string]map[string]int // salon → pseudo → connexions (presence.go)
	presOut   map[string]*time.Timer    // sorties en attente (presenceGrace)
	presGrace time.Duration             // 0 = presenceGrace ; les tests le raccourcissent
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*client]struct{}), calls: make(map[string]map[*client]struct{}), stage: make(map[string][]*client), relay: make(map[*client]*client)}
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
	if len(h.calls[c.channel]) > 0 {
		h.sendLocked(c, h.rosterJSONLocked(c.channel)) // qui est déjà dans l'appel
	}
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
	h.callLeaveLocked(c) // une connexion perdue quitte l'appel
	h.presenceLeaveLocked(c)
}

// sendLocked n'attend jamais : buffer plein = client déconnecté.
func (h *Hub) sendLocked(c *client, b []byte) {
	select {
	case c.send <- b:
	default:
		h.dropLocked(c)
	}
}

// Evict envoie un dernier événement à tout le salon puis ferme ses connexions :
// writePump vide le buffer avant de fermer, l'événement arrive donc bien.
func (h *Hub) Evict(channel string, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.rooms[channel] {
		if b != nil {
			select {
			case c.send <- b:
			default:
			}
		}
		h.dropLocked(c)
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

// readPump détecte la fermeture, reçoit les pongs, et passe à onMessage ce que
// le client envoie (seulement la signalisation des appels).
func (c *client) readPump(h *Hub, onMessage func(*client, []byte)) {
	defer func() {
		h.leave(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxSignalBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, b, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if onMessage != nil && c.allow() {
			onMessage(c, b)
		}
	}
}

// allow : 20 messages par seconde, rafale de 60 (une négociation envoie une volée de candidats ICE).
func (c *client) allow() bool {
	now := time.Now()
	if c.last.IsZero() {
		c.tokens = 60
	} else {
		c.tokens = min(60, c.tokens+now.Sub(c.last).Seconds()*20)
	}
	c.last = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}
