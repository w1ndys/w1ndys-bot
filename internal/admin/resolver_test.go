// 📌 影响范围：无；使用内存仓库验证最高管理员快照和并发读取。
package admin

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeAdminRepository struct {
	admins []SystemAdmin
	err    error
}

// ListSystemAdmins 返回测试预设管理员数据。
// @param ctx：未使用的测试上下文。
// @returns 预设管理员列表或错误。
// ⚠️副作用说明：无。
func (f *fakeAdminRepository) ListSystemAdmins(_ context.Context) ([]SystemAdmin, error) {
	// >>> 数据演变示例
	// 1. admins=[100] -> 返回 [100],nil。
	// 2. err=boom -> 返回 nil,boom。
	return f.admins, f.err
}

// TestAdminResolverLoadsOnlyEnabledAccounts 验证快照只授权启用且有效的 QQ 号。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存仓库并可能终止当前测试。
func TestAdminResolverLoadsOnlyEnabledAccounts(t *testing.T) {
	repository := &fakeAdminRepository{admins: []SystemAdmin{{UserID: "100", Nickname: "卷卷", Enabled: true}, {UserID: "200", Enabled: false}, {UserID: "", Enabled: true}}}
	resolver := NewAdminResolver(repository)
	err := resolver.Load(context.Background())
	// [决策理由] 有效仓库数据必须成功发布后才能检查权限结果。
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// [决策理由] 启用且具有 QQ 号的账号必须被识别为最高管理员。
	if !resolver.IsSuperAdmin("100") {
		t.Fatal("IsSuperAdmin(100) = false")
	}
	// [决策理由] 数据库禁用账号必须立即失去授权。
	if resolver.IsSuperAdmin("200") {
		t.Fatal("IsSuperAdmin(200) = true")
	}
	account, exists := resolver.Resolve("100")
	// [决策理由] Resolve 应返回身份详情供管理入口构造审计 Actor。
	if !exists || account.Nickname != "卷卷" {
		t.Fatalf("Resolve(100) = %+v,%v", account, exists)
	}

	// >>> 数据演变示例
	// 1. 100:true -> Load -> IsSuperAdmin=true。
	// 2. 200:false或空ID -> Load过滤 -> IsSuperAdmin=false。
}

// TestAdminResolverKeepsSnapshotWhenReloadFails 验证刷新失败不会清空已授权身份。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改测试仓库预设并可能终止当前测试。
func TestAdminResolverKeepsSnapshotWhenReloadFails(t *testing.T) {
	repository := &fakeAdminRepository{admins: []SystemAdmin{{UserID: "100", Enabled: true}}}
	resolver := NewAdminResolver(repository)
	// [决策理由] 初始快照加载失败会使后续保留语义无法验证。
	if err := resolver.Load(context.Background()); err != nil {
		t.Fatalf("initial Load() error = %v", err)
	}
	repository.err = errors.New("database unavailable")
	err := resolver.Load(context.Background())
	// [决策理由] 仓库错误必须向调用者显式报告以便告警和重试。
	if err == nil {
		t.Fatal("reload error = nil")
	}
	// [决策理由] 瞬时数据库故障不应撤销上一份已验证的管理员权限。
	if !resolver.IsSuperAdmin("100") {
		t.Fatal("previous snapshot was cleared")
	}

	// >>> 数据演变示例
	// 1. snapshot{100} + DB失败 -> 返回错误 -> snapshot仍含100。
	// 2. 初次成功后重载失败 -> 授权服务持续可读。
}

// TestAdminResolverSupportsConcurrentReadsAndReload 验证原子快照可并发读取和替换。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动并等待测试 goroutine。
func TestAdminResolverSupportsConcurrentReadsAndReload(t *testing.T) {
	repository := &fakeAdminRepository{admins: []SystemAdmin{{UserID: "100", Enabled: true}}}
	resolver := NewAdminResolver(repository)
	// [决策理由] 并发测试需要先发布一份有效初始快照。
	if err := resolver.Load(context.Background()); err != nil {
		t.Fatalf("initial Load() error = %v", err)
	}
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				resolver.IsSuperAdmin("100")
			}
		}()
	}
	// [决策理由] 发布全新 map 而不修改旧 map，保证读者不会观察到中间状态。
	if err := resolver.Load(context.Background()); err != nil {
		t.Fatalf("reload error = %v", err)
	}
	group.Wait()

	// >>> 数据演变示例
	// 1. 32读者 + Load -> 原子替换指针 -> 无数据竞争。
	// 2. 旧读者持旧快照 + 新读者取新快照 -> 两者均为完整map。
}

// TestNumericUserID 验证最高管理员引导只接受纯数字 QQ 号。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestNumericUserID(t *testing.T) {
	cases := map[string]bool{"123456789": true, "": false, "123 abc": false, "+123": false, "１２３": false}
	for input, expected := range cases {
		actual := numericUserID(input)
		// [决策理由] 每类输入都必须符合严格 ASCII QQ 号约束。
		if actual != expected {
			t.Fatalf("numericUserID(%q) = %v, want %v", input, actual, expected)
		}
	}

	// >>> 数据演变示例
	// 1. "123456789" -> ASCII数字扫描 -> true。
	// 2. "+123" -> 遇到加号 -> false。
}
