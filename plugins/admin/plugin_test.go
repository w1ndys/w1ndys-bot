// 📌 影响范围：无；使用内存 Messenger 与 Management 验证 QQ 管理命令处理。
package admin

import (
	"context"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeMessenger struct {
	message          any
	referenceID      int64
	referenceContent string
}

// ReplyToMessage 记录测试中的引用回复消息 ID 和内容。
// @param ctx：未使用的上下文；event：原消息；messageID：引用消息 ID；content：回复文本。
// @returns 固定新消息 ID 与 nil。
// ⚠️副作用说明：修改引用回复记录字段。
func (f *fakeMessenger) ReplyToMessage(_ context.Context, _ *ws.MessageEvent, messageID int64, content string) (int64, error) {
	f.referenceID = messageID
	f.referenceContent = content

	// >>> 数据演变示例
	// 1. id=88,content=成功 -> 记录字段 -> 1,nil。
	// 2. id=1,content="" -> 记录空文本 -> 1,nil。
	return 1, nil
}

// Reply 记录测试中的 QQ 回复内容。
// @param ctx：未使用的测试上下文；event：原消息；message：回复内容。
// @returns 固定消息 ID 与 nil。
// ⚠️副作用说明：修改 fakeMessenger.message。
func (f *fakeMessenger) Reply(_ context.Context, _ *ws.MessageEvent, message any) (int64, error) {
	f.message = message

	// >>> 数据演变示例
	// 1. Reply("成功") -> message="成功" -> 1,nil。
	// 2. Reply(nil) -> message=nil -> 1,nil。
	return 1, nil
}

type fakeManagement struct {
	states       []management.PluginState
	actor        management.Actor
	name         string
	enabled      bool
	priority     int
	commandInput management.CommandCreate
	commandID    int64
	commandText  string
}

// ListPlugins 返回测试插件列表并记录操作者。
// @param ctx：未使用的上下文；actor：管理操作者。
// @returns 预设插件列表与 nil。
// ⚠️副作用说明：记录 actor。
func (f *fakeManagement) ListPlugins(_ context.Context, actor management.Actor) ([]management.PluginState, error) {
	f.actor = actor

	// >>> 数据演变示例
	// 1. states=[ping] -> List -> [ping],nil。
	// 2. actor=100 -> 记录100 -> 返回列表。
	return f.states, nil
}

// SetPluginEnabled 记录插件启停操作。
// @param ctx：未使用的上下文；actor：操作者；name：插件名；enabled：目标状态。
// @returns 更新后的测试状态与 nil。
// ⚠️副作用说明：记录 actor、name 和 enabled。
func (f *fakeManagement) SetPluginEnabled(_ context.Context, actor management.Actor, name string, enabled bool) (management.PluginState, error) {
	f.actor, f.name, f.enabled = actor, name, enabled

	// >>> 数据演变示例
	// 1. ping,false -> 记录禁用 -> 返回ping:false。
	// 2. ping,true -> 记录启用 -> 返回ping:true。
	return management.PluginState{Name: name, Enabled: enabled, Priority: 100}, nil
}

// SetPluginPriority 记录插件优先级操作。
// @param ctx：未使用的上下文；actor：操作者；name：插件名；priority：目标优先级。
// @returns 更新后的测试状态与 nil。
// ⚠️副作用说明：记录 actor、name 和 priority。
func (f *fakeManagement) SetPluginPriority(_ context.Context, actor management.Actor, name string, priority int) (management.PluginState, error) {
	f.actor, f.name, f.priority = actor, name, priority

	// >>> 数据演变示例
	// 1. ping,200 -> 记录优先级 -> 返回ping:200。
	// 2. admin,1000 -> 记录优先级 -> 返回admin:1000。
	return management.PluginState{Name: name, Enabled: true, Priority: priority}, nil
}

// ListCommands 返回空测试命令列表。
// @param ctx：未使用的上下文；actor：操作者。
// @returns 空列表与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) ListCommands(context.Context, management.Actor) ([]management.CommandState, error) {
	// >>> 数据演变示例
	// 1. 查询命令 -> [] -> nil。
	// 2. 无预设数据 -> 保持空列表。
	return nil, nil
}

// CreateCommand 返回空测试命令。
// @param ctx：未使用的上下文；actor：操作者；input：命令输入。
// @returns 零值命令与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) CreateCommand(_ context.Context, _ management.Actor, input management.CommandCreate) (management.CommandState, error) {
	f.commandInput = input
	// >>> 数据演变示例
	// 1. CreateCommand(test) -> 记录input -> id=9,nil。
	// 2. 带空格命令 -> 原文本保存在Command字段。
	return management.CommandState{ID: 9, PluginName: input.PluginName, FeatureKey: input.FeatureKey, Command: input.Command}, nil
}

// RenameCommand 返回空测试命令。
// @param ctx：未使用的上下文；actor：操作者；id：命令 ID；command：新文本。
// @returns 零值命令与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) RenameCommand(_ context.Context, _ management.Actor, id int64, command string) (management.CommandState, error) {
	f.commandID, f.commandText = id, command
	// >>> 数据演变示例
	// 1. RenameCommand -> 零值,nil。
	// 2. 当前插件测试未调用 -> 无状态变化。
	return management.CommandState{ID: id, Command: command}, nil
}

// DeleteCommand 返回测试成功。
// @param ctx：未使用的上下文；actor：操作者；id：命令 ID。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) DeleteCommand(_ context.Context, _ management.Actor, id int64) error {
	f.commandID = id
	// >>> 数据演变示例
	// 1. DeleteCommand -> nil。
	// 2. 当前插件测试未调用 -> 无状态变化。
	return nil
}

