// 📌 影响范围：仅测试内存命令标准化与快照匹配；不访问数据库或外部变量。
package command

import "testing"

// TestNormalize 验证前缀、空白和大小写标准化。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestNormalize(t *testing.T) {
	result, err := Normalize(" /每日   签到 ", "/")
	// [决策理由] 合法中文命令必须成功标准化。
	if err != nil || result != "每日 签到" {
		t.Fatalf("Normalize = %q,%v", result, err)
	}
	result, err = Normalize(" PING ", "/")
	// [决策理由] 英文命令大小写必须折叠以阻止重复绕过。
	if err != nil || result != "ping" {
		t.Fatalf("Normalize = %q,%v", result, err)
	}

	// >>> 数据演变示例
	// 1. " /每日   签到 " -> "每日 签到"。
	// 2. " PING " -> "ping"。
}

// TestResolveMatchesLongestParameterizedCommand 验证带参数输入按最长命令前缀路由。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestResolveMatchesLongestParameterizedCommand(t *testing.T) {
	registry := NewRegistry(nil)
	err := registry.Replace([]Binding{
		{ScopeType: ScopeGlobal, ScopeID: "0", PluginName: "admin", FeatureKey: "plugin", Command: "插件", NormalizedCommand: "插件", Enabled: true},
		{ScopeType: ScopeGlobal, ScopeID: "0", PluginName: "admin", FeatureKey: "enable", Command: "插件 启用", NormalizedCommand: "插件 启用", Enabled: true},
	})
	// [决策理由] 测试快照必须先成功发布才能验证匹配行为。
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	binding, found := registry.Resolve("123", "/插件 启用 ping", "/")
	// [决策理由] 参数化命令必须识别且选择更具体的最长前缀。
	if !found || binding.FeatureKey != "enable" {
		t.Fatalf("Resolve() = %+v,%v", binding, found)
	}

	// >>> 数据演变示例
	// 1. /插件 启用 ping -> 候选“插件”“插件 启用” -> enable。
	// 2. 最长前缀胜出 -> 参数ping不参与命令唯一键。
}

// TestRegistryResolveScopePriority 验证群级覆盖全局及重复检测。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：替换测试 Registry 的内存快照。
func TestRegistryResolveScopePriority(t *testing.T) {
	registry := NewRegistry(nil)
	bindings := []Binding{
		{ScopeType: ScopeGlobal, ScopeID: "0", PluginName: "score", FeatureKey: "check_in", Command: "签到", NormalizedCommand: "签到", Enabled: true},
		{ScopeType: ScopeGroup, ScopeID: "123", PluginName: "special", FeatureKey: "check_in", Command: "签到", NormalizedCommand: "签到", Enabled: true},
	}
	// [决策理由] 合法不同作用域命令必须能发布为快照。
	if err := registry.Replace(bindings); err != nil {
		t.Fatal(err)
	}
	groupBinding, exists := registry.Resolve("123", "/签到", "/")
	// [决策理由] 群级绑定必须覆盖全局同名命令。
	if !exists || groupBinding.PluginName != "special" {
		t.Fatalf("群级匹配错误: %+v,%t", groupBinding, exists)
	}
	globalBinding, exists := registry.Resolve("456", "/签到", "/")
	// [决策理由] 没有群级覆盖时必须回退全局绑定。
	if !exists || globalBinding.PluginName != "score" {
		t.Fatalf("全局匹配错误: %+v,%t", globalBinding, exists)
	}
	duplicate := append(bindings, bindings[0])
	// [决策理由] 同作用域重复命令必须在内存层再次拦截。
	if err := registry.Replace(duplicate); err == nil {
		t.Fatal("重复命令未返回错误")
	}

	// >>> 数据演变示例
	// 1. 群123 /签到 -> special.check_in。
	// 2. 群456 /签到 -> score.check_in。
}
