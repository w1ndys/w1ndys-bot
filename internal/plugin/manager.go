// 📌 影响范围：调用已注册插件及 StateStore；并发修改内存中的插件启用状态和优先级。
package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type entry struct {
	name          string
	plugin        Plugin
	enabled       bool
	transitioning bool
	inFlight      int
	idle          chan struct{}
	priority      int
}

type handlingPluginContextKey struct{}

const lifecycleRollbackTimeout = 10 * time.Second

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
	item := &entry{name: name, plugin: candidate}
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
	currentPlugin, handling := ctx.Value(handlingPluginContextKey{}).(string)
	// [决策理由] 插件在自己的 Handle 中同步禁用自身会等待当前调用结束而死锁，必须要求改为处理完成后的外部管理操作。
	if handling && currentPlugin == name {
		return fmt.Errorf("插件 %s 不能在自身 Handle 中同步切换状态", name)
	}
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
	ordered := append([]*entry(nil), m.ordered...)
	m.mu.RUnlock()
	for _, item := range ordered {
		_, ready := m.beginHandling(item)
		// [决策理由] 状态快照之后插件可能被禁用或进入迁移，此时本轮广播应跳过该插件。
		if !ready {
			continue
		}
		err := m.invokePlugin(ctx, item, event)
		// [决策理由] 插件显式中止传播代表事件已完整消费，不应作为错误上报。
		if errors.Is(err, ErrStopPropagation) {
			return nil
		}
		// [决策理由] 普通插件错误应携带插件名返回，避免后续插件在未知状态下继续处理。
		if err != nil {
			return fmt.Errorf("插件 %s 处理事件: %w", item.name, err)
		}
	}

	// >>> 数据演变示例
	// 1. A(priority=10),B(priority=5) 均启用 -> A.Handle -> B.Handle -> nil。
	// 2. A迁移中或返回ErrStopPropagation -> 跳过A或停止循环 -> 不使用释放中的资源。
	return nil
}

// beginHandling 在路由锁内确认插件可用并登记一个在途调用。
// @param item：候选插件路由项。
// @returns 可安全调用的插件及是否允许本次调用。
// ⚠️副作用说明：允许调用时增加插件在途计数，调用方必须在 Handle 返回后执行 Done。
func (m *Manager) beginHandling(item *entry) (Plugin, bool) {
	m.mu.Lock()
	ready := item.enabled && !item.transitioning
	// [决策理由] 禁用迁移会先设置 transitioning，锁内登记可保证 Wait 开始后不再出现新的在途调用。
	if ready {
		// [决策理由] 首个在途调用创建新的完成信号，禁用方可等待该批调用全部退出。
		if item.inFlight == 0 {
			item.idle = make(chan struct{})
		}
		item.inFlight++
	}
	current := item.plugin
	m.mu.Unlock()

	// >>> 数据演变示例
	// 1. enabled且稳定 -> inFlight 0→1 -> 返回插件,true。
	// 2. disabled或transitioning -> 不增加计数 -> 返回插件,false。
	return current, ready
}

// invokePlugin 调用已登记在途状态的插件，并保证调用退出时释放计数。
// @param ctx：事件上下文；item：已通过 beginHandling 的插件项；event：OneBot 事件。
// @returns 插件 Handle 返回的错误。
// ⚠️副作用说明：调用插件 Handle，并在正常返回或 panic 展开时减少在途计数。
func (m *Manager) invokePlugin(ctx context.Context, item *entry, event ws.Event) error {
	defer m.finishHandling(item)
	handlingContext := context.WithValue(ctx, handlingPluginContextKey{}, item.name)
	err := item.plugin.Handle(handlingContext, event)

	// >>> 数据演变示例
	// 1. Handle返回nil -> defer将inFlight 1→0 -> 返回nil。
	// 2. Handle发生panic -> defer仍将inFlight 1→0 -> panic继续向上展开。
	return err
}

