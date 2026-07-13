// 📌 影响范围：无；仅定义管理服务的数据模型和稳定错误。
package admin

import (
	"errors"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

var ErrPluginNotFound = errors.New("插件不存在")
var ErrInvalidActor = errors.New("操作者不能为空")
var ErrInvalidChannel = errors.New("管理通道无效")
var ErrForbidden = errors.New("无最高管理员权限")
var ErrProtectedPlugin = errors.New("系统管理插件不可禁用")

type Channel = management.Channel
type Actor = management.Actor
type PluginState = management.PluginState

const ChannelWebUI = management.ChannelWebUI
const ChannelQQ = management.ChannelQQ
const ChannelSystem = management.ChannelSystem

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
