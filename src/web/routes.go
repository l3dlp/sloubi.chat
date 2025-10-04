package web

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sloubi/src/chat"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func Run(port string) {

	store := openStore()
	defer store.DB.Close()

	// static dir "public" must contain your PWA files (index.html, app.js, styles.css, manifest.json, icons, etc.)
	publicDir := filepath.Join(".", "public")
	if _, err := os.Stat(publicDir); os.IsNotExist(err) {
		log.Printf("warning: %s not found; create it and drop your PWA assets in there\n", publicDir)
	}

	hub := NewHub()
	go hub.Run()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), gzip.Gzip(gzip.DefaultCompression))

	// API: receive chat via REST, store, broadcast
	r.POST("/verif.json", func(c *gin.Context) {
		var in chat.Message
		if err := c.BindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}
		in.Nickname = strings.TrimSpace(in.Nickname)
		if in.Nickname == "" {
			in.Nickname = "Anonyme"
		}
		in.Message = strings.TrimSpace(in.Message)

		msg, err := store.insertMessage(in)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db insert failed"})
			return
		}
		select {
		case hub.Broadcast <- msg:
		default:
			go func(m chat.Message) { hub.Broadcast <- m }(msg)
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "ok"})
	})

	// API: last sloubi (to feature it on app load)
	r.GET("/last-sloubi.json", func(c *gin.Context) {
		ls, err := store.lastSloubi()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		if ls == nil {
			// Start at sloubi 0
			c.JSON(http.StatusOK, gin.H{
				"number":    0,
				"exists":    false,
				"nickname":  "",
				"message":   "",
				"timestamp": "",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"number":    *ls.SloubiNumber,
			"exists":    true,
			"nickname":  ls.Nickname,
			"message":   ls.Message,
			"timestamp": ls.TimestampISO,
		})
	})

	// WS: push backlog then live
	r.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		hub.mu.Lock()
		hub.clients[conn] = true
		hub.mu.Unlock()

		// push last 100 from DB
		if msgs, err := store.lastMessages(100); err == nil {
			for _, m := range msgs {
				if err := conn.WriteJSON(m); err != nil {
					break
				}
			}
		}
		// reader: drain until close
		go func() {
			defer func() {
				hub.mu.Lock()
				delete(hub.clients, conn)
				hub.mu.Unlock()
				_ = conn.Close()
			}()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	})

	// Static assets (JS, CSS, manifest, icons…)
	r.Static("/assets", "./assets")

	// Views
	r.LoadHTMLGlob("views/*")

	// Root -> index.html
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{})
	})

	r.GET("/app.js", func(c *gin.Context) {
		c.File("./assets/app.js")
	})
	r.GET("/manifest.json", func(c *gin.Context) {
		c.File("./assets/manifest.json")
	})
	r.GET("/icon-192.png", func(c *gin.Context) {
		c.File("./assets/icon-192.png")
	})
	r.GET("/icon-512.png", func(c *gin.Context) {
		c.File("./assets/icon-512.png")
	})
	r.GET("/styles.css", func(c *gin.Context) {
		c.File("./assets/styles.css")
	})
	// 404 fallback : renvoyer index.html pour ton PWA
	r.NoRoute(func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{})
	})

	log.Printf("listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
