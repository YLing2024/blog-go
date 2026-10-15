package httpapi

import (
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blog-go/internal/collections"
	"blog-go/internal/posts"
	"blog-go/internal/tags"
)

// 响应体结构（字段顺序与旧实现逐字一致）。
type (
	authModeBody struct {
		AuthMode string `json:"authMode"`
	}
	loginBody struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Message  string `json:"message"`
	}
	okMessageBody struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	meBody struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	previewBody struct {
		Published bool   `json:"published"`
		URL       string `json:"url"`
	}
	uploadBody struct {
		URL string `json:"url"`
	}
	collectionsListBody struct {
		List []collections.WithCount `json:"list"`
	}
	adminPostsBody struct {
		List []posts.Detail `json:"list"`
	}
)

// --- 认证入口 ---

// handleAuthMode 返回当前认证模式（免鉴权、两种模式都可用）。
func (s *Server) handleAuthMode(_ *Context) (Response, error) {
	return Response{Status: http.StatusOK, Body: authModeBody{AuthMode: string(s.cfg.AuthMode)}}, nil
}

// handleLogin 校验账号口令，签发 JWT 并下发会话 cookie。
func (s *Server) handleLogin(c *Context) (Response, error) {
	usernameRaw, hasUser := c.field("username")
	passwordRaw, hasPass := c.field("password")
	if !hasUser || !coerceTruthy(usernameRaw) || !hasPass || !coerceTruthy(passwordRaw) {
		return Response{Status: http.StatusBadRequest, Body: messageJSON("请输入账号和密码")}, nil
	}
	username := strings.TrimSpace(coerceString(usernameRaw))
	password := coerceString(passwordRaw)

	user, ok, err := s.auth.VerifyCredentials(c.Request.Context(), username, password)
	if err != nil {
		return Response{}, err
	}
	if !ok {
		return Response{Status: http.StatusUnauthorized, Body: messageJSON("账号或密码错误")}, nil
	}

	token, err := s.auth.SignSession(user.ID, user.Username)
	if err != nil {
		return Response{}, err
	}
	s.auth.SetSession(c.Request.Context(), token)
	http.SetCookie(c.Writer, s.auth.SessionCookie(c.Request, token))
	return Response{Status: http.StatusOK, Body: loginBody{Token: token, Username: user.Username, Message: "登录成功"}}, nil
}

// handleLogout 删除会话 + 清 cookie；未登录也 200（幂等）。
func (s *Server) handleLogout(c *Context) (Response, error) {
	token := s.auth.BuiltinToken(c.Request)
	s.auth.DestroySession(c.Request.Context(), token)
	http.SetCookie(c.Writer, s.auth.ClearSessionCookie(c.Request))
	return Response{Status: http.StatusOK, Body: okMessageBody{OK: true, Message: "已退出登录"}}, nil
}

// handleMe 返回当前登录身份。
func (s *Server) handleMe(c *Context) (Response, error) {
	name := c.User.Username
	if name == "" {
		name = "admin"
	}
	role := c.User.Role
	if role == "" {
		role = "admin"
	}
	return Response{Status: http.StatusOK, Body: meBody{Name: name, Role: role}}, nil
}

// --- 公开文章 ---

// handlePostsList 公开文章列表：仅已发布，支持 ?page= 与 ?tag=。
func (s *Server) handlePostsList(c *Context) (Response, error) {
	result, err := s.posts.ListPublic(c.Request.Context(),
		posts.ParsePage(c.Query.Get("page")), strings.TrimSpace(c.Query.Get("tag")))
	if err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: result}, nil
}

// handlePostDetail 文章详情：已发布公开；草稿仅登录态或有效预览令牌可读。
func (s *Server) handlePostDetail(c *Context) (Response, error) {
	key := c.Params["slug"]
	detail, err := s.posts.FindByKey(c.Request.Context(), key)
	if err != nil {
		return Response{}, err
	}
	canPreview := c.HasUser || s.auth.VerifyPreview(c.Query.Get("preview"), key)
	if detail == nil || (!detail.Published && !canPreview) {
		return Response{Status: http.StatusNotFound, Body: messageJSON("文章不存在或未发布")}, nil
	}
	return Response{Status: http.StatusOK, Body: *detail}, nil
}

