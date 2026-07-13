// 📌 影响范围：调用只读插件功能 Repository，并使用最高管理员授权快照。
package admin

import (
	"context"
	"fmt"
)

// ListPluginFeatures 校验管理员后返回插件功能元数据。
// @param ctx：查询生命周期；actor：操作者；pluginName：插件稳定名称。
// @returns 功能元数据或授权、插件未找到、仓库错误。
// ⚠️副作用说明：读取管理员快照和插件功能 Repository。
func (s *Service) ListPluginFeatures(ctx context.Context, actor Actor, pluginName string) ([]FeatureState, error) {
	// [决策理由] 功能元数据会暴露命令与默认权限设计，只允许最高管理员读取。
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	// [决策理由] 空插件名无法稳定定位功能集合。
	if pluginName == "" {
		return nil, fmt.Errorf("%w: 名称为空", ErrPluginNotFound)
	}
	features, err := s.repository.ListPluginFeatures(ctx, pluginName)
	// [决策理由] 查询失败时不应返回部分功能列表。
	if err != nil {
		return nil, fmt.Errorf("列出插件 %s 功能: %w", pluginName, err)
	}

	// >>> 数据演变示例
	// 1. 管理员+ping -> Repository -> [ping功能]。
	// 2. 非管理员 -> ErrForbidden -> 不查询数据库。
	return features, nil
}
