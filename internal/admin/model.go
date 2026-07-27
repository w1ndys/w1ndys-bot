// 📌 影响范围：无；定义平台设置、审计与管理员认证的稳定错误和别名。
package admin

import (
	"errors"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

var ErrInvalidActor = errors.New("操作者不能为空")
var ErrInvalidChannel = errors.New("管理通道无效")
var ErrForbidden = errors.New("无最高管理员权限")
var ErrSettingNotFound = errors.New("系统设置不存在")
var ErrUnknownSetting = errors.New("未知系统设置")
var ErrInvalidSetting = errors.New("系统设置值无效")
var ErrAuditNotFound = errors.New("审计日志不存在")
var ErrInvalidAuditQuery = errors.New("审计查询参数无效")

type Channel = management.Channel
type Actor = management.Actor
type SystemAdmin = management.AdminState
type SettingState = management.SettingState
type AuditQuery = management.AuditQuery
type AuditState = management.AuditState
type AuditPage = management.AuditPage

const ChannelWebUI = management.ChannelWebUI
const ChannelQQ = management.ChannelQQ
const ChannelSystem = management.ChannelSystem
