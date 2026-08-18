package database

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxConnectRetries = 5
	retryDelay        = 2 * time.Second
)

// Connect 建立数据库连接，带重试机制。
// 容器环境下 postgres 可能比应用晚启动，重试可以避免启动失败。
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	var pool *pgxpool.Pool
	var err error

	for attempt := 1; attempt <= maxConnectRetries; attempt++ {
		pool, err = pgxpool.New(ctx, postgresURL(databaseURL))
		if err != nil {
			log.Printf("[database] connect attempt %d/%d failed: %v", attempt, maxConnectRetries, err)
			if attempt < maxConnectRetries {
				time.Sleep(retryDelay)
				continue
			}
			return nil, fmt.Errorf("failed after %d attempts: %w", maxConnectRetries, err)
		}

		if err = pool.Ping(ctx); err != nil {
			pool.Close()
			log.Printf("[database] ping attempt %d/%d failed: %v", attempt, maxConnectRetries, err)
			if attempt < maxConnectRetries {
				time.Sleep(retryDelay)
				continue
			}
			return nil, fmt.Errorf("ping failed after %d attempts: %w", maxConnectRetries, err)
		}

		log.Printf("[database] connected successfully on attempt %d", attempt)
		return pool, nil
	}

	return nil, err
}

func postgresURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return databaseURL
	}
	query := parsed.Query()
	query.Del("schema")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
