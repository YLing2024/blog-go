package store

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// nowMillis 是 Slugify 回退分支的时间源；抽成变量便于测试注入。
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// 说明：正则逐字复刻旧实现 routes/posts.ts 的 slugify（Go 用 \x{...} 写 Unicode 码点边界，
// RE2 不支持 JS 风格的 \uXXXX）。
var (
	slugSpaces  = regexp.MustCompile(`\s+`)
	slugIllegal = regexp.MustCompile(`[^a-z0-9\x{4e00}-\x{9fa5}-]`)
)

// Slugify 生成 slug：转小写、空白转短横线、过滤非法字符、去首尾短横线；
// 结果为空时退回 <fallback>-<当前毫秒>（与旧实现一致，中文标题也能得到可用别名）。
func Slugify(text, fallback string) string {
	s := strings.ToLower(strings.TrimSpace(text))
	s = slugSpaces.ReplaceAllString(s, "-")
	s = slugIllegal.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s != "" {
		return s
	}
	return fallback + "-" + strconv.FormatInt(nowMillis(), 10)
}

// EscapeLike 转义 LIKE 通配符（\ % _），避免标签名被当成通配符匹配。
func EscapeLike(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '%' || c == '_' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}
