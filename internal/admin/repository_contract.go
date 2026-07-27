// 📌 影响范围：声明系统设置与审计的管理持久化契约并持有 PostgreSQL 连接池。
package admin

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	ListSystemSettings(context.Context) ([]SettingState, error)
	SetSystemSetting(context.Context, Actor, SettingState) (SettingState, error)
	DeleteSystemSetting(context.Context, Actor, string) error
	ListAuditLogs(context.Context, AuditQuery) (AuditPage, error)
	GetAuditLog(context.Context, int64) (AuditState, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}
