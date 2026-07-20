// 📌 影响范围：读写 PostgreSQL plugin_config、plugin_group_overrides 与 admin_audit_logs；开启数据库事务。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type groupDefaultAuditSnapshot struct {
	PluginName     string `json:"plugin_name"`
	PluginEnabled  bool   `json:"plugin_enabled"`
	DefaultEnabled bool   `json:"default_enabled"`
	DefaultVersion int64  `json:"default_version"`
}

// RecordPluginGroupRefreshFailure 记录群 gate 热刷新失败。
// @param ctx：写入生命周期；actor：操作者；name：插件名。
// @returns 审计写入错误，成功 nil。
// ⚠️副作用说明：向 admin_audit_logs 写入固定 action 和脱敏错误，不写业务数据。
func (r *PostgresRepository) RecordPluginGroupRefreshFailure(ctx context.Context, actor Actor, name string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,success,error_message,request_id) VALUES($1,$2,$3,'plugin.group_control.refresh_failed','plugin_group_control',$4,FALSE,'群控制运行快照刷新失败',NULLIF($5,''))`, actor.ID, actor.Role, actor.Channel, name, actor.RequestID)
	// [决策理由] 数据库异常必须返回供服务层与刷新根因合并。
	if err != nil {
		return fmt.Errorf("写入群控制刷新失败审计: %w", err)
	}

	// >>> 数据演变示例
	// 1. ReloadGroupGate失败 -> 固定action+success=false -> nil。
	// 2. 审计表写失败 -> 返回数据库错误。
	return nil
}

// GetPluginGroupControl 读取插件全局开关、群默认策略与全部覆盖。
// @param ctx：查询生命周期；name：插件名。
// @returns 群控制快照或未找到、数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) GetPluginGroupControl(ctx context.Context, name string) (PluginGroupControlState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	// [决策理由] 默认策略与覆盖必须来自同一 MVCC 快照，避免两次查询间夹入并发写。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("开启群控制只读事务: %w", err)
	}
	defer tx.Rollback(ctx)
	var state PluginGroupControlState
	var controllable bool
	err = tx.QueryRow(ctx, `SELECT c.plugin_name,c.enabled,c.group_default_enabled,c.group_default_version,d.group_controllable FROM plugin_config c JOIN plugin_definitions d ON d.plugin_name=c.plugin_name WHERE c.plugin_name=$1`, name).Scan(&state.PluginName, &state.PluginEnabled, &state.DefaultEnabled, &state.DefaultVersion, &controllable)
	// [决策理由] 不存在的插件不得伪造默认策略。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginGroupControlState{}, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}
	// [决策理由] 主状态查询失败时不能返回部分覆盖。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("读取插件群默认策略: %w", err)
	}
	// [决策理由] 只有 Manifest 明确声明的插件才能暴露群管理界面和写路径。
	if !controllable {
		return PluginGroupControlState{}, ErrGroupControlNotSupported
	}
	rows, err := tx.Query(ctx, `SELECT group_id,enabled,version FROM plugin_group_overrides WHERE plugin_name=$1 ORDER BY group_id`, name)
	// [决策理由] 覆盖列表必须与默认状态一起完整返回。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("读取插件群覆盖: %w", err)
	}
	defer rows.Close()
	state.Overrides = make([]PluginGroupOverride, 0)
	for rows.Next() {
		var item PluginGroupOverride
		// [决策理由] 任一损坏行都会使最终状态判断不可信。
		if err := rows.Scan(&item.GroupID, &item.Enabled, &item.Version); err != nil {
			return PluginGroupControlState{}, fmt.Errorf("扫描插件群覆盖: %w", err)
		}
		state.Overrides = append(state.Overrides, item)
	}
	// [决策理由] 迭代结束后仍需检查连接或协议错误。
	if err := rows.Err(); err != nil {
		return PluginGroupControlState{}, fmt.Errorf("遍历插件群覆盖: %w", err)
	}
	rows.Close()
	// [决策理由] 显式提交只读事务以完整释放快照资源。
	if err := tx.Commit(ctx); err != nil {
		return PluginGroupControlState{}, fmt.Errorf("提交群控制只读事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. enabled=true,default=false,group100=true -> 完整快照。
	// 2. missing -> ErrPluginNotFound。
	return state, nil
}

// SetPluginGroupDefault 使用独立 group_default_version CAS 修改群默认策略并写审计。
// @param ctx：事务生命周期；actor：操作者；name：插件名；enabled：目标值；version：期望版本。
// @returns 更新后完整快照或冲突、数据库错误。
// ⚠️副作用说明：在同一 PostgreSQL 事务更新 plugin_config 并写审计。
func (r *PostgresRepository) SetPluginGroupDefault(ctx context.Context, actor Actor, name string, enabled bool, version int64) (PluginGroupControlState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	// [决策理由] 默认策略与审计必须原子提交。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("开启插件群默认事务: %w", err)
	}
	defer tx.Rollback(ctx)
	var before groupDefaultAuditSnapshot
	var controllable bool
	err = tx.QueryRow(ctx, `SELECT c.plugin_name,c.enabled,c.group_default_enabled,c.group_default_version,d.group_controllable FROM plugin_config c JOIN plugin_definitions d ON d.plugin_name=c.plugin_name WHERE c.plugin_name=$1 FOR UPDATE OF c`, name).Scan(&before.PluginName, &before.PluginEnabled, &before.DefaultEnabled, &before.DefaultVersion, &controllable)
	// [决策理由] 默认策略更新前必须在同一事务锁定有界快照。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginGroupControlState{}, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}
	// [决策理由] 锁定失败时不得继续 CAS。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("锁定插件群默认: %w", err)
	}
	// [决策理由] Manifest 未声明群控制的插件不得被直接仓库调用绕过。
	if !controllable {
		return PluginGroupControlState{}, ErrGroupControlNotSupported
	}
	// [决策理由] 在 UPDATE 前显式检查版本，使审计的 before 与 CAS 基线一致。
	if before.DefaultVersion != version {
		return PluginGroupControlState{}, ErrGroupControlConflict
	}
	var next int64
	err = tx.QueryRow(ctx, `UPDATE plugin_config SET group_default_enabled=$2,group_default_version=group_default_version+1,updated_at=NOW() WHERE plugin_name=$1 AND group_default_version=$3 RETURNING group_default_version`, name, enabled, version).Scan(&next)
	// [决策理由] 无返回行表示版本已变化或插件被移除。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginGroupControlState{}, ErrGroupControlConflict
	}
	// [决策理由] 更新失败时不得写成功审计。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("更新插件群默认: %w", err)
	}
	afterAudit := before
	afterAudit.DefaultEnabled, afterAudit.DefaultVersion = enabled, next
	after := PluginGroupControlState{PluginName: name, PluginEnabled: before.PluginEnabled, DefaultEnabled: enabled, DefaultVersion: next}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(afterAudit)
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,'plugin.group_default.update','plugin_group_control',$4,$5,$6,TRUE,NULLIF($7,''))`, actor.ID, actor.Role, actor.Channel, name, beforeJSON, afterJSON, actor.RequestID)
	// [决策理由] 审计失败必须回滚默认策略。
	if err != nil {
		return PluginGroupControlState{}, fmt.Errorf("写入插件群默认审计: %w", err)
	}
	// [决策理由] 只在业务与审计都成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return PluginGroupControlState{}, fmt.Errorf("提交插件群默认事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. default=true,v2 -> false -> v3+审计。
	// 2. expected2,current3 -> ErrGroupControlConflict。
	return after, nil
}

