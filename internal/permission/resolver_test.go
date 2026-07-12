// 📌 影响范围：仅测试内存权限快照；不访问数据库或外部变量。
package permission

import "testing"

// TestResolverFiveLevelPrecedence 验证五级权限覆盖顺序。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：替换测试 Resolver 的内存快照。
func TestResolverFiveLevelPrecedence(t *testing.T) {
	resolver := NewResolver(nil)
	policies := []Policy{
		{ScopeType: "global", ScopeID: "0", PluginName: "score", SubjectRole: RoleMember, Effect: EffectAllow},
		{ScopeType: "global", ScopeID: "0", PluginName: "score", FeatureKey: "reset", SubjectRole: RoleMember, Effect: EffectDeny},
		{ScopeType: "group", ScopeID: "123", PluginName: "score", SubjectRole: RoleMember, Effect: EffectDeny},
		{ScopeType: "group", ScopeID: "123", PluginName: "score", FeatureKey: "check_in", SubjectRole: RoleMember, Effect: EffectAllow},
	}
	// [决策理由] 合法策略快照必须成功发布后才能验证优先级。
	if err := resolver.Replace(policies); err != nil {
		t.Fatal(err)
	}
	defaults := Defaults{Member: false}
	// [决策理由] 群级功能 allow 应覆盖群级插件 deny。
	if !resolver.Allowed("123", "score", "check_in", RoleMember, defaults) {
		t.Fatal("群级功能 allow 未覆盖群级插件 deny")
	}
	// [决策理由] 没有群级功能规则时，群级插件 deny 应覆盖全局插件 allow。
	if resolver.Allowed("123", "score", "rank", RoleMember, defaults) {
		t.Fatal("群级插件 deny 未覆盖全局插件 allow")
	}
	// [决策理由] 其他群应使用全局功能 deny。
	if resolver.Allowed("456", "score", "reset", RoleMember, defaults) {
		t.Fatal("全局功能 deny 未生效")
	}
	// [决策理由] 无功能覆盖时应使用全局插件 allow。
	if !resolver.Allowed("456", "score", "rank", RoleMember, defaults) {
		t.Fatal("全局插件 allow 未生效")
	}

	// >>> 数据演变示例
	// 1. 群123 score.check_in -> 群功能 allow -> true。
	// 2. 群456 score.reset -> 全局功能 deny -> false。
}

// TestResolverFallsBackToDefaults 验证无显式策略时使用 Manifest 默认值。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestResolverFallsBackToDefaults(t *testing.T) {
	resolver := NewResolver(nil)
	defaults := Defaults{SuperAdmin: true, Member: false}
	// [决策理由] 无数据库规则时超级管理员使用默认允许。
	if !resolver.Allowed("123", "ping", "ping", RoleSuperAdmin, defaults) {
		t.Fatal("超级管理员默认权限错误")
	}
	// [决策理由] 无数据库规则时普通成员使用默认拒绝。
	if resolver.Allowed("123", "ping", "ping", RoleMember, defaults) {
		t.Fatal("普通成员默认权限错误")
	}

	// >>> 数据演变示例
	// 1. 无规则 + SuperAdmin=true -> true。
	// 2. 无规则 + Member=false -> false。
}
