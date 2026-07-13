// 📌 影响范围：仅测试内存权限快照；不访问数据库或外部变量。
package permission

import "testing"

// TestResolverRolePrecedence 验证角色权限的四级覆盖顺序。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：替换测试 Resolver 的内存快照。
func TestResolverRolePrecedence(t *testing.T) {
	resolver := NewResolver(nil)
	policies := []Policy{
		{ScopeType: "global", ScopeID: "0", PluginName: "score", SubjectType: SubjectRole, SubjectID: string(RoleMember), Effect: EffectAllow},
		{ScopeType: "global", ScopeID: "0", PluginName: "score", FeatureKey: "reset", SubjectType: SubjectRole, SubjectID: string(RoleMember), Effect: EffectDeny},
		{ScopeType: "group", ScopeID: "123", PluginName: "score", SubjectType: SubjectRole, SubjectID: string(RoleMember), Effect: EffectDeny},
		{ScopeType: "group", ScopeID: "123", PluginName: "score", FeatureKey: "check_in", SubjectType: SubjectRole, SubjectID: string(RoleMember), Effect: EffectAllow},
	}
	// [决策理由] 合法策略快照必须成功发布后才能验证优先级。
	if err := resolver.Replace(policies); err != nil {
		t.Fatal(err)
	}
	defaults := Defaults{Member: false}
	// [决策理由] 群级功能 allow 应覆盖群级插件 deny。
	if !resolver.Allowed("123", "score", "check_in", "100", RoleMember, defaults) {
		t.Fatal("群级功能 allow 未覆盖群级插件 deny")
	}
	// [决策理由] 没有群级功能规则时，群级插件 deny 应覆盖全局插件 allow。
	if resolver.Allowed("123", "score", "rank", "100", RoleMember, defaults) {
		t.Fatal("群级插件 deny 未覆盖全局插件 allow")
	}
	// [决策理由] 其他群应使用全局功能 deny。
	if resolver.Allowed("456", "score", "reset", "100", RoleMember, defaults) {
		t.Fatal("全局功能 deny 未生效")
	}
	// [决策理由] 无功能覆盖时应使用全局插件 allow。
	if !resolver.Allowed("456", "score", "rank", "100", RoleMember, defaults) {
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
	if !resolver.Allowed("123", "ping", "ping", "100", RoleSuperAdmin, defaults) {
		t.Fatal("超级管理员默认权限错误")
	}
	// [决策理由] 无数据库规则时普通成员使用默认拒绝。
	if resolver.Allowed("123", "ping", "ping", "200", RoleMember, defaults) {
		t.Fatal("普通成员默认权限错误")
	}

	// >>> 数据演变示例
	// 1. 无规则 + SuperAdmin=true -> true。
	// 2. 无规则 + Member=false -> false。
}

// TestResolverUserPolicyPrecedesRolePolicy 验证指定用户策略优先于其群角色策略。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：替换测试 Resolver 的内存快照。
func TestResolverUserPolicyPrecedesRolePolicy(t *testing.T) {
	resolver := NewResolver(nil)
	policies := []Policy{
		{ScopeType: "group", ScopeID: "123", PluginName: "score", FeatureKey: "reset", SubjectType: SubjectRole, SubjectID: string(RoleGroupAdmin), Effect: EffectDeny},
		{ScopeType: "global", ScopeID: "0", PluginName: "score", SubjectType: SubjectUser, SubjectID: "100", Effect: EffectAllow},
	}
	// [决策理由] 合法的角色与用户组合策略必须能够同时发布。
	if err := resolver.Replace(policies); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 指定用户的全局插件授权应优先于更具体的群角色拒绝。
	if !resolver.Allowed("123", "score", "reset", "100", RoleGroupAdmin, Defaults{}) {
		t.Fatal("用户级 allow 未覆盖角色级 deny")
	}
	// [决策理由] 未被指定的同角色用户仍应命中群功能拒绝。
	if resolver.Allowed("123", "score", "reset", "200", RoleGroupAdmin, Defaults{GroupAdmin: true}) {
		t.Fatal("角色级 deny 未对其他用户生效")
	}

	// >>> 数据演变示例
	// 1. 用户100+全局插件allow+群角色deny -> 用户规则先命中 -> true。
	// 2. 用户200+群角色deny -> 无用户规则 -> 角色规则 -> false。
}
