package store

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Containers often report 1 CPU → pgx default MaxConns is 4, which
	// saturates under ITMAP concurrent reads and stalls /healthz Ping.
	if cfg.MaxConns <= 4 {
		cfg.MaxConns = 16
	}
	if v := os.Getenv("KC_DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("KC_DB_MAX_CONNS: invalid value %q", v)
		}
		cfg.MaxConns = int32(n)
	}
	if cfg.MinConns == 0 {
		cfg.MinConns = 2
	}
	if cfg.MaxConnIdleTime == 0 {
		cfg.MaxConnIdleTime = 5 * time.Minute
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	db := stdlib.OpenDBFromPool(pool)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, db, dir); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
