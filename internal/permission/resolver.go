// 📌 影响范围：从 PostgreSQL 读取权限策略；维护进程内不可变权限快照。
package permission

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

const pluginLevelFeature = "*"

// Resolver 按具体程度解析插件功能权限。
type Resolver struct {
	pool     *pgxpool.Pool
	snapshot atomic.Pointer[map[string]Effect]
}

// NewResolver 创建空权限解析器。
// @param pool：PostgreSQL 连接池。
// @returns Resolver。
// ⚠️副作用说明：分配并发布空权限快照。
func NewResolver(pool *pgxpool.Pool) *Resolver {
	resolver := &Resolver{pool: pool}
	empty := make(map[string]Effect)
	resolver.snapshot.Store(&empty)

	// >>> 数据演变示例
	// 1. pool -> Resolver{empty snapshot}。
	// 2. nil -> 可使用 Replace 做纯内存解析测试。
	return resolver
}

// Load 从数据库原子刷新权限快照。
// @param ctx：控制查询生命周期。
// @returns 查询、扫描或策略校验错误。
// ⚠️副作用说明：读取 PostgreSQL 并替换内存快照。
func (r *Resolver) Load(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `
        SELECT id, scope_type, scope_id, plugin_name, COALESCE(feature_key, ''), subject_type, subject_id, effect
        FROM permission_policies`)
	// [决策理由] 查询失败时保留旧快照，避免权限规则瞬间丢失。
	if err != nil {
		return fmt.Errorf("查询权限策略: %w", err)
	}
	defer rows.Close()
	policies := make([]Policy, 0)
	for rows.Next() {
		var policy Policy
		// [决策理由] 任一策略无法扫描都不能发布部分权限快照。
		if err := rows.Scan(&policy.ID, &policy.ScopeType, &policy.ScopeID, &policy.PluginName, &policy.FeatureKey, &policy.SubjectType, &policy.SubjectID, &policy.Effect); err != nil {
			return fmt.Errorf("扫描权限策略: %w", err)
		}
		policies = append(policies, policy)
	}
	// [决策理由] 迭代错误可能发生在部分读取之后，必须检查。
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历权限策略: %w", err)
	}
	// [决策理由] Replace 统一执行数据库和测试使用的策略校验。
	if err := r.Replace(policies); err != nil {
		return err
	}

	// >>> 数据演变示例
	// 1. DB 策略列表 -> Map -> 原子发布。
	// 2. DB 查询失败 -> 返回错误 -> 旧快照继续生效。
	return nil
}

