package httpserver

import (
	"log"
	"net/http"
	"runtime/debug"

	"transithub/backend/internal/shared/httpjson"
)

// panicRecovery 在 handler panic 时恢复，避免单个请求崩溃整个服务。
// 记录堆栈并返回 500，让客户端知道请求失败而不是超时。
func panicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("[panic] %s %s: %v\n%s", r.Method, r.URL.Path, recovered, debug.Stack())
				httpjson.WriteError(w, http.StatusInternalServerError, "api.errors.internal")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
