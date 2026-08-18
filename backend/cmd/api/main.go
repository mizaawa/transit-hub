package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"transithub/backend/internal/config"
	"transithub/backend/internal/database"
	"transithub/backend/internal/database/migrations"
	"transithub/backend/internal/httpserver"
)

func main() {
	cfg := config.Load()

	// 启动时验证配置，尽早发现问题
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx := context.Background()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	// 数据库迁移：连接 DB 后立即执行，保证表结构就绪后再初始化业务模块
	if err := migrations.Run(ctx, db); err != nil {
		log.Fatalf("[migrations] %v", err)
	}

	// Redis 用于仪表盘 admin 会话存储与令牌自动刷新调度。
	redisClient, err := database.ConnectRedis(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("connect redis: %v", err)
	}
	defer redisClient.Close()

	// 启动时验证所有关键依赖连接是否可用
	log.Println("verifying dependencies...")
	if err := database.PingDatabase(ctx, db); err != nil {
		log.Fatalf("database not reachable: %v", err)
	}
	if err := database.PingRedis(ctx, redisClient); err != nil {
		log.Fatalf("redis not reachable: %v", err)
	}
	log.Println("all dependencies healthy")

	// 启动后台健康监控，每 30 秒探测一次连接状态
	monitorCtx, cancelMonitor := context.WithCancel(ctx)
	defer cancelMonitor()
	database.StartHealthMonitor(monitorCtx, db, redisClient, 30*time.Second)

	server := httpserver.New(cfg, db, redisClient)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("backend listening on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server background shutdown: %v", err)
	}
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Println("server stopped")
}
