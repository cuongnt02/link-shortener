package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/link"
	"github.com/cuongnt02/link-shortener/internal/shortener"
	"github.com/cuongnt02/link-shortener/internal/store"
)

const port string = "8080"
const codeLength = 6

func main() {
	appStore := store.NewInMemoryStore()
	generator := func() (string, error) {
		return shortener.GenCode(codeLength)
	}
	service := link.NewService(appStore, generator)
	appHandler := handler.New(service, fmt.Sprintf("http://127.0.0.1:%v", port))
	log.Printf("Starting server at %v", port)
	err := http.ListenAndServe(":"+port, appHandler.Routes())
	if err != nil {
		log.Fatal(err)
	}
}
