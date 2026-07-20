// 📌 影响范围：使用内存插件与状态仓库验证配置恢复和热应用；不访问数据库、网络或外部服务。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type configurablePlugin struct {
	name        string
	configured  string
	enabledAt   string
	validateErr error
	applyErr    error
	panicApply  bool
}

// Name 返回测试插件名。
// @param 无。
// @returns 固定插件名。
// ⚠️副作用说明：无。
func (p *configurablePlugin) Name() string {
	// >>> 数据演变示例
	// 1. name=config -> config。
	// 2. name=other -> other。
	return p.name
}

// Handle 实现测试插件事件接口。
// @param context.Context：处理上下文；ws.Event：测试事件。
// @returns nil。
// ⚠️副作用说明：无。
func (p *configurablePlugin) Handle(context.Context, ws.Event) error {
	// >>> 数据演变示例
	// 1. 消息事件 -> 不处理 -> nil。
	// 2. 心跳事件 -> 不处理 -> nil。
	return nil
}

// OnEnable 记录启用时已发布的配置。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：写入 enabledAt 测试字段。
func (p *configurablePlugin) OnEnable(context.Context) error {
	p.enabledAt = p.configured

	// >>> 数据演变示例
	// 1. configured=[db] -> enabledAt=[db]。
	// 2. configured="" -> enabledAt=""。
	return nil
}

// OnDisable 实现测试插件禁用生命周期。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (p *configurablePlugin) OnDisable(context.Context) error {
	// >>> 数据演变示例
	// 1. enabled -> disabled -> nil。
	// 2. disabled -> disabled -> nil。
	return nil
}

// ConfigSchema 返回单个前缀字段。
// @param 无。
// @returns 测试配置 Schema。
// ⚠️副作用说明：无。
func (p *configurablePlugin) ConfigSchema() ConfigSchema {
	result := ConfigSchema{Fields: []ConfigField{{Key: "prefix", DisplayName: "前缀", Type: FieldString, Default: json.RawMessage(`""`)}}}

	// >>> 数据演变示例
	// 1. 读取Schema -> prefix:string -> 默认空。
	// 2. Normalize({}) -> 补齐prefix=""。
	return result
}

// ValidateConfig 返回预设领域校验错误。
// @param context.Context：校验上下文；raw：规范化配置。
// @returns 预设错误。
// ⚠️副作用说明：无。
func (p *configurablePlugin) ValidateConfig(context.Context, json.RawMessage) error {
	// >>> 数据演变示例
	// 1. validateErr=nil -> nil。
	// 2. validateErr=invalid -> invalid。
	return p.validateErr
}

// ApplyConfig 解码并发布测试配置。
// @param context.Context：应用上下文；raw：规范化配置。
// @returns 预设错误或 JSON 解码错误。
// ⚠️副作用说明：成功时修改 configured 测试字段。
func (p *configurablePlugin) ApplyConfig(_ context.Context, raw json.RawMessage) error {
	// [决策理由] panic 开关用于验证 Manager 的故障隔离与迁移标志清理。
	if p.panicApply {
		panic("apply panic")
	}
	// [决策理由] 预设失败用于验证 Manager 不进入生命周期。
	if p.applyErr != nil {
		return p.applyErr
	}
	var value struct {
		Prefix string `json:"prefix"`
	}
	// [决策理由] 测试 fake 也应保持失败不部分发布契约。
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	p.configured = value.Prefix

	// >>> 数据演变示例
	// 1. {prefix:"[db]"} -> configured=[db] -> nil。
	// 2. applyErr=失败 -> configured保持旧值 -> 返回失败。
	return nil
}

// TestManagerApplyConfigRecoversPanic 验证配置回调 panic 不会卡住插件迁移状态。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：触发并恢复测试插件 panic，随后热应用内存配置。
func TestManagerApplyConfigRecoversPanic(t *testing.T) {
	manager := NewManager(nil)
	candidate := &configurablePlugin{name: "config", panicApply: true}
	// [决策理由] panic 路径必须通过已注册实例进入 Manager 互斥协议。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	err := manager.ApplyConfig(context.Background(), "config", json.RawMessage(`{"prefix":"first"}`))
	// [决策理由] panic 应转换为可判定错误而非逃逸到测试进程。
	if !errors.Is(err, ErrConfigCallbackPanic) {
		t.Fatalf("ApplyConfig() error = %v", err)
	}
	candidate.panicApply = false
	// [决策理由] panic 后第二次应用成功可证明 transitioning 已由 defer 清除。
	if err := manager.ApplyConfig(context.Background(), "config", json.RawMessage(`{"prefix":"second"}`)); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 恢复后的插件必须正常发布新快照。
	if candidate.configured != "second" {
		t.Fatalf("configured = %q", candidate.configured)
	}

	// >>> 数据演变示例
	// 1. Apply panic -> ErrConfigCallbackPanic -> transitioning恢复false。
	// 2. 第二次Apply -> configured=second -> nil。
}

