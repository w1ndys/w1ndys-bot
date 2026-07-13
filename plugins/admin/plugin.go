// 📌 影响范围：注册系统管理插件；通过 Management 修改插件配置并通过 Messenger 回复 QQ 消息。
package admin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

const featureList = "plugin_list"
const featureEnable = "plugin_enable"
const featureDisable = "plugin_disable"
const featurePriority = "plugin_priority"
const featureCommandList = "command_list"
const featureCommandCreateGlobal = "command_create_global"
const featureCommandCreateGroup = "command_create_group"
const featureCommandRename = "command_rename"
const featureCommandDelete = "command_delete"

type implementation struct {
	messenger  plugin.Messenger
	management management.Controller
}

// Name 返回插件稳定名称。
// @param 无。
// @returns admin。
// ⚠️副作用说明：无。
func (p *implementation) Name() string {
	// >>> 数据演变示例
	// 1. implementation -> Name -> admin。
	// 2. 任意运行状态 -> Name保持admin。
	return "admin"
}

// Handle 执行最高管理员的插件查询、启停和优先级命令。
// @param ctx：包含功能键的上下文；event：OneBot消息事件。
// @returns 参数、管理服务或回复错误。
// ⚠️副作用说明：可能修改插件数据库状态、刷新运行时并回复 QQ 消息。
func (p *implementation) Handle(ctx context.Context, event ws.Event) error {
	message, ok := event.(*ws.MessageEvent)
	// [决策理由] 管理功能只接受具有明确操作者 QQ 号的消息事件。
	if !ok {
		return fmt.Errorf("admin收到非消息事件 %T", event)
	}
	actor := management.Actor{ID: strconv.FormatInt(message.UserID, 10), Role: "super_admin", Channel: management.ChannelQQ, RequestID: strconv.FormatInt(message.MessageID, 10)}
	feature := plugin.FeatureFromContext(ctx)
	var response string
	var err error
	switch feature {
	case featureList:
		response, err = p.list(ctx, actor)
	case featureEnable:
		response, err = p.setEnabled(ctx, actor, message.RawMessage, true)
	case featureDisable:
		response, err = p.setEnabled(ctx, actor, message.RawMessage, false)
	case featurePriority:
		response, err = p.setPriority(ctx, actor, message.RawMessage)
	case featureCommandList:
		response, err = p.listCommands(ctx, actor)
	case featureCommandCreateGlobal:
		response, err = p.createCommand(ctx, actor, message.RawMessage, false)
	case featureCommandCreateGroup:
		response, err = p.createCommand(ctx, actor, message.RawMessage, true)
	case featureCommandRename:
		response, err = p.renameCommand(ctx, actor, message.RawMessage)
	case featureCommandDelete:
		response, err = p.deleteCommand(ctx, actor, message.RawMessage)
	default:
		return nil
	}
	// [决策理由] 管理错误应转换为用户可见结果，同时仍由回复错误进入服务日志。
	if err != nil {
		response = "操作失败：" + err.Error()
	}
	_, replyErr := p.messenger.ReplyToMessage(ctx, message, message.MessageID, response)
	// [决策理由] NapCat 回复失败必须带管理上下文返回日志链路。
	if replyErr != nil {
		return fmt.Errorf("回复管理结果: %w", replyErr)
	}

	// >>> 数据演变示例
	// 1. /启用插件 ping -> AdminService启用 -> 回复操作成功。
	// 2. 非最高管理员 -> ErrForbidden -> 回复操作失败。
	return plugin.ErrStopPropagation
}

// listCommands 格式化全部命令及其作用域和功能目标。
// @param ctx：查询生命周期；actor：操作者。
// @returns 可发送的命令列表文本或授权、查询错误。
// ⚠️副作用说明：读取命令管理 Repository。
func (p *implementation) listCommands(ctx context.Context, actor management.Actor) (string, error) {
	commands, err := p.management.ListCommands(ctx, actor)
	// [决策理由] 查询失败时不能向管理员展示不完整快照。
	if err != nil {
		return "", err
	}
	lines := []string{"命令列表："}
	for _, current := range commands {
		status := "关闭"
		// [决策理由] 命令启用状态需要转换为 QQ 可读文本。
		if current.Enabled {
			status = "启用"
		}
		lines = append(lines, fmt.Sprintf("- #%d [%s:%s] %s → %s.%s（%s）", current.ID, current.ScopeType, current.ScopeID, current.Command, current.PluginName, current.FeatureKey, status))
	}

	// >>> 数据演变示例
	// 1. id=1,global:ping -> “#1 [global:0] ping → ping.ping”。
	// 2. 空列表 -> 仅返回“命令列表：”。
	return strings.Join(lines, "\n"), nil
}

