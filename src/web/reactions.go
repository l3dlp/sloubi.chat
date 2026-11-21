package web

import (
	"database/sql"
	"log"
	"net/http"
	"regexp"
	"sloubi/src/chat"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

/* Liens et likes. On ne compte que des totaux : combien de fois un lien a été
   cliqué, combien de likes a reçu un message. Personne n'est suivi ; c'est le
   navigateur qui se souvient de ce qu'il a aimé (pour pouvoir retirer un like). */

const maxLinksPerMessage = 20

// les liens d'un message, dans l'ordre : la même règle que linkify() dans app.js
var linkRe = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>"'` + "`" + `]+`)

func extractLinks(text string) []string {
	var out []string
	for _, m := range linkRe.FindAllString(text, maxLinksPerMessage) {
		// la ponctuation qui suit un lien n'en fait pas partie (sauf une parenthèse ouverte dans le lien)
		m = strings.TrimRight(m, ".,;:!?»”’'")
		if strings.HasSuffix(m, ")") && !strings.Contains(m, "(") {
			m = strings.TrimSuffix(m, ")")
		}
		if len(m) > 4 {
			out = append(out, m)
		}
	}
	return out
}

func (s *Store) messageIn(channel string, id int64) (string, bool, error) {
	var text string
	err := s.DB.QueryRow(`SELECT message FROM messages WHERE id = ? AND channel = ?`, id, channel).Scan(&text)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return text, err == nil, err
}

// like ajoute (delta = 1) ou retire (delta = -1) un like ; jamais sous zéro.
func (s *Store) like(id int64, delta int) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.DB.Exec(`INSERT INTO likes (message_id, n) VALUES (?, MAX(0, ?))
		ON CONFLICT(message_id) DO UPDATE SET n = MAX(0, n + ?)`, id, delta, delta); err != nil {
		return 0, err
	}
	var n int
	err := s.DB.QueryRow(`SELECT n FROM likes WHERE message_id = ?`, id).Scan(&n)
	return n, err
}

func (s *Store) click(id int64, idx int) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.DB.Exec(`INSERT INTO link_clicks (message_id, idx, n) VALUES (?, ?, 1)
		ON CONFLICT(message_id, idx) DO UPDATE SET n = n + 1`, id, idx); err != nil {
		return 0, err
	}
	var n int
	err := s.DB.QueryRow(`SELECT n FROM link_clicks WHERE message_id = ? AND idx = ?`, id, idx).Scan(&n)
	return n, err
}

// fillClicks joint les clics aux messages du backlog.
func (s *Store) fillClicks(channel string, msgs []chat.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	byID := make(map[int64]*chat.Message, len(msgs))
	for i := range msgs {
		byID[msgs[i].ID] = &msgs[i]
	}
	rows, err := s.DB.Query(`SELECT c.message_id, c.idx, c.n FROM link_clicks c JOIN messages m ON m.id = c.message_id
		WHERE m.channel = ? AND c.message_id >= ?`, channel, msgs[0].ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var idx, n int
		if err := rows.Scan(&id, &idx, &n); err != nil {
			return err
		}
		if m := byID[id]; m != nil {
			if m.Clicks == nil {
				m.Clicks = map[string]int{}
			}
			m.Clicks[strconv.Itoa(idx)] = n
		}
	}
	return rows.Err()
}

/* ========== Routes ========== */

func (s *server) postLike(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if _, ok := s.enter(c, channel); !ok {
		return
	}
	if !s.reactLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	var in struct {
		ID    int64 `json:"id"`
		Delta int   `json:"delta"`
	}
	if !readJSON(c, 256, &in) {
		return
	}
	if in.Delta != 1 && in.Delta != -1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "delta"})
		return
	}
	if _, found, err := s.store.messageIn(channel, in.ID); err != nil || !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such message"})
		return
	}
	n, err := s.store.like(in.ID, in.Delta)
	if err != nil {
		log.Printf("like: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	s.hub.Broadcast(channel, mustJSON(gin.H{"type": "like", "id": in.ID, "likes": n}))
	c.JSON(http.StatusOK, gin.H{"likes": n})
}

func (s *server) postClick(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if _, ok := s.enter(c, channel); !ok {
		return
	}
	if !s.reactLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	var in struct {
		ID  int64 `json:"id"`
		Idx int   `json:"i"`
	}
	if !readJSON(c, 256, &in) {
		return
	}
	text, found, err := s.store.messageIn(channel, in.ID)
	if err != nil || !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such message"})
		return
	}
	// on ne compte que des liens qui existent vraiment dans ce message
	if in.Idx < 0 || in.Idx >= len(extractLinks(text)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no such link"})
		return
	}
	n, err := s.store.click(in.ID, in.Idx)
	if err != nil {
		log.Printf("click: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	s.hub.Broadcast(channel, mustJSON(gin.H{"type": "click", "id": in.ID, "i": in.Idx, "clicks": n}))
	c.JSON(http.StatusOK, gin.H{"clicks": n})
}
