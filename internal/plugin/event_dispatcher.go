package plugin

import (
	"context"
	"errors"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// DispatchResult 描述统一事件入口实际选择的执行分支。
type DispatchResult struct {
	CommandMatched   bool
	ObserversHandled int
}

// EventDispatcher 编排命令优先、未匹配事件观察的唯一内存入口。
type EventDispatcher struct {
	commands  *Dispatcher
	observers *ObserverDispatcher
}

// NewEventDispatcher 创建命令和观察器共享的统一事件入口。
func NewEventDispatcher(commands *Dispatcher, observers *ObserverDispatcher) (*EventDispatcher, error) {
	if commands == nil {
		return nil, errors.New("命令 Dispatcher 不能为空")
	}
	if observers == nil {
		return nil, errors.New("观察器 Dispatcher 不能为空")
	}
	return &EventDispatcher{commands: commands, observers: observers}, nil
}

// Dispatch 优先执行群命令，仅将未匹配事件交给观察器。
func (d *EventDispatcher) Dispatch(ctx context.Context, event ws.Event) (DispatchResult, error) {
	if message, isMessage := event.(*ws.MessageEvent); isMessage {
		matched, err := d.commands.Dispatch(ctx, message)
		if matched || err != nil {
			return DispatchResult{CommandMatched: matched}, err
		}
	}
	handled, err := d.observers.Dispatch(ctx, event)
	return DispatchResult{ObserversHandled: handled}, err
}
