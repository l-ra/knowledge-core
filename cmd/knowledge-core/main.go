package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rasekl/knowledge-core/internal/auth"
	"github.com/rasekl/knowledge-core/internal/config"
	apihttp "github.com/rasekl/knowledge-core/internal/api/http"
	"github.com/rasekl/knowledge-core/internal/engine"
	"github.com/rasekl/knowledge-core/internal/store"
)

func main() {
	migrationsDir := flag.String("migrations", "migrations", "path to goose migrations")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	setupLogger(cfg.LogLevel)

	ctx := context.Background()
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	migPath, err := filepath.Abs(*migrationsDir)
	if err != nil {
		slog.Error("migrations path", "err", err)
		os.Exit(1)
	}
	if err := store.Migrate(ctx, pool, migPath); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}

	st := store.New(pool)
	authEng := auth.NewEngine(st, cfg.BootstrapAdminSubject)
	if err := authEng.Reload(ctx); err != nil {
		slog.Error("auth policies", "err", err)
		os.Exit(1)
	}
	eng := engine.New(st, authEng)
	handler := apihttp.New(eng, st, apihttp.NewAuthenticator(cfg))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func setupLogger(level string) {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})))
}
