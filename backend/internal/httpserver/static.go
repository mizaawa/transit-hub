package httpserver

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticHandler 提供前端静态文件服务和 Vue history 路由回退。
// API 请求（/api/）不经过此 handler，由调用方在路由层分流。
func staticHandler(publicDir string) http.Handler {
	fs := http.Dir(publicDir)
	fileServer := http.FileServer(fs)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// history 回退只对读方法有意义。对未知路径上的 POST/PUT/DELETE 返回
		// index.html 会让前端把一份 HTML 当成 JSON 解析，产生
		// `Unexpected token '<', "<script sr"...` 这类看不懂的报错；
		// 直接回 405 能让问题在第一现场就暴露出来。
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 安全：禁止路径穿越。用 path.Clean 处理 URL（始终以 / 分隔），
		// 不要用 filepath.Clean——它在 Windows 上会把 \ 当分隔符，
		// 导致跨平台行为不一致。
		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if strings.Contains(cleanPath, "..") {
			http.NotFound(w, r)
			return
		}

		// 检查文件是否存在
		fullPath := filepath.Join(publicDir, filepath.FromSlash(cleanPath))
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			// 构建产物的文件名带内容 hash，可以长期强缓存；
			// 其余静态文件交给 FileServer 的默认协商缓存。
			if strings.HasPrefix(cleanPath, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// 文件不存在或是目录，回退到 index.html（Vue history 路由）
		serveIndex(w, r, publicDir)
	})
}

// serveIndex 返回 SPA 外壳。index.html 必须禁用缓存：它引用的是带 hash 的资源名，
// 升级后若浏览器/中间层仍复用旧的 index.html，就会去请求已经不存在的旧资源，
// 表现为升级后页面白屏或功能异常，且强制刷新才能恢复。
func serveIndex(w http.ResponseWriter, r *http.Request, publicDir string) {
	indexPath := filepath.Join(publicDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, indexPath)
}
