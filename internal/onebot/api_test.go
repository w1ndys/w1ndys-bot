// 📌 影响范围：使用内存 fake ActionCaller 验证 BotAPI；不访问真实 WebSocket 或 QQ。
package onebot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeCaller struct {
	action string
	params any
	result ws.ActionResponse
	err    error
}

// Call 记录 Action 并返回预设响应。
// @param context.Context：测试上下文；action：操作名；params：操作参数。
// @returns 预设 ActionResponse 和错误。
// ⚠️副作用说明：修改 fakeCaller 的 action 和 params。
func (f *fakeCaller) Call(_ context.Context, action string, params any) (ws.ActionResponse, error) {
	f.action = action
	f.params = params

	// >>> 数据演变示例
	// 1. send_group_msg -> 记录 action/params -> 返回成功响应。
	// 2. 预设 err -> 记录请求 -> 返回错误。
	return f.result, f.err
}

// TestReplyRoutesByMessageType 验证群聊和私聊回复选择正确 Action。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改 fakeCaller 请求记录。
func TestReplyRoutesByMessageType(t *testing.T) {
	caller := &fakeCaller{result: ws.ActionResponse{Status: "ok", Data: json.RawMessage(`{"message_id":99}`)}}
	api := New(caller)
	groupEvent := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", GroupID: 123}
	messageID, err := api.Reply(context.Background(), groupEvent, "pong")
	// [决策理由] 群回复必须成功返回消息 ID 并选择 send_group_msg。
	if err != nil || messageID != 99 || caller.action != "send_group_msg" {
		t.Fatalf("群回复结果错误: id=%d action=%s err=%v", messageID, caller.action, err)
	}
	privateEvent := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "private", UserID: 456}
	_, err = api.Reply(context.Background(), privateEvent, "pong")
	// [决策理由] 私聊回复必须选择 send_private_msg。
	if err != nil || caller.action != "send_private_msg" {
		t.Fatalf("私聊回复 action=%s err=%v", caller.action, err)
	}

	// >>> 数据演变示例
	// 1. group event -> send_group_msg -> message_id=99。
	// 2. private event -> send_private_msg -> message_id=99。
}

// TestSendMessageReturnsActionFailure 验证 NapCat 业务错误不会被当成成功。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改 fakeCaller 请求记录。
func TestSendMessageReturnsActionFailure(t *testing.T) {
	caller := &fakeCaller{result: ws.ActionResponse{Status: "failed", RetCode: 100, Message: "failed"}}
	api := New(caller)
	_, err := api.SendGroupMessage(context.Background(), 123, "pong")
	// [决策理由] status=failed 必须转换为业务错误。
	if err == nil {
		t.Fatal("失败 Action 未返回错误")
	}

	// >>> 数据演变示例
	// 1. status=failed -> response.OK=false -> 返回错误。
	// 2. status=ok -> 解析 data -> 返回 message_id。
}

// TestReplyToMessageBuildsQuotedSegments 验证引用回复自动构造 reply 与 text 消息段。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改 fakeCaller 请求记录。
func TestReplyToMessageBuildsQuotedSegments(t *testing.T) {
	caller := &fakeCaller{result: ws.ActionResponse{Status: "ok", Data: json.RawMessage(`{"message_id":99}`)}}
	api := New(caller)
	event := &ws.MessageEvent{MessageType: "group", GroupID: 123, MessageID: 88}
	messageID, err := api.ReplyToMessage(context.Background(), event, event.MessageID, "操作成功")
	// [决策理由] 引用回复必须使用原群目标并返回 NapCat 新消息 ID。
	if err != nil || messageID != 99 || caller.action != "send_group_msg" {
		t.Fatalf("ReplyToMessage() id=%d action=%s err=%v", messageID, caller.action, err)
	}
	encoded, err := json.Marshal(caller.params)
	// [决策理由] 测试需要检查最终传给 ActionCaller 的 JSON 协议形态。
	if err != nil {
		t.Fatalf("Marshal(params) error = %v", err)
	}
	want := `{"group_id":123,"message":[{"type":"reply","data":{"id":"88"}},{"type":"text","data":{"text":"操作成功"}}]}`
	// [决策理由] reply 必须位于文本前，且引用 ID 按 OneBot 字符串字段编码。
	if string(encoded) != want {
		t.Fatalf("params = %s, want %s", encoded, want)
	}

	// >>> 数据演变示例
	// 1. event.message_id=88 + 操作成功 -> reply{id:"88"}+text -> send_group_msg。
	// 2. NapCat message_id=99 -> ReplyToMessage返回99。
}

// TestReplyToMessageRejectsInvalidReference 验证无效引用 ID 不会发送 Action。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：读取 fakeCaller 请求记录并可能终止测试。
func TestReplyToMessageRejectsInvalidReference(t *testing.T) {
	caller := &fakeCaller{}
	api := New(caller)
	_, err := api.ReplyToMessage(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 123}, 0, "失败")
	// [决策理由] 无效消息 ID 必须返回参数错误。
	if err == nil {
		t.Fatal("ReplyToMessage() error = nil")
	}
	// [决策理由] 本地校验失败不得产生任何 NapCat Action。
	if caller.action != "" {
		t.Fatalf("unexpected action = %s", caller.action)
	}

	// >>> 数据演变示例
	// 1. id=0 -> 校验失败 -> error且action为空。
	// 2. id=-1 -> 同样拒绝 -> 不发送消息。
}
