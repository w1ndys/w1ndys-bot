// Package db 管理 PostgreSQL 连接池与数据库迁移。
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/w1ndys/w1ndys-bot/internal/config"
)

// Connect 依据 cfg 中强类型的连接字段建立 pgxpool 连接池，并在返回前用一次
// ping 验证连通性，无法连通时立即失败。
func Connect(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("解析 pgx 配置: %w", err)
	}
	poolCfg.ConnConfig.Host = cfg.DBHost
	poolCfg.ConnConfig.Port = uint16(cfg.DBPort)
	poolCfg.ConnConfig.User = cfg.DBUser
	poolCfg.ConnConfig.Password = cfg.DBPassword
	poolCfg.ConnConfig.Database = cfg.DBName
	poolCfg.ConnConfig.RuntimeParams["sslmode"] = cfg.DBSSLMode

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping 数据库: %w", err)
	}
	return pool, nil
}