// Replace 校验并替换完整权限快照。
// @param policies：数据库权限策略列表。
// @returns 无效字段或重复规则错误。
// ⚠️副作用说明：成功时替换内存权限快照。
func (r *Resolver) Replace(policies []Policy) error {
	next := make(map[string]Effect, len(policies))
	for _, policy := range policies {
		// [决策理由] 全局规则必须固定在 scope_id=0。
		if policy.ScopeType == "global" && policy.ScopeID != "0" {
			return fmt.Errorf("全局权限规则 scope_id 必须为 0")
		}
		// [决策理由] 群规则必须提供具体群号。
		if policy.ScopeType == "group" && policy.ScopeID == "0" {
			return fmt.Errorf("群级权限规则缺少群号")
		}
		// [决策理由] 未知作用域不能进入固定优先级解析链。
		if policy.ScopeType != "global" && policy.ScopeType != "group" {
			return fmt.Errorf("未知权限作用域 %q", policy.ScopeType)
		}
		// [决策理由] 只允许显式 allow/deny，其他值不能默认解释。
		if policy.Effect != EffectAllow && policy.Effect != EffectDeny {
			return fmt.Errorf("未知权限效果 %q", policy.Effect)
		}
		// [决策理由] 权限主体只允许稳定的角色或指定用户两类，避免无法解析的扩展值。
		if policy.SubjectType != SubjectRole && policy.SubjectType != SubjectUser {
			return fmt.Errorf("未知权限主体类型 %q", policy.SubjectType)
		}
		// [决策理由] 空主体无法匹配任何请求，应在发布快照前拒绝。
		if policy.SubjectID == "" {
			return fmt.Errorf("权限主体不能为空")
		}
		// [决策理由] 角色主体必须能映射到运行时支持的固定角色集合。
		if policy.SubjectType == SubjectRole {
			role := Role(policy.SubjectID)
			// [决策理由] 未知角色无法与消息身份匹配，不能进入快照。
			if role != RoleSuperAdmin && role != RoleGroupOwner && role != RoleGroupAdmin && role != RoleMember {
				return fmt.Errorf("未知权限角色 %q", policy.SubjectID)
			}
		}
		// [决策理由] 用户主体必须是正十进制 QQ 号，与消息 UserID 字符串格式一致。
		if policy.SubjectType == SubjectUser {
			userID, err := strconv.ParseUint(policy.SubjectID, 10, 64)
			// [决策理由] 解析失败或零值策略永远无法安全匹配真实用户。
			if err != nil || userID == 0 {
				return fmt.Errorf("权限用户 QQ %q 格式无效", policy.SubjectID)
			}
		}
		feature := policy.FeatureKey
		// [决策理由] 数据库 NULL 经扫描变为空字符串，内存使用星号表示插件级策略。
		if feature == "" {
			feature = pluginLevelFeature
		}
		key := policyKey(policy.ScopeType, policy.ScopeID, policy.PluginName, feature, policy.SubjectType, policy.SubjectID)
		// [决策理由] 相同具体程度的重复规则会导致结果不确定。
		if _, exists := next[key]; exists {
			return fmt.Errorf("权限规则重复: %s", key)
		}
		next[key] = policy.Effect
	}
	r.snapshot.Store(&next)

	// >>> 数据演变示例
	// 1. 全局功能 allow + 群插件 deny -> 两个不同 key -> 发布成功。
	// 2. 两条相同群功能角色规则 -> 重复 key -> 返回错误。
	return nil
}

// Allowed 先按指定用户、再按角色的具体程度判断功能权限。
// @param groupID：当前群号；pluginName：插件名；featureKey：功能键；userID：QQ号；role：用户角色；defaults：Manifest 默认值。
// @returns 最具体显式策略或默认策略结果。
// ⚠️副作用说明：无；仅读取不可变快照。
func (r *Resolver) Allowed(groupID string, pluginName string, featureKey string, userID string, role Role, defaults Defaults) bool {
	current := r.snapshot.Load()
	candidates := []string{
		policyKey("group", groupID, pluginName, featureKey, SubjectUser, userID),
		policyKey("group", groupID, pluginName, pluginLevelFeature, SubjectUser, userID),
		policyKey("global", "0", pluginName, featureKey, SubjectUser, userID),
		policyKey("global", "0", pluginName, pluginLevelFeature, SubjectUser, userID),
		policyKey("group", groupID, pluginName, featureKey, SubjectRole, string(role)),
		policyKey("group", groupID, pluginName, pluginLevelFeature, SubjectRole, string(role)),
		policyKey("global", "0", pluginName, featureKey, SubjectRole, string(role)),
		policyKey("global", "0", pluginName, pluginLevelFeature, SubjectRole, string(role)),
	}
	for _, key := range candidates {
		effect, exists := (*current)[key]
		// [决策理由] 第一条命中的规则是最具体策略，应立即返回而不继续回退。
		if exists {
			return effect == EffectAllow
		}
	}

	// >>> 数据演变示例
	// 1. 群功能 deny + 全局插件 allow -> 首条命中 deny -> false。
	// 2. 无显式规则 + Defaults.Member=true -> true。
	return defaults.Allows(role)
}

// policyKey 生成权限快照键。
// @param scopeType、scopeID、pluginName、featureKey、subjectType、subjectID：权限规则维度。
// @returns 使用 NUL 分隔的稳定键。
// ⚠️副作用说明：无。
func policyKey(scopeType string, scopeID string, pluginName string, featureKey string, subjectType SubjectType, subjectID string) string {
	result := scopeType + "\x00" + scopeID + "\x00" + pluginName + "\x00" + featureKey + "\x00" + string(subjectType) + "\x00" + subjectID

	// >>> 数据演变示例
	// 1. group,123,score,rank,member -> 唯一群功能角色键。
	// 2. global,0,score,*,member -> 唯一全局插件角色键。
	return result
}
