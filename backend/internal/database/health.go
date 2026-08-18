package database

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// PingDatabase 检查数据库连接是否健康。
func PingDatabase(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return pool.Ping(ctx)
}

// PingRedis 检查 Redis 连接是否健康。
func PingRedis(ctx context.Context, client *redis.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}

// StartHealthMonitor 启动后台健康检查，定期探测数据库和 Redis 连接状态。
// 连接异常时记录日志但不中断服务，依赖连接池自身的重连机制。
func StartHealthMonitor(ctx context.Context, pool *pgxpool.Pool, redisClient *redis.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[health] monitor stopped")
				return
			case <-ticker.C:
				// 检查数据库连接池状态
				stats := pool.Stat()
				log.Printf("[health] database pool - total: %d, idle: %d, acquired: %d, max: %d",
					stats.TotalConns(), stats.IdleConns(), stats.AcquiredConns(), stats.MaxConns())

				if err := PingDatabase(ctx, pool); err != nil {
					log.Printf("[health] ⚠️ database ping failed: %v", err)
				} else {
					log.Println("[health] ✓ database healthy")
				}

				// 检查 Redis 连接
				if err := PingRedis(ctx, redisClient); err != nil {
					log.Printf("[health] ⚠️ redis ping failed: %v", err)
				} else {
					log.Println("[health] ✓ redis healthy")
				}
			}
		}
	}()
	log.Printf("[health] monitor started, interval: %v", interval)
}
