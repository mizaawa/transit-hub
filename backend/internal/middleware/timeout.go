package middleware

import (
	"context"
	"net/http"
	"time"

	"transithub/backend/internal/shared/httpjson"
)

// Timeout 为请求添加超时控制，防止长时间挂起的请求占用资源
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)

			done := make(chan struct{})
			go func() {
				next.ServeHTTP(w, r)
				close(done)
			}()

			select {
			case <-done:
				return
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					httpjson.WriteError(w, http.StatusGatewayTimeout, "request timeout")
				}
			}
		})
	}
}
