// 📌 影响范围：通过注入的 ActionCaller 读取 NapCat Go-CQHTTP 兼容的群历史消息；不修改 QQ 数据。
package onebot

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetGroupMessageHistoryParams 表示获取群历史消息的 NapCat 4.18.13 请求参数。
type GetGroupMessageHistoryParams struct {
	GroupID        string `json:"group_id"`
	MessageSeq     string `json:"message_seq,omitempty"`
	Count          int    `json:"count"`
	ReverseOrder   bool   `json:"reverse_order"`
	DisableGetURL  bool   `json:"disable_get_url"`
	ParseMultMsg   bool   `json:"parse_mult_msg"`
	QuickReply     bool   `json:"quick_reply"`
	ReverseOrderV1 bool   `json:"reverseOrder"`
}

// GroupHistoryMessage 保留违规处置定位发送者、时间和撤回目标所需的历史消息字段。
type GroupHistoryMessage struct {
	Time       int64           `json:"time"`
	MessageID  int64           `json:"message_id"`
	UserID     int64           `json:"user_id"`
	RawMessage string          `json:"raw_message"`
	Sender     json.RawMessage `json:"sender"`
	Message    json.RawMessage `json:"message"`
}

// GetGroupMessageHistoryResult 表示群历史消息 Action 的业务数据。
type GetGroupMessageHistoryResult struct {
	Messages []GroupHistoryMessage `json:"messages"`
}

// GetGroupMessageHistory 获取一页群历史消息。
// @param ctx：请求上下文；params：群号、起始消息序号、页大小和 NapCat 解析开关。
// @returns 包含可撤回消息标识、发送者、时间与原文的结果，或参数、Action、业务与解析错误。
// ⚠️副作用说明：通过 NapCat 读取群聊记录；disable_get_url=false 时 NapCat 可能解析媒体 URL。
func (a *API) GetGroupMessageHistory(ctx context.Context, params GetGroupMessageHistoryParams) (GetGroupMessageHistoryResult, error) {
	// [决策理由] OpenAPI 要求 group_id 为必填字符串。
	if params.GroupID == "" {
		return GetGroupMessageHistoryResult{}, fmt.Errorf("群号不能为空")
	}
	// [决策理由] 限制单次 1..100 条可避免无效请求和无界响应内存占用。
	if params.Count < 1 || params.Count > 100 {
		return GetGroupMessageHistoryResult{}, fmt.Errorf("历史消息数量必须在1到100之间")
	}
	var result GetGroupMessageHistoryResult
	err := a.Call(ctx, ActionGetGroupMessageHistory, params, &result)

	// >>> 数据演变示例
	// 1. {group_id:100,count:30} -> data.messages 30条 -> 保留message_id/user_id/time/raw_message/sender。
	// 2. {group_id:100,count:101} -> 本地边界错误 -> 不调用 NapCat。
	return result, err
}
