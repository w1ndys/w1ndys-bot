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
		},
	}, Factory: newPlugin})

	// >>> 数据演变示例
	// 1. 进程加载包 -> 注册admin System Manifest与四项功能。
	// 2. 重复admin -> MustRegister panic暴露构建错误。
}
