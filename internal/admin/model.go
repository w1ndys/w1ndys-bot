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
var ErrCommandNotFound = errors.New("命令不存在")
var ErrCommandConflict = errors.New("命令在作用域内重复")
var ErrInvalidCommand = errors.New("命令参数无效")
var ErrPermissionNotFound = errors.New("权限策略不存在")
var ErrInvalidPermission = errors.New("权限策略参数无效")
var ErrFeatureNotFound = errors.New("插件功能不存在")

type Channel = management.Channel
type Actor = management.Actor
type PluginState = management.PluginState
type CommandState = management.CommandState
type CommandCreate = management.CommandCreate
type PermissionState = management.PermissionState
type PermissionSet = management.PermissionSet
type SystemAdmin = management.AdminState
type SettingState = management.SettingState

var ErrSettingNotFound = errors.New("系统设置不存在")
var ErrUnknownSetting = errors.New("未知系统设置")

const ChannelWebUI = management.ChannelWebUI
const ChannelQQ = management.ChannelQQ
const ChannelSystem = management.ChannelSystem

// PluginPatch 描述一次插件配置变更；nil 字段保持原值。
type PluginPatch struct {
	Enabled  *bool
	Priority *int
}
