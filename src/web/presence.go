package web

import (
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

/* ========== Qui entre, qui sort ==========

Le seul message « système » partagé : « Arthur est entré·e », « Arthur est
sorti·e ». Le navigateur annonce son pseudo par « hello » une fois connecté ;
le serveur compte les connexions par pseudo (plusieurs onglets = une personne)
et ne parle qu'au passage de 0 à 1 et de 1 à 0. Une sortie attend presenceGrace :
un rechargement de page, une reconnexion ou un changement de décor (qui fait
recharger tout le monde) ne disent rien. Le nouveau venu reçoit la liste de
qui est déjà là. */

const presenceGrace = 10 * time.Second

func presenceKey(channel, nick string) string { return channel + "\x00" + nick }

// setPresence : c annonce son pseudo (à la connexion, ou après un changement de pseudo).
func (h *Hub) setPresence(c *client, nick string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.rooms[c.channel][c]; !ok || c.presNick == nick {
		return
	}
	if h.presence == nil {
		h.presence, h.presOut = map[string]map[string]int{}, map[string]*time.Timer{}
	}
	room := h.presence[c.channel]
	if room == nil {
		room = map[string]int{}
		h.presence[c.channel] = room
	}
	old := c.presNick
	if old != "" { // un changement de pseudo : l'ancien nom s'efface sans bruit
		if room[old]--; room[old] <= 0 {
			delete(room, old)
		}
	}
	c.presNick = nick
	room[nick]++
	arriving := room[nick] == 1
	if t := h.presOut[presenceKey(c.channel, nick)]; t != nil { // revenu à temps : personne n'est sorti
		t.Stop()
		delete(h.presOut, presenceKey(c.channel, nick))
		arriving = false
	}
	if old != "" {
		return
	}
	if arriving {
		h.roomLocked(c.channel, c, mustJSON(gin.H{"type": "presence", "op": "in", "nick": nick}))
	}
	here := []string{}
	for n := range room {
		if n != nick {
			here = append(here, n)
		}
	}
	sort.Strings(here)
	h.sendLocked(c, mustJSON(gin.H{"type": "presence", "op": "here", "nicks": here}))
}

// presenceLeaveLocked : la connexion se ferme ; si c'était la dernière de ce pseudo, il sort (après le délai).
func (h *Hub) presenceLeaveLocked(c *client) {
	nick := c.presNick
	room := h.presence[c.channel]
	if nick == "" || room == nil {
		return
	}
	c.presNick = ""
	if room[nick]--; room[nick] > 0 {
		return
	}
	delete(room, nick)
	if len(room) == 0 {
		delete(h.presence, c.channel)
	}
	key, channel := presenceKey(c.channel, nick), c.channel
	var t *time.Timer
	grace := h.presGrace
	if grace == 0 {
		grace = presenceGrace
	}
	t = time.AfterFunc(grace, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.presOut[key] != t {
			return // revenu entre-temps
		}
		delete(h.presOut, key)
		h.roomLocked(channel, nil, mustJSON(gin.H{"type": "presence", "op": "out", "nick": nick}))
	})
	if old := h.presOut[key]; old != nil {
		old.Stop()
	}
	h.presOut[key] = t
}

// roomLocked envoie à tout le salon, sauf à except.
func (h *Hub) roomLocked(channel string, except *client, b []byte) {
	for o := range h.rooms[channel] {
		if o != except {
			h.sendLocked(o, b)
		}
	}
}
