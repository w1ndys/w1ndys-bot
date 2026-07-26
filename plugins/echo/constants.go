// 📌 影响范围：定义 Echo 插件的稳定标识、命令触发词和展示信息；无外部变量。
package echo

// 插件配置区：开发新插件时优先集中修改这里，避免元数据散落在业务函数中。
const (
	pluginKey         = "echo"                  // 插件机器标识，用作状态表主键、日志标签和管理 API 路径
	pluginDisplayName = "Echo 回声"               // WebUI 插件列表中的显示名称
	pluginDescription = "回复命令后携带的文本，用于演示插件开发链路" // 插件简介，WebUI 展示

	commandKey         = "echo"     // 命令机器 Key，用于审计和只读 WebUI 展示
	commandDisplayName = "回声"       // 命令显示名称，管理界面展示
	commandDescription = "引用回复输入参数" // 命令描述，说明该命令做了什么
	triggerEcho        = "echo"     // 英文触发词，由代码持有，数据库不可覆盖
	triggerEchoCN      = "回声"       // 中文触发词，由代码持有，数据库不可覆盖

	usageTemplate = "用法：%s <要重复的内容>" // 空参数时的引导回复
)
