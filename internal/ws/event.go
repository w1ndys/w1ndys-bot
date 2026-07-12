// 📌 影响范围：解析 NapCat OneBot 11 JSON；不访问网络、数据库或外部变量。
package ws

import (
	"encoding/json"
	"fmt"
)

// Event 是所有 OneBot 11 上报事件的统一路由接口。
type Event interface {
	Name() string
	Base() BaseEvent
}

// BaseEvent 保存所有 OneBot 事件共有的信封字段。
type BaseEvent struct {
	Time     int64  `json:"time"`
	SelfID   int64  `json:"self_id"`
	PostType string `json:"post_type"`
}

// Base 返回事件公共信封。
// @param 无。
// @returns BaseEvent 自身。
// ⚠️副作用说明：无。
func (e BaseEvent) Base() BaseEvent {
	// >>> 数据演变示例
	// 1. BaseEvent{SelfID:1} -> Base -> BaseEvent{SelfID:1}。
	// 2. BaseEvent{} -> Base -> 零值 BaseEvent。
	return e
}

type MessageEvent struct {
	BaseEvent
	MessageType string `json:"message_type"`
	SubType     string `json:"sub_type"`
	MessageID   int64  `json:"message_id"`
	UserID      int64  `json:"user_id"`
	GroupID     int64  `json:"group_id"`
	RawMessage  string `json:"raw_message"`
}

// Name 返回消息事件分层名称。
// @param 无。
// @returns message 或 message_sent 分类名称。
// ⚠️副作用说明：无。
func (e MessageEvent) Name() string {
	// >>> 数据演变示例
	// 1. message+group+normal -> message.group.normal。
	// 2. message_sent+private+friend -> message_sent.private.friend。
	return joinName(e.PostType, e.MessageType, e.SubType)
}

type LifecycleEvent struct {
	BaseEvent
	MetaEventType string `json:"meta_event_type"`
	SubType       string `json:"sub_type"`
}

// Name 返回生命周期事件名称。
// @param 无。
// @returns meta_event.lifecycle 加可选子类型。
// ⚠️副作用说明：无。
func (e LifecycleEvent) Name() string {
	// >>> 数据演变示例
	// 1. lifecycle+connect -> meta_event.lifecycle.connect。
	// 2. lifecycle+空 -> meta_event.lifecycle。
	return joinName(e.PostType, e.MetaEventType, e.SubType)
}

type HeartbeatStatus struct {
	Online *bool `json:"online"`
	Good   bool  `json:"good"`
}

// String 返回适合日志展示的心跳状态。
// @param 无。
// @returns online 与 good 的可读文本；online 未上报时显示 unknown。
// ⚠️副作用说明：无。
func (s HeartbeatStatus) String() string {
	online := "unknown"
	// [决策理由] NapCat 源码允许 online 为 undefined，只有非 nil 时才能安全解引用。
	if s.Online != nil {
		online = fmt.Sprintf("%t", *s.Online)
	}

	// >>> 数据演变示例
	// 1. Online=&true,Good=true -> online=true good=true。
	// 2. Online=nil,Good=false -> online=unknown good=false。
	return fmt.Sprintf("online=%s good=%t", online, s.Good)
}

type HeartbeatEvent struct {
	BaseEvent
	MetaEventType string          `json:"meta_event_type"`
	Status        HeartbeatStatus `json:"status"`
	Interval      int64           `json:"interval"`
}

// Name 返回心跳事件名称。
// @param 无。
// @returns meta_event.heartbeat。
// ⚠️副作用说明：无。
func (e HeartbeatEvent) Name() string {
	// >>> 数据演变示例
	// 1. heartbeat -> Name -> meta_event.heartbeat。
	// 2. interval=30000 -> Name 不受载荷影响 -> meta_event.heartbeat。
	return joinName(e.PostType, e.MetaEventType)
}

