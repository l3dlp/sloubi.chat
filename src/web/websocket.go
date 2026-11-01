package web

import (
	"net/http"
	"net/url"

	"github.com/gophre/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     sameOrigin,
}

// sameOrigin : l'Origin doit correspondre au Host demandé (Caddy transmet le Host
// d'origine). Sans Origin (curl, outils), on laisse passer : ce n'est pas un navigateur
// piégé par un site tiers, et le rate-limit s'applique quand même.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}
