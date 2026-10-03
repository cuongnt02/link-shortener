package main

import (
	"log"
	"net/http"
)

const port string = "8080"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			log.Printf("Error at handler: %v", err)
		}
	})
	log.Printf("Starting server at %v", port)
	err := http.ListenAndServe(":"+port, mux)
	if err != nil {
		log.Fatal(err)
	}
}
