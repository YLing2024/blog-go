// Package auth 复刻旧实现 auth.ts 的认证语义：AUTH_MODE 双模式、HS256 会话 JWT、
// Redis admin 会话第二通道（多前缀 + 滑动续期 + 降级）、草稿预览令牌与请求 cookie。
//
// builtin：Bearer / admin_session cookie → JWT 或 Redis 会话，二者有一即可；X-Auth-User 被忽略。
// sso：只认网关注入的 X-Auth-User（缺失/空 → 401），不解析 cookie/JWT。
// Redis 不可用时退回纯 JWT（此时登出无法立即失效，属与旧实现相同的已知取舍）。
package auth

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"blog-go/internal/bcrypt"
	"blog-go/internal/config"
	"blog-go/internal/jwt"
	"blog-go/internal/store"
)

// 会话与预览口径（对齐旧实现 auth.ts）。
const (
	// SessionCookieName 是 builtin 模式登录后下发的会话 cookie 名。
	SessionCookieName = "admin_session"
	// SessionTTL 是 Redis admin 会话有效期（12 小时，校验通过滑动续期）。
	SessionTTL = 12 * time.Hour
	// SessionJWTttl 是会话 JWT 有效期（旧实现 EXPIRES_IN='7d'）。
	SessionJWTttl = 7 * 24 * time.Hour

	// MsgNotLoggedIn / MsgExpired 是鉴权失败的两条对外文案（旧实现逐字一致）。
	MsgNotLoggedIn = "未登录，请先登录"
	MsgExpired     = "登录已过期，请重新登录"
)

// User 是通过鉴权后的身份。
type User struct {
	ID       int64
	Username string
	Role     string
	Via      string
}

// sessionState 是 Redis 会话查询结论。
type sessionState int

const (
	stateMissing sessionState = iota
	stateValid
	stateUnavailable
)

// Authenticator 持有认证所需的密钥、模式与 Redis 客户端。
type Authenticator struct {
	mode       config.AuthMode
	secret     []byte
	db         *store.DB
	redis      *redis.Client
	prefixes   []string
	previewTTL time.Duration
}

// New 构造认证器。Redis URL 非法时客户端为空，所有会话查询按「不可用」处理（降级为纯 JWT）。
func New(cfg *config.Config, db *store.DB) *Authenticator {
	a := &Authenticator{
		mode:       cfg.AuthMode,
		secret:     []byte(cfg.JWTSecret),
		db:         db,
		prefixes:   splitPrefixes(cfg.AdminRedisPrefix),
		previewTTL: cfg.PreviewTTL,
	}
	if opt, err := redis.ParseURL(cfg.AdminRedisURL); err == nil {
		opt.MaxRetries = 1
		opt.DialTimeout = 2 * time.Second
		opt.ReadTimeout = 2 * time.Second
		opt.WriteTimeout = 2 * time.Second
		opt.PoolTimeout = 2 * time.Second
		a.redis = redis.NewClient(opt)
	}
	return a
}

// Close 释放 Redis 连接。
func (a *Authenticator) Close() error {
	if a.redis == nil {
		return nil
	}
	return a.redis.Close()
}

// Mode 返回认证模式。
func (a *Authenticator) Mode() config.AuthMode { return a.mode }

// ModeLabel 返回启动日志用的模式文案（旧实现 authModeLabel）。
func (a *Authenticator) ModeLabel() string {
	if a.mode == config.AuthSSO {
		return "SSO（信任 X-Auth-User）"
	}
	return "自带账号（builtin）"
}

// VerifyCredentials 校验用户名 + 口令（bcrypt），成功返回身份；用户不存在或口令不符返回 (false,nil)。
func (a *Authenticator) VerifyCredentials(ctx context.Context, username, password string) (User, bool, error) {
	if a.db == nil {
		return User{}, false, nil
	}
	var (
		id   int64
		name string
		hash string
	)
	err := a.db.SQL.QueryRowContext(ctx,
		"SELECT id, username, password FROM users WHERE username = ?", username).Scan(&id, &name, &hash)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	if !bcrypt.Compare(password, hash) {
		return User{}, false, nil
	}
	return User{ID: id, Username: name, Role: "admin"}, true, nil
}

// SignSession 签发 7 天会话 JWT。
func (a *Authenticator) SignSession(id int64, username string) (string, error) {
	return jwt.Sign(map[string]any{"id": id, "username": username}, a.secret, SessionJWTttl)
}

// SignPreview 签发绑定 slug 的短时效预览令牌。
func (a *Authenticator) SignPreview(slug string) (string, error) {
	return jwt.Sign(map[string]any{"preview": slug}, a.secret, a.previewTTL)
}

// VerifyPreview 校验预览令牌签名、时效且绑定到指定 slug。
func (a *Authenticator) VerifyPreview(token, slug string) bool {
	if token == "" {
		return false
	}
	claims, err := jwt.Verify(token, a.secret)
	if err != nil {
		return false
	}
	value, ok := claims["preview"].(string)
	return ok && value == slug
}

