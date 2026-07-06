package main

import (
	"botsonv2/core/builder"
	"log"
)

func main() {
	port := ":8081"
	log.Printf("Starting Agent Builder on http://localhost%s\n", port)
	if err := builder.StartServer(port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
