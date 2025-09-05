package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"keeper.websocket.go/internal/auth"
	"keeper.websocket.go/internal/config"
	"keeper.websocket.go/internal/handler"
	"keeper.websocket.go/internal/rabbitmq"
	"keeper.websocket.go/internal/websocket"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()
	logger.Info("configuration loaded")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := websocket.NewHub(logger)
	go hub.Run()

	publisher, err := rabbitmq.NewPublisher(cfg, logger)
	if err != nil {
		logger.Error("failed to create rabbitmq publisher", "error", err)
		os.Exit(1)
	}
	defer publisher.Close()

	authorizer := auth.NewAuthorizer(cfg, logger)

	publishFunc := func(ctx context.Context, routingKey string, body interface{}) error {
		return publisher.Publish(ctx, routingKey, body)
	}

	websocketHandler := handler.NewWebsocketHandler(hub, logger, cfg, authorizer, publishFunc)

	consumer := rabbitmq.NewConsumer(logger, cfg, hub.Broadcast)
	consumer.Start(ctx)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OK"))
	})
	r.Get("/ws", websocketHandler.ServeWs)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		logger.Info("server starting", "address", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	logger.Info("shutdown signal received, starting graceful shutdown")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", "error", err)
	} else {
		logger.Info("server shutdown gracefully")
	}
}