// TestHandleCreateGroupCommandPreservesCommandWords 验证群级新增命令解析目标和多词文本。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandleCreateGroupCommandPreservesCommandWords(t *testing.T) {
	messenger := &fakeMessenger{}
	managementService := &fakeManagement{}
	current := &implementation{messenger: messenger, management: managementService}
	ctx := plugin.WithFeature(context.Background(), featureCommandCreateGroup)
	err := current.Handle(ctx, &ws.MessageEvent{UserID: 2769731875, MessageID: 90, RawMessage: "/新增群命令 123 ping ping 测 试"})
	// [决策理由] 创建成功后管理事件应停止继续传播。
	if err != plugin.ErrStopPropagation {
		t.Fatalf("Handle() error = %v", err)
	}
	input := managementService.commandInput
	// [决策理由] 群号、插件和功能必须按固定参数位置解析。
	if input.ScopeType != "group" || input.ScopeID != "123" || input.PluginName != "ping" || input.FeatureKey != "ping" {
		t.Fatalf("command input = %+v", input)
	}
	// [决策理由] 命令参数后的所有词都属于命令文本，不能只保留第一个词。
	if input.Command != "测 试" {
		t.Fatalf("command text = %q", input.Command)
	}
	// [决策理由] 创建结果必须引用回复原命令消息。
	if messenger.referenceID != 90 || messenger.referenceContent != "命令 #9 已创建：测 试 → ping.ping" {
		t.Fatalf("reply = %d,%q", messenger.referenceID, messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. 群123+ping.ping+“测 试” -> CommandCreate -> id=9。
	// 2. message_id=90 -> 引用回复创建结果。
}

// TestHandleDeleteCommandRejectsInvalidID 验证删除命令在无效 ID 时提前终止。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandleDeleteCommandRejectsInvalidID(t *testing.T) {
	messenger := &fakeMessenger{}
	managementService := &fakeManagement{}
	current := &implementation{messenger: messenger, management: managementService}
	ctx := plugin.WithFeature(context.Background(), featureCommandDelete)
	err := current.Handle(ctx, &ws.MessageEvent{MessageID: 91, RawMessage: "/删除命令 abc"})
	// [决策理由] 参数错误已回复，事件仍应视为消费完成。
	if err != plugin.ErrStopPropagation {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] 非数字 ID 不得调用删除事务。
	if managementService.commandID != 0 {
		t.Fatalf("DeleteCommand unexpectedly called with %d", managementService.commandID)
	}
	// [决策理由] 用户应收到明确的正整数格式提示。
	if messenger.referenceContent != "操作失败：命令 ID 必须是正整数" {
		t.Fatalf("reply = %q", messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. id=abc -> ParseInt失败 -> 不调用Service。
	// 2. 参数错误 -> 引用回复明确提示。
}

// TestHandleEnablePluginCommand 验证启用命令提取参数、身份并回复结果。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandleEnablePluginCommand(t *testing.T) {
	messenger := &fakeMessenger{}
	managementService := &fakeManagement{}
	current := &implementation{messenger: messenger, management: managementService}
	ctx := plugin.WithFeature(context.Background(), featureEnable)
	event := &ws.MessageEvent{UserID: 2769731875, MessageID: 88, RawMessage: "/启用插件 ping"}
	err := current.Handle(ctx, event)
	// [决策理由] 成功处理以 ErrStopPropagation 表示命令已消费。
	if err != plugin.ErrStopPropagation {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] 原始消息的第二字段必须作为插件名传入管理服务。
	if managementService.name != "ping" || !managementService.enabled {
		t.Fatalf("management call = %q,%v", managementService.name, managementService.enabled)
	}
	// [决策理由] QQ 用户和消息 ID 必须进入审计 Actor。
	if managementService.actor.ID != "2769731875" || managementService.actor.RequestID != "88" {
		t.Fatalf("actor = %+v", managementService.actor)
	}
	// [决策理由] 成功操作必须向用户返回可理解结果。
	if messenger.referenceID != 88 || messenger.referenceContent != "插件 ping 已启用（优先级 100）" {
		t.Fatalf("reference reply = %d,%q", messenger.referenceID, messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. /启用插件 ping -> name=ping,enabled=true -> 成功回复。
	// 2. user=2769731875,message=88 -> Actor ID与RequestID透传。
}

// TestHandlePriorityRejectsInvalidNumber 验证非法优先级不会调用管理服务。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandlePriorityRejectsInvalidNumber(t *testing.T) {
	messenger := &fakeMessenger{}
	managementService := &fakeManagement{}
	current := &implementation{messenger: messenger, management: managementService}
	ctx := plugin.WithFeature(context.Background(), featurePriority)
	err := current.Handle(ctx, &ws.MessageEvent{UserID: 1, RawMessage: "/设置插件优先级 ping high"})
	// [决策理由] 参数错误已回复用户，事件仍应视为消费完成。
	if err != plugin.ErrStopPropagation {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] 非整数参数必须在进入管理事务前停止。
	if managementService.name != "" {
		t.Fatalf("management unexpectedly called for %q", managementService.name)
	}
	// [决策理由] 用户应收到明确整数格式提示。
	if messenger.referenceContent != "操作失败：优先级必须是整数" {
		t.Fatalf("reply = %q", messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. priority=high -> Atoi失败 -> 回复格式错误。
	// 2. 参数失败 -> Management未调用 -> name为空。
}
