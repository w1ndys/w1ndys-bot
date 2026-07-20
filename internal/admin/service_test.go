// 📌 影响范围：无；使用内存替身验证 AdminService，不连接真实 PostgreSQL。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	states          []PluginState
	before          PluginState
	updated         PluginState
	updateCalls     int
	updateActor     Actor
	updateName      string
	updatePatch     PluginPatch
	commandInput    CommandCreate
	normalized      string
	commandID       int64
	permissionInput PermissionSet
	permissionID    int64
	settings        []SettingState
	setting         SettingState
	settingKey      string
	err             error
	errs            []error
	updateEntered   chan struct{}
	updateRelease   chan struct{}
}

// ListPluginFeatures 返回空功能列表以满足管理仓库契约。
// @param ctx：未使用的上下文；pluginName：插件名。
// @returns 空功能列表或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListPluginFeatures(_ context.Context, _ string) ([]FeatureState, error) {
	// >>> 数据演变示例
	// 1. ping -> []FeatureState{},nil。
	// 2. err=boom -> 空列表,boom。
	return []FeatureState{}, f.err
}

// ListAuditLogs 返回空测试审计页以满足管理仓库契约。
// @param ctx：未使用的上下文；query：审计查询条件。
// @returns 空分页或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListAuditLogs(_ context.Context, query AuditQuery) (AuditPage, error) {
	page := AuditPage{Items: []AuditState{}, Page: query.Page, PageSize: query.PageSize}

	// >>> 数据演变示例
	// 1. page1,size20 -> 空AuditPage,nil。
	// 2. err=boom -> 空AuditPage,boom。
	return page, f.err
}

// GetAuditLog 返回测试审计详情以满足管理仓库契约。
// @param ctx：未使用的上下文；id：审计ID。
// @returns 使用指定ID的审计状态或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) GetAuditLog(_ context.Context, id int64) (AuditState, error) {
	state := AuditState{ID: id}

	// >>> 数据演变示例
	// 1. id8 -> AuditState{8},nil。
	// 2. err=boom -> AuditState{id},boom。
	return state, f.err
}

// ListSystemSettings 返回测试预设设置列表。
// @param ctx：未使用的测试上下文。
// @returns 预设设置列表或错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListSystemSettings(_ context.Context) ([]SettingState, error) {
	// >>> 数据演变示例
	// 1. settings=[prefix] -> 返回列表,nil。
	// 2. err=boom -> 返回列表,boom。
	return f.settings, f.err
}

// SetSystemSetting 记录测试设置并返回相同状态。
// @param ctx：未使用的上下文；actor：操作者；setting：设置状态。
// @returns 原设置或预设错误。
// ⚠️副作用说明：记录 setting。
func (f *fakeRepository) SetSystemSetting(_ context.Context, _ Actor, setting SettingState) (SettingState, error) {
	f.setting = setting

	// >>> 数据演变示例
	// 1. prefix="!" -> 记录 -> 返回相同设置。
	// 2. err=boom -> 记录 -> 返回boom。
	return setting, f.err
}

// DeleteSystemSetting 记录测试删除键。
// @param ctx：未使用的上下文；actor：操作者；key：设置键。
// @returns 预设错误。
// ⚠️副作用说明：记录 settingKey。
func (f *fakeRepository) DeleteSystemSetting(_ context.Context, _ Actor, key string) error {
	f.settingKey = key

	// >>> 数据演变示例
	// 1. key=command_prefix -> 记录 -> nil。
	// 2. err=boom -> 记录 -> boom。
	return f.err
}

// TestSetSettingValidatesPersistsAndRefreshes 验证合法设置写入后热刷新。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetSettingValidatesPersistsAndRefreshes(t *testing.T) {
	repository := &fakeRepository{}
	settings := &fakeRuntime{}
	service := NewService(repository, nil, nil, nil, settings, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	saved, err := service.SetSetting(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, "command_prefix", json.RawMessage(`"!"`))
	// [决策理由] 合法管理员和设置值必须保存成功。
	if err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	// [决策理由] Repository 应收到标准定义说明和原始 JSON 值。
	if saved.Key != "command_prefix" || string(repository.setting.Value) != `"!"` || repository.setting.Description == "" {
		t.Fatalf("setting saved/repository = %+v/%+v", saved, repository.setting)
	}
	// [决策理由] 提交后 SettingsResolver 必须且只能刷新一次。
	if settings.loads != 1 {
		t.Fatalf("settings loads = %d, want 1", settings.loads)
	}

	// >>> 数据演变示例
	// 1. command_prefix="!" -> 校验+Repository -> Load一次。
	// 2. 返回设置包含定义说明 -> WebUI可直接展示。
}

