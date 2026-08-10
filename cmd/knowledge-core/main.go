package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	apihttp "github.com/l-ra/knowledge-core/internal/api/http"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/bootstrap"
	"github.com/l-ra/knowledge-core/internal/config"
	"github.com/l-ra/knowledge-core/internal/engine"
	"github.com/l-ra/knowledge-core/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			os.Exit(runAdmin(os.Args[2:]))
		case "serve", "server":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			runServer()
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}
	runServer()
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  knowledge-core [serve] [flags]
  knowledge-core admin reset-password

Environment:
  KC_DATABASE_URL, KC_AUTH_MODE (dev|oidc|bootstrap), KC_BOOTSTRAP_PASSWORD_FILE, ...

`)
}

func runAdmin(args []string) int {
	if len(args) == 0 || args[0] != "reset-password" {
		fmt.Fprintln(os.Stderr, "usage: knowledge-core admin reset-password")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}
	if cfg.BootstrapPasswordFile != "" {
		_ = os.Setenv("KC_BOOTSTRAP_PASSWORD_FILE", cfg.BootstrapPasswordFile)
	}
	if cfg.AuthMode != "bootstrap" {
		fmt.Fprintln(os.Stderr, "KC_AUTH_MODE must be bootstrap")
		return 1
	}
	pw, err := bootstrap.ResetPassword()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset:", err)
		return 1
	}
	fmt.Printf("bootstrap admin password reset\nsubject=%s\npassword=%s\n", cfg.BootstrapAdminSubject, pw)
	return 0
}

func runServer() {
	migrationsDir := flag.String("migrations", "migrations", "path to goose migrations")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	setupLogger(cfg.LogLevel)

	if cfg.AuthMode == "bootstrap" {
		if cfg.BootstrapPasswordFile != "" {
			_ = os.Setenv("KC_BOOTSTRAP_PASSWORD_FILE", cfg.BootstrapPasswordFile)
		}
		pw, created, err := bootstrap.EnsurePassword()
		if err != nil {
			slog.Error("bootstrap password", "err", err)
			os.Exit(1)
		}
		if created {
			slog.Warn("bootstrap admin password generated",
				"subject", cfg.BootstrapAdminSubject,
				"password", pw,
				"file", bootstrap.PasswordFile(),
				"hint", "use Authorization: Bearer <password> or X-Admin-Password header",
			)
		}
	}

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
		slog.Info("listening", "addr", cfg.HTTPAddr, "authMode", cfg.AuthMode)
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
