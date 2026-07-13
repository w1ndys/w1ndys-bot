// 📌 影响范围：无；使用内存 Messenger 与 Management 验证 QQ 应急管理命令。
package admin

import (
	"context"
	"encoding/json"
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

type fakeManagement struct {
	states  []management.PluginState
	actor   management.Actor
	name    string
	enabled bool
}

// ListAuditLogs 满足完整管理契约；QQ 应急插件不调用审计查询。
// @param ctx：未使用的上下文；actor：操作者；query：审计查询。
// @returns 空审计页与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) ListAuditLogs(context.Context, management.Actor, management.AuditQuery) (management.AuditPage, error) {
	page := management.AuditPage{Items: []management.AuditState{}, Page: 1, PageSize: 20}

	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 空分页。
	// 2. WebUI审计查询 -> 由AdminService负责。
	return page, nil
}

// GetAuditLog 满足完整管理契约；QQ 应急插件不调用审计详情。
// @param ctx：未使用的上下文；actor：操作者；id：审计ID。
// @returns 零值审计记录与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) GetAuditLog(context.Context, management.Actor, int64) (management.AuditState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 零值记录。
	// 2. WebUI审计详情 -> 由AdminService负责。
	return management.AuditState{}, nil
}

// ListPlugins 返回测试插件列表并记录操作者。
// @param ctx：未使用的上下文；actor：管理操作者。
// @returns 预设插件列表与nil。
// ⚠️副作用说明：记录actor。
func (f *fakeManagement) ListPlugins(_ context.Context, actor management.Actor) ([]management.PluginState, error) {
	f.actor = actor

	// >>> 数据演变示例
	// 1. states=[ping] -> 返回[ping]。
	// 2. actor=100 -> 记录100。
	return f.states, nil
}

// SetPluginEnabled 记录插件启停操作。
// @param ctx：未使用的上下文；actor：操作者；name：插件名；enabled：目标状态。
// @returns 更新后的测试状态与nil。
// ⚠️副作用说明：记录actor、name和enabled。
func (f *fakeManagement) SetPluginEnabled(_ context.Context, actor management.Actor, name string, enabled bool) (management.PluginState, error) {
	f.actor, f.name, f.enabled = actor, name, enabled

	// >>> 数据演变示例
	// 1. ping,true -> 记录启用 -> 返回ping:true。
	// 2. ping,false -> 记录禁用 -> 返回ping:false。
	return management.PluginState{Name: name, Enabled: enabled, Priority: 100}, nil
}

// SetPluginPriority 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者；name：插件名；priority：优先级。
// @returns 零值插件状态与nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) SetPluginPriority(context.Context, management.Actor, string, int) (management.PluginState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 零值。
	// 2. WebUI接口测试由AdminService负责。
	return management.PluginState{}, nil
}

// ListCommands 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者。
// @returns 空列表与nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) ListCommands(context.Context, management.Actor) ([]management.CommandState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 空列表。
	// 2. WebUI命令管理由AdminService负责。
	return nil, nil
}

// CreateCommand 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者；input：命令输入。
// @returns 零值命令与nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) CreateCommand(context.Context, management.Actor, management.CommandCreate) (management.CommandState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 零值。
	// 2. WebUI创建命令由AdminService负责。
	return management.CommandState{}, nil
}

// RenameCommand 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者；id：命令ID；command：新命令。
// @returns 零值命令与nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) RenameCommand(context.Context, management.Actor, int64, string) (management.CommandState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 零值。
	// 2. WebUI改名由AdminService负责。
	return management.CommandState{}, nil
}

// DeleteCommand 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者；id：命令ID。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) DeleteCommand(context.Context, management.Actor, int64) error {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> nil。
	// 2. WebUI删除由AdminService负责。
	return nil
}

// ListPermissions 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者。
// @returns 空列表与nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) ListPermissions(context.Context, management.Actor) ([]management.PermissionState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 空列表。
	// 2. WebUI权限查询由AdminService负责。
	return nil, nil
}

