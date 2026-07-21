// 📌 影响范围：无外部依赖；验证群门禁快照优先级与Manager广播、定向路由短路。
package plugin

import (
	"context"
	"reflect"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type observingGroupGate struct {
	allowed      bool
	allowedCalls int
}

// Load 实现无需外部状态的测试门禁加载。
// @param context.Context：加载上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (g *observingGroupGate) Load(context.Context) error {
	// >>> 数据演变示例
	// 1. Manager.Load -> nil且计数不变。
	// 2. 重复Load -> 仍为nil。
	return nil
}

// Allowed 记录群门禁被预检的次数并返回预设结果。
// @param name：插件名；event：路由事件。
// @returns allowed预设值。
// ⚠️副作用说明：allowedCalls加一。
func (g *observingGroupGate) Allowed(string, ws.Event) bool {
	g.allowedCalls++

	// >>> 数据演变示例
	// 1. allowed=true+首次调用 -> calls1,true。
	// 2. allowed=false+第二次调用 -> calls2,false。
	return g.allowed
}

// TestPostgresGroupGateAllowed 验证覆盖优先、默认开关及私聊放行语义。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：原子发布纯内存测试策略。
func TestPostgresGroupGateAllowed(t *testing.T) {
	gate := NewPostgresGroupGate(nil)
	gate.publishForTest(groupGateSnapshot{
		"default_off": {DefaultEnabled: false, Overrides: map[int64]bool{100: true}},
		"default_on":  {DefaultEnabled: true, Overrides: map[int64]bool{100: false}},
	})
	tests := []struct {
		name       string
		pluginName string
		event      ws.Event
		want       bool
	}{
		{name: "默认关闭", pluginName: "default_off", event: &ws.MessageEvent{MessageType: "group", GroupID: 200}, want: false},
		{name: "关闭插件群覆盖开启", pluginName: "default_off", event: &ws.MessageEvent{MessageType: "group", GroupID: 100}, want: true},
		{name: "默认开启", pluginName: "default_on", event: &ws.MessageEvent{MessageType: "group", GroupID: 200}, want: true},
		{name: "开启插件群覆盖关闭", pluginName: "default_on", event: &ws.MessageEvent{MessageType: "group", GroupID: 100}, want: false},
		{name: "私聊不受影响", pluginName: "default_off", event: &ws.MessageEvent{MessageType: "private", GroupID: 0}, want: true},
		{name: "群通知受默认关闭约束", pluginName: "default_off", event: &ws.NoticeEvent{GroupID: 200}, want: false},
		{name: "禁言通知命中开启覆盖", pluginName: "default_off", event: &ws.GroupBanNotice{NoticeEvent: ws.NoticeEvent{GroupID: 100}}, want: true},
		{name: "未声明插件放行", pluginName: "global", event: &ws.MessageEvent{MessageType: "group", GroupID: 100}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := gate.Allowed(test.pluginName, test.event)
			// [决策理由] 表驱动结果直接验证门禁策略，无需启动生命周期。
			if got != test.want {
				t.Fatalf("Allowed() = %v, want %v", got, test.want)
			}

			// >>> 数据演变示例
			// 1. 覆盖存在 -> 返回覆盖值。
			// 2. 私聊或未声明 -> true。
		})
	}

	// >>> 数据演变示例
	// 1. default_off群200 -> false；群100覆盖 -> true。
	// 2. default_on群200 -> true；群100覆盖 -> false。
}

// TestManagerGroupGateRoutes 验证Handle和HandleNamed均在beginHandling前应用门禁且私聊不受影响。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：启用内存插件并调用测试Handle。
func TestManagerGroupGateRoutes(t *testing.T) {
	gate := NewPostgresGroupGate(nil)
	gate.publishForTest(groupGateSnapshot{"controlled": {DefaultEnabled: false, Overrides: map[int64]bool{100: true}}})
	calls := make([]string, 0)
	manager := NewManager(nil, gate)
	candidate := &fakePlugin{name: "controlled", calls: &calls}
	// [决策理由] 注册和启用是路由门禁测试的前置条件。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 禁用插件本身不会进入路由，必须先启用以隔离群门禁变量。
	if err := manager.SetEnabled(context.Background(), "controlled", true); err != nil {
		t.Fatal(err)
	}
	closed := &ws.MessageEvent{MessageType: "group", GroupID: 200}
	opened := &ws.MessageEvent{MessageType: "group", GroupID: 100}
	private := &ws.MessageEvent{MessageType: "private", GroupID: 0}
	// [决策理由] 广播关闭群应无错误安静跳过插件。
	if err := manager.Handle(context.Background(), closed); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 定向关闭群也必须防止绕过门禁。
	if err := manager.HandleNamed(context.Background(), "controlled", closed); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 覆盖开启群应允许广播调用。
	if err := manager.Handle(context.Background(), opened); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 覆盖开启群应允许定向调用。
	if err := manager.HandleNamed(context.Background(), "controlled", opened); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 私聊不受群默认关闭影响。
	if err := manager.HandleNamed(context.Background(), "controlled", private); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 只有开启群的两次路由和私聊应到达插件。
	if !reflect.DeepEqual(calls, []string{"controlled", "controlled", "controlled"}) {
		t.Fatalf("calls = %v", calls)
	}

	// >>> 数据演变示例
	// 1. 关闭群广播+定向 -> calls保持空。
	// 2. 开启群广播+定向+私聊 -> calls追加三次。
}