// TestSetSettingRejectsUnknownKey 验证未知设置不进入数据库。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetSettingRejectsUnknownKey(t *testing.T) {
	repository := &fakeRepository{}
	settings := &fakeRuntime{}
	service := NewService(repository, nil, nil, nil, settings, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.SetSetting(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, "db_password", json.RawMessage(`"secret"`))
	// [决策理由] 基础设施或未知键必须返回 ErrUnknownSetting。
	if !errors.Is(err, ErrUnknownSetting) {
		t.Fatalf("SetSetting() error = %v, want ErrUnknownSetting", err)
	}
	// [决策理由] 未知键不得写入数据库或刷新运行时。
	if repository.setting.Key != "" || settings.loads != 0 {
		t.Fatalf("unexpected repository/refresh = %+v/%d", repository.setting, settings.loads)
	}

	// >>> 数据演变示例
	// 1. db_password -> 未注册 -> ErrUnknownSetting。
	// 2. 拒绝发生在Repository前 -> 零写入零刷新。
}

// ListCommands 返回测试预设的空命令列表。
// @param ctx：未使用的测试上下文。
// @returns 空命令列表或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListCommands(_ context.Context) ([]CommandState, error) {
	// >>> 数据演变示例
	// 1. err=nil -> 空列表,nil。
	// 2. err=boom -> 空列表,boom。
	return nil, f.err
}

// CreateCommand 返回测试预设的新命令结果。
// @param ctx：未使用的上下文；actor：操作者；input：新命令；normalized：标准化命令。
// @returns 构造的命令或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) CreateCommand(_ context.Context, _ Actor, input CommandCreate, normalized string) (CommandState, error) {
	f.commandInput, f.normalized = input, normalized
	// >>> 数据演变示例
	// 1. input=测试 -> CommandState{测试},nil。
	// 2. err=boom -> CommandState,boom。
	return CommandState{ID: 1, ScopeType: input.ScopeType, ScopeID: input.ScopeID, PluginName: input.PluginName, FeatureKey: input.FeatureKey, Command: input.Command, NormalizedCommand: normalized, Enabled: true}, f.err
}

// RenameCommand 返回测试构造的改名结果。
// @param ctx：未使用的上下文；actor：操作者；id：命令 ID；command：新命令；normalized：标准化命令。
// @returns 改名命令或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) RenameCommand(_ context.Context, _ Actor, id int64, command string, normalized string) (CommandState, error) {
	f.commandID, f.normalized = id, normalized
	// >>> 数据演变示例
	// 1. id=1,测试 -> CommandState{测试},nil。
	// 2. err=boom -> CommandState,boom。
	return CommandState{ID: id, Command: command, NormalizedCommand: normalized, Enabled: true}, f.err
}

// DeleteCommand 返回测试预设删除错误。
// @param ctx：未使用的上下文；actor：操作者；id：命令 ID。
// @returns 预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) DeleteCommand(_ context.Context, _ Actor, id int64) error {
	f.commandID = id
	// >>> 数据演变示例
	// 1. err=nil -> 删除成功。
	// 2. err=boom -> 删除失败。
	return f.err
}

// ListPermissions 返回测试预设的空权限列表。
// @param ctx：未使用的测试上下文。
// @returns 空权限列表或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListPermissions(_ context.Context) ([]PermissionState, error) {
	// >>> 数据演变示例
	// 1. err=nil -> 空列表,nil。
	// 2. err=boom -> 空列表,boom。
	return nil, f.err
}

// SetPermission 返回测试构造的权限策略。
// @param ctx：未使用的上下文；actor：操作者；input：权限输入。
// @returns 构造策略或预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) SetPermission(_ context.Context, _ Actor, input PermissionSet) (PermissionState, error) {
	f.permissionInput = input
	// >>> 数据演变示例
	// 1. member:deny -> PermissionState{deny},nil。
	// 2. err=boom -> PermissionState,boom。
	return PermissionState{ID: 1, ScopeType: input.ScopeType, ScopeID: input.ScopeID, PluginName: input.PluginName, FeatureKey: input.FeatureKey, SubjectType: input.SubjectType, SubjectID: input.SubjectID, Effect: input.Effect}, f.err
}

