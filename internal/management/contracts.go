// 📌 影响范围：无；定义管理入口、AdminService 与插件运行时共享的稳定契约。
package management

import "context"

// Channel 表示管理操作来源。
type Channel string

const (
	ChannelWebUI  Channel = "webui"
	ChannelQQ     Channel = "qq"
	ChannelSystem Channel = "system"
)

// Actor 描述执行管理操作的身份和来源。
type Actor struct {
	ID        string
	Role      string
	Channel   Channel
	RequestID string
}

// PluginState 是管理端使用的插件运行配置快照。
type PluginState struct {
	Name       string
	Enabled    bool
	Priority   int
	ConfigJSON []byte
}

// CommandState 表示一条可管理的插件功能命令。
type CommandState struct {
	ID                int64
	ScopeType         string
	ScopeID           string
	PluginName        string
	FeatureKey        string
	Command           string
	NormalizedCommand string
	Enabled           bool
}

// CommandCreate 描述新增命令所需字段。
type CommandCreate struct {
	ScopeType  string
	ScopeID    string
	PluginName string
	FeatureKey string
	Command    string
}

// PermissionState 表示一条角色权限覆盖策略。
type PermissionState struct {
	ID          int64
	ScopeType   string
	ScopeID     string
	PluginName  string
	FeatureKey  string
	SubjectRole string
	Effect      string
}

// PermissionSet 描述新增或更新权限策略的唯一维度和效果。
type PermissionSet struct {
	ScopeType   string
	ScopeID     string
	PluginName  string
	FeatureKey  string
	SubjectRole string
	Effect      string
}

// Controller 定义 QQ 管理插件与未来 WebUI 共用的管理能力。
type Controller interface {
	ListPlugins(context.Context, Actor) ([]PluginState, error)
	SetPluginEnabled(context.Context, Actor, string, bool) (PluginState, error)
	SetPluginPriority(context.Context, Actor, string, int) (PluginState, error)
	ListCommands(context.Context, Actor) ([]CommandState, error)
	CreateCommand(context.Context, Actor, CommandCreate) (CommandState, error)
	RenameCommand(context.Context, Actor, int64, string) (CommandState, error)
	DeleteCommand(context.Context, Actor, int64) error
	ListPermissions(context.Context, Actor) ([]PermissionState, error)
	SetPermission(context.Context, Actor, PermissionSet) (PermissionState, error)
	DeletePermission(context.Context, Actor, int64) error
}
