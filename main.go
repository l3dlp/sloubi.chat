package main

import (
	"net"
	"os"
	"sloubi/src/web"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
	// Caddy est devant : par défaut on n'écoute qu'en local. HOST=0.0.0.0 pour exposer.
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	web.Run(net.JoinHostPort(host, port))
}