// createCommand 解析全局或群级命令并调用统一管理服务。
// @param ctx：操作生命周期；actor：操作者；raw：原始消息；groupScope：是否为群级命令。
// @returns 创建结果文本或参数、事务、刷新错误。
// ⚠️副作用说明：新增命令、写入审计并热刷新命令快照。
func (p *implementation) createCommand(ctx context.Context, actor management.Actor, raw string, groupScope bool) (string, error) {
	fields := strings.Fields(raw)
	minimum := 4
	// [决策理由] 群级命令比全局命令多一个群号参数。
	if groupScope {
		minimum = 5
	}
	// [决策理由] 必须包含插件、功能和至少一个非空命令词。
	if len(fields) < minimum {
		// [决策理由] 两种作用域需要返回不同且可直接复制的用法提示。
		if groupScope {
			return "", fmt.Errorf("用法：/新增群命令 <群号> <插件> <功能> <命令>")
		}
		return "", fmt.Errorf("用法：/新增全局命令 <插件> <功能> <命令>")
	}
	input := management.CommandCreate{ScopeType: "global", ScopeID: "0"}
	commandStart := 3
	// [决策理由] 群级命令需要显式保存目标群号，不能默认使用命令来源群。
	if groupScope {
		input.ScopeType, input.ScopeID = "group", fields[1]
		input.PluginName, input.FeatureKey = fields[2], fields[3]
		commandStart = 4
	} else {
		input.PluginName, input.FeatureKey = fields[1], fields[2]
	}
	input.Command = strings.Join(fields[commandStart:], " ")
	created, err := p.management.CreateCommand(ctx, actor, input)
	// [决策理由] 事务或重复检测失败时不得回复创建成功。
	if err != nil {
		return "", err
	}

	// >>> 数据演变示例
	// 1. /新增全局命令 ping ping 测试 -> global:0 -> 创建“测试”。
	// 2. /新增群命令 123 ping ping 测 试 -> group:123 -> 创建“测 试”。
	return fmt.Sprintf("命令 #%d 已创建：%s → %s.%s", created.ID, created.Command, created.PluginName, created.FeatureKey), nil
}

