// 📌 影响范围：仅构造并校验内存 Manifest；不修改全局 Catalog 或外部状态。
package plugin

import "testing"

// TestManifestValidate 验证稳定标识和功能唯一性规则。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		Name: "score", DisplayName: "积分",
		Features: []FeatureManifest{{Key: "check_in", DisplayName: "签到"}, {Key: "rank", DisplayName: "排行"}},
	}
	// [决策理由] 合法 Manifest 是后续错误用例的基线。
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法 Manifest 校验失败: %v", err)
	}
	duplicate := valid
	duplicate.Features = append(duplicate.Features, FeatureManifest{Key: "rank", DisplayName: "重复排行"})
	// [决策理由] 重复 feature_key 会让命令和权限引用产生歧义，必须失败。
	if err := duplicate.Validate(); err == nil {
		t.Fatal("重复功能未返回错误")
	}
	invalidName := valid
	invalidName.Name = "Score-Plugin"
	// [决策理由] 不稳定标识格式不能进入数据库主键。
	if err := invalidName.Validate(); err == nil {
		t.Fatal("非法插件名未返回错误")
	}

	// >>> 数据演变示例
	// 1. score+[check_in,rank] -> Validate -> nil。
	// 2. score+[rank,rank] -> Validate -> 重复错误。
}

// TestCloneManifest 验证 Catalog 快照不会共享可变切片。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestCloneManifest(t *testing.T) {
	original := Manifest{Features: []FeatureManifest{{Key: "echo", DefaultCommands: []string{"echo"}}}}
	cloned := cloneManifest(original)
	cloned.Features[0].Key = "changed"
	cloned.Features[0].DefaultCommands[0] = "changed"
	// [决策理由] 调用方修改快照时不能污染进程全局 Catalog。
	if original.Features[0].Key != "echo" || original.Features[0].DefaultCommands[0] != "echo" {
		t.Fatalf("原 Manifest 被快照修改污染: %+v", original)
	}

	// >>> 数据演变示例
	// 1. 原 echo -> 修改副本为 changed -> 原值仍为 echo。
	// 2. 原命令 echo -> 修改副本命令 -> 原命令仍为 echo。
}

// TestRegisterBindsManifestAndImplementation 验证统一注册的名称绑定和重名约束。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：向测试进程全局 Catalog 注册 catalog_test_plugin。
func TestRegisterBindsManifestAndImplementation(t *testing.T) {
	calls := make([]string, 0)
	manifest := Manifest{
		Name: "catalog_test_plugin", DisplayName: "注册测试",
		Features: []FeatureManifest{{Key: "echo", DisplayName: "回声", DefaultCommands: []string{"echo"}}},
	}
	factory := func(Runtime) (Plugin, error) { return &fakePlugin{name: "catalog_test_plugin", calls: &calls}, nil }
	// [决策理由] 名称一致的 Manifest 与实现必须注册成功。
	if err := Register(Registration{Manifest: manifest, Factory: factory}); err != nil {
		t.Fatal(err)
	}
	manifest.Features[0].DefaultCommands[0] = "changed"
	registrations := Registrations()
	var registeredCommand string
	for _, registration := range registrations {
		// [决策理由] 测试进程可能包含其他 init 注册项，只检查本测试的稳定插件名。
		if registration.Manifest.Name == "catalog_test_plugin" {
			registeredCommand = registration.Manifest.Features[0].DefaultCommands[0]
		}
	}
	// [决策理由] 注册后修改调用方切片不能污染全局 Catalog。
	if registeredCommand != "echo" {
		t.Fatalf("注册项被调用方修改污染: command=%q", registeredCommand)
	}
	// [决策理由] 第二次注册相同名称必须失败，避免运行实例歧义。
	if err := Register(Registration{Manifest: manifest, Factory: factory}); err == nil {
		t.Fatal("重复统一注册未返回错误")
	}
	emptyFactory := manifest
	emptyFactory.Name = "catalog_empty_factory"
	// [决策理由] 空工厂不能进入 Catalog。
	if err := Register(Registration{Manifest: emptyFactory}); err == nil {
		t.Fatal("空工厂未返回错误")
	}

	// >>> 数据演变示例
	// 1. catalog_test_plugin + 同名实现 -> 注册成功；修改输入后 Catalog 仍为 echo。
	// 2. catalog_empty_factory + nil -> 返回工厂为空错误。
}
