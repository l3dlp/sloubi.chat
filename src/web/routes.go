package web

import (
	"fmt"
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
	lockLimit   *Limiter // mot de passe et réglages du créateur : PBKDF2 coûte cher, et on devine
	anyLimit    *Limiter // toutes les requêtes : plafond large, contre les inondations
	reactLimit  *Limiter // likes et clics de liens
	uploadLimit *Limiter // envois de fichiers
	uploads     reservations
	videos      *videoFetcher // miniatures et titres des vidéos partagées en lien

	turn turnConfig // coturn partagé avec BeamCam (calls.go)
	push *pusher    // notifications d'appel (push.go) ; nil si désactivées

	dirMu   sync.Mutex // cache de l'annuaire /_/
	dirAt   time.Time
	dirRows []directoryRow

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
		lockLimit:   NewLimiter(0.2, 5, 10, 15*time.Minute),
		anyLimit:    NewLimiter(30, 120, 0, 0),
		reactLimit:  NewLimiter(3, 20, 0, 0),
		uploadLimit: NewLimiter(2, 40, 0, 0),
		turn:        loadTurn(),
	}
	s.push = newPusher(store)
	s.videos = newVideoFetcher()
	go store.reclassify() // les images rangées en documents par une version précédente retrouvent leur aperçu

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
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders, s.floodGuard, gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{"/ws"}),
		// les fichiers : déjà compressés, et leurs envois longs ont besoin de l'écrivain d'origine (délais prolongés)
		gzip.WithExcludedPathsRegexs([]string{`^/api/[^/]+/files`})))

	r.LoadHTMLGlob("views/*.html")

	// API
	r.GET("/api/check/:chan", s.check)
	r.POST("/api/nick", s.nick)
	r.POST("/api/:chan/messages", s.postMessage(s.paramChannel))
	r.GET("/api/:chan/last-sloubi", s.lastSloubi(s.paramChannel))
	r.POST("/api/:chan/postit", s.postPostit)
	r.POST("/api/:chan/like", s.postLike)
	r.POST("/api/:chan/click", s.postClick)
	r.POST("/api/:chan/files", s.uploadFile)
	r.GET("/api/:chan/files", s.listFilesRoute)
	r.GET("/api/:chan/files/:id", s.serveFile)
	r.GET("/api/:chan/favorites", s.favoritesRoute)
	r.GET("/api/:chan/vthumb/:provider/:id", s.videoThumb)
	r.GET("/api/:chan/info", s.info)
	r.POST("/api/:chan/unlock", s.unlock)
	r.POST("/api/:chan/settings", s.settings)
	r.GET("/api/:chan/microbiotes", s.microbiotes)
	r.GET("/api/:chan/microbiotes/:tag", s.microbiotes)
	r.GET("/api/:chan/turn", s.turnCreds)
	r.GET("/api/push-key", s.pushKey)
	r.POST("/api/:chan/push", s.pushSubscribe)
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
	// le service worker des notifications : à la racine, pour couvrir tout le site
	r.GET("/sw.js", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.File("./assets/sw.js")
	})

	// Pages
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "home.html", gin.H{})
	})
	r.GET("/_/", s.directory) // aucun salon ne peut commencer par _ : pas de collision
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
	// micro, caméra et écran servent aux appels et au live des lieux gardés (calls.go)
	h.Set("Permissions-Policy", "camera=(self), microphone=(self), display-capture=(self), geolocation=(), payment=(), usb=(), serial=(), browsing-topics=()")
	// pas de style inline dans le HTML (les scripts passent par le CSSOM, que la CSP n'interdit pas)
	// media-src : les vidéos et les sons partagés (et blob: pour réécouter un enregistrement avant de l'envoyer)
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; img-src 'self' data:; media-src 'self' blob:; "+
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

// Annuaire /_/ : tous les salons. Mis en cache quelques secondes, la requête parcourt
// toute la table des messages.
const directoryLimit = 2000

type directoryRow struct {
	ChannelInfo
	Ago       string
	ThemeName string
}

