// 📌 影响范围：构造并校验内存 Manifest；测试期间向进程全局 Catalog 注册固定测试插件；不访问外部状态。
package plugin

import (
	"errors"
	"strings"
	"testing"
)

var errFactoryDependency = errors.New("依赖缺失")

// catalogTestFactory 创建 Catalog 注册测试实例。
// @param Runtime：未使用的测试运行环境。
// @returns 名称为 catalog_test_plugin 的 fakePlugin。
// ⚠️副作用说明：分配内存调用记录和插件实例。
func catalogTestFactory(Runtime) (Plugin, error) {
	calls := make([]string, 0)

	// >>> 数据演变示例
	// 1. Runtime{} -> catalog_test_plugin实例,nil。
	// 2. 任意Runtime -> 新调用记录 -> 独立实例。
	return &fakePlugin{name: "catalog_test_plugin", calls: &calls}, nil
}

// expectedTestFactory 创建名称匹配 Manifest 的测试实例。
// @param Runtime：未使用的测试运行环境。
// @returns 名称为 expected 的 fakePlugin。
// ⚠️副作用说明：分配内存调用记录和插件实例。
func expectedTestFactory(Runtime) (Plugin, error) {
	calls := make([]string, 0)

	// >>> 数据演变示例
	// 1. Manifest=expected -> Factory -> expected实例。
	// 2. 新调用 -> 新实例 -> 不共享调用记录。
	return &fakePlugin{name: "expected", calls: &calls}, nil
}

// otherTestFactory 创建名称不匹配 Manifest 的测试实例。
// @param Runtime：未使用的测试运行环境。
// @returns 名称为 other 的 fakePlugin。
// ⚠️副作用说明：分配内存调用记录和插件实例。
func otherTestFactory(Runtime) (Plugin, error) {
	calls := make([]string, 0)

	// >>> 数据演变示例
	// 1. Manifest=expected -> Factory -> other实例。
	// 2. Registration.New -> 名称比较 -> 拒绝实例。
	return &fakePlugin{name: "other", calls: &calls}, nil
}

// emptyTestFactory 返回无类型空插件接口。
// @param Runtime：未使用的测试运行环境。
// @returns nil 插件和 nil 错误。
// ⚠️副作用说明：无。
func emptyTestFactory(Runtime) (Plugin, error) {
	// >>> 数据演变示例
	// 1. Runtime{} -> nil,nil。
	// 2. Registration.New -> 空实例检查 -> 返回错误。
	return nil, nil
}

// typedNilTestFactory 返回封装 nil 指针的插件接口。
// @param Runtime：未使用的测试运行环境。
// @returns 动态类型为 *fakePlugin、底层值为 nil 的接口。
// ⚠️副作用说明：无。
func typedNilTestFactory(Runtime) (Plugin, error) {
	var instance *fakePlugin

	// >>> 数据演变示例
	// 1. (*fakePlugin)(nil) -> 转为Plugin接口 -> 接口本身非nil。
	// 2. Registration.New反射底层指针 -> 识别nil且不调用Name。
	return instance, nil
}

// failingTestFactory 返回固定依赖错误。
// @param Runtime：未使用的测试运行环境。
// @returns nil 插件和 errFactoryDependency。
// ⚠️副作用说明：无。
func failingTestFactory(Runtime) (Plugin, error) {
	// >>> 数据演变示例
	// 1. Runtime{} -> nil,依赖缺失。
	// 2. Registration.New -> 包装错误 -> errors.Is保留根因。
	return nil, errFactoryDependency
}

// TestManifestValidate 验证稳定标识和功能唯一性规则。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		Name: "score", DisplayName: "积分", Description: "积分服务",
		Features: []FeatureManifest{
			{Key: "check_in", DisplayName: "签到", Description: "每日签到", DefaultCommands: []string{"签到"}},
			{Key: "rank", DisplayName: "排行", Description: "积分排行", DefaultCommands: []string{"排行"}},
		},
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

