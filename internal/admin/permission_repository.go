// 📌 影响范围：读写 PostgreSQL permission_policies 与 admin_audit_logs 表；开启权限管理事务。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ListPermissions 查询全部权限覆盖策略。
// @param ctx：控制数据库查询生命周期。
// @returns 按作用域和目标排序的权限策略，或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) ListPermissions(ctx context.Context) ([]PermissionState, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,scope_type,scope_id,plugin_name,COALESCE(feature_key,''),subject_type,subject_id,effect FROM permission_policies ORDER BY scope_type,scope_id,plugin_name,feature_key,subject_type,subject_id`)
	// [决策理由] 查询失败时无法提供完整权限快照。
	if err != nil {
		return nil, fmt.Errorf("查询权限策略: %w", err)
	}
	defer rows.Close()
	policies := make([]PermissionState, 0)
	for rows.Next() {
		var current PermissionState
		// [决策理由] 任一行异常都会使管理视图不完整。
		if err := rows.Scan(&current.ID, &current.ScopeType, &current.ScopeID, &current.PluginName, &current.FeatureKey, &current.SubjectType, &current.SubjectID, &current.Effect); err != nil {
			return nil, fmt.Errorf("扫描权限策略: %w", err)
		}
		policies = append(policies, current)
	}
	// [决策理由] 迭代完成后仍需检查连接和协议错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历权限策略: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB[global:ping:member:deny] -> []PermissionState{策略}。
	// 2. 空表 -> 空切片。
	return policies, nil
}

