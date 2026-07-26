// 📌 影响范围：使用内存 fake Messenger 验证 Echo 规格与 Handler；不访问 NapCat、PostgreSQL 或网络。
package echo

import (
	"context"
	"errors"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeMessenger struct {
	messageID int64
	reply     string
	calls     int
	err       error
}

func (f *fakeMessenger) Reply(context.Context, *ws.MessageEvent, any) (int64, error) {
	return 1, f.err
}

func (f *fakeMessenger) ReplyToMessage(_ context.Context, _ *ws.MessageEvent, messageID int64, message string) (int64, error) {
	f.calls++
	f.messageID = messageID
	f.reply = message
	return 1, f.err
}

func newEchoTestCommand(t *testing.T, messenger plugin.Messenger) plugin.CommandSpec {
	t.Helper()
	spec, err := Spec(messenger)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Commands) != 1 {
		t.Fatalf("commands = %d", len(spec.Commands))
	}
	return spec.Commands[0]
}

func TestSpecDeclaresStableCommandContract(t *testing.T) {
	spec, err := Spec(&fakeMessenger{})
	if err != nil {
		t.Fatal(err)
	}
	// 规格必须能进入目录：Key、触发词、作用域和身份声明都要通过平台校验。
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	if spec.Key != "echo" || spec.DisplayName == "" || spec.Description == "" {
		t.Fatalf("spec = %+v", spec)
	}
	command := spec.Commands[0]
	if command.Key != "echo" || command.Scope != plugin.CommandScopeGroup {
		t.Fatalf("command = %+v", command)
	}
	if len(command.Triggers) != 2 || command.Triggers[0] != "echo" || command.Triggers[1] != "回声" {
		t.Fatalf("triggers = %v", command.Triggers)
	}
	// 四类封闭身份都允许，且身份集合必须显式声明而非留空。
	for _, role := range []plugin.Role{plugin.RoleSuperAdmin, plugin.RoleGroupOwner, plugin.RoleGroupAdmin, plugin.RoleGroupMember} {
		if !command.AllowedRoles.Contains(role) {
			t.Fatalf("role %q not allowed", role)
		}
	}
	if len(command.AllowedRoles) != 4 {
		t.Fatalf("roles = %v", command.AllowedRoles)
	}
	// Echo 没有后台资源，不应声明生命周期或观察器。
	if spec.Lifecycle != nil || len(spec.Observers) != 0 {
		t.Fatalf("unexpected lifecycle or observers: %+v", spec)
	}
}

func TestSpecRejectsMissingMessenger(t *testing.T) {
	spec, err := Spec(nil)
	if err == nil {
		t.Fatalf("Spec(nil) = %+v", spec)
	}
}

func TestHandlerRepliesWithArgumentsOrUsage(t *testing.T) {
	tests := []struct {
		name      string
		trigger   string
		arguments string
		want      string
	}{
		{name: "有参数", trigger: "echo", arguments: "Hello World", want: "Hello World"},
		{name: "空参数回复用法", trigger: "echo", want: "用法：echo <要重复的内容>"},
		{name: "空参数使用实际触发词", trigger: "回声", want: "用法：回声 <要重复的内容>"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messenger := &fakeMessenger{}
			command := newEchoTestCommand(t, messenger)
			message := &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, MessageID: 20}
			err := command.Handler(plugin.CommandContext{
				Context: context.Background(), Message: message,
				Trigger: test.trigger, Arguments: test.arguments, Role: plugin.RoleGroupMember,
			})
			if err != nil {
				t.Fatal(err)
			}
			// 引用回复必须指向触发命令的消息，避免回复串到其他消息。
			if messenger.reply != test.want || messenger.messageID != 20 || messenger.calls != 1 {
				t.Fatalf("messenger = %+v", messenger)
			}
		})
	}
}

func TestHandlerPropagatesMessengerFailure(t *testing.T) {
	sendFailure := errors.New("send failed")
	messenger := &fakeMessenger{err: sendFailure}
	command := newEchoTestCommand(t, messenger)
	err := command.Handler(plugin.CommandContext{
		Context: context.Background(),
		Message: &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, MessageID: 20},
		Trigger: "echo", Arguments: "Hello", Role: plugin.RoleGroupMember,
	})
	if !errors.Is(err, sendFailure) {
		t.Fatalf("Handler() error = %v", err)
	}
}
