// 📌 影响范围：无；验证 SUPER_ADMIN_QQ 单管理员身份解析，不访问数据库。
package admin

import "testing"

// TestAdminResolverUsesSingleConfiguredAccount 验证仅环境配置账号获得最高权限。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestAdminResolverUsesSingleConfiguredAccount(t *testing.T) {
	resolver := NewAdminResolver(" 100 ")
	// [决策理由] 配置中的首尾空格应被清理，避免 Compose 格式细节导致无法登录。
	if !resolver.IsSuperAdmin("100") {
		t.Fatal("IsSuperAdmin(100) = false")
	}
	// [决策理由] 单管理员模式必须拒绝任何其他来源账号。
	if resolver.IsSuperAdmin("200") {
		t.Fatal("IsSuperAdmin(200) = true")
	}
	account, exists := resolver.Resolve("100")
	// [决策理由] WebUI 需要得到稳定的启用身份详情。
	if !exists || account.UserID != "100" || !account.Enabled {
		t.Fatalf("Resolve(100) = %+v,%v", account, exists)
	}

	// >>> 数据演变示例
	// 1. 配置100 -> IsSuperAdmin(100) -> true。
	// 2. 配置100 -> IsSuperAdmin(200) -> false。
}

// TestAdminResolverRejectsEmptyConfiguration 验证缺少环境管理员时不授权空账号。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestAdminResolverRejectsEmptyConfiguration(t *testing.T) {
	resolver := NewAdminResolver("")
	// [决策理由] 空配置和空输入相同也不能视为有效身份。
	if resolver.IsSuperAdmin("") {
		t.Fatal("IsSuperAdmin(empty) = true")
	}
	_, exists := resolver.Resolve("100")
	// [决策理由] 未配置时任意真实 QQ 都必须被拒绝。
	if exists {
		t.Fatal("Resolve(100) exists = true")
	}

	// >>> 数据演变示例
	// 1. 配置空+输入空 -> false。
	// 2. 配置空+输入100 -> 未找到。
}