// SetPermission 新增或更新唯一维度的权限策略并记录审计。
// @param ctx：事务生命周期；actor：操作者；input：权限维度和效果。
// @returns 保存后的权限策略或事务错误。
// ⚠️副作用说明：插入或更新 permission_policies 并写入 admin_audit_logs。
func (r *PostgresRepository) SetPermission(ctx context.Context, actor Actor, input PermissionSet) (PermissionState, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 权限修改和审计必须原子提交。
	if err != nil {
		return PermissionState{}, fmt.Errorf("开启权限管理事务: %w", err)
	}
	defer tx.Rollback(ctx)
	lockKey := permissionLockKey(input)
	// [决策理由] 不存在行无法被 FOR UPDATE 锁定，维度级 advisory lock 防止并发新增撞唯一索引。
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return PermissionState{}, fmt.Errorf("锁定权限策略维度: %w", err)
	}
	before, found, err := selectPermissionByKey(ctx, tx, input)
	// [决策理由] 查询现状失败时不能安全决定新增或更新。
	if err != nil {
		return PermissionState{}, err
	}
	var after PermissionState
	// [决策理由] 已有唯一维度策略应更新效果，避免制造重复规则。
	if found {
		err = tx.QueryRow(ctx, `UPDATE permission_policies SET effect=$2,updated_by=$3,updated_at=NOW() WHERE id=$1 RETURNING id,scope_type,scope_id,plugin_name,COALESCE(feature_key,''),subject_type,subject_id,effect`, before.ID, input.Effect, actor.ID).Scan(&after.ID, &after.ScopeType, &after.ScopeID, &after.PluginName, &after.FeatureKey, &after.SubjectType, &after.SubjectID, &after.Effect)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO permission_policies(scope_type,scope_id,plugin_name,feature_key,subject_type,subject_id,effect,updated_by) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8) RETURNING id,scope_type,scope_id,plugin_name,COALESCE(feature_key,''),subject_type,subject_id,effect`, input.ScopeType, input.ScopeID, input.PluginName, input.FeatureKey, input.SubjectType, input.SubjectID, input.Effect, actor.ID).Scan(&after.ID, &after.ScopeType, &after.ScopeID, &after.PluginName, &after.FeatureKey, &after.SubjectType, &after.SubjectID, &after.Effect)
	}
	// [决策理由] 数据库约束或外键失败时不得写入成功审计。
	if err != nil {
		var databaseError *pgconn.PgError
		// [决策理由] 插件或功能外键错误应转换为稳定领域错误供 WebUI 精确提示。
		if errors.As(err, &databaseError) {
			switch databaseError.ConstraintName {
			case "fk_permission_plugin":
				return PermissionState{}, ErrPluginNotFound
			case "fk_permission_feature":
				return PermissionState{}, ErrFeatureNotFound
			}
		}
		return PermissionState{}, fmt.Errorf("保存权限策略: %w", err)
	}
	var beforeValue any
	// [决策理由] 新增策略没有 before 快照，应在审计中保存 NULL。
	if found {
		beforeValue = before
	}
	// [决策理由] 审计必须记录新增或更新前后的完整策略。
	if err := insertPermissionAudit(ctx, tx, actor, "permission.set", after.ID, beforeValue, after); err != nil {
		return PermissionState{}, err
	}
	// [决策理由] 策略和审计均成功后再提交。
	if err := tx.Commit(ctx); err != nil {
		return PermissionState{}, fmt.Errorf("提交权限管理事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. 无member策略 + deny -> INSERT+audit -> 返回新ID。
	// 2. 已有member:allow + deny -> UPDATE同一ID+audit -> deny。
	return after, nil
}

// permissionLockKey 将权限唯一维度编码为PostgreSQL可接受且无歧义的锁键。
// @param input：权限策略唯一维度。
// @returns 不含NUL字节的JSON数组字符串。
// ⚠️副作用说明：分配JSON编码字节，不访问数据库。
func permissionLockKey(input PermissionSet) string {
	dimensions := [...]string{input.ScopeType, input.ScopeID, input.PluginName, input.FeatureKey, input.SubjectType, input.SubjectID}
	encoded, err := json.Marshal(dimensions)
	// [决策理由] 固定字符串数组理论上不会编码失败；安全回退仍需保持可打印且维度有边界。
	if err != nil {
		return fmt.Sprintf("%q", dimensions)
	}
	result := string(encoded)

	// >>> 数据演变示例
	// 1. global,0,ping,"",role,member -> JSON数组 -> 无NUL稳定锁键。
	// 2. 字段分别为"a,b"与"a","b" -> JSON边界不同 -> 不会锁键碰撞。
	return result
}

// DeletePermission 删除权限策略并记录删除前快照。
// @param ctx：事务生命周期；actor：操作者；id：权限策略 ID。
// @returns 未找到或事务错误。
// ⚠️副作用说明：删除 permission_policies 并写入 admin_audit_logs。
func (r *PostgresRepository) DeletePermission(ctx context.Context, actor Actor, id int64) error {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 删除和审计必须在同一事务提交。
	if err != nil {
		return fmt.Errorf("开启删除权限事务: %w", err)
	}
	defer tx.Rollback(ctx)
	before, err := selectPermissionByID(ctx, tx, id)
	// [决策理由] 不存在的策略不能形成有效删除审计。
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM permission_policies WHERE id=$1`, id)
	// [决策理由] 删除失败时不得记录成功审计。
	if err != nil {
		return fmt.Errorf("删除权限策略: %w", err)
	}
	// [决策理由] 删除审计保留完整 before 快照并使用 NULL after。
	if err := insertPermissionAudit(ctx, tx, actor, "permission.delete", id, before, nil); err != nil {
		return err
	}
	// [决策理由] 删除和审计均成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交删除权限事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. id=3存在 -> DELETE+before审计 -> nil。
	// 2. id=404 -> ErrPermissionNotFound -> 回滚。
	return nil
}

