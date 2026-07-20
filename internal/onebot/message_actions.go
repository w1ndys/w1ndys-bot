// 📌 影响范围：声明 NapCat 消息接口 Action；无外部变量。
package onebot

const (
	ActionForwardFriendSingleMessage Action = "forward_friend_single_msg"
	ActionForwardGroupSingleMessage  Action = "forward_group_single_msg"
	ActionMarkGroupMessageAsRead     Action = "mark_group_msg_as_read"
	ActionMarkPrivateMessageAsRead   Action = "mark_private_msg_as_read"
	ActionGetMessage                 Action = "get_msg"
	ActionSendPrivateMessage         Action = "send_private_msg"
	ActionSendMessage                Action = "send_msg"
	ActionDeleteMessage              Action = "delete_msg"
	ActionMarkMessageAsRead          Action = "mark_msg_as_read"
	ActionMarkAllMessagesAsRead      Action = "_mark_all_as_read"
)
