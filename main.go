package main

import (
	"bufio"
	"net"
	"os"
	"sloubi/src/web"
	"strings"
)

// loadDotEnv lit ./.env (s'il existe) sans écraser les variables déjà posées :
// ADMIN_EMAIL, l'envoi des codes (SMTP ou Brevo), le TURN… Voir .env.example.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

func main() {
	loadDotEnv(".env")
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