// selectPermissionByKey 锁定唯一维度对应的权限策略。
// @param ctx：查询生命周期；tx：活动事务；input：唯一权限维度。
// @returns 策略、是否存在及查询错误。
// ⚠️副作用说明：存在时对目标行加锁至事务结束。
func selectPermissionByKey(ctx context.Context, tx pgx.Tx, input PermissionSet) (PermissionState, bool, error) {
	var current PermissionState
	err := tx.QueryRow(ctx, `SELECT id,scope_type,scope_id,plugin_name,COALESCE(feature_key,''),subject_type,subject_id,effect FROM permission_policies WHERE scope_type=$1 AND scope_id=$2 AND plugin_name=$3 AND feature_key IS NOT DISTINCT FROM NULLIF($4,'') AND subject_type=$5 AND subject_id=$6 FOR UPDATE`, input.ScopeType, input.ScopeID, input.PluginName, input.FeatureKey, input.SubjectType, input.SubjectID).Scan(&current.ID, &current.ScopeType, &current.ScopeID, &current.PluginName, &current.FeatureKey, &current.SubjectType, &current.SubjectID, &current.Effect)
	// [决策理由] 无行表示本次操作应创建新策略，不属于错误。
	if errors.Is(err, pgx.ErrNoRows) {
		return PermissionState{}, false, nil
	}
	// [决策理由] 其他查询错误需要保留数据库上下文。
	if err != nil {
		return PermissionState{}, false, fmt.Errorf("查询现有权限策略: %w", err)
	}

	// >>> 数据演变示例
	// 1. 唯一维度已存在 -> FOR UPDATE -> state,true,nil。
	// 2. 唯一维度不存在 -> ErrNoRows -> zero,false,nil。
	return current, true, nil
}

// selectPermissionByID 锁定并读取指定权限策略。
// @param ctx：查询生命周期；tx：活动事务；id：权限策略 ID。
// @returns 权限策略或未找到、数据库错误。
// ⚠️副作用说明：对目标行加锁至事务结束。
func selectPermissionByID(ctx context.Context, tx pgx.Tx, id int64) (PermissionState, error) {
	var current PermissionState
	err := tx.QueryRow(ctx, `SELECT id,scope_type,scope_id,plugin_name,COALESCE(feature_key,''),subject_type,subject_id,effect FROM permission_policies WHERE id=$1 FOR UPDATE`, id).Scan(&current.ID, &current.ScopeType, &current.ScopeID, &current.PluginName, &current.FeatureKey, &current.SubjectType, &current.SubjectID, &current.Effect)
	// [决策理由] 无行错误应转换为稳定领域错误。
	if errors.Is(err, pgx.ErrNoRows) {
		return PermissionState{}, fmt.Errorf("%w: %d", ErrPermissionNotFound, id)
	}
	// [决策理由] 其他查询错误需要保留数据库上下文。
	if err != nil {
		return PermissionState{}, fmt.Errorf("读取权限策略: %w", err)
	}

	// >>> 数据演变示例
	// 1. id=3 -> 锁定 -> 返回策略。
	// 2. id=404 -> ErrPermissionNotFound。
	return current, nil
}

// insertPermissionAudit 写入权限事务审计快照。
// @param ctx：事务生命周期；tx：活动事务；actor：操作者；action：动作；id：策略 ID；before、after：前后状态。
// @returns JSON 编码或数据库错误。
// ⚠️副作用说明：向 admin_audit_logs 插入一行。
func insertPermissionAudit(ctx context.Context, tx pgx.Tx, actor Actor, action string, id int64, before any, after any) error {
	beforeJSON, err := json.Marshal(before)
	// [决策理由] 前快照编码失败时不能提交权限变更。
	if err != nil {
		return fmt.Errorf("编码权限变更前快照: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	// [决策理由] 后快照编码失败时不能提交权限变更。
	if err != nil {
		return fmt.Errorf("编码权限变更后快照: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,$4,'permission',$5,NULLIF($6,'null')::jsonb,NULLIF($7,'null')::jsonb,TRUE,NULLIF($8,''))`, actor.ID, actor.Role, actor.Channel, action, fmt.Sprintf("%d", id), string(beforeJSON), string(afterJSON), actor.RequestID)
	// [决策理由] 审计写入失败必须让调用方回滚权限事务。
	if err != nil {
		return fmt.Errorf("写入权限管理审计: %w", err)
	}

	// >>> 数据演变示例
	// 1. allow->deny -> before/after JSONB -> audit。
	// 2. delete -> after=nil -> after_json=NULL。
	return nil
}
