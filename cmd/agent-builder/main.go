package main

import (
	webui "botsonv2/core/interface/web"
	"log"
)

func main() {
	port := ":8081"
	log.Printf("Starting Standalone Console on http://localhost%s\n", port)
	if err := webui.StartServer(port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
