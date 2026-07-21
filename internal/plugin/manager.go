// 📌 影响范围：调用已注册插件及 StateStore；并发修改插件启用状态、优先级和运行配置。
package plugin

import (
	"context"
	"encoding/json"
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
	mu        sync.RWMutex
	store     StateStore
	entries   map[string]*entry
	ordered   []*entry
	groupGate GroupGate
}

// NewManager 创建插件管理器。
// @param store：插件状态持久化仓库，可为nil；gates：可选群门禁，生产环境最多注入一个。
// @returns 空的并发安全 Manager。
// ⚠️副作用说明：仅分配内存，不访问数据库或调用插件。
func NewManager(store StateStore, gates ...GroupGate) *Manager {
	manager := &Manager{store: store, entries: make(map[string]*entry)}
	// [决策理由] 可选参数保持纯内存测试和旧组装兼容，同时生产只注入一个共享门禁。
	if len(gates) > 0 {
		manager.groupGate = gates[0]
	}

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
	// [决策理由] 群门禁必须在插件开始接收事件前发布完整快照，失败时阻止启动。
	if m.groupGate != nil {
		if err := m.ReloadGroupGate(ctx); err != nil {
			return fmt.Errorf("加载插件群门禁: %w", err)
		}
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
		_, configurableErr := m.configurable(state.Name)
		// [决策理由] 未注册状态属于旧二进制残留，应与启停加载一致地忽略。
		if errors.Is(configurableErr, errNotRegistered) {
			continue
		}
		// [决策理由] 声明配置能力的插件必须通过统一互斥入口恢复快照，避免与事件处理或其他热应用并发。
		if configurableErr == nil && state.Enabled {
			// [决策理由] 启用插件必须在生命周期回调前恢复完整有效配置。
			if applyErr := m.ApplyConfig(ctx, state.Name, state.ConfigJSON); applyErr != nil {
				return fmt.Errorf("恢复插件 %s 配置: %w", state.Name, applyErr)
			}
		}
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

// ErrConfigNotSupported 表示插件未声明配置能力。
var ErrConfigNotSupported = errors.New("插件不支持配置")

// ErrConfigCallbackPanic 表示插件配置回调发生 panic。
var ErrConfigCallbackPanic = errors.New("插件配置回调发生 panic")

// ErrConfigValidationFailed 表示插件领域校验拒绝配置，且不携带可能含 secret 的原始错误。
var ErrConfigValidationFailed = errors.New("插件配置校验失败")

// ErrConfigApplyFailed 表示插件热应用失败，且不携带可能含 secret 的原始错误。
var ErrConfigApplyFailed = errors.New("插件配置应用失败")

// ErrAdminResourceNotSupported 表示插件未声明通用管理资源。
var ErrAdminResourceNotSupported = errors.New("插件不支持管理资源")

// ErrAdminResourceNotFound 表示资源键未由目标插件注册。
var ErrAdminResourceNotFound = errors.New("插件管理资源不存在")

// AdminResources 返回指定插件的已校验资源声明。
// @param name：插件稳定名称。
// @returns 资源声明副本，或未注册、不支持及声明无效错误。
// ⚠️副作用说明：调用插件 AdminResources；不持有 Manager 主锁。
func (m *Manager) AdminResources(name string) ([]AdminResource, error) {
	registrations, err := m.adminResourceRegistrations(name)
	// [决策理由] 未注册与未实现能力必须保留稳定错误语义。
	if err != nil {
		return nil, err
	}
	result := make([]AdminResource, 0, len(registrations))
	seen := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		// [决策理由] nil 处理器无法安全委派业务操作。
		if registration.Handler == nil {
			return nil, fmt.Errorf("插件 %s 资源 %s 处理器为空", name, registration.Descriptor.Key)
		}
		// [决策理由] 无效声明不能暴露给 WebUI 或用于路由。
		if err := registration.Descriptor.Validate(); err != nil {
			return nil, fmt.Errorf("插件 %s 管理资源无效: %w", name, err)
		}
		// [决策理由] 重复键会使 URL 委派产生歧义，必须整组拒绝。
		if _, exists := seen[registration.Descriptor.Key]; exists {
			return nil, fmt.Errorf("插件 %s 管理资源 %s 重复", name, registration.Descriptor.Key)
		}
		seen[registration.Descriptor.Key] = struct{}{}
		result = append(result, registration.Descriptor)
	}

	// >>> 数据演变示例
	// 1. keyword_reply声明rules -> 校验并复制 -> [rules]。
	// 2. echo未实现Provider -> 能力断言 -> ErrAdminResourceNotSupported。
	return result, nil
}

// AdminResourceHandler 返回固定插件与资源键对应的处理器。
// @param name：插件名；resourceKey：资源键。
// @returns 已校验声明、处理器，或稳定查找错误。
// ⚠️副作用说明：调用插件 AdminResources；不持有 Manager 主锁。
func (m *Manager) AdminResourceHandler(name, resourceKey string) (AdminResource, AdminResourceHandler, error) {
	registrations, err := m.adminResourceRegistrations(name)
	// [决策理由] 插件定位错误必须先于资源键错误返回。
	if err != nil {
		return AdminResource{}, nil, err
	}
	seen := make(map[string]struct{}, len(registrations))
	var matched *AdminResourceRegistration
	for _, registration := range registrations {
		// [决策理由] CRUD 查找必须与列表使用相同的整组去重规则，不得静默选择首个处理器。
		if _, exists := seen[registration.Descriptor.Key]; exists {
			return AdminResource{}, nil, fmt.Errorf("插件 %s 管理资源 %s 重复", name, registration.Descriptor.Key)
		}
		seen[registration.Descriptor.Key] = struct{}{}
		// [决策理由] 整组声明必须先完整校验，避免目标项后的重复键或损坏资源被直接路由绕过。
		if registration.Handler == nil {
			return AdminResource{}, nil, fmt.Errorf("插件 %s 资源 %s 处理器为空", name, registration.Descriptor.Key)
		}
		// [决策理由] CRUD 与资源列表必须对所有声明采用相同有效性边界。
		if err := registration.Descriptor.Validate(); err != nil {
			return AdminResource{}, nil, fmt.Errorf("插件 %s 管理资源无效: %w", name, err)
		}
		// [决策理由] 只能委派插件明确注册的稳定键，不从客户端推导表名。
		if registration.Descriptor.Key == resourceKey {
			copy := registration
			matched = &copy
		}
	}
	// [决策理由] 只有整组检查通过后才能返回目标处理器。
	if matched != nil {
		return matched.Descriptor, matched.Handler, nil
	}

	// >>> 数据演变示例
	// 1. keyword_reply/rules -> 命中注册 -> descriptor+handler。
	// 2. keyword_reply/tables -> 无注册键 -> ErrAdminResourceNotFound。
	return AdminResource{}, nil, fmt.Errorf("%w: %s/%s", ErrAdminResourceNotFound, name, resourceKey)
}

// adminResourceRegistrations 读取插件当前资源注册副本。
// @param name：插件稳定名称。
// @returns 注册副本或稳定能力错误。
// ⚠️副作用说明：调用插件 AdminResources，插件必须并发安全。
func (m *Manager) adminResourceRegistrations(name string) ([]AdminResourceRegistration, error) {
	m.mu.RLock()
	item, exists := m.entries[name]
	// [决策理由] 在释放主锁前只复制稳定插件接口，避免持锁调用插件代码。
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	provider, supported := item.plugin.(AdminResourceProvider)
	m.mu.RUnlock()
	// [决策理由] 未实现可选能力的插件不应暴露空的可写资源集。
	if !supported {
		return nil, fmt.Errorf("%w: %s", ErrAdminResourceNotSupported, name)
	}
	registrations, callbackErr := callAdminResources(provider)
	// [决策理由] 插件声明回调 panic 必须隔离为错误，不得击穿 HTTP 管理请求。
	if callbackErr != nil {
		return nil, fmt.Errorf("读取插件 %s 管理资源: %w", name, callbackErr)
	}
	result := make([]AdminResourceRegistration, len(registrations))
	for index, registration := range registrations {
		registration.Descriptor.Fields = cloneResourceFields(registration.Descriptor.Fields)
		result[index] = registration
	}

	// >>> 数据演变示例
	// 1. Provider返回[rules] -> slice复制 -> 返回可遍历注册。
	// 2. 未注册插件 -> entries查找失败 -> ErrNotRegistered。
	return result, nil
}

// callAdminResources 安全调用插件资源声明回调。
// @param provider：插件资源提供者。
// @returns 资源注册或 panic 转换错误。
// ⚠️副作用说明：调用插件代码并捕获 panic。
func callAdminResources(provider AdminResourceProvider) (registrations []AdminResourceRegistration, err error) {
	defer func() {
		// [决策理由] 任意插件 panic 都不应终止管理服务进程。
		if recover() != nil {
			err = errors.New("插件管理资源回调失败")
		}

		// >>> 数据演变示例
		// 1. 正常返回[rules] -> 保留注册。
		// 2. panic("x") -> recover -> 稳定错误。
	}()
	registrations = provider.AdminResources()

	// >>> 数据演变示例
	// 1. Provider[rules] -> [rules],nil。
	// 2. Provider panic -> defer改写error。
	return registrations, nil
}

// cloneResourceFields 深拷贝资源字段及嵌套可变切片。
// @param fields：插件持有的字段声明。
// @returns 与插件后续修改隔离的字段副本。
// ⚠️副作用说明：分配新切片并复制 RawMessage 与 Options。
func cloneResourceFields(fields []ResourceField) []ResourceField {
	result := make([]ResourceField, len(fields))
	for index, field := range fields {
		field.Default = append([]byte(nil), field.Default...)
		field.Options = append([]string(nil), field.Options...)
		result[index] = field
	}

	// >>> 数据演变示例
	// 1. enum options[a,b] -> 新Options切片 -> 插件修改不影响响应。
	// 2. nil fields -> 空副本。
	return result
}

// ConfigSchema 返回插件声明的配置 Schema。
// @param name：插件稳定名称。
// @returns 配置 Schema，或未注册、不支持配置错误。
// ⚠️副作用说明：调用插件 ConfigSchema；不持有 Manager 主锁。
func (m *Manager) ConfigSchema(name string) (ConfigSchema, error) {
	configurable, err := m.configurable(name)
	// [决策理由] 未注册或未实现配置契约时必须保留可判定的根因。
	if err != nil {
		return ConfigSchema{}, err
	}
	schema, err := callConfigSchema(configurable)
	// [决策理由] 插件 Schema 回调 panic 必须转成错误，不能终止管理进程。
	if err != nil {
		return ConfigSchema{}, fmt.Errorf("读取插件 %s 配置 Schema: %w", name, err)
	}
	// [决策理由] 无效 Schema 不能交给管理端渲染或用于接受配置。
	if err := schema.Validate(); err != nil {
		return ConfigSchema{}, fmt.Errorf("插件 %s 配置 Schema 无效: %w", name, err)
	}

	// >>> 数据演变示例
	// 1. echo实现Configurable -> 校验Schema -> 返回response_prefix字段。
	// 2. 普通插件 -> 接口断言失败 -> 返回ErrConfigNotSupported。
	return schema, nil
}

// ValidateConfig 规范化并执行插件领域配置校验。
// @param ctx：校验上下文；name：插件名；raw：完整配置 JSON。
// @returns Schema、JSON 或插件领域校验错误；成功时返回 nil。
// ⚠️副作用说明：调用插件 ConfigSchema 和 ValidateConfig；不持有 Manager 主锁。
func (m *Manager) ValidateConfig(ctx context.Context, name string, raw json.RawMessage) error {
	configurable, err := m.configurable(name)
	// [决策理由] 只有声明配置能力的已注册插件才能校验配置。
	if err != nil {
		return err
	}
	schema, err := callConfigSchema(configurable)
	// [决策理由] Schema 回调失败时无法安全解释配置对象。
	if err != nil {
		return fmt.Errorf("读取插件 %s 配置 Schema: %w", name, err)
	}
	normalized, err := NormalizeConfig(schema, raw)
	// [决策理由] Schema 基础校验失败时不能调用依赖完整类型的插件校验代码。
	if err != nil {
		return fmt.Errorf("规范化插件 %s 配置: %w", name, err)
	}
	// [决策理由] 插件领域约束错误需要携带插件名供管理 API 定位。
	if err := callValidateConfig(ctx, configurable, normalized); err != nil {
		return fmt.Errorf("校验插件 %s 配置: %w", name, err)
	}

	// >>> 数据演变示例
	// 1. echo+{} -> 补齐response_prefix默认值 -> 领域校验通过。
	// 2. echo+未知字段 -> NormalizeConfig拒绝 -> 不调用领域校验。
	return nil
}

// ApplyConfig 原子切换插件运行配置并与生命周期和事件处理互斥。
// @param ctx：控制等待与热应用；name：插件名；raw：已持久化的完整配置 JSON。
// @returns 未注册、不支持、配置无效、等待取消或插件应用错误。
// ⚠️副作用说明：暂停目标插件事件路由，等待在途调用结束并调用插件 ApplyConfig。
func (m *Manager) ApplyConfig(ctx context.Context, name string, raw json.RawMessage) error {
	currentPlugin, handling := ctx.Value(handlingPluginContextKey{}).(string)
	// [决策理由] 插件在自身 Handle 中同步热应用会等待当前调用结束而死锁，必须快速拒绝。
	if handling && currentPlugin == name {
		return fmt.Errorf("插件 %s 不能在自身 Handle 中同步应用配置", name)
	}
	m.mu.Lock()
	item, exists := m.entries[name]
	// [决策理由] 配置只能应用到编译进当前二进制的插件实例。
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	configurable, supported := item.plugin.(Configurable)
	// [决策理由] 未声明配置能力时不能假装热应用成功。
	if !supported {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrConfigNotSupported, name)
	}
	// [决策理由] 配置切换必须与生命周期及其他配置切换串行，快速失败可避免回调重入死锁。
	if item.transitioning {
		m.mu.Unlock()
		return fmt.Errorf("插件 %s 正在切换状态", name)
	}
	item.transitioning = true
	waitForIdle := item.idle
	m.mu.Unlock()
	defer m.finishConfigTransition(name)
	// [决策理由] 不可变快照发布前等待旧快照处理完成，保证热应用与事件处理边界清晰。
	if waitForIdle != nil {
		select {
		case <-waitForIdle:
		case <-ctx.Done():
			return fmt.Errorf("等待插件 %s 在途调用结束: %w", name, ctx.Err())
		}
	}
	schema, err := callConfigSchema(configurable)
	// [决策理由] Schema panic 或错误不能让插件永久停留在迁移状态。
	if err != nil {
		return fmt.Errorf("读取插件 %s 配置 Schema: %w", name, err)
	}
	normalized, err := NormalizeConfig(schema, raw)
	// [决策理由] 管理层可能直接调用 ApplyConfig，运行时仍必须防御无效快照。
	if err != nil {
		return fmt.Errorf("规范化插件 %s 配置: %w", name, err)
	}
	// [决策理由] Apply 前重复领域校验确保运行时入口自身完整，不依赖调用方顺序。
	if err := callValidateConfig(ctx, configurable, normalized); err != nil {
		return fmt.Errorf("校验插件 %s 配置: %w", name, err)
	}
	err = callApplyConfig(ctx, configurable, normalized)
	// [决策理由] 插件承诺失败时不部分发布，错误应保留根因交由服务层补偿持久化。
	if err != nil {
		return fmt.Errorf("应用插件 %s 配置: %w", name, err)
	}

	// >>> 数据演变示例
	// 1. enabled echo+prefix=[bot] -> 暂停路由 -> 原子发布 -> 恢复路由。
	// 2. ctx取消等待 -> 清除transitioning -> 保留旧配置并返回取消错误。
	return nil
}