// TestGroupControllableManifestClone 验证群控制能力在Manifest校验和快照复制中保留。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无；仅校验并复制内存Manifest。
func TestGroupControllableManifestClone(t *testing.T) {
	manifest := Manifest{Name: "observer", DisplayName: "观察插件", Description: "测试群门禁声明", GroupControllable: true}
	// [决策理由] 纯广播插件允许无Features，群控制标记不应使Manifest校验失败。
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	cloned := cloneManifest(manifest)
	// [决策理由] Catalog快照必须保留标量能力，否则同步数据库后门禁不会生效。
	if !cloned.GroupControllable {
		t.Fatal("GroupControllable was lost")
	}

	// >>> 数据演变示例
	// 1. observer GroupControllable=true -> Validate nil -> clone仍true。
	// 2. 空Features -> 作为广播观察插件继续合法。
}

// TestManagerRoutePrecheckOrder 验证全局禁用和迁移优先于群门禁，启用稳定后两种路由才调用gate。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：切换内存插件状态并记录门禁调用次数。
func TestManagerRoutePrecheckOrder(t *testing.T) {
	gate := &observingGroupGate{allowed: true}
	calls := make([]string, 0)
	manager := NewManager(nil, gate)
	candidate := &fakePlugin{name: "controlled", calls: &calls}
	// [决策理由] 注册后默认禁用，正好验证全局开关先于gate。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	event := &ws.MessageEvent{MessageType: "group", GroupID: 100}
	// [决策理由] 广播禁用插件应安静跳过且不调用gate。
	if err := manager.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 定向禁用插件应保留错误语义且不调用gate。
	if err := manager.HandleNamed(context.Background(), "controlled", event); err == nil {
		t.Fatal("disabled HandleNamed should fail")
	}
	// [决策理由] 两种禁用路由均不能触达群门禁。
	if gate.allowedCalls != 0 {
		t.Fatalf("disabled gate calls = %d", gate.allowedCalls)
	}
	// [决策理由] 启用后才能进一步验证迁移优先级和正常gate调用。
	if err := manager.SetEnabled(context.Background(), "controlled", true); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.entries["controlled"].transitioning = true
	manager.mu.Unlock()
	// [决策理由] 迁移隔离中的广播仍应在gate前被全局状态短路。
	if err := manager.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 迁移隔离中的定向路由应返回错误且不调用gate。
	if err := manager.HandleNamed(context.Background(), "controlled", event); err == nil {
		t.Fatal("transitioning HandleNamed should fail")
	}
	// [决策理由] transitioning状态也不得触达gate。
	if gate.allowedCalls != 0 {
		t.Fatalf("transitioning gate calls = %d", gate.allowedCalls)
	}
	manager.mu.Lock()
	manager.entries["controlled"].transitioning = false
	manager.mu.Unlock()
	// [决策理由] 启用稳定的广播路由应执行一次gate后调用插件。
	if err := manager.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 启用稳定的定向路由也应执行一次gate后调用插件。
	if err := manager.HandleNamed(context.Background(), "controlled", event); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 仅最后两次稳定路由允许查询门禁并进入插件。
	if gate.allowedCalls != 2 || !reflect.DeepEqual(calls, []string{"controlled", "controlled"}) {
		t.Fatalf("gate calls = %d, plugin calls = %v", gate.allowedCalls, calls)
	}

	// >>> 数据演变示例
	// 1. disabled/transitioning广播与定向 -> gate0,插件0。
	// 2. enabled稳定广播与定向 -> gate2,插件2。
}
