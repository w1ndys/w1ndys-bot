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
	UpdatePlugin(context.Context, Actor, string, PluginPatch) (PluginState, PluginState, error)
	GetPluginConfig(context.Context, string) (PluginConfigState, error)
	UpdatePluginConfig(context.Context, Actor, string, PluginConfigUpdate, json.RawMessage, json.RawMessage) (PluginConfigState, error)
	ListPluginFeatures(context.Context, string) ([]FeatureState, error)
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
	ListAuditLogs(context.Context, AuditQuery) (AuditPage, error)
	GetAuditLog(context.Context, int64) (AuditState, error)
}

// GetPluginConfig 读取插件内部完整配置与乐观锁版本。
// @param ctx：查询生命周期；name：插件稳定名称。
// @returns 完整配置快照或未找到、数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询；返回值只能在服务端校验与应用，不能直接输出到 API。
func (r *PostgresRepository) GetPluginConfig(ctx context.Context, name string) (PluginConfigState, error) {
	var state PluginConfigState
	err := r.pool.QueryRow(ctx, `SELECT plugin_name,config_json,config_version FROM plugin_config WHERE plugin_name=$1`, name).Scan(&state.PluginName, &state.ConfigJSON, &state.Version)
	// [决策理由] 稳定领域错误让 API 不依赖数据库驱动错误类型。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginConfigState{}, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}
	// [决策理由] 查询异常时不能返回不完整配置快照。
	if err != nil {
		return PluginConfigState{}, fmt.Errorf("读取插件声明式配置: %w", err)
	}

	// >>> 数据演变示例
	// 1. echo:{response_prefix:"[bot]"}:3 -> 扫描 -> 完整快照version=3。
	// 2. missing -> pgx.ErrNoRows -> ErrPluginNotFound。
	return state, nil
}

