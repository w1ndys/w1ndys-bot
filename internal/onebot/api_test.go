// 📌 影响范围：使用内存 fake ActionCaller 验证 BotAPI；不访问真实 WebSocket 或 QQ。
package onebot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeCaller struct {
	action string
	params any
	result ws.ActionResponse
	err    error
}

// TestCallDecodesDataAndReturnsTypedActionError 验证公共调用层统一处理成功数据与业务错误。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改 fakeCaller 请求记录。
func TestCallDecodesDataAndReturnsTypedActionError(t *testing.T) {
	caller := &fakeCaller{result: ws.ActionResponse{Status: "ok", Data: json.RawMessage(`{"user_id":123,"nickname":"bot"}`)}}
	api := New(caller)
	var login LoginInfo
	err := api.Call(context.Background(), ActionGetLoginInfo, nil, &login)
	// [决策理由] 成功响应必须按调用方提供的类型解码 data。
	if err != nil || login.UserID != 123 || login.Nickname != "bot" {
		t.Fatalf("Call() login=%+v err=%v", login, err)
	}
	caller.result = ws.ActionResponse{Status: "failed", RetCode: 1400, Message: "bad request", Wording: "参数错误"}
	err = api.Call(context.Background(), ActionGetStatus, nil, nil)
	var actionErr *ActionError
	// [决策理由] 调用方需要用 errors.As 稳定区分 NapCat 业务错误与网络错误。
	if !errors.As(err, &actionErr) || actionErr.Action != ActionGetStatus || actionErr.RetCode != 1400 {
		t.Fatalf("Call() error=%T %v", err, err)
	}

	// >>> 数据演变示例
	// 1. ok,data{user_id:123} -> LoginInfo{UserID:123}。
	// 2. failed,retcode=1400 -> *ActionError{Action:get_status,RetCode:1400}。
}

// TestCallValidatesActionAndResponseData 验证空 Action 与不完整 data 在本地失败。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：读取并修改 fakeCaller 请求记录。
func TestCallValidatesActionAndResponseData(t *testing.T) {
	caller := &fakeCaller{result: ws.ActionResponse{Status: "ok"}}
	api := New(caller)
	err := api.Call(context.Background(), "", nil, nil)
	// [决策理由] 空 Action 必须在调用 ActionCaller 前被拒绝。
	if err == nil || caller.action != "" {
		t.Fatalf("empty action error=%v called=%q", err, caller.action)
	}
	err = api.Call(context.Background(), ActionGetStatus, nil, &Status{})
	// [决策理由] 调用方要求结果时空 data 不能被误判为成功零值。
	if err == nil {
		t.Fatal("empty data error=nil")
	}

	// >>> 数据演变示例
	// 1. action="" -> 本地错误 -> caller 未调用。
	// 2. status=ok,data 缺失 -> data 为空错误。
}

// TestMessageMethodsRejectNonScalarIDs 验证强类型消息入口拒绝 OpenAPI 未允许的 ID 类型。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：读取 fakeCaller 请求记录。
func TestMessageMethodsRejectNonScalarIDs(t *testing.T) {
	caller := &fakeCaller{}
	api := New(caller)
	_, err := api.GetMessage(context.Background(), map[string]any{"id": 1})
	// [决策理由] object 不属于 number|string，不得发送给 NapCat。
	if err == nil || caller.action != "" {
		t.Fatalf("GetMessage() error=%v action=%q", err, caller.action)
	}
	err = api.SetMessageEmojiLike(context.Background(), SetMessageEmojiLikeParams{MessageID: 1, EmojiID: []int{2}, Set: true})
	// [决策理由] 数组表情 ID 同样必须在本地拒绝。
	if err == nil || caller.action != "" {
		t.Fatalf("SetMessageEmojiLike() error=%v action=%q", err, caller.action)
	}

	// >>> 数据演变示例
	// 1. message_id=object -> 参数错误 -> action 为空。
	// 2. emoji_id=array -> 参数错误 -> action 为空。
}

// TestValidStringOrNumberAcceptsProtocolScalarTypes 验证 ID 校验兼容官方联合类型与业务命名类型。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestValidStringOrNumberAcceptsProtocolScalarTypes(t *testing.T) {
	type namedID int64
	type namedString string
	values := []any{"88", int64(88), json.Number("88"), namedID(88), namedString("88")}
	for _, value := range values {
		// [决策理由] 每种值都可合法编码为 OpenAPI 允许的 string 或 number。
		if !validStringOrNumber(value) {
			t.Errorf("validStringOrNumber(%T(%v)) = false", value, value)
		}
	}

	// >>> 数据演变示例
	// 1. namedID(88) -> reflect.Int64 -> true。
	// 2. json.Number("88") -> JSON number 特例 -> true。
}

// TestGetLoginInfoPreservesOfficialUserFields 验证 OB11User 官方扩展字段不会被类型化入口丢弃。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改 fakeCaller 请求记录。
func TestGetLoginInfoPreservesOfficialUserFields(t *testing.T) {
	caller := &fakeCaller{result: ws.ActionResponse{Status: "ok", Data: json.RawMessage(`{"user_id":123,"nickname":"bot","phone_num":"10086","categoryName":"默认分组","categoryId":7}`)}}
	result, err := New(caller).GetLoginInfo(context.Background())
	// [决策理由] OpenAPI OB11User 的 camelCase 与 snake_case 字段都必须按原名解码。
	if err != nil || result.PhoneNumber != "10086" || result.CategoryName != "默认分组" || result.CategoryIDV2 != 7 {
		t.Fatalf("GetLoginInfo() result=%+v err=%v", result, err)
	}

	// >>> 数据演变示例
	// 1. phone_num=10086 -> UserInfo.PhoneNumber=10086。
	// 2. categoryName/categoryId -> UserInfo.CategoryName/CategoryIDV2。
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
