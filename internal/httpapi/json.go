package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// maxBodyBytes 是 JSON 请求体上限（对齐 express.json() 默认的 100kb）。
const maxBodyBytes = 100 * 1024

// messageBody 是统一错误体 `{message}`（字段顺序与旧实现一致）。
type messageBody struct {
	Message string `json:"message"`
}

// messageJSON 构造 `{message:<msg>}` 响应体。
func messageJSON(msg string) messageBody { return messageBody{Message: msg} }

// readJSONBody 解析 JSON 顶层对象字段；非 JSON 内容类型/空体返回空对象，
// 解析失败返回错误（由调用方映射为 500，对齐旧实现的统一错误处理）。
func readJSONBody(r io.Reader) (map[string]json.RawMessage, error) {
	empty := map[string]json.RawMessage{}
	data, err := io.ReadAll(io.LimitReader(r, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, errBodyTooLarge
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return empty, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		if obj == nil {
			return empty, nil
		}
		return obj, nil
	}
	// 顶层数组：Express strict 模式接受，但字段访问一律 undefined；据此回落空对象。
	var anyVal any
	if err := json.Unmarshal(data, &anyVal); err == nil {
		if _, isArray := anyVal.([]any); isArray {
			return empty, nil
		}
	}
	return nil, errBodyParse
}

// isJSONContentType 复刻 body-parser 的类型判定：媒介类型为 application/json 或以 +json 结尾。
func isJSONContentType(header string) bool {
	if header == "" {
		return false
	}
	media := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	if media == "application/json" {
		return true
	}
	return strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")
}

// bodyError 是请求体解析错误。
type bodyError string

func (e bodyError) Error() string { return string(e) }

var (
	errBodyRead     = bodyError("读取请求体失败")
	errBodyTooLarge = bodyError("request entity too large")
	errBodyParse    = bodyError("请求体不是合法 JSON")
)

// marshalNoEscape 与 JSON.stringify 对齐：不转义 < > & 与 U+2028/U+2029。
func marshalNoEscape(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("null")
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}