type FriendRequestEvent struct {
	BaseEvent
	RequestType string `json:"request_type"`
	UserID      int64  `json:"user_id"`
	Comment     string `json:"comment"`
	Flag        string `json:"flag"`
}

// Name 返回好友请求名称。
// @param 无。
// @returns request.friend。
// ⚠️副作用说明：无。
func (e FriendRequestEvent) Name() string {
	// >>> 数据演变示例
	// 1. request_type=friend -> request.friend。
	// 2. comment=hello -> 名称不受载荷影响 -> request.friend。
	return joinName(e.PostType, e.RequestType)
}

type GroupRequestEvent struct {
	BaseEvent
	RequestType string `json:"request_type"`
	SubType     string `json:"sub_type"`
	GroupID     int64  `json:"group_id"`
	UserID      int64  `json:"user_id"`
	Comment     string `json:"comment"`
	Flag        string `json:"flag"`
}

// Name 返回群请求名称。
// @param 无。
// @returns request.group.add 或 request.group.invite。
// ⚠️副作用说明：无。
func (e GroupRequestEvent) Name() string {
	// >>> 数据演变示例
	// 1. group+add -> request.group.add。
	// 2. group+invite -> request.group.invite。
	return joinName(e.PostType, e.RequestType, e.SubType)
}

type NoticeEvent struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
	SubType    string `json:"sub_type"`
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
	OperatorID int64  `json:"operator_id"`
	TargetID   int64  `json:"target_id"`
	MessageID  int64  `json:"message_id"`
}

// Name 返回通知事件名称。
// @param 无。
// @returns notice 分类名称。
// ⚠️副作用说明：无。
func (e NoticeEvent) Name() string {
	// >>> 数据演变示例
	// 1. group_admin+set -> notice.group_admin.set。
	// 2. friend_add+空 -> notice.friend_add。
	return joinName(e.PostType, e.NoticeType, e.SubType)
}

type GroupBanNotice struct {
	NoticeEvent
	Duration int64 `json:"duration"`
}

type GroupCardNotice struct {
	NoticeEvent
	CardNew string `json:"card_new"`
	CardOld string `json:"card_old"`
}

type GroupUploadFile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	BusID int64  `json:"busid"`
}

type GroupUploadNotice struct {
	NoticeEvent
	File GroupUploadFile `json:"file"`
}

type EssenceNotice struct {
	NoticeEvent
	SenderID int64 `json:"sender_id"`
}

type EmojiLike struct {
	EmojiID string `json:"emoji_id"`
	Count   int    `json:"count"`
}

type EmojiLikeNotice struct {
	NoticeEvent
	Likes      []EmojiLike `json:"likes"`
	IsAdd      bool        `json:"is_add"`
	MessageSeq string      `json:"message_seq,omitempty"`
}

type NotifyNotice struct {
	NoticeEvent
	SenderID     int64           `json:"sender_id"`
	NameNew      string          `json:"name_new"`
	Title        string          `json:"title"`
	BusiID       string          `json:"busi_id"`
	Content      string          `json:"content"`
	RawInfo      json.RawMessage `json:"raw_info"`
	StatusText   string          `json:"status_text"`
	EventType    int             `json:"event_type"`
	OperatorNick string          `json:"operator_nick"`
	Times        int             `json:"times"`
}

type OnlineFileNotice struct {
	NoticeEvent
	PeerID int64 `json:"peer_id"`
}

type BotOfflineNotice struct {
	NoticeEvent
	Tag     string `json:"tag"`
	Message string `json:"message"`
}

type UnknownEvent struct {
	BaseEvent
	Raw json.RawMessage `json:"-"`
}

// Name 返回未知事件名称。
// @param 无。
// @returns post_type；为空时返回 unknown。
// ⚠️副作用说明：无。
func (e UnknownEvent) Name() string {
	// [决策理由] 缺少 post_type 的上报仍需稳定名称以供日志检索。
	if e.PostType == "" {
		return "unknown"
	}

	// >>> 数据演变示例
	// 1. post_type=custom -> custom。
	// 2. post_type="" -> unknown。
	return e.PostType
}

