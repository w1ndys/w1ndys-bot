// 📌 影响范围：声明 NapCat 4.18.13 核心、频道和 AI Action；外部副作用由调用的具体 Action 决定。
package onebot

const (
	ActionSetGroupTodo           Action = "set_group_todo"
	ActionCompleteGroupTodo      Action = "complete_group_todo"
	ActionCancelGroupTodo        Action = "cancel_group_todo"
	ActionGroupPoke              Action = "group_poke"
	ActionFriendPoke             Action = "friend_poke"
	ActionSendPoke               Action = "send_poke"
	ActionGetGuildList           Action = "get_guild_list"
	ActionGetGuildServiceProfile Action = "get_guild_service_profile"
	ActionGetAIRecord            Action = "get_ai_record"
	ActionSendGroupAIRecord      Action = "send_group_ai_record"
)
