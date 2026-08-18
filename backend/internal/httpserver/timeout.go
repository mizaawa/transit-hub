package httpserver

import (
	"context"
	"net/http"
	"time"
)

// TimeoutMiddleware 为每个请求添加超时控制，防止请求挂起
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			// 创建一个通道来检测处理是否完成
			done := make(chan struct{})
			
			// 在新的 goroutine 中处理请求
			go func() {
				next.ServeHTTP(w, r.WithContext(ctx))
				close(done)
			}()

			// 等待处理完成或超时
			select {
			case <-done:
				// 请求正常完成
				return
			case <-ctx.Done():
				// 请求超时
				if ctx.Err() == context.DeadlineExceeded {
					http.Error(w, `{"message":"request timeout"}`, http.StatusGatewayTimeout)
				}
			}
		})
	}
}

// RequestIDMiddleware 为每个请求添加唯一ID，便于追踪和调试
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			// 如果客户端没有提供，生成一个简单的ID
			requestID = generateRequestID()
		}

		// 将 Request ID 添加到响应头
		w.Header().Set("X-Request-ID", requestID)

		// 将 Request ID 添加到上下文中，供日志使用
		ctx := context.WithValue(r.Context(), "request_id", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// 生成简单的请求ID（实际项目中可以使用 UUID）
func generateRequestID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}
