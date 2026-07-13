// 📌 影响范围：读写 PostgreSQL system_admins 与 admin_audit_logs 表；使用事务级 advisory lock 串行化管理员变更。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const systemAdminLockKey int64 = 0x77316e647973

// CreateSystemAdmin 新增启用的最高管理员并记录审计。
// @param ctx：事务生命周期；actor：操作者；input：新管理员账号和备注。
// @returns 新管理员状态或重复、事务错误。
// ⚠️副作用说明：插入 system_admins 与 admin_audit_logs。
func (r *PostgresRepository) CreateSystemAdmin(ctx context.Context, actor Actor, input AdminCreate) (SystemAdmin, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 管理员创建和审计必须原子提交。
	if err != nil {
		return SystemAdmin{}, fmt.Errorf("开启新增管理员事务: %w", err)
	}
	defer tx.Rollback(ctx)
	// [决策理由] 所有管理员写操作共用事务锁，避免与并发禁用或删除发生竞态。
	if err := lockSystemAdmins(ctx, tx); err != nil {
		return SystemAdmin{}, err
	}
	var created SystemAdmin
	err = tx.QueryRow(ctx, `INSERT INTO system_admins(user_id,nickname,enabled,created_by) VALUES($1,$2,TRUE,$3) RETURNING user_id,nickname,enabled`, input.UserID, input.Nickname, actor.ID).Scan(&created.UserID, &created.Nickname, &created.Enabled)
	// [决策理由] 主键冲突应转换为稳定领域错误。
	if commandConflict(err) {
		return SystemAdmin{}, ErrAdminConflict
	}
	// [决策理由] 其他写入错误需保留数据库上下文。
	if err != nil {
		return SystemAdmin{}, fmt.Errorf("新增最高管理员: %w", err)
	}
	// [决策理由] 审计失败时不得留下未审计管理员。
	if err := insertSystemAdminAudit(ctx, tx, actor, "admin.create", created.UserID, nil, created); err != nil {
		return SystemAdmin{}, err
	}
	// [决策理由] 账号与审计均成功后再提交。
	if err := tx.Commit(ctx); err != nil {
		return SystemAdmin{}, fmt.Errorf("提交新增管理员事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. 新QQ=200 -> INSERT enabled=true + audit -> 返回200。
	// 2. QQ已存在 -> 23505 -> ErrAdminConflict并回滚。
	return created, nil
}

// UpdateSystemAdmin 修改管理员备注或启用状态并保护最后一个启用账号。
// @param ctx：事务生命周期；actor：操作者；userID：目标 QQ；patch：可选字段。
// @returns 更新后管理员或未找到、最后管理员、事务错误。
// ⚠️副作用说明：更新 system_admins 并写入 admin_audit_logs。
func (r *PostgresRepository) UpdateSystemAdmin(ctx context.Context, actor Actor, userID string, patch AdminPatch) (SystemAdmin, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 管理员修改和审计必须原子提交。
	if err != nil {
		return SystemAdmin{}, fmt.Errorf("开启修改管理员事务: %w", err)
	}
	defer tx.Rollback(ctx)
	// [决策理由] 串行化管理员写操作，确保最后管理员计数稳定。
	if err := lockSystemAdmins(ctx, tx); err != nil {
		return SystemAdmin{}, err
	}
	before, err := selectSystemAdmin(ctx, tx, userID)
	// [决策理由] 不存在账号不能生成修改审计。
	if err != nil {
		return SystemAdmin{}, err
	}
	after := before
	// [决策理由] nil 备注表示调用方未修改此字段。
	if patch.Nickname != nil {
		after.Nickname = *patch.Nickname
	}
	// [决策理由] nil 启用状态表示调用方未修改此字段。
	if patch.Enabled != nil {
		after.Enabled = *patch.Enabled
	}
	// [决策理由] 从启用变为禁用前必须保证仍有其他启用管理员。
	if before.Enabled && !after.Enabled {
		if err := ensureAnotherEnabledAdmin(ctx, tx, userID); err != nil {
			return SystemAdmin{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE system_admins SET nickname=$2,enabled=$3,updated_at=NOW() WHERE user_id=$1`, userID, after.Nickname, after.Enabled)
	// [决策理由] 更新失败时不得记录成功审计。
	if err != nil {
		return SystemAdmin{}, fmt.Errorf("修改最高管理员: %w", err)
	}
	// [决策理由] 审计需要保留前后完整状态。
	if err := insertSystemAdminAudit(ctx, tx, actor, "admin.update", userID, before, after); err != nil {
		return SystemAdmin{}, err
	}
	// [决策理由] 修改和审计均成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return SystemAdmin{}, fmt.Errorf("提交修改管理员事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. 200:true + Enabled=false且另有100 -> 更新false + audit。
	// 2. 唯一100:true + Enabled=false -> ErrLastEnabledAdmin并回滚。
	return after, nil
}

// DeleteSystemAdmin 删除管理员并保护最后一个启用账号。
// @param ctx：事务生命周期；actor：操作者；userID：目标 QQ。
// @returns 未找到、最后管理员或事务错误。
// ⚠️副作用说明：删除 system_admins 并写入 admin_audit_logs。
func (r *PostgresRepository) DeleteSystemAdmin(ctx context.Context, actor Actor, userID string) error {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 管理员删除和审计必须原子提交。
	if err != nil {
		return fmt.Errorf("开启删除管理员事务: %w", err)
	}
	defer tx.Rollback(ctx)
	// [决策理由] 串行化管理员写操作，防止并发删除绕过最后账号保护。
	if err := lockSystemAdmins(ctx, tx); err != nil {
		return err
	}
	before, err := selectSystemAdmin(ctx, tx, userID)
	// [决策理由] 不存在账号不能形成有效删除审计。
	if err != nil {
		return err
	}
	// [决策理由] 删除启用账号前必须保证另有可用最高管理员。
	if before.Enabled {
		if err := ensureAnotherEnabledAdmin(ctx, tx, userID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM system_admins WHERE user_id=$1`, userID)
	// [决策理由] 删除失败时不得记录成功审计。
	if err != nil {
		return fmt.Errorf("删除最高管理员: %w", err)
	}
	// [决策理由] 删除审计保存完整 before 和 NULL after。
	if err := insertSystemAdminAudit(ctx, tx, actor, "admin.delete", userID, before, nil); err != nil {
		return err
	}
	// [决策理由] 删除和审计均成功后提交。
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交删除管理员事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. 删除禁用200 -> DELETE+audit -> nil。
	// 2. 删除唯一启用100 -> ErrLastEnabledAdmin -> 回滚。
	return nil
}

// lockSystemAdmins 获取事务级管理员写锁。
// @param ctx：事务生命周期；tx：活动事务。
// @returns PostgreSQL advisory lock 错误。
// ⚠️副作用说明：持有事务级 advisory lock 直至事务结束。
func lockSystemAdmins(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, systemAdminLockKey)
	// [决策理由] 未取得锁时不能安全执行最后管理员判断。
	if err != nil {
		return fmt.Errorf("锁定最高管理员变更: %w", err)
	}

	// >>> 数据演变示例
	// 1. 无并发事务 -> 获得锁 -> nil。
	// 2. 另一事务持锁 -> 等待或ctx取消 -> error。
	return nil
}

// selectSystemAdmin 锁定并读取目标管理员。
// @param ctx：查询生命周期；tx：活动事务；userID：目标 QQ。
// @returns 管理员状态或未找到、数据库错误。
// ⚠️副作用说明：对目标管理员行加锁至事务结束。
func selectSystemAdmin(ctx context.Context, tx pgx.Tx, userID string) (SystemAdmin, error) {
	var current SystemAdmin
	err := tx.QueryRow(ctx, `SELECT user_id,nickname,enabled FROM system_admins WHERE user_id=$1 FOR UPDATE`, userID).Scan(&current.UserID, &current.Nickname, &current.Enabled)
	// [决策理由] 无行错误应转换为稳定领域错误。
	if errors.Is(err, pgx.ErrNoRows) {
		return SystemAdmin{}, fmt.Errorf("%w: %s", ErrAdminNotFound, userID)
	}
	// [决策理由] 其他查询错误需要保留数据库上下文。
	if err != nil {
		return SystemAdmin{}, fmt.Errorf("读取最高管理员: %w", err)
	}

	// >>> 数据演变示例
	// 1. userID=100存在 -> FOR UPDATE -> 返回状态。
	// 2. userID=404 -> ErrAdminNotFound。
	return current, nil
}

// ensureAnotherEnabledAdmin 确认目标之外仍有启用管理员。
// @param ctx：查询生命周期；tx：活动事务；excludedUserID：即将禁用或删除的 QQ。
// @returns 存在其他启用管理员时 nil，否则 ErrLastEnabledAdmin。
// ⚠️副作用说明：读取 system_admins；依赖调用方已持有管理员 advisory lock。
func ensureAnotherEnabledAdmin(ctx context.Context, tx pgx.Tx, excludedUserID string) error {
	var count int
	err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM system_admins WHERE enabled=TRUE AND user_id<>$1`, excludedUserID).Scan(&count)
	// [决策理由] 计数查询失败时不能假设存在安全备份管理员。
	if err != nil {
		return fmt.Errorf("统计启用最高管理员: %w", err)
	}
	// [决策理由] 零个其他启用账号会造成系统永久失去管理入口。
	if count == 0 {
		return ErrLastEnabledAdmin
	}

	// >>> 数据演变示例
	// 1. excluded=100且200启用 -> count=1 -> nil。
	// 2. 仅100启用 -> count=0 -> ErrLastEnabledAdmin。
	return nil
}

// insertSystemAdminAudit 写入管理员事务审计快照。
// @param ctx：事务生命周期；tx：活动事务；actor：操作者；action：动作；userID：目标 QQ；before、after：前后状态。
// @returns JSON 编码或数据库错误。
// ⚠️副作用说明：向 admin_audit_logs 插入一行。
func insertSystemAdminAudit(ctx context.Context, tx pgx.Tx, actor Actor, action string, userID string, before any, after any) error {
	beforeJSON, err := json.Marshal(before)
	// [决策理由] 前快照编码失败时不能提交管理员变更。
	if err != nil {
		return fmt.Errorf("编码管理员变更前快照: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	// [决策理由] 后快照编码失败时不能提交管理员变更。
	if err != nil {
		return fmt.Errorf("编码管理员变更后快照: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,$4,'system_admin',$5,NULLIF($6,'null')::jsonb,NULLIF($7,'null')::jsonb,TRUE,NULLIF($8,''))`, actor.ID, actor.Role, actor.Channel, action, userID, string(beforeJSON), string(afterJSON), actor.RequestID)
	// [决策理由] 审计失败必须让调用方回滚整个管理员事务。
	if err != nil {
		return fmt.Errorf("写入管理员审计: %w", err)
	}

	// >>> 数据演变示例
	// 1. admin.update + before/after -> 两份JSONB审计。
	// 2. admin.delete + after=nil -> after_json=NULL。
	return nil
}
