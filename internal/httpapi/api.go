// Package httpapi 用标准库 net/http 复刻旧实现（Express 4）的 HTTP 契约，并额外托管嵌入的前端站点。
//
// 装配与旧 server/src/index.ts 对齐：JSON 解析 → 认证入口（/auth-mode 免鉴权；
// builtin 的 login/logout/me）→ 公开读（posts / collections / uploads）→
// 管理写（admin posts / collections / upload / preview-link）→ 404 兜底 →
// 其余 GET 交给静态托管 + SPA 回退（旧实现由 nginx 承担，这里内建）。
//
// 派生行为与旧实现一致：路由 404 一律 `{message:'接口不存在'}`；请求体不合法的 JSON 归为 500；
// sso 模式下自带账号三个端点一律 404；builtin 模式忽略 X-Auth-User，不因外部头提权。
package httpapi

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"blog-go/internal/auth"
	"blog-go/internal/collections"
	"blog-go/internal/config"
	"blog-go/internal/posts"
)

// 响应内容类型（字符集与旧实现一致）。
const (
	contentTypeJSON = "application/json; charset=utf-8"
	contentTypeHTML = "text/html; charset=UTF-8"
)

// Options 是装配参数。
type Options struct {
	Config      *config.Config
	Auth        *auth.Authenticator
	Posts       *posts.Repo
	Collections *collections.Repo
	StaticFS    fs.FS
	UploadDir   string
	Logger      *slog.Logger
}

// Server 是 HTTP 装配体。
type Server struct {
	cfg         *config.Config
	auth        *auth.Authenticator
	posts       *posts.Repo
	collections *collections.Repo
	static      fs.FS
	uploadDir   string
	log         *slog.Logger
	routes      []apiRoute

	// siteURLWarned 保证「PUBLIC_SITE_URL 未配置」只告警一次（对齐旧实现）。
	siteURLWarned sync.Once
}

// New 构造服务并解析路由表。
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Server{
		cfg:         opts.Config,
		auth:        opts.Auth,
		posts:       opts.Posts,
		collections: opts.Collections,
		static:      opts.StaticFS,
		uploadDir:   opts.UploadDir,
		log:         log,
	}
	s.routes = s.routeTable()
	return s
}

// Context 是单次请求的处理上下文。
type Context struct {
	// Method 是原始方法。
	Method string
	// Path 是 URL 路径。
	Path string
	// Query 是解析后的查询参数（仅保留首个值）。
	Query url.Values
	// Body 是解析后的 JSON 顶层对象字段。
	Body map[string]json.RawMessage
	// Params 是路径参数（:slug / :id / :name）。
	Params map[string]string
	// User 是当前身份；HasUser 为假表示匿名。
	User    auth.User
	HasUser bool
	// Request 是原始请求。
	Request *http.Request
	// Writer 是响应写入器（上传需要 MaxBytesReader）。
	Writer http.ResponseWriter
}

// Response 是处理结果。
type Response struct {
	Status      int
	Body        any
	ContentType string
	Headers     map[string]string
}

// Handler 是单条路由的处理函数。
type Handler func(*Context) (Response, error)

// Handler 返回顶层 http.Handler。
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

// serveHTTP 是主分发。
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	head := r.Method == http.MethodHead
	method := r.Method
	if head {
		method = http.MethodGet
	}
	path := r.URL.Path

	body, err := s.parseBody(r)
	if err != nil {
		s.write(w, head, Response{Status: http.StatusInternalServerError, Body: messageJSON("服务器内部错误")})
		return
	}
	ctx := &Context{
		Method:  r.Method,
		Path:    path,
		Query:   firstValues(r.URL.RawQuery),
		Body:    body,
		Request: r,
		Writer:  w,
	}

	if isBlogAPI(path) {
		// ① 公开读
		if route, params, ok := s.matchStage(stagePublic, method, path); ok {
			ctx.Params = params
			if route.optionalAuth {
				ctx.User, ctx.HasUser = s.auth.Optional(r.Context(), r)
			}
			s.run(w, head, ctx, route)
			return
		}
		// ② 自带账号入口（sso 下一律 404）
		if route, params, ok := s.matchStage(stageBuiltin, method, path); ok {
			if s.cfg.AuthMode == config.AuthSSO {
				s.write(w, head, notFound())
				return
			}
			ctx.Params = params
			if route.authRequired {
				user, ok, msg := s.auth.Require(r.Context(), r)
				if !ok {
					s.write(w, head, Response{Status: http.StatusUnauthorized, Body: messageJSON(msg)})
					return
				}
				ctx.User, ctx.HasUser = user, true
			}
			s.run(w, head, ctx, route)
			return
		}
		// ③ 管理写（需鉴权）
		if route, params, ok := s.matchStage(stageAdmin, method, path); ok {
			user, ok, msg := s.auth.Require(r.Context(), r)
			if !ok {
				s.write(w, head, Response{Status: http.StatusUnauthorized, Body: messageJSON(msg)})
				return
			}
			ctx.User, ctx.HasUser = user, true
			ctx.Params = params
			s.run(w, head, ctx, route)
			return
		}
		s.write(w, head, notFound())
		return
	}

	// ④ 静态托管 + SPA 回退（仅 GET/HEAD）
	if method == http.MethodGet {
		s.serveStatic(w, r, head)
		return
	}
	s.write(w, head, notFound())
}

