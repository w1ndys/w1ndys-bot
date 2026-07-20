// 📌 影响范围：无；定义管理入口、AdminService 与插件运行时共享的稳定契约。
package management

import (
	"context"
	"encoding/json"
	"time"
)

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
	Name        string
	DisplayName string
	Description string
	Available   bool
	Enabled     bool
	Priority    int
	ConfigJSON  json.RawMessage
}

// PluginConfigState 是插件声明式配置的脱敏管理快照。
type PluginConfigState struct {
	PluginName string
	ConfigJSON json.RawMessage
	Version    int64
}

// PluginConfigUpdate 描述带乐观锁版本的完整配置更新。
type PluginConfigUpdate struct {
	ConfigJSON      json.RawMessage
	ExpectedVersion int64
}

// FeatureState 表示插件 Manifest 同步后的功能元数据。
type FeatureState struct {
	PluginName         string
	Key                string
	DisplayName        string
	Description        string
	Available          bool
	DefaultCommands    []string
	DefaultPermissions json.RawMessage
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

// PermissionState 表示一条角色或指定用户权限覆盖策略。
type PermissionState struct {
	ID          int64
	ScopeType   string
	ScopeID     string
	PluginName  string
	FeatureKey  string
	SubjectType string
	SubjectID   string
	Effect      string
}

// PermissionSet 描述新增或更新权限策略的唯一维度和效果。
type PermissionSet struct {
	ScopeType   string
	ScopeID     string
	PluginName  string
	FeatureKey  string
	SubjectType string
	SubjectID   string
	Effect      string
}

// AdminState 表示最高管理员账号状态。
type AdminState struct {
	UserID   string
	Nickname string
	Enabled  bool
}

// SettingState 表示一项数据库系统业务设置。
type SettingState struct {
	Key         string
	Value       json.RawMessage
	Description string
	Overridden  bool
}

// AuditQuery 描述审计日志分页与筛选条件。
type AuditQuery struct {
	Page       int
	PageSize   int
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	StartTime  *time.Time
	EndTime    *time.Time
}

// AuditState 表示一条不可修改的管理审计记录。
type AuditState struct {
	ID           int64
	ActorID      string
	ActorRole    string
	Channel      string
	Action       string
	TargetType   string
	TargetID     string
	BeforeJSON   json.RawMessage
	AfterJSON    json.RawMessage
	Success      bool
	ErrorMessage string
	RequestID    string
	CreatedAt    time.Time
}

// AuditPage 表示一页审计记录及总数。
type AuditPage struct {
	Items    []AuditState
	Page     int
	PageSize int
	Total    int64
}

// Controller 定义 QQ 管理插件与未来 WebUI 共用的管理能力。
type Controller interface {
	ListPlugins(context.Context, Actor) ([]PluginState, error)
	SetPluginEnabled(context.Context, Actor, string, bool) (PluginState, error)
	SetPluginPriority(context.Context, Actor, string, int) (PluginState, error)
	ListPluginFeatures(context.Context, Actor, string) ([]FeatureState, error)
	ListCommands(context.Context, Actor) ([]CommandState, error)
	CreateCommand(context.Context, Actor, CommandCreate) (CommandState, error)
	RenameCommand(context.Context, Actor, int64, string) (CommandState, error)
	DeleteCommand(context.Context, Actor, int64) error
	ListPermissions(context.Context, Actor) ([]PermissionState, error)
	SetPermission(context.Context, Actor, PermissionSet) (PermissionState, error)
	DeletePermission(context.Context, Actor, int64) error
	ListSettings(context.Context, Actor) ([]SettingState, error)
	SetSetting(context.Context, Actor, string, json.RawMessage) (SettingState, error)
	DeleteSetting(context.Context, Actor, string) error
	ListAuditLogs(context.Context, Actor, AuditQuery) (AuditPage, error)
	GetAuditLog(context.Context, Actor, int64) (AuditState, error)
}
