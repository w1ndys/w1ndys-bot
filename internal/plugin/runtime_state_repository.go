package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeStateDatabase interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PersistedGroupState 是一个插件的显式逐群开关快照。
type PersistedGroupState struct {
	GroupID   int64
	Enabled   bool
	Version   int64
	UpdatedAt time.Time
}

// PersistedPluginState 是管理员意图及其全部逐群开关快照。
type PersistedPluginState struct {
	PluginKey      string
	DesiredEnabled bool
	Version        int64
	UpdatedAt      time.Time
	Groups         []PersistedGroupState
}

// PostgresRuntimeStateRepository 持久化 V2 插件运行意图。
type PostgresRuntimeStateRepository struct {
	database runtimeStateDatabase
}

// NewPostgresRuntimeStateRepository 创建 V2 插件状态仓库。
func NewPostgresRuntimeStateRepository(pool *pgxpool.Pool) (*PostgresRuntimeStateRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("插件状态数据库连接池不能为空")
	}
	return &PostgresRuntimeStateRepository{database: pool}, nil
}

// SyncCatalog 为当前编译目录补充默认关闭的全局状态，不覆盖已有意图。
func (r *PostgresRuntimeStateRepository) SyncCatalog(ctx context.Context, catalog *SpecCatalog) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("插件状态仓库未初始化")
	}
	if catalog == nil {
		return fmt.Errorf("插件规格目录不能为空")
	}
	specs := catalog.Specs()
	if len(specs) == 0 {
		return nil
	}
	keys := make([]string, len(specs))
	for index, spec := range specs {
		keys[index] = spec.Key
	}
	_, err := r.database.Exec(ctx, `INSERT INTO plugin_states(plugin_key,desired_enabled)
SELECT plugin_key,FALSE FROM unnest($1::text[]) AS plugin_key
ON CONFLICT (plugin_key) DO NOTHING`, keys)
	if err != nil {
		return fmt.Errorf("同步插件状态目录: %w", err)
	}
	return nil
}

// LoadSnapshot 一次查询读取全部全局意图和逐群开关。
func (r *PostgresRuntimeStateRepository) LoadSnapshot(ctx context.Context) ([]PersistedPluginState, error) {
	if r == nil || r.database == nil {
		return nil, fmt.Errorf("插件状态仓库未初始化")
	}
	rows, err := r.database.Query(ctx, `SELECT s.plugin_key,s.desired_enabled,s.version,s.updated_at,
       g.group_id,g.enabled,g.version,g.updated_at
FROM plugin_states s
LEFT JOIN plugin_group_states g ON g.plugin_key=s.plugin_key
ORDER BY s.plugin_key,g.group_id`)
	if err != nil {
		return nil, fmt.Errorf("查询插件状态快照: %w", err)
	}
	defer rows.Close()

	states := make([]PersistedPluginState, 0)
	for rows.Next() {
		var pluginKey string
		var desiredEnabled bool
		var pluginVersion int64
		var pluginUpdatedAt time.Time
		var groupID *int64
		var groupEnabled *bool
		var groupVersion *int64
		var groupUpdatedAt *time.Time
		if err := rows.Scan(&pluginKey, &desiredEnabled, &pluginVersion, &pluginUpdatedAt, &groupID, &groupEnabled, &groupVersion, &groupUpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描插件状态快照: %w", err)
		}
		if len(states) == 0 || states[len(states)-1].PluginKey != pluginKey {
			states = append(states, PersistedPluginState{
				PluginKey: pluginKey, DesiredEnabled: desiredEnabled, Version: pluginVersion,
				UpdatedAt: pluginUpdatedAt.UTC(), Groups: make([]PersistedGroupState, 0),
			})
		}
		if groupID != nil && groupEnabled != nil && groupVersion != nil && groupUpdatedAt != nil {
			last := &states[len(states)-1]
			last.Groups = append(last.Groups, PersistedGroupState{
				GroupID: *groupID, Enabled: *groupEnabled, Version: *groupVersion, UpdatedAt: groupUpdatedAt.UTC(),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历插件状态快照: %w", err)
	}
	return states, nil
}
