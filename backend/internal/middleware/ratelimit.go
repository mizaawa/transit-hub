package middleware

import (
	"net/http"
	"sync"
	"time"

	"transithub/backend/internal/shared/httpjson"
)

// rateLimiter 基于 token bucket 算法的限流器
type rateLimiter struct {
	tokens    int
	maxTokens int
	refillRate time.Duration
	mu        sync.Mutex
	lastRefill time.Time
}

func newRateLimiter(maxTokens int, refillRate time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	
	// 补充 tokens
	if elapsed >= rl.refillRate {
		tokensToAdd := int(elapsed / rl.refillRate)
		rl.tokens = min(rl.maxTokens, rl.tokens+tokensToAdd)
		rl.lastRefill = now
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RateLimit 全局限流中间件，防止过多请求压垮服务
func RateLimit(requestsPerSecond int) func(http.Handler) http.Handler {
	limiter := newRateLimiter(requestsPerSecond, time.Second/time.Duration(requestsPerSecond))
	
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.allow() {
				httpjson.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
