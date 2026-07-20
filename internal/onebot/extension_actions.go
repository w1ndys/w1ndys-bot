// 📌 影响范围：声明 NapCat 4.18.13 通用扩展 Action；外部副作用由调用的具体 Action 决定。
package onebot

const (
	ActionSetGroupKickMembers       Action = "set_group_kick_members"
	ActionCreateCollection          Action = "create_collection"
	ActionSetSelfLongNickname       Action = "set_self_longnick"
	ActionSetQQAvatar               Action = "set_qq_avatar"
	ActionTranslateEnglishToChinese Action = "translate_en2zh"
	ActionGetClientKey              Action = "get_clientkey"
	ActionOCRImage                  Action = "ocr_image"
	ActionOCRImageInternal          Action = ".ocr_image"
	ActionSetGroupSpecialTitle      Action = "set_group_special_title"
	ActionGetAICharacters           Action = "get_ai_characters"
)