// SetPluginGroupOverride 新增或 CAS 更新单群覆盖并写审计。
// @param ctx：事务上下文；actor：操作者；name/groupID：目标；enabled：目标值；version：0 表示新增，正数表示 CAS 更新。
// @returns 新覆盖快照或冲突、数据库错误。
// ⚠️副作用说明：在同一 PostgreSQL 事务 upsert 覆盖并写审计。
func (r *PostgresRepository) SetPluginGroupOverride(ctx context.Context, actor Actor, name, groupID string, enabled bool, version int64) (PluginGroupOverride, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	// [决策理由] 覆盖与审计必须原子提交。
	if err != nil {
		return PluginGroupOverride{}, fmt.Errorf("开启群覆盖事务: %w", err)
	}
	defer tx.Rollback(ctx)
	var item PluginGroupOverride
	var beforeJSON json.RawMessage
	// [决策理由] 更新审计必须包含事务内锁定的可信旧快照。
	if version > 0 {
		var before PluginGroupOverride
		lockErr := tx.QueryRow(ctx, `SELECT group_id,enabled,version FROM plugin_group_overrides WHERE plugin_name=$1 AND group_id=$2 FOR UPDATE`, name, groupID).Scan(&before.GroupID, &before.Enabled, &before.Version)
		// [决策理由] 旧覆盖缺失时陈旧更新必须作为冲突拒绝。
		if errors.Is(lockErr, pgx.ErrNoRows) {
			return PluginGroupOverride{}, ErrGroupControlConflict
		}
		// [决策理由] 锁定失败时不能继续更新。
		if lockErr != nil {
			return PluginGroupOverride{}, fmt.Errorf("锁定群覆盖: %w", lockErr)
		}
		beforeJSON, _ = json.Marshal(before)
	}
	// [决策理由] version=0 仅允许新增，已存在行由唯一约束转换为冲突。
	if version == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO plugin_group_overrides(plugin_name,group_id,enabled) VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING group_id,enabled,version`, name, groupID, enabled).Scan(&item.GroupID, &item.Enabled, &item.Version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE plugin_group_overrides SET enabled=$3,version=version+1,updated_at=NOW() WHERE plugin_name=$1 AND group_id=$2 AND version=$4 RETURNING group_id,enabled,version`, name, groupID, enabled, version).Scan(&item.GroupID, &item.Enabled, &item.Version)
	}
	// [决策理由] 无返回行统一表示唯一或版本冲突。
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginGroupOverride{}, ErrGroupControlConflict
	}
	// [决策理由] SQL 失败时不写成功审计。
	if err != nil {
		return PluginGroupOverride{}, fmt.Errorf("保存群覆盖: %w", err)
	}
	afterJSON, _ := json.Marshal(item)
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,'plugin.group_override.set','plugin_group_override',$4,$5,$6,TRUE,NULLIF($7,''))`, actor.ID, actor.Role, actor.Channel, name+":"+groupID, beforeJSON, afterJSON, actor.RequestID)
	// [决策理由] 审计失败必须回滚覆盖。
	if err != nil {
		return PluginGroupOverride{}, fmt.Errorf("写入群覆盖审计: %w", err)
	}
	// [决策理由] 仅在覆盖与审计成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return PluginGroupOverride{}, fmt.Errorf("提交群覆盖事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. group100,version0 -> INSERT -> version1。
	// 2. group100,expected1,current2 -> ErrGroupControlConflict。
	return item, nil
}

// DeletePluginGroupOverride 按版本删除单群覆盖并写审计。
// @param ctx：事务上下文；actor：操作者；name/groupID：目标；version：期望版本。
// @returns 成功 nil，或不存在、冲突与数据库错误。
// ⚠️副作用说明：在同一 PostgreSQL 事务删除覆盖并写审计。
func (r *PostgresRepository) DeletePluginGroupOverride(ctx context.Context, actor Actor, name, groupID string, version int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	// [决策理由] 删除与审计必须原子提交。
	if err != nil {
		return fmt.Errorf("开启删除群覆盖事务: %w", err)
	}
	defer tx.Rollback(ctx)
	var before PluginGroupOverride
	err = tx.QueryRow(ctx, `DELETE FROM plugin_group_overrides WHERE plugin_name=$1 AND group_id=$2 AND version=$3 RETURNING group_id,enabled,version`, name, groupID, version).Scan(&before.GroupID, &before.Enabled, &before.Version)
	// [决策理由] 无影响行需要前端刷新，安全地统一为冲突。
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGroupControlConflict
	}
	// [决策理由] SQL 失败时不得写成功审计。
	if err != nil {
		return fmt.Errorf("删除群覆盖: %w", err)
	}
	beforeJSON, _ := json.Marshal(before)
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,success,request_id) VALUES($1,$2,$3,'plugin.group_override.delete','plugin_group_override',$4,$5,TRUE,NULLIF($6,''))`, actor.ID, actor.Role, actor.Channel, name+":"+groupID, beforeJSON, actor.RequestID)
	// [决策理由] 审计失败必须回滚删除。
	if err != nil {
		return fmt.Errorf("写入删除群覆盖审计: %w", err)
	}
	// [决策理由] 仅在删除与审计都成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交删除群覆盖事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. group100/v2 -> DELETE+审计 -> nil。
	// 2. group100/v1而当前v2 -> ErrGroupControlConflict。
	return nil
}
