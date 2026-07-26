// 📌 影响范围：无；定义关键词回复插件稳定标识与输入边界。
package keywordreply

const (
	pluginKey         = "keyword_reply"           // 插件机器标识，用作状态表主键、管理 API 路径与审计目标
	pluginDisplayName = "关键词回复"                   // WebUI 展示名称
	pluginDescription = "群消息与本群已启用关键词完全相等时自动引用回复" // 插件简介
	adminPageKey      = "keyword_reply"           // 专属页面 Key，由前端编译期注册表映射到本地组件
	observerKey       = "keyword_match"           // 观察器机器 Key，用于审计与只读展示
	observerDesc      = "对通过群门禁的群消息执行关键词完全匹配并引用回复"

	maxKeywordLength = 200  // 关键词长度上限，与数据库约束一致
	maxReplyLength   = 2000 // 回复内容长度上限，与数据库约束一致
	maxPageSize      = 100  // 单页规则数量上限，限制响应内存开销
)
