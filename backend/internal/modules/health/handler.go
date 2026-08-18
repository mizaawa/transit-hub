package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"transithub/backend/internal/shared/httpjson"
)

type response struct {
	Status       string            `json:"status"`
	Timestamp    string            `json:"timestamp"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
	Version      string            `json:"version,omitempty"`
}

var (
	db    *pgxpool.Pool
	redis *redis.Client
)

// Initialize 初始化健康检查所需的依赖。
func Initialize(database *pgxpool.Pool, redisClient *redis.Client) {
	db = database
	redis = redisClient
}

func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", check)
	mux.HandleFunc("GET /api/health/detailed", detailed)
}

func check(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, response{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Version:   "1.0.0",
	})
}

func detailed(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	deps := make(map[string]string)
	overallStatus := "healthy"

	// 检查数据库
	if db != nil {
		if err := db.Ping(ctx); err != nil {
			deps["database"] = "unhealthy: " + err.Error()
			overallStatus = "degraded"
		} else {
			stats := db.Stat()
			deps["database"] = "healthy"
			deps["database_connections"] = formatStats(stats)
		}
	} else {
		deps["database"] = "not_initialized"
		overallStatus = "degraded"
	}

	// 检查 Redis
	if redis != nil {
		if err := redis.Ping(ctx).Err(); err != nil {
			deps["redis"] = "unhealthy: " + err.Error()
			overallStatus = "degraded"
		} else {
			deps["redis"] = "healthy"
		}
	} else {
		deps["redis"] = "not_initialized"
		overallStatus = "degraded"
	}

	statusCode := http.StatusOK
	if overallStatus == "degraded" {
		statusCode = http.StatusServiceUnavailable
	}

	httpjson.Write(w, statusCode, response{
		Status:       overallStatus,
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		Dependencies: deps,
		Version:      "1.0.0",
	})
}

func formatStats(stats *pgxpool.Stat) string {
	return fmt.Sprintf("total:%d/idle:%d/acquired:%d/max:%d",
		stats.TotalConns(), stats.IdleConns(), stats.AcquiredConns(), stats.MaxConns())
}
