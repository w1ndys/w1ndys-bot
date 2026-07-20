// 📌 影响范围：声明 NapCat 用户及用户扩展接口 Action；无外部变量。
package onebot

const (
	ActionSetFriendRemark             Action = "set_friend_remark"
	ActionSendLike                    Action = "send_like"
	ActionGetFriendList               Action = "get_friend_list"
	ActionSetFriendAddRequest         Action = "set_friend_add_request"
	ActionGetCookies                  Action = "get_cookies"
	ActionGetRecentContact            Action = "get_recent_contact"
	ActionGetFriendsWithCategory      Action = "get_friends_with_category"
	ActionGetProfileLike              Action = "get_profile_like"
	ActionSetDIYOnlineStatus          Action = "set_diy_online_status"
	ActionGetUnidirectionalFriendList Action = "get_unidirectional_friend_list"
)