// SetPermission 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者；input：权限输入。
// @returns 零值策略与nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) SetPermission(context.Context, management.Actor, management.PermissionSet) (management.PermissionState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 零值。
	// 2. WebUI权限写入由AdminService负责。
	return management.PermissionState{}, nil
}

// DeletePermission 满足完整WebUI管理契约；QQ插件不调用。
// @param ctx：未使用的上下文；actor：操作者；id：策略ID。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) DeletePermission(context.Context, management.Actor, int64) error {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> nil。
	// 2. WebUI权限删除由AdminService负责。
	return nil
}

// ListSettings 满足完整 WebUI 管理契约；QQ 插件不调用。
// @param ctx：未使用的上下文；actor：操作者。
// @returns 空列表与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) ListSettings(context.Context, management.Actor) ([]management.SettingState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 空列表。
	// 2. WebUI设置查询由AdminService负责。
	return nil, nil
}

// SetSetting 满足完整 WebUI 管理契约；QQ 插件不调用。
// @param ctx：未使用的上下文；actor：操作者；key：设置键；value：JSON值。
// @returns 零值设置与 nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) SetSetting(context.Context, management.Actor, string, json.RawMessage) (management.SettingState, error) {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> 零值。
	// 2. WebUI设置写入由AdminService负责。
	return management.SettingState{}, nil
}

// DeleteSetting 满足完整 WebUI 管理契约；QQ 插件不调用。
// @param ctx：未使用的上下文；actor：操作者；key：设置键。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeManagement) DeleteSetting(context.Context, management.Actor, string) error {
	// >>> 数据演变示例
	// 1. QQ应急插件 -> 不调用 -> nil。
	// 2. WebUI删除设置由AdminService负责。
	return nil
}

// TestHandleEnablePluginCommand 验证启用命令提取参数、身份并引用回复。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存插件并可能终止当前测试。
func TestHandleEnablePluginCommand(t *testing.T) {
	messenger := &fakeMessenger{}
	managementService := &fakeManagement{}
	current := &implementation{messenger: messenger, management: managementService}
	ctx := plugin.WithFeature(context.Background(), featureEnable)
	event := &ws.MessageEvent{UserID: 2769731875, MessageID: 88, RawMessage: "/启用插件 ping"}
	err := current.Handle(ctx, event)
	// [决策理由] 成功处理以ErrStopPropagation表示命令已消费。
	if err != plugin.ErrStopPropagation {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] 原始消息第二字段必须作为插件名传入管理服务。
	if managementService.name != "ping" || !managementService.enabled {
		t.Fatalf("management call = %q,%v", managementService.name, managementService.enabled)
	}
	// [决策理由] QQ用户和消息ID必须进入审计Actor。
	if managementService.actor.ID != "2769731875" || managementService.actor.RequestID != "88" {
		t.Fatalf("actor = %+v", managementService.actor)
	}
	// [决策理由] 操作结果必须引用回复原命令。
	if messenger.referenceID != 88 || messenger.referenceContent != "插件 ping 已启用（优先级 100）" {
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
	managementService := &fakeManagement{states: []management.PluginState{{Name: "ping", Enabled: true, Priority: 100}}}
	current := &implementation{messenger: messenger, management: managementService}
	ctx := plugin.WithFeature(context.Background(), featureList)
	err := current.Handle(ctx, &ws.MessageEvent{UserID: 1, MessageID: 9, RawMessage: "/插件列表"})
	// [决策理由] 列表命令成功后应停止传播。
	if err != plugin.ErrStopPropagation {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] 回复必须包含插件状态并引用原消息。
	if messenger.referenceID != 9 || messenger.referenceContent != "插件列表：\n- ping：启用（优先级 100）" {
		t.Fatalf("reply = %d,%q", messenger.referenceID, messenger.referenceContent)
	}

	// >>> 数据演变示例
	// 1. ping:true -> /插件列表 -> 显示启用。
	// 2. message_id=9 -> 引用回复列表。
}