type discriminator struct {
	BaseEvent
	MessageType   string `json:"message_type"`
	NoticeType    string `json:"notice_type"`
	RequestType   string `json:"request_type"`
	MetaEventType string `json:"meta_event_type"`
	SubType       string `json:"sub_type"`
}

// ParseEvent 分两阶段解析 OneBot 事件并返回精确类型。
// @param payload：NapCat 上报的 JSON。
// @returns 强类型 Event 或 JSON 解析错误。
// ⚠️副作用说明：无。
func ParseEvent(payload []byte) (Event, error) {
	var kind discriminator
	// [决策理由] 无法解析分类字段时不能安全选择具体事件类型。
	if err := json.Unmarshal(payload, &kind); err != nil {
		return nil, fmt.Errorf("解析 OneBot JSON: %w", err)
	}
	var target Event
	switch kind.PostType {
	case "message", "message_sent":
		target = &MessageEvent{}
	case "meta_event":
		// [决策理由] 心跳和生命周期载荷不同，必须选择各自结构体。
		if kind.MetaEventType == "heartbeat" {
			target = &HeartbeatEvent{}
		} else {
			target = &LifecycleEvent{}
		}
	case "request":
		// [决策理由] 好友请求没有 group_id/sub_type，与群请求应使用不同类型。
		if kind.RequestType == "friend" {
			target = &FriendRequestEvent{}
		} else {
			target = &GroupRequestEvent{}
		}
	case "notice":
		target = noticeTarget(kind.NoticeType, kind.SubType)
	default:
		target = &UnknownEvent{Raw: append(json.RawMessage(nil), payload...)}
	}
	// [决策理由] 第二次解析把完整载荷写入已选定类型，避免无关字段出现在插件 API。
	if err := json.Unmarshal(payload, target); err != nil {
		return nil, fmt.Errorf("解析 %s 事件: %w", kind.PostType, err)
	}

	// >>> 数据演变示例
	// 1. notice.group_ban -> discriminator -> GroupBanNotice -> 返回 Event。
	// 2. custom -> discriminator -> UnknownEvent{Raw} -> 保留原始 JSON。
	return target, nil
}

// noticeTarget 为 NapCat 通知选择精确载荷类型。
// @param noticeType：通知类型；subType：通知子类型。
// @returns 对应通知结构体指针。
// ⚠️副作用说明：仅分配内存。
func noticeTarget(noticeType string, subType string) Event {
	switch noticeType {
	case "group_ban":
		return &GroupBanNotice{}
	case "group_card":
		return &GroupCardNotice{}
	case "group_upload":
		return &GroupUploadNotice{}
	case "essence":
		return &EssenceNotice{}
	case "group_msg_emoji_like":
		return &EmojiLikeNotice{}
	case "online_file_receive", "online_file_send":
		return &OnlineFileNotice{}
	case "bot_offline":
		return &BotOfflineNotice{}
	case "notify":
		return &NotifyNotice{}
	default:
		return &NoticeEvent{NoticeType: noticeType, SubType: subType}
	}

	// >>> 数据演变示例
	// 1. group_upload -> GroupUploadNotice 指针。
	// 2. friend_add -> 通用且字段受限的 NoticeEvent 指针。
}

// joinName 连接非空事件分类片段。
// @param parts：从大类到子类的名称片段。
// @returns 点号分隔的事件名称。
// ⚠️副作用说明：无。
func joinName(parts ...string) string {
	name := ""
	for _, part := range parts {
		// [决策理由] 跳过空分类可避免无子类型事件生成多余点号。
		if part == "" {
			continue
		}
		// [决策理由] 首段前不应添加分隔符。
		if name != "" {
			name += "."
		}
		name += part
	}

	// >>> 数据演变示例
	// 1. [notice,group_ban,ban] -> notice.group_ban.ban。
	// 2. [request,friend,""] -> request.friend。
	return name
}