// QuarantineConfig 将配置状态未知的插件隔离出事件路由。
// @param name：插件稳定名称。
// @returns 插件未注册错误。
// ⚠️副作用说明：将目标插件保持为 transitioning；当前进程需重启并从持久化快照重新初始化后才能解除隔离。
func (m *Manager) QuarantineConfig(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, exists := m.entries[name]
	// [决策理由] 只能隔离当前二进制中已注册的插件。
	if !exists {
		return fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	item.transitioning = true

	// >>> 数据演变示例
	// 1. echo运行态未知 -> transitioning=true -> 后续Handle跳过。
	// 2. missing -> ErrNotRegistered -> 注册表不变。
	return nil
}

// callConfigSchema 安全调用插件 Schema 回调。
// @param configurable：目标配置插件。
// @returns Schema 或 panic 转换错误。
// ⚠️副作用说明：调用插件代码并捕获 panic。
func callConfigSchema(configurable Configurable) (schema ConfigSchema, err error) {
	defer func() {
		recovered := recover()
		// [决策理由] 插件 panic 不应终止机器人进程，需转换为稳定可判定错误。
		if recovered != nil {
			err = ErrConfigCallbackPanic
		}

		// >>> 数据演变示例
		// 1. ConfigSchema正常返回 -> 保留Schema,nil。
		// 2. ConfigSchema panic("x") -> 零值Schema,ErrConfigCallbackPanic。
	}()
	schema = configurable.ConfigSchema()

	// >>> 数据演变示例
	// 1. echo -> response_prefix Schema,nil。
	// 2. panic插件 -> defer恢复 -> 返回稳定错误。
	return schema, nil
}

// callValidateConfig 安全调用插件领域校验回调。
// @param ctx：校验上下文；configurable：目标插件；raw：规范化配置。
// @returns 插件校验错误或 panic 转换错误。
// ⚠️副作用说明：调用插件代码并捕获 panic。
func callValidateConfig(ctx context.Context, configurable Configurable, raw json.RawMessage) (err error) {
	defer func() {
		recovered := recover()
		// [决策理由] 校验 panic 属于插件故障，必须隔离为错误而非崩溃进程。
		if recovered != nil {
			err = ErrConfigCallbackPanic
		}

		// >>> 数据演变示例
		// 1. ValidateConfig返回invalid -> 保留invalid。
		// 2. ValidateConfig panic -> 返回ErrConfigCallbackPanic。
	}()

	// >>> 数据演变示例
	// 1. 合法配置 -> ValidateConfig -> nil。
	// 2. 领域配置错误 -> ValidateConfig -> 原错误。
	err = configurable.ValidateConfig(ctx, raw)
	// [决策理由] 上下文错误不含配置值，应保留取消语义供调用方判断。
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	// [决策理由] 仅返回标准超时 sentinel，避免插件包装文本携带 secret。
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	// [决策理由] 插件原始错误可能拼接 secret，只允许安全 sentinel 跨越运行时边界。
	if err != nil {
		return ErrConfigValidationFailed
	}
	return nil
}

// callApplyConfig 安全调用插件原子配置发布回调。
// @param ctx：应用上下文；configurable：目标插件；raw：规范化配置。
// @returns 插件应用错误或 panic 转换错误。
// ⚠️副作用说明：调用插件代码并捕获 panic；插件仍须保证失败不改变旧快照。
func callApplyConfig(ctx context.Context, configurable Configurable, raw json.RawMessage) (err error) {
	defer func() {
		recovered := recover()
		// [决策理由] 应用 panic 必须转成错误，让 Manager 的 defer 恢复路由状态。
		if recovered != nil {
			err = ErrConfigCallbackPanic
		}

		// >>> 数据演变示例
		// 1. ApplyConfig返回失败 -> 保留失败错误。
		// 2. ApplyConfig panic -> 返回ErrConfigCallbackPanic。
	}()

	// >>> 数据演变示例
	// 1. 新快照发布成功 -> nil。
	// 2. 插件拒绝配置 -> 返回插件错误。
	err = configurable.ApplyConfig(ctx, raw)
	// [决策理由] 上下文错误可安全保留，并维持取消与超时语义。
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	// [决策理由] 规范化超时错误可保留 errors.Is 语义且删除插件不可信文本。
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	// [决策理由] 插件原始应用错误可能包含 secret，跨边界只返回安全 sentinel。
	if err != nil {
		return ErrConfigApplyFailed
	}
	return nil
}

// configurable 获取插件配置能力且不在调用插件代码期间持锁。
// @param name：插件稳定名称。
// @returns Configurable实例，或未注册、不支持错误。
// ⚠️副作用说明：无；仅读取注册表。
func (m *Manager) configurable(name string) (Configurable, error) {
	m.mu.RLock()
	item, exists := m.entries[name]
	m.mu.RUnlock()
	// [决策理由] 未注册名称必须与不支持配置区分，便于 API 映射正确状态码。
	if !exists {
		return nil, fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	configurable, supported := item.plugin.(Configurable)
	// [决策理由] 配置能力是可选接口，普通插件应返回稳定可判定错误。
	if !supported {
		return nil, fmt.Errorf("%w: %s", ErrConfigNotSupported, name)
	}

	// >>> 数据演变示例
	// 1. echo -> 类型断言成功 -> 返回Configurable。
	// 2. legacy -> 类型断言失败 -> 返回ErrConfigNotSupported。
	return configurable, nil
}

// finishConfigTransition 结束配置热应用并恢复事件路由资格。
// @param name：插件稳定名称。
// @returns 无。
// ⚠️副作用说明：清除目标插件的迁移标记。
func (m *Manager) finishConfigTransition(name string) {
	m.mu.Lock()
	item, exists := m.entries[name]
	// [决策理由] 注册项当前不会删除，但防御未来卸载能力避免空指针。
	if exists {
		item.transitioning = false
	}
	m.mu.Unlock()

	// >>> 数据演变示例
	// 1. echo transitioning=true -> false -> 后续事件可进入。
	// 2. 未来已卸载echo -> exists=false -> 安全忽略。
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
		allowed, err := m.RouteAllowed(item.name, event)
		// [决策理由] 广播快照中的插件可能在预检前被禁用或进入迁移，此时安静跳过且不查询群门禁。
		if err != nil || !allowed {
			continue
		}
		_, ready := m.beginHandling(item)
		// [决策理由] 状态快照之后插件可能被禁用或进入迁移，此时本轮广播应跳过该插件。
		if !ready {
			continue
		}
		err = m.invokePlugin(ctx, item, event)
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
	allowed, err := m.RouteAllowed(name, event)
	// [决策理由] 定向路由需保留未注册和未启用错误语义，并在这些状态下避免调用群门禁。
	if err != nil {
		return err
	}
	// [决策理由] 群策略关闭属于正常业务短路，不应作为命令处理错误。
	if !allowed {
		return nil
	}
	m.mu.RLock()
	item := m.entries[name]
	m.mu.RUnlock()
	_, ready := m.beginHandling(item)
	// [决策理由] 命令不能绕过插件关闭、迁移或故障隔离状态。
	if !ready {
		return fmt.Errorf("插件 %s 未启用或正在切换状态", name)
	}
	err = m.invokePlugin(ctx, item, event)
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

// RouteAllowed 在不登记在途调用的前提下按全局状态和群策略顺序预检路由。
// @param name：插件稳定名称；event：待路由事件。
// @returns 全局启用稳定且群策略放行时返回true；未注册或未启用/迁移返回错误。
// ⚠️副作用说明：读取Manager状态和群门禁快照；不增加inFlight，调用前仍须beginHandling二次确认。
func (m *Manager) RouteAllowed(name string, event ws.Event) (bool, error) {
	m.mu.RLock()
	item, exists := m.entries[name]
	// [决策理由] 未注册插件必须保持定向路由原有稳定错误语义，并且不能查询无意义的群策略。
	if !exists {
		m.mu.RUnlock()
		return false, fmt.Errorf("%w: %s", errNotRegistered, name)
	}
	ready := item.enabled && !item.transitioning
	m.mu.RUnlock()
	// [决策理由] 全局禁用或迁移隔离优先于群门禁，避免权限链和gate承担无效工作。
	if !ready {
		return false, fmt.Errorf("插件 %s 未启用或正在切换状态", name)
	}
	allowed := m.GroupAllowed(name, event)

	// >>> 数据演变示例
	// 1. enabled稳定+群100开启 -> true,nil且不登记inFlight。
	// 2. disabled或transitioning -> false,error且不调用GroupGate。
	return allowed, nil
}

// GroupAllowed 判断插件是否允许处理事件所属群。
// @param name：插件稳定名称；event：待路由事件。
// @returns 未注入门禁、私聊、非群事件或策略放行时返回true。
// ⚠️副作用说明：无；仅读取群门禁原子快照。
func (m *Manager) GroupAllowed(name string, event ws.Event) bool {
	// [决策理由] 未配置群门禁的测试和内存运行模式保持原有全局行为。
	if m.groupGate == nil {
		return true
	}
	result := m.groupGate.Allowed(name, event)

	// >>> 数据演变示例
	// 1. keyword_reply+关闭群100 -> false。
	// 2. 私聊或echo非可控 -> true。
	return result
}

// ReloadGroupGate 从持久化状态重建并原子发布群门禁快照。
// @param ctx：控制数据库查询生命周期。
// @returns 未配置门禁时nil，否则返回加载错误。
// ⚠️副作用说明：调用GroupGate.Load，成功后影响后续群事件路由。
func (m *Manager) ReloadGroupGate(ctx context.Context) error {
	// [决策理由] 未注入门禁的内存模式没有可刷新状态，应保持幂等成功。
	if m.groupGate == nil {
		return nil
	}
	err := m.groupGate.Load(ctx)

	// >>> 数据演变示例
	// 1. DB群100由开改关 -> Load新快照 -> 后续GroupAllowed=false。
	// 2. 无GroupGate -> 不访问外部状态 -> nil。
	return err
}

// ErrNotRegistered 表示目标插件未编译注册到当前运行时。
var ErrNotRegistered = errors.New("插件未注册")

var errNotRegistered = ErrNotRegistered
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
