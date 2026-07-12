// 📌 影响范围：调用已注册插件及 StateStore；并发修改内存中的插件启用状态和优先级。
package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type entry struct {
	plugin   Plugin
	enabled  bool
	priority int
}

// Manager 管理插件注册、状态刷新、热开关和事件路由。
type Manager struct {
	mu      sync.RWMutex
	store   StateStore
	entries map[string]*entry
	ordered []*entry
}

// NewManager 创建插件管理器。
// @param store：插件状态持久化仓库，可为 nil。
// @returns 空的并发安全 Manager。
// ⚠️副作用说明：仅分配内存，不访问数据库或调用插件。
func NewManager(store StateStore) *Manager {
	manager := &Manager{store: store, entries: make(map[string]*entry)}

	// >>> 数据演变示例
	// 1. PostgreSQL Store -> 空 entries -> 返回可持久化状态的 Manager。
	// 2. nil Store -> 空 entries -> 返回仅内存运行的 Manager。
	return manager
}

// Register 注册一个默认禁用的插件。
// @param candidate：待注册插件。
// @returns 插件为空、名称为空或名称重复时返回错误。
// ⚠️副作用说明：成功时修改管理器注册表和路由顺序。
func (m *Manager) Register(candidate Plugin) error {
	// [决策理由] nil 插件无法安全调用接口方法，必须在进入注册表前拒绝。
	if candidate == nil {
		return errors.New("插件不能为空")
	}
	name := candidate.Name()
	// [决策理由] 插件名是数据库主键和运行时索引，空名称无法稳定定位状态。
	if name == "" {
		return errors.New("插件名称不能为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// [决策理由] 同名插件会造成状态和路由歧义，因此禁止覆盖已有注册。
	if _, exists := m.entries[name]; exists {
		return fmt.Errorf("插件 %q 已注册", name)
	}
	item := &entry{plugin: candidate}
	m.entries[name] = item
	m.ordered = append(m.ordered, item)
	m.sortLocked()

	// >>> 数据演变示例
	// 1. 空管理器 + sign_in -> entries[sign_in]=disabled -> 注册成功。
	// 2. 已有 sign_in + sign_in -> 重名检查 -> 返回错误且保持原注册表。
	return nil
}

// Load 从仓库刷新已注册插件的启用状态和优先级。
// @param ctx：控制数据库读取和生命周期回调。
// @returns 仓库读取或插件生命周期回调错误。
// ⚠️副作用说明：读取 StateStore，修改内存状态，并可能调用 OnEnable 或 OnDisable。
func (m *Manager) Load(ctx context.Context) error {
	m.mu.RLock()
	hasEntries := len(m.entries) > 0
	m.mu.RUnlock()
	// [决策理由] 没有编译时注册插件时无需访问状态表，允许迁移模块落地前保持基础链路运行。
	if !hasEntries {
		return nil
	}
	// [决策理由] 无状态仓库时没有可加载内容，内存插件保持默认禁用。
	if m.store == nil {
		return nil
	}
	states, err := m.store.Load(ctx)
	// [决策理由] 数据库状态不完整时不能安全更新部分插件，读取失败直接保持旧快照。
	if err != nil {
		return fmt.Errorf("加载插件状态: %w", err)
	}
	for _, state := range states {
		// [决策理由] 数据库可能保留当前二进制未集成的插件，刷新时应忽略而非阻断启动。
		if err := m.apply(ctx, state.Name, state.Enabled, state.Priority, false); err != nil && !errors.Is(err, errNotRegistered) {
			return err
		}
	}

	// >>> 数据演变示例
	// 1. DB{sign_in:true,priority:10} -> 查找注册项 -> OnEnable -> 更新排序。
	// 2. DB{removed_plugin:true} -> 未注册 -> 忽略 -> 其他插件继续加载。
	return nil
}

// SetEnabled 热切换插件状态并持久化。
// @param ctx：控制生命周期回调和数据库写入；name：插件名；enabled：目标状态。
// @returns 插件不存在、生命周期或持久化错误。
// ⚠️副作用说明：调用插件生命周期、写入 StateStore 并修改内存状态。
func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool) error {
	priority, err := m.priority(name)
	// [决策理由] 不存在的插件不能写入孤立状态，先验证注册项。
	if err != nil {
		return err
	}
	// [决策理由] 生命周期回调必须先成功，避免内存显示启用但插件初始化失败。
	if err := m.apply(ctx, name, enabled, priority, true); err != nil {
		return err
	}

	// >>> 数据演变示例
	// 1. sign_in:false -> SetEnabled(true) -> OnEnable -> DB=true -> 内存=true。
	// 2. OnEnable 返回错误 -> 保持 false -> 不写数据库 -> 返回错误。
	return nil
}

// Handle 按优先级将事件传递给所有已启用插件。
// @param ctx：事件处理上下文；event：OneBot 上报事件。
// @returns 普通错误；ErrStopPropagation 会被视为成功终止。
// ⚠️副作用说明：按顺序调用启用插件的 Handle 方法。
func (m *Manager) Handle(ctx context.Context, event ws.Event) error {
	m.mu.RLock()
	active := make([]Plugin, 0, len(m.ordered))
	for _, item := range m.ordered {
		// [决策理由] 路由快照只包含启用项，使锁可在执行第三方插件代码前释放。
		if item.enabled {
			active = append(active, item.plugin)
		}
	}
	m.mu.RUnlock()

	for _, current := range active {
		err := current.Handle(ctx, event)
		// [决策理由] 插件显式中止传播代表事件已完整消费，不应作为错误上报。
		if errors.Is(err, ErrStopPropagation) {
			return nil
		}
		// [决策理由] 普通插件错误应携带插件名返回，避免后续插件在未知状态下继续处理。
		if err != nil {
			return fmt.Errorf("插件 %s 处理事件: %w", current.Name(), err)
		}
	}

	// >>> 数据演变示例
	// 1. A(priority=10),B(priority=5) 均启用 -> A.Handle -> B.Handle -> nil。
	// 2. A 返回 ErrStopPropagation -> 停止循环 -> B 不执行 -> nil。
	return nil
}

// HandleNamed 将事件定向发送给一个已启用插件。
// @param ctx：处理上下文；name：插件名；event：OneBot 事件。
// @returns 未注册、未启用或插件处理错误。
// ⚠️副作用说明：调用目标插件 Handle。
func (m *Manager) HandleNamed(ctx context.Context, name string, event ws.Event) error {
	m.mu.RLock()
	item, exists := m.entries[name]
	// [决策理由] 锁内复制接口和状态，避免执行插件代码时长期持锁。
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	current := item.plugin
	enabled := item.enabled
	m.mu.RUnlock()
	// [决策理由] 命令不能绕过数据库驱动的插件关闭状态。
	if !enabled {
		return fmt.Errorf("插件 %s 未启用", name)
	}
	err := current.Handle(ctx, event)
	// [决策理由] 定向处理中的停止传播表示成功消费。
	if errors.Is(err, ErrStopPropagation) {
		return nil
	}
	// [决策理由] 错误需附加插件名便于定位。
	if err != nil {
		return fmt.Errorf("插件 %s 处理事件: %w", name, err)
	}

	// >>> 数据演变示例
	// 1. enabled ping + event -> ping.Handle -> nil。
	// 2. disabled ping -> 不调用 Handle -> 返回未启用错误。
	return nil
}

var errNotRegistered = errors.New("插件未注册")

// apply 执行状态迁移并更新优先级。
// @param ctx：生命周期上下文；name：插件名；enabled：目标状态；priority：目标优先级；persist：是否持久化。
// @returns 注册、生命周期或持久化错误。
// ⚠️副作用说明：可能调用插件回调、写入 StateStore 并修改内存路由。
func (m *Manager) apply(ctx context.Context, name string, enabled bool, priority int, persist bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, exists := m.entries[name]
	// [决策理由] 状态只能应用到编译进当前二进制的插件。
	if !exists {
		return fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	// [决策理由] 仅在状态实际变化时调用生命周期，避免重复加载产生重复资源。
	if item.enabled != enabled {
		var err error
		// [决策理由] 启用和禁用需要调用不同生命周期以正确申请或释放资源。
		if enabled {
			err = item.plugin.OnEnable(ctx)
		} else {
			err = item.plugin.OnDisable(ctx)
		}
		// [决策理由] 回调失败意味着迁移未完成，内存和数据库都保持旧状态。
		if err != nil {
			return fmt.Errorf("切换插件 %s: %w", name, err)
		}
	}
	// [决策理由] 热切换要求数据库与内存一致，持久化失败时不提交新内存状态。
	if persist && m.store != nil {
		if err := m.store.SaveEnabled(ctx, name, enabled); err != nil {
			return fmt.Errorf("保存插件 %s 状态: %w", name, err)
		}
	}
	item.enabled = enabled
	item.priority = priority
	m.sortLocked()

	// >>> 数据演变示例
	// 1. disabled + enabled=true -> OnEnable -> SaveEnabled -> 内存 enabled。
	// 2. enabled + priority=20 -> 无生命周期调用 -> 更新 priority -> 重新排序。
	return nil
}

// priority 读取插件当前优先级。
// @param name：插件名。
// @returns 当前优先级或插件未注册错误。
// ⚠️副作用说明：无；仅读取内存状态。
func (m *Manager) priority(name string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, exists := m.entries[name]
	// [决策理由] 未注册插件没有可用优先级，必须返回明确错误。
	if !exists {
		return 0, fmt.Errorf("%w: %s", errNotRegistered, name)
	}

	// >>> 数据演变示例
	// 1. sign_in(priority=10) -> 查找 -> 返回 10,nil。
	// 2. missing -> 查找失败 -> 返回 0,插件未注册错误。
	return item.priority, nil
}

// sortLocked 按优先级降序、名称升序稳定排序；调用方必须持有写锁。
// @param 无。
// @returns 无。
// ⚠️副作用说明：原地修改 ordered 路由顺序。
func (m *Manager) sortLocked() {
	sort.SliceStable(m.ordered, func(i int, j int) bool {
		// [决策理由] 相同优先级按名称排序，确保不同进程和注册顺序产生一致路由。
		if m.ordered[i].priority == m.ordered[j].priority {
			return m.ordered[i].plugin.Name() < m.ordered[j].plugin.Name()
		}

		// >>> 数据演变示例
		// 1. A=10,B=5 -> 10>5 -> A 排在 B 前。
		// 2. B=10,A=10 -> 名称 A<B -> A 排在 B 前。
		return m.ordered[i].priority > m.ordered[j].priority
	})

	// >>> 数据演变示例
	// 1. [A:0,B:10] -> priority 降序 -> [B,A]。
	// 2. [B:5,A:5] -> name 升序 -> [A,B]。
}
