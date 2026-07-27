// 📌 影响范围：无；使用内存 Messenger 与 Management 验证 QQ 应急管理命令。
package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeMessenger struct {
	referenceID      int64
	referenceContent string
}

// Reply 兼容普通消息发送接口。
// @param ctx：未使用的上下文；event：原消息；message：回复内容。
// @returns 固定新消息 ID 与 nil。
// ⚠️副作用说明：无。
func (f *fakeMessenger) Reply(context.Context, *ws.MessageEvent, any) (int64, error) {
	// >>> 数据演变示例
	// 1. Reply文本 -> 1,nil。
	// 2. 当前admin插件不调用 -> 无记录变化。
	return 1, nil
}

// ReplyToMessage 记录引用回复消息 ID 和内容。
// @param ctx：未使用的上下文；event：原消息；messageID：引用消息 ID；content：回复文本。
// @returns 固定新消息 ID与nil。
// ⚠️副作用说明：修改引用回复记录字段。
func (f *fakeMessenger) ReplyToMessage(_ context.Context, _ *ws.MessageEvent, messageID int64, content string) (int64, error) {
	f.referenceID, f.referenceContent = messageID, content

	// >>> 数据演变示例
	// 1. id=88,content=成功 -> 记录字段 -> 1,nil。
	// 2. id=1,content空 -> 记录空文本 -> 1,nil。
	return 1, nil
}

type fakeRuntimeManagement struct {
	states          []plugin.RuntimeStateView
	actor           management.Actor
	pluginKey       string
	enabled         bool
	expectedVersion int64
	groupID         int64
	err             error
}

func (f *fakeRuntimeManagement) List(_ context.Context, actor management.Actor) ([]plugin.RuntimeStateView, error) {
	f.actor = actor
	return f.states, f.err
}

func (f *fakeRuntimeManagement) Get(_ context.Context, actor management.Actor, pluginKey string) (plugin.RuntimeStateView, error) {
	f.actor, f.pluginKey = actor, pluginKey
	if f.err != nil {
		return plugin.RuntimeStateView{}, f.err
	}
	for _, state := range f.states {
		if state.PluginKey == pluginKey {
			return state, nil
		}
	}
	return plugin.RuntimeStateView{PluginKey: pluginKey, Version: 2}, nil
}

func (f *fakeRuntimeManagement) SetGlobalEnabled(_ context.Context, actor management.Actor, pluginKey string, enabled bool, expectedVersion int64) (plugin.RuntimeStateView, error) {
	f.actor, f.pluginKey, f.enabled, f.expectedVersion = actor, pluginKey, enabled, expectedVersion
	if f.err != nil {
		return plugin.RuntimeStateView{}, f.err
	}
	return plugin.RuntimeStateView{PluginKey: pluginKey, DesiredEnabled: enabled, Version: expectedVersion + 1, Status: plugin.RuntimeReady}, nil
}

func (f *fakeRuntimeManagement) SetGroupEnabled(_ context.Context, actor management.Actor, pluginKey string, groupID int64, enabled bool, expectedVersion int64) (plugin.RuntimeStateView, error) {
	f.actor, f.pluginKey, f.groupID, f.enabled, f.expectedVersion = actor, pluginKey, groupID, enabled, expectedVersion
	if f.err != nil {
		return plugin.RuntimeStateView{}, f.err
	}
	return plugin.RuntimeStateView{PluginKey: pluginKey, DesiredEnabled: true, Status: plugin.RuntimeReady}, nil
}

