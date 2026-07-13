// 📌 影响范围：创建临时 WebUI 静态目录并通过内存 HTTP 请求验证响应。
package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandlerServesAssetsAndHistoryFallback 验证静态资源与Vue路由回退。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：创建并自动清理临时文件。
func TestHandlerServesAssetsAndHistoryFallback(t *testing.T) {
	directory := prepareWebUI(t)
	handler, err := New(directory)
	// [决策理由] 合法测试目录必须成功创建处理器。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	// [决策理由] 真实资源必须保持其内容，不能回退入口页。
	if asset.Code != http.StatusOK || asset.Body.String() != "app" {
		t.Fatalf("asset status/body = %d/%q", asset.Code, asset.Body.String())
	}
	route := httptest.NewRecorder()
	handler.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/permissions", nil))
	// [决策理由] History路由必须返回入口页供Vue Router接管。
	if route.Code != http.StatusOK || !strings.Contains(route.Body.String(), "webui") {
		t.Fatalf("route status/body = %d/%q", route.Code, route.Body.String())
	}
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/permissions", nil))
	// [决策理由] HEAD路由应返回入口元数据但不写响应体。
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status/body length = %d/%d", head.Code, head.Body.Len())
	}

	// >>> 数据演变示例
	// 1. /assets/app.js -> 文件存在 -> 200 app。
	// 2. /permissions -> 文件不存在且无扩展名 -> 200 index.html。
}

// TestHandlerRejectsMissingAssetsAndWritesSecurityHeaders 验证资源404、方法限制和安全头。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：创建并自动清理临时文件。
func TestHandlerRejectsMissingAssetsAndWritesSecurityHeaders(t *testing.T) {
	handler, err := New(prepareWebUI(t))
	// [决策理由] 合法测试目录必须成功创建处理器。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	// [决策理由] 缺失资源应明确404，不能返回入口HTML。
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", missing.Code)
	}
	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/plugins", nil))
	// [决策理由] 非读取方法必须拒绝，且所有响应都应携带安全头。
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Content-Security-Policy") == "" || method.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("method status/CSP/Allow = %d/%q/%q", method.Code, method.Header().Get("Content-Security-Policy"), method.Header().Get("Allow"))
	}
	// [决策理由] Naive UI依赖CSS-in-JS，CSP必须允许内联样式但不能放开内联脚本。
	if !strings.Contains(method.Header().Get("Content-Security-Policy"), "style-src 'self' 'unsafe-inline'") || strings.Contains(method.Header().Get("Content-Security-Policy"), "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("unexpected CSP = %q", method.Header().Get("Content-Security-Policy"))
	}

	// >>> 数据演变示例
	// 1. /assets/missing.js -> 扩展名缺失资源 -> 404。
	// 2. POST /plugins -> 非读取方法 -> 405+CSP。
}

// TestNewRejectsMissingIndex 验证不完整构建目录无法启动处理器。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：创建并自动清理临时目录。
func TestNewRejectsMissingIndex(t *testing.T) {
	_, err := New(t.TempDir())
	// [决策理由] 缺失入口页时必须返回构建错误。
	if err == nil {
		t.Fatal("New() error = nil")
	}

	// >>> 数据演变示例
	// 1. 空目录 -> index Stat失败 -> error。
	// 2. 完整目录由其他测试覆盖 -> Handler,nil。
}

// prepareWebUI 创建最小WebUI构建目录。
// @param t：Go测试上下文。
// @returns 包含入口页和脚本资源的临时目录。
// ⚠️副作用说明：创建临时目录及两个文件。
func prepareWebUI(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	// [决策理由] assets目录必须先创建才能写入脚本文件。
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	// [决策理由] 入口页写入失败时测试夹具不可信。
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<html>webui</html>"), 0o600); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}
	// [决策理由] 脚本写入失败时无法验证真实资源分支。
	if err := os.WriteFile(filepath.Join(directory, "assets", "app.js"), []byte("app"), 0o600); err != nil {
		t.Fatalf("WriteFile(asset) error = %v", err)
	}

	// >>> 数据演变示例
	// 1. TempDir -> assets+index+app.js -> 返回目录。
	// 2. 任一步失败 -> Fatal终止测试。
	return directory
}
