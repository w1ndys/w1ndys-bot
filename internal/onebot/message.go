// 📌 影响范围：通过注入的 ActionCaller 调用 NapCat 消息接口；不直接管理 WebSocket 或 echo。
package onebot

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

// GetMessageParams 是获取消息的参数。
type GetMessageParams struct {
	MessageID any `json:"message_id"`
}

// MessageInfo 是 NapCat 返回的消息摘要。
type MessageInfo struct {
	Time           int64           `json:"time"`
	MessageType    string          `json:"message_type"`
	MessageID      int64           `json:"message_id"`
	RealID         int64           `json:"real_id"`
	MessageSeq     int64           `json:"message_seq"`
	Sender         json.RawMessage `json:"sender"`
	Message        json.RawMessage `json:"message"`
	RawMessage     string          `json:"raw_message"`
	Font           int64           `json:"font"`
	GroupID        any             `json:"group_id,omitempty"`
	UserID         any             `json:"user_id"`
	EmojiLikesList []any           `json:"emoji_likes_list,omitempty"`
}

// GetMessage 获取指定消息。
// @param ctx：请求上下文；messageID：数字或字符串消息 ID。
// @returns 消息详情，或参数、Action、业务与解析错误。
// ⚠️副作用说明：通过 ActionCaller 查询 NapCat 消息数据。
func (a *API) GetMessage(ctx context.Context, messageID any) (MessageInfo, error) {
	// [决策理由] OpenAPI 仅允许 number|string，其他 JSON 类型会被 NapCat 拒绝。
	if !validStringOrNumber(messageID) {
		return MessageInfo{}, fmt.Errorf("消息 ID 必须为非空字符串或数字")
	}
	var result MessageInfo
	err := a.Call(ctx, ActionGetMessage, GetMessageParams{MessageID: messageID}, &result)

	// >>> 数据演变示例
	// 1. messageID=88 -> get_msg -> MessageInfo{MessageID:88}。
	// 2. messageID=nil -> 参数校验失败 -> 不调用 NapCat。
	return result, err
}

// MessageIDParams 是仅需消息 ID 的操作参数。
type MessageIDParams struct {
	MessageID any `json:"message_id"`
}

// DeleteMessage 撤回指定消息。
// @param ctx：请求上下文；messageID：数字或字符串消息 ID。
// @returns Action、业务与解析错误。
// ⚠️副作用说明：通过 NapCat 撤回 QQ 消息。
func (a *API) DeleteMessage(ctx context.Context, messageID any) error {
	// [决策理由] OpenAPI 仅允许 number|string，其他 JSON 类型不能定位消息。
	if !validStringOrNumber(messageID) {
		return fmt.Errorf("消息 ID 必须为非空字符串或数字")
	}
	err := a.Call(ctx, ActionDeleteMessage, MessageIDParams{MessageID: messageID}, nil)

	// >>> 数据演变示例
	// 1. messageID=88 -> delete_msg -> 消息被撤回。
	// 2. messageID=nil -> 参数校验失败 -> 消息不变。
	return err
}

// MarkMessageAsRead 标记指定消息为已读。
// @param ctx：请求上下文；messageID：数字或字符串消息 ID。
// @returns Action、业务与解析错误。
// ⚠️副作用说明：更新 NapCat/QQ 会话的消息已读状态。
func (a *API) MarkMessageAsRead(ctx context.Context, messageID any) error {
	// [决策理由] OpenAPI 仅允许 number|string，其他 JSON 类型不能定位消息。
	if !validStringOrNumber(messageID) {
		return fmt.Errorf("消息 ID 必须为非空字符串或数字")
	}
	err := a.Call(ctx, ActionMarkMessageAsRead, MessageIDParams{MessageID: messageID}, nil)

	// >>> 数据演变示例
	// 1. messageID="88" -> mark_msg_as_read -> 消息变为已读。
	// 2. messageID=nil -> 参数校验失败 -> 已读状态不变。
	return err
}

// validStringOrNumber 判断动态值是否符合 OpenAPI 的 string|number 联合类型。
// @param value：待验证的消息、表情或其他协议标识。
// @returns 非空字符串或 Go 数字类型返回 true。
// ⚠️副作用说明：无。
func validStringOrNumber(value any) bool {
	// [决策理由] json.Number 的底层是 string，但协议语义是 JSON number，应优先识别。
	if _, ok := value.(json.Number); ok {
		return true
	}
	// [决策理由] reflect.Kind 同时支持内建类型和以它们为底层类型的业务 ID。
	if value == nil {
		return false
	}
	switch reflected := reflect.ValueOf(value); reflected.Kind() {
	case reflect.String:
		return reflected.String() != ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}

	// >>> 数据演变示例
	// 1. int64(88) -> number 分支 -> true。
	// 2. map[string]any{} -> default 分支 -> false。
}

// MarkAllMessagesAsRead 标记所有消息为已读。
// @param ctx：请求上下文。
// @returns Action、业务与解析错误。
// ⚠️副作用说明：批量更新 NapCat/QQ 的消息已读状态。
func (a *API) MarkAllMessagesAsRead(ctx context.Context) error {
	err := a.Call(ctx, ActionMarkAllMessagesAsRead, struct{}{}, nil)

	// >>> 数据演变示例
	// 1. 存在 2 个未读会话 -> _mark_all_as_read -> 全部已读。
	// 2. 不存在未读会话 -> _mark_all_as_read -> 状态保持不变。
	return err
}
