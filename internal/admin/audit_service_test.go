// 📌 影响范围：无；使用内存管理仓库替身验证审计查询授权与参数，不访问数据库。
package admin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestListAuditLogsValidatesPaginationAndTimeRange 验证审计查询边界在仓库前被拒绝。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestListAuditLogsValidatesPaginationAndTimeRange(t *testing.T) {
	service := NewService(&fakeRepository{}, nil, nil, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.ListAuditLogs(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, AuditQuery{Page: 1, PageSize: 201})
	// [决策理由] 超过单页上限必须返回稳定参数错误。
	if !errors.Is(err, ErrInvalidAuditQuery) {
		t.Fatalf("page size error = %v", err)
	}
	start := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	end := start.Add(-time.Hour)
	_, err = service.ListAuditLogs(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, AuditQuery{Page: 1, PageSize: 20, StartTime: &start, EndTime: &end})
	// [决策理由] 反向时间区间必须在查询前拒绝。
	if !errors.Is(err, ErrInvalidAuditQuery) {
		t.Fatalf("time range error = %v", err)
	}

	// >>> 数据演变示例
	// 1. page_size=201 -> ErrInvalidAuditQuery。
	// 2. start>end -> ErrInvalidAuditQuery。
}

// TestGetAuditLogRequiresSuperAdmin 验证审计详情不可由非管理员读取。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存授权替身并可能终止当前测试。
func TestGetAuditLogRequiresSuperAdmin(t *testing.T) {
	service := NewService(&fakeRepository{}, nil, nil, nil, nil, &fakeAuthorizer{})
	_, err := service.GetAuditLog(context.Background(), Actor{ID: "200", Channel: ChannelWebUI}, 8)
	// [决策理由] 非最高管理员必须被统一授权层拒绝。
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("GetAuditLog() error = %v, want ErrForbidden", err)
	}

	// >>> 数据演变示例
	// 1. 非管理员200+id8 -> ErrForbidden。
	// 2. 管理员100+id8 -> 可进入Repository。
}
