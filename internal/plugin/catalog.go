// 📌 影响范围：仅定义目标插件运行时所需的最小依赖契约。
package plugin

import (
	"context"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/onebot"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// Messenger 是插件发送回复所需的最小 BotAPI 能力。
type Messenger interface {
	Reply(context.Context, *ws.MessageEvent, any) (int64, error)
	ReplyToMessage(context.Context, *ws.MessageEvent, int64, string) (int64, error)
}

// ActionAPI 是插件调用非消息类 OneBot Action 所需的最小能力。
type ActionAPI interface {
	SetGroupBan(context.Context, onebot.SetGroupBanParams) error
	GetGroupMemberList(context.Context, onebot.GetGroupMemberListParams) ([]onebot.GroupMemberInfo, error)
	GetGroupMessageHistory(context.Context, onebot.GetGroupMessageHistoryParams) (onebot.GetGroupMessageHistoryResult, error)
	GetMessage(context.Context, any) (onebot.MessageInfo, error)
	DeleteMessage(context.Context, any) error
}

// RuntimeManagement 是 QQ 应急入口使用的目标插件状态管理最小契约。
type RuntimeManagement interface {
	List(context.Context, management.Actor) ([]RuntimeStateView, error)
	Get(context.Context, management.Actor, string) (RuntimeStateView, error)
	SetGlobalEnabled(context.Context, management.Actor, string, bool, int64) (RuntimeStateView, error)
	SetGroupEnabled(context.Context, management.Actor, string, int64, bool, int64) (RuntimeStateView, error)
}
