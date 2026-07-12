// 📌 影响范围：通过网络连接 PostgreSQL，并创建和探测 pgx 连接池。
package db

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/w1ndys/w1ndys-bot/internal/config"
)

// Open 创建 PostgreSQL 连接池并验证数据库可达性。
// @param ctx：控制连接和探测生命周期；cfg：数据库连接配置。
// @returns 可用的 pgxpool.Pool，或配置、连接及探测错误。
// ⚠️副作用说明：建立数据库网络连接；探测失败时关闭已创建的连接池。
func Open(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	dsn := URL(cfg)

	pool, err := pgxpool.New(ctx, dsn)
	// [决策理由] 连接池配置解析失败时没有可供探测的实例，直接返回原始上下文错误。
	if err != nil {
		return nil, fmt.Errorf("创建 PostgreSQL 连接池: %w", err)
	}
	// [决策理由] pgxpool.New 为惰性初始化，必须 Ping 才能在启动阶段发现网络或鉴权故障。
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("探测 PostgreSQL: %w", err)
	}

	// >>> 数据演变示例
	// 1. postgres:5432 + 有效凭据 -> PostgreSQL DSN -> 连接池 -> Ping 成功 -> 返回连接池。
	// 2. db.invalid:5432 -> PostgreSQL DSN -> 连接池 -> Ping 失败 -> 关闭连接池并返回错误。
	return pool, nil
}

// URL 将数据库配置转换为 PostgreSQL 连接 URL。
// @param cfg：数据库主机、端口、用户、密码、名称与 TLS 模式。
// @returns 已正确转义凭据和查询参数的连接字符串。
// ⚠️副作用说明：无。
func URL(cfg config.Database) string {
	result := (&url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   cfg.Name,
		RawQuery: url.Values{
			"sslmode": []string{cfg.SSLMode},
		}.Encode(),
	}).String()

	// >>> 数据演变示例
	// 1. host=postgres,port=5432,name=w1ndys_bot -> postgres://...@postgres:5432/w1ndys_bot。
	// 2. password=a@b -> URL 转义 -> 密码不会破坏连接字符串结构。
	return result
}
