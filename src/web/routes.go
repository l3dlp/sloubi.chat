package web

import (
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"regexp"
	"sloubi/src/chat"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

const (
	minNicknameLen = 2
	maxNicknameLen = 24
	maxMessageLen  = 500
	maxPostitLen   = 280
	backlogSize    = 100
)

var clientIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type server struct {
	store     *Store
	hub       *Hub
	forbidden *Forbidden

	postLimit   *Limiter // messages
	postitLimit *Limiter
	checkLimit  *Limiter // /api/check : cible n°1 des bots
	wsLimit     *Limiter
	createLimit *Limiter // fondation de salons
	anyLimit    *Limiter // toutes les requêtes : plafond large, contre les inondations

	wsMu     sync.Mutex
	wsPerKey map[string]int // WS ouverts par IP (ou /64)
	wsTotal  int
}

const (
	maxWSPerKey = 12   // plusieurs onglets, ou une famille derrière la même box
	maxWSTotal  = 5000 // garde-fou global
)

// clientKey : la clé de rate-limit. IPv4 telle quelle ; IPv6 ramenée à son /64,
// sinon un attaquant fait tourner les adresses de son préfixe et contourne tout.
func clientKey(c *gin.Context) string {
	ip := net.ParseIP(c.ClientIP())
	if ip == nil {
		return c.ClientIP()
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func (s *server) wsAcquire(key string) bool {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.wsPerKey == nil {
		s.wsPerKey = map[string]int{}
	}
	if s.wsTotal >= maxWSTotal || s.wsPerKey[key] >= maxWSPerKey {
		return false
	}
	s.wsPerKey[key]++
	s.wsTotal++
	return true
}

func (s *server) wsRelease(key string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.wsPerKey[key]--; s.wsPerKey[key] <= 0 {
		delete(s.wsPerKey, key)
	}
	s.wsTotal--
}

func Run(addr string) {
	store, err := openStore("db/chat.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer store.DB.Close()

	// sans liste valide on refuse de démarrer plutôt que de tout autoriser
	forbidden, err := loadForbidden("forbidden.json")
	if err != nil {
		log.Fatalf("forbidden.json: %v", err)
	}
	go forbidden.watch(30 * time.Second)

	s := &server{
		store:       store,
		hub:         NewHub(),
		forbidden:   forbidden,
		postLimit:   NewLimiter(1, 5, 0, 0),
		postitLimit: NewLimiter(1.0/3, 3, 0, 0),
		checkLimit:  NewLimiter(4, 10, 30, 10*time.Minute),
		wsLimit:     NewLimiter(0.5, 10, 0, 0),
		createLimit: NewLimiter(10.0/3600, 10, 0, 0),
		anyLimit:    NewLimiter(30, 120, 0, 0),
	}

	// timeouts : sans eux, une connexion qui n'envoie jamais ses en-têtes reste ouverte (slowloris)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second, // les WS posent leurs propres délais après l'upgrade
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func (s *server) router() *gin.Engine {
	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode) // pas de mode debug en prod
	}
	r := gin.New()
	// Caddy est devant, en local : on ne croit X-Forwarded-For que venant de lui
	if err := r.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		log.Fatal(err)
	}
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders, s.floodGuard, gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{"/ws"})))

	r.LoadHTMLGlob("views/*.html")

	// API
	r.GET("/api/check/:chan", s.check)
	r.POST("/api/nick", s.nick)
	r.POST("/api/:chan/messages", s.postMessage(s.paramChannel))
	r.GET("/api/:chan/last-sloubi", s.lastSloubi(s.paramChannel))
	r.POST("/api/:chan/postit", s.postPostit)
	r.GET("/ws/:chan", s.ws(s.paramChannel))

	// Anciennes routes, gardées pour les PWA en cache : elles visent la taverne historique.
	r.POST("/verif.json", s.postMessage(defaultChannel))
	r.GET("/last-sloubi.json", s.lastSloubi(defaultChannel))
	r.GET("/ws", s.ws(defaultChannel))

	// Static assets (JS, CSS, manifest, icons…), plus les anciens chemins à la racine
	r.Static("/assets", "./assets")
	for _, f := range []string{"app.js", "manifest.json", "icon-192.png", "icon-512.png"} {
		r.StaticFile("/"+f, "./assets/"+f)
	}

	// Pages
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "home.html", gin.H{})
	})
	r.GET("/:chan", s.channelPage)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.HTML(http.StatusNotFound, "home.html", gin.H{})
	})
	return r
}

