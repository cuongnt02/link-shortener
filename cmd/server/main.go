package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/link"
	"github.com/cuongnt02/link-shortener/internal/shortener"
	"github.com/cuongnt02/link-shortener/internal/store"
)

const port string = "8080"
const codeLength = 6

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	appStore, err := store.NewSQLiteStore(ctx, "sqlite.db")
	if err != nil {
		log.Fatal(err)
	}
	cancel()
	defer func() {
		err := appStore.Close()
		if err != nil {
			log.Printf("failed to close store: %v", err)
		}
	}()
	generator := func() (string, error) {
		return shortener.GenCode(codeLength)
	}
	service := link.NewService(appStore, generator)
	appHandler := handler.New(service, fmt.Sprintf("http://127.0.0.1:%v", port))
	log.Printf("Starting server at %v", port)
	err = http.ListenAndServe(":"+port, appHandler.Routes())
	if err != nil {
		log.Fatal(err)
	}
}
