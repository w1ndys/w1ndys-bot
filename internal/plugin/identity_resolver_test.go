package plugin

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

func identityTestMessage(groupID, userID int64, role string) *ws.MessageEvent {
	return &ws.MessageEvent{
		MessageType: "group", GroupID: groupID, UserID: userID,
		Sender: ws.MessageSender{UserID: userID, Role: role},
	}
}

func TestCodeIdentityResolverPrioritizesConfiguredSuperAdmin(t *testing.T) {
	resolver, err := NewCodeIdentityResolver(100)
	if err != nil {
		t.Fatal(err)
	}
	// 最高管理员即使在群里只是普通成员也必须解析为 super_admin。
	role, err := resolver.Resolve(context.Background(), identityTestMessage(200, 100, "member"))
	if err != nil || role != RoleSuperAdmin {
		t.Fatalf("Resolve() = %q,%v", role, err)
	}
}

func TestCodeIdentityResolverMapsClosedGroupRoles(t *testing.T) {
	tests := map[string]Role{"owner": RoleGroupOwner, "admin": RoleGroupAdmin, "member": RoleGroupMember}
	for sender, want := range tests {
		t.Run(sender, func(t *testing.T) {
			resolver, err := NewCodeIdentityResolver(0)
			if err != nil {
				t.Fatal(err)
			}
			role, err := resolver.Resolve(context.Background(), identityTestMessage(200, 300, sender))
			if err != nil || role != want {
				t.Fatalf("Resolve() = %q,%v", role, err)
			}
		})
	}
}

func TestCodeIdentityResolverFailsClosed(t *testing.T) {
	private := identityTestMessage(0, 300, "member")
	private.MessageType = "private"
	mismatched := identityTestMessage(200, 300, "member")
	mismatched.Sender.UserID = 400
	tests := []struct {
		name    string
		message *ws.MessageEvent
		want    error
	}{
		{name: "nil message", want: ErrIdentityInvalidSubject},
		{name: "private message", message: private, want: ErrIdentityInvalidSubject},
		{name: "invalid group", message: identityTestMessage(0, 300, "member"), want: ErrIdentityInvalidSubject},
		{name: "invalid user", message: identityTestMessage(200, 0, "member"), want: ErrIdentityInvalidSubject},
		{name: "sender mismatch", message: mismatched, want: ErrIdentityInvalidSubject},
		{name: "empty role", message: identityTestMessage(200, 300, ""), want: ErrIdentityUnknownRole},
		{name: "unknown role", message: identityTestMessage(200, 300, "owner_typo"), want: ErrIdentityUnknownRole},
		{name: "super admin role", message: identityTestMessage(200, 300, string(RoleSuperAdmin)), want: ErrIdentityUnknownRole},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, err := NewCodeIdentityResolver(100)
			if err != nil {
				t.Fatal(err)
			}
			role, err := resolver.Resolve(context.Background(), test.message)
			if role != "" || !errors.Is(err, test.want) {
				t.Fatalf("Resolve() = %q,%v", role, err)
			}
		})
	}
}

func TestNewCodeIdentityResolverRejectsInvalidSuperAdmin(t *testing.T) {
	resolver, err := NewCodeIdentityResolver(-1)
	if resolver != nil || err == nil {
		t.Fatalf("NewCodeIdentityResolver(-1) = %v,%v", resolver, err)
	}
	// 未配置最高管理员时仍可解析普通群身份。
	resolver, err = NewCodeIdentityResolver(0)
	if resolver == nil || err != nil {
		t.Fatalf("NewCodeIdentityResolver(0) = %v,%v", resolver, err)
	}
	if role, err := resolver.Resolve(context.Background(), identityTestMessage(200, 100, "member")); role != RoleGroupMember || err != nil {
		t.Fatalf("Resolve() = %q,%v", role, err)
	}
}

func TestCodeIdentityResolverSupportsConcurrentResolution(t *testing.T) {
	const calls = 64
	resolver, err := NewCodeIdentityResolver(100)
	if err != nil {
		t.Fatal(err)
	}
	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, calls)
	for index := 0; index < calls; index++ {
		waitGroup.Add(1)
		go func(userID int64) {
			defer waitGroup.Done()
			role, resolveErr := resolver.Resolve(context.Background(), identityTestMessage(200, userID, "member"))
			want := RoleGroupMember
			if userID == 100 {
				want = RoleSuperAdmin
			}
			if resolveErr != nil || role != want {
				errorsFound <- errors.New("并发身份解析结果错误")
			}
		}(int64(index + 69))
	}
	waitGroup.Wait()
	close(errorsFound)
	if len(errorsFound) != 0 {
		t.Fatalf("concurrent errors = %d", len(errorsFound))
	}
}
