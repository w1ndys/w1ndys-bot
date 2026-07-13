// 📌 影响范围：调用管理 Repository，并在写入后刷新 PluginManager 运行快照。
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
	"github.com/w1ndys/w1ndys-bot/internal/permission"
)

// RuntimeRefresher 定义数据库管理变更后的运行时刷新能力。
type RuntimeRefresher interface {
	Load(context.Context) error
}

// AdminAuthorizer 定义最高管理员身份校验能力。
type AdminAuthorizer interface {
	IsSuperAdmin(string) bool
}

// Service 是 QQ 管理命令与 WebUI 共用的管理业务入口。
type Service struct {
	repository  Repository
	runtime     RuntimeRefresher
	commands    RuntimeRefresher
	permissions RuntimeRefresher
	settings    RuntimeRefresher
	authorizer  AdminAuthorizer
}

// NewService 创建管理服务。
// @param repository：管理仓库；runtime：插件刷新器；commands：命令刷新器；permissions：权限刷新器；settings：设置刷新器；authorizer：最高管理员解析器。
// @returns 可复用的管理服务。
// ⚠️副作用说明：无；仅保存依赖引用。
func NewService(repository Repository, runtime RuntimeRefresher, commands RuntimeRefresher, permissions RuntimeRefresher, settings RuntimeRefresher, authorizer AdminAuthorizer) *Service {
	service := &Service{repository: repository, runtime: runtime, commands: commands, permissions: permissions, settings: settings, authorizer: authorizer}

	// >>> 数据演变示例
	// 1. Repository + Manager + CommandRegistry + Resolver -> Service -> 支持授权、持久化与热刷新。
	// 2. Repository + nil刷新器 + Resolver -> Service -> 授权后仅持久化管理配置。
	return service
}

// ListCommands 返回管理端命令列表。
// @param ctx：查询生命周期；actor：操作者。
// @returns 命令快照或授权、仓库错误。
// ⚠️副作用说明：读取命令 Repository 和管理员快照。
func (s *Service) ListCommands(ctx context.Context, actor Actor) ([]CommandState, error) {
	// [决策理由] 命令配置属于管理信息，读取前也必须鉴权。
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	commands, err := s.repository.ListCommands(ctx)
	// [决策理由] 查询失败和空列表必须明确区分。
	if err != nil {
		return nil, fmt.Errorf("列出命令: %w", err)
	}

	// >>> 数据演变示例
	// 1. 管理员 + Repository[ping] -> [ping]。
	// 2. 非管理员 -> ErrForbidden -> 不查询数据库。
	return commands, nil
}

