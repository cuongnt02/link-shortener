package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/store"
)

const port string = "8080"

func main() {
	appStore := store.NewInMemoryStore()
	appHandler := handler.New(appStore, fmt.Sprintf("http://127.0.0.1:%v", port))
	log.Printf("Starting server at %v", port)
	err := http.ListenAndServe(":"+port, appHandler.Routes())
	if err != nil {
		log.Fatal(err)
	}
}
