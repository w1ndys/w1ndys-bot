// 📌 影响范围：读写 PostgreSQL plugin_config 与 admin_audit_logs 表；开启数据库事务。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 定义管理服务所需的插件持久化能力。
type Repository interface {
	ListPlugins(context.Context) ([]PluginState, error)
	UpdatePlugin(context.Context, Actor, string, PluginPatch) (PluginState, error)
	ListCommands(context.Context) ([]CommandState, error)
	CreateCommand(context.Context, Actor, CommandCreate, string) (CommandState, error)
	RenameCommand(context.Context, Actor, int64, string, string) (CommandState, error)
	DeleteCommand(context.Context, Actor, int64) error
	ListPermissions(context.Context) ([]PermissionState, error)
	SetPermission(context.Context, Actor, PermissionSet) (PermissionState, error)
	DeletePermission(context.Context, Actor, int64) error
	ListSystemSettings(context.Context) ([]SettingState, error)
	SetSystemSetting(context.Context, Actor, SettingState) (SettingState, error)
	DeleteSystemSetting(context.Context, Actor, string) error
}

// PostgresRepository 使用 PostgreSQL 原子保存插件配置和审计记录。
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository 创建管理仓库。
// @param pool：可用的 PostgreSQL 连接池。
// @returns 管理仓库实例。
// ⚠️副作用说明：无；仅保存连接池引用。
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	repository := &PostgresRepository{pool: pool}

	// >>> 数据演变示例
	// 1. pool=有效连接池 -> PostgresRepository -> 可执行管理事务。
	// 2. pool=nil -> PostgresRepository -> 组装阶段应避免调用其方法。
	return repository
}

// ListPlugins 查询全部插件运行配置。
// @param ctx：控制数据库查询生命周期。
// @returns 按优先级和名称排序的插件快照，或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) ListPlugins(ctx context.Context) ([]PluginState, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.plugin_name,d.display_name,d.description,d.version,d.available,c.enabled,c.priority,c.config_json FROM plugin_config c JOIN plugin_definitions d ON d.plugin_name=c.plugin_name ORDER BY c.priority DESC,c.plugin_name ASC`)
	// [决策理由] 查询失败时无法提供可信的完整管理快照。
	if err != nil {
		return nil, fmt.Errorf("查询插件配置: %w", err)
	}
	defer rows.Close()
	states := make([]PluginState, 0)
	for rows.Next() {
		var state PluginState
		// [决策理由] 单行结构异常意味着快照不完整，不能静默忽略。
		if err := rows.Scan(&state.Name, &state.DisplayName, &state.Description, &state.Version, &state.Available, &state.Enabled, &state.Priority, &state.ConfigJSON); err != nil {
			return nil, fmt.Errorf("扫描插件配置: %w", err)
		}
		states = append(states, state)
	}
	// [决策理由] rows 在迭代结束后可能携带网络或协议错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历插件配置: %w", err)
	}

	// >>> 数据演变示例
	// 1. ping:true:100 -> 查询并扫描 -> []PluginState{ping}。
	// 2. 空表 -> 零行 -> 空切片。
	return states, nil
}

// UpdatePlugin 在同一事务内更新插件配置并记录成功审计。
// @param ctx：事务生命周期；actor：操作者；name：插件名；patch：目标字段。
// @returns 更新后的插件快照，或未找到、事务错误。
// ⚠️副作用说明：更新 plugin_config 并插入 admin_audit_logs。
func (r *PostgresRepository) UpdatePlugin(ctx context.Context, actor Actor, name string, patch PluginPatch) (PluginState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	// [决策理由] 无法开启事务时不能保证配置与审计原子写入。
	if err != nil {
		return PluginState{}, fmt.Errorf("开启插件管理事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, err := selectPlugin(ctx, tx, name)
	// [决策理由] 不存在的插件不得产生孤立管理记录。
	if err != nil {
		return PluginState{}, err
	}
	after := before
	// [决策理由] nil 表示调用方未要求修改启用状态。
	if patch.Enabled != nil {
		after.Enabled = *patch.Enabled
	}
	// [决策理由] nil 表示调用方未要求修改优先级。
	if patch.Priority != nil {
		after.Priority = *patch.Priority
	}
	_, err = tx.Exec(ctx, `UPDATE plugin_config SET enabled=$2, priority=$3, updated_at=NOW() WHERE plugin_name=$1`, name, after.Enabled, after.Priority)
	// [决策理由] 配置写入失败时必须回滚，不能留下成功审计。
	if err != nil {
		return PluginState{}, fmt.Errorf("更新插件配置: %w", err)
	}
	beforeJSON, err := json.Marshal(before)
	// [决策理由] 审计快照无法序列化时不得提交无法追溯的管理变更。
	if err != nil {
		return PluginState{}, fmt.Errorf("编码变更前快照: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	// [决策理由] 审计快照无法序列化时不得提交无法追溯的管理变更。
	if err != nil {
		return PluginState{}, fmt.Errorf("编码变更后快照: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,'plugin.update','plugin',$4,$5,$6,TRUE,NULLIF($7,''))`, actor.ID, actor.Role, actor.Channel, name, beforeJSON, afterJSON, actor.RequestID)
	// [决策理由] 审计写入失败必须连同配置修改一起回滚。
	if err != nil {
		return PluginState{}, fmt.Errorf("写入插件管理审计: %w", err)
	}
	// [决策理由] 只有配置与审计均成功后才能提交事务。
	if err := tx.Commit(ctx); err != nil {
		return PluginState{}, fmt.Errorf("提交插件管理事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. ping:false + Enabled=true -> UPDATE + audit -> ping:true。
	// 2. missing + Priority=10 -> SELECT 无行 -> ErrPluginNotFound + 回滚。
	return after, nil
}

// selectPlugin 在事务内锁定并读取一个插件配置。
// @param ctx：查询生命周期；tx：当前事务；name：插件名。
// @returns 当前插件快照，或未找到、扫描错误。
// ⚠️副作用说明：对目标 plugin_config 行加行锁直至事务结束。
func selectPlugin(ctx context.Context, tx pgx.Tx, name string) (PluginState, error) {
	var state PluginState
	err := tx.QueryRow(ctx, `SELECT c.plugin_name,d.display_name,d.description,d.version,d.available,c.enabled,c.priority,c.config_json FROM plugin_config c JOIN plugin_definitions d ON d.plugin_name=c.plugin_name WHERE c.plugin_name=$1 FOR UPDATE OF c`, name).Scan(&state.Name, &state.DisplayName, &state.Description, &state.Version, &state.Available, &state.Enabled, &state.Priority, &state.ConfigJSON)
	// [决策理由] 将数据库无行错误转换为稳定领域错误，避免上层依赖 pgx。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginState{}, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}
	// [决策理由] 其他扫描错误需要保留数据库上下文供排障。
	if err != nil {
		return PluginState{}, fmt.Errorf("读取插件配置: %w", err)
	}

	// >>> 数据演变示例
	// 1. name=ping -> 锁定存在行 -> 返回当前快照。
	// 2. name=missing -> pgx.ErrNoRows -> ErrPluginNotFound。
	return state, nil
}
