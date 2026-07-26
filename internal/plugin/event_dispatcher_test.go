package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type eventDispatcherIdentity struct {
	role Role
}

func (r eventDispatcherIdentity) Resolve(context.Context, *ws.MessageEvent) (Role, error) {
	return r.role, nil
}

func newEventDispatcherTestSubject(t *testing.T, commandHandler CommandHandler, observerHandler ObserverHandler) (*EventDispatcher, *RuntimeController) {
	t.Helper()
	spec := PluginSpec{
		Key:         "combined",
		DisplayName: "统一分发测试",
		Description: "验证命令与观察器优先级",
		Commands: []CommandSpec{{
			Key:          "run",
			DisplayName:  "执行",
			Description:  "测试命令",
			Triggers:     []string{"run"},
			Scope:        CommandScopeGroup,
			AllowedRoles: Roles(RoleGroupMember),
			Handler:      commandHandler,
		}},
		Observers: []ObserverSpec{{
			Key:         "messages",
			Description: "观察未匹配群消息",
			EventKinds:  []ObserverEventKind{ObserverGroupMessage, ObserverGroupNotice, ObserverGroupRequest},
			Handler:     observerHandler,
		}},
	}
	catalog, err := NewSpecCatalog([]PluginSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntimeController(catalog)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := NewDispatcher(catalog, runtime, eventDispatcherIdentity{role: RoleGroupMember})
	if err != nil {
		t.Fatal(err)
	}
	observers, err := NewObserverDispatcher(catalog, runtime)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewEventDispatcher(commands, observers)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher, runtime
}

func TestEventDispatcherCommandMatchSuppressesObservers(t *testing.T) {
	commandCalls := 0
	observerCalls := 0
	dispatcher, runtime := newEventDispatcherTestSubject(t, func(CommandContext) error {
		commandCalls++
		return nil
	}, func(ObserverContext) error {
		observerCalls++
		return nil
	})
	if err := runtime.SetGroupEnabled("combined", 100, true); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Enable(context.Background(), "combined"); err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "run value"})
	if err != nil || !result.CommandMatched || result.ObserversHandled != 0 || commandCalls != 1 || observerCalls != 0 {
		t.Fatalf("Dispatch() = %+v,%v command=%d observer=%d", result, err, commandCalls, observerCalls)
	}
}

func TestEventDispatcherMatchedRejectionDoesNotFallThrough(t *testing.T) {
	observerCalls := 0
	dispatcher, _ := newEventDispatcherTestSubject(t, func(CommandContext) error {
		t.Fatal("disabled command called")
		return nil
	}, func(ObserverContext) error {
		observerCalls++
		return nil
	})
	result, err := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "run"})
	if !errors.Is(err, ErrPluginNotReady) || !result.CommandMatched || observerCalls != 0 {
		t.Fatalf("Dispatch() = %+v,%v observer=%d", result, err, observerCalls)
	}
}

func TestEventDispatcherHandlerErrorDoesNotFallThrough(t *testing.T) {
	handlerErr := errors.New("command failed")
	observerCalls := 0
	dispatcher, runtime := newEventDispatcherTestSubject(t, func(CommandContext) error { return handlerErr }, func(ObserverContext) error {
		observerCalls++
		return nil
	})
	if err := runtime.SetGroupEnabled("combined", 100, true); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Enable(context.Background(), "combined"); err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "run"})
	if !errors.Is(err, handlerErr) || !result.CommandMatched || result.ObserversHandled != 0 || observerCalls != 0 {
		t.Fatalf("Dispatch() = %+v,%v observer=%d", result, err, observerCalls)
	}
}

func TestEventDispatcherSendsUnmatchedMessagesAndNoticesToObservers(t *testing.T) {
	var events []ws.Event
	dispatcher, runtime := newEventDispatcherTestSubject(t, func(CommandContext) error { return nil }, func(ctx ObserverContext) error {
		events = append(events, ctx.Event)
		return nil
	})
	if err := runtime.SetGroupEnabled("combined", 100, true); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Enable(context.Background(), "combined"); err != nil {
		t.Fatal(err)
	}
	inputs := []ws.Event{
		&ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "unmatched"},
		&ws.NoticeEvent{GroupID: 100},
		&ws.GroupRequestEvent{RequestType: "group", GroupID: 100},
	}
	for _, event := range inputs {
		result, err := dispatcher.Dispatch(context.Background(), event)
		if err != nil || result.CommandMatched || result.ObserversHandled != 1 {
			t.Fatalf("Dispatch(%T) = %+v,%v", event, result, err)
		}
	}
	if len(events) != 3 {
		t.Fatalf("observer events = %d", len(events))
	}
}

func TestEventDispatcherHandlesTypedNilAndUnsupportedEvents(t *testing.T) {
	dispatcher, _ := newEventDispatcherTestSubject(t, func(CommandContext) error { return nil }, func(ObserverContext) error {
		t.Fatal("invalid event called observer")
		return nil
	})
	var message *ws.MessageEvent
	result, err := dispatcher.Dispatch(context.Background(), message)
	if !errors.Is(err, ErrGroupMessageRequired) || result != (DispatchResult{}) {
		t.Fatalf("typed nil Dispatch() = %+v,%v", result, err)
	}
	result, err = dispatcher.Dispatch(context.Background(), &ws.HeartbeatEvent{})
	if err != nil || result != (DispatchResult{}) {
		t.Fatalf("heartbeat Dispatch() = %+v,%v", result, err)
	}
}

func TestEventDispatcherRejectsPrivateBeforeObservers(t *testing.T) {
	dispatcher, _ := newEventDispatcherTestSubject(t, func(CommandContext) error { return nil }, func(ObserverContext) error {
		t.Fatal("private message called observer")
		return nil
	})
	result, err := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "private", UserID: 200, RawMessage: "unmatched"})
	if !errors.Is(err, ErrGroupMessageRequired) || result != (DispatchResult{}) {
		t.Fatalf("Dispatch() = %+v,%v", result, err)
	}
}

func TestNewEventDispatcherRejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		commands  *Dispatcher
		observers *ObserverDispatcher
	}{
		{},
		{commands: &Dispatcher{}},
		{observers: &ObserverDispatcher{}},
	}
	for _, test := range tests {
		dispatcher, err := NewEventDispatcher(test.commands, test.observers)
		if dispatcher != nil || err == nil {
			t.Fatalf("NewEventDispatcher() = %v,%v", dispatcher, err)
		}
	}
}