// CreateCommand 新增命令并热刷新命令快照。
// @param ctx：操作生命周期；actor：操作者；input：新命令字段。
// @returns 新命令快照或校验、事务、刷新错误。
// ⚠️副作用说明：写入命令与审计表，并可能替换命令注册表快照。
func (s *Service) CreateCommand(ctx context.Context, actor Actor, input CommandCreate) (CommandState, error) {
	// [决策理由] 所有管理写入必须先验证服务端身份快照。
	if err := s.authorize(actor); err != nil {
		return CommandState{}, err
	}
	normalized, err := validateCommandInput(input.ScopeType, input.ScopeID, input.Command)
	// [决策理由] 无效作用域或命令不得进入数据库事务。
	if err != nil {
		return CommandState{}, fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	created, err := s.repository.CreateCommand(ctx, actor, input, normalized)
	// [决策理由] 持久化失败时没有新快照需要发布。
	if err != nil {
		return CommandState{}, fmt.Errorf("新增命令: %w", err)
	}
	// [决策理由] 写入成功后必须立即让新命令参与路由。
	if err := s.reloadCommands(ctx); err != nil {
		return created, err
	}

	// >>> 数据演变示例
	// 1. global:测试 -> Normalize -> INSERT+audit -> Registry.Load -> 返回新命令。
	// 2. 重复命令 -> ErrCommandConflict -> 不刷新快照。
	return created, nil
}

// RenameCommand 修改命令文本并热刷新命令快照。
// @param ctx：操作生命周期；actor：操作者；id：命令 ID；command：新文本。
// @returns 更新后命令或授权、校验、事务、刷新错误。
// ⚠️副作用说明：更新命令与审计表，并可能替换命令注册表快照。
func (s *Service) RenameCommand(ctx context.Context, actor Actor, id int64, command string) (CommandState, error) {
	// [决策理由] 命令改名必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return CommandState{}, err
	}
	// [决策理由] 非正数 ID 不可能对应数据库命令。
	if id <= 0 {
		return CommandState{}, fmt.Errorf("%w: %d", ErrCommandNotFound, id)
	}
	normalized, err := commandregistry.Normalize(command, "/")
	// [决策理由] 改名使用与注册表相同的标准化规则。
	if err != nil {
		return CommandState{}, fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	updated, err := s.repository.RenameCommand(ctx, actor, id, command, normalized)
	// [决策理由] 事务失败时保持旧内存快照。
	if err != nil {
		return CommandState{}, fmt.Errorf("修改命令: %w", err)
	}
	// [决策理由] 改名提交后必须立即发布新匹配键。
	if err := s.reloadCommands(ctx); err != nil {
		return updated, err
	}

	// >>> 数据演变示例
	// 1. id=1,“测试” -> UPDATE+audit -> Load -> 返回测试。
	// 2. id=0 -> ErrCommandNotFound -> 不访问数据库。
	return updated, nil
}

// DeleteCommand 删除命令并热刷新命令快照。
// @param ctx：操作生命周期；actor：操作者；id：命令 ID。
// @returns 授权、未找到、事务或刷新错误。
// ⚠️副作用说明：删除命令、写入审计并可能替换命令注册表快照。
func (s *Service) DeleteCommand(ctx context.Context, actor Actor, id int64) error {
	// [决策理由] 命令删除必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return err
	}
	// [决策理由] 非正数 ID 不可能对应数据库命令。
	if id <= 0 {
		return fmt.Errorf("%w: %d", ErrCommandNotFound, id)
	}
	// [决策理由] 删除失败时内存快照仍应保留旧命令。
	if err := s.repository.DeleteCommand(ctx, actor, id); err != nil {
		return fmt.Errorf("删除命令: %w", err)
	}
	// [决策理由] 删除提交后必须立即从路由快照移除。
	if err := s.reloadCommands(ctx); err != nil {
		return err
	}

	// >>> 数据演变示例
	// 1. id=1 -> DELETE+audit -> Registry.Load -> nil。
	// 2. id=404 -> ErrCommandNotFound -> 保留旧快照。
	return nil
}

// validateCommandInput 校验命令作用域并返回标准化文本。
// @param scopeType：global或group；scopeID：0或群号；command：命令文本。
// @returns 标准化命令或作用域、文本错误。
// ⚠️副作用说明：无。
func validateCommandInput(scopeType string, scopeID string, command string) (string, error) {
	// [决策理由] 全局命令只能位于固定作用域0。
	if scopeType == "global" && scopeID != "0" {
		return "", fmt.Errorf("全局命令 scope_id 必须为 0")
	}
	// [决策理由] 群级命令必须提供具体群号。
	if scopeType == "group" && (scopeID == "" || scopeID == "0") {
		return "", fmt.Errorf("群级命令必须提供群号")
	}
	// [决策理由] 未知作用域不能依赖数据库约束才拒绝。
	if scopeType != "global" && scopeType != "group" {
		return "", fmt.Errorf("命令作用域 %q 无效", scopeType)
	}
	normalized, err := commandregistry.Normalize(command, "/")

	// >>> 数据演变示例
	// 1. global,0,“/测试” -> Normalize -> “测试”。
	// 2. group,0,“测试” -> 作用域校验 -> error。
	return normalized, err
}

