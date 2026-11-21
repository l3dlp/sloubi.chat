package web

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sloubi/src/chat"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/unicode/norm"
)

/* ========== Les trois lieux ========== */

// Theme : un décor et son petit jeu. Le texte vient d'ici pour que la page soit
// juste dès le premier rendu, sans attendre le JS.
type Theme struct {
	ID           string
	Name         string // « Taverne »
	Description  string
	CounterLabel string
	Placeholder  string
	Welcome      string
	Color        string // theme-color du navigateur
	FX           string // la scène Three.js, dans assets/
}

const DefaultTheme = "taverne"

var themes = map[string]Theme{
	"taverne": {
		ID: "taverne", Name: "Taverne",
		Description:  "Une taverne enfumée où l'on compte les sloubis.",
		CounterLabel: "sloubis comptés",
		Placeholder:  "Écrivez « sloubi 1 » pour compter...",
		Welcome:      "La fumée s'écarte. Bienvenue, voyageur.",
		Color:        "#140b07",
		FX:           "room-fx.js",
	},
	"foret": {
		ID: "foret", Name: "Forêt",
		Description:  "Une forêt nourricière où chaque microbiote est une encyclopédie.",
		CounterLabel: "graines semées",
		Placeholder:  "Sème une idée… un #mot fait pousser un microbiote",
		Welcome:      "Les fougères s'écartent. Bienvenue, promeneur.",
		Color:        "#08130c",
		FX:           "forest-fx.js",
	},
	"moulin": {
		ID: "moulin", Name: "Moulin",
		Description:  "Un moulin au bord de la rivière : on discute au bord de l'eau.",
		CounterLabel: "tours de roue",
		Placeholder:  "Dis quelque chose au fil de l'eau…",
		Welcome:      "La roue grince, l'eau chante. Assieds-toi sur la berge.",
		Color:        "#0d1a24",
		FX:           "mill-fx.js",
	},
	"cyber": {
		ID: "cyber", Name: "Cyberpunk",
		Description:  "Un fanzine photocopié de 1984 : collages, néons, bandes VHS.",
		CounterLabel: "paquets piratés",
		Placeholder:  "Colle ton message sur le mur…",
		Welcome:      "SYS> Connexion établie. Personne ne vous a vus entrer. Enfin, presque.",
		Color:        "#0d0614",
		FX:           "cyber-fx.js",
	},
	"kawaii": {
		ID: "kawaii", Name: "Kawaii",
		Description:  "Un ciel pastel où tout a des yeux, même le bouton envoyer.",
		CounterLabel: "câlins envoyés",
		Placeholder:  "Dis un truc gentil…",
		Welcome:      "Coucou ! Ici tout le monde est gentil. Même le bouton. Surtout le bouton.",
		Color:        "#ffe4f1",
		FX:           "kawaii-fx.js",
	},
	"bd": {
		ID: "bd", Name: "BD",
		Description:  "Une planche franco-belge en ligne claire : bulles, cartouches, onomatopées.",
		CounterLabel: "cases dessinées",
		Placeholder:  "Ta réplique… (un BOUM ! fait du bruit)",
		Welcome:      "Pendant ce temps, dans un salon pas comme les autres…",
		Color:        "#fdf6e3",
		FX:           "bd-fx.js",
	},
}

// themeOrder : l'ordre des cartes dans les réglages et sur l'accueil.
var themeOrder = []string{"taverne", "foret", "moulin", "cyber", "kawaii", "bd"}

func themeList() []Theme {
	out := make([]Theme, len(themeOrder))
	for i, id := range themeOrder {
		out[i] = themes[id]
	}
	return out
}

func themeOf(id string) Theme {
	if t, ok := themes[id]; ok {
		return t
	}
	return themes[DefaultTheme]
}

/* ========== Stockage ========== */

// Room : les réglages d'un lieu. Un lieu sans créateur (fondé avant les réglages,
// comme /chante) ne peut pas être réglé.
type Room struct {
	Exists    bool
	Theme     string
	OwnerHash string
	PwHash    string
}

func (r Room) Locked() bool { return r.PwHash != "" }

