package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cuongnt02/link-shortener/internal/config"
	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/link"
	"github.com/cuongnt02/link-shortener/internal/shortener"
	"github.com/cuongnt02/link-shortener/internal/store"
)

const codeLength = 6

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Getenv, os.Stdout)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	opts := slog.HandlerOptions{
		Level: cfg.LogLevel,
	}
	jsonLogHandler := slog.NewJSONHandler(stdout, &opts)
	logger := slog.New(jsonLogHandler)

	storeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	appStore, err := store.NewSQLiteStore(storeCtx, cfg.DBPath)
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		err := appStore.Close()
		if err != nil {
			logger.Error("closing store failed", slog.Any("error", err))
		}
	}()
	generator := func() (string, error) {
		return shortener.GenCode(codeLength)
	}
	service := link.NewService(appStore, generator)
	appHandler := handler.New(service, cfg.BaseURL, logger)

	server := http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           appHandler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errChan := make(chan error, 1)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	go func() {
		logger.Info("listening", slog.String("address", listener.Addr().String()))
		errChan <- server.Serve(listener)
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("server shutdown failed: %w", err)
		}

		logger.Info("server is shut down")
		return nil
	}
}
