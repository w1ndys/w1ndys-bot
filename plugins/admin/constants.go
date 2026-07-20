// 📌 影响范围：定义系统管理插件的元数据、功能键和默认命令；无外部变量。
package admin

// 插件配置区：系统插件元数据集中维护，init只负责注册。
const (
	pluginName        = "admin"           // 插件机器标识，用作数据库主键、日志标签和权限路由
	pluginDisplayName = "系统管理"            // WebUI 插件列表中的显示名称
	pluginDescription = "通过QQ查询和紧急切换插件状态" // 插件简介，WebUI 展示
	pluginPriority    = 1000              // 广播事件处理优先级(高于普通插件的100)，确保系统级事件优先处理

	featureList               = "plugin_list"    // 插件列表功能的机器Key，用于命令路由和权限校验
	featureListDisplay        = "插件列表"           // 列表功能的显示名称
	featureListDescription    = "查询当前插件运行状态"     // 列表功能说明，管理界面展示
	featureListCommand        = "插件列表"           // 列表功能的默认触发命令，每次启动同步到数据库
	featureEnable             = "plugin_enable"  // 启用插件功能的机器Key，用于命令路由和权限校验
	featureEnableDisplay      = "启用插件"           // 启用功能的显示名称
	featureEnableDescription  = "启用指定的非系统插件"     // 启用功能说明，管理界面展示
	featureEnableCommand      = "启用插件"           // 启用功能的默认触发命令，每次启动同步到数据库
	featureDisable            = "plugin_disable" // 禁用插件功能的机器Key，用于命令路由和权限校验
	featureDisableDisplay     = "禁用插件"           // 禁用功能的显示名称
	featureDisableDescription = "禁用指定的非系统插件"     // 禁用功能说明，管理界面展示
	featureDisableCommand     = "禁用插件"           // 禁用功能的默认触发命令，每次启动同步到数据库
)
