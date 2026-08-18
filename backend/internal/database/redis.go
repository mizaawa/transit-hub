package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisMaxConnectRetries = 5
	redisRetryDelay        = 2 * time.Second
)

// ConnectRedis 建立 Redis 连接并做一次 Ping 健康检查，带重试机制。
//
// 仪表盘的 admin 会话（access/refresh token、到期时间、刷新时间）保存在 Redis 中，
// 后台刷新协程也依赖它来扫描临期会话。连接参数统一通过标准的 redis URL 解析，
// 例如 redis://127.0.0.1:6379/0。
func ConnectRedis(ctx context.Context, redisURL string) (*redis.Client, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}

	var client *redis.Client
	for attempt := 1; attempt <= redisMaxConnectRetries; attempt++ {
		client = redis.NewClient(options)
		if err = client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			log.Printf("[redis] connect attempt %d/%d failed: %v", attempt, redisMaxConnectRetries, err)
			if attempt < redisMaxConnectRetries {
				time.Sleep(redisRetryDelay)
				continue
			}
			return nil, fmt.Errorf("ping failed after %d attempts: %w", redisMaxConnectRetries, err)
		}

		log.Printf("[redis] connected successfully on attempt %d", attempt)
		return client, nil
	}

	return nil, err
}