// DeletePermission 返回测试预设删除错误。
// @param ctx：未使用的上下文；actor：操作者；id：策略 ID。
// @returns 预设错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) DeletePermission(_ context.Context, _ Actor, id int64) error {
	f.permissionID = id
	// >>> 数据演变示例
	// 1. err=nil -> 删除成功。
	// 2. err=boom -> 删除失败。
	return f.err
}

// TestSetPermissionValidatesAndRefreshes 验证合法权限写入后刷新权限快照。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPermissionValidatesAndRefreshes(t *testing.T) {
	repository := &fakeRepository{}
	permissions := &fakeRuntime{}
	service := NewService(repository, nil, nil, permissions, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	input := PermissionSet{ScopeType: "group", ScopeID: "123", PluginName: "ping", FeatureKey: "ping", SubjectType: "role", SubjectID: "member", Effect: "deny"}
	saved, err := service.SetPermission(context.Background(), Actor{ID: "100", Channel: ChannelQQ}, input)
	// [决策理由] 合法管理员权限策略必须保存成功。
	if err != nil {
		t.Fatalf("SetPermission() error = %v", err)
	}
	// [决策理由] Repository 必须收到完整唯一维度和效果。
	if repository.permissionInput != input || saved.Effect != "deny" {
		t.Fatalf("permission input/saved = %+v/%+v", repository.permissionInput, saved)
	}
	// [决策理由] 提交后权限快照必须且只能刷新一次。
	if permissions.loads != 1 {
		t.Fatalf("permission loads = %d, want 1", permissions.loads)
	}

	// >>> 数据演变示例
	// 1. group123,ping.ping,member,deny -> Repository -> Load一次。
	// 2. 保存返回deny -> 调用方可展示实际状态。
}

// TestSetPermissionRejectsUnknownRole 验证未知角色在写库前被拒绝。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPermissionRejectsUnknownRole(t *testing.T) {
	repository := &fakeRepository{}
	permissions := &fakeRuntime{}
	service := NewService(repository, nil, nil, permissions, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.SetPermission(context.Background(), Actor{ID: "100", Channel: ChannelQQ}, PermissionSet{ScopeType: "global", ScopeID: "0", PluginName: "ping", SubjectType: "role", SubjectID: "root", Effect: "allow"})
	// [决策理由] Resolver 不认识的角色必须返回校验错误。
	if err == nil {
		t.Fatal("SetPermission() error = nil")
	}
	// [决策理由] 校验失败不得进入事务或刷新快照。
	if repository.permissionInput.PluginName != "" || permissions.loads != 0 {
		t.Fatalf("unexpected repository/refresh = %+v/%d", repository.permissionInput, permissions.loads)
	}

	// >>> 数据演变示例
	// 1. role=root -> 校验失败 -> 零写入零刷新。
	// 2. role=member -> 可进入Repository。
}

// TestSetPermissionAcceptsUserSubject 验证指定 QQ 用户可获得群级插件全功能权限。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPermissionAcceptsUserSubject(t *testing.T) {
	repository := &fakeRepository{}
	permissions := &fakeRuntime{}
	service := NewService(repository, nil, nil, permissions, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	input := PermissionSet{ScopeType: "group", ScopeID: "123", PluginName: "ping", SubjectType: "user", SubjectID: "200", Effect: "allow"}
	saved, err := service.SetPermission(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, input)
	// [决策理由] 合法 QQ 用户与空功能键应表示群内插件全功能授权。
	if err != nil {
		t.Fatalf("SetPermission(user) error = %v", err)
	}
	// [决策理由] Repository 必须保留用户主体和插件级空功能键。
	if saved.SubjectType != "user" || saved.SubjectID != "200" || saved.FeatureKey != "" {
		t.Fatalf("saved user permission = %+v", saved)
	}
	// [决策理由] 写入后权限快照必须立即刷新。
	if permissions.loads != 1 {
		t.Fatalf("permission loads = %d, want 1", permissions.loads)
	}

	// >>> 数据演变示例
	// 1. group123+ping插件+user200+allow -> 保存并刷新 -> 全功能授权。
	// 2. 空FeatureKey -> 数据库NULL -> Resolver插件级*候选。
}