// TestManagerLoadRestoresConfigBeforeEnable 验证持久化配置先于启用生命周期恢复。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存插件生命周期并修改测试字段。
func TestManagerLoadRestoresConfigBeforeEnable(t *testing.T) {
	store := &fakeStore{states: []State{{Name: "config", Enabled: true, Priority: 3, ConfigJSON: json.RawMessage(`{"prefix":"[db]"}`), ConfigVersion: 2}}}
	manager := NewManager(store)
	candidate := &configurablePlugin{name: "config"}
	// [决策理由] 只有已注册实例才能恢复数据库状态。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] Load 应完成配置恢复和启用两个阶段。
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// [决策理由] OnEnable 观察值能直接证明 ApplyConfig 发生在生命周期之前。
	if candidate.configured != "[db]" || candidate.enabledAt != "[db]" {
		t.Fatalf("configured/enabledAt = %q/%q", candidate.configured, candidate.enabledAt)
	}

	// >>> 数据演变示例
	// 1. DB prefix=[db],enabled=true -> ApplyConfig -> OnEnable观察[db]。
	// 2. 若顺序反转 -> enabledAt为空 -> 断言失败。
}

// TestManagerConfigCapabilitiesAndErrors 验证配置查询、校验、热应用与稳定错误。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：热应用修改内存测试插件配置。
func TestManagerConfigCapabilitiesAndErrors(t *testing.T) {
	manager := NewManager(nil)
	candidate := &configurablePlugin{name: "config"}
	// [决策理由] 配置能力只对已注册插件可见。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	schema, err := manager.ConfigSchema("config")
	// [决策理由] 有效 Configurable 必须暴露可渲染 Schema。
	if err != nil || len(schema.Fields) != 1 {
		t.Fatalf("ConfigSchema() = %+v,%v", schema, err)
	}
	// [决策理由] 未知字段必须在进入插件领域校验前被拒绝。
	if err := manager.ValidateConfig(context.Background(), "config", json.RawMessage(`{"unknown":true}`)); err == nil {
		t.Fatal("ValidateConfig() expected error")
	}
	// [决策理由] 合法热配置必须发布给运行实例。
	if err := manager.ApplyConfig(context.Background(), "config", json.RawMessage(`{"prefix":"[hot]"}`)); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 热应用结果应立即成为后续处理读取的快照。
	if candidate.configured != "[hot]" {
		t.Fatalf("configured = %q", candidate.configured)
	}
	legacy := &fakePlugin{name: "legacy", calls: &[]string{}}
	// [决策理由] 普通插件用于验证可选配置接口的错误语义。
	if err := manager.Register(legacy); err != nil {
		t.Fatal(err)
	}
	_, err = manager.ConfigSchema("legacy")
	// [决策理由] 管理 API 依赖 errors.Is 映射不支持状态。
	if !errors.Is(err, ErrConfigNotSupported) {
		t.Fatalf("legacy error = %v", err)
	}
	_, err = manager.ConfigSchema("missing")
	// [决策理由] 未注册和不支持必须可区分。
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("missing error = %v", err)
	}

	// >>> 数据演变示例
	// 1. config+prefix=[hot] -> Validate+Apply -> configured=[hot]。
	// 2. legacy/missing -> ErrConfigNotSupported/ErrNotRegistered。
}

// TestManagerLoadRejectsInvalidOrFailedConfig 验证启动配置错误阻止启用。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存配置插件，不访问外部状态。
func TestManagerLoadRejectsInvalidOrFailedConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    json.RawMessage
		applyErr  error
		wantError string
	}{{name: "invalid", config: json.RawMessage(`{"unknown":true}`), wantError: "恢复插件"}, {name: "apply", config: json.RawMessage(`{"prefix":"x"}`), applyErr: errors.New("apply failed"), wantError: "应用插件"}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := &configurablePlugin{name: "config", applyErr: test.applyErr}
			manager := NewManager(&fakeStore{states: []State{{Name: "config", Enabled: true, ConfigJSON: test.config}}})
			// [决策理由] 启动错误测试需要注册真实目标实例。
			if err := manager.Register(candidate); err != nil {
				t.Fatal(err)
			}
			err := manager.Load(context.Background())
			// [决策理由] 无效或不可应用配置必须阻止 OnEnable，避免运行态与数据库分裂。
			if err == nil || !strings.Contains(err.Error(), test.wantError) || candidate.enabledAt != "" {
				t.Fatalf("Load() error/enabledAt = %v/%q", err, candidate.enabledAt)
			}

			// >>> 数据演变示例
			// 1. unknown字段 -> Normalize失败 -> OnEnable未调用。
			// 2. ApplyConfig失败 -> 返回应用错误 -> OnEnable未调用。
		})
	}

	// >>> 数据演变示例
	// 1. invalid用例 -> 恢复错误 -> enabledAt空。
	// 2. apply用例 -> 应用错误 -> enabledAt空。
}
