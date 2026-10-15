// Package bloggo 承载嵌入式前端产物，使二进制自带博客站点。
package bloggo

import (
	"embed"
	"io/fs"
)

// distFS 嵌入站点构建产物；all: 前缀保证点文件与下划线开头的文件一并打包。
//
//go:embed all:web/dist
var distFS embed.FS

// DistFS 返回定位到 dist 根的文件系统；定位失败时退回原始 FS（调用方按不存在处理）。
func DistFS() fs.FS {
	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		return distFS
	}
	return sub
}
