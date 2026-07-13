// 📌 影响范围：从 SystemAdminRepository 读取管理员账号；原子发布进程内授权快照。
package admin

import (
	"context"
	"fmt"
	"sync/atomic"
)

type adminSnapshot struct {
	enabled map[string]SystemAdmin
}

// AdminResolver 提供并发安全的最高管理员身份解析。
type AdminResolver struct {
	repository SystemAdminRepository
	snapshot   atomic.Pointer[adminSnapshot]
}

// NewAdminResolver 创建空的最高管理员解析器。
// @param repository：管理员账号数据源。
// @returns 尚未加载数据的解析器。
// ⚠️副作用说明：仅初始化空内存快照，不访问数据库。
func NewAdminResolver(repository SystemAdminRepository) *AdminResolver {
	resolver := &AdminResolver{repository: repository}
	resolver.snapshot.Store(&adminSnapshot{enabled: map[string]SystemAdmin{}})

	// >>> 数据演变示例
	// 1. PostgreSQL Repository -> 空快照 Resolver -> Load 后可授权。
	// 2. nil Repository -> 空快照 Resolver -> IsSuperAdmin 始终 false。
	return resolver
}

// Load 从仓库构建并原子发布最高管理员快照。
// @param ctx：控制数据库查询生命周期。
// @returns 仓库查询错误；成功时返回 nil。
// ⚠️副作用说明：读取管理员仓库并替换进程内授权快照。
func (r *AdminResolver) Load(ctx context.Context) error {
	// [决策理由] 无仓库时无法得到可信身份数据，保持旧快照并返回组装错误。
	if r.repository == nil {
		return fmt.Errorf("最高管理员仓库未配置")
	}
	admins, err := r.repository.ListSystemAdmins(ctx)
	// [决策理由] 查询失败时保留上一份可用快照，避免瞬时数据库故障清空权限。
	if err != nil {
		return fmt.Errorf("加载最高管理员: %w", err)
	}
	enabled := make(map[string]SystemAdmin, len(admins))
	for _, account := range admins {
		// [决策理由] 禁用账号不得进入授权索引，但仍保留在数据库供审计和恢复。
		if !account.Enabled {
			continue
		}
		// [决策理由] 空 QQ 号无法对应真实用户，拒绝发布为有效身份。
		if account.UserID == "" {
			continue
		}
		enabled[account.UserID] = account
	}
	r.snapshot.Store(&adminSnapshot{enabled: enabled})

	// >>> 数据演变示例
	// 1. [100:true,200:false] -> 过滤 -> snapshot{100}。
	// 2. Repository空列表 -> 发布空快照 -> 所有用户无最高权限。
	return nil
}

// IsSuperAdmin 判断 QQ 号是否为当前启用的最高管理员。
// @param userID：待校验 QQ 号字符串。
// @returns 存在于当前启用快照时返回 true。
// ⚠️副作用说明：无；仅读取原子快照。
func (r *AdminResolver) IsSuperAdmin(userID string) bool {
	current := r.snapshot.Load()
	// [决策理由] 防御未通过构造器创建的零值 Resolver，避免空指针异常。
	if current == nil {
		return false
	}
	_, exists := current.enabled[userID]

	// >>> 数据演变示例
	// 1. snapshot{100} + userID=100 -> true。
	// 2. snapshot{100} + userID=200 -> false。
	return exists
}

// Resolve 返回启用最高管理员的身份详情。
// @param userID：待查询 QQ 号字符串。
// @returns 管理员快照及是否存在。
// ⚠️副作用说明：无；返回结构体副本，不暴露内部 map。
func (r *AdminResolver) Resolve(userID string) (SystemAdmin, bool) {
	current := r.snapshot.Load()
	// [决策理由] 零值 Resolver 没有可查询快照，应安全返回未找到。
	if current == nil {
		return SystemAdmin{}, false
	}
	account, exists := current.enabled[userID]

	// >>> 数据演变示例
	// 1. snapshot{100:卷卷} -> Resolve(100) -> account,true。
	// 2. snapshot{} -> Resolve(100) -> 零值,false。
	return account, exists
}
