// 📌 影响范围：引用 OneBot 事件模型；定义插件生命周期契约，不读写外部状态。
package plugin

import (
	"context"
	"errors"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// ErrStopPropagation 表示当前事件已处理完毕，不再传递给后续插件。
var ErrStopPropagation = errors.New("停止事件传播")

// Plugin 定义编译时集成插件必须实现的行为。
type Plugin interface {
	Name() string
	Handle(context.Context, ws.Event) error
	OnEnable(context.Context) error
	OnDisable(context.Context) error
}

// State 表示数据库持久化的插件运行状态。
type State struct {
	Name     string
	Enabled  bool
	Priority int
}

// StateStore 定义插件状态持久化能力。
type StateStore interface {
	Load(context.Context) ([]State, error)
	SaveEnabled(context.Context, string, bool) error
}
