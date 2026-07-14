// 📌 影响范围：定义 Echo 插件的元数据、功能键、默认命令和默认权限开关；无外部变量。
package echo

// 插件配置区：开发新插件时优先集中修改这里，避免元数据散落在业务函数中。
const (
	pluginName        = "echo"                  // 插件机器标识，用作数据库主键、日志标签和权限路由
	pluginDisplayName = "Echo 回声"               // WebUI 插件列表中的显示名称
	pluginDescription = "回复命令后携带的文本，用于演示插件开发链路" // 插件简介，WebUI 展示
	pluginPriority    = 100                     // 广播事件处理优先级，数值越大越先被处理

	featureEcho            = "echo"     // 功能机器Key，用于命令路由(格式: 插件名.功能Key)和权限校验
	featureDisplayName     = "回声"       // 功能显示名称，管理界面展示
	featureDescription     = "引用回复输入参数" // 功能描述，说明该功能做了什么
	defaultCommandEcho     = "echo"     // 默认英文触发命令，每次启动同步到数据库(缺失时补回)
	defaultCommandEchoCN   = "回声"       // 默认中文触发命令，每次启动同步到数据库(缺失时补回)
	defaultMemberAvailable = true       // 普通群成员默认权限开关，true=成员可用，false=仅管理员以上可用
)