// UpdatePluginConfig 使用版本 CAS 原子更新完整配置并写入脱敏审计。
// @param ctx：事务生命周期；actor：操作者；name：插件名；update：完整配置及期望版本；beforeAudit、afterAudit：已脱敏审计快照。
// @returns 更新后的内部完整配置与递增版本，或冲突、数据库错误。
// ⚠️副作用说明：更新 plugin_config 并插入 admin_audit_logs；审计内容由调用方预先脱敏。
func (r *PostgresRepository) UpdatePluginConfig(ctx context.Context, actor Actor, name string, update PluginConfigUpdate, beforeAudit, afterAudit json.RawMessage) (PluginConfigState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	// [决策理由] 配置与审计必须在同一事务提交。
	if err != nil {
		return PluginConfigState{}, fmt.Errorf("开启插件配置事务: %w", err)
	}
	defer tx.Rollback(ctx)
	var currentVersion int64
	err = tx.QueryRow(ctx, `SELECT config_version FROM plugin_config WHERE plugin_name=$1 FOR UPDATE`, name).Scan(&currentVersion)
	// [决策理由] 不存在的插件不能产生孤立配置。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginConfigState{}, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}
	// [决策理由] 锁定失败时不能执行不可靠的版本判断。
	if err != nil {
		return PluginConfigState{}, fmt.Errorf("锁定插件配置: %w", err)
	}
	// [决策理由] 陈旧页面不得覆盖其他管理员已保存的配置。
	if currentVersion != update.ExpectedVersion {
		return PluginConfigState{}, fmt.Errorf("%w: 当前版本 %d", ErrPluginConfigConflict, currentVersion)
	}
	var state PluginConfigState
	err = tx.QueryRow(ctx, `UPDATE plugin_config SET config_json=$2,config_version=config_version+1,updated_at=NOW() WHERE plugin_name=$1 RETURNING plugin_name,config_json,config_version`, name, update.ConfigJSON).Scan(&state.PluginName, &state.ConfigJSON, &state.Version)
	// [决策理由] 更新失败时不得留下成功审计。
	if err != nil {
		return PluginConfigState{}, fmt.Errorf("更新插件声明式配置: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,'plugin.config.update','plugin_config',$4,$5,$6,TRUE,NULLIF($7,''))`, actor.ID, actor.Role, actor.Channel, name, beforeAudit, afterAudit, actor.RequestID)
	// [决策理由] 审计失败必须回滚配置更新。
	if err != nil {
		return PluginConfigState{}, fmt.Errorf("写入插件配置审计: %w", err)
	}
	// [决策理由] 仅在配置与审计都成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return PluginConfigState{}, fmt.Errorf("提交插件配置事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. version=3,expected=3 -> 写入配置+审计 -> version=4。
	// 2. version=4,expected=3 -> 冲突 -> 事务回滚且不写审计。
	return state, nil
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
	rows, err := r.pool.Query(ctx, `SELECT c.plugin_name,d.display_name,d.description,d.available,c.enabled,c.priority,d.group_controllable,c.group_default_enabled,c.config_json FROM plugin_config c JOIN plugin_definitions d ON d.plugin_name=c.plugin_name ORDER BY c.priority DESC,c.plugin_name ASC`)
	// [决策理由] 查询失败时无法提供可信的完整管理快照。
	if err != nil {
		return nil, fmt.Errorf("查询插件配置: %w", err)
	}
	defer rows.Close()
	states := make([]PluginState, 0)
	for rows.Next() {
		var state PluginState
		// [决策理由] 单行结构异常意味着快照不完整，不能静默忽略。
		if err := rows.Scan(&state.Name, &state.DisplayName, &state.Description, &state.Available, &state.Enabled, &state.Priority, &state.GroupControllable, &state.GroupDefaultEnabled, &state.ConfigJSON); err != nil {
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
// @returns 事务锁定的更新前快照、更新后快照，或未找到、事务错误。
// ⚠️副作用说明：更新 plugin_config 并插入 admin_audit_logs。
func (r *PostgresRepository) UpdatePlugin(ctx context.Context, actor Actor, name string, patch PluginPatch) (PluginState, PluginState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	// [决策理由] 无法开启事务时不能保证配置与审计原子写入。
	if err != nil {
		return PluginState{}, PluginState{}, fmt.Errorf("开启插件管理事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, err := selectPlugin(ctx, tx, name)
	// [决策理由] 不存在的插件不得产生孤立管理记录。
	if err != nil {
		return PluginState{}, PluginState{}, err
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
		return PluginState{}, PluginState{}, fmt.Errorf("更新插件配置: %w", err)
	}
	beforeAudit := before
	beforeAudit.ConfigJSON = nil
	afterAudit := after
	afterAudit.ConfigJSON = nil
	beforeJSON, err := json.Marshal(beforeAudit)
	// [决策理由] 审计快照无法序列化时不得提交无法追溯的管理变更。
	if err != nil {
		return PluginState{}, PluginState{}, fmt.Errorf("编码变更前快照: %w", err)
	}
	afterJSON, err := json.Marshal(afterAudit)
	// [决策理由] 审计快照无法序列化时不得提交无法追溯的管理变更。
	if err != nil {
		return PluginState{}, PluginState{}, fmt.Errorf("编码变更后快照: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,'plugin.update','plugin',$4,$5,$6,TRUE,NULLIF($7,''))`, actor.ID, actor.Role, actor.Channel, name, beforeJSON, afterJSON, actor.RequestID)
	// [决策理由] 审计写入失败必须连同配置修改一起回滚。
	if err != nil {
		return PluginState{}, PluginState{}, fmt.Errorf("写入插件管理审计: %w", err)
	}
	// [决策理由] 只有配置与审计均成功后才能提交事务。
	if err := tx.Commit(ctx); err != nil {
		return PluginState{}, PluginState{}, fmt.Errorf("提交插件管理事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. ping:false+secret配置 + Enabled=true -> UPDATE + 不含配置的audit -> ping:true。
	// 2. missing + Priority=10 -> SELECT 无行 -> ErrPluginNotFound + 回滚。
	return before, after, nil
}

// selectPlugin 在事务内锁定并读取一个插件配置。
// @param ctx：查询生命周期；tx：当前事务；name：插件名。
// @returns 当前插件快照，或未找到、扫描错误。
// ⚠️副作用说明：对目标 plugin_config 行加行锁直至事务结束。
func selectPlugin(ctx context.Context, tx pgx.Tx, name string) (PluginState, error) {
	var state PluginState
	err := tx.QueryRow(ctx, `SELECT c.plugin_name,d.display_name,d.description,d.available,c.enabled,c.priority,d.group_controllable,c.group_default_enabled,c.config_json FROM plugin_config c JOIN plugin_definitions d ON d.plugin_name=c.plugin_name WHERE c.plugin_name=$1 FOR UPDATE OF c`, name).Scan(&state.Name, &state.DisplayName, &state.Description, &state.Available, &state.Enabled, &state.Priority, &state.GroupControllable, &state.GroupDefaultEnabled, &state.ConfigJSON)
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
