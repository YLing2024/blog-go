package httpapi

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"blog-go/internal/posts"
)

// field 取请求体字段原始 JSON；ok 为假表示字段未出现。
func (c *Context) field(key string) (json.RawMessage, bool) {
	raw, ok := c.Body[key]
	return raw, ok
}

// optionalPtr 把 (value, present) 收敛为 *string：present=false 时为 nil（表示保持原值）。
func optionalPtr(value string, present bool) *string {
	if !present {
		return nil
	}
	return &value
}

// rawIsNull 判断字段是否为 JSON null。
func rawIsNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// stringField 取字符串字段：字段缺失/null 用默认值，否则按 JS String() 语义转换。
func (c *Context) stringField(key, def string) string {
	raw, ok := c.field(key)
	if !ok || rawIsNull(raw) {
		return def
	}
	return coerceString(raw)
}

// optionalStringField 取可空字符串字段：缺失/null 返回 nil（表示「不改动」），否则返回转换值。
func (c *Context) optionalStringField(key string) *string {
	raw, ok := c.field(key)
	if !ok || rawIsNull(raw) {
		return nil
	}
	value := coerceString(raw)
	return &value
}

// truthyStringField 复刻旧实现 JS 的「取值或空串」语义：字段缺失返回 present=false，
// 出现但为 JS 假值（null/0/""/false）时 value 为空串，否则为 String(value)。
// 用于「null 会清空/归一为空串」的字段（subtitle、slug、name）。
func (c *Context) truthyStringField(key string) (value string, present bool) {
	raw, ok := c.field(key)
	if !ok {
		return "", false
	}
	if !coerceTruthy(raw) {
		return "", true
	}
	return coerceString(raw), true
}

// coerceString 复刻 JS String(value) 的常见形态（字符串/数字/布尔/null/数组/对象）。
func coerceString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	case 'n':
		return "null"
	case 't':
		return "true"
	case 'f':
		return "false"
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil {
			parts := make([]string, len(arr))
			for i, e := range arr {
				parts[i] = coerceString(e)
			}
			return strings.Join(parts, ",")
		}
		return ""
	case '{':
		return "[object Object]"
	default:
		return trimmed
	}
}

// coerceTruthy 复刻 JS 真值判定（null/0/""/false 为假，其余为真）。
func coerceTruthy(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return false
	}
	switch trimmed[0] {
	case '"':
		return trimmed != `""`
	case 'n':
		return false
	case 't':
		return true
	case 'f':
		return false
	case '[', '{':
		return true
	default:
		return trimmed != "0" && trimmed != "-0"
	}
}

// parseTagsInput 解析 tags 字段。fallbackOnNull 为真时，null 视为「未提供」（更新语义）。
func parseTagsInput(c *Context, fallbackOnNull bool) posts.TagsInput {
	raw, ok := c.field("tags")
	if !ok {
		return posts.TagsInput{}
	}
	if rawIsNull(raw) {
		if fallbackOnNull {
			return posts.TagsInput{}
		}
		return posts.TagsInput{Present: true, Scalar: "null"}
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil {
			elems := make([]string, len(arr))
			for i, e := range arr {
				elems[i] = joinElement(e)
			}
			return posts.TagsInput{Present: true, IsArray: true, Array: elems}
		}
	}
	return posts.TagsInput{Present: true, Scalar: coerceString(raw)}
}

// parseCollectionInput 解析 collection_id：缺失 → 未提供；null/"" → 清空；其余取候选整数。
func parseCollectionInput(c *Context) posts.CollectionInput {
	raw, ok := c.field("collection_id")
	if !ok {
		return posts.CollectionInput{}
	}
	if rawIsNull(raw) {
		return posts.CollectionInput{Present: true, Clear: true}
	}
	value := coerceString(raw)
	if strings.TrimSpace(value) == "" {
		return posts.CollectionInput{Present: true, Clear: true}
	}
	return posts.CollectionInput{Present: true, Value: numberToInt(value)}
}

// numberToInt 复刻 JS Number() 后「必须是正整数整数」的判定；非法一律 0。
func numberToInt(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f) {
		return int64(f)
	}
	return 0
}

// joinElement 复刻 JS `Array.prototype.join` 对单个元素的字符串化：null 变空串，其余走 String()。
// tags 为数组时旧实现直接 `arr.join(',')`，与逐元素 String() 在 null 上不同，故单列一条。
func joinElement(raw json.RawMessage) string {
	if rawIsNull(raw) {
		return ""
	}
	return coerceString(raw)
}

// leadingInt64 把路径里的 :id 按 JS `Number()` 语义转成整数：整体是有限整数才取整，否则 0。
// 旧实现把 `Number(param)` 绑定进 `WHERE id = ?`，非整数（如 "3abc"）不会命中任何行；
// 回 0 与 404 等价（id 自增从 1 起）。
func leadingInt64(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0
	}
	return int64(f)
}
