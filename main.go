package main

import (
	"os"
	"sloubi/src/web"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}

	web.Run(port)
}