// TestCreateCommandNormalizesAndRefreshes 验证新增命令标准化并发布运行快照。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestCreateCommandNormalizesAndRefreshes(t *testing.T) {
	repository := &fakeRepository{}
	commands := &fakeRuntime{}
	service := NewService(repository, nil, commands, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	input := CommandCreate{ScopeType: "global", ScopeID: "0", PluginName: "ping", FeatureKey: "ping", Command: " /测  试 "}
	created, err := service.CreateCommand(context.Background(), Actor{ID: "100", Channel: ChannelQQ}, input)
	// [决策理由] 有效管理员和全局命令必须创建成功。
	if err != nil {
		t.Fatalf("CreateCommand() error = %v", err)
	}
	// [决策理由] 数据库唯一键必须使用与路由相同的标准化文本。
	if repository.normalized != "测 试" || created.NormalizedCommand != "测 试" {
		t.Fatalf("normalized repository/created = %q/%q", repository.normalized, created.NormalizedCommand)
	}
	// [决策理由] 命令事务提交后运行快照必须刷新且只刷新一次。
	if commands.loads != 1 {
		t.Fatalf("command loads = %d, want 1", commands.loads)
	}

	// >>> 数据演变示例
	// 1. “ /测  试 ” -> Normalize -> “测 试” -> Create -> Load一次。
	// 2. global,0 -> 作用域合法 -> 返回新命令。
}

// TestCreateCommandRejectsInvalidScopeBeforeRepository 验证无效群作用域不会写库。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestCreateCommandRejectsInvalidScopeBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	commands := &fakeRuntime{}
	service := NewService(repository, nil, commands, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.CreateCommand(context.Background(), Actor{ID: "100", Channel: ChannelQQ}, CommandCreate{ScopeType: "group", ScopeID: "0", Command: "测试"})
	// [决策理由] 群级命令缺少具体群号必须返回校验错误。
	if err == nil {
		t.Fatal("CreateCommand() error = nil")
	}
	// [决策理由] 校验失败必须发生在 Repository 之前。
	if repository.commandInput.Command != "" || commands.loads != 0 {
		t.Fatalf("unexpected repository/refresh = %+v/%d", repository.commandInput, commands.loads)
	}

	// >>> 数据演变示例
	// 1. group,0 -> 校验失败 -> 零写入零刷新。
	// 2. group,123 -> 可进入Repository。
}

// ListPlugins 返回测试预设插件快照。
// @param ctx：未使用的测试上下文。
// @returns 预设插件快照或错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListPlugins(_ context.Context) ([]PluginState, error) {
	// >>> 数据演变示例
	// 1. states=[ping] -> 返回 [ping]。
	// 2. err=boom -> 返回 nil,boom。
	return f.states, f.err
}

// UpdatePlugin 记录测试调用并返回预设结果。
// @param ctx：未使用的测试上下文；actor：操作者；name：插件名；patch：变更。
// @returns 预设插件状态或错误。
// ⚠️副作用说明：修改 fakeRepository 的调用记录字段。
func (f *fakeRepository) UpdatePlugin(_ context.Context, actor Actor, name string, patch PluginPatch) (PluginState, PluginState, error) {
	f.updateCalls++
	// [决策理由] 可选通道用于验证同名插件的完整更新与补偿窗口被串行化。
	if f.updateEntered != nil {
		f.updateEntered <- struct{}{}
		<-f.updateRelease
	}
	f.updateActor = actor
	f.updateName = name
	f.updatePatch = patch
	// [决策理由] 错误序列用于区分首次提交和补偿提交的故障。
	if len(f.errs) >= f.updateCalls {
		return f.before, f.updated, f.errs[f.updateCalls-1]
	}

	// >>> 数据演变示例
	// 1. ping + enabled=true -> 记录参数 -> 返回 updated。
	// 2. missing + err -> 记录参数 -> 返回 error。
	return f.before, f.updated, f.err
}