// parseBody 读取并解析 JSON 请求体；非 JSON 类型或空体返回空对象。
func (s *Server) parseBody(r *http.Request) (map[string]json.RawMessage, error) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return map[string]json.RawMessage{}, nil
	}
	return readJSONBody(r.Body)
}

// run 调用路由处理函数并写出响应。
func (s *Server) run(w http.ResponseWriter, head bool, ctx *Context, route *apiRoute) {
	resp, err := route.handler(ctx)
	if err != nil {
		s.log.Error("请求处理失败: " + err.Error())
		s.write(w, head, Response{Status: http.StatusInternalServerError, Body: messageJSON("服务器内部错误")})
		return
	}
	s.write(w, head, resp)
}

// write 统一写出响应。
func (s *Server) write(w http.ResponseWriter, head bool, resp Response) {
	for name, value := range resp.Headers {
		w.Header().Set(name, value)
	}
	contentType := resp.ContentType
	if contentType == "" {
		contentType = contentTypeJSON
	}
	var payload []byte
	if resp.Body != nil {
		// rawBody 是原样输出的字节（图片回源等），不能走 JSON 编码，否则会被 base64 包装。
		if raw, ok := resp.Body.(rawBody); ok {
			payload = raw
		} else {
			payload = marshalNoEscape(resp.Body)
		}
	}
	w.Header().Set("Content-Type", contentType)
	if head {
		w.Header().Set("Content-Length", itoa(len(payload)))
		w.WriteHeader(resp.Status)
		return
	}
	w.WriteHeader(resp.Status)
	if len(payload) > 0 {
		_, _ = w.Write(payload)
	}
}

// notFound 构造统一 404 体。
func notFound() Response {
	return Response{Status: http.StatusNotFound, Body: messageJSON("接口不存在")}
}

// isBlogAPI 判断路径是否落在 /api/blog 挂载点（Express 挂载匹配：等于 /api/blog 或以 /api/blog/ 开头）。
func isBlogAPI(path string) bool {
	return path == "/api/blog" || strings.HasPrefix(path, "/api/blog/")
}

// --- 路由匹配 ---

// matchStage 在当前阶段里按注册顺序找第一条命中（方法 + 路径）的路由。
func (s *Server) matchStage(stage routeStage, method, path string) (*apiRoute, map[string]string, bool) {
	for i := range s.routes {
		rt := &s.routes[i]
		if rt.stage != stage || rt.method != method {
			continue
		}
		if params, ok := matchRoutePath(rt.routePath, path); ok {
			return rt, params, true
		}
	}
	return nil, nil, false
}

// matchRoutePath 把请求路径与路由模板比对：支持 :name 段，容忍单个尾斜杠。
func matchRoutePath(pattern, path string) (map[string]string, bool) {
	pseg := splitPath(pattern)
	hseg := splitPath(path)
	if len(pseg) != len(hseg) {
		return nil, false
	}
	params := map[string]string{}
	for i, want := range pseg {
		got := hseg[i]
		if strings.HasPrefix(want, ":") {
			decoded, err := url.PathUnescape(got)
			if err != nil {
				return nil, false
			}
			params[want[1:]] = decoded
			continue
		}
		if want != got {
			return nil, false
		}
	}
	return params, true
}

// splitPath 去掉首尾斜杠后按 / 切段。
func splitPath(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// firstValues 解析查询串，重复键只保留首个值。
func firstValues(rawQuery string) url.Values {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return url.Values{}
	}
	out := url.Values{}
	for key, list := range values {
		if len(list) > 0 {
			out.Set(key, list[0])
		}
	}
	return out
}

// itoa 是十进制整数转换。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
