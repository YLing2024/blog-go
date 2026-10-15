// Package jwt 是最小 HS256 JWT 实现，仅用标准库，兼容旧实现所用 jsonwebtoken 的编码。
//
// 旧实现用 jsonwebtoken 签发两类令牌：管理端会话（7 天）与草稿预览（PREVIEW_TTL）。
// 二者都只需 HS256 与 exp 校验，故这里不引 JWT 库，手写签发/校验，减少依赖面。
package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidToken 表示令牌格式、签名或时效不合法。
var ErrInvalidToken = errors.New("JWT 不合法或已过期")

// Sign 用 HS256 签发令牌；claims 会补上 iat / exp（秒级），ttl<=0 时立即过期。
func Sign(claims map[string]any, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	body := make(map[string]any, len(claims)+2)
	for k, v := range claims {
		body[k] = v
	}
	body["iat"] = now.Unix()
	body["exp"] = now.Add(ttl).Unix()

	header := base64URL([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadJSON, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	payload := base64URL(payloadJSON)
	signingInput := header + "." + payload
	sig := sign(signingInput, secret)
	return signingInput + "." + sig, nil
}

// Verify 校验签名与 exp，返回 payload 声明。
func Verify(token string, secret []byte) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil || header.Alg != "HS256" {
		return nil, ErrInvalidToken
	}
	expected := sign(parts[0]+"."+parts[1], secret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return nil, ErrInvalidToken
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	now := time.Now().Unix()
	if exp, ok := numericClaim(claims["exp"]); ok && now >= exp {
		return nil, ErrInvalidToken
	}
	if nbf, ok := numericClaim(claims["nbf"]); ok && now < nbf {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// sign 计算 HS256 签名并做 base64url 编码。
func sign(input string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// base64URL 做无填充的 base64url 编码。
func base64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// numericClaim 把 JSON 解出的数值声明（float64/json.Number）转成整数秒。
func numericClaim(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}
