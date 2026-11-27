package web

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

/* ========== Domaines perso ==========

Un lieu peut avoir son propre nom de domaine (réglé par son créateur, membre
validé ou admin). Tous les domaines pointent sur le même serveur :
- l'hôte du site (SITE_URL, par défaut https://sloubi.chat, et son www) : rien ne change ;
- un domaine rattaché à un lieu : le lieu s'ouvre sur /, l'API et les fichiers
  statiques passent, le reste repart vers le site ;
- un domaine inconnu : redirection vers SITE_URL.
Le proxy (Caddy, TLS à la demande) demande à GET /_/domaine?domain=… s'il peut
prendre un certificat pour un nom : 200 seulement pour un domaine rattaché.
*/

var (
	domainRe        = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	errDomainTaken  = errors.New("domain taken")
	errDomainBad    = errors.New("invalid domain")
	errDomainOfSite = errors.New("site domain")
)

// loadSite : l'adresse du site et ses hôtes (SITE_URL, SITE_HOSTS séparés par des virgules).
func loadSite() (siteURL string, hosts map[string]bool) {
	siteURL = strings.TrimRight(envOr("SITE_URL", "https://sloubi.chat"), "/")
	hosts = map[string]bool{}
	if u, err := url.Parse(siteURL); err == nil && u.Hostname() != "" {
		h := strings.ToLower(u.Hostname())
		hosts[h] = true
		hosts["www."+strings.TrimPrefix(h, "www.")] = true
	}
	for _, h := range strings.Split(os.Getenv("SITE_HOSTS"), ",") {
		if h = normHost(h); h != "" {
			hosts[h] = true
		}
	}
	return siteURL, hosts
}

// requestHost : l'hôte demandé par le navigateur. Derrière un relais local (le
// proxy de nadaprod envoie Host: 127.0.0.1:8888), il est dans X-Forwarded-Host ;
// on ne le croit que d'un voisin en loopback, comme X-Forwarded-For.
func requestHost(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" && fromLoopback(r) {
		first, _, _ := strings.Cut(fwd, ",")
		if h := normHost(first); h != "" {
			return h
		}
	}
	return normHost(r.Host)
}

func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}

// devHost : localhost, une IP, *.localhost ou *.test, servis en HTTP clair en développement.
func devHost(h string) bool {
	return h == "localhost" || net.ParseIP(h) != nil || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".test")
}

// secureRequest : faut-il des cookies Secure ? Oui en HTTPS, et sur tout vrai nom
// quand le site est en HTTPS : un relais peut annoncer X-Forwarded-Proto: http
// alors que le navigateur, lui, parle HTTPS à Caddy.
func (s *server) secureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		return true
	}
	return strings.HasPrefix(s.siteURL, "https://") && !devHost(requestHost(c.Request))
}

// normHost : minuscules, sans port ni point final.
func normHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(strings.Trim(h, "[]"), ".")
}

// cleanDomain valide un domaine saisi dans les réglages ("" = retirer).
// Les domaines internationaux s'écrivent en punycode (xn--…).
func (s *server) cleanDomain(raw string) (string, error) {
	d := normHost(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "https://"), "http://"))
	d = strings.TrimSuffix(d, "/")
	if d == "" {
		return "", nil
	}
	if len(d) > 253 || !domainRe.MatchString(d) {
		return "", errDomainBad
	}
	if s.isSiteHost(d) || d == "localhost" || strings.HasSuffix(d, ".localhost") {
		return "", errDomainOfSite
	}
	for h := range s.siteHosts {
		if strings.HasSuffix(d, "."+strings.TrimPrefix(h, "www.")) {
			return "", errDomainOfSite // les sous-domaines du site restent au site
		}
	}
	return d, nil
}

// isSiteHost : l'hôte du site lui-même (ou une adresse IP, localhost : le dev et le proxy local).
func (s *server) isSiteHost(h string) bool {
	if s.siteHosts == nil || h == "" || h == "localhost" || net.ParseIP(h) != nil {
		return true
	}
	return s.siteHosts[h]
}

func (st *Store) channelByDomain(d string) (string, error) {
	var name string
	err := st.DB.QueryRow(`SELECT name FROM channels WHERE domain = ?`, d).Scan(&name)
	return name, err
}

func (st *Store) setDomain(channel, d string) error {
	_, err := st.DB.Exec(`UPDATE channels SET domain = NULLIF(?, '') WHERE name = ?`, d, channel)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errDomainTaken
	}
	return err
}

// hostGate aiguille les requêtes qui arrivent par un domaine perso.
func (s *server) hostGate(c *gin.Context) {
	host := requestHost(c.Request)
	if s.isSiteHost(host) {
		c.Next()
		return
	}
	// rien de ce qui suit ne doit rester dans le cache du navigateur : un domaine
	// rattaché plus tard doit s'ouvrir tout de suite, sans vider le cache
	noStore := func() { c.Header("Cache-Control", "no-store") }
	channel, err := s.store.channelByDomain(host)
	if err == sql.ErrNoRows {
		noStore()
		c.Redirect(http.StatusFound, s.siteURL+"/")
		c.Abort()
		return
	}
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	p := c.Request.URL.Path
	if s.serveRoomApp(c, channel) { // son nom et ses icônes d'application (icons.go)
		c.Abort()
		return
	}
	switch {
	case p == "/":
		s.renderRoom(c, channel, true)
		c.Abort()
	case strings.HasPrefix(p, "/api/"), strings.HasPrefix(p, "/ws/"), strings.HasPrefix(p, "/assets/"),
		p == "/sw.js", p == "/manifest.json", p == "/icon-192.png", p == "/icon-512.png":
		c.Next()
	case strings.TrimSuffix(p, "/") == "/"+channel:
		// /<lieu> sur son propre domaine (lien ancien, notification) : c'est /
		target := "/"
		if q := c.Request.URL.RawQuery; q != "" {
			target += "?" + q
		}
		noStore()
		c.Redirect(http.StatusFound, target)
		c.Abort()
	default:
		// les autres lieux, l'annuaire, le compte : sur le site
		noStore()
		c.Redirect(http.StatusFound, s.siteURL+c.Request.URL.RequestURI())
		c.Abort()
	}
}

// domainAsk : GET /_/domaine?domain=… pour le TLS à la demande du proxy, en local seulement.
func (s *server) domainAsk(c *gin.Context) {
	// Caddy appelle en direct ; une requête relayée (X-Forwarded-*) vient d'un navigateur
	if ip := net.ParseIP(c.ClientIP()); ip == nil || !ip.IsLoopback() || c.GetHeader("X-Forwarded-Host") != "" || c.GetHeader("X-Forwarded-For") != "" {
		c.Status(http.StatusNotFound)
		return
	}
	d := normHost(c.Query("domain"))
	if _, err := s.store.channelByDomain(d); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusOK)
}
