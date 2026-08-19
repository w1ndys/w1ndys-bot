package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/w1ndys/w1ndys-bot/internal/migration"
)

// Runner 执行数据库迁移，由 golang-migrate 实现与测试中的 fake 共同满足。
type Runner interface {
	Up() error
	Down() error
}

// Direction 表示迁移方向。
type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

// migrator 将 *migrate.Migrate 适配为 Runner 契约。
type migrator struct {
	m *migrate.Migrate
}

func (r *migrator) Up() error { return r.m.Up() }

// Down 回滚最近应用的一个版本（单步）。
func (r *migrator) Down() error { return r.m.Steps(-1) }

// NewRunner 基于 pool 与内嵌迁移构建 Runner。
func NewRunner(pool *pgxpool.Pool) (Runner, error) {
	// stdlib.OpenDBFromPool 只是把 pgxpool 包成 *sql.DB，关闭它不会关连接池。
	sqlDB := stdlib.OpenDBFromPool(pool)
	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		return nil, fmt.Errorf("创建迁移数据库驱动: %w", err)
	}
	src, err := iofs.New(migration.Files, "migrations")
	if err != nil {
		return nil, fmt.Errorf("创建迁移源: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return nil, fmt.Errorf("创建迁移器: %w", err)
	}
	return &migrator{m: m}, nil
}

// Migrate 沿方向 d 应用迁移。返回是否有迁移被执行；无变更（已处于目标版本）
// 不算错误。
func Migrate(r Runner, d Direction) (bool, error) {
	var err error
	switch d {
	case DirectionUp:
		err = r.Up()
	case DirectionDown:
		err = r.Down()
	default:
		return false, fmt.Errorf("未知迁移方向 %q", d)
	}
	if errors.Is(err, migrate.ErrNoChange) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("迁移 %s: %w", d, err)
	}
	return true, nil
}
