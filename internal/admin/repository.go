// 📌 影响范围：读写 PostgreSQL plugin_config 与 admin_audit_logs 表；开启数据库事务。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
}

// SystemAdminRepository 定义最高管理员身份数据的加载能力。
type SystemAdminRepository interface {
	ListSystemAdmins(context.Context) ([]SystemAdmin, error)
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
	rows, err := r.pool.Query(ctx, `SELECT plugin_name, enabled, priority, config_json FROM plugin_config ORDER BY priority DESC, plugin_name ASC`)
	// [决策理由] 查询失败时无法提供可信的完整管理快照。
	if err != nil {
		return nil, fmt.Errorf("查询插件配置: %w", err)
	}
	defer rows.Close()
	states := make([]PluginState, 0)
	for rows.Next() {
		var state PluginState
		// [决策理由] 单行结构异常意味着快照不完整，不能静默忽略。
		if err := rows.Scan(&state.Name, &state.Enabled, &state.Priority, &state.ConfigJSON); err != nil {
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

// ListSystemAdmins 查询全部最高管理员账号状态。
// @param ctx：控制数据库查询生命周期。
// @returns 按 QQ 号排序的管理员快照，或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) ListSystemAdmins(ctx context.Context) ([]SystemAdmin, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id, nickname, enabled FROM system_admins ORDER BY user_id ASC`)
	// [决策理由] 查询失败时不能发布不完整的管理员身份快照。
	if err != nil {
		return nil, fmt.Errorf("查询最高管理员: %w", err)
	}
	defer rows.Close()
	admins := make([]SystemAdmin, 0)
	for rows.Next() {
		var account SystemAdmin
		// [决策理由] 任一管理员行异常都会让授权结果不可信，必须终止加载。
		if err := rows.Scan(&account.UserID, &account.Nickname, &account.Enabled); err != nil {
			return nil, fmt.Errorf("扫描最高管理员: %w", err)
		}
		admins = append(admins, account)
	}
	// [决策理由] 迭代结束仍需检查连接和协议层错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历最高管理员: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB[100:true,200:false] -> 扫描 -> 返回两个账号及状态。
	// 2. 空表 -> 零行 -> 返回空切片。
	return admins, nil
}

// BootstrapSystemAdmin 首次写入环境变量指定的最高管理员，已有记录保持数据库配置。
// @param ctx：写入生命周期；userID：首次引导的 QQ 号。
// @returns QQ 号格式或数据库写入错误。
// ⚠️副作用说明：可能向 system_admins 插入一条启用账号；不会覆盖已有账号状态。
func (r *PostgresRepository) BootstrapSystemAdmin(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	// [决策理由] 空值表示部署者暂不启用管理入口，不写入无效账号。
	if userID == "" {
		return nil
	}
	// [决策理由] QQ 号必须只含数字，防止配置拼写错误形成无法登录的管理员。
	if !numericUserID(userID) {
		return fmt.Errorf("最高管理员 QQ 号 %q 格式无效", userID)
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO system_admins(user_id,nickname,enabled,created_by) VALUES($1,'环境变量引导',TRUE,$1) ON CONFLICT(user_id) DO NOTHING`, userID)
	// [决策理由] 引导写入失败时管理员权限不可用，必须阻止依赖它的入口启动。
	if err != nil {
		return fmt.Errorf("引导最高管理员: %w", err)
	}

	// >>> 数据演变示例
	// 1. SUPER_ADMIN_QQ=123且DB无记录 -> INSERT -> 123启用。
	// 2. DB已有123且已禁用 -> ON CONFLICT DO NOTHING -> 保持数据库禁用状态。
	return nil
}

// numericUserID 判断用户 ID 是否为纯数字字符串。
// @param value：待校验用户 ID。
// @returns 非空且全部为十进制数字时返回 true。
// ⚠️副作用说明：无。
func numericUserID(value string) bool {
	// [决策理由] 空字符串不能代表 QQ 用户。
	if value == "" {
		return false
	}
	for _, current := range value {
		// [决策理由] QQ 号只允许 ASCII 十进制数字，拒绝符号、空格和其他数字字符。
		if current < '0' || current > '9' {
			return false
		}
	}

	// >>> 数据演变示例
	// 1. "123456" -> 每字符均为0-9 -> true。
	// 2. "123abc" -> 遇到a -> false。
	return true
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
	err := tx.QueryRow(ctx, `SELECT plugin_name, enabled, priority, config_json FROM plugin_config WHERE plugin_name=$1 FOR UPDATE`, name).Scan(&state.Name, &state.Enabled, &state.Priority, &state.ConfigJSON)
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