// finishHandling 结束一次插件调用，并在最后一个调用退出时通知禁用等待方。
// @param item：已登记在途调用的插件路由项。
// @returns 无。
// ⚠️副作用说明：减少在途计数，归零时关闭完成信号通道。
func (m *Manager) finishHandling(item *entry) {
	m.mu.Lock()
	item.inFlight--
	// [决策理由] 只有最后一个处理者退出时资源才可安全释放，并且完成信号只能关闭一次。
	if item.inFlight == 0 {
		close(item.idle)
		item.idle = nil
	}
	m.mu.Unlock()

	// >>> 数据演变示例
	// 1. inFlight=2 -> 结束一次 -> 1,不通知禁用方。
	// 2. inFlight=1 -> 结束一次 -> 0,关闭idle通知禁用方。
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
	m.mu.RUnlock()
	_, ready := m.beginHandling(item)
	// [决策理由] 命令不能绕过插件关闭、迁移或故障隔离状态。
	if !ready {
		return fmt.Errorf("插件 %s 未启用或正在切换状态", name)
	}
	err := m.invokePlugin(ctx, item, event)
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
var errLifecyclePanic = errors.New("插件生命周期发生 panic")

// apply 执行状态迁移并更新优先级。
// @param ctx：生命周期上下文；name：插件名；enabled：目标状态；priority：目标优先级；persist：是否持久化。
// @returns 注册、生命周期或持久化错误。
// ⚠️副作用说明：可能调用插件回调、写入 StateStore 并修改内存路由。
func (m *Manager) apply(ctx context.Context, name string, enabled bool, priority int, persist bool) error {
	m.mu.Lock()
	item, exists := m.entries[name]
	// [决策理由] 状态只能应用到编译进当前二进制的插件。
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	// [决策理由] 同一插件的并发或生命周期重入切换无法安全合并，应快速拒绝而不是等待自身回调造成死锁。
	if item.transitioning {
		m.mu.Unlock()
		return fmt.Errorf("插件 %s 正在切换状态", name)
	}
	previousEnabled := item.enabled
	stateChanged := previousEnabled != enabled
	// [决策理由] 仅优先级刷新也必须与同插件的其他快照更新排他，避免并发 Load 由旧快照覆盖新排序。
	item.transitioning = true
	current := item.plugin
	waitForIdle := item.idle
	m.mu.Unlock()
	// [决策理由] 禁用前等待已登记的处理调用结束，避免生命周期释放仍被 Handle 使用的资源。
	if stateChanged && !enabled && waitForIdle != nil {
		select {
		case <-waitForIdle:
		case <-ctx.Done():
			m.finishTransition(name, previousEnabled, priority, false, false)
			return fmt.Errorf("等待插件 %s 在途调用结束: %w", name, ctx.Err())
		}
	}

	// [决策理由] 仅在状态实际变化时调用生命周期，避免重复加载产生重复资源。
	if stateChanged {
		err := callLifecycle(ctx, current, enabled)
		// [决策理由] 生命周期返回错误也可能已产生部分资源副作用，必须恢复旧标志并保持隔离，不能继续路由未知状态资源。
		if err != nil {
			m.finishTransition(name, previousEnabled, priority, false, true)
			return fmt.Errorf("切换插件 %s: %w", name, err)
		}
	}
	// [决策理由] 热切换要求数据库与内存一致，持久化失败时不提交新内存状态。
	if persist && m.store != nil {
		if err := m.store.SaveEnabled(ctx, name, enabled); err != nil {
			// [决策理由] 生命周期已成功但数据库拒绝保存时必须反向补偿，避免运行资源与持久化状态分裂。
			if stateChanged {
				rollbackContext, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), lifecycleRollbackTimeout)
				rollbackErr := callLifecycle(rollbackContext, current, previousEnabled)
				cancelRollback()
				// [决策理由] 补偿失败表示资源状态未知，必须同时保留保存错误和补偿错误供运维处理。
				if rollbackErr != nil {
					m.finishTransition(name, previousEnabled, priority, false, true)
					return errors.Join(fmt.Errorf("保存插件 %s 状态: %w", name, err), fmt.Errorf("回滚插件 %s 生命周期: %w", name, rollbackErr))
				}
				m.finishTransition(name, previousEnabled, priority, false, false)
			} else {
				m.finishTransition(name, previousEnabled, priority, false, false)
			}
			return fmt.Errorf("保存插件 %s 状态: %w", name, err)
		}
	}
	m.finishTransition(name, enabled, priority, true, false)

	// >>> 数据演变示例
	// 1. disabled + enabled=true -> OnEnable -> SaveEnabled -> 内存 enabled。
	// 2. enabled + priority=20 -> 无生命周期调用 -> 更新 priority -> 重新排序。
	return nil
}

// callLifecycle 执行目标启用状态对应的插件生命周期回调。
// @param ctx：生命周期上下文；candidate：插件实例；enabled：目标启用状态。
// @returns 生命周期回调错误。
// ⚠️副作用说明：调用插件 OnEnable 或 OnDisable，可能申请或释放外部资源。
func callLifecycle(ctx context.Context, candidate Plugin, enabled bool) (err error) {
	defer func() {
		recovered := recover()
		// [决策理由] 第三方生命周期 panic 不应让迁移标记永久卡住，必须转为可隔离处理的错误。
		if recovered != nil {
			err = errLifecyclePanic
		}

		// >>> 数据演变示例
		// 1. OnEnable正常返回 -> recovered=nil -> 保留原错误。
		// 2. OnDisable panic -> recovered非nil -> 返回errLifecyclePanic。
	}()
	// [决策理由] 目标状态决定资源应被创建还是释放，必须调用对应的生命周期方法。
	if enabled {
		return candidate.OnEnable(ctx)
	}

	// >>> 数据演变示例
	// 1. disabled→enabled -> OnEnable -> 返回初始化结果。
	// 2. enabled→disabled -> OnDisable -> 返回清理结果。
	return candidate.OnDisable(ctx)
}

// finishTransition 提交或撤销一次内存状态迁移。
// @param name：插件名；enabled：最终启用状态；priority：目标优先级；commitPriority：是否提交优先级；quarantine：是否保持故障隔离。
// @returns 无。
// ⚠️副作用说明：修改插件内存状态、清除迁移标记并可能重新排序路由。
func (m *Manager) finishTransition(name string, enabled bool, priority int, commitPriority bool, quarantine bool) {
	m.mu.Lock()
	item, exists := m.entries[name]
	// [决策理由] 注册项在当前实现中不会删除，但仍需防御未来支持卸载后出现空指针。
	if exists {
		item.enabled = enabled
		item.transitioning = quarantine
		// [决策理由] 失败回滚只能清除迁移状态，不能意外提交尚未持久化的优先级。
		if commitPriority {
			item.priority = priority
			m.sortLocked()
		}
	}
	m.mu.Unlock()

	// >>> 数据演变示例
	// 1. disabled迁移成功 -> enabled=true,transitioning=false,priority提交。
	// 2. 保存且补偿失败 -> enabled恢复旧值,transitioning=true -> 保持故障隔离。
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
			return m.ordered[i].name < m.ordered[j].name
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
