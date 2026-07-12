// 📌 影响范围：定义权限角色、效果和默认策略；不访问数据库或外部变量。
package permission

// Role 表示 QQ 用户在当前操作中的身份。
type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleGroupOwner Role = "group_owner"
	RoleGroupAdmin Role = "group_admin"
	RoleMember     Role = "member"
)

// Effect 表示显式允许或拒绝。
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Defaults 表示 Manifest 声明的最终回退权限。
type Defaults struct {
	SuperAdmin bool
	GroupOwner bool
	GroupAdmin bool
	Member     bool
}

// Allows 返回默认策略是否允许指定角色。
// @param role：待判断角色。
// @returns 对应角色的默认布尔值，未知角色返回 false。
// ⚠️副作用说明：无。
func (d Defaults) Allows(role Role) bool {
	switch role {
	case RoleSuperAdmin:
		return d.SuperAdmin
	case RoleGroupOwner:
		return d.GroupOwner
	case RoleGroupAdmin:
		return d.GroupAdmin
	case RoleMember:
		return d.Member
	default:
		return false
	}

	// >>> 数据演变示例
	// 1. Defaults{Member:true}+member -> true。
	// 2. Defaults{}+unknown -> false。
}

// Policy 表示一条数据库权限覆盖规则。
type Policy struct {
	ID          int64
	ScopeType   string
	ScopeID     string
	PluginName  string
	FeatureKey  string
	SubjectRole Role
	Effect      Effect
}
