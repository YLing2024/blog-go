package httpapi

import "strings"

// routeStage 是路由所属的中间件阶段，决定它在旧 Express 挂载链里的位置与鉴权方式。
type routeStage int

const (
	// stagePublic 是公开读：posts / collections / uploads（无需鉴权）。
	stagePublic routeStage = iota
	// stageBuiltin 是自带账号入口（auth-mode 之外仅在 builtin 模式存在；sso 下一律 404）。
	stageBuiltin
	// stageAdmin 是管理写：全部 /admin/* 业务路由（硬鉴权）。
	stageAdmin
)

// apiRoute 是一条路由。pattern 既是匹配依据，也是 scripts/check-api-docs.mjs 与
// docs/API.md 机检对照的字面量锚点（形如 "METHOD /path"）。
type apiRoute struct {
	pattern      string
	method       string
	routePath    string
	stage        routeStage
	optionalAuth bool
	authRequired bool
	handler      Handler
}

// routeTable 是全部 API 路由，按旧实现 auth 路由 + posts 路由的注册顺序排列；顺序即优先级。
//
// 注意 GET /api/blog/auth-mode 放在 stagePublic：它是「协议类端点」，两种认证模式下都必须可用
// （sso 下只有 login/logout/me 是 404）。
func (s *Server) routeTable() []apiRoute {
	routes := []apiRoute{
		// ① 公开读（免鉴权；posts/:slug 走软鉴权以便登录态预览草稿）
		{pattern: "GET /api/blog/auth-mode", stage: stagePublic, handler: s.handleAuthMode},
		{pattern: "GET /api/blog/posts", stage: stagePublic, handler: s.handlePostsList},
		{pattern: "GET /api/blog/posts/:slug", stage: stagePublic, optionalAuth: true, handler: s.handlePostDetail},
		{pattern: "GET /api/blog/collections", stage: stagePublic, handler: s.handleCollectionsList},
		{pattern: "GET /api/blog/collections/:slug", stage: stagePublic, handler: s.handleCollectionDetail},
		{pattern: "GET /api/blog/uploads/:name", stage: stagePublic, handler: s.handleUploadGet},

		// ② 自带账号入口（sso 下 404）
		{pattern: "POST /api/blog/admin/login", stage: stageBuiltin, handler: s.handleLogin},
		{pattern: "POST /api/blog/admin/logout", stage: stageBuiltin, handler: s.handleLogout},
		{pattern: "GET /api/blog/admin/me", stage: stageBuiltin, authRequired: true, handler: s.handleMe},

		// ③ 管理写（硬鉴权）
		{pattern: "GET /api/blog/admin/posts", stage: stageAdmin, handler: s.handleAdminPostsList},
		{pattern: "GET /api/blog/admin/posts/:id/preview-link", stage: stageAdmin, handler: s.handlePreviewLink},
		{pattern: "POST /api/blog/admin/posts", stage: stageAdmin, handler: s.handlePostCreate},
		{pattern: "PUT /api/blog/admin/posts/:id", stage: stageAdmin, handler: s.handlePostUpdate},
		{pattern: "DELETE /api/blog/admin/posts/:id", stage: stageAdmin, handler: s.handlePostDelete},
		{pattern: "GET /api/blog/admin/collections", stage: stageAdmin, handler: s.handleAdminCollectionsList},
		{pattern: "POST /api/blog/admin/collections", stage: stageAdmin, handler: s.handleCollectionCreate},
		{pattern: "PUT /api/blog/admin/collections/:id", stage: stageAdmin, handler: s.handleCollectionUpdate},
		{pattern: "DELETE /api/blog/admin/collections/:id", stage: stageAdmin, handler: s.handleCollectionDelete},
		{pattern: "POST /api/blog/admin/upload", stage: stageAdmin, handler: s.handleUploadPost},
	}
	for i := range routes {
		routes[i].method, routes[i].routePath = splitPattern(routes[i].pattern)
	}
	return routes
}

// splitPattern 把 "METHOD /path" 拆成方法与路径。
func splitPattern(pattern string) (string, string) {
	if idx := strings.IndexByte(pattern, ' '); idx >= 0 {
		return pattern[:idx], pattern[idx+1:]
	}
	return "", pattern
}
