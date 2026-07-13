// 📌 影响范围：读写 PostgreSQL plugin_commands 与 admin_audit_logs 表；开启命令管理事务。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ListCommands 查询全部插件命令。
// @param ctx：控制数据库查询生命周期。
// @returns 按作用域和命令排序的命令快照，或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) ListCommands(ctx context.Context) ([]CommandState, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,scope_type,scope_id,plugin_name,feature_key,command,normalized_command,enabled FROM plugin_commands ORDER BY scope_type,scope_id,normalized_command`)
	// [决策理由] 查询失败时无法返回完整命令配置。
	if err != nil {
		return nil, fmt.Errorf("查询插件命令: %w", err)
	}
	defer rows.Close()
	commands := make([]CommandState, 0)
	for rows.Next() {
		var current CommandState
		// [决策理由] 任一行扫描失败都会导致管理快照不完整。
		if err := scanCommand(rows, &current); err != nil {
			return nil, err
		}
		commands = append(commands, current)
	}
	// [决策理由] 迭代完成后仍需检查连接和协议错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历插件命令: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB[global:ping] -> 扫描 -> []CommandState{ping}。
	// 2. 空表 -> 零行 -> 空切片。
	return commands, nil
}

// CreateCommand 在事务中新增命令并记录审计。
// @param ctx：事务生命周期；actor：操作者；input：命令字段；normalized：标准化命令。
// @returns 新命令快照，或重复、外键、事务错误。
// ⚠️副作用说明：插入 plugin_commands 与 admin_audit_logs。
func (r *PostgresRepository) CreateCommand(ctx context.Context, actor Actor, input CommandCreate, normalized string) (CommandState, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 命令与审计必须在同一事务提交。
	if err != nil {
		return CommandState{}, fmt.Errorf("开启新增命令事务: %w", err)
	}
	defer tx.Rollback(ctx)
	var created CommandState
	err = tx.QueryRow(ctx, `INSERT INTO plugin_commands(scope_type,scope_id,plugin_name,feature_key,command,normalized_command,enabled,is_default,created_by) VALUES($1,$2,$3,$4,$5,$6,TRUE,FALSE,$7) RETURNING id,scope_type,scope_id,plugin_name,feature_key,command,normalized_command,enabled`, input.ScopeType, input.ScopeID, input.PluginName, input.FeatureKey, input.Command, normalized, actor.ID).Scan(&created.ID, &created.ScopeType, &created.ScopeID, &created.PluginName, &created.FeatureKey, &created.Command, &created.NormalizedCommand, &created.Enabled)
	// [决策理由] 唯一约束冲突需要转换为稳定领域错误供 QQ/WebUI 展示。
	if commandConflict(err) {
		return CommandState{}, ErrCommandConflict
	}
	// [决策理由] 其他插入错误需保留数据库上下文。
	if err != nil {
		return CommandState{}, fmt.Errorf("新增插件命令: %w", err)
	}
	// [决策理由] 审计失败时新增命令必须回滚。
	if err := insertCommandAudit(ctx, tx, actor, "command.create", created.ID, nil, created); err != nil {
		return CommandState{}, err
	}
	// [决策理由] 命令和审计都成功后才能发布持久化结果。
	if err := tx.Commit(ctx); err != nil {
		return CommandState{}, fmt.Errorf("提交新增命令事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. global:测试 -> INSERT+audit -> 返回新ID。
	// 2. 同作用域已有测试 -> 23505 -> ErrCommandConflict并回滚。
	return created, nil
}

// RenameCommand 在事务中修改命令文本并记录前后快照。
// @param ctx：事务生命周期；actor：操作者；id：命令 ID；command：新文本；normalized：标准化文本。
// @returns 更新后命令，或未找到、重复、事务错误。
// ⚠️副作用说明：更新 plugin_commands 并插入 admin_audit_logs。
func (r *PostgresRepository) RenameCommand(ctx context.Context, actor Actor, id int64, command string, normalized string) (CommandState, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 改名与审计必须原子提交。
	if err != nil {
		return CommandState{}, fmt.Errorf("开启命令改名事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, err := selectCommand(ctx, tx, id)
	// [决策理由] 不存在的命令不能产生改名审计。
	if err != nil {
		return CommandState{}, err
	}
	after := before
	after.Command, after.NormalizedCommand = command, normalized
	_, err = tx.Exec(ctx, `UPDATE plugin_commands SET command=$2,normalized_command=$3,updated_at=NOW() WHERE id=$1`, id, command, normalized)
	// [决策理由] 改名与同作用域其他命令冲突时必须明确拒绝。
	if commandConflict(err) {
		return CommandState{}, ErrCommandConflict
	}
	// [决策理由] 其他更新错误必须回滚事务。
	if err != nil {
		return CommandState{}, fmt.Errorf("修改插件命令: %w", err)
	}
	// [决策理由] 审计记录必须包含改名前后完整快照。
	if err := insertCommandAudit(ctx, tx, actor, "command.rename", id, before, after); err != nil {
		return CommandState{}, err
	}
	// [决策理由] 所有事务操作完成后再提交。
	if err := tx.Commit(ctx); err != nil {
		return CommandState{}, fmt.Errorf("提交命令改名事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. id=1,ping -> 改为测试 -> audit前后快照 -> 返回测试。
	// 2. id=404 -> ErrCommandNotFound -> 不更新不审计。
	return after, nil
}

// DeleteCommand 在事务中删除命令并保存删除前快照。
// @param ctx：事务生命周期；actor：操作者；id：命令 ID。
// @returns 未找到或事务错误。
// ⚠️副作用说明：删除 plugin_commands 并插入 admin_audit_logs。
func (r *PostgresRepository) DeleteCommand(ctx context.Context, actor Actor, id int64) error {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 删除与审计必须原子提交。
	if err != nil {
		return fmt.Errorf("开启删除命令事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, err := selectCommand(ctx, tx, id)
	// [决策理由] 不存在的命令无法形成有效删除记录。
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM plugin_commands WHERE id=$1`, id)
	// [决策理由] 删除失败时不得写入成功审计。
	if err != nil {
		return fmt.Errorf("删除插件命令: %w", err)
	}
	// [决策理由] 删除审计只包含 before，明确表示目标已不存在。
	if err := insertCommandAudit(ctx, tx, actor, "command.delete", id, before, nil); err != nil {
		return err
	}
	// [决策理由] 删除与审计均完成后再提交。
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交删除命令事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. id=1存在 -> DELETE+before审计 -> nil。
	// 2. id=404 -> ErrCommandNotFound -> 回滚。
	return nil
}

type commandScanner interface {
	Scan(...any) error
}

// scanCommand 从 pgx 行读取统一命令字段。
// @param scanner：Rows 或 Row 扫描器；target：接收快照。
// @returns 扫描错误。
// ⚠️副作用说明：修改 target 字段。
func scanCommand(scanner commandScanner, target *CommandState) error {
	err := scanner.Scan(&target.ID, &target.ScopeType, &target.ScopeID, &target.PluginName, &target.FeatureKey, &target.Command, &target.NormalizedCommand, &target.Enabled)
	// [决策理由] 扫描失败需要统一增加命令上下文。
	if err != nil {
		return fmt.Errorf("扫描插件命令: %w", err)
	}

	// >>> 数据演变示例
	// 1. SQL八列 -> target完整填充 -> nil。
	// 2. 列类型不匹配 -> Scan错误 -> 包装返回。
	return nil
}

// selectCommand 在事务内锁定并读取命令。
// @param ctx：查询生命周期；tx：活动事务；id：命令 ID。
// @returns 命令快照或未找到、扫描错误。
// ⚠️副作用说明：对目标 plugin_commands 行加锁至事务结束。
func selectCommand(ctx context.Context, tx pgx.Tx, id int64) (CommandState, error) {
	var current CommandState
	err := tx.QueryRow(ctx, `SELECT id,scope_type,scope_id,plugin_name,feature_key,command,normalized_command,enabled FROM plugin_commands WHERE id=$1 FOR UPDATE`, id).Scan(&current.ID, &current.ScopeType, &current.ScopeID, &current.PluginName, &current.FeatureKey, &current.Command, &current.NormalizedCommand, &current.Enabled)
	// [决策理由] 数据库无行应转换为稳定领域错误。
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandState{}, fmt.Errorf("%w: %d", ErrCommandNotFound, id)
	}
	// [决策理由] 其他扫描错误需要保留数据库上下文。
	if err != nil {
		return CommandState{}, fmt.Errorf("读取插件命令: %w", err)
	}

	// >>> 数据演变示例
	// 1. id=1 -> FOR UPDATE -> 返回命令快照。
	// 2. id=404 -> pgx.ErrNoRows -> ErrCommandNotFound。
	return current, nil
}

// insertCommandAudit 写入当前命令事务的审计快照。
// @param ctx：事务生命周期；tx：活动事务；actor：操作者；action：动作；id：命令 ID；before、after：前后状态。
// @returns JSON 编码或数据库错误。
// ⚠️副作用说明：向 admin_audit_logs 插入一行。
func insertCommandAudit(ctx context.Context, tx pgx.Tx, actor Actor, action string, id int64, before any, after any) error {
	beforeJSON, err := json.Marshal(before)
	// [决策理由] 审计前快照编码失败时不能提交业务变更。
	if err != nil {
		return fmt.Errorf("编码命令变更前快照: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	// [决策理由] 审计后快照编码失败时不能提交业务变更。
	if err != nil {
		return fmt.Errorf("编码命令变更后快照: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,$4,'command',$5,NULLIF($6,'null')::jsonb,NULLIF($7,'null')::jsonb,TRUE,NULLIF($8,''))`, actor.ID, actor.Role, actor.Channel, action, fmt.Sprintf("%d", id), string(beforeJSON), string(afterJSON), actor.RequestID)
	// [决策理由] 审计插入失败时调用方必须回滚整个命令事务。
	if err != nil {
		return fmt.Errorf("写入命令管理审计: %w", err)
	}

	// >>> 数据演变示例
	// 1. rename + before/after -> 两份JSONB -> audit成功。
	// 2. delete + after=nil -> after_json=NULL -> audit成功。
	return nil
}

// commandConflict 判断数据库错误是否为作用域命令唯一约束冲突。
// @param err：PostgreSQL 写入错误。
// @returns SQLSTATE 23505 时返回 true。
// ⚠️副作用说明：无。
func commandConflict(err error) bool {
	var postgresError *pgconn.PgError
	matched := errors.As(err, &postgresError) && postgresError.Code == "23505"

	// >>> 数据演变示例
	// 1. PgError{23505} -> true。
	// 2. nil或连接错误 -> false。
	return matched
}
