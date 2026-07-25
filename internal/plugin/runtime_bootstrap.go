package plugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

const runtimeBootstrapCleanupTimeout = 10 * time.Second

// RuntimeSnapshotRepository 提供启动时目录同步和持久化状态快照。
type RuntimeSnapshotRepository interface {
	SyncCatalog(context.Context, *SpecCatalog) error
	LoadSnapshot(context.Context) ([]PersistedPluginState, error)
}

// RuntimeBootstrap 将持久化管理员意图恢复到纯内存 RuntimeController。
type RuntimeBootstrap struct {
	catalog    *SpecCatalog
	controller *RuntimeController
	repository RuntimeSnapshotRepository
}

// NewRuntimeBootstrap 创建启动恢复服务。
func NewRuntimeBootstrap(catalog *SpecCatalog, controller *RuntimeController, repository RuntimeSnapshotRepository) (*RuntimeBootstrap, error) {
	if catalog == nil || controller == nil || isNilRuntimeSnapshotRepository(repository) {
		return nil, errors.New("运行时启动依赖不能为空")
	}
	return &RuntimeBootstrap{catalog: catalog, controller: controller, repository: repository}, nil
}

// Initialize 同步目录并恢复群开关与全局生命周期状态。
func (b *RuntimeBootstrap) Initialize(ctx context.Context) error {
	if b == nil || b.catalog == nil || b.controller == nil || isNilRuntimeSnapshotRepository(b.repository) {
		return errors.New("运行时启动服务未初始化")
	}
	if err := b.repository.SyncCatalog(ctx, b.catalog); err != nil {
		return fmt.Errorf("同步运行时插件目录: %w", err)
	}
	states, err := b.repository.LoadSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("加载运行时状态快照: %w", err)
	}
	known := make(map[string]struct{})
	for _, spec := range b.catalog.Specs() {
		known[spec.Key] = struct{}{}
	}
	validated := make(map[string]PersistedPluginState, len(states))
	seenPlugins := make(map[string]struct{})
	for _, state := range states {
		if _, found := known[state.PluginKey]; !found {
			continue
		}
		if _, duplicate := seenPlugins[state.PluginKey]; duplicate {
			return fmt.Errorf("插件 %s 状态快照重复", state.PluginKey)
		}
		seenPlugins[state.PluginKey] = struct{}{}
		seenGroups := make(map[int64]struct{}, len(state.Groups))
		for _, group := range state.Groups {
			if group.GroupID <= 0 {
				return fmt.Errorf("插件 %s 包含无效群状态: %w", state.PluginKey, ErrInvalidRuntimeGroupID)
			}
			if _, duplicate := seenGroups[group.GroupID]; duplicate {
				return fmt.Errorf("插件 %s 的群 %d 状态重复", state.PluginKey, group.GroupID)
			}
			seenGroups[group.GroupID] = struct{}{}
		}
		validated[state.PluginKey] = state
	}
	enabled := make([]string, 0)
	for _, spec := range b.catalog.Specs() {
		state, found := validated[spec.Key]
		if !found {
			state = PersistedPluginState{PluginKey: spec.Key, Groups: make([]PersistedGroupState, 0)}
		}
		groups := make(map[int64]bool, len(state.Groups))
		for _, group := range state.Groups {
			groups[group.GroupID] = group.Enabled
		}
		if err := b.controller.replaceGroupStates(state.PluginKey, groups); err != nil {
			return errors.Join(fmt.Errorf("恢复插件 %s 群状态: %w", state.PluginKey, err), b.cleanup(ctx, enabled))
		}
		if !state.DesiredEnabled {
			if err := b.controller.Disable(ctx, state.PluginKey); err != nil {
				return errors.Join(fmt.Errorf("关闭插件 %s 生命周期: %w", state.PluginKey, err), b.cleanup(ctx, append(enabled, state.PluginKey)))
			}
			continue
		}
		if err := b.controller.Enable(ctx, state.PluginKey); err != nil {
			return errors.Join(fmt.Errorf("恢复插件 %s 生命周期: %w", state.PluginKey, err), b.cleanup(ctx, append(enabled, state.PluginKey)))
		}
		enabled = append(enabled, state.PluginKey)
	}
	return nil
}

func isNilRuntimeSnapshotRepository(repository RuntimeSnapshotRepository) bool {
	if repository == nil {
		return true
	}
	value := reflect.ValueOf(repository)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (b *RuntimeBootstrap) cleanup(ctx context.Context, pluginKeys []string) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeBootstrapCleanupTimeout)
	defer cancel()
	var cleanupErrors []error
	for index := len(pluginKeys) - 1; index >= 0; index-- {
		if err := b.controller.Disable(cleanupContext, pluginKeys[index]); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("清理插件 %s: %w", pluginKeys[index], err))
		}
	}
	return errors.Join(cleanupErrors...)
}