// TestPluginUpdatesAreSerializedByName 验证同一插件的写入、刷新及补偿窗口不会交错。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动两个管理请求 goroutine，并通过通道控制内存仓库时序。
func TestPluginUpdatesAreSerializedByName(t *testing.T) {
	repository := &fakeRepository{
		before:        PluginState{Name: "ping", Enabled: false},
		updated:       PluginState{Name: "ping", Enabled: true},
		updateEntered: make(chan struct{}, 2),
		updateRelease: make(chan struct{}, 2),
	}
	service := NewService(repository, &fakeRuntime{}, nil, nil, nil, &fakeAuthorizer{})
	actor := Actor{ID: "system", Role: "system", Channel: ChannelSystem}
	results := make(chan error, 2)
	// 启动第一个插件更新请求。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 SetPluginEnabled 并发送错误。
	go func() {
		_, err := service.SetPluginEnabled(context.Background(), actor, "ping", true)
		results <- err

		// >>> 数据演变示例
		// 1. ping锁空闲 -> 请求一获得锁 -> 进入Repository。
		// 2. Repository释放 -> Load完成 -> 释放ping锁。
	}()
	<-repository.updateEntered
	// 启动同名插件的第二个更新请求。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 SetPluginEnabled 并发送错误。
	go func() {
		_, err := service.SetPluginEnabled(context.Background(), actor, "ping", false)
		results <- err

		// >>> 数据演变示例
		// 1. 请求一持有ping锁 -> 请求二等待且不进入Repository。
		// 2. 请求一释放 -> 请求二进入Repository -> 独立完成。
	}()
	select {
	case <-repository.updateEntered:
		t.Fatal("同名插件第二个请求在首个请求完成前进入仓库")
	case <-time.After(20 * time.Millisecond):
		// [决策理由] 未观察到第二次仓库调用证明同名更新锁覆盖了完整请求窗口。
	}
	repository.updateRelease <- struct{}{}
	// [决策理由] 第一个请求完成并释放同名锁后，第二个请求才允许进入仓库。
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	<-repository.updateEntered
	repository.updateRelease <- struct{}{}
	// [决策理由] 第二个串行请求也必须正常完成。
	if err := <-results; err != nil {
		t.Fatal(err)
	}

	// >>> 数据演变示例
	// 1. ping请求一写库→刷新 -> 解锁 -> ping请求二写库→刷新。
	// 2. 第二请求启动时第一请求未完成 -> Repository调用数保持1直至释放。
}

type fakeRuntime struct {
	loads int
	err   error
	errs  []error
}

type fakeAuthorizer struct {
	allowed map[string]bool
}

// IsSuperAdmin 返回测试预设的授权判断。
// @param userID：待校验用户 ID。
// @returns allowed 中对应的布尔值。
// ⚠️副作用说明：无。
func (f *fakeAuthorizer) IsSuperAdmin(userID string) bool {
	allowed := f.allowed[userID]

	// >>> 数据演变示例
	// 1. allowed[123]=true -> IsSuperAdmin(123) -> true。
	// 2. allowed无200 -> IsSuperAdmin(200) -> false。
	return allowed
}

// Load 记录一次运行时刷新。
// @param ctx：未使用的测试上下文。
// @returns 预设刷新错误。
// ⚠️副作用说明：递增 loads 计数。
func (f *fakeRuntime) Load(_ context.Context) error {
	f.loads++
	// [决策理由] 错误序列用于分别模拟首次刷新失败与补偿刷新成功或失败。
	if len(f.errs) >= f.loads {
		return f.errs[f.loads-1]
	}

	// >>> 数据演变示例
	// 1. loads=0 -> Load -> loads=1,nil。
	// 2. err=boom -> Load -> loads+1,boom。
	return f.err
}