// TestManifestValidateRejectsIncompleteAndConflictingCommands 验证说明、功能及默认命令完整性。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestManifestValidateRejectsIncompleteAndConflictingCommands(t *testing.T) {
	valid := Manifest{Name: "score", DisplayName: "积分", Description: "积分服务", Features: []FeatureManifest{{Key: "rank", DisplayName: "排行", Description: "积分排行", DefaultCommands: []string{"排行"}}}}
	tests := []struct {
		name      string
		candidate Manifest
		want      string
	}{
		{name: "插件说明为空", candidate: Manifest{Name: "score", DisplayName: "积分", Features: valid.Features}, want: "插件说明不能为空"},
		{name: "功能说明为空", candidate: Manifest{Name: "score", DisplayName: "积分", Description: "积分服务", Features: []FeatureManifest{{Key: "rank", DisplayName: "排行", DefaultCommands: []string{"排行"}}}}, want: "说明为空"},
		{name: "默认命令为空", candidate: Manifest{Name: "score", DisplayName: "积分", Description: "积分服务", Features: []FeatureManifest{{Key: "rank", DisplayName: "排行", Description: "积分排行"}}}, want: "至少需要一个默认命令"},
		{name: "默认命令不可标准化", candidate: Manifest{Name: "score", DisplayName: "积分", Description: "积分服务", Features: []FeatureManifest{{Key: "rank", DisplayName: "排行", Description: "积分排行", DefaultCommands: []string{"   "}}}}, want: "命令不能为空"},
		{name: "标准化后重复", candidate: Manifest{Name: "score", DisplayName: "积分", Description: "积分服务", Features: []FeatureManifest{{Key: "rank", DisplayName: "排行", Description: "积分排行", DefaultCommands: []string{" PING ", "ping"}}}}, want: "标准化后重复"},
	}
	for _, test := range tests {
		err := test.candidate.Validate()
		// [决策理由] 每类不完整或路由冲突 Manifest 都必须在注册前返回可定位错误。
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: Validate() error = %v, want contains %q", test.name, err, test.want)
		}
	}
	observer := Manifest{Name: "observer", DisplayName: "观察器", Description: "仅处理广播事件"}
	// [决策理由] 纯观察插件不依赖命令路由，零功能 Manifest 必须继续受到框架支持。
	if err := observer.Validate(); err != nil {
		t.Fatalf("观察型 Manifest.Validate() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. observer+空Features -> 纯广播插件 -> 校验通过。
	// 2. rank默认命令=[" PING ","ping"] -> 均标准化为ping -> 返回重复错误。
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
	manifest := Manifest{
		Name: "catalog_test_plugin", DisplayName: "注册测试", Description: "测试注册绑定",
		Features: []FeatureManifest{{Key: "echo", DisplayName: "回声", Description: "返回输入", DefaultCommands: []string{"echo"}}},
	}
	// [决策理由] 名称一致的 Manifest 与实现必须注册成功。
	if err := Register(Registration{Manifest: manifest, Factory: catalogTestFactory}); err != nil {
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
	if err := Register(Registration{Manifest: manifest, Factory: catalogTestFactory}); err == nil {
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

// TestRegistrationNewValidatesFactoryBinding 验证工厂结果与 Manifest 名称强绑定。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用测试工厂；不修改全局 Catalog。
func TestRegistrationNewValidatesFactoryBinding(t *testing.T) {
	manifest := Manifest{Name: "expected", DisplayName: "预期插件", Description: "测试实例绑定", Features: []FeatureManifest{{Key: "run", DisplayName: "运行", Description: "执行测试", DefaultCommands: []string{"run"}}}}
	matching := Registration{Manifest: manifest, Factory: expectedTestFactory}
	implementation, err := matching.New(Runtime{})
	// [决策理由] 同名实例是正常启动路径，必须原样返回给 Manager。
	if err != nil || implementation.Name() != "expected" {
		t.Fatalf("New() = %v,%v", implementation, err)
	}
	mismatching := Registration{Manifest: manifest, Factory: otherTestFactory}
	_, err = mismatching.New(Runtime{})
	// [决策理由] 名称错位会拆分持久化配置和运行路由，必须拒绝。
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("名称不一致 New() error = %v", err)
	}
	empty := Registration{Manifest: manifest, Factory: emptyTestFactory}
	_, err = empty.New(Runtime{})
	// [决策理由] nil 实例不能注册，且调用 Name 会 panic。
	if err == nil || !strings.Contains(err.Error(), "空实例") {
		t.Fatalf("空实例 New() error = %v", err)
	}
	typedNil := Registration{Manifest: manifest, Factory: typedNilTestFactory}
	_, err = typedNil.New(Runtime{})
	// [决策理由] typed nil 同样无法进入路由，且直接调用 Name 通常会 panic。
	if err == nil || !strings.Contains(err.Error(), "空实例") {
		t.Fatalf("typed nil New() error = %v", err)
	}
	failing := Registration{Manifest: manifest, Factory: failingTestFactory}
	_, err = failing.New(Runtime{})
	// [决策理由] 工厂原始错误必须可通过 errors.Is 识别，供启动层记录根因。
	if !errors.Is(err, errFactoryDependency) {
		t.Fatalf("工厂错误 New() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. Manifest=expected + 实例Name=expected -> 返回实例,nil。
	// 2. Manifest=expected + typed nil实例 -> 返回空实例错误且不panic。
}
