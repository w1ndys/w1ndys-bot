// 📌 影响范围：声明并调用 NapCat 消息扩展接口；不直接管理 WebSocket 或 echo。
package onebot

import (
	"context"
	"fmt"
)

const (
	ActionFetchEmojiLike            Action = "fetch_emoji_like"
	ActionGetEmojiLikes             Action = "get_emoji_likes"
	ActionFetchPTTText              Action = "fetch_ptt_text"
	ActionArkShareGroup             Action = "ArkShareGroup"
	ActionArkSharePeer              Action = "ArkSharePeer"
	ActionSendGroupArkShare         Action = "send_group_ark_share"
	ActionSendArkShare              Action = "send_ark_share"
	ActionSetMessageEmojiLike       Action = "set_msg_emoji_like"
	ActionClickInlineKeyboardButton Action = "click_inline_keyboard_button"
)

// SetMessageEmojiLikeParams 是设置消息表情回应的参数。
type SetMessageEmojiLikeParams struct {
	MessageID any  `json:"message_id"`
	EmojiID   any  `json:"emoji_id"`
	Set       bool `json:"set"`
}

// SetMessageEmojiLike 添加或移除消息表情回应。
// @param ctx：请求上下文；params：消息、表情及添加/移除标记。
// @returns Action、业务与解析错误。
// ⚠️副作用说明：修改指定 QQ 消息的表情回应状态。
func (a *API) SetMessageEmojiLike(ctx context.Context, params SetMessageEmojiLikeParams) error {
	// [决策理由] OpenAPI 要求消息和表情 ID 均为 number|string。
	if !validStringOrNumber(params.MessageID) || !validStringOrNumber(params.EmojiID) {
		return fmt.Errorf("消息 ID 和表情 ID 必须为非空字符串或数字")
	}
	err := a.Call(ctx, ActionSetMessageEmojiLike, params, nil)

	// >>> 数据演变示例
	// 1. {88,66,true} -> set_msg_emoji_like -> 新增回应。
	// 2. {nil,66,true} -> 参数校验失败 -> 回应不变。
	return err
}

// FetchPTTTextResult 是语音转文字结果。
type FetchPTTTextResult struct {
	Text string `json:"text"`
}

// FetchPTTText 获取指定语音消息的转写文本。
// @param ctx：请求上下文；messageID：数字或字符串消息 ID。
// @returns 转写文本，或参数、Action、业务与解析错误。
// ⚠️副作用说明：请求 NapCat 执行或查询语音识别。
func (a *API) FetchPTTText(ctx context.Context, messageID any) (string, error) {
	// [决策理由] OpenAPI 仅允许 number|string，其他 JSON 类型不能定位语音消息。
	if !validStringOrNumber(messageID) {
		return "", fmt.Errorf("消息 ID 必须为非空字符串或数字")
	}
	var result FetchPTTTextResult
	err := a.Call(ctx, ActionFetchPTTText, MessageIDParams{MessageID: messageID}, &result)

	// >>> 数据演变示例
	// 1. messageID=88 -> fetch_ptt_text -> "你好"。
	// 2. messageID=nil -> 参数校验失败 -> 不发起识别。
	return result.Text, err
}
