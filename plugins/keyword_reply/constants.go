// 📌 影响范围：无；定义关键词回复插件稳定标识与输入边界。
package keywordreply

const (
	pluginName        = "keyword_reply"
	pluginDisplayName = "关键词回复"
	pluginDescription = "群消息与已启用关键词完全相等时自动引用回复"
	resourceKey       = "rules"
	maxKeywordLength  = 200
	maxReplyLength    = 2000
)
