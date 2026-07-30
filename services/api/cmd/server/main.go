package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"focusflow/services/api/internal/server"
	"focusflow/services/api/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := server.Config{
		Addr:            env("API_ADDR", ":8080"),
		DatabaseURL:     env("DATABASE_URL", "postgres://focusflow:focusflow@localhost:5432/focusflow?sslmode=disable"),
		AppOrigin:       env("APP_ORIGIN", "http://localhost:5173"),
		Environment:     env("APP_ENV", "development"),
		SessionTTL:      time.Duration(envInt("SESSION_TTL_HOURS", 720)) * time.Hour,
		StaticDir:       env("STATIC_DIR", "/app/web"),
		VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:    env("VAPID_SUBJECT", "mailto:admin@example.com"),
	}
	if _, err := os.Stat(cfg.StaticDir); err != nil {
		cfg.StaticDir = ""
	}

	if err := runMigrations(ctx, cfg.DatabaseURL); err != nil {
		logger.Error("migration_failed", "error", err)
		os.Exit(1)
	}
	app, err := server.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("server_init_failed", "error", err)
		os.Exit(1)
	}
	defer app.Close()
	go app.StartWorker(ctx)

	httpServer := &http.Server{
		Addr: cfg.Addr, Handler: app.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second,
	}
	go func() {
		logger.Info("server_started", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server_failed", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdown)
}

func runMigrations(ctx context.Context, databaseURL string) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	raw, err := migrations.Files.ReadFile("00001_init.sql")
	if err != nil {
		return err
	}
	sql := string(raw)
	if idx := strings.Index(sql, "-- +goose Down"); idx >= 0 {
		sql = sql[:idx]
	}
	_, err = pool.Exec(ctx, sql)
	return err
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return fallback
}
