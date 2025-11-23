package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ocenb/pr-assignment/internal/config"
)

func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to parse db config: %w", err)
	}

	poolConfig.MaxConns = cfg.Postgres.MaxOpenConns
	poolConfig.MinConns = cfg.Postgres.MaxIdleConns
	poolConfig.MaxConnLifetime = cfg.Postgres.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to db: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}

	return pool, nil
}
