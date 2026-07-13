// 📌 影响范围：读取构造参数指定的 WebUI 静态目录；写入 HTTP 响应安全头和静态文件内容。
package webui

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// handler 为静态资源提供服务，并为无扩展名的前端路由回退 index.html。
type handler struct {
	files  fs.FS
	server http.Handler
}

// New 创建生产 WebUI 静态文件处理器。
// @param directory：包含 index.html 与 assets 的构建产物目录。
// @returns 可挂载到根路径的 HTTP Handler，或入口文件缺失错误。
// ⚠️副作用说明：读取一次 index.html 文件元数据。
func New(directory string) (http.Handler, error) {
	files := os.DirFS(directory)
	_, err := fs.Stat(files, "index.html")
	// [决策理由] 没有入口文件时所有前端路由都会失败，应在启动阶段暴露镜像构建问题。
	if err != nil {
		return nil, fmt.Errorf("读取 WebUI 入口文件: %w", err)
	}
	result := &handler{files: files, server: http.FileServerFS(files)}

	// >>> 数据演变示例
	// 1. web/dist含index.html -> DirFS -> 静态Handler,nil。
	// 2. 目录缺失 -> Stat失败 -> nil,error。
	return result, nil
}

// ServeHTTP 返回静态文件或为 Vue History 路由回退入口页。
// @param writer：HTTP响应写入器；request：静态资源或页面路由请求。
// @returns 无。
// ⚠️副作用说明：读取静态文件并写入HTTP状态、响应头和内容。
func (h *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'self'; frame-ancestors 'none'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	// [决策理由] 静态站点只支持安全读取方法，其他方法不得被误当成前端路由。
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	requested := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	// [决策理由] 根路径应直接交给文件服务，以使用标准 index.html 与缓存协商行为。
	if requested == "." || requested == "" {
		h.server.ServeHTTP(writer, request)
		return
	}
	info, err := fs.Stat(h.files, requested)
	// [决策理由] 真实普通文件应直接返回，避免资源路径被错误回退为 HTML。
	if err == nil && !info.IsDir() {
		h.server.ServeHTTP(writer, request)
		return
	}
	// [决策理由] 权限或I/O异常不是前端路由未命中，不能用入口页掩盖服务器故障。
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		http.Error(writer, "读取 WebUI 静态资源失败", http.StatusInternalServerError)
		return
	}
	// [决策理由] 带扩展名的缺失路径属于静态资源错误，不应返回 index.html 掩盖部署问题。
	if path.Ext(requested) != "" {
		http.NotFound(writer, request)
		return
	}
	clone := request.Clone(request.Context())
	urlCopy := *request.URL
	urlCopy.Path = "/"
	clone.URL = &urlCopy
	h.server.ServeHTTP(writer, clone)

	// >>> 数据演变示例
	// 1. GET /assets/app.js -> 文件存在 -> 返回JavaScript。
	// 2. GET /permissions -> 无扩展名且文件不存在 -> /index.html -> Vue接管路由。
}
