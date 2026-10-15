package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAllowedMethods 固化 Express Router 的 OPTIONS 方法集合：按注册顺序、去重、GET 附带 HEAD。
// 期望值来自旧实现（Express 4.22 + 旧路由）实测。
func TestAllowedMethods(t *testing.T) {
	s := New(Options{})
	cases := []struct {
		path string
		want string
	}{
		{"/api/blog/auth-mode", "GET,HEAD"},
		{"/api/blog/posts", "GET,HEAD"},
		{"/api/blog/posts/hello-world", "GET,HEAD"},
		{"/api/blog/collections", "GET,HEAD"},
		{"/api/blog/collections/whatever", "GET,HEAD"},
		{"/api/blog/uploads/a.png", "GET,HEAD"},
		{"/api/blog/admin/login", "POST"},
		{"/api/blog/admin/logout", "POST"},
		{"/api/blog/admin/me", "GET,HEAD"},
		{"/api/blog/admin/posts", "GET,HEAD,POST"},
		{"/api/blog/admin/posts/1", "PUT,DELETE"},
		{"/api/blog/admin/posts/1/preview-link", "GET,HEAD"},
		{"/api/blog/admin/collections", "GET,HEAD,POST"},
		{"/api/blog/admin/collections/1", "PUT,DELETE"},
		{"/api/blog/admin/upload", "POST"},
		{"/api/blog/nope", ""},
		{"/api/blog", ""},
		{"/api/blog/", ""},
	}
	for _, c := range cases {
		if got := s.allowedMethods(c.path); got != c.want {
			t.Errorf("allowedMethods(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestOptionsAutoFallback 验证匹配路径回 200 + Allow + 体同串，未匹配走既有 404。
func TestOptionsAutoFallback(t *testing.T) {
	s := New(Options{})
	h := s.Handler()

	matched := httptest.NewRecorder()
	h.ServeHTTP(matched, httptest.NewRequest(http.MethodOptions, "/api/blog/admin/posts", nil))
	if matched.Code != http.StatusOK {
		t.Fatalf("匹配 OPTIONS 状态 = %d, want 200", matched.Code)
	}
	if got := matched.Header().Get("Allow"); got != "GET,HEAD,POST" {
		t.Errorf("Allow = %q, want %q", got, "GET,HEAD,POST")
	}
	if got := matched.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/html; charset=utf-8")
	}
	if got := matched.Body.String(); got != "GET,HEAD,POST" {
		t.Errorf("体 = %q, want %q", got, "GET,HEAD,POST")
	}

	unmatched := httptest.NewRecorder()
	h.ServeHTTP(unmatched, httptest.NewRequest(http.MethodOptions, "/api/blog/nope", nil))
	if unmatched.Code != http.StatusNotFound {
		t.Fatalf("未匹配 OPTIONS 状态 = %d, want 404", unmatched.Code)
	}
	if got := unmatched.Body.String(); got != `{"message":"接口不存在"}` {
		t.Errorf("未匹配体 = %q", got)
	}
}

// TestHeadFallsBackToGetHandler 验证 HEAD 命中只有 GET 的路由时走 GET 处理器：状态/头同 GET，体为空。
func TestHeadFallsBackToGetHandler(t *testing.T) {
	s := New(Options{})
	called := false
	s.routes = []apiRoute{{
		pattern:   "GET /api/blog/only-get",
		method:    http.MethodGet,
		routePath: "/api/blog/only-get",
		stage:     stagePublic,
		handler: func(*Context) (Response, error) {
			called = true
			return Response{Status: http.StatusOK, Body: rawBody([]byte("hello"))}, nil
		},
	}}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/blog/only-get", nil))
	if !called {
		t.Fatal("HEAD 未走 GET 处理器")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("状态 = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD 体应为空，实际 %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Length"); got != "5" {
		t.Errorf("Content-Length = %q, want 5（与 GET 体一致）", got)
	}
}

// TestHeadSkipsNonGetRoute 验证 HEAD 命中只有非 GET 方法的路由时回落 404（与旧实现一致）。
func TestHeadSkipsNonGetRoute(t *testing.T) {
	s := New(Options{})
	s.routes = []apiRoute{{
		pattern:   "PUT /api/blog/only-put",
		method:    http.MethodPut,
		routePath: "/api/blog/only-put",
		stage:     stageAdmin,
		handler: func(*Context) (Response, error) {
			t.Fatal("PUT 处理器不应被 HEAD 调用")
			return Response{}, nil
		},
	}}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/blog/only-put", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("状态 = %d, want 404", rec.Code)
	}
}
