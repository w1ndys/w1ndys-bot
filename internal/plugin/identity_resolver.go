package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

var (
	ErrIdentityInvalidSubject = errors.New("身份解析目标无效")
	ErrIdentityUnknownRole    = errors.New("群成员身份未知")
)

// CodeIdentityResolver 使用最高管理员快照和 NapCat 上报的群角色解析封闭代码身份。
type CodeIdentityResolver struct {
	superAdminID int64
}

// NewCodeIdentityResolver 创建不依赖旧权限矩阵的代码身份解析器。
func NewCodeIdentityResolver(superAdminID int64) (*CodeIdentityResolver, error) {
	if superAdminID < 0 {
		return nil, errors.New("最高管理员 QQ 不能为负数")
	}
	return &CodeIdentityResolver{superAdminID: superAdminID}, nil
}

// Resolve 按最高管理员优先级解析群消息发送者的封闭代码身份。
// 群角色取自 NapCat 上报的 sender 字段；未知或缺失角色一律 fail-closed。
func (r *CodeIdentityResolver) Resolve(_ context.Context, message *ws.MessageEvent) (Role, error) {
	if r == nil || message == nil || message.MessageType != "group" || message.GroupID <= 0 || message.UserID <= 0 {
		return "", ErrIdentityInvalidSubject
	}
	// 发送者与事件用户不一致时无法确定角色归属，必须拒绝而不是就近取值。
	if message.Sender.UserID != 0 && message.Sender.UserID != message.UserID {
		return "", fmt.Errorf("%w: sender %d 与事件用户 %d 不一致", ErrIdentityInvalidSubject, message.Sender.UserID, message.UserID)
	}
	if r.superAdminID > 0 && message.UserID == r.superAdminID {
		return RoleSuperAdmin, nil
	}
	switch message.Sender.Role {
	case "owner":
		return RoleGroupOwner, nil
	case "admin":
		return RoleGroupAdmin, nil
	case "member":
		return RoleGroupMember, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrIdentityUnknownRole, message.Sender.Role)
	}
}
