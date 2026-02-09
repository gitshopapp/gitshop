package main

// GitShop is the main entry point for the application.

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gitshopapp/gitshop/app"
	"github.com/gitshopapp/gitshop/server"
)

func main() {
	fallbackLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	application, err := app.New()
	if err != nil {
		fallbackLogger.Error("failed to initialize app", "error", err)
		os.Exit(1)
	}
	srv, err := server.New(application.Config, application.Logger, application.Handlers)
	if err != nil {
		fallbackLogger.Error("failed to initialize server", "error", err)
		application.Close()
		os.Exit(1)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Run()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErr:
		if err != nil {
			application.Logger.Error("server failed", "error", err)
			application.Close()
			os.Exit(1)
		}
		application.Close()
		return
	case <-quit:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	if err := srv.Close(ctx); err != nil {
		cancel()
		application.Logger.Error("server forced to shutdown", "error", err)
		application.Close()
		os.Exit(1)
	}
	cancel()

	application.Close()
}