func (s *Store) room(channel string) (Room, error) {
	r := Room{Theme: DefaultTheme}
	var owner, pw sql.NullString
	err := s.DB.QueryRow(`SELECT theme, owner_hash, pw_hash FROM channels WHERE name = ?`, channel).Scan(&r.Theme, &owner, &pw)
	switch err {
	case sql.ErrNoRows:
		return r, nil
	case nil:
		r.Exists, r.OwnerHash, r.PwHash = true, owner.String, pw.String
		return r, nil
	default:
		return r, err
	}
}

// createRoom fonde un lieu depuis les réglages. created est faux si quelqu'un l'a fondé entre-temps.
func (s *Store) createRoom(channel, ownerHash, theme string) (bool, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	res, err := s.DB.Exec(`INSERT OR IGNORE INTO channels (name, created_at, theme, owner_hash) VALUES (?,?,?,?)`,
		channel, now(), theme, ownerHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// claimOwner donne le lieu à qui vient de le fonder par un message ou le post-it.
func (s *Store) claimOwner(channel, ownerHash string) error {
	_, err := s.DB.Exec(`UPDATE channels SET owner_hash = ? WHERE name = ? AND owner_hash IS NULL`, ownerHash, channel)
	return err
}

func (s *Store) setTheme(channel, theme string) error {
	_, err := s.DB.Exec(`UPDATE channels SET theme = ? WHERE name = ?`, theme, channel)
	return err
}

// setPassword pose (ou retire, avec "") le mot de passe haché.
func (s *Store) setPassword(channel, pwHash string) error {
	_, err := s.DB.Exec(`UPDATE channels SET pw_hash = NULLIF(?, '') WHERE name = ?`, pwHash, channel)
	return err
}

// roomStats : nombre de messages, dernier id (pour ne pas recompter le backlog) et dernier sloubi.
func (s *Store) roomStats(channel string) (messages int, lastID int64, sloubi int, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(id), 0), COALESCE(MAX(sloubi_number), 0) FROM messages WHERE channel = ?`,
		channel).Scan(&messages, &lastID, &sloubi)
	return
}

func (s *Store) loadSecret() ([]byte, error) {
	var b []byte
	err := s.DB.QueryRow(`SELECT value FROM meta WHERE key = 'cookie_secret'`).Scan(&b)
	if err == nil && len(b) >= 32 {
		return b, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	b = make([]byte, 32)
	rand.Read(b)
	if _, err := s.DB.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('cookie_secret', ?)`, b); err != nil {
		return nil, err
	}
	return b, nil
}

/* ========== Microbiotes (les #mots de la forêt) ========== */

const maxTagsPerMessage = 5

// un # en début de mot, 2 à 32 lettres, chiffres, _ ou -
var tagRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_&#])#([\p{L}\p{N}_-]{2,32})`)

func extractTags(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range tagRe.FindAllStringSubmatch(text, -1) {
		tag := strings.ToLower(norm.NFC.String(strings.Trim(m[1], "-_")))
		if utf8.RuneCountInString(tag) < 2 || seen[tag] {
			continue
		}
		seen[tag] = true
		if out = append(out, tag); len(out) == maxTagsPerMessage {
			break
		}
	}
	return out
}

