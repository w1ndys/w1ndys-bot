// 📌 影响范围：通过注入的 ActionCaller 调用 NapCat OneBot API；不直接管理 WebSocket 或 echo。
package onebot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// ActionCaller 是 BotAPI 依赖的最小 Action Client 能力。
type ActionCaller interface {
	Call(context.Context, string, any) (ws.ActionResponse, error)
}

// API 提供类型化 OneBot 操作。
type API struct {
	caller ActionCaller
}

// Action 是 NapCat OpenAPI 路径对应的 OneBot Action 名。
type Action string

// NapCatOpenAPIVersion 是本包 Action 与路径清单对齐的官方文档版本。
const NapCatOpenAPIVersion = "4.18.13"

// ActionError 表示 NapCat 已响应但拒绝执行操作。
type ActionError struct {
	Action  Action
	RetCode int
	Message string
	Wording string
}

// Error 返回稳定且包含 NapCat 诊断信息的错误文本。
// @param 无。
// @returns OneBot Action 失败说明。
// ⚠️副作用说明：无。
func (e *ActionError) Error() string {
	result := fmt.Sprintf("OneBot Action %s 失败: retcode=%d message=%s wording=%s", e.Action, e.RetCode, e.Message, e.Wording)

	// >>> 数据演变示例
	// 1. send_msg,retcode=100 -> 带 Action 与错误码的诊断文本。
	// 2. wording 为空 -> 保留空字段 -> 调用方仍可定位 Action。
	return result
}

// New 创建类型化 BotAPI。
// @param caller：负责 echo 和 WebSocket 请求响应的 ActionCaller。
// @returns API。
// ⚠️副作用说明：无；仅保存调用器引用。
func New(caller ActionCaller) *API {
	result := &API{caller: caller}

	// >>> 数据演变示例
	// 1. ActionClient -> API{caller} -> 可发送消息。
	// 2. fakeCaller -> API{fake} -> 可单元测试请求参数。
	return result
}

// Call 调用任意 NapCat Action，并将成功响应的 data 解码到 result。
// @param ctx：控制请求取消；action：OpenAPI 路径去掉前导斜杠后的 Action；params：请求参数；result：data 的解码目标，可为 nil。
// @returns 传输、NapCat 业务或 JSON 解码错误。
// ⚠️副作用说明：通过 ActionCaller 发送请求；具体外部副作用由 action 决定。
func (a *API) Call(ctx context.Context, action Action, params any, result any) error {
	// [决策理由] 空 Action 无法路由，且不应占用 ActionClient 的 pending 资源。
	if action == "" {
		return errors.New("Action 名称不能为空")
	}
	// [决策理由] 未注入调用器时返回可诊断错误，避免配置或测试错误触发 nil panic。
	if a == nil || a.caller == nil {
		return errors.New("ActionCaller 未配置")
	}
	response, err := a.caller.Call(ctx, string(action), params)
	// [决策理由] 传输或等待失败时不存在可信业务数据。
	if err != nil {
		return err
	}
	// [决策理由] HTTP/WebSocket 返回成功不代表 NapCat 业务执行成功。
	if !response.OK() {
		return &ActionError{Action: action, RetCode: response.RetCode, Message: response.Message, Wording: response.Wording}
	}
	// [决策理由] 无返回值 Action 或调用方不关心 data 时无需强制解析 null/空数据。
	if result == nil {
		return nil
	}
	// [决策理由] 空 data 对有结果目标的调用属于协议不完整，必须显式暴露。
	if len(response.Data) == 0 {
		return fmt.Errorf("解析 %s 响应: data 为空", action)
	}
	// [决策理由] 类型化 API 必须在边界处报告响应结构漂移。
	if err := json.Unmarshal(response.Data, result); err != nil {
		return fmt.Errorf("解析 %s 响应: %w", action, err)
	}

	// >>> 数据演变示例
	// 1. get_login_info + data{user_id:1} -> 解码到 LoginInfo -> nil。
	// 2. status=failed,retcode=100 -> ActionError -> 不解析 data。
	return nil
}

// SendMessageResult 表示 NapCat 发送消息后的返回数据。
type SendMessageResult struct {
	MessageID int64 `json:"message_id"`
}

// MessageSegment 表示一个 OneBot 11 数组消息段。
type MessageSegment struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// SendGroupMessage 向指定 QQ 群发送消息。
// @param ctx：控制请求超时；groupID：目标群号；message：OneBot 消息内容。
// @returns 新消息 ID 或 Action、业务、解析错误。
// ⚠️副作用说明：通过 NapCat 向 QQ 群发送消息。
func (a *API) SendGroupMessage(ctx context.Context, groupID int64, message any) (int64, error) {
	// [决策理由] 群号必须为正数，避免发送无法路由的 Action。
	if groupID <= 0 {
		return 0, fmt.Errorf("群号必须大于 0")
	}
	params := struct {
		GroupID int64 `json:"group_id"`
		Message any   `json:"message"`
	}{GroupID: groupID, Message: message}
	result, err := a.callAndDecode(ctx, "send_group_msg", params)

	// >>> 数据演变示例
	// 1. group=123,message=pong -> send_group_msg -> message_id=456。
	// 2. group=0 -> 参数校验 -> 返回错误且不调用 NapCat。
	return result.MessageID, err
}

