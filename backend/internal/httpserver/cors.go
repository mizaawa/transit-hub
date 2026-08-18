package httpserver

import (
	"net/http"
	"strings"
)

// CORS 配置选项
type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// 默认 CORS 配置
func DefaultCORSOptions() CORSOptions {
	return CORSOptions{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-Request-ID",
		},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
	}
}

// CORS 中间件：处理跨域请求
func CORSMiddleware(options CORSOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// 检查是否允许该源
			allowed := false
			for _, allowedOrigin := range options.AllowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}

			if !allowed && origin != "" {
				// 源不在允许列表中，但继续处理请求（不设置 CORS 头）
				next.ServeHTTP(w, r)
				return
			}

			// 设置 CORS 头
			if origin != "" {
				if len(options.AllowedOrigins) == 1 && options.AllowedOrigins[0] == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			}

			if options.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if len(options.AllowedMethods) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(options.AllowedMethods, ", "))
			}

			if len(options.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(options.AllowedHeaders, ", "))
			}

			if len(options.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(options.ExposedHeaders, ", "))
			}

			if options.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", string(rune(options.MaxAge)))
			}

			// 处理预检请求
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
