// 📌 影响范围：读取 PostgreSQL 插件群门禁配置；原子发布供高频事件路由读取的不可变快照。
package plugin

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// GroupGate 定义插件在指定事件所属群是否允许运行的内存判定能力。
type GroupGate interface {
	Load(context.Context) error
	Allowed(string, ws.Event) bool
}

type groupPolicy struct {
	DefaultEnabled bool
	Overrides      map[int64]bool
}

type groupGateSnapshot map[string]groupPolicy

// PostgresGroupGate 从数据库加载声明为可逐群控制的插件策略。
type PostgresGroupGate struct {
	pool     *pgxpool.Pool
	snapshot atomic.Pointer[groupGateSnapshot]
}

// NewPostgresGroupGate 创建默认放行的群门禁解析器。
// @param pool：应用共享PostgreSQL连接池。
// @returns 尚未加载数据库但可安全放行的解析器。
// ⚠️副作用说明：仅分配并发布空内存快照，不访问数据库。
func NewPostgresGroupGate(pool *pgxpool.Pool) *PostgresGroupGate {
	result := &PostgresGroupGate{pool: pool}
	empty := groupGateSnapshot{}
	result.snapshot.Store(&empty)

	// >>> 数据演变示例
	// 1. pool -> 空策略快照 -> 所有插件暂时放行。
	// 2. 后续Load -> 原子替换为数据库完整策略。
	return result
}

// Load 从数据库加载可逐群控制插件的默认值与覆盖值。
// @param ctx：控制数据库查询生命周期。
// @returns 查询、扫描或遍历错误。
// ⚠️副作用说明：查询三张插件表；成功后原子替换完整门禁快照。
func (g *PostgresGroupGate) Load(ctx context.Context) error {
	rows, err := g.pool.Query(ctx, `SELECT d.plugin_name,c.group_default_enabled,o.group_id,o.enabled FROM plugin_definitions d JOIN plugin_config c ON c.plugin_name=d.plugin_name LEFT JOIN plugin_group_overrides o ON o.plugin_name=d.plugin_name WHERE d.available=TRUE AND d.group_controllable=TRUE ORDER BY d.plugin_name,o.group_id`)
	// [决策理由] 查询失败时无法证明策略完整，必须保留旧快照并返回错误。
	if err != nil {
		return fmt.Errorf("查询插件群门禁: %w", err)
	}
	defer rows.Close()
	next := make(groupGateSnapshot)
	for rows.Next() {
		var pluginName string
		var defaultEnabled bool
		var groupID *int64
		var enabled *bool
		// [决策理由] LEFT JOIN无覆盖时允许空group字段，其余列必须完整扫描。
		if err := rows.Scan(&pluginName, &defaultEnabled, &groupID, &enabled); err != nil {
			return fmt.Errorf("扫描插件群门禁: %w", err)
		}
		policy, exists := next[pluginName]
		// [决策理由] 每个插件首次出现时建立独立覆盖map，后续行只追加覆盖。
		if !exists {
			policy = groupPolicy{DefaultEnabled: defaultEnabled, Overrides: make(map[int64]bool)}
		}
		// [决策理由] 无覆盖的LEFT JOIN行只负责发布默认值，不能写入零群号。
		if groupID != nil && enabled != nil {
			policy.Overrides[*groupID] = *enabled
		}
		next[pluginName] = policy
	}
	// [决策理由] 网络中断可能在迭代结束时才出现，部分策略不得发布。
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历插件群门禁: %w", err)
	}
	g.snapshot.Store(&next)

	// >>> 数据演变示例
	// 1. keyword默认true+群100=false -> policy{true,{100:false}}。
	// 2. 可控插件无覆盖 -> policy{default,{}}；非可控插件不进入快照。
	return nil
}

// Allowed 判断目标插件是否允许处理事件所属群。
// @param pluginName：插件稳定名称；event：待路由OneBot事件。
// @returns 私聊、无有效群号或非可控插件返回true；群策略按覆盖优先于默认值返回。
// ⚠️副作用说明：无；仅原子读取不可变快照。
func (g *PostgresGroupGate) Allowed(pluginName string, event ws.Event) bool {
	message, ok := event.(*ws.MessageEvent)
	// [决策理由] 群门禁只约束具备正GroupID的消息，私聊和非消息事件保持原行为。
	if !ok || message.MessageType != "group" || message.GroupID <= 0 {
		return true
	}
	current := g.snapshot.Load()
	// [决策理由] 初始化防御和未声明可控插件都应默认放行，避免门禁扩大作用域。
	if current == nil {
		return true
	}
	policy, controlled := (*current)[pluginName]
	// [决策理由] 快照只包含Manifest声明可控的插件，缺失即不应用群策略。
	if !controlled {
		return true
	}
	enabled, overridden := policy.Overrides[message.GroupID]
	// [决策理由] 明确的逐群覆盖优先于插件默认值。
	if overridden {
		return enabled
	}

	// >>> 数据演变示例
	// 1. keyword默认true+群100覆盖false -> false。
	// 2. echo不在快照或私聊GroupID=0 -> true。
	return policy.DefaultEnabled
}

// publishForTest 发布测试构造的不可变策略副本。
// @param policies：插件到默认值及群覆盖的测试策略。
// @returns 无。
// ⚠️副作用说明：深复制并原子替换解析器快照，仅供同包测试使用。
func (g *PostgresGroupGate) publishForTest(policies groupGateSnapshot) {
	next := make(groupGateSnapshot, len(policies))
	for name, policy := range policies {
		overrides := make(map[int64]bool, len(policy.Overrides))
		for groupID, enabled := range policy.Overrides {
			overrides[groupID] = enabled
		}
		next[name] = groupPolicy{DefaultEnabled: policy.DefaultEnabled, Overrides: overrides}
	}
	g.snapshot.Store(&next)

	// >>> 数据演变示例
	// 1. 输入{p:{false,{1:true}}} -> 深复制 -> p群1放行。
	// 2. 调用方后改原map -> 已发布快照不变。
}
