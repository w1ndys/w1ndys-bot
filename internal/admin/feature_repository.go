// 📌 影响范围：只读查询 PostgreSQL plugin_definitions 与 plugin_features 表。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ListPluginFeatures 返回指定插件的全部功能元数据。
// @param ctx：查询生命周期；pluginName：插件稳定名称。
// @returns 按功能键排序的元数据、插件未找到或数据库错误。
// ⚠️副作用说明：执行 PostgreSQL 只读查询。
func (r *PostgresRepository) ListPluginFeatures(ctx context.Context, pluginName string) ([]FeatureState, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT TRUE FROM plugin_definitions WHERE plugin_name=$1`, pluginName).Scan(&exists)
	// [决策理由] 无定义行需要与“合法插件没有功能”明确区分。
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrPluginNotFound, pluginName)
	}
	// [决策理由] 其他查询错误表示无法确认插件目标。
	if err != nil {
		return nil, fmt.Errorf("检查插件定义: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT plugin_name,feature_key,display_name,description,available,default_commands,default_permissions FROM plugin_features WHERE plugin_name=$1 ORDER BY feature_key`, pluginName)
	// [决策理由] 功能查询失败时不能返回不完整元数据。
	if err != nil {
		return nil, fmt.Errorf("查询插件功能: %w", err)
	}
	defer rows.Close()
	features := make([]FeatureState, 0)
	for rows.Next() {
		var state FeatureState
		var commands json.RawMessage
		// [决策理由] 任一功能行异常都会使前端选择目标不可信。
		if err := rows.Scan(&state.PluginName, &state.Key, &state.DisplayName, &state.Description, &state.Available, &commands, &state.DefaultPermissions); err != nil {
			return nil, fmt.Errorf("扫描插件功能: %w", err)
		}
		// [决策理由] 默认触发词必须保持字符串数组类型供前端展示。
		if err := json.Unmarshal(commands, &state.DefaultCommands); err != nil {
			return nil, fmt.Errorf("解析插件 %s 功能 %s 默认触发词: %w", pluginName, state.Key, err)
		}
		state.DefaultPermissions = append(json.RawMessage(nil), state.DefaultPermissions...)
		features = append(features, state)
	}
	// [决策理由] 迭代完成后仍需检查连接或协议错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历插件功能: %w", err)
	}

	// >>> 数据演变示例
	// 1. ping定义+ping功能 -> []FeatureState{ping}。
	// 2. missing插件 -> ErrPluginNotFound。
	return features, nil
}