// TestSetPluginEnabledPersistsAuditedChangeAndRefreshes 验证启停操作参数和热刷新。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledPersistsAuditedChangeAndRefreshes(t *testing.T) {
	repository := &fakeRepository{updated: PluginState{Name: "ping", Enabled: true, Priority: 100}}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"123": true}})
	actor := Actor{ID: "123", Role: "super_admin", Channel: ChannelQQ, RequestID: "req-1"}
	state, err := service.SetPluginEnabled(context.Background(), actor, "ping", true)
	// [决策理由] 正常管理路径必须无错误才能继续验证结果。
	if err != nil {
		t.Fatalf("SetPluginEnabled() error = %v", err)
	}
	// [决策理由] Service 必须返回仓库提交后的真实状态。
	if !state.Enabled || state.Name != "ping" {
		t.Fatalf("state = %+v", state)
	}
	// [决策理由] 操作者和请求标识必须原样进入事务审计。
	if repository.updateActor != actor || repository.updateName != "ping" {
		t.Fatalf("update actor/name = %+v/%q", repository.updateActor, repository.updateName)
	}
	// [决策理由] 指针 patch 必须明确表达 enabled=false 与未修改的区别。
	if repository.updatePatch.Enabled == nil || !*repository.updatePatch.Enabled {
		t.Fatalf("enabled patch = %+v", repository.updatePatch.Enabled)
	}
	// [决策理由] 成功写入后运行时必须且只能刷新一次。
	if runtime.loads != 1 {
		t.Fatalf("runtime loads = %d, want 1", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. ping:false + QQ管理员启用 -> Repository调用 -> Load一次 -> ping:true。
	// 2. RequestID=req-1 -> Actor透传 -> 审计可关联请求。
}

// TestSetPluginPriorityRejectsInvalidActor 验证缺失操作者时不产生写入和刷新。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginPriorityRejectsInvalidActor(t *testing.T) {
	repository := &fakeRepository{}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{})
	_, err := service.SetPluginPriority(context.Background(), Actor{Channel: ChannelWebUI}, "ping", 20)
	// [决策理由] 空 Actor ID 必须返回稳定领域错误供入口转换为拒绝响应。
	if !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("error = %v, want ErrInvalidActor", err)
	}
	// [决策理由] 校验失败发生在仓库前，因此插件名调用记录必须为空。
	if repository.updateName != "" {
		t.Fatalf("repository unexpectedly called for %q", repository.updateName)
	}
	// [决策理由] 未持久化的状态不得发布到运行时。
	if runtime.loads != 0 {
		t.Fatalf("runtime loads = %d, want 0", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. Actor.ID空 + priority=20 -> ErrInvalidActor -> 零写入。
	// 2. Actor.ID空 + runtime -> 校验提前返回 -> loads=0。
}

// TestSetPluginEnabledReturnsRefreshFailure 验证数据库成功后刷新失败会明确上报。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledReturnsRefreshFailure(t *testing.T) {
	repository := &fakeRepository{before: PluginState{Name: "ping", Enabled: false, Priority: 10}, updated: PluginState{Name: "ping", Enabled: true, Priority: 10}}
	runtime := &fakeRuntime{errs: []error{errors.New("lifecycle failed"), nil}}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{})
	state, err := service.SetPluginEnabled(context.Background(), Actor{ID: "system", Role: "system", Channel: ChannelSystem}, "ping", true)
	// [决策理由] 热刷新失败不能向管理入口报告完全成功。
	if err == nil {
		t.Fatal("SetPluginEnabled() error = nil")
	}
	// [决策理由] 返回已提交状态让上层知道数据库目标值，便于告警和重试刷新。
	if !state.Enabled {
		t.Fatalf("state = %+v, want committed state", state)
	}
	// [决策理由] 首次刷新失败后必须写回旧数据库状态并再次刷新运行时。
	if runtime.loads != 2 || repository.updateCalls != 2 {
		t.Fatalf("runtime loads/update calls = %d/%d, want 2/2", runtime.loads, repository.updateCalls)
	}
	// [决策理由] 第二次仓库更新必须完整恢复事务前的启用状态和优先级。
	if repository.updatePatch.Enabled == nil || *repository.updatePatch.Enabled || repository.updatePatch.Priority == nil || *repository.updatePatch.Priority != 10 {
		t.Fatalf("rollback patch = %+v", repository.updatePatch)
	}

	// >>> 数据演变示例
	// 1. DB提交true + Load失败 -> 返回 true状态和刷新错误。
	// 2. lifecycle failed -> DB补偿false:10 -> Load恢复 -> 返回原刷新错误。
}

