// 📌 影响范围：无；仅定义 OneBot 11 事件的数据结构。
package ws

// MessageEvent 表示 OneBot 11 上报事件的通用字段。
type MessageEvent struct {
	Time          int64  `json:"time"`
	SelfID        int64  `json:"self_id"`
	PostType      string `json:"post_type"`
	MessageType   string `json:"message_type"`
	NoticeType    string `json:"notice_type"`
	RequestType   string `json:"request_type"`
	MetaEventType string `json:"meta_event_type"`
	SubType       string `json:"sub_type"`
	MessageID     int64  `json:"message_id"`
	GroupID       int64  `json:"group_id"`
	UserID        int64  `json:"user_id"`
	OperatorID    int64  `json:"operator_id"`
	TargetID      int64  `json:"target_id"`
	Duration      int64  `json:"duration"`
	File          any    `json:"file"`
	Comment       string `json:"comment"`
	Flag          string `json:"flag"`
	RawMessage    string `json:"raw_message"`
}

// Name 返回由 OneBot 分类字段组成的事件名。
// @param 无。
// @returns 形如 message.group.normal 或 notice.group_ban.ban 的事件名。
// ⚠️副作用说明：无。
func (e MessageEvent) Name() string {
	category := map[string]string{
		"message":      e.MessageType,
		"message_sent": e.MessageType,
		"notice":       e.NoticeType,
		"request":      e.RequestType,
		"meta_event":   e.MetaEventType,
	}[e.PostType]
	name := e.PostType
	// [决策理由] 分类字段为空时保留 post_type，确保未知或不完整事件仍有可检索名称。
	if category != "" {
		name += "." + category
	}
	// [决策理由] sub_type 只在存在时追加，避免生成末尾带点的无效分类名。
	if e.SubType != "" {
		name += "." + e.SubType
	}

	// >>> 数据演变示例
	// 1. post_type=message,message_type=group,sub_type=normal -> message.group.normal。
	// 2. post_type=meta_event,meta_event_type=heartbeat -> meta_event.heartbeat。
	return name
}