// TestHandleEnablePluginCommand 验证启用命令提取参数、身份并引用回复。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandleEnablePluginCommand(t *testing.T) {
	messenger := &fakeMessenger{}
	runtimes := &fakeRuntimeManagement{states: []plugin.RuntimeStateView{{PluginKey: "ping", Version: 2}}}
	current := &EmergencyHandler{messenger: messenger, runtimes: runtimes}
	ctx := context.Background()
	event := &ws.MessageEvent{UserID: 2769731875, MessageID: 88, RawMessage: "/启用插件 ping"}
	matched, err := current.Handle(ctx, event, "/")
	if err != nil || !matched {
		t.Fatalf("Handle() = %v,%v", matched, err)
	}
	// [决策理由] 原始消息第二字段必须作为插件名传入管理服务。
	if runtimes.pluginKey != "ping" || !runtimes.enabled || runtimes.expectedVersion != 2 {
		t.Fatalf("runtime call = %q,%v,v%d", runtimes.pluginKey, runtimes.enabled, runtimes.expectedVersion)
	}
	// [决策理由] QQ用户和消息ID必须进入审计Actor。
	if runtimes.actor.ID != "2769731875" || runtimes.actor.RequestID != "88" {
		t.Fatalf("actor = %+v", runtimes.actor)
	}
	// [决策理由] 操作结果必须引用回复原命令。
	if messenger.referenceID != 88 || messenger.referenceContent != "插件 ping 已启用，实际状态 ready（版本 3）" {
		t.Fatalf("reference reply = %d,%q", messenger.referenceID, messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. /启用插件 ping -> name=ping,enabled=true -> 成功回复。
	// 2. user=2769731875,message=88 -> Actor与引用ID透传。
}

// TestHandleListPlugins 验证轻量QQ入口仍可查询插件状态。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandleListPlugins(t *testing.T) {
	messenger := &fakeMessenger{}
	runtimes := &fakeRuntimeManagement{states: []plugin.RuntimeStateView{{PluginKey: "ping", DesiredEnabled: true, Status: plugin.RuntimeReady, Version: 2}}}
	current := &EmergencyHandler{messenger: messenger, runtimes: runtimes}
	matched, err := current.Handle(context.Background(), &ws.MessageEvent{UserID: 1, MessageID: 9, RawMessage: "/插件列表"}, "/")
	if err != nil || !matched {
		t.Fatalf("Handle() = %v,%v", matched, err)
	}
	// [决策理由] 回复必须包含插件状态并引用原消息。
	if messenger.referenceID != 9 || messenger.referenceContent != "插件列表：\n- ping：意图启用，实际 ready（版本 2）" {
		t.Fatalf("reply = %d,%q", messenger.referenceID, messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. ping:true -> /插件列表 -> 显示启用。
	// 2. message_id=9 -> 引用回复列表。
}

func TestHandleStatusAndCurrentGroupCommands(t *testing.T) {
	messenger := &fakeMessenger{}
	runtimes := &fakeRuntimeManagement{states: []plugin.RuntimeStateView{{
		PluginKey: "echo", DesiredEnabled: true, Status: plugin.RuntimeReady, Version: 4, InFlight: 2,
		Groups: []plugin.RuntimeGroupView{{GroupID: 100, Enabled: false, Version: 3}},
	}}}
	current := &EmergencyHandler{messenger: messenger, runtimes: runtimes}

	if matched, err := current.Handle(context.Background(), &ws.MessageEvent{UserID: 1, MessageID: 10, RawMessage: "/插件状态 echo"}, "/"); err != nil || !matched {
		t.Fatalf("Handle() = %v,%v", matched, err)
	}
	if messenger.referenceContent != "插件 echo：意图启用，实际 ready，在途 2（版本 4）" {
		t.Fatalf("status reply = %q", messenger.referenceContent)
	}

	if matched, err := current.Handle(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 1, MessageID: 11, RawMessage: "/启用当前群插件 echo"}, "/"); err != nil || !matched {
		t.Fatalf("Handle() = %v,%v", matched, err)
	}
	if runtimes.groupID != 100 || runtimes.expectedVersion != 3 || !runtimes.enabled {
		t.Fatalf("group call = group%d enabled=%v version=%d", runtimes.groupID, runtimes.enabled, runtimes.expectedVersion)
	}

	if matched, err := current.Handle(context.Background(), &ws.MessageEvent{MessageType: "private", UserID: 1, MessageID: 12, RawMessage: "/启用当前群插件 echo"}, "/"); err != nil || !matched {
		t.Fatalf("Handle() = %v,%v", matched, err)
	}
	if messenger.referenceContent != "操作失败：当前群操作只能在群聊中使用" {
		t.Fatalf("private group reply = %q", messenger.referenceContent)
	}
}

func TestRuntimeErrorsAreSafeForQQ(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "不存在", err: plugin.ErrRuntimePluginNotFound, want: "操作失败：目标插件不存在"},
		{name: "冲突", err: plugin.ErrRuntimeStateConflict, want: "操作失败：状态已变化，请重试"},
		{name: "内部敏感错误", err: errors.New("连接 postgres://user:secret@db 失败"), want: "操作失败，请稍后重试"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messenger := &fakeMessenger{}
			current := &EmergencyHandler{messenger: messenger, runtimes: &fakeRuntimeManagement{err: test.err}}
			if matched, err := current.Handle(context.Background(), &ws.MessageEvent{MessageType: "private", UserID: 1, MessageID: 20, RawMessage: "/启用插件 echo"}, "/"); err != nil || !matched {
				t.Fatalf("Handle() = %v,%v", matched, err)
			}
			if messenger.referenceContent != test.want {
				t.Fatalf("reply = %q", messenger.referenceContent)
			}
		})
	}
}

func TestNewEmergencyHandlerRequiresDependencies(t *testing.T) {
	if instance, err := NewEmergencyHandler(&fakeMessenger{}, nil); instance != nil || err == nil {
		t.Fatalf("NewEmergencyHandler() = %v,%v", instance, err)
	}
	if instance, err := NewEmergencyHandler(&fakeMessenger{}, &fakeRuntimeManagement{}); instance == nil || err != nil {
		t.Fatalf("NewEmergencyHandler() = %v,%v", instance, err)
	}
}

// TestHandleIgnoresHeartbeatEvent 验证广播元事件不会被 admin 当作处理错误。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：构造内存插件并可能终止当前测试。
func TestHandleIgnoresUnknownCommand(t *testing.T) {
	current := &EmergencyHandler{}
	matched, err := current.Handle(context.Background(), &ws.MessageEvent{RawMessage: "/未知命令"}, "/")
	if err != nil || matched {
		t.Fatalf("Handle() = %v,%v", matched, err)
	}
}
