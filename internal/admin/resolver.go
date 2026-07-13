// 📌 影响范围：读取启动配置传入的 SUPER_ADMIN_QQ；不访问数据库。
package admin

import "strings"

// AdminResolver 提供单管理员模式的最高管理员身份解析。
type AdminResolver struct {
	userID string
}

// NewAdminResolver 使用环境配置快照创建唯一最高管理员解析器。
// @param userID：SUPER_ADMIN_QQ 配置值。
// @returns 固定到本次进程生命周期的解析器。
// ⚠️副作用说明：无；仅清理并保存配置副本。
func NewAdminResolver(userID string) *AdminResolver {
	resolver := &AdminResolver{userID: strings.TrimSpace(userID)}

	// >>> 数据演变示例
	// 1. " 100 " -> TrimSpace -> Resolver{userID:"100"}。
	// 2. "" -> Resolver{userID:""} -> 不授权任何账号。
	return resolver
}

// IsSuperAdmin 判断 QQ 号是否为环境配置中的唯一最高管理员。
// @param userID：待校验 QQ 号字符串。
// @returns 与非空 SUPER_ADMIN_QQ 完全一致时返回 true。
// ⚠️副作用说明：无。
func (r *AdminResolver) IsSuperAdmin(userID string) bool {
	// [决策理由] 空配置不得让空身份意外获得最高权限。
	if r == nil || r.userID == "" {
		return false
	}
	matched := userID == r.userID

	// >>> 数据演变示例
	// 1. SUPER_ADMIN_QQ=100 + userID=100 -> true。
	// 2. SUPER_ADMIN_QQ=100 + userID=200 -> false。
	return matched
}

// Resolve 返回唯一最高管理员的 WebUI 身份详情。
// @param userID：待查询 QQ 号字符串。
// @returns 匹配时返回固定启用身份，否则返回未找到。
// ⚠️副作用说明：无。
func (r *AdminResolver) Resolve(userID string) (SystemAdmin, bool) {
	// [决策理由] 所有入口共用相同精确匹配规则，避免登录与业务授权结果分叉。
	if !r.IsSuperAdmin(userID) {
		return SystemAdmin{}, false
	}
	account := SystemAdmin{UserID: r.userID, Nickname: "最高管理员", Enabled: true}

	// >>> 数据演变示例
	// 1. 配置100+Resolve(100) -> 固定启用身份,true。
	// 2. 配置100+Resolve(200) -> 零值,false。
	return account, true
}
