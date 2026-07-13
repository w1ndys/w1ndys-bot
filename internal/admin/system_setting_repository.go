// 📌 影响范围：读写 PostgreSQL system_settings 与 admin_audit_logs 表；开启系统设置管理事务。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ListSystemSettings 查询全部数据库系统设置。
// @param ctx：控制数据库查询生命周期。
// @returns 按键排序的设置列表或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) ListSystemSettings(ctx context.Context) ([]SettingState, error) {
	rows, err := r.pool.Query(ctx, `SELECT setting_key,setting_value,description FROM system_settings ORDER BY setting_key`)
	// [决策理由] 查询失败时无法构建可信完整快照。
	if err != nil {
		return nil, fmt.Errorf("查询系统设置: %w", err)
	}
	defer rows.Close()
	settings := make([]SettingState, 0)
	for rows.Next() {
		var current SettingState
		// [决策理由] 任一行异常都会使设置快照不完整。
		if err := rows.Scan(&current.Key, &current.Value, &current.Description); err != nil {
			return nil, fmt.Errorf("扫描系统设置: %w", err)
		}
		current.Value = append(json.RawMessage(nil), current.Value...)
		current.Overridden = true
		settings = append(settings, current)
	}
	// [决策理由] 迭代完成后仍需检查连接和协议错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历系统设置: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB command_prefix="!" -> []SettingState{prefix}。
	// 2. 空表 -> 空切片。
	return settings, nil
}

// SetSystemSetting 新增或更新设置并记录前后快照。
// @param ctx：事务生命周期；actor：操作者；setting：已校验设置。
// @returns 保存后设置或事务错误。
// ⚠️副作用说明：写入 system_settings 与 admin_audit_logs。
func (r *PostgresRepository) SetSystemSetting(ctx context.Context, actor Actor, setting SettingState) (SettingState, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 设置和审计必须原子提交。
	if err != nil {
		return SettingState{}, fmt.Errorf("开启系统设置事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, found, err := selectSystemSetting(ctx, tx, setting.Key)
	// [决策理由] 读取现状失败时不能形成可信审计前快照。
	if err != nil {
		return SettingState{}, err
	}
	var saved SettingState
	err = tx.QueryRow(ctx, `INSERT INTO system_settings(setting_key,setting_value,description) VALUES($1,$2,$3) ON CONFLICT(setting_key) DO UPDATE SET setting_value=EXCLUDED.setting_value,description=EXCLUDED.description,updated_at=NOW() RETURNING setting_key,setting_value,description`, setting.Key, setting.Value, setting.Description).Scan(&saved.Key, &saved.Value, &saved.Description)
	// [决策理由] UPSERT 失败时不得写入成功审计。
	if err != nil {
		return SettingState{}, fmt.Errorf("保存系统设置: %w", err)
	}
	saved.Overridden = true
	var beforeValue any
	// [决策理由] 新设置没有 before 快照，数据库审计应保存 NULL。
	if found {
		beforeValue = before
	}
	// [决策理由] 审计必须记录设置前后值。
	if err := insertSystemSettingAudit(ctx, tx, actor, "setting.set", saved.Key, beforeValue, saved); err != nil {
		return SettingState{}, err
	}
	// [决策理由] 设置和审计均成功后再提交。
	if err := tx.Commit(ctx); err != nil {
		return SettingState{}, fmt.Errorf("提交系统设置事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. 无prefix + "!" -> INSERT+audit -> 返回!。
	// 2. prefix="/" + "!" -> UPDATE+before/after audit -> 返回!。
	return saved, nil
}

// DeleteSystemSetting 删除数据库覆盖并记录删除前快照。
// @param ctx：事务生命周期；actor：操作者；key：设置键。
// @returns 未找到或事务错误。
// ⚠️副作用说明：删除 system_settings 并写入 admin_audit_logs。
func (r *PostgresRepository) DeleteSystemSetting(ctx context.Context, actor Actor, key string) error {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 删除设置和审计必须原子提交。
	if err != nil {
		return fmt.Errorf("开启删除系统设置事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, found, err := selectSystemSetting(ctx, tx, key)
	// [决策理由] 查询错误时不能继续删除。
	if err != nil {
		return err
	}
	// [决策理由] 不存在的数据库覆盖必须返回稳定未找到错误。
	if !found {
		return fmt.Errorf("%w: %s", ErrSettingNotFound, key)
	}
	_, err = tx.Exec(ctx, `DELETE FROM system_settings WHERE setting_key=$1`, key)
	// [决策理由] 删除失败时不得记录成功审计。
	if err != nil {
		return fmt.Errorf("删除系统设置: %w", err)
	}
	// [决策理由] 删除审计保存 before 并使用 NULL after。
	if err := insertSystemSettingAudit(ctx, tx, actor, "setting.delete", key, before, nil); err != nil {
		return err
	}
	// [决策理由] 删除和审计均成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交删除系统设置事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. 删除prefix覆盖 -> DELETE+audit -> Resolver回退默认/。
	// 2. key不存在 -> ErrSettingNotFound -> 回滚。
	return nil
}

// selectSystemSetting 锁定并读取一个数据库设置。
// @param ctx：查询生命周期；tx：活动事务；key：设置键。
// @returns 设置、是否存在及数据库错误。
// ⚠️副作用说明：存在时对目标行加锁至事务结束。
func selectSystemSetting(ctx context.Context, tx pgx.Tx, key string) (SettingState, bool, error) {
	var current SettingState
	err := tx.QueryRow(ctx, `SELECT setting_key,setting_value,description FROM system_settings WHERE setting_key=$1 FOR UPDATE`, key).Scan(&current.Key, &current.Value, &current.Description)
	// [决策理由] 无行表示没有数据库覆盖，不属于查询错误。
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingState{}, false, nil
	}
	// [决策理由] 其他查询错误需要保留数据库上下文。
	if err != nil {
		return SettingState{}, false, fmt.Errorf("读取系统设置: %w", err)
	}

	// >>> 数据演变示例
	// 1. key=command_prefix存在 -> state,true,nil。
	// 2. key=missing -> zero,false,nil。
	return current, true, nil
}

// insertSystemSettingAudit 写入设置事务审计快照。
// @param ctx：事务生命周期；tx：活动事务；actor：操作者；action：动作；key：设置键；before、after：前后状态。
// @returns JSON 编码或数据库错误。
// ⚠️副作用说明：向 admin_audit_logs 插入一行。
func insertSystemSettingAudit(ctx context.Context, tx pgx.Tx, actor Actor, action string, key string, before any, after any) error {
	beforeJSON, err := json.Marshal(before)
	// [决策理由] 前快照编码失败时不能提交设置变更。
	if err != nil {
		return fmt.Errorf("编码设置变更前快照: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	// [决策理由] 后快照编码失败时不能提交设置变更。
	if err != nil {
		return fmt.Errorf("编码设置变更后快照: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,$4,'system_setting',$5,NULLIF($6,'null')::jsonb,NULLIF($7,'null')::jsonb,TRUE,NULLIF($8,''))`, actor.ID, actor.Role, actor.Channel, action, key, string(beforeJSON), string(afterJSON), actor.RequestID)
	// [决策理由] 审计写入失败必须让调用方回滚整个设置事务。
	if err != nil {
		return fmt.Errorf("写入系统设置审计: %w", err)
	}

	// >>> 数据演变示例
	// 1. setting.set + before/after -> 两份JSONB审计。
	// 2. setting.delete + after=nil -> after_json=NULL。
	return nil
}