func (s *Store) createTags() error {
	if _, err := s.DB.Exec(`
	CREATE TABLE tags (
		channel TEXT NOT NULL,
		tag TEXT NOT NULL,
		message_id INTEGER NOT NULL
	);
	CREATE INDEX idx_tags_channel_tag ON tags(channel, tag, message_id);`); err != nil {
		return err
	}
	rows, err := s.DB.Query(`SELECT id, channel, message FROM messages WHERE message LIKE '%#%'`)
	if err != nil {
		return err
	}
	type row struct {
		id      int64
		channel string
		tags    []string
	}
	var todo []row
	for rows.Next() {
		var r row
		var text string
		if err := rows.Scan(&r.id, &r.channel, &text); err != nil {
			rows.Close()
			return err
		}
		if r.tags = extractTags(text); len(r.tags) > 0 {
			todo = append(todo, r)
		}
	}
	rows.Close()
	for _, r := range todo {
		for _, t := range r.tags {
			if _, err := s.DB.Exec(`INSERT INTO tags (channel, tag, message_id) VALUES (?,?,?)`, r.channel, t, r.id); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func (s *Store) tags(channel string, limit int) ([]TagCount, error) {
	rows, err := s.DB.Query(`SELECT tag, COUNT(*) FROM tags WHERE channel = ? GROUP BY tag ORDER BY 2 DESC, MAX(message_id) DESC LIMIT ?`, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// tagged : l'encyclopédie d'un microbiote, du plus ancien au plus récent.
func (s *Store) tagged(channel, tag string, limit int) ([]chat.Message, error) {
	rows, err := s.DB.Query(`SELECT m.id, m.channel, m.nickname, m.message, m.ts FROM tags t JOIN messages m ON m.id = t.message_id
		WHERE t.channel = ? AND t.tag = ? ORDER BY m.id DESC LIMIT ?`, channel, tag, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chat.Message{}
	for rows.Next() {
		var m chat.Message
		if err := rows.Scan(&m.ID, &m.Channel, &m.Nickname, &m.Message, &m.TimestampISO); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

/* ========== Clés : créateur, mot de passe, cookie d'accès ========== */

const (
	minPasswordLen = 4
	maxPasswordLen = 128
	pbkdf2Iter     = 210_000
	accessMaxAge   = 30 * 24 * 3600
)

// newOwnerKey : la clé est donnée une fois au créateur ; seule son empreinte est gardée.
func newOwnerKey() (key, hash string) {
	b := make([]byte, 32)
	rand.Read(b)
	key = base64.RawURLEncoding.EncodeToString(b)
	return key, ownerHash(key)
}

func ownerHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func isOwner(r Room, key string) bool {
	return r.OwnerHash != "" && key != "" &&
		subtle.ConstantTimeCompare([]byte(ownerHash(key)), []byte(r.OwnerHash)) == 1
}

// hashPassword : PBKDF2-SHA256, sel aléatoire. Format : pbkdf2-sha256$iter$sel$hash
func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	dk, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, enc.EncodeToString(salt), enc.EncodeToString(dk)), nil
}

func checkPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 || iter > 10_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func accessCookieName(channel string) string { return "sloubi_k_" + channel }

// accessToken dépend du hachage : changer le mot de passe invalide tous les cookies.
func (s *server) accessToken(channel string, r Room) string {
	m := hmac.New(sha256.New, s.store.secret)
	m.Write([]byte("access\x00" + channel + "\x00" + r.PwHash))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *server) hasAccess(c *gin.Context, channel string, r Room) bool {
	if !r.Locked() {
		return true
	}
	got, err := c.Cookie(accessCookieName(channel))
	return err == nil && hmac.Equal([]byte(got), []byte(s.accessToken(channel, r)))
}

func (s *server) grantAccess(c *gin.Context, channel string, r Room) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     accessCookieName(channel),
		Value:    s.accessToken(channel, r),
		Path:     "/",
		MaxAge:   accessMaxAge,
		HttpOnly: true,
		Secure:   c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

// enter : le lieu, si le visiteur a le droit d'y entrer ; sinon répond 403 lui-même.
func (s *server) enter(c *gin.Context, channel string) (Room, bool) {
	r, err := s.store.room(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return r, false
	}
	if !s.hasAccess(c, channel, r) {
		c.JSON(http.StatusForbidden, gin.H{"error": "locked"})
		return r, false
	}
	return r, true
}

// founded : à appeler quand un message ou le post-it vient de fonder le lieu. Renvoie la clé du créateur.
func (s *server) founded(channel string) string {
	key, hash := newOwnerKey()
	if err := s.store.claimOwner(channel, hash); err != nil {
		log.Printf("owner: %v", err)
		return ""
	}
	return key
}

/* ========== Handlers ========== */

// info : de quoi dessiner la page et le compteur. Sans accès, seulement le thème et le verrou.
func (s *server) info(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	r, err := s.store.room(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	out := gin.H{
		"exists": r.Exists, "theme": r.Theme, "locked": r.Locked(),
		"access": s.hasAccess(c, channel, r),
		"owner":  isOwner(r, c.GetHeader("X-Owner-Key")),
		// un lieu fondé avant les réglages n'a pas de créateur : personne ne peut le régler
		"claimable": !r.Exists,
	}
	if out["access"] == true {
		n, last, sloubi, err := s.store.roomStats(channel)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		out["messages"], out["lastId"], out["sloubi"] = n, last, sloubi
	}
	c.JSON(http.StatusOK, out)
}

func (s *server) unlock(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.lockLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(c, 1024, &in) {
		return
	}
	r, err := s.store.room(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if !r.Locked() {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if !checkPassword(r.PwHash, in.Password) {
		c.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "wrong password"})
		return
	}
	s.grantAccess(c, channel, r)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// settings : le créateur choisit le thème et le mot de passe. Sur un lieu qui
// n'existe pas encore, l'appel le fonde et rend la clé du créateur.
func (s *server) settings(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.lockLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	var in struct {
		OwnerKey       string  `json:"ownerKey"`
		Theme          string  `json:"theme"`
		Password       *string `json:"password"`
		RemovePassword bool    `json:"removePassword"`
	}
	if !readJSON(c, 2048, &in) {
		return
	}
	if in.Theme != "" {
		if _, ok := themes[in.Theme]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown theme"})
			return
		}
	}
	var pwHash string
	if in.Password != nil {
		if n := utf8.RuneCountInString(*in.Password); n < minPasswordLen || n > maxPasswordLen || !utf8.ValidString(*in.Password) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password length"})
			return
		}
		var err error
		if pwHash, err = hashPassword(*in.Password); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "hash error"})
			return
		}
	}

	r, err := s.store.room(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	newKey := ""
	if !r.Exists {
		if !s.mayCreate(c, channel) {
			return
		}
		theme := in.Theme
		if theme == "" {
			theme = DefaultTheme
		}
		key, hash := newOwnerKey()
		created, err := s.store.createRoom(channel, hash, theme)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		if created {
			newKey, in.OwnerKey = key, key
		}
		if r, err = s.store.room(channel); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
	}
	if !isOwner(r, in.OwnerKey) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not owner"})
		return
	}

	changed := false
	if in.Theme != "" && in.Theme != r.Theme {
		if err := s.store.setTheme(channel, in.Theme); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		r.Theme, changed = in.Theme, true
	}
	switch {
	case pwHash != "":
		r.PwHash = pwHash
	case in.RemovePassword && r.Locked():
		r.PwHash = ""
	default:
		pwHash = "-" // inchangé
	}
	if pwHash != "-" {
		if err := s.store.setPassword(channel, r.PwHash); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		changed = true
		if r.Locked() {
			s.grantAccess(c, channel, r) // le créateur garde la porte ouverte pour lui
		}
	}
	if changed {
		s.dirMu.Lock()
		s.dirAt = time.Time{} // l'annuaire montre tout de suite le nouveau thème ou le verrou
		s.dirMu.Unlock()
		// tout le monde recharge : nouveau décor, ou nouvelle serrure (les connexions sans clé tombent)
		s.hub.Evict(channel, mustJSON(gin.H{"type": "room", "theme": r.Theme, "locked": r.Locked()}))
	}
	out := gin.H{"ok": true, "theme": r.Theme, "locked": r.Locked()}
	if newKey != "" {
		out["ownerKey"] = newKey
	}
	c.JSON(http.StatusOK, out)
}

const (
	maxTagsListed = 60
	maxTagged     = 100
)

func (s *server) microbiotes(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	if _, ok := s.enter(c, channel); !ok {
		return
	}
	if tag := c.Param("tag"); tag != "" {
		tag = strings.ToLower(norm.NFC.String(tag))
		if utf8.RuneCountInString(tag) > 32 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tag too long"})
			return
		}
		msgs, err := s.store.tagged(channel, tag, maxTagged)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tag": tag, "messages": msgs})
		return
	}
	list, err := s.store.tags(channel, maxTagsListed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": list})
}
