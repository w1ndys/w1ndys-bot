package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

const runtimeConfigCompensateTimeout = 10 * time.Second

// RuntimeStateStore 是插件开关应用服务依赖的持久化意图读写契约。
type RuntimeStateStore interface {
	LoadSnapshot(context.Context) ([]PersistedPluginState, error)
	FindState(context.Context, string) (PersistedPluginState, error)
	UpdateDesiredEnabled(context.Context, management.Actor, string, bool, int64) (PersistedPluginState, error)
	SetGroupEnabled(context.Context, management.Actor, string, int64, bool, int64) (PersistedGroupState, error)
	FindConfig(context.Context, string) (PersistedPluginConfig, error)
	SaveConfig(context.Context, management.Actor, string, json.RawMessage, int64) (PersistedPluginConfig, error)
}

var (
	ErrRuntimeConfigNotSupported = errors.New("插件未声明小型配置")
	ErrRuntimeConfigInvalid      = errors.New("插件配置无效")
	ErrRuntimeConfigHookPanic    = errors.New("插件配置钩子发生 panic")
)

// RuntimeConfigView 是配置管理页需要的 Schema、脱敏值与乐观锁版本。
type RuntimeConfigView struct {
	PluginKey string          `json:"plugin_key"`
	Schema    ConfigSchema    `json:"schema"`
	Config    json.RawMessage `json:"config"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// RuntimeAuthorizer 由平台管理服务实现，校验管理操作者身份与来源。
type RuntimeAuthorizer interface {
	Authorize(management.Actor) error
}

// RuntimeGroupView 是一个群开关的管理视图。
type RuntimeGroupView struct {
	GroupID   int64     `json:"group_id"`
	Enabled   bool      `json:"enabled"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RuntimeCommandView 是代码持有的命令声明，WebUI 只读展示，不接受修改。
type RuntimeCommandView struct {
	Key          string   `json:"key"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	Triggers     []string `json:"triggers"`
	Scope        string   `json:"scope"`
	AllowedRoles []string `json:"allowed_roles"`
}

// RuntimeStateView 同时暴露管理员意图与进程内实际运行状态，供 WebUI 展示两者分歧。
type RuntimeStateView struct {
	PluginKey      string               `json:"plugin_key"`
	DisplayName    string               `json:"display_name"`
	Description    string               `json:"description"`
	AdminPageKey   string               `json:"admin_page_key"`
	DesiredEnabled bool                 `json:"desired_enabled"`
	Version        int64                `json:"version"`
	UpdatedAt      time.Time            `json:"updated_at"`
	Status         RuntimeStatus        `json:"status"`
	InFlight       int                  `json:"in_flight"`
	LastError      string               `json:"last_error"`
	HasConfig      bool                 `json:"has_config"`
	Commands       []RuntimeCommandView `json:"commands"`
	Groups         []RuntimeGroupView   `json:"groups"`
}

// RuntimeService 是 WebUI 与 QQ 应急入口共用的插件开关唯一写路径。
//
// 持久化意图与内存运行状态按“更保守的一侧先执行”排序，两个方向都 fail-closed：
// 启用先落库再驱动生命周期，生命周期失败时运行时停在 failed 且不接流量；
// 关闭先停止内存准入再落库，落库失败时运行时已经关闭。
type RuntimeService struct {
	catalog    *SpecCatalog
	controller *RuntimeController
	store      RuntimeStateStore
	authorizer RuntimeAuthorizer
	locks      sync.Map
}

// NewRuntimeService 创建插件开关应用服务。
func NewRuntimeService(catalog *SpecCatalog, controller *RuntimeController, store RuntimeStateStore, authorizer RuntimeAuthorizer) (*RuntimeService, error) {
	if catalog == nil || controller == nil || isNilRuntimeDependency(store) || isNilRuntimeDependency(authorizer) {
		return nil, errors.New("插件开关服务依赖不能为空")
	}
	return &RuntimeService{catalog: catalog, controller: controller, store: store, authorizer: authorizer}, nil
}

// List 按编译期目录顺序返回全部插件的意图、运行状态和群开关。
func (s *RuntimeService) List(ctx context.Context, actor management.Actor) ([]RuntimeStateView, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return nil, err
	}
	states, err := s.store.LoadSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取插件状态快照: %w", err)
	}
	persisted := make(map[string]PersistedPluginState, len(states))
	for _, state := range states {
		persisted[state.PluginKey] = state
	}
	specs := s.catalog.Specs()
	views := make([]RuntimeStateView, 0, len(specs))
	for _, spec := range specs {
		// 目录中存在但尚未同步出状态行的插件按默认关闭展示，不隐藏其存在。
		views = append(views, s.view(spec, persisted[spec.Key]))
	}
	return views, nil
}

// Get 返回单个插件的意图、运行状态和群开关。
func (s *RuntimeService) Get(ctx context.Context, actor management.Actor, pluginKey string) (RuntimeStateView, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RuntimeStateView{}, err
	}
	spec, err := s.spec(pluginKey)
	if err != nil {
		return RuntimeStateView{}, err
	}
	state, err := s.store.FindState(ctx, pluginKey)
	if err != nil {
		return RuntimeStateView{}, fmt.Errorf("读取插件 %s 状态: %w", pluginKey, err)
	}
	return s.view(spec, state), nil
}

// SetGlobalEnabled 按乐观锁修改全局启用意图并驱动生命周期。
// 已是目标意图时不产生写入与审计。
func (s *RuntimeService) SetGlobalEnabled(ctx context.Context, actor management.Actor, pluginKey string, enabled bool, expectedVersion int64) (RuntimeStateView, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RuntimeStateView{}, err
	}
	spec, err := s.spec(pluginKey)
	if err != nil {
		return RuntimeStateView{}, err
	}
	unlock := s.lock(pluginKey)
	defer unlock()

	current, err := s.store.FindState(ctx, pluginKey)
	if err != nil {
		return RuntimeStateView{}, fmt.Errorf("读取插件 %s 状态: %w", pluginKey, err)
	}
	if current.Version != expectedVersion {
		return RuntimeStateView{}, ErrRuntimeStateConflict
	}
	if current.DesiredEnabled == enabled {
		return s.view(spec, current), nil
	}
	// 生命周期无法接受本次切换时提前拒绝，避免把注定失败的意图写进数据库。
	if err := s.checkTransition(pluginKey, enabled); err != nil {
		return RuntimeStateView{}, err
	}
	if enabled {
		return s.enable(ctx, actor, spec, current)
	}
	return s.disable(ctx, actor, spec, current)
}

// SetGroupEnabled 按乐观锁修改单群开关；expectedVersion 为 0 表示尚无记录。
// 全局关闭时仍允许维护群意图，群开关由运行门禁在分发时判定。
func (s *RuntimeService) SetGroupEnabled(ctx context.Context, actor management.Actor, pluginKey string, groupID int64, enabled bool, expectedVersion int64) (RuntimeStateView, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RuntimeStateView{}, err
	}
	spec, err := s.spec(pluginKey)
	if err != nil {
		return RuntimeStateView{}, err
	}
	if groupID <= 0 {
		return RuntimeStateView{}, ErrInvalidRuntimeGroupID
	}
	unlock := s.lock(pluginKey)
	defer unlock()

	current, err := s.store.FindState(ctx, pluginKey)
	if err != nil {
		return RuntimeStateView{}, fmt.Errorf("读取插件 %s 状态: %w", pluginKey, err)
	}
	before, found := findPersistedGroup(current.Groups, groupID)
	switch {
	case found && before.Version != expectedVersion:
		return RuntimeStateView{}, ErrRuntimeStateConflict
	case !found && expectedVersion != 0:
		return RuntimeStateView{}, ErrRuntimeStateConflict
	case found && before.Enabled == enabled:
		return s.view(spec, current), nil
	}
	apply := func() error { return s.controller.SetGroupEnabled(pluginKey, groupID, enabled) }
	persist := func() (PersistedGroupState, error) {
		return s.store.SetGroupEnabled(ctx, actor, pluginKey, groupID, enabled, expectedVersion)
	}
	var saved PersistedGroupState
	if enabled {
		if saved, err = persist(); err != nil {
			return RuntimeStateView{}, fmt.Errorf("保存插件 %s 群 %d 状态: %w", pluginKey, groupID, err)
		}
		if err = apply(); err != nil {
			return RuntimeStateView{}, fmt.Errorf("应用插件 %s 群 %d 开关: %w", pluginKey, groupID, err)
		}
	} else {
		if err = apply(); err != nil {
			return RuntimeStateView{}, fmt.Errorf("应用插件 %s 群 %d 开关: %w", pluginKey, groupID, err)
		}
		if saved, err = persist(); err != nil {
			return RuntimeStateView{}, fmt.Errorf("保存插件 %s 群 %d 状态: %w", pluginKey, groupID, err)
		}
	}
	current.Groups = mergePersistedGroup(current.Groups, saved)
	return s.view(spec, current), nil
}

// GetConfig 返回插件配置 Schema 与脱敏后的当前值。
func (s *RuntimeService) GetConfig(ctx context.Context, actor management.Actor, pluginKey string) (RuntimeConfigView, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RuntimeConfigView{}, err
	}
	spec, err := s.configurableSpec(pluginKey)
	if err != nil {
		return RuntimeConfigView{}, err
	}
	stored, err := s.store.FindConfig(ctx, pluginKey)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("读取插件 %s 配置: %w", pluginKey, err)
	}
	// 脱敏同时补齐 Schema 默认值，首次保存前缺失的字段也能正常渲染。
	redacted, err := RedactConfig(spec.Config.Schema, stored.ConfigJSON)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("%w: %v", ErrRuntimeConfigInvalid, err)
	}
	return RuntimeConfigView{
		PluginKey: pluginKey, Schema: spec.Config.Schema, Config: redacted,
		Version: stored.Version, UpdatedAt: stored.UpdatedAt.UTC(),
	}, nil
}

// SetConfig 按乐观锁合并、校验并保存配置，随后热应用到运行实例。
// 热应用失败会把数据库补偿回旧值，避免持久化配置与运行快照长期不一致。
func (s *RuntimeService) SetConfig(ctx context.Context, actor management.Actor, pluginKey string, update json.RawMessage, expectedVersion int64) (RuntimeConfigView, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RuntimeConfigView{}, err
	}
	spec, err := s.configurableSpec(pluginKey)
	if err != nil {
		return RuntimeConfigView{}, err
	}
	unlock := s.lock(pluginKey)
	defer unlock()

	current, err := s.store.FindConfig(ctx, pluginKey)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("读取插件 %s 配置: %w", pluginKey, err)
	}
	if current.Version != expectedVersion {
		return RuntimeConfigView{}, ErrRuntimeStateConflict
	}
	// 合并保留未提交的 secret 原值，随后严格规范化以拒绝未知字段和类型错误。
	merged, err := MergeConfigUpdate(spec.Config.Schema, current.ConfigJSON, update)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("%w: %v", ErrRuntimeConfigInvalid, err)
	}
	normalized, err := NormalizeConfig(spec.Config.Schema, merged)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("%w: %v", ErrRuntimeConfigInvalid, err)
	}
	if err := invokeConfigHook(ctx, spec.Config.Validate, normalized); err != nil {
		return RuntimeConfigView{}, fmt.Errorf("%w: %v", ErrRuntimeConfigInvalid, err)
	}
	saved, err := s.store.SaveConfig(ctx, actor, pluginKey, normalized, expectedVersion)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("保存插件 %s 配置: %w", pluginKey, err)
	}
	if applyErr := invokeConfigHook(ctx, spec.Config.Apply, normalized); applyErr != nil {
		return RuntimeConfigView{}, s.compensateConfig(ctx, actor, spec, current, saved, applyErr)
	}
	return s.configView(spec, saved)
}

// compensateConfig 在热应用失败后把数据库写回旧配置并重新发布旧快照。
func (s *RuntimeService) compensateConfig(ctx context.Context, actor management.Actor, spec PluginSpec, previous, saved PersistedPluginConfig, applyErr error) error {
	failure := fmt.Errorf("热应用插件 %s 配置: %w", spec.Key, applyErr)
	// 补偿必须脱离调用方取消信号，否则请求超时会留下已写库但未生效的配置。
	compensateContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeConfigCompensateTimeout)
	defer cancel()
	restored, err := s.store.SaveConfig(compensateContext, actor, spec.Key, previous.ConfigJSON, saved.Version)
	if err != nil {
		return errors.Join(failure, fmt.Errorf("补偿插件 %s 配置: %w", spec.Key, err))
	}
	// 旧值曾经成功应用过，重新发布可确保运行快照与补偿后的数据库一致。
	if err := invokeConfigHook(compensateContext, spec.Config.Apply, restored.ConfigJSON); err != nil {
		return errors.Join(failure, fmt.Errorf("恢复插件 %s 配置快照: %w", spec.Key, err))
	}
	return failure
}

// ApplyStoredConfig 在启动恢复期把持久化配置发布到运行实例。
func (s *RuntimeService) ApplyStoredConfig(ctx context.Context, spec PluginSpec, stored PersistedPluginConfig) error {
	if spec.Config == nil {
		return nil
	}
	normalized, err := NormalizeConfig(spec.Config.Schema, stored.ConfigJSON)
	if err != nil {
		return fmt.Errorf("%w: 插件 %s: %v", ErrRuntimeConfigInvalid, spec.Key, err)
	}
	if err := invokeConfigHook(ctx, spec.Config.Apply, normalized); err != nil {
		return fmt.Errorf("热应用插件 %s 配置: %w", spec.Key, err)
	}
	return nil
}

func (s *RuntimeService) configView(spec PluginSpec, stored PersistedPluginConfig) (RuntimeConfigView, error) {
	redacted, err := RedactConfig(spec.Config.Schema, stored.ConfigJSON)
	if err != nil {
		return RuntimeConfigView{}, fmt.Errorf("%w: %v", ErrRuntimeConfigInvalid, err)
	}
	return RuntimeConfigView{
		PluginKey: spec.Key, Schema: spec.Config.Schema, Config: redacted,
		Version: stored.Version, UpdatedAt: stored.UpdatedAt.UTC(),
	}, nil
}

func (s *RuntimeService) configurableSpec(pluginKey string) (PluginSpec, error) {
	spec, err := s.spec(pluginKey)
	if err != nil {
		return PluginSpec{}, err
	}
	// 未声明配置的插件没有该子资源，不应伪装成空 Schema。
	if spec.Config == nil {
		return PluginSpec{}, fmt.Errorf("%w: %s", ErrRuntimeConfigNotSupported, pluginKey)
	}
	return spec, nil
}

// invokeConfigHook 调用插件配置钩子并隔离 panic。
func invokeConfigHook(ctx context.Context, hook func(context.Context, json.RawMessage) error, raw json.RawMessage) (err error) {
	if hook == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			err = ErrRuntimeConfigHookPanic
		}
	}()
	return hook(ctx, raw)
}

// enable 先持久化启用意图，再执行生命周期；生命周期失败不回滚意图，由运行状态暴露分歧。
func (s *RuntimeService) enable(ctx context.Context, actor management.Actor, spec PluginSpec, current PersistedPluginState) (RuntimeStateView, error) {
	saved, err := s.store.UpdateDesiredEnabled(ctx, actor, spec.Key, true, current.Version)
	if err != nil {
		return RuntimeStateView{}, fmt.Errorf("保存插件 %s 启用意图: %w", spec.Key, err)
	}
	saved.Groups = current.Groups
	if err := s.controller.Enable(ctx, spec.Key); err != nil {
		return s.view(spec, saved), fmt.Errorf("启用插件 %s 运行时: %w", spec.Key, err)
	}
	return s.view(spec, saved), nil
}

// disable 先停止内存准入并排空在途调用，再持久化关闭意图。
func (s *RuntimeService) disable(ctx context.Context, actor management.Actor, spec PluginSpec, current PersistedPluginState) (RuntimeStateView, error) {
	// 生命周期清理失败时运行时已停止准入，仍需落库关闭意图，避免重启后复活管理员已关停的插件。
	disableErr := s.controller.Disable(ctx, spec.Key)
	saved, err := s.store.UpdateDesiredEnabled(ctx, actor, spec.Key, false, current.Version)
	if err != nil {
		return RuntimeStateView{}, errors.Join(fmt.Errorf("保存插件 %s 关闭意图: %w", spec.Key, err), disableErr)
	}
	saved.Groups = current.Groups
	if disableErr != nil {
		return s.view(spec, saved), fmt.Errorf("关闭插件 %s 运行时: %w", spec.Key, disableErr)
	}
	return s.view(spec, saved), nil
}

// checkTransition 在写库前判断生命周期是否可以接受本次切换。
func (s *RuntimeService) checkTransition(pluginKey string, enabled bool) error {
	state, found := s.controller.State(pluginKey)
	if !found {
		return ErrRuntimePluginNotFound
	}
	switch state.Status {
	case RuntimeEnabling, RuntimeDisabling:
		return ErrRuntimeTransition
	case RuntimeFailed:
		// 故障插件必须先关闭完成清理，再重新启用。
		if enabled {
			return ErrRuntimeRecoveryNeeded
		}
	}
	return nil
}

func (s *RuntimeService) spec(pluginKey string) (PluginSpec, error) {
	spec, found := s.catalog.Find(pluginKey)
	if !found {
		return PluginSpec{}, fmt.Errorf("%w: %s", ErrRuntimePluginNotFound, pluginKey)
	}
	return spec, nil
}

func (s *RuntimeService) lock(pluginKey string) func() {
	value, _ := s.locks.LoadOrStore(pluginKey, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (s *RuntimeService) view(spec PluginSpec, state PersistedPluginState) RuntimeStateView {
	view := RuntimeStateView{
		PluginKey:      spec.Key,
		DisplayName:    spec.DisplayName,
		Description:    spec.Description,
		AdminPageKey:   spec.AdminPageKey,
		DesiredEnabled: state.DesiredEnabled,
		Version:        state.Version,
		UpdatedAt:      state.UpdatedAt.UTC(),
		Status:         RuntimeDisabled,
		HasConfig:      spec.Config != nil,
		Commands:       make([]RuntimeCommandView, 0, len(spec.Commands)),
		Groups:         make([]RuntimeGroupView, 0, len(state.Groups)),
	}
	for _, command := range spec.Commands {
		roles := make([]string, 0, len(command.AllowedRoles))
		for role := range command.AllowedRoles {
			roles = append(roles, string(role))
		}
		// 身份集合来自映射，排序后输出使 API 响应稳定可比对。
		sort.Strings(roles)
		view.Commands = append(view.Commands, RuntimeCommandView{
			Key: command.Key, DisplayName: command.DisplayName, Description: command.Description,
			Triggers: append([]string{}, command.Triggers...), Scope: string(command.Scope), AllowedRoles: roles,
		})
	}
	if runtimeState, found := s.controller.State(spec.Key); found {
		view.Status, view.InFlight = runtimeState.Status, runtimeState.InFlight
		if runtimeState.LastError != nil {
			view.LastError = runtimeState.LastError.Error()
		}
	}
	for _, group := range state.Groups {
		view.Groups = append(view.Groups, RuntimeGroupView{
			GroupID: group.GroupID, Enabled: group.Enabled, Version: group.Version, UpdatedAt: group.UpdatedAt.UTC(),
		})
	}
	return view
}

func findPersistedGroup(groups []PersistedGroupState, groupID int64) (PersistedGroupState, bool) {
	for _, group := range groups {
		if group.GroupID == groupID {
			return group, true
		}
	}
	return PersistedGroupState{}, false
}

func mergePersistedGroup(groups []PersistedGroupState, saved PersistedGroupState) []PersistedGroupState {
	merged := make([]PersistedGroupState, 0, len(groups)+1)
	replaced := false
	for _, group := range groups {
		if group.GroupID == saved.GroupID {
			merged, replaced = append(merged, saved), true
			continue
		}
		merged = append(merged, group)
	}
	if !replaced {
		merged = append(merged, saved)
	}
	sort.Slice(merged, func(first, second int) bool { return merged[first].GroupID < merged[second].GroupID })
	return merged
}

func isNilRuntimeDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
