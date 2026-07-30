package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/api"
	"github.com/clipbridge/clipbridge/relay-server/internal/auth"
	"github.com/clipbridge/clipbridge/relay-server/internal/config"
	"github.com/clipbridge/clipbridge/relay-server/internal/database"
	"github.com/clipbridge/clipbridge/relay-server/internal/logging"
	"github.com/clipbridge/clipbridge/relay-server/internal/service"
	clipws "github.com/clipbridge/clipbridge/relay-server/internal/ws"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server_stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.LogLevel)
	slog.SetDefault(log)
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o700); err != nil {
		return err
	}
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := database.Open(startupCtx, cfg.DSN())
	if err != nil {
		return err
	}
	defer db.Close()
	store := service.NewStore(db, cfg.MaxQueuedMessages, cfg.MaxQueuedBytes, cfg.PairingTokenTTL)
	tokens := auth.NewTokenManager(db, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	hub := clipws.NewHub()
	wsServer := clipws.NewServer(store, tokens, hub, log, cfg.AllowedOrigin, cfg.MaxCiphertextBytes)
	handler := api.New(store, tokens, hub, wsServer, db, log,
		cfg.MaxBodyBytes, cfg.RateLimitPerMinute)
	server := &http.Server{
		Addr: cfg.ListenAddress, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // WebSocket 自行设置每次写截止时间。
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(cfg.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-rootCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(rootCtx, 15*time.Second)
				deleted, cleanupErr := store.Cleanup(ctx)
				cancel()
				if cleanupErr != nil && !errors.Is(cleanupErr, context.Canceled) {
					log.Error("cleanup_failed", "error", cleanupErr)
				} else if deleted > 0 {
					log.Info("expired_messages_deleted", "count", deleted)
				}
			}
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		log.Info("server_started", "listen", cfg.ListenAddress)
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-rootCtx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	stop()
	<-cleanupDone
	log.Info("server_stopped")
	return nil
}