// floodGuard : un plafond large sur toutes les requêtes (une page en charge ~15).
func (s *server) floodGuard(c *gin.Context) {
	if s.anyLimit != nil && !s.anyLimit.Allow(clientKey(c)) {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	c.Next()
}

func securityHeaders(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), serial=(), browsing-topics=()")
	// pas de style inline dans le HTML (les scripts passent par le CSSOM, que la CSP n'interdit pas)
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; img-src 'self' data:; "+
		"style-src 'self'; font-src 'self'; manifest-src 'self'; object-src 'none'; "+
		"base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	c.Next()
}

/* ========== Salons ========== */

type channelResolver func(*gin.Context) (string, bool)

func defaultChannel(*gin.Context) (string, bool) { return DefaultChannel, true }

// paramChannel valide :chan pour l'API ; répond 404 lui-même si le nom est refusé.
func (s *server) paramChannel(c *gin.Context) (string, bool) {
	name := strings.ToLower(c.Param("chan"))
	if reason := s.forbidden.checkChannel(name); reason != "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel " + reason})
		return "", false
	}
	return name, true
}

func (s *server) channelPage(c *gin.Context) {
	raw := c.Param("chan")
	name := strings.ToLower(raw)
	if s.forbidden.checkChannel(name) != "" {
		// l'accueil reprend le nom tapé et l'affiche en rouge
		if utf8.RuneCountInString(raw) > 64 {
			raw = string([]rune(raw)[:64])
		}
		c.HTML(http.StatusNotFound, "home.html", gin.H{"Prefill": raw})
		return
	}
	if raw != name {
		c.Redirect(http.StatusMovedPermanently, "/"+name)
		return
	}
	c.HTML(http.StatusOK, "room.html", gin.H{"Chan": name})
}

func (s *server) check(c *gin.Context) {
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	name := strings.ToLower(c.Param("chan"))
	reason := s.forbidden.checkChannel(name)
	exists := false
	if reason == "" {
		var err error
		if exists, err = s.store.channelExists(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"name": name, "ok": reason == "", "reason": reason, "exists": exists})
}

// mayCreate consomme un jeton de fondation si le salon n'existe pas encore.
func (s *server) mayCreate(c *gin.Context, channel string) bool {
	exists, err := s.store.channelExists(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return false
	}
	if !exists && !s.createLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many new channels"})
		return false
	}
	return true
}

/* ========== Saisie ========== */

// readJSON impose application/json (un formulaire d'un site tiers ne peut pas
// l'envoyer sans preflight CORS) et une taille de corps maximale.
func readJSON(c *gin.Context, maxBytes int64, v any) bool {
	mt, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mt != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "application/json expected"})
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	if err := c.ShouldBindJSON(v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return false
	}
	return true
}

// cleanText retire les caractères de contrôle (sauf \n si keepNewlines) et les espaces autour.
// maxMarks : diacritiques empilés tolérés sur une lettre (au-delà, c'est du zalgo).
const maxMarks = 2

// cleanText normalise un texte libre (message, post-it) : NFC, sans caractères de
// contrôle (sauf \n si keepNewlines), sans caractères de format invisibles (inversion
// de direction, espaces sans chasse…) hormis le ZWJ des emojis composés, et sans
// pile de diacritiques.
func cleanText(s string, keepNewlines bool) string {
	s = norm.NFC.String(strings.ToValidUTF8(strings.ReplaceAll(s, "\r\n", "\n"), ""))
	var b strings.Builder
	marks := 0
	for _, r := range s {
		switch {
		case r == '\n' && keepNewlines:
		case unicode.IsControl(r), r == '\u2028', r == '\u2029':
			continue
		case unicode.Is(unicode.Cf, r) && r != '\u200d':
			continue
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
			if marks++; marks > maxMarks {
				continue
			}
			b.WriteRune(r)
			continue
		}
		marks = 0
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// Raisons de refus d'un pseudo.
const (
	nickTooShort  = "tooshort"
	nickTooLong   = "toolong"
	nickChars     = "chars"
	nickForbidden = "forbidden"
)

// nickRune : alphabet latin (accents compris), chiffres ASCII, espace et _ - . '
// Le reste est refusé, pas retiré : pas de lettres cyrilliques sosies, pas de
// caractères invisibles ou de direction, pas de diacritiques empilés.
func nickRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r == ' ', r == '_', r == '-', r == '.', r == '\'':
		return true
	}
	return unicode.Is(unicode.Latin, r)
}

