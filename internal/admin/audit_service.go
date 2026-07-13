// 📌 影响范围：调用只读审计 Repository，并使用最高管理员授权快照。
package admin

import (
	"context"
	"fmt"
)

// ListAuditLogs 校验管理员和分页条件后查询审计日志。
// @param ctx：查询生命周期；actor：操作者；query：分页与筛选条件。
// @returns 审计分页结果或授权、参数、仓库错误。
// ⚠️副作用说明：读取管理员快照及审计 Repository。
func (s *Service) ListAuditLogs(ctx context.Context, actor Actor, query AuditQuery) (AuditPage, error) {
	// [决策理由] 审计日志包含敏感配置前后快照，必须验证最高管理员。
	if err := s.authorize(actor); err != nil {
		return AuditPage{}, err
	}
	// [决策理由] 页码必须为正且设置上限，避免整数偏移异常。
	if query.Page < 1 || query.Page > 1_000_000 {
		return AuditPage{}, fmt.Errorf("%w: page 必须在 1 至 1000000 之间", ErrInvalidAuditQuery)
	}
	// [决策理由] 单页限制控制数据库和响应内存开销。
	if query.PageSize < 1 || query.PageSize > 200 {
		return AuditPage{}, fmt.Errorf("%w: page_size 必须在 1 至 200 之间", ErrInvalidAuditQuery)
	}
	// [决策理由] 反向时间区间不可能匹配合理记录，应在查询前拒绝。
	if query.StartTime != nil && query.EndTime != nil && query.StartTime.After(*query.EndTime) {
		return AuditPage{}, fmt.Errorf("%w: start_time 不能晚于 end_time", ErrInvalidAuditQuery)
	}
	page, err := s.repository.ListAuditLogs(ctx, query)
	// [决策理由] 仓库失败时不能返回不完整分页结果。
	if err != nil {
		return AuditPage{}, fmt.Errorf("列出审计日志: %w", err)
	}

	// >>> 数据演变示例
	// 1. 管理员+page1,size20 -> Repository -> AuditPage。
	// 2. page_size=500 -> ErrInvalidAuditQuery -> 不查询数据库。
	return page, nil
}

// GetAuditLog 校验管理员后读取单条审计详情。
// @param ctx：查询生命周期；actor：操作者；id：审计日志 ID。
// @returns 审计记录或授权、未找到、仓库错误。
// ⚠️副作用说明：读取管理员快照及审计 Repository。
func (s *Service) GetAuditLog(ctx context.Context, actor Actor, id int64) (AuditState, error) {
	// [决策理由] 审计详情包含完整 JSON 前后快照，必须验证最高管理员。
	if err := s.authorize(actor); err != nil {
		return AuditState{}, err
	}
	// [决策理由] 非正数 ID 不可能对应 BIGSERIAL 主键。
	if id <= 0 {
		return AuditState{}, fmt.Errorf("%w: %d", ErrAuditNotFound, id)
	}
	state, err := s.repository.GetAuditLog(ctx, id)
	// [决策理由] 查询错误需保留服务语义并支持 errors.Is。
	if err != nil {
		return AuditState{}, fmt.Errorf("读取审计详情: %w", err)
	}

	// >>> 数据演变示例
	// 1. 管理员+id8 -> Repository -> AuditState。
	// 2. id0 -> ErrAuditNotFound -> 不查询数据库。
	return state, nil
}
