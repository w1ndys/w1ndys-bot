// 📌 影响范围：无；仅验证 PostgreSQL 连接 URL，不建立网络连接。
package db

import (
	"net/url"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/config"
)

// TestURLForcesUTCSession 验证所有 PostgreSQL 连接固定使用 UTC 会话时区。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestURLForcesUTCSession(t *testing.T) {
	dsn := URL(config.Database{Host: "postgres", Port: 5432, User: "bot", Password: "secret", Name: "w1ndys_bot", SSLMode: "disable"})
	parsed, err := url.Parse(dsn)
	// [决策理由] 连接 URL 必须保持合法，才能检查其会话参数。
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	// [决策理由] timezone=UTC 是数据库读写时间统一的显式保障。
	if parsed.Query().Get("timezone") != "UTC" {
		t.Fatalf("timezone = %q, want UTC", parsed.Query().Get("timezone"))
	}

	// >>> 数据演变示例
	// 1. Database配置 -> URL -> timezone=UTC。
	// 2. sslmode=disable -> URL同时保留TLS参数与UTC时区。
}
