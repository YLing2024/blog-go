package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// staticMime 是嵌入站点产物的内容类型表，字段取自常见静态产物的扩展名。
// 站点由本进程直接托管（旧实现由 nginx 承担），这里覆盖 nginx + Vite 产物的实际种类。
var staticMime = map[string]string{
	".html":        "text/html; charset=UTF-8",
	".js":          "application/javascript; charset=UTF-8",
	".mjs":         "application/javascript; charset=UTF-8",
	".css":         "text/css; charset=UTF-8",
	".json":        "application/json; charset=UTF-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".ico":         "image/vnd.microsoft.icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".txt":         "text/plain; charset=UTF-8",
	".webmanifest": "application/manifest+json",
	".map":         "application/json; charset=UTF-8",
	".xml":         "application/xml; charset=UTF-8",
}

// staticContentType 返回静态文件内容类型；未知扩展名回落 application/octet-stream。
func staticContentType(name string) string {
	if value, ok := staticMime[strings.ToLower(path.Ext(name))]; ok {
		return value
	}
	return "application/octet-stream"
}

// serveStatic 托管嵌入的站点产物 + SPA 回退（仅 GET/HEAD）。
// 缓存口径对齐现网 nginx 配置：/assets/ 一年不可变；sw.js 走 must-revalidate；
// 其余（html / 图标 / hljs / manifest / 回退页）一律 no-cache，保证部署即更新。
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, head bool) {
	if name, ok := resolveStaticName(r.URL.Path); ok {
		if s.writeStaticFile(w, head, name, false) {
			return
		}
	}
	// try_files 的最后一项：未命中任何文件时回退站点骨架。
	if s.writeStaticFile(w, head, "index.html", true) {
		return
	}
	s.write(w, head, Response{Status: http.StatusNotFound, ContentType: contentTypeHTML, Body: rawBody([]byte("Not Found"))})
}

// resolveStaticName 把 URL 路径映射为产物内的相对文件名；目录路径（含尾斜杠）补 index.html。
func resolveStaticName(urlPath string) (string, bool) {
	if urlPath == "" {
		urlPath = "/"
	}
	clean := path.Clean(urlPath)
	if clean == "." {
		clean = "/"
	}
	trimmed := strings.TrimPrefix(clean, "/")
	// path.Clean 不会产生越界的 ".."，这里再防御性拒绝一次。
	if strings.HasPrefix(trimmed, "..") {
		return "", false
	}
	if trimmed == "" {
		return "index.html", true
	}
	if strings.HasSuffix(urlPath, "/") {
		return path.Join(trimmed, "index.html"), true
	}
	return trimmed, true
}

// writeStaticFile 写出静态文件；文件不存在返回 false（交回上层做 SPA 回退）。
func (s *Server) writeStaticFile(w http.ResponseWriter, head bool, name string, spaFallback bool) bool {
	info, err := fs.Stat(s.static, name)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	content, err := fs.ReadFile(s.static, name)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", staticContentType(name))
	w.Header().Set("Cache-Control", staticCacheControl(name, spaFallback))
	w.Header().Set("Accept-Ranges", "bytes")
	if head {
		w.Header().Set("Content-Length", itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		return true
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
	return true
}

// staticCacheControl 复刻现网 nginx 的缓存头：/assets/ 长缓存，sw.js 每次校验，
// 其余（含 html、回退页）no-cache。
func staticCacheControl(name string, spaFallback bool) string {
	if spaFallback {
		return "no-cache"
	}
	if strings.HasPrefix(name, "assets/") {
		return "public, max-age=31536000, immutable"
	}
	if name == "sw.js" {
		return "no-cache, must-revalidate"
	}
	return "no-cache"
}