// --- 公开合集 ---

// handleCollectionsList 公开合集列表（含已发布文章数）。
func (s *Server) handleCollectionsList(c *Context) (Response, error) {
	list, err := s.collections.ListPublic(c.Request.Context())
	if err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: collectionsListBody{List: list}}, nil
}

// handleCollectionDetail 公开合集详情。
func (s *Server) handleCollectionDetail(c *Context) (Response, error) {
	detail, err := s.collections.GetPublic(c.Request.Context(), c.Params["slug"])
	if err != nil {
		return Response{}, err
	}
	if detail == nil {
		return Response{Status: http.StatusNotFound, Body: messageJSON("合集不存在")}, nil
	}
	return Response{Status: http.StatusOK, Body: *detail}, nil
}

// --- 管理端文章 ---

// handleAdminPostsList 全部文章（含草稿），按更新时间倒序。
func (s *Server) handleAdminPostsList(c *Context) (Response, error) {
	list, err := s.posts.ListAdmin(c.Request.Context())
	if err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: adminPostsBody{List: list}}, nil
}

// handlePreviewLink 返回文章页地址：已发布给公开地址，草稿附短时效预览令牌。
func (s *Server) handlePreviewLink(c *Context) (Response, error) {
	info, err := s.posts.PreviewInfoByID(c.Request.Context(), leadingInt64(c.Params["id"]))
	if err != nil {
		return Response{}, err
	}
	if info == nil {
		return Response{Status: http.StatusNotFound, Body: messageJSON("文章不存在")}, nil
	}
	key := info.Slug
	if info.PublicID != nil && *info.PublicID != "" {
		key = *info.PublicID
	}
	base := s.publicSiteURL() + "/blog/" + key
	if info.Published {
		return Response{Status: http.StatusOK, Body: previewBody{Published: true, URL: base}}, nil
	}
	token, err := s.auth.SignPreview(key)
	if err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: previewBody{Published: false, URL: base + "?preview=" + url.QueryEscape(token)}}, nil
}

// handlePostCreate 新建文章。
func (s *Server) handlePostCreate(c *Context) (Response, error) {
	title, _ := c.truthyStringField("title")
	slug, _ := c.truthyStringField("slug")
	subtitle, _ := c.truthyStringField("subtitle")
	in := posts.CreateInput{
		Title:      title,
		Slug:       slug,
		Content:    c.stringField("content", ""),
		Excerpt:    c.stringField("excerpt", ""),
		Subtitle:   subtitle,
		Tags:       parseTagsInput(c, false),
		Collection: parseCollectionInput(c),
	}
	if raw, ok := c.field("published"); ok {
		in.Published = coerceTruthy(raw)
	}
	detail, err := s.posts.Create(c.Request.Context(), in)
	if err != nil {
		if errors.Is(err, posts.ErrTitleEmpty) || errors.Is(err, posts.ErrCollectionNotFound) {
			return Response{Status: http.StatusBadRequest, Body: messageJSON(err.Error())}, nil
		}
		return Response{}, err
	}
	if err := tags.Rebuild(c.Request.Context(), s.posts.DB); err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusCreated, Body: *detail}, nil
}

