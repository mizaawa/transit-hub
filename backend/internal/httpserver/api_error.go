package httpserver

import (
	"net/http"
	"strings"

	"transithub/backend/internal/shared/httpjson"
)

// apiErrorRewriter 把 net/http ServeMux 内置的 404/405 纯文本响应改写成 JSON。
//
// 背景：所有 /api 响应都应该是 JSON。ServeMux 对未注册路径返回 "404 page not found"，
// 对方法不匹配返回 "405 method not allowed"，两者都是 text/plain。前端请求层拿到
// 非 JSON 响应只能降级成一个笼统的错误 key，既看不出「接口不存在」还是「参数不对」，
// 也让滚动升级期间的降级逻辑无法按状态码判断。
//
// 只改写这两个状态码，且仅在业务 handler 没有自己写过响应体时生效，
// 因此不会影响任何模块已有的错误语义。
type apiErrorRewriter struct {
	http.ResponseWriter
	rewriting bool
	handled   bool
}

func newAPIErrorRewriter(w http.ResponseWriter) *apiErrorRewriter {
	return &apiErrorRewriter{ResponseWriter: w}
}

func (w *apiErrorRewriter) WriteHeader(status int) {
	// 只改写「非 JSON 的 404/405」。ServeMux 的兜底分支走 http.Error，
	// 会在 WriteHeader 之前把 Content-Type 设成 text/plain；而业务 handler
	// 一律经过 httpjson.Write，Content-Type 已是 application/json。
	// 因此用「Content-Type 不含 json」来区分两者，不能判断它是否为空。
	if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
		!strings.Contains(w.Header().Get("Content-Type"), "json") {
		w.rewriting = true
		messageKey := "api.errors.notFound"
		if status == http.StatusMethodNotAllowed {
			messageKey = "api.errors.methodNotAllowed"
		}
		// http.Error 预设的 Content-Length 针对纯文本 body，长度与 JSON 不符，
		// 必须清掉，交给 net/http 重新计算。
		w.Header().Del("Content-Length")
		httpjson.Write(w.ResponseWriter, status, httpjson.ErrorResponse{Message: messageKey})
		w.handled = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiErrorRewriter) Write(body []byte) (int, error) {
	if w.rewriting {
		// 丢弃 ServeMux 的纯文本 body，JSON 已在 WriteHeader 中写出。
		// 返回原长度以满足 io.Writer 约定，避免调用方误判为写入不完整。
		return len(body), nil
	}
	if !w.handled {
		w.handled = true
	}
	return w.ResponseWriter.Write(body)
}

// Flush 透传底层 Flusher，保证 SSE 等流式响应仍可实时刷新。
func (w *apiErrorRewriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
