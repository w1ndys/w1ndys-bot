// 📌 影响范围：读写 PostgreSQL 的 plugin_config 表；依赖 pgx 连接池。
package plugin

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore 使用 PostgreSQL 持久化插件状态。
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore 创建插件状态仓库。
// @param pool：可用的 PostgreSQL 连接池。
// @returns PostgresStore。
// ⚠️副作用说明：无；仅保存连接池引用。
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	store := &PostgresStore{pool: pool}

	// >>> 数据演变示例
	// 1. 有效 pool -> PostgresStore{pool} -> 可加载插件状态。
	// 2. nil pool -> PostgresStore{nil} -> 调用方法时由程序组装阶段负责避免。
	return store
}

// Load 加载所有插件的启用状态和优先级。
// @param ctx：控制数据库查询生命周期。
// @returns 插件状态列表或查询、扫描错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (s *PostgresStore) Load(ctx context.Context) ([]State, error) {
	rows, err := s.pool.Query(ctx, `SELECT plugin_name, enabled, priority, config_json, config_version FROM plugin_config ORDER BY priority DESC, plugin_name ASC`)
	// [决策理由] 查询失败时没有完整状态快照，必须返回错误而非部分初始化。
	if err != nil {
		return nil, fmt.Errorf("查询 plugin_config: %w", err)
	}
	defer rows.Close()

	states := make([]State, 0)
	for rows.Next() {
		var state State
		// [决策理由] 任一行无法扫描都说明表结构或数据异常，不能静默遗漏插件状态。
		if err := rows.Scan(&state.Name, &state.Enabled, &state.Priority, &state.ConfigJSON, &state.ConfigVersion); err != nil {
			return nil, fmt.Errorf("扫描 plugin_config: %w", err)
		}
		states = append(states, state)
	}
	// [决策理由] 迭代结束仍可能包含网络错误，必须在返回快照前检查。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 plugin_config: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB[A:true:10,B:false:0] -> 顺序扫描 -> []State{A,B}。
	// 2. 空表 -> 零行 -> 返回空切片,nil。
	return states, nil
}

// SaveEnabled 保存单个插件的启用状态。
// @param ctx：控制写入生命周期；name：插件名；enabled：目标状态。
// @returns 数据库写入错误。
// ⚠️副作用说明：更新 PostgreSQL plugin_config 表。
func (s *PostgresStore) SaveEnabled(ctx context.Context, name string, enabled bool) error {
	result, err := s.pool.Exec(ctx, `UPDATE plugin_config SET enabled = $2, updated_at = NOW() WHERE plugin_name = $1`, name, enabled)
	// [决策理由] SQL 执行失败时状态未可靠持久化，必须返回错误。
	if err != nil {
		return fmt.Errorf("更新 plugin_config: %w", err)
	}
	// [决策理由] 零行更新表示插件尚无数据库记录，避免界面显示成功但重启后丢失状态。
	if result.RowsAffected() != 1 {
		return fmt.Errorf("插件 %q 的数据库记录不存在", name)
	}

	// >>> 数据演变示例
	// 1. sign_in 存在 + true -> UPDATE 1 行 -> nil。
	// 2. missing + true -> UPDATE 0 行 -> 返回记录不存在错误。
	return nil
}