// handlePostUpdate 更新文章。
func (s *Server) handlePostUpdate(c *Context) (Response, error) {
	in := posts.UpdateInput{
		Title:      c.optionalStringField("title"),
		Slug:       optionalPtr(c.truthyStringField("slug")),
		Content:    c.optionalStringField("content"),
		Excerpt:    c.optionalStringField("excerpt"),
		Subtitle:   optionalPtr(c.truthyStringField("subtitle")),
		Tags:       parseTagsInput(c, true),
		Collection: parseCollectionInput(c),
	}
	if raw, ok := c.field("published"); ok {
		published := coerceTruthy(raw)
		in.Published = &published
	}
	detail, err := s.posts.Update(c.Request.Context(), leadingInt64(c.Params["id"]), in)
	if err != nil {
		switch {
		case errors.Is(err, posts.ErrPostNotFound):
			return Response{Status: http.StatusNotFound, Body: messageJSON(err.Error())}, nil
		case errors.Is(err, posts.ErrCollectionNotFound):
			return Response{Status: http.StatusBadRequest, Body: messageJSON(err.Error())}, nil
		default:
			return Response{}, err
		}
	}
	if err := tags.Rebuild(c.Request.Context(), s.posts.DB); err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: *detail}, nil
}

// handlePostDelete 删除文章。
func (s *Server) handlePostDelete(c *Context) (Response, error) {
	ok, err := s.posts.Delete(c.Request.Context(), leadingInt64(c.Params["id"]))
	if err != nil {
		return Response{}, err
	}
	if !ok {
		return Response{Status: http.StatusNotFound, Body: messageJSON("文章不存在")}, nil
	}
	if err := tags.Rebuild(c.Request.Context(), s.posts.DB); err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: okMessageBody{OK: true, Message: "已删除"}}, nil
}

// --- 管理端合集 ---

// handleAdminCollectionsList 全部合集（含草稿文章数）。
func (s *Server) handleAdminCollectionsList(c *Context) (Response, error) {
	list, err := s.collections.ListAdmin(c.Request.Context())
	if err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: collectionsListBody{List: list}}, nil
}

// handleCollectionCreate 新建合集。
func (s *Server) handleCollectionCreate(c *Context) (Response, error) {
	name, _ := c.truthyStringField("name")
	in := collections.Input{
		Name:        &name,
		Slug:        optionalPtr(c.truthyStringField("slug")),
		Description: optionalPtr(c.truthyStringField("description")),
	}
	created, err := s.collections.Create(c.Request.Context(), in)
	if err != nil {
		if errors.Is(err, collections.ErrNameEmpty) {
			return Response{Status: http.StatusBadRequest, Body: messageJSON(err.Error())}, nil
		}
		return Response{}, err
	}
	return Response{Status: http.StatusCreated, Body: *created}, nil
}

// handleCollectionUpdate 更新合集。
func (s *Server) handleCollectionUpdate(c *Context) (Response, error) {
	in := collections.Input{
		Name:        c.optionalStringField("name"),
		Slug:        optionalPtr(c.truthyStringField("slug")),
		Description: c.optionalStringField("description"),
	}
	updated, err := s.collections.Update(c.Request.Context(), leadingInt64(c.Params["id"]), in)
	if err != nil {
		if errors.Is(err, collections.ErrCollectionNotFound) {
			return Response{Status: http.StatusNotFound, Body: messageJSON(err.Error())}, nil
		}
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: *updated}, nil
}

// handleCollectionDelete 删除合集（文章 collection_id 置 NULL）。
func (s *Server) handleCollectionDelete(c *Context) (Response, error) {
	ok, err := s.collections.Delete(c.Request.Context(), leadingInt64(c.Params["id"]))
	if err != nil {
		return Response{}, err
	}
	if !ok {
		return Response{Status: http.StatusNotFound, Body: messageJSON("合集不存在")}, nil
	}
	return Response{Status: http.StatusOK, Body: okMessageBody{OK: true, Message: "已删除"}}, nil
}

// --- 图片上传 ---

// uploadTypes 是允许上传的图片类型（同时作为回源 Content-Type 白名单）。
var uploadTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".svg":  "image/svg+xml",
}

// uploadLimit 是单文件大小上限（10MB，旧实现 multer limits.fileSize）。
const uploadLimit = 10 * 1024 * 1024