// TestSetPluginEnabledJoinsDatabaseRollbackFailure 验证刷新与数据库补偿错误均被保留。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存仓库和运行时替身。
func TestSetPluginEnabledJoinsDatabaseRollbackFailure(t *testing.T) {
	refreshErr := errors.New("刷新失败")
	rollbackErr := errors.New("补偿写库失败")
	repository := &fakeRepository{
		before:  PluginState{Name: "ping", Enabled: false},
		updated: PluginState{Name: "ping", Enabled: true},
		errs:    []error{nil, rollbackErr},
	}
	runtime := &fakeRuntime{errs: []error{refreshErr}}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{})
	_, err := service.SetPluginEnabled(context.Background(), Actor{ID: "system", Role: "system", Channel: ChannelSystem}, "ping", true)
	// [决策理由] 运维必须能分别识别原刷新故障与数据库补偿故障。
	if !errors.Is(err, refreshErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("SetPluginEnabled() error = %v", err)
	}
	// [决策理由] 数据库补偿失败后不能执行基于旧数据库状态的二次刷新。
	if runtime.loads != 1 || repository.updateCalls != 2 {
		t.Fatalf("runtime loads/update calls = %d/%d", runtime.loads, repository.updateCalls)
	}

	// >>> 数据演变示例
	// 1. DB新值提交→Load失败→DB补偿失败 -> errors.Join保留两个根因。
	// 2. 补偿未落库 -> 不执行第二次Load -> runtime.loads=1。
}

// TestSetPluginEnabledJoinsRollbackReloadFailure 验证数据库恢复后运行态恢复失败被聚合。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存仓库和运行时替身。
func TestSetPluginEnabledJoinsRollbackReloadFailure(t *testing.T) {
	refreshErr := errors.New("首次刷新失败")
	restoreErr := errors.New("恢复刷新失败")
	repository := &fakeRepository{before: PluginState{Name: "ping", Enabled: false}, updated: PluginState{Name: "ping", Enabled: true}}
	runtime := &fakeRuntime{errs: []error{refreshErr, restoreErr}}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{})
	_, err := service.SetPluginEnabled(context.Background(), Actor{ID: "system", Role: "system", Channel: ChannelSystem}, "ping", true)
	// [决策理由] 数据库已恢复但运行态恢复失败时，两个阶段错误都需要可分类。
	if !errors.Is(err, refreshErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("SetPluginEnabled() error = %v", err)
	}
	// [决策理由] 完整补偿路径应包含两次仓库更新和两次运行态加载。
	if runtime.loads != 2 || repository.updateCalls != 2 {
		t.Fatalf("runtime loads/update calls = %d/%d", runtime.loads, repository.updateCalls)
	}

	// >>> 数据演变示例
	// 1. 首次Load失败→DB恢复成功→第二次Load失败 -> 聚合两个Load根因。
	// 2. 两次Update+两次Load -> 持久化已恢复但运行态需重启修复。
}

// TestSetPluginEnabledRejectsUntrustedRole 验证自报角色不能绕过管理员快照。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledRejectsUntrustedRole(t *testing.T) {
	repository := &fakeRepository{}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.SetPluginEnabled(context.Background(), Actor{ID: "200", Role: "super_admin", Channel: ChannelQQ}, "ping", true)
	// [决策理由] actor.Role 来自入口组装，不能替代服务端身份快照。
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	// [决策理由] 未授权操作必须在事务开始前终止。
	if repository.updateName != "" {
		t.Fatalf("repository unexpectedly called for %q", repository.updateName)
	}
	// [决策理由] 未授权操作不得改变运行时状态。
	if runtime.loads != 0 {
		t.Fatalf("runtime loads = %d, want 0", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. actor.Role=super_admin但Resolver=false -> ErrForbidden。
	// 2. 拒绝发生在Repository前 -> 无数据库写入和热刷新。
}

// TestSetPluginEnabledRejectsDisablingAdmin 验证系统管理入口不能关闭自身。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledRejectsDisablingAdmin(t *testing.T) {
	repository := &fakeRepository{}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, nil, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.SetPluginEnabled(context.Background(), Actor{ID: "100", Channel: ChannelQQ}, "admin", false)
	// [决策理由] 关闭唯一 QQ 恢复入口必须返回稳定保护错误。
	if !errors.Is(err, ErrProtectedPlugin) {
		t.Fatalf("error = %v, want ErrProtectedPlugin", err)
	}
	// [决策理由] 保护判断必须在数据库事务前完成。
	if repository.updateName != "" {
		t.Fatalf("repository unexpectedly called for %q", repository.updateName)
	}

	// >>> 数据演变示例
	// 1. /禁用插件 admin -> ErrProtectedPlugin -> admin保持启用。
	// 2. 最高管理员请求关闭admin -> 仍拒绝 -> 无数据库写入。
}
