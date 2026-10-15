package posts

import (
	"errors"
	"strconv"
	"strings"
)

// 领域错误：文案即对外响应体里的 message（由 httpapi 映射状态码）。
var (
	// ErrTitleEmpty 新建文章标题为空。
	ErrTitleEmpty = errors.New("标题不能为空")
	// ErrCollectionNotFound 指定的合集不存在或非法。
	ErrCollectionNotFound = errors.New("合集不存在")
	// ErrPostNotFound 管理接口按 id 操作的文章不存在。
	ErrPostNotFound = errors.New("文章不存在")
)

// subtitleMax 是副标题上限（旧实现 SUBTITLE_MAX）。
const subtitleMax = 200

// NormalizeSubtitle 归一化副标题：去首尾空白，超过 200 字符截断。
// 与旧实现一致按 UTF-16 代码单元切分（BMP 内与按码点切分等价）。
func NormalizeSubtitle(value string) string {
	value = strings.TrimSpace(value)
	return sliceUTF16(value, subtitleMax)
}

// sliceUTF16 按 UTF-16 代码单元切分，复刻 JS String.prototype.slice 的长度口径。
func sliceUTF16(s string, max int) string {
	units := 0
	for i, r := range s {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > max {
			return s[:i]
		}
		units += width
	}
	return s
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ParsePage 复刻 `Math.max(1, parseInt(page,10) || 1)`：只取前导整数，非法/非正一律回 1。
func ParsePage(raw string) int {
	n := parseIntPrefix(raw)
	if n < 1 {
		return 1
	}
	return n
}

// parseIntPrefix 解析字符串前导的十进制整数（可带正负号）；无数字返回 0。
func parseIntPrefix(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	neg := false
	if s[0] == '-' || s[0] == '+' {
		neg = s[0] == '-'
		i = 1
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0
	}
	n, err := strconv.Atoi(s[i:j])
	if err != nil {
		return 0
	}
	if neg {
		return -n
	}
	return n
}
