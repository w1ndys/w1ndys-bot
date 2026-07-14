// 📌 影响范围：只读查询 PostgreSQL admin_audit_logs 表；不修改审计记录。
package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const auditFilter = `
    WHERE ($1 = '' OR actor_id = $1)
      AND ($2 = '' OR action = $2)
      AND ($3 = '' OR target_type = $3)
      AND ($4 = '' OR target_id = $4)
      AND ($5::timestamptz IS NULL OR created_at >= $5)
      AND ($6::timestamptz IS NULL OR created_at <= $6)`

// ListAuditLogs 按筛选条件倒序返回一页审计日志。
// @param ctx：查询生命周期；query：已校验分页与筛选条件。
// @returns 审计分页结果或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读计数和分页查询。
func (r *PostgresRepository) ListAuditLogs(ctx context.Context, query AuditQuery) (AuditPage, error) {
	arguments := []any{query.ActorID, query.Action, query.TargetType, query.TargetID, query.StartTime, query.EndTime}
	var total int64
	// [决策理由] 总数和列表使用相同筛选条件，保证分页元数据一致。
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_logs `+auditFilter, arguments...).Scan(&total); err != nil {
		return AuditPage{}, fmt.Errorf("统计审计日志: %w", err)
	}
	offset := (query.Page - 1) * query.PageSize
	rows, err := r.pool.Query(ctx, `SELECT id,actor_id,actor_role,channel,action,target_type,target_id,NULL::jsonb,NULL::jsonb,success,COALESCE(error_message,''),COALESCE(request_id,''),created_at FROM admin_audit_logs `+auditFilter+` ORDER BY created_at DESC,id DESC LIMIT $7 OFFSET $8`, append(arguments, query.PageSize, offset)...)
	// [决策理由] 查询失败时不能返回与 total 不匹配的空页。
	if err != nil {
		return AuditPage{}, fmt.Errorf("查询审计日志: %w", err)
	}
	defer rows.Close()
	items := make([]AuditState, 0)
	for rows.Next() {
		var state AuditState
		// [决策理由] 任一行扫描失败会造成审计页面不完整，必须整体失败。
		if err := scanAudit(rows, &state); err != nil {
			return AuditPage{}, err
		}
		items = append(items, state)
	}
	// [决策理由] 迭代结束后仍需检查网络或协议错误。
	if err := rows.Err(); err != nil {
		return AuditPage{}, fmt.Errorf("遍历审计日志: %w", err)
	}
	page := AuditPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}

	// >>> 数据演变示例
	// 1. page=1,size=20,actor=100 -> COUNT+不读取快照的LIMIT20 OFFSET0 -> AuditPage。
	// 2. 无匹配记录 -> total=0,items=[] -> 空页。
	return page, nil
}

// GetAuditLog 按主键读取单条审计详情。
// @param ctx：查询生命周期；id：审计日志 ID。
// @returns 完整审计记录、未找到或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 单行只读查询。
func (r *PostgresRepository) GetAuditLog(ctx context.Context, id int64) (AuditState, error) {
	var state AuditState
	err := scanAudit(r.pool.QueryRow(ctx, `SELECT id,actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,COALESCE(error_message,''),COALESCE(request_id,''),created_at FROM admin_audit_logs WHERE id=$1`, id), &state)
	// [决策理由] pgx 无行错误需转换成稳定领域错误。
	if errors.Is(err, pgx.ErrNoRows) {
		return AuditState{}, fmt.Errorf("%w: %d", ErrAuditNotFound, id)
	}
	// [决策理由] 其他扫描或查询错误需要保留上下文供排障。
	if err != nil {
		return AuditState{}, fmt.Errorf("读取审计日志: %w", err)
	}

	// >>> 数据演变示例
	// 1. id=8存在 -> 扫描完整前后快照 -> state,nil。
	// 2. id=404 -> pgx.ErrNoRows -> ErrAuditNotFound。
	return state, nil
}

type auditScanner interface {
	Scan(...any) error
}

// scanAudit 扫描统一审计字段并复制 JSON 快照。
// @param scanner：pgx Row 或 Rows；target：接收审计状态。
// @returns 扫描错误。
// ⚠️副作用说明：修改 target 并复制其 JSON 字节。
func scanAudit(scanner auditScanner, target *AuditState) error {
	err := scanner.Scan(&target.ID, &target.ActorID, &target.ActorRole, &target.Channel, &target.Action, &target.TargetType, &target.TargetID, &target.BeforeJSON, &target.AfterJSON, &target.Success, &target.ErrorMessage, &target.RequestID, &target.CreatedAt)
	// [决策理由] 扫描失败时 target 不可信，直接返回原始错误供上层识别 ErrNoRows。
	if err != nil {
		return err
	}
	target.BeforeJSON = append(target.BeforeJSON[:0:0], target.BeforeJSON...)
	target.AfterJSON = append(target.AfterJSON[:0:0], target.AfterJSON...)

	// >>> 数据演变示例
	// 1. SQL十三列 -> target完整字段+独立JSON副本 -> nil。
	// 2. 无行或类型错误 -> Scan error -> target不可用。
	return nil
}
