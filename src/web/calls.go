package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

/* Appels de groupe, dans les lieux gardés par un mot de passe.

   Chacun envoie son son à chacun (maillage pair à pair, 6 personnes au plus) ;
   la vidéo, elle, va de deux personnes au plus vers les autres : la scène
   (le live, maxLive places) est à qui la prend quand une place est libre. Le serveur ne voit passer
   aucun média. Il fait trois choses :
   - il relaie la signalisation WebRTC (offres, réponses, candidats ICE) entre
     les membres de l'appel, par le WebSocket du salon : l'appel est donc
     exactement aussi privé que le salon ;
   - il arbitre la scène : deux personnes en live au plus par appel ;
   - il donne des identifiants TURN éphémères pour le coturn partagé avec
     BeamCam (même secret, API REST de coturn). */

const (
	maxCallers     = 6
	maxLive        = 2        // en live en même temps : un duo, chacun envoie sa vidéo à tous (maillage, pas de serveur média)
	maxSignalBytes = 32 << 10 // une offre SDP avec vidéo tient en ~10 Ko
	turnTTL        = 6 * time.Hour
)

type turnConfig struct {
	secret []byte
	host   string
	port   string
}

// loadTurn : TURN_SECRET, ou le fichier TURN_SECRET_FILE (celui de BeamCam). Sans secret,
// les appels passent en STUN seul : ça marche presque partout, sauf derrière les NAT stricts.
func loadTurn() turnConfig {
	t := turnConfig{host: os.Getenv("TURN_HOST"), port: os.Getenv("TURN_PORT")}
	if t.host == "" {
		t.host = "beamcam.live"
	}
	if t.port == "" {
		t.port = "3478"
	}
	secret := os.Getenv("TURN_SECRET")
	if secret == "" {
		if f := os.Getenv("TURN_SECRET_FILE"); f != "" {
			b, err := os.ReadFile(f)
			if err != nil {
				log.Printf("turn: %v", err)
			}
			secret = string(b)
		}
	}
	if secret = strings.TrimSpace(secret); secret != "" {
		t.secret = []byte(secret)
		log.Printf("turn: relais %s:%s", t.host, t.port)
	} else {
		log.Printf("turn: pas de secret, appels en STUN seul")
	}
	return t
}

// turn : les serveurs ICE d'un appel. Réservé aux membres d'un lieu gardé.
func (s *server) turnCreds(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	r, ok := s.enter(c, channel)
	if !ok {
		return
	}
	if !r.Locked() {
		c.JSON(http.StatusForbidden, gin.H{"error": "calls need a password"})
		return
	}
	c.Header("Cache-Control", "no-store")
	t := s.turn
	ice := []gin.H{{"urls": fmt.Sprintf("stun:%s:%s", t.host, t.port)}}
	if len(t.secret) > 0 {
		// username = date d'expiration, mot de passe = base64(HMAC-SHA1(secret, username)) : ce que coturn attend
		user := fmt.Sprintf("%d:sloubi", time.Now().Add(turnTTL).Unix())
		m := hmac.New(sha1.New, t.secret)
		m.Write([]byte(user))
		ice = append(ice, gin.H{
			"urls": []string{
				fmt.Sprintf("turn:%s:%s?transport=udp", t.host, t.port),
				fmt.Sprintf("turn:%s:%s?transport=tcp", t.host, t.port),
			},
			"username":   user,
			"credential": base64.StdEncoding.EncodeToString(m.Sum(nil)),
		})
	}
	c.JSON(http.StatusOK, gin.H{"iceServers": ice, "ttl": int(turnTTL.Seconds())})
}

func newPeerID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

/* ========== Messages du client ========== */

type callIn struct {
	Type   string          `json:"type"`
	Op     string          `json:"op"`
	To     string          `json:"to"`
	Nick   string          `json:"nick"`
	Muted  bool            `json:"muted"`
	On     bool            `json:"on"`
	Listen bool            `json:"listen"`
	Push   string          `json:"push"`
	Data   json.RawMessage `json:"data"`
}

