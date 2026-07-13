// 📌 影响范围：调用管理 Repository，并在写入后刷新 PluginManager 运行快照。
package admin

import (
	"context"
	"fmt"
)

// RuntimeRefresher 定义数据库管理变更后的运行时刷新能力。
type RuntimeRefresher interface {
	Load(context.Context) error
}

// Service 是 QQ 管理命令与 WebUI 共用的管理业务入口。
type Service struct {
	repository Repository
	runtime    RuntimeRefresher
}

// NewService 创建管理服务。
// @param repository：插件管理仓库；runtime：插件运行时刷新器。
// @returns 可复用的管理服务。
// ⚠️副作用说明：无；仅保存依赖引用。
func NewService(repository Repository, runtime RuntimeRefresher) *Service {
	service := &Service{repository: repository, runtime: runtime}

	// >>> 数据演变示例
	// 1. PostgreSQL Repository + Manager -> Service -> 支持持久化与热刷新。
	// 2. Repository + nil Runtime -> Service -> 仅持久化管理配置。
	return service
}

// ListPlugins 返回管理端插件列表。
// @param ctx：查询生命周期。
// @returns 插件快照或仓库错误。
// ⚠️副作用说明：调用 Repository 执行只读查询。
func (s *Service) ListPlugins(ctx context.Context) ([]PluginState, error) {
	states, err := s.repository.ListPlugins(ctx)
	// [决策理由] 管理端需要明确区分空列表和查询失败。
	if err != nil {
		return nil, fmt.Errorf("列出插件: %w", err)
	}

	// >>> 数据演变示例
	// 1. Repository=[ping] -> Service -> [ping]。
	// 2. Repository error -> 包装上下文 -> error。
	return states, nil
}

// SetPluginEnabled 修改插件启用状态并热刷新运行时。
// @param ctx：操作生命周期；actor：操作者；name：插件名；enabled：目标状态。
// @returns 更新后的插件快照或校验、仓库、刷新错误。
// ⚠️副作用说明：更新数据库、写审计，并可能触发插件启用或禁用回调。
func (s *Service) SetPluginEnabled(ctx context.Context, actor Actor, name string, enabled bool) (PluginState, error) {
	state, err := s.updatePlugin(ctx, actor, name, PluginPatch{Enabled: &enabled})

	// >>> 数据演变示例
	// 1. ping:false + true -> 事务写入 -> Runtime.Load -> ping:true。
	// 2. actor.ID="" -> 校验失败 -> 数据库不变。
	return state, err
}

// SetPluginPriority 修改插件优先级并热刷新运行时排序。
// @param ctx：操作生命周期；actor：操作者；name：插件名；priority：目标优先级。
// @returns 更新后的插件快照或校验、仓库、刷新错误。
// ⚠️副作用说明：更新数据库、写审计，并重新发布插件运行顺序。
func (s *Service) SetPluginPriority(ctx context.Context, actor Actor, name string, priority int) (PluginState, error) {
	state, err := s.updatePlugin(ctx, actor, name, PluginPatch{Priority: &priority})

	// >>> 数据演变示例
	// 1. ping:0 + 100 -> 事务写入 -> Runtime.Load -> ping:100。
	// 2. missing + 10 -> ErrPluginNotFound -> 不刷新运行时。
	return state, err
}

// updatePlugin 校验管理上下文、写入变更并刷新运行时。
// @param ctx：操作生命周期；actor：操作者；name：插件名；patch：字段变更。
// @returns 更新后的插件快照或业务错误。
// ⚠️副作用说明：调用 Repository 写事务，并可能刷新插件运行状态。
func (s *Service) updatePlugin(ctx context.Context, actor Actor, name string, patch PluginPatch) (PluginState, error) {
	// [决策理由] 审计记录必须能定位真实操作者，空 ID 不允许进入仓库。
	if actor.ID == "" {
		return PluginState{}, ErrInvalidActor
	}
	// [决策理由] 数据库约束只允许已定义的双控制通道和系统操作。
	if !validChannel(actor.Channel) {
		return PluginState{}, ErrInvalidChannel
	}
	// [决策理由] 空插件名不能稳定定位配置与审计目标。
	if name == "" {
		return PluginState{}, fmt.Errorf("%w: 名称为空", ErrPluginNotFound)
	}
	state, err := s.repository.UpdatePlugin(ctx, actor, name, patch)
	// [决策理由] 持久化失败时数据库未形成新目标状态，不应刷新运行时。
	if err != nil {
		return PluginState{}, fmt.Errorf("更新插件 %s: %w", name, err)
	}
	// [决策理由] 无运行时刷新器适用于迁移工具等仅管理数据库的进程。
	if s.runtime == nil {
		return state, nil
	}
	// [决策理由] 写入后立即刷新，使 QQ 与 WebUI 修改无需重启即可生效。
	if err := s.runtime.Load(ctx); err != nil {
		return state, fmt.Errorf("刷新插件 %s 运行状态: %w", name, err)
	}

	// >>> 数据演变示例
	// 1. qq管理员 + ping启用 -> Repository事务 -> Runtime.Load -> 返回新状态。
	// 2. 非法channel -> 校验拒绝 -> Repository不调用。
	return state, nil
}

// validChannel 判断管理来源是否可写入审计表。
// @param channel：待校验的管理通道。
// @returns webui、qq、system 返回 true，其余返回 false。
// ⚠️副作用说明：无。
func validChannel(channel Channel) bool {
	switch channel {
	case ChannelWebUI, ChannelQQ, ChannelSystem:
		return true
	default:
		return false
	}

	// >>> 数据演变示例
	// 1. qq -> 命中允许分支 -> true。
	// 2. cli -> default -> false。
}