// handleUploadPost 接收图片（字段名 image），返回公开访问地址。
func (s *Server) handleUploadPost(c *Context) (Response, error) {
	r := c.Request
	r.Body = http.MaxBytesReader(c.Writer, r.Body, uploadLimit+1<<20)
	if err := r.ParseMultipartForm(uploadLimit + 1<<20); err != nil {
		return Response{Status: http.StatusBadRequest, Body: messageJSON("上传失败（文件过大或不是有效的图片表单）")}, nil
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		return Response{Status: http.StatusBadRequest, Body: messageJSON("未收到图片（字段名 image）")}, nil
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if _, ok := uploadTypes[ext]; !ok {
		return Response{Status: http.StatusBadRequest, Body: messageJSON(unsupportedExtMessage(ext))}, nil
	}
	if header.Size > uploadLimit {
		return Response{Status: http.StatusBadRequest, Body: messageJSON("上传失败（文件过大，上限 10MB）")}, nil
	}

	// 文件名格式对齐旧实现：<毫秒>-<0..1e6 随机数><扩展名>（扩展名截断到 10 字符）。
	nameExt := ext
	if len(nameExt) > 10 {
		nameExt = nameExt[:10]
	}
	name := itoa64(time.Now().UnixMilli()) + "-" + itoa(rand.Intn(1000001)) + nameExt
	dest := filepath.Join(s.uploadDir, name)
	out, err := os.Create(dest)
	if err != nil {
		return Response{}, err
	}
	if _, err := copyLimited(out, file, uploadLimit); err != nil {
		out.Close()
		_ = os.Remove(dest)
		return Response{}, err
	}
	if err := out.Close(); err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusOK, Body: uploadBody{URL: "/api/blog/uploads/" + name}}, nil
}

// handleUploadGet 返回上传目录里的图片（公开）。
func (s *Server) handleUploadGet(c *Context) (Response, error) {
	name := filepath.Base(c.Params["name"])
	if name == "" || name == "." || strings.Contains(name, "..") {
		return Response{Status: http.StatusBadRequest, Body: messageJSON("非法文件名")}, nil
	}
	contentType, ok := uploadTypes[strings.ToLower(filepath.Ext(name))]
	if !ok {
		return Response{Status: http.StatusNotFound, Body: messageJSON("图片不存在")}, nil
	}
	content, err := os.ReadFile(filepath.Join(s.uploadDir, name))
	if err != nil {
		return Response{Status: http.StatusNotFound, Body: messageJSON("图片不存在")}, nil
	}
	return Response{
		Status:      http.StatusOK,
		ContentType: contentType,
		Headers: map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; sandbox",
			"Content-Length":          itoa(len(content)),
		},
		Body: rawBody(content),
	}, nil
}

// publicSiteURL 返回公共站点基址；未配置时告警一次并返回空串（相对路径）。
func (s *Server) publicSiteURL() string {
	if s.cfg.PublicSiteURL == "" {
		s.siteURLWarned.Do(func() {
			s.log.Warn("PUBLIC_SITE_URL 未配置，preview-link 将返回相对地址（后台在子域上会打不开）")
		})
	}
	return s.cfg.PublicSiteURL
}

// unsupportedExtMessage 生成「不支持的文件类型」文案（对齐旧实现）。
func unsupportedExtMessage(ext string) string {
	suffix := ""
	if ext != "" {
		suffix = " " + ext
	}
	return "不支持的文件类型" + suffix + "，仅限 .png / .jpg / .jpeg / .gif / .webp / .avif / .svg"
}

// rawBody 包装原始字节，使 write 原样输出（不经过 JSON 编码）。
type rawBody []byte

// copyLimited 从 src 拷贝至 dst，最多 limit 字节；超过上限返回 errTooLarge（不写超出部分）。
func copyLimited(dst *os.File, src io.Reader, limit int64) (int64, error) {
	written, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if written > limit {
		return written, errTooLarge
	}
	return written, err
}

// errTooLarge 表示上传文件超过上限。
var errTooLarge = errors.New("文件过大")

// itoa64 是 int64 十进制转换。
func itoa64(n int64) string { return itoa(int(n)) }
