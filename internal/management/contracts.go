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

// Controller 定义 QQ 管理插件与未来 WebUI 共用的管理能力。
type Controller interface {
	ListPlugins(context.Context, Actor) ([]PluginState, error)
	SetPluginEnabled(context.Context, Actor, string, bool) (PluginState, error)
	SetPluginPriority(context.Context, Actor, string, int) (PluginState, error)
}
