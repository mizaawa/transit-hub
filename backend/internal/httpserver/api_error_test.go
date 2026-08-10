package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"transithub/backend/internal/shared/httpjson"
)

// 未注册的 /api 路径必须返回 JSON，否则前端只能把 text/plain 的 404
// 归为「响应不是合法 JSON」，丢失「接口不存在」这个可据以降级的信息。
func TestAPIErrorRewriterConvertsNotFoundToJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/known", func(w http.ResponseWriter, r *http.Request) {})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(newAPIErrorRewriter(recorder), httptest.NewRequest("GET", "/api/unknown", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var payload httpjson.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body=%q)", err, recorder.Body.String())
	}
	if payload.Message != "api.errors.notFound" {
		t.Fatalf("message = %q, want api.errors.notFound", payload.Message)
	}
}

// 方法不匹配必须保留 405，不能被统一伪装成 404：
// 前端的滚动升级降级逻辑要靠这个区别判断后端是否支持该方法。
func TestAPIErrorRewriterPreservesMethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/known", func(w http.ResponseWriter, r *http.Request) {})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(newAPIErrorRewriter(recorder), httptest.NewRequest("PATCH", "/api/known", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	var payload httpjson.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if payload.Message != "api.errors.methodNotAllowed" {
		t.Fatalf("message = %q, want api.errors.methodNotAllowed", payload.Message)
	}
}

// 业务 handler 自己返回的 404 必须原样透传，不能被改写覆盖，
// 否则各模块精心设计的 i18n 错误 key 会全部丢失。
func TestAPIErrorRewriterKeepsHandlerOwnedNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/thing", func(w http.ResponseWriter, r *http.Request) {
		httpjson.WriteError(w, http.StatusNotFound, "admin.tickets.errors.notFound")
	})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(newAPIErrorRewriter(recorder), httptest.NewRequest("GET", "/api/thing", nil))

	var payload httpjson.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if payload.Message != "admin.tickets.errors.notFound" {
		t.Fatalf("message = %q, want the handler's own key", payload.Message)
	}
}

func newStaticTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=app></div>"), 0o600); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return dir
}

// index.html 必须禁用缓存：它引用带 hash 的资源名，
// 升级后复用旧 index.html 会去请求已不存在的旧资源，导致白屏。
func TestStaticHandlerDisablesIndexCaching(t *testing.T) {
	handler := staticHandler(newStaticTestDir(t))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/dashboard", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (history fallback)", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q, want no-store for index.html", got)
	}
}

func TestStaticHandlerCachesHashedAssets(t *testing.T) {
	handler := staticHandler(newStaticTestDir(t))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/assets/app-abc123.js", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want immutable for hashed assets", got)
	}
}

// 对未知路径的写方法返回 index.html 会让前端把 HTML 当 JSON 解析，
// 正是 `Unexpected token '<'` 报错的一个来源；这里必须回 405。
func TestStaticHandlerRejectsNonReadMethods(t *testing.T) {
	handler := staticHandler(newStaticTestDir(t))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("POST", "/some/spa/route", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if got := recorder.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want \"GET, HEAD\"", got)
	}
}

func TestStaticHandlerBlocksPathTraversal(t *testing.T) {
	dir := newStaticTestDir(t)
	secretDir := filepath.Dir(dir)
	if err := os.WriteFile(filepath.Join(secretDir, "secret.txt"), []byte("top secret"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	handler := staticHandler(dir)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/", nil)
	// 绕过 net/http 的自动清理，直接构造带穿越片段的路径。
	request.URL.Path = "/../secret.txt"
	handler.ServeHTTP(recorder, request)

	if recorder.Body.String() == "top secret" {
		t.Fatal("path traversal leaked a file outside the public dir")
	}
}
