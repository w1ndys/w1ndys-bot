// 📌 影响范围：在 PostgreSQL 中同步 plugin_definitions、plugin_features 和 plugin_config 元数据。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
)

// Synchronizer 将编译时 Manifest 同步到数据库。
type Synchronizer struct {
	pool *pgxpool.Pool
}

// NewSynchronizer 创建插件元数据同步器。
// @param pool：PostgreSQL 连接池。
// @returns Synchronizer。
// ⚠️副作用说明：无；仅保存连接池引用。
func NewSynchronizer(pool *pgxpool.Pool) *Synchronizer {
	result := &Synchronizer{pool: pool}

	// >>> 数据演变示例
	// 1. pool -> Synchronizer{pool}。
	// 2. nil -> Synchronizer{nil}，调用方负责避免使用。
	return result
}

// Sync 在单个事务中同步全部 Manifest，并标记已移除插件。
// @param ctx：控制数据库事务；manifests：当前二进制内置插件清单。
// @returns Manifest 校验、序列化或数据库错误。
// ⚠️副作用说明：写入插件定义、功能和默认运行状态表。
func (s *Synchronizer) Sync(ctx context.Context, manifests []Manifest) error {
	seen := make(map[string]struct{}, len(manifests))
	for _, manifest := range manifests {
		// [决策理由] 数据库写入前统一校验，避免无效元数据产生部分事务操作。
		if err := manifest.Validate(); err != nil {
			return err
		}
		// [决策理由] 不同 Manifest 不能声明相同插件名。
		if _, exists := seen[manifest.Name]; exists {
			return fmt.Errorf("插件 Manifest %q 重复", manifest.Name)
		}
		seen[manifest.Name] = struct{}{}
	}
	transaction, err := s.pool.Begin(ctx)
	// [决策理由] 无事务时无法保证定义、功能和状态原子同步。
	if err != nil {
		return fmt.Errorf("开始插件同步事务: %w", err)
	}
	defer transaction.Rollback(ctx)
	// [决策理由] 每轮同步先标记全部未安装，随后当前 Manifest 再恢复 installed=true。
	if _, err := transaction.Exec(ctx, `UPDATE plugin_definitions SET installed = FALSE, updated_at = NOW()`); err != nil {
		return fmt.Errorf("标记插件未安装: %w", err)
	}
	for _, manifest := range manifests {
		// [决策理由] 任一插件同步失败都必须回滚整批元数据。
		if err := syncManifest(ctx, transaction, manifest); err != nil {
			return err
		}
	}
	// [决策理由] 只有全部 Manifest 成功后才提交可见状态。
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("提交插件同步事务: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB[A旧],manifests=[A新,B] -> A更新、B插入、两者 installed=true。
	// 2. DB[A,B],manifests=[A] -> B installed=false，历史配置保留。
	return nil
}

// syncManifest 同步单个插件定义、默认状态和全部功能。
// @param ctx：数据库上下文；transaction：活动事务；manifest：已校验元数据。
// @returns JSON 序列化或 SQL 错误。
// ⚠️副作用说明：在当前事务写入三张插件表。
func syncManifest(ctx context.Context, transaction pgx.Tx, manifest Manifest) error {
	_, err := transaction.Exec(ctx, `
        INSERT INTO plugin_definitions
            (plugin_name, display_name, description, version, priority, schema_version, installed)
        VALUES ($1, $2, $3, $4, $5, $6, TRUE)
        ON CONFLICT (plugin_name) DO UPDATE SET
            display_name = EXCLUDED.display_name,
            description = EXCLUDED.description,
            version = EXCLUDED.version,
            priority = EXCLUDED.priority,
            schema_version = EXCLUDED.schema_version,
            installed = TRUE,
            updated_at = NOW()`,
		manifest.Name, manifest.DisplayName, manifest.Description, manifest.Version, manifest.Priority, manifest.SchemaVersion)
	// [决策理由] 插件定义是功能外键父记录，失败后不能继续写功能。
	if err != nil {
		return fmt.Errorf("同步插件 %s 定义: %w", manifest.Name, err)
	}
	_, err = transaction.Exec(ctx, `
        INSERT INTO plugin_config (plugin_name, enabled, priority)
        VALUES ($1, FALSE, $2)
        ON CONFLICT (plugin_name) DO UPDATE SET priority = EXCLUDED.priority, updated_at = NOW()`, manifest.Name, manifest.Priority)
	// [决策理由] 运行状态表必须为新插件创建默认关闭记录。
	if err != nil {
		return fmt.Errorf("同步插件 %s 运行状态: %w", manifest.Name, err)
	}
	// [决策理由] 标记旧功能而不删除，避免将来级联删除管理员配置的命令和权限。
	if _, err := transaction.Exec(ctx, `UPDATE plugin_features SET installed = FALSE, updated_at = NOW() WHERE plugin_name = $1`, manifest.Name); err != nil {
		return fmt.Errorf("标记插件 %s 旧功能: %w", manifest.Name, err)
	}
	for _, feature := range manifest.Features {
		commands, err := json.Marshal(feature.DefaultCommands)
		// [决策理由] 默认命令无法序列化时不能写入损坏元数据。
		if err != nil {
			return fmt.Errorf("序列化插件 %s 功能 %s 命令: %w", manifest.Name, feature.Key, err)
		}
		permissions, err := json.Marshal(feature.DefaultPermissions)
		// [决策理由] 默认权限必须作为合法 JSONB 原子写入。
		if err != nil {
			return fmt.Errorf("序列化插件 %s 功能 %s 权限: %w", manifest.Name, feature.Key, err)
		}
		_, err = transaction.Exec(ctx, `
            INSERT INTO plugin_features
		        (plugin_name, feature_key, display_name, description, default_commands, default_permissions, installed)
		    VALUES ($1, $2, $3, $4, $5, $6, TRUE)
		    ON CONFLICT (plugin_name, feature_key) DO UPDATE SET
		        display_name = EXCLUDED.display_name,
		        description = EXCLUDED.description,
		        default_commands = EXCLUDED.default_commands,
		        default_permissions = EXCLUDED.default_permissions,
		        installed = TRUE,
		        updated_at = NOW()`,
			manifest.Name, feature.Key, feature.DisplayName, feature.Description, commands, permissions)
		// [决策理由] 任一功能失败都应回滚整个插件 Manifest。
		if err != nil {
			return fmt.Errorf("同步插件 %s 功能 %s: %w", manifest.Name, feature.Key, err)
		}
		for _, defaultCommand := range feature.DefaultCommands {
			normalized, err := commandregistry.Normalize(defaultCommand, "")
			// [决策理由] Manifest 默认命令也必须遵循运行时相同标准化规则。
			if err != nil {
				return fmt.Errorf("标准化插件 %s 功能 %s 默认命令: %w", manifest.Name, feature.Key, err)
			}
			_, err = transaction.Exec(ctx, `
                INSERT INTO plugin_commands
                    (scope_type, scope_id, plugin_name, feature_key, command, normalized_command, is_default)
                VALUES ('global', '0', $1, $2, $3, $4, TRUE)
                ON CONFLICT (scope_type, scope_id, normalized_command) DO NOTHING`,
				manifest.Name, feature.Key, defaultCommand, normalized)
			// [决策理由] 默认命令写入失败时 Manifest 与 Command Registry 不一致，必须回滚。
			if err != nil {
				return fmt.Errorf("同步插件 %s 功能 %s 默认命令: %w", manifest.Name, feature.Key, err)
			}
		}
	}

	// >>> 数据演变示例
	// 1. 新 ping Manifest -> definition + disabled config + ping feature。
	// 2. 已有 score v1 -> score v2 -> 更新当前功能并标记已移除功能 installed=false。
	return nil
}
