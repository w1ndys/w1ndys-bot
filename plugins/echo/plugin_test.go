// 📌 影响范围：使用内存fake Messenger验证echo插件；不访问NapCat、PostgreSQL或网络。
package echo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeMessenger struct {
	messageID int64
	reply     string
	err       error
}

// TestConfigHotApply 验证 Echo 配置 Schema、领域校验和原子热应用。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：替换内存 Echo 配置并记录 fake Messenger 回复。
func TestConfigHotApply(t *testing.T) {
	messenger := &fakeMessenger{}
	instance, err := newPlugin(plugin.Runtime{Messenger: messenger})
	// [决策理由] 只有完整运行实例才能验证配置对 Handle 的实际影响。
	if err != nil {
		t.Fatal(err)
	}
	configurable, ok := instance.(plugin.Configurable)
	// [决策理由] Echo 是首个配置示例，必须持续实现平台可选契约。
	if !ok {
		t.Fatal("echo does not implement Configurable")
	}
	schema := configurable.ConfigSchema()
	// [决策理由] Schema 必须稳定声明唯一的 response_prefix 字段。
	if err := schema.Validate(); err != nil || len(schema.Fields) != 1 || schema.Fields[0].Key != "response_prefix" {
		t.Fatalf("ConfigSchema() = %+v,error=%v", schema, err)
	}
	raw := json.RawMessage(`{"response_prefix":"[bot] "}`)
	// [决策理由] 合法前缀应同时通过领域校验与应用。
	if err := configurable.ValidateConfig(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	// [决策理由] ApplyConfig 成功后新快照必须立即可读。
	if err := configurable.ApplyConfig(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	ctx := plugin.WithInvocation(context.Background(), plugin.Invocation{FeatureKey: featureEcho, Command: "echo", Arguments: "Hello"})
	err = instance.Handle(ctx, &ws.MessageEvent{MessageID: 20})
	// [决策理由] 热应用不能改变正常停止传播语义。
	if !errors.Is(err, plugin.ErrStopPropagation) {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] Handle 必须从原子快照读取并拼接配置前缀。
	if messenger.reply != "[bot] Hello" {
		t.Fatalf("reply = %q", messenger.reply)
	}
	tooLong, err := json.Marshal(map[string]string{"response_prefix": strings.Repeat("前", 101)})
	// [决策理由] 测试输入编码失败会让边界断言失去意义。
	if err != nil {
		t.Fatal(err)
	}
	// [决策理由] 领域长度限制必须拒绝无界回复放大。
	if err := configurable.ValidateConfig(context.Background(), tooLong); err == nil {
		t.Fatal("ValidateConfig() expected length error")
	}

	// >>> 数据演变示例
	// 1. prefix=[bot]+Hello -> 原子发布 -> 回复[bot] Hello。
	// 2. prefix为101字符 -> ValidateConfig拒绝 -> 旧快照保持不变。
}

// Reply 实现测试未使用的普通回复能力。
// @param context.Context：请求上下文；event：消息事件；message：消息内容。
// @returns 固定消息ID与预设错误。
// ⚠️副作用说明：无。
func (f *fakeMessenger) Reply(context.Context, *ws.MessageEvent, any) (int64, error) {
	// >>> 数据演变示例
	// 1. err=nil -> 1,nil。
	// 2. err=失败 -> 1,失败。
	return 1, f.err
}

// ReplyToMessage 记录echo引用回复。
// @param context.Context：请求上下文；event：消息；messageID：引用ID；message：回复文本。
// @returns 固定消息ID与预设错误。
// ⚠️副作用说明：修改fake记录字段。
func (f *fakeMessenger) ReplyToMessage(_ context.Context, _ *ws.MessageEvent, messageID int64, message string) (int64, error) {
	f.messageID = messageID
	f.reply = message

	// >>> 数据演变示例
	// 1. id=9,text=Hello -> 记录 -> 1,nil。
	// 2. 预设失败 -> 记录输入 -> 1,error。
	return 1, f.err
}

// TestHandleEcho 验证Invocation参数被引用回复且保持大小写。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：修改内存fake记录。
func TestHandleEcho(t *testing.T) {
	messenger := &fakeMessenger{}
	instance, err := newPlugin(plugin.Runtime{Messenger: messenger})
	// [决策理由] 完整运行依赖必须创建成功。
	if err != nil {
		t.Fatal(err)
	}
	event := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", MessageID: 9, UserID: 100, GroupID: 200}
	ctx := plugin.WithInvocation(context.Background(), plugin.Invocation{FeatureKey: featureEcho, Command: "echo", Arguments: "Hello World"})
	err = instance.Handle(ctx, event)
	// [决策理由] 成功处理定向命令后必须停止传播。
	if !errors.Is(err, plugin.ErrStopPropagation) {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] echo必须引用原消息并完整回复上层解析的参数。
	if messenger.messageID != 9 || messenger.reply != "Hello World" {
		t.Fatalf("reply id/text = %d/%q", messenger.messageID, messenger.reply)
	}

	// >>> 数据演变示例
	// 1. Invocation{echo,Hello World}+message9 -> 引用9回复Hello World。
	// 2. 回复内容或ID变化 -> fake断言失败。
}

// TestHandleValidationAndReplyError 验证空参数、非消息和发送失败路径。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：修改内存fake记录。
func TestHandleValidationAndReplyError(t *testing.T) {
	messenger := &fakeMessenger{}
	instance, err := newPlugin(plugin.Runtime{Messenger: messenger})
	// [决策理由] 错误路径必须在真实实现实例上验证。
	if err != nil {
		t.Fatal(err)
	}
	emptyContext := plugin.WithInvocation(context.Background(), plugin.Invocation{FeatureKey: featureEcho, Command: "回声"})
	event := &ws.MessageEvent{MessageID: 10}
	err = instance.Handle(emptyContext, event)
	// [决策理由] 空参数应引用回复含当前触发词的用法并正常停止传播。
	if !errors.Is(err, plugin.ErrStopPropagation) || messenger.reply != "用法：回声 <要重复的内容>" || messenger.messageID != 10 {
		t.Fatalf("empty arguments error/reply = %v/%q", err, messenger.reply)
	}
	heartbeat := &ws.HeartbeatEvent{BaseEvent: ws.BaseEvent{PostType: "meta_event"}, MetaEventType: "heartbeat"}
	ctx := plugin.WithInvocation(context.Background(), plugin.Invocation{FeatureKey: featureEcho, Command: "echo", Arguments: "Hi"})
	err = instance.Handle(ctx, heartbeat)
	// [决策理由] 非消息事件不能进入引用回复链路。
	if err == nil || !strings.Contains(err.Error(), "非消息事件") {
		t.Fatalf("non-message error = %v", err)
	}
	messenger.err = errors.New("发送失败")
	err = instance.Handle(ctx, event)
	// [决策理由] Messenger错误必须包装业务上下文并保留根因。
	if err == nil || !strings.Contains(err.Error(), "发送echo回复") || !errors.Is(err, messenger.err) {
		t.Fatalf("reply error = %v", err)
	}

	// >>> 数据演变示例
	// 1. echo无参数 -> 引用回复用法；echo+Heartbeat -> 类型错误。
	// 2. echo Hi+Messenger失败 -> 包装发送错误并保留根因。
}

// TestManifestIgnoreAndLifecycle 验证集中Manifest、非目标调用和幂等生命周期。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：修改内存fake Messenger记录。
func TestManifestIgnoreAndLifecycle(t *testing.T) {
	// [决策理由] 集中配置区生成的Manifest必须持续通过真实Catalog校验。
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() error = %v", err)
	}
	// [决策理由] 示例必须保留一个echo功能和两个可直接体验的默认触发词。
	if len(manifest.Features) != 1 || len(manifest.Features[0].DefaultCommands) != 2 || manifest.Features[0].Key != featureEcho {
		t.Fatalf("unexpected manifest features = %+v", manifest.Features)
	}
	messenger := &fakeMessenger{}
	instance, err := newPlugin(plugin.Runtime{Messenger: messenger})
	// [决策理由] 生命周期和忽略分支必须基于真实实例验证。
	if err != nil {
		t.Fatal(err)
	}
	other := plugin.WithInvocation(context.Background(), plugin.Invocation{FeatureKey: "other", Command: "other", Arguments: "Hi"})
	err = instance.Handle(other, &ws.MessageEvent{MessageID: 12})
	// [决策理由] 非echo功能不能发送回复或返回错误。
	if err != nil || messenger.reply != "" {
		t.Fatalf("ignored error/reply = %v/%q", err, messenger.reply)
	}
	// [决策理由] 无资源示例的启停方法必须支持重复调用，给开发者展示幂等契约。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatalf("OnEnable() error = %v", err)
	}
	// [决策理由] 第二次启用不能因重复初始化失败。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatalf("second OnEnable() error = %v", err)
	}
	// [决策理由] 禁用同样必须安全完成资源清理。
	if err := instance.OnDisable(context.Background()); err != nil {
		t.Fatalf("OnDisable() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. 集中Manifest -> Validate通过 -> echo功能和两个默认命令完整。
	// 2. other调用+重复启用/禁用 -> 不回复且生命周期均nil。
}

// TestNewPluginRejectsMissingMessenger 验证Factory依赖检查。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestNewPluginRejectsMissingMessenger(t *testing.T) {
	instance, err := newPlugin(plugin.Runtime{})
	// [决策理由] 没有Messenger时echo无法运行，必须启动失败。
	if err == nil || instance != nil {
		t.Fatalf("newPlugin() = %v,%v", instance, err)
	}

	// >>> 数据演变示例
	// 1. Runtime{} -> nil,error。
	// 2. Runtime{Messenger} -> 其他测试验证 -> implementation,nil。
}