// onClientMessage reçoit ce qu'un client envoie sur le WebSocket du salon.
func (s *server) onClientMessage(c *client, raw []byte) {
	var m callIn
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	if m.Type == "hello" { // le navigateur dit quel abonnement aux notifications est le sien, et son pseudo
		if pushTagRe.MatchString(m.Push) {
			s.hub.setTag(c, m.Push)
		}
		if m.Nick != "" {
			if nick, reason := s.cleanNickname(m.Nick); reason == "" {
				s.hub.setPresence(c, nick)
			}
		}
		return
	}
	if m.Type != "call" {
		return
	}
	switch m.Op {
	case "join":
		// le lieu a pu perdre son mot de passe depuis l'ouverture du socket (normalement il nous a expulsés)
		r, err := s.store.room(c.channel)
		if err != nil || !r.Locked() {
			s.hub.callSend(c, callEvent("denied"))
			return
		}
		nick, reason := s.cleanNickname(m.Nick)
		if reason != "" {
			s.hub.callSend(c, callEvent("nick-required"))
			return
		}
		// le premier arrivé lance l'appel : on prévient ceux qui ne sont pas là
		if present, started := s.hub.callJoin(c, nick, m.Listen); started {
			go s.notifyCall(c.channel, nick, present)
		}
	case "leave":
		s.hub.callLeave(c)
	case "signal":
		if len(m.Data) > 0 && m.To != "" {
			s.hub.callRelay(c, m.To, m.Data)
		}
	case "state":
		s.hub.callState(c, m.Muted)
	case "live":
		s.hub.callLive(c, m.On)
	case "handoff": // la personne en live passe la scène à quelqu'un (qui doit accepter)
		s.hub.callHandoff(c, m.To)
	case "handoff-decline":
		s.hub.callDecline(c)
	}
}

func callEvent(op string) []byte { return mustJSON(gin.H{"type": "call", "op": op}) }

/* ========== L'appel, côté Hub (tout sous h.mu) ========== */

type callPeer struct {
	ID     string `json:"id"`
	Nick   string `json:"nick"`
	Muted  bool   `json:"muted"`
	Listen bool   `json:"listen,omitempty"`
}

func (h *Hub) peersLocked(channel string, except *client) []callPeer {
	out := []callPeer{}
	for c := range h.calls[channel] {
		if c != except {
			out = append(out, callPeer{c.pid, c.nick, c.muted, c.listen})
		}
	}
	return out
}

func (h *Hub) rosterJSONLocked(channel string) []byte {
	lives := []string{}
	for _, c := range h.stage[channel] {
		lives = append(lives, c.pid)
	}
	live := "" // l'ancien champ, pour les pages encore en cache : la première personne en live
	if len(lives) > 0 {
		live = lives[0]
	}
	return mustJSON(gin.H{"type": "call", "op": "roster", "peers": h.peersLocked(channel, nil), "live": live, "lives": lives})
}

func (h *Hub) onStageLocked(c *client) bool {
	for _, o := range h.stage[c.channel] {
		if o == c {
			return true
		}
	}
	return false
}

// leaveStageLocked : c descend de scène ; ses propositions de relais (reçues ou faites) tombent.
func (h *Hub) leaveStageLocked(c *client) {
	st := h.stage[c.channel]
	for i, o := range st {
		if o == c {
			st = append(st[:i:i], st[i+1:]...)
			break
		}
	}
	if len(st) == 0 {
		delete(h.stage, c.channel)
	} else {
		h.stage[c.channel] = st
	}
	delete(h.relay, c)
	for to, from := range h.relay {
		if from == c {
			delete(h.relay, to)
		}
	}
}

// rosterLocked dit à tout le salon (pas seulement à l'appel) qui est en ligne.
func (h *Hub) rosterLocked(channel string) {
	b := h.rosterJSONLocked(channel)
	var slow []*client
	for c := range h.rooms[channel] {
		select {
		case c.send <- b:
		default:
			slow = append(slow, c)
		}
	}
	for _, c := range slow {
		h.dropLocked(c)
	}
}

func (h *Hub) callSend(c *client, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.rooms[c.channel][c]; ok {
		h.sendLocked(c, b)
	}
}