// reloadCommands 在命令写事务提交后刷新运行时快照。
// @param ctx：刷新生命周期。
// @returns 刷新器错误；未配置刷新器时返回 nil。
// ⚠️副作用说明：可能从数据库重载并替换命令注册表快照。
func (s *Service) reloadCommands(ctx context.Context) error {
	// [决策理由] nil 刷新器支持迁移或离线管理进程只修改数据库。
	if s.commands == nil {
		return nil
	}
	// [决策理由] 刷新失败需要明确告知数据库已提交但运行时仍使用旧快照。
	if err := s.commands.Load(ctx); err != nil {
		return fmt.Errorf("刷新命令运行快照: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB新增测试 -> Load -> 内存立即可匹配测试。
	// 2. Load失败 -> 返回错误 -> Registry保留旧不可变快照。
	return nil
}

// ListPermissions 返回管理端权限策略列表。
// @param ctx：查询生命周期；actor：操作者。
// @returns 权限策略快照或授权、仓库错误。
// ⚠️副作用说明：读取权限 Repository 和管理员快照。
func (s *Service) ListPermissions(ctx context.Context, actor Actor) ([]PermissionState, error) {
	// [决策理由] 权限配置属于敏感管理信息，读取前必须鉴权。
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	policies, err := s.repository.ListPermissions(ctx)
	// [决策理由] 查询失败和空策略列表必须明确区分。
	if err != nil {
		return nil, fmt.Errorf("列出权限策略: %w", err)
	}

	// >>> 数据演变示例
	// 1. 管理员 + Repository[member:deny] -> 返回策略列表。
	// 2. 非管理员 -> ErrForbidden -> 不查询数据库。
	return policies, nil
}

// SetPermission 新增或更新权限覆盖并热刷新权限快照。
// @param ctx：操作生命周期；actor：操作者；input：权限唯一维度和效果。
// @returns 保存后策略或授权、校验、事务、刷新错误。
// ⚠️副作用说明：写入权限与审计表，并可能替换 Permission Resolver 快照。
func (s *Service) SetPermission(ctx context.Context, actor Actor, input PermissionSet) (PermissionState, error) {
	// [决策理由] 权限变更必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return PermissionState{}, err
	}
	// [决策理由] 作用域规则必须在进入数据库前统一验证。
	if err := validatePermission(input); err != nil {
		return PermissionState{}, fmt.Errorf("%w: %v", ErrInvalidPermission, err)
	}
	saved, err := s.repository.SetPermission(ctx, actor, input)
	// [决策理由] 事务失败时不应刷新运行时快照。
	if err != nil {
		return PermissionState{}, fmt.Errorf("保存权限策略: %w", err)
	}
	// [决策理由] 写入提交后必须立即让新权限参与命令判断。
	if err := s.reloadPermissions(ctx); err != nil {
		return saved, err
	}

	// >>> 数据演变示例
	// 1. global:ping:member:deny -> upsert+audit -> Resolver.Load -> deny生效。
	// 2. role=unknown -> 校验失败 -> 不写数据库。
	return saved, nil
}

// DeletePermission 删除权限覆盖并回退到下一层策略。
// @param ctx：操作生命周期；actor：操作者；id：权限策略 ID。
// @returns 授权、未找到、事务或刷新错误。
// ⚠️副作用说明：删除权限、写入审计并可能替换 Permission Resolver 快照。
func (s *Service) DeletePermission(ctx context.Context, actor Actor, id int64) error {
	// [决策理由] 权限删除必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return err
	}
	// [决策理由] 非正数 ID 不可能对应数据库策略。
	if id <= 0 {
		return fmt.Errorf("%w: %d", ErrPermissionNotFound, id)
	}
	// [决策理由] 删除失败时保持旧权限快照。
	if err := s.repository.DeletePermission(ctx, actor, id); err != nil {
		return fmt.Errorf("删除权限策略: %w", err)
	}
	// [决策理由] 删除提交后必须立即按下一层策略重新解析权限。
	if err := s.reloadPermissions(ctx); err != nil {
		return err
	}

	// >>> 数据演变示例
	// 1. 删除群功能deny -> Resolver.Load -> 回退群插件或全局策略。
	// 2. id=0 -> ErrPermissionNotFound -> 不访问数据库。
	return nil
}

// validatePermission 校验权限策略的作用域、角色和效果。
// @param input：待校验权限策略。
// @returns 合法时 nil，否则返回字段错误。
// ⚠️副作用说明：无。
func validatePermission(input PermissionSet) error {
	// [决策理由] 全局权限固定使用 scope_id=0。
	if input.ScopeType == "global" && input.ScopeID != "0" {
		return fmt.Errorf("全局权限 scope_id 必须为 0")
	}
	// [决策理由] 群级权限必须指向具体群号。
	if input.ScopeType == "group" && (input.ScopeID == "" || input.ScopeID == "0") {
		return fmt.Errorf("群级权限必须提供群号")
	}
	// [决策理由] 未知作用域不能进入固定五级覆盖链。
	if input.ScopeType != "global" && input.ScopeType != "group" {
		return fmt.Errorf("权限作用域 %q 无效", input.ScopeType)
	}
	subjectType := permission.SubjectType(input.SubjectType)
	// [决策理由] 主体类型只允许角色或指定用户，不能隐式推断输入含义。
	if subjectType != permission.SubjectRole && subjectType != permission.SubjectUser {
		return fmt.Errorf("权限主体类型 %q 无效", input.SubjectType)
	}
	// [决策理由] 角色主体必须与 Permission Resolver 支持集合一致。
	if subjectType == permission.SubjectRole {
		role := permission.Role(input.SubjectID)
		// [决策理由] 未知角色无法映射 NapCat 群身份，必须拒绝。
		if role != permission.RoleSuperAdmin && role != permission.RoleGroupOwner && role != permission.RoleGroupAdmin && role != permission.RoleMember {
			return fmt.Errorf("权限角色 %q 无效", input.SubjectID)
		}
	}
	// [决策理由] 用户主体必须是正十进制 QQ 号，避免永远无法匹配的策略。
	if subjectType == permission.SubjectUser {
		userID, err := strconv.ParseUint(input.SubjectID, 10, 64)
		// [决策理由] 解析失败或零值都不是有效 QQ 身份。
		if err != nil || userID == 0 {
			return fmt.Errorf("权限用户 QQ %q 格式无效", input.SubjectID)
		}
	}
	effect := permission.Effect(input.Effect)
	// [决策理由] 权限效果只允许明确 allow 或 deny。
	if effect != permission.EffectAllow && effect != permission.EffectDeny {
		return fmt.Errorf("权限效果 %q 无效", input.Effect)
	}
	// [决策理由] 插件名为空会形成无法匹配的孤立策略。
	if input.PluginName == "" {
		return fmt.Errorf("权限插件名不能为空")
	}

	// >>> 数据演变示例
	// 1. group,123,ping,ping,role,member,deny -> nil。
	// 2. global,123或user,abc -> error。
	return nil
}

