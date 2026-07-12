// 📌 影响范围：读取内嵌 SQL 迁移；连接并修改 PostgreSQL schema_migrations 与业务表。
package migration

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
)

//go:embed migrations/*.sql
var files embed.FS

// Runner 管理嵌入式 PostgreSQL 迁移生命周期。
type Runner struct {
	migration *migrate.Migrate
	database  *sql.DB
}

// New 创建连接到指定数据库的迁移执行器。
// @param cfg：PostgreSQL 配置。
// @returns Runner 或迁移源、数据库连接初始化错误。
// ⚠️副作用说明：打开 database/sql 连接池并初始化迁移驱动。
func New(cfg config.Database) (*Runner, error) {
	source, err := iofs.New(files, "migrations")
	// [决策理由] 内嵌迁移源不可读取时没有安全的 schema 版本可执行。
	if err != nil {
		return nil, fmt.Errorf("打开内嵌迁移: %w", err)
	}
	database, err := sql.Open("pgx", db.URL(cfg))
	// [决策理由] SQL 驱动无法初始化时迁移不可执行。
	if err != nil {
		return nil, fmt.Errorf("打开迁移数据库: %w", err)
	}
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	// [决策理由] 驱动初始化失败时必须关闭已打开的连接池。
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("初始化 PostgreSQL 迁移驱动: %w", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, cfg.Name, driver)
	// [决策理由] migrate 实例创建失败时数据库句柄不再由调用方使用。
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("创建迁移执行器: %w", err)
	}
	runner := &Runner{migration: migrator, database: database}

	// >>> 数据演变示例
	// 1. 有效配置 + 内嵌 SQL -> postgres driver -> Runner。
	// 2. 数据库驱动初始化失败 -> 关闭 sql.DB -> 返回错误。
	return runner, nil
}

// Up 执行所有尚未应用的迁移。
// @param 无。
// @returns 迁移错误；无新迁移视为成功。
// ⚠️副作用说明：可能创建或修改 PostgreSQL 表和索引。
func (r *Runner) Up() error {
	err := r.migration.Up()
	// [决策理由] ErrNoChange 表示数据库已经是最新版本，不应阻断重复启动。
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	// [决策理由] 其他错误可能留下 dirty 版本，必须向启动流程返回。
	if err != nil {
		return fmt.Errorf("执行数据库迁移: %w", err)
	}

	// >>> 数据演变示例
	// 1. version=0 -> Up -> 创建 plugin_config -> version=1。
	// 2. version=1 -> Up -> ErrNoChange -> nil。
	return nil
}

// Down 回滚指定数量的迁移。
// @param steps：要回滚的正整数步数。
// @returns 参数或迁移错误。
// ⚠️副作用说明：可能删除数据库表及其中数据。
func (r *Runner) Down(steps int) error {
	// [决策理由] 非正步数语义不明确，拒绝执行避免意外全量回滚。
	if steps <= 0 {
		return errors.New("回滚步数必须大于 0")
	}
	err := r.migration.Steps(-steps)
	// [决策理由] 没有可回滚迁移时保持幂等成功。
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	// [决策理由] 回滚失败必须保留错误供运维判断 dirty 状态。
	if err != nil {
		return fmt.Errorf("回滚数据库迁移: %w", err)
	}

	// >>> 数据演变示例
	// 1. version=1,steps=1 -> 执行 down SQL -> version=0。
	// 2. steps=0 -> 参数校验 -> 返回错误且不访问数据库。
	return nil
}

// Version 返回当前迁移版本与 dirty 状态。
// @param 无。
// @returns 版本、dirty 标记及读取错误。
// ⚠️副作用说明：读取 PostgreSQL schema_migrations 表。
func (r *Runner) Version() (uint, bool, error) {
	version, dirty, err := r.migration.Version()
	// [决策理由] 尚未执行任何迁移时版本表为空，统一表示为版本 0。
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	// [决策理由] 其他读取错误必须保留，避免把数据库故障误报成版本 0。
	if err != nil {
		return 0, false, fmt.Errorf("读取迁移版本: %w", err)
	}

	// >>> 数据演变示例
	// 1. schema_migrations=1,false -> 返回 1,false,nil。
	// 2. 尚无版本 -> ErrNilVersion -> 返回 0,false,nil。
	return version, dirty, nil
}

// Close 释放迁移源和数据库连接。
// @param 无。
// @returns 迁移驱动或 sql.DB 关闭错误。
// ⚠️副作用说明：关闭迁移器持有的文件源与数据库连接。
func (r *Runner) Close() error {
	sourceErr, databaseErr := r.migration.Close()
	// [决策理由] 迁移器数据库关闭错误优先返回，通常更影响资源释放。
	if databaseErr != nil {
		return databaseErr
	}
	// [决策理由] 文件源关闭错误也应反馈给调用方。
	if sourceErr != nil {
		return sourceErr
	}
	err := r.database.Close()

	// >>> 数据演变示例
	// 1. migrate.Close 成功 -> sql.DB.Close -> nil。
	// 2. database driver Close 失败 -> 立即返回对应错误。
	return err
}