// callJoin : le nouveau venu reçoit la liste des présents et leur envoie les offres.
// started : il est le premier, l'appel commence ; present : les navigateurs déjà dans le salon.
func (h *Hub) callJoin(c *client, nick string, listen bool) (present map[string]bool, started bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.rooms[c.channel][c]; !ok || c.inCall {
		return nil, false
	}
	call := h.calls[c.channel]
	if len(call) >= maxCallers {
		h.sendLocked(c, callEvent("full"))
		return nil, false
	}
	started = len(call) == 0
	if call == nil {
		call = make(map[*client]struct{})
		h.calls[c.channel] = call
	}
	peers := h.peersLocked(c.channel, nil)
	c.inCall, c.nick, c.muted, c.listen = true, nick, listen, listen
	call[c] = struct{}{}
	h.sendLocked(c, mustJSON(gin.H{"type": "call", "op": "joined", "you": c.pid, "peers": peers}))
	h.rosterLocked(c.channel)
	present = map[string]bool{}
	for o := range h.rooms[c.channel] {
		if o.tag != "" {
			present[o.tag] = true
		}
	}
	return present, started
}

func (h *Hub) setTag(c *client, tag string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.tag = tag
}

func (h *Hub) callLeave(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callLeaveLocked(c)
}

func (h *Hub) callLeaveLocked(c *client) {
	if !c.inCall {
		return
	}
	c.inCall = false
	h.leaveStageLocked(c) // qui raccroche descend de scène
	call := h.calls[c.channel]
	delete(call, c)
	if len(call) == 0 {
		delete(h.calls, c.channel)
	}
	h.rosterLocked(c.channel)
}

// callRelay transmet la signalisation d'un membre de l'appel à un autre, et à lui seul.
func (h *Hub) callRelay(from *client, to string, data json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !from.inCall {
		return
	}
	for c := range h.calls[from.channel] {
		if c.pid == to && c != from {
			h.sendLocked(c, mustJSON(gin.H{"type": "call", "op": "signal", "from": from.pid, "data": data}))
			return
		}
	}
}

func (h *Hub) callState(c *client, muted bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !c.inCall || c.muted == muted || c.listen {
		return
	}
	c.muted = muted
	h.rosterLocked(c.channel)
}

// callLive : monter sur scène s'il reste une place (sinon refusé), ou en descendre.
func (h *Hub) callLive(c *client, on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !c.inCall || c.listen {
		return
	}
	switch from := h.relay[c]; {
	case on && h.onStageLocked(c):
		return
	case on && from != nil && h.onStageLocked(from):
		// on lui a passé une place : il la prend, la personne qui la lui donne en descend
		h.leaveStageLocked(from)
		h.stage[c.channel] = append(h.stage[c.channel], c)
		delete(h.relay, c)
	case on && len(h.stage[c.channel]) < maxLive:
		h.stage[c.channel] = append(h.stage[c.channel], c)
		delete(h.relay, c)
	case on:
		h.sendLocked(c, callEvent("stage-busy"))
		return
	case h.onStageLocked(c):
		h.leaveStageLocked(c)
	default:
		return
	}
	h.rosterLocked(c.channel)
}

// callHandoff : une personne en live propose sa place à un membre de l'appel qui a un micro (et n'est pas déjà en live).
func (h *Hub) callHandoff(c *client, to string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.onStageLocked(c) {
		return
	}
	for o := range h.calls[c.channel] {
		if o.pid == to && o != c && !o.listen && !h.onStageLocked(o) {
			h.relay[o] = c
			h.sendLocked(o, mustJSON(gin.H{"type": "call", "op": "handoff", "from": c.pid, "nick": c.nick}))
			return
		}
	}
}

// callDecline : la personne sollicitée refuse ; la personne en live garde sa place et le sait.
func (h *Hub) callDecline(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	from := h.relay[c]
	if from == nil {
		return
	}
	delete(h.relay, c)
	if h.onStageLocked(from) {
		h.sendLocked(from, mustJSON(gin.H{"type": "call", "op": "handoff-declined", "nick": c.nick}))
	}
}