// reloadPermissions 在权限事务提交后刷新运行时快照。
// @param ctx：刷新生命周期。
// @returns 刷新器错误；未配置刷新器时返回 nil。
// ⚠️副作用说明：可能从数据库重载并替换 Permission Resolver 快照。
func (s *Service) reloadPermissions(ctx context.Context) error {
	// [决策理由] nil 刷新器支持离线管理进程只修改数据库。
	if s.permissions == nil {
		return nil
	}
	// [决策理由] 刷新失败需要明确告知数据库已提交但运行时仍使用旧快照。
	if err := s.permissions.Load(ctx); err != nil {
		return fmt.Errorf("刷新权限运行快照: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB新增deny -> Load -> 内存立即拒绝对应角色。
	// 2. Load失败 -> 返回错误 -> Resolver保留旧不可变快照。
	return nil
}

// ListSettings 返回全部已注册设置及当前有效值。
// @param ctx：查询生命周期；actor：操作者。
// @returns 设置列表或授权、仓库错误。
// ⚠️副作用说明：读取 system_settings；缺失数据库值时合并定义默认值。
func (s *Service) ListSettings(ctx context.Context, actor Actor) ([]SettingState, error) {
	// [决策理由] 系统设置属于敏感管理配置，读取前必须鉴权。
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	stored, err := s.repository.ListSystemSettings(ctx)
	// [决策理由] 数据库查询失败时无法确定覆盖值。
	if err != nil {
		return nil, fmt.Errorf("列出系统设置: %w", err)
	}
	byKey := make(map[string]SettingState, len(stored))
	for _, state := range stored {
		byKey[state.Key] = state
	}
	definitions := Definitions()
	result := make([]SettingState, 0, len(definitions))
	for key, definition := range definitions {
		state, exists := byKey[key]
		// [决策理由] 数据库未覆盖时管理端也需要看到默认有效值。
		if !exists {
			state = SettingState{Key: key, Value: append(json.RawMessage(nil), definition.Default...), Description: definition.Description}
		}
		result = append(result, state)
	}
	sort.Slice(result, func(i int, j int) bool {
		// >>> 数据演变示例
		// 1. default_page_size,command_prefix -> command_prefix排前。
		// 2. 单项列表 -> 顺序不变。
		return result[i].Key < result[j].Key
	})

	// >>> 数据演变示例
	// 1. DB prefix="!" + 其他缺失 -> 返回!及其余默认值。
	// 2. DB空 -> 返回4项完整默认设置。
	return result, nil
}

// SetSetting 校验并保存已注册系统设置。
// @param ctx：操作生命周期；actor：操作者；key：设置键；value：JSON原始值。
// @returns 保存后设置或授权、校验、事务、刷新错误。
// ⚠️副作用说明：写入设置与审计表，并可能替换 SettingsResolver 快照。
func (s *Service) SetSetting(ctx context.Context, actor Actor, key string, value json.RawMessage) (SettingState, error) {
	// [决策理由] 系统设置写入必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return SettingState{}, err
	}
	definition, exists := settingDefinitions[key]
	// [决策理由] 未注册键不得进入数据库形成不可控动态配置。
	if !exists {
		return SettingState{}, fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	// [决策理由] 写入前使用当前版本定义执行类型和范围校验。
	if err := validateSetting(key, value); err != nil {
		return SettingState{}, err
	}
	setting := SettingState{Key: key, Value: append(json.RawMessage(nil), value...), Description: definition.Description}
	saved, err := s.repository.SetSystemSetting(ctx, actor, setting)
	// [决策理由] 事务失败时不应刷新设置快照。
	if err != nil {
		return SettingState{}, fmt.Errorf("保存系统设置: %w", err)
	}
	// [决策理由] 提交后必须立即让支持热更新的业务读取新值。
	if err := s.reloadSettings(ctx); err != nil {
		return saved, err
	}

	// >>> 数据演变示例
	// 1. command_prefix="!" -> 校验+UPSERT+audit -> 路由立即使用!。
	// 2. unknown=true -> ErrUnknownSetting -> 不写数据库。
	return saved, nil
}

// DeleteSetting 删除数据库覆盖并回退到定义默认值。
// @param ctx：操作生命周期；actor：操作者；key：设置键。
// @returns 授权、未知键、未找到、事务或刷新错误。
// ⚠️副作用说明：删除设置覆盖、写审计并可能替换 SettingsResolver 快照。
func (s *Service) DeleteSetting(ctx context.Context, actor Actor, key string) error {
	// [决策理由] 系统设置删除必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return err
	}
	_, exists := settingDefinitions[key]
	// [决策理由] 未注册键不能通过管理服务操作。
	if !exists {
		return fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	// [决策理由] 删除失败时保持旧运行快照。
	if err := s.repository.DeleteSystemSetting(ctx, actor, key); err != nil {
		return fmt.Errorf("删除系统设置: %w", err)
	}
	// [决策理由] 删除覆盖后必须立即发布默认有效值。
	if err := s.reloadSettings(ctx); err != nil {
		return err
	}

	// >>> 数据演变示例
	// 1. 删除command_prefix覆盖 -> Resolver.Load -> 前缀回退/。
	// 2. 删除unknown -> ErrUnknownSetting -> 不访问数据库。
	return nil
}

// reloadSettings 在设置事务提交后刷新运行时快照。
// @param ctx：刷新生命周期。
// @returns 刷新器错误；未配置刷新器时返回 nil。
// ⚠️副作用说明：可能从数据库重载并替换 SettingsResolver 快照。
func (s *Service) reloadSettings(ctx context.Context) error {
	// [决策理由] nil 刷新器支持离线管理进程只修改数据库。
	if s.settings == nil {
		return nil
	}
	// [决策理由] 刷新失败需明确告知数据库已提交但运行时仍用旧快照。
	if err := s.settings.Load(ctx); err != nil {
		return fmt.Errorf("刷新系统设置快照: %w", err)
	}

	// >>> 数据演变示例
	// 1. DB prefix="!" -> Load -> 路由立即读取!。
	// 2. Load失败 -> 返回错误 -> Resolver保留旧快照。
	return nil
}

// ListPlugins 返回管理端插件列表。
// @param ctx：查询生命周期；actor：操作者。
// @returns 插件快照或仓库错误。
// ⚠️副作用说明：调用 Repository 执行只读查询。
func (s *Service) ListPlugins(ctx context.Context, actor Actor) ([]PluginState, error) {
	// [决策理由] 插件状态属于管理信息，读取也必须验证最高管理员身份。
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
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
	// [决策理由] admin 是恢复其他插件的唯一 QQ 控制入口，禁止通过自身接口关闭造成管理锁死。
	if name == "admin" && !enabled {
		return PluginState{}, ErrProtectedPlugin
	}
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
	// [决策理由] 所有管理读写共享同一身份校验，防止入口间授权规则漂移。
	if err := s.authorize(actor); err != nil {
		return PluginState{}, err
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

// authorize 使用服务端身份快照校验管理操作来源和操作者。
// @param actor：待校验操作者。
// @returns 身份有效且获授权时返回 nil，否则返回稳定领域错误。
// ⚠️副作用说明：读取最高管理员内存快照。
func (s *Service) authorize(actor Actor) error {
	// [决策理由] 审计记录必须能定位真实操作者，空 ID 不允许进入管理服务。
	if actor.ID == "" {
		return ErrInvalidActor
	}
	// [决策理由] 数据库约束只允许已定义的双控制通道和系统操作。
	if !validChannel(actor.Channel) {
		return ErrInvalidChannel
	}
	// [决策理由] QQ 与 WebUI 输入不可信，必须以服务端管理员快照重新校验，不能信任 actor.Role。
	if actor.Channel != ChannelSystem && (s.authorizer == nil || !s.authorizer.IsSuperAdmin(actor.ID)) {
		return ErrForbidden
	}

	// >>> 数据演变示例
	// 1. QQ用户100 + Resolver允许 -> nil。
	// 2. actor.Role伪造super_admin + Resolver拒绝 -> ErrForbidden。
	return nil
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
