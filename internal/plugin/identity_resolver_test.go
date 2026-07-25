package plugin

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type identityTestRoleSource struct {
	role    Role
	err     error
	calls   int
	groupID int64
	userID  int64
}

func (s *identityTestRoleSource) ResolveGroupRole(_ context.Context, groupID int64, userID int64) (Role, error) {
	s.calls++
	s.groupID = groupID
	s.userID = userID
	return s.role, s.err
}

func TestCodeIdentityResolverPrioritizesConfiguredSuperAdmin(t *testing.T) {
	source := &identityTestRoleSource{role: RoleGroupMember}
	resolver, err := NewCodeIdentityResolver(100, source)
	if err != nil {
		t.Fatal(err)
	}
	role, err := resolver.Resolve(context.Background(), 200, 100)
	if err != nil || role != RoleSuperAdmin {
		t.Fatalf("Resolve() = %q,%v", role, err)
	}
	if source.calls != 0 {
		t.Fatalf("group source calls = %d", source.calls)
	}
}

func TestCodeIdentityResolverMapsClosedGroupRoles(t *testing.T) {
	for _, want := range []Role{RoleGroupOwner, RoleGroupAdmin, RoleGroupMember} {
		t.Run(string(want), func(t *testing.T) {
			source := &identityTestRoleSource{role: want}
			resolver, err := NewCodeIdentityResolver(0, source)
			if err != nil {
				t.Fatal(err)
			}
			role, err := resolver.Resolve(context.Background(), 200, 300)
			if err != nil || role != want {
				t.Fatalf("Resolve() = %q,%v", role, err)
			}
			if source.calls != 1 || source.groupID != 200 || source.userID != 300 {
				t.Fatalf("source = %+v", source)
			}
		})
	}
}

func TestCodeIdentityResolverFailsClosed(t *testing.T) {
	sourceFailure := errors.New("source unavailable")
	tests := []struct {
		name    string
		groupID int64
		userID  int64
		role    Role
		source  error
		want    error
	}{
		{name: "invalid group", groupID: 0, userID: 300, role: RoleGroupMember, want: ErrIdentityInvalidSubject},
		{name: "invalid user", groupID: 200, userID: 0, role: RoleGroupMember, want: ErrIdentityInvalidSubject},
		{name: "unknown role", groupID: 200, userID: 300, role: "owner_typo", want: ErrIdentityUnknownRole},
		{name: "source error", groupID: 200, userID: 300, source: sourceFailure, want: sourceFailure},
		{name: "source super admin", groupID: 200, userID: 300, role: RoleSuperAdmin, want: ErrIdentityUnknownRole},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &identityTestRoleSource{role: test.role, err: test.source}
			resolver, err := NewCodeIdentityResolver(100, source)
			if err != nil {
				t.Fatal(err)
			}
			role, err := resolver.Resolve(context.Background(), test.groupID, test.userID)
			if role != "" || !errors.Is(err, test.want) {
				t.Fatalf("Resolve() = %q,%v", role, err)
			}
		})
	}
}

func TestNewCodeIdentityResolverRejectsInvalidDependencies(t *testing.T) {
	var typedNil *identityTestRoleSource
	tests := []struct {
		superAdminID int64
		source       GroupRoleSource
	}{
		{superAdminID: -1, source: &identityTestRoleSource{}},
		{superAdminID: 100},
		{superAdminID: 100, source: typedNil},
	}
	for _, test := range tests {
		resolver, err := NewCodeIdentityResolver(test.superAdminID, test.source)
		if resolver != nil || err == nil {
			t.Fatalf("NewCodeIdentityResolver() = %v,%v", resolver, err)
		}
	}
}

type concurrentIdentityRoleSource struct{}

func (concurrentIdentityRoleSource) ResolveGroupRole(context.Context, int64, int64) (Role, error) {
	return RoleGroupMember, nil
}

func TestCodeIdentityResolverSupportsConcurrentResolution(t *testing.T) {
	const calls = 64
	resolver, err := NewCodeIdentityResolver(100, concurrentIdentityRoleSource{})
	if err != nil {
		t.Fatal(err)
	}
	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, calls)
	for index := 0; index < calls; index++ {
		waitGroup.Add(1)
		go func(userID int64) {
			defer waitGroup.Done()
			role, resolveErr := resolver.Resolve(context.Background(), 200, userID)
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