// renameCommand 解析命令 ID 和可含空格的新命令文本。
// @param ctx：操作生命周期；actor：操作者；raw：原始消息。
// @returns 改名结果文本或参数、事务、刷新错误。
// ⚠️副作用说明：更新命令、写入审计并热刷新命令快照。
func (p *implementation) renameCommand(ctx context.Context, actor management.Actor, raw string) (string, error) {
	fields := strings.Fields(raw)
	// [决策理由] 改名必须包含命令 ID 和至少一个新命令词。
	if len(fields) < 3 {
		return "", fmt.Errorf("用法：/修改命令 <命令ID> <新命令>")
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	// [决策理由] 非正整数 ID 无法定位数据库命令。
	if err != nil || id <= 0 {
		return "", fmt.Errorf("命令 ID 必须是正整数")
	}
	updated, err := p.management.RenameCommand(ctx, actor, id, strings.Join(fields[2:], " "))
	// [决策理由] 改名失败时继续展示旧命令更符合真实状态。
	if err != nil {
		return "", err
	}

	// >>> 数据演变示例
	// 1. /修改命令 12 新 测试 -> id=12,command=“新 测试” -> 成功文本。
	// 2. /修改命令 abc 测试 -> ID解析失败 -> 参数错误。
	return fmt.Sprintf("命令 #%d 已修改为：%s", updated.ID, updated.Command), nil
}

// deleteCommand 解析命令 ID 并删除对应别名。
// @param ctx：操作生命周期；actor：操作者；raw：原始消息。
// @returns 删除结果文本或参数、事务、刷新错误。
// ⚠️副作用说明：删除命令、写入审计并热刷新命令快照。
func (p *implementation) deleteCommand(ctx context.Context, actor management.Actor, raw string) (string, error) {
	fields := strings.Fields(raw)
	// [决策理由] 删除命令只允许一个 ID 参数，避免误解后续文本。
	if len(fields) != 2 {
		return "", fmt.Errorf("用法：/删除命令 <命令ID>")
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	// [决策理由] 非正整数 ID 无法定位数据库命令。
	if err != nil || id <= 0 {
		return "", fmt.Errorf("命令 ID 必须是正整数")
	}
	// [决策理由] Service 负责事务、审计和快照刷新，任一步失败都不能回复成功。
	if err := p.management.DeleteCommand(ctx, actor, id); err != nil {
		return "", err
	}

	// >>> 数据演变示例
	// 1. /删除命令 12 -> DeleteCommand(12) -> 成功文本。
	// 2. /删除命令 0 -> 参数拒绝 -> 不访问Repository。
	return fmt.Sprintf("命令 #%d 已删除", id), nil
}

// list 格式化插件运行状态列表。
// @param ctx：查询生命周期；actor：操作者。
// @returns 可发送的状态文本或查询错误。
// ⚠️副作用说明：读取管理 Repository。
func (p *implementation) list(ctx context.Context, actor management.Actor) (string, error) {
	states, err := p.management.ListPlugins(ctx, actor)
	// [决策理由] 查询失败时没有可信列表可回复。
	if err != nil {
		return "", err
	}
	sort.Slice(states, func(i int, j int) bool {
		// >>> 数据演变示例
		// 1. ping:100,admin:1000 -> admin排前。
		// 2. A:10,B:10 -> 名称A排前。
		if states[i].Priority == states[j].Priority {
			return states[i].Name < states[j].Name
		}
		return states[i].Priority > states[j].Priority
	})
	lines := []string{"插件列表："}
	for _, state := range states {
		status := "关闭"
		// [决策理由] 启用状态需要转换为适合 QQ 阅读的短文本。
		if state.Enabled {
			status = "启用"
		}
		lines = append(lines, fmt.Sprintf("- %s：%s（优先级 %d）", state.Name, status, state.Priority))
	}

	// >>> 数据演变示例
	// 1. [ping:true:100] -> “ping：启用（优先级100）”。
	// 2. [] -> 仅返回“插件列表：”。
	return strings.Join(lines, "\n"), nil
}

// setEnabled 从消息末尾提取插件名并修改启用状态。
// @param ctx：操作生命周期；actor：操作者；raw：原始消息；enabled：目标状态。
// @returns 成功文本或参数、管理错误。
// ⚠️副作用说明：调用 Management 更新数据库、审计并热刷新插件。
func (p *implementation) setEnabled(ctx context.Context, actor management.Actor, raw string, enabled bool) (string, error) {
	fields := strings.Fields(raw)
	// [决策理由] 启停命令必须包含且只包含一个插件名参数。
	if len(fields) != 2 {
		return "", fmt.Errorf("用法：/%s插件 <插件名>", map[bool]string{true: "启用", false: "禁用"}[enabled])
	}
	name := fields[1]
	state, err := p.management.SetPluginEnabled(ctx, actor, name, enabled)
	// [决策理由] 管理服务负责鉴权和事务，失败不得输出成功状态。
	if err != nil {
		return "", err
	}

	// >>> 数据演变示例
	// 1. “/启用插件 ping” -> fields[1]=ping -> enabled=true -> 成功文本。
	// 2. “/禁用插件” -> 参数缺失 -> 用法错误。
	return fmt.Sprintf("插件 %s 已%s（优先级 %d）", state.Name, map[bool]string{true: "启用", false: "禁用"}[enabled], state.Priority), nil
}

// setPriority 从消息提取插件名和整数优先级并保存。
// @param ctx：操作生命周期；actor：操作者；raw：原始消息。
// @returns 成功文本或参数、管理错误。
// ⚠️副作用说明：调用 Management 更新数据库、审计并刷新插件排序。
func (p *implementation) setPriority(ctx context.Context, actor management.Actor, raw string) (string, error) {
	fields := strings.Fields(raw)
	// [决策理由] 优先级命令必须同时提供插件名和整数值。
	if len(fields) != 3 {
		return "", fmt.Errorf("用法：/设置插件优先级 <插件名> <整数>")
	}
	priority, err := strconv.Atoi(fields[2])
	// [决策理由] 非整数无法映射数据库 priority 字段，应在调用服务前拒绝。
	if err != nil {
		return "", fmt.Errorf("优先级必须是整数")
	}
	state, err := p.management.SetPluginPriority(ctx, actor, fields[1], priority)
	// [决策理由] 管理服务错误表示事务或热刷新未完整成功。
	if err != nil {
		return "", err
	}

	// >>> 数据演变示例
	// 1. “/设置插件优先级 ping 200” -> Atoi=200 -> 返回成功文本。
	// 2. “/设置插件优先级 ping high” -> Atoi失败 -> 参数错误。
	return fmt.Sprintf("插件 %s 优先级已设为 %d", state.Name, state.Priority), nil
}

// OnEnable 初始化系统管理插件。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (p *implementation) OnEnable(context.Context) error {
	// >>> 数据演变示例
	// 1. 首次同步System插件 -> OnEnable -> ready。
	// 2. 重启加载 -> OnEnable -> ready。
	return nil
}

// OnDisable 拒绝系统管理插件关闭后的资源清理语义。
// @param context.Context：生命周期上下文。
// @returns nil；正常管理链路不会调用。
// ⚠️副作用说明：无。
func (p *implementation) OnDisable(context.Context) error {
	// >>> 数据演变示例
	// 1. 正常管理请求禁用admin -> Service提前拒绝 -> 不调用。
	// 2. 数据库被直接修改 -> 下次启动Manifest重新启用。
	return nil
}

// newPlugin 使用运行时依赖创建系统管理插件。
// @param runtime：Messenger 与 Management 运行依赖。
// @returns admin Plugin 或依赖缺失错误。
// ⚠️副作用说明：无。
func newPlugin(runtime plugin.Runtime) (plugin.Plugin, error) {
	// [决策理由] 管理结果必须通过 QQ 回复，Messenger 不可缺失。
	if runtime.Messenger == nil {
		return nil, fmt.Errorf("admin缺少Messenger")
	}
	// [决策理由] 管理插件必须经过统一Service，禁止直接访问数据库绕过鉴权审计。
	if runtime.Management == nil {
		return nil, fmt.Errorf("admin缺少Management")
	}
	result := &implementation{messenger: runtime.Messenger, management: runtime.Management}

	// >>> 数据演变示例
	// 1. Runtime依赖完整 -> implementation -> nil。
	// 2. Management=nil -> 返回依赖缺失错误。
	return result, nil
}

// init 注册不可关闭的系统管理插件。
// @param 无。
// @returns 无。
// ⚠️副作用说明：向全局Plugin Catalog注册admin。
func init() {
	permissions := plugin.RolePermissions{SuperAdmin: true}
	plugin.MustRegister(plugin.Registration{Manifest: plugin.Manifest{
		Name: "admin", DisplayName: "系统管理", Description: "通过QQ管理插件运行状态", Version: "1.0.0", SchemaVersion: 1, Priority: 1000, System: true,
		Features: []plugin.FeatureManifest{
			{Key: featureList, DisplayName: "插件列表", DefaultCommands: []string{"插件列表"}, DefaultPermissions: permissions},
			{Key: featureEnable, DisplayName: "启用插件", DefaultCommands: []string{"启用插件"}, DefaultPermissions: permissions},
			{Key: featureDisable, DisplayName: "禁用插件", DefaultCommands: []string{"禁用插件"}, DefaultPermissions: permissions},
			{Key: featurePriority, DisplayName: "设置插件优先级", DefaultCommands: []string{"设置插件优先级"}, DefaultPermissions: permissions},
			{Key: featureCommandList, DisplayName: "命令列表", DefaultCommands: []string{"命令列表"}, DefaultPermissions: permissions},
			{Key: featureCommandCreateGlobal, DisplayName: "新增全局命令", DefaultCommands: []string{"新增全局命令"}, DefaultPermissions: permissions},
			{Key: featureCommandCreateGroup, DisplayName: "新增群命令", DefaultCommands: []string{"新增群命令"}, DefaultPermissions: permissions},
			{Key: featureCommandRename, DisplayName: "修改命令", DefaultCommands: []string{"修改命令"}, DefaultPermissions: permissions},
			{Key: featureCommandDelete, DisplayName: "删除命令", DefaultCommands: []string{"删除命令"}, DefaultPermissions: permissions},
		},
	}, Factory: newPlugin})

	// >>> 数据演变示例
	// 1. 进程加载包 -> 注册admin System Manifest与四项功能。
	// 2. 重复admin -> MustRegister panic暴露构建错误。
}