// SetSession 写入 Redis admin 会话（登录用）；失败静默（退回纯 JWT）。
func (a *Authenticator) SetSession(ctx context.Context, token string) bool {
	if token == "" || a.redis == nil || len(a.prefixes) == 0 {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return a.redis.Set(cctx, a.prefixes[0]+token, token, SessionTTL).Err() == nil
}

// DestroySession 删除各前缀下的会话 key（登出用，幂等）。
func (a *Authenticator) DestroySession(ctx context.Context, token string) {
	if token == "" || a.redis == nil {
		return
	}
	for _, prefix := range a.prefixes {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = a.redis.Del(cctx, prefix+token).Err()
		cancel()
	}
}

// Require 执行硬鉴权：成功返回身份；失败返回对外文案。
func (a *Authenticator) Require(ctx context.Context, r *http.Request) (User, bool, string) {
	if a.mode == config.AuthSSO {
		if name := strings.TrimSpace(r.Header.Get("X-Auth-User")); name != "" {
			return User{Username: name, Via: "gateway"}, true, ""
		}
		return User{}, false, MsgNotLoggedIn
	}
	token := a.BuiltinToken(r)
	if token == "" {
		return User{}, false, MsgNotLoggedIn
	}
	user, ok := a.verifyBuiltin(ctx, token)
	if !ok {
		return User{}, false, MsgExpired
	}
	return user, true, ""
}

// Optional 执行软鉴权：识别到身份就返回，否则放行（不报错）。
func (a *Authenticator) Optional(ctx context.Context, r *http.Request) (User, bool) {
	if a.mode == config.AuthSSO {
		if name := strings.TrimSpace(r.Header.Get("X-Auth-User")); name != "" {
			return User{Username: name, Via: "gateway"}, true
		}
		return User{}, false
	}
	token := a.BuiltinToken(r)
	if token == "" {
		return User{}, false
	}
	return a.verifyBuiltin(ctx, token)
}

// BuiltinToken 取 builtin 凭证：Bearer 优先，其次 admin_session cookie。
func (a *Authenticator) BuiltinToken(r *http.Request) string {
	if bearer := bearerToken(r); bearer != "" {
		return bearer
	}
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// verifyBuiltin 校验 builtin 凭证：JWT 与 Redis 会话任一有效即通过，并复刻降级取舍。
func (a *Authenticator) verifyBuiltin(ctx context.Context, token string) (User, bool) {
	jwtUser, jwtOK := a.verifySessionJWT(token)
	state := a.redisSessionState(ctx, token)
	switch {
	case state == stateValid:
		if jwtOK {
			return User{ID: jwtUser.id, Username: jwtUser.username, Via: "jwt+session"}, true
		}
		return User{Role: "admin", Via: "admin-session"}, true
	case state == stateUnavailable && jwtOK:
		return User{ID: jwtUser.id, Username: jwtUser.username, Via: "jwt-redis-down"}, true
	default:
		return User{}, false
	}
}

// sessionClaims 是会话 JWT 里我们关心的声明。
type sessionClaims struct {
	id       int64
	username string
}

// verifySessionJWT 校验会话 JWT；成功返回声明。
func (a *Authenticator) verifySessionJWT(token string) (sessionClaims, bool) {
	claims, err := jwt.Verify(token, a.secret)
	if err != nil {
		return sessionClaims{}, false
	}
	var out sessionClaims
	if v, ok := claims["id"]; ok {
		out.id = toInt64(v)
	}
	if v, ok := claims["username"].(string); ok {
		out.username = v
	}
	return out, true
}

// redisSessionState 查询会话状态，并做滑动续期。区分「不可用」与「没有」：
// 只按 JWT 有效放行会让登出形同虚设。
func (a *Authenticator) redisSessionState(ctx context.Context, token string) sessionState {
	if token == "" {
		return stateMissing
	}
	if a.redis == nil {
		return stateUnavailable
	}
	sawError := false
	for _, prefix := range a.prefixes {
		key := prefix + token
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		value, err := a.redis.Get(cctx, key).Result()
		cancel()
		switch {
		case err == nil:
			if value == token {
				rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
				_ = a.redis.Expire(rctx, key, SessionTTL).Err()
				rcancel()
				return stateValid
			}
		case err == redis.Nil:
			// 该前缀下不存在，继续看下一个。
		default:
			sawError = true
		}
	}
	if sawError {
		return stateUnavailable
	}
	return stateMissing
}

// SessionCookie 构造下发用会话 cookie（Path=/; HttpOnly; SameSite=Lax; HTTPS 加 Secure）。
// 同时带 Max-Age 与 Expires，与 Express `res.cookie(..., {maxAge})` 发出的头一致。
func (a *Authenticator) SessionCookie(r *http.Request, token string) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   int(SessionTTL / time.Second),
		Expires:  time.Now().Add(SessionTTL),
	}
}

// ClearSessionCookie 构造清除用 cookie：仅以 Expires（1970-01-01）表达删除，
// 复刻 Express `res.clearCookie`（不写 Max-Age）发出的头。
func (a *Authenticator) ClearSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		Expires:  time.Unix(0, 0).UTC(),
	}
}

// bearerToken 取 `Authorization: Bearer <token>`。
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

// isSecureRequest 判定请求是否 HTTPS（含反代 X-Forwarded-Proto），决定 cookie 的 Secure。
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return proto == "https"
}

// splitPrefixes 按逗号切分多前缀（正式 + 测试），去掉空白与空项。
func splitPrefixes(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// toInt64 把 JSON 解出的数值声明转成 int64。
func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
