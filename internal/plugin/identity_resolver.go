package plugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrIdentityInvalidSubject = errors.New("身份解析目标无效")
	ErrIdentityUnknownRole    = errors.New("群成员身份未知")
)

// GroupRoleSource 根据可信群号与用户号提供当前群身份，实现必须支持并发调用。
type GroupRoleSource interface {
	ResolveGroupRole(ctx context.Context, groupID int64, userID int64) (Role, error)
}

// CodeIdentityResolver 将平台最高管理员和群身份来源映射为代码 Role。
type CodeIdentityResolver struct {
	superAdminID int64
	groupRoles   GroupRoleSource
}

// NewCodeIdentityResolver 创建不依赖旧权限矩阵的代码身份解析器。
func NewCodeIdentityResolver(superAdminID int64, groupRoles GroupRoleSource) (*CodeIdentityResolver, error) {
	if superAdminID < 0 {
		return nil, errors.New("最高管理员 QQ 不能为负数")
	}
	if isNilGroupRoleSource(groupRoles) {
		return nil, errors.New("群身份来源不能为空")
	}
	return &CodeIdentityResolver{superAdminID: superAdminID, groupRoles: groupRoles}, nil
}

func isNilGroupRoleSource(source GroupRoleSource) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Resolve 按最高管理员优先级解析封闭代码身份。
func (r *CodeIdentityResolver) Resolve(ctx context.Context, groupID int64, userID int64) (Role, error) {
	if r == nil || r.groupRoles == nil || groupID <= 0 || userID <= 0 {
		return "", ErrIdentityInvalidSubject
	}
	if r.superAdminID > 0 && userID == r.superAdminID {
		return RoleSuperAdmin, nil
	}
	role, err := r.groupRoles.ResolveGroupRole(ctx, groupID, userID)
	if err != nil {
		return "", fmt.Errorf("查询群身份: %w", err)
	}
	switch role {
	case RoleGroupOwner, RoleGroupAdmin, RoleGroupMember:
		return role, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrIdentityUnknownRole, role)
	}
}