// cleanNickname met un pseudo sous forme canonique (NFC, espaces resserrés) et le valide.
func (s *server) cleanNickname(raw string) (string, string) {
	n := norm.NFC.String(strings.ToValidUTF8(raw, ""))
	n = strings.Join(strings.Fields(strings.ReplaceAll(n, "\u00a0", " ")), " ")
	for _, r := range n {
		if !nickRune(r) {
			return "", nickChars
		}
	}
	switch l := utf8.RuneCountInString(n); {
	case l < minNicknameLen:
		return "", nickTooShort
	case l > maxNicknameLen:
		return "", nickTooLong
	}
	if s.forbidden.Match(n) {
		return "", nickForbidden
	}
	return n, ""
}

func cleanClientID(id string) string {
	if clientIDRe.MatchString(id) {
		return id
	}
	return ""
}

/* ========== Handlers ========== */

func (s *server) postMessage(resolve channelResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolve(c)
		if !ok {
			return
		}
		if !s.postLimit.Allow(clientKey(c)) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
			return
		}
		var in struct {
			ClientID string `json:"clientId"`
			Nickname string `json:"nickname"`
			Message  string `json:"message"`
		}
		if !readJSON(c, 4096, &in) {
			return
		}
		msg := chat.Message{
			Channel:  channel,
			ClientID: cleanClientID(in.ClientID),
			Message:  cleanText(in.Message, false),
		}
		// un pseudo refusé (ou d'un vieux client) ne bloque pas le message : on passe en Anonyme
		if nick, reason := s.cleanNickname(in.Nickname); reason == "" {
			msg.Nickname = nick
		} else {
			msg.Nickname = "Anonyme"
		}
		switch n := utf8.RuneCountInString(msg.Message); {
		case n == 0:
			c.JSON(http.StatusBadRequest, gin.H{"error": "empty message"})
			return
		case n > maxMessageLen:
			c.JSON(http.StatusBadRequest, gin.H{"error": "message too long"})
			return
		}
		if !s.mayCreate(c, channel) {
			return
		}

		msg, _, err := s.store.insertMessage(msg)
		if err != nil {
			log.Printf("insert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db insert failed"})
			return
		}
		s.hub.Broadcast(channel, messageJSON(msg))
		c.JSON(http.StatusAccepted, gin.H{"status": "ok"})
	}
}

// nick valide un pseudo pour /nick et les paramètres ; renvoie la forme canonique à garder.
func (s *server) nick(c *gin.Context) {
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	var in struct {
		Nickname string `json:"nickname"`
	}
	if !readJSON(c, 1024, &in) {
		return
	}
	nick, reason := s.cleanNickname(in.Nickname)
	c.JSON(http.StatusOK, gin.H{"ok": reason == "", "nickname": nick, "reason": reason})
}

func (s *server) postPostit(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.postitLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	var in struct {
		ClientID string `json:"clientId"`
		Text     string `json:"text"`
	}
	if !readJSON(c, 4096, &in) {
		return
	}
	text := cleanText(in.Text, true)
	if utf8.RuneCountInString(text) > maxPostitLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "postit too long"})
		return
	}
	if !s.mayCreate(c, channel) {
		return
	}
	p, _, err := s.store.setPostit(channel, text, cleanClientID(in.ClientID))
	if err != nil {
		log.Printf("postit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	s.hub.Broadcast(channel, postitJSON(p))
	c.JSON(http.StatusOK, p)
}

func (s *server) lastSloubi(resolve channelResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolve(c)
		if !ok {
			return
		}
		ls, err := s.store.lastSloubi(channel)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		if ls == nil {
			// Start at sloubi 0
			c.JSON(http.StatusOK, gin.H{"number": 0, "exists": false, "nickname": "", "message": "", "timestamp": ""})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"number":    *ls.SloubiNumber,
			"exists":    true,
			"nickname":  ls.Nickname,
			"message":   ls.Message,
			"timestamp": ls.TimestampISO,
		})
	}
}

// WS: push backlog + post-it, then live
func (s *server) ws(resolve channelResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolve(c)
		if !ok {
			return
		}
		key := clientKey(c)
		if !s.wsLimit.Allow(key) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
			return
		}
		if !s.wsAcquire(key) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many connections"})
			return
		}
		defer s.wsRelease(key)
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		cl := &client{conn: conn, channel: channel, send: make(chan []byte, sendBuffer)}
		go cl.writePump()
		s.hub.join(cl, func() [][]byte {
			var out [][]byte
			if msgs, err := s.store.lastMessages(channel, backlogSize); err == nil {
				for _, m := range msgs {
					out = append(out, messageJSON(m))
				}
			}
			if p, err := s.store.postit(channel); err == nil && p.UpdatedAt != "" {
				out = append(out, postitJSON(p))
			}
			return out
		})
		cl.readPump(s.hub)
	}
}