// SendPrivateMessage 向指定 QQ 用户发送私聊消息。
// @param ctx：控制请求超时；userID：目标 QQ；message：OneBot 消息内容。
// @returns 新消息 ID 或 Action、业务、解析错误。
// ⚠️副作用说明：通过 NapCat 向 QQ 用户发送私聊消息。
func (a *API) SendPrivateMessage(ctx context.Context, userID int64, message any) (int64, error) {
	// [决策理由] QQ 号必须为正数，避免发送无法路由的 Action。
	if userID <= 0 {
		return 0, fmt.Errorf("QQ 号必须大于 0")
	}
	params := struct {
		UserID  int64 `json:"user_id"`
		Message any   `json:"message"`
	}{UserID: userID, Message: message}
	result, err := a.callAndDecode(ctx, "send_private_msg", params)

	// >>> 数据演变示例
	// 1. user=123,message=pong -> send_private_msg -> message_id=456。
	// 2. user=0 -> 参数校验 -> 返回错误且不调用 NapCat。
	return result.MessageID, err
}

// Reply 根据来源消息类型回复到原会话。
// @param ctx：控制请求超时；event：原始消息事件；message：回复内容。
// @returns 新消息 ID 或不支持的消息类型错误。
// ⚠️副作用说明：通过 NapCat 向原群聊或私聊发送消息。
func (a *API) Reply(ctx context.Context, event *ws.MessageEvent, message any) (int64, error) {
	// [决策理由] nil 事件没有可用回复目标。
	if event == nil {
		return 0, fmt.Errorf("回复事件不能为空")
	}
	switch event.MessageType {
	case "group":
		return a.SendGroupMessage(ctx, event.GroupID, message)
	case "private":
		return a.SendPrivateMessage(ctx, event.UserID, message)
	default:
		return 0, fmt.Errorf("不支持回复消息类型 %q", event.MessageType)
	}

	// >>> 数据演变示例
	// 1. message.group group_id=123 -> SendGroupMessage(123)。
	// 2. message.private user_id=456 -> SendPrivateMessage(456)。
}

// ReplyToMessage 向原会话发送带引用的文本回复。
// @param ctx：控制请求超时；event：用于确定群聊或私聊目标的命令事件；commandMessageID：被引用的命令消息 ID；content：回复文本。
// @returns 新消息 ID 或参数、Action、业务、解析错误。
// ⚠️副作用说明：通过 NapCat 向原会话发送 reply 与 text 消息段。
func (a *API) ReplyToMessage(ctx context.Context, event *ws.MessageEvent, commandMessageID int64, content string) (int64, error) {
	// [决策理由] OneBot reply 段必须引用有效的正数消息 ID。
	if commandMessageID <= 0 {
		return 0, fmt.Errorf("被引用消息 ID 必须大于 0")
	}
	message := []MessageSegment{
		{Type: "reply", Data: struct {
			ID string `json:"id"`
		}{ID: fmt.Sprintf("%d", commandMessageID)}},
		{Type: "text", Data: struct {
			Text string `json:"text"`
		}{Text: content}},
	}
	result, err := a.Reply(ctx, event, message)

	// >>> 数据演变示例
	// 1. group event + id=88 + "成功" -> [reply{id:"88"},text{"成功"}] -> send_group_msg。
	// 2. id=0 -> 参数校验失败 -> 不调用 NapCat。
	return result, err
}

// callAndDecode 执行发送消息 Action 并解析统一结果。
// @param ctx：请求上下文；action：发送 Action 名；params：类型化参数。
// @returns SendMessageResult 或网络、OneBot 业务与 JSON 错误。
// ⚠️副作用说明：通过 ActionCaller 发送 OneBot 请求。
func (a *API) callAndDecode(ctx context.Context, action string, params any) (SendMessageResult, error) {
	var result SendMessageResult
	err := a.Call(ctx, Action(action), params, &result)
	// [决策理由] 兼容方法需要同时返回结果零值与底层错误。
	if err != nil {
		return SendMessageResult{}, err
	}

	// >>> 数据演变示例
	// 1. status=ok,data{message_id:1} -> SendMessageResult{1}。
	// 2. status=failed,retcode=100 -> 返回业务错误，不解析 data。
	return result, nil
}
