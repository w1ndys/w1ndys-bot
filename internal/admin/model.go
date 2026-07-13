// 📌 影响范围：无；仅定义管理服务的数据模型和稳定错误。
package admin

import "errors"

var ErrPluginNotFound = errors.New("插件不存在")
var ErrInvalidActor = errors.New("操作者不能为空")
var ErrInvalidChannel = errors.New("管理通道无效")
var ErrForbidden = errors.New("无最高管理员权限")

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

// PluginState 是管理界面使用的插件运行配置快照。
type PluginState struct {
	Name       string
	Enabled    bool
	Priority   int
	ConfigJSON []byte
}

// PluginPatch 描述一次插件配置变更；nil 字段保持原值。
type PluginPatch struct {
	Enabled  *bool
	Priority *int
}

// SystemAdmin 表示数据库配置的最高管理员账号。
type SystemAdmin struct {
	UserID   string
	Nickname string
	Enabled  bool
}
