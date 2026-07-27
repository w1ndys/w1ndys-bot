// 📌 影响范围：无；定义平台管理与插件专属业务共享的稳定数据契约。
package management

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrResourceRecordNotFound = errors.New("插件资源记录不存在")
var ErrInvalidResourceData = errors.New("插件资源数据无效")
var ErrResourceConflict = errors.New("插件资源冲突")

type Channel string

const (
	ChannelWebUI  Channel = "webui"
	ChannelQQ     Channel = "qq"
	ChannelSystem Channel = "system"
)

type Actor struct {
	ID        string
	Role      string
	Channel   Channel
	RequestID string
}

// ResourceQuery、ResourceRecord 与 ResourcePage 暂由 Forbidden Monitor 内部仓储 DTO 使用。
type ResourceQuery struct {
	Page     int
	PageSize int
}

type ResourceRecord struct {
	ID      int64
	Version int64
	Data    json.RawMessage
}

type ResourcePage struct {
	Items    []ResourceRecord
	Page     int
	PageSize int
	Total    int64
}

type AdminState struct {
	UserID   string
	Nickname string
	Enabled  bool
}

type SettingState struct {
	Key         string
	Value       json.RawMessage
	Description string
	Overridden  bool
}

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

type AuditPage struct {
	Items    []AuditState
	Page     int
	PageSize int
	Total    int64
}
