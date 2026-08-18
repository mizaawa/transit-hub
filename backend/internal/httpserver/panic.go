package httpserver

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"

	"transithub/backend/internal/httpjson"
)

// panicRecovery 捕获 handler 中的 panic，防止单个请求崩溃整个服务。
func panicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("[panic] %s %s: %v\n%s", r.Method, r.URL.Path, recovered, debug.Stack())
				
				// 如果响应头已经发送，无法再写入错误
				if w.Header().Get("Content-Type") == "" {
					httpjson.WriteError(w, http.StatusInternalServerError, "server.errors.internal")
				} else {
					// 只能记录日志
					log.Printf("[panic] response already sent, cannot write error response")
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// mustNot 用于启动阶段检查致命错误。
func mustNot(err error, msg string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", msg, err))
	}
}