func (s *server) directory(c *gin.Context) {
	s.dirMu.Lock()
	if time.Since(s.dirAt) > 15*time.Second {
		list, err := s.store.listChannels(directoryLimit)
		if err != nil {
			s.dirMu.Unlock()
			log.Printf("directory: %v", err)
			c.String(http.StatusInternalServerError, "db error")
			return
		}
		rows := make([]directoryRow, len(list))
		for i, ci := range list {
			rows[i] = directoryRow{ci, ago(ci.LastActive), themeOf(ci.Theme).Name}
		}
		s.dirRows, s.dirAt = rows, time.Now()
	}
	rows := s.dirRows
	s.dirMu.Unlock()

	sloubis := 0
	for _, r := range rows {
		if !r.Locked {
			sloubis += r.Sloubis
		}
	}
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.HTML(http.StatusOK, "directory.html", gin.H{"Rows": rows, "Count": len(rows), "Sloubis": sloubis})
}

// ago : « il y a 3 h » à partir d'un horodatage du serveur.
func ago(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	switch d := time.Since(t); {
	case d < time.Minute:
		return "à l’instant"
	case d < time.Hour:
		return fmt.Sprintf("il y a %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("il y a %d h", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("il y a %d j", int(d.Hours()/24))
	default:
		return t.Format("01/2006")
	}
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
	r, err := s.store.room(name)
	if err != nil {
		c.String(http.StatusInternalServerError, "db error")
		return
	}
	c.HTML(http.StatusOK, "room.html", gin.H{
		"Chan":   name,
		"Theme":  themeOf(r.Theme),
		"Themes": themeList(),
		"Locked": !s.hasAccess(c, name, r),
		"Calls":  r.Locked() && s.hasAccess(c, name, r), // on s'appelle seulement entre gens qui ont le mot de passe
	})
}

func (s *server) check(c *gin.Context) {
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	name := strings.ToLower(c.Param("chan"))
	reason := s.forbidden.checkChannel(name)
	var r Room
	if reason == "" {
		var err error
		if r, err = s.store.room(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
	}
	// access : l'accueil demande le mot de passe seulement à qui ne l'a pas encore donné
	c.JSON(http.StatusOK, gin.H{"name": name, "ok": reason == "", "reason": reason, "exists": r.Exists,
		"theme": themeOf(r.Theme).ID, "locked": r.Locked(), "access": reason == "" && s.hasAccess(c, name, r)})
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
	// « Anonyme » était le nom par défaut : il ne vaut pas un pseudo choisi
	if s.forbidden.Match(n) || strings.EqualFold(n, "anonyme") {
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
		if _, ok := s.enter(c, channel); !ok {
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
		// pas de pseudo valide, pas de message : on choisit son nom avant d'écrire
		nick, reason := s.cleanNickname(in.Nickname)
		if reason != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "nickname required", "reason": reason})
			return
		}
		msg.Nickname = nick
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

		msg, created, err := s.store.insertMessage(msg)
		if err != nil {
			log.Printf("insert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db insert failed"})
			return
		}
		s.hub.Broadcast(channel, messageJSON(msg))
		out := gin.H{"status": "ok", "id": msg.ID} // l'auteur sait ainsi quelles bulles sont les siennes
		if created {
			out["ownerKey"] = s.founded(channel)
		}
		c.JSON(http.StatusAccepted, out)
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
	if _, ok := s.enter(c, channel); !ok {
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
	p, created, err := s.store.setPostit(channel, text, cleanClientID(in.ClientID))
	if err != nil {
		log.Printf("postit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	s.hub.Broadcast(channel, postitJSON(p))
	if created {
		c.JSON(http.StatusOK, gin.H{"channel": p.Channel, "text": p.Text, "updatedAt": p.UpdatedAt, "ownerKey": s.founded(channel)})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (s *server) lastSloubi(resolve channelResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := resolve(c)
		if !ok {
			return
		}
		if _, ok := s.enter(c, channel); !ok {
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
		if _, ok := s.enter(c, channel); !ok {
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
		cl := &client{conn: conn, channel: channel, send: make(chan []byte, sendBuffer), pid: newPeerID()}
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
		cl.readPump(s.hub, s.onClientMessage)
	}
}
