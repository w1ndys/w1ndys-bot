// 📌 影响范围：从 PostgreSQL 读取启用命令；维护进程内不可变命令快照。
package command

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Registry 提供命令热加载与无锁匹配。
type Registry struct {
	pool     *pgxpool.Pool
	snapshot atomic.Pointer[map[string]Binding]
}

// NewRegistry 创建空命令注册中心。
// @param pool：PostgreSQL 连接池。
// @returns Registry。
// ⚠️副作用说明：分配并保存一个空快照。
func NewRegistry(pool *pgxpool.Pool) *Registry {
	registry := &Registry{pool: pool}
	empty := make(map[string]Binding)
	registry.snapshot.Store(&empty)

	// >>> 数据演变示例
	// 1. pool -> Registry{empty snapshot}。
	// 2. nil pool -> 可用纯内存 Replace，Load 由调用方避免。
	return registry
}

// Load 从数据库构建并原子替换完整命令快照。
// @param ctx：控制数据库查询生命周期。
// @returns 查询、扫描或重复数据错误。
// ⚠️副作用说明：读取 PostgreSQL 并替换内存快照。
func (r *Registry) Load(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `
        SELECT c.id, c.scope_type, c.scope_id, c.plugin_name, c.feature_key,
               c.command, c.normalized_command, c.enabled
        FROM plugin_commands c
        JOIN plugin_definitions p ON p.plugin_name = c.plugin_name AND p.installed = TRUE
        JOIN plugin_features f ON f.plugin_name = c.plugin_name
                              AND f.feature_key = c.feature_key
                              AND f.installed = TRUE
        WHERE c.enabled = TRUE`)
	// [决策理由] 查询失败时必须保留旧快照，避免瞬间清空所有命令。
	if err != nil {
		return fmt.Errorf("查询插件命令: %w", err)
	}
	defer rows.Close()
	bindings := make([]Binding, 0)
	for rows.Next() {
		var binding Binding
		// [决策理由] 任一行解析失败都不能发布不完整快照。
		if err := rows.Scan(&binding.ID, &binding.ScopeType, &binding.ScopeID, &binding.PluginName, &binding.FeatureKey, &binding.Command, &binding.NormalizedCommand, &binding.Enabled); err != nil {
			return fmt.Errorf("扫描插件命令: %w", err)
		}
		bindings = append(bindings, binding)
	}
	// [决策理由] 迭代错误可能发生在部分读取后，必须在发布前检查。
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历插件命令: %w", err)
	}
	// [决策理由] Replace 统一验证作用域和重复项，数据库与测试共享快照规则。
	if err := r.Replace(bindings); err != nil {
		return err
	}

	// >>> 数据演变示例
	// 1. DB[全局签到,群123打卡] -> 构建 Map -> 原子发布。
	// 2. DB 查询失败 -> 返回错误 -> 旧快照继续服务。
	return nil
}

// Replace 验证并原子替换命令快照。
// @param bindings：完整启用命令列表。
// @returns 无效作用域或快照内重复命令错误。
// ⚠️副作用说明：成功时替换进程内命令快照。
func (r *Registry) Replace(bindings []Binding) error {
	next := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		// [决策理由] 全局作用域固定使用 scope_id=0，避免产生多个伪全局空间。
		if binding.ScopeType == ScopeGlobal && binding.ScopeID != "0" {
			return fmt.Errorf("全局命令 %q 的 scope_id 必须为 0", binding.Command)
		}
		// [决策理由] 群级命令必须指向具体群号。
		if binding.ScopeType == ScopeGroup && binding.ScopeID == "0" {
			return fmt.Errorf("群级命令 %q 缺少群号", binding.Command)
		}
		// [决策理由] 只接受已定义作用域，防止未知规则绕过匹配优先级。
		if binding.ScopeType != ScopeGlobal && binding.ScopeType != ScopeGroup {
			return fmt.Errorf("命令 %q 的作用域无效", binding.Command)
		}
		key := snapshotKey(binding.ScopeType, binding.ScopeID, binding.NormalizedCommand)
		// [决策理由] 同一作用域重复命令会使路由目标不确定。
		if _, exists := next[key]; exists {
			return fmt.Errorf("命令 %q 在作用域内重复", binding.NormalizedCommand)
		}
		next[key] = binding
	}
	r.snapshot.Store(&next)

	// >>> 数据演变示例
	// 1. [global:签到,group123:签到] -> 不同 key -> 发布成功。
	// 2. [global:签到,global:签到] -> 重复 key -> 返回错误且保留旧快照。
	return nil
}

// Resolve 按群级优先于全局的规则匹配命令。
// @param groupID：群号；input：用户输入；prefix：系统命令前缀。
// @returns 匹配 Binding 与是否找到。
// ⚠️副作用说明：无；仅读取不可变快照。
func (r *Registry) Resolve(groupID string, input string, prefix string) (Binding, bool) {
	normalized, err := Normalize(input, prefix)
	// [决策理由] 无效输入不可能匹配已注册命令。
	if err != nil {
		return Binding{}, false
	}
	current := r.snapshot.Load()
	// [决策理由] 具体群配置应覆盖全局同名命令。
	if groupID != "" {
		if binding, exists := (*current)[snapshotKey(ScopeGroup, groupID, normalized)]; exists {
			return binding, true
		}
	}
	binding, exists := (*current)[snapshotKey(ScopeGlobal, "0", normalized)]

	// >>> 数据演变示例
	// 1. 群123和全局都有签到 -> 返回群123 Binding。
	// 2. 群123无打卡、全局有打卡 -> 返回全局 Binding。
	return binding, exists
}

// snapshotKey 生成不可碰撞的作用域内存键。
// @param scopeType：作用域类型；scopeID：作用域 ID；normalized：标准化命令。
// @returns 使用 NUL 分隔的快照键。
// ⚠️副作用说明：无。
func snapshotKey(scopeType ScopeType, scopeID string, normalized string) string {
	result := string(scopeType) + "\x00" + scopeID + "\x00" + normalized

	// >>> 数据演变示例
	// 1. global,0,签到 -> global\0 0\0签到。
	// 2. group,123,签到 -> group\0123\0签到。
	return result
}
