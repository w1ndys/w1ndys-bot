// 📌 影响范围：提供目标插件架构的纯内存群命令匹配、运行门禁、身份授权和 Handler 分发；不访问数据库、旧 Manager 或旧权限解析器。
package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

var (
	ErrGroupMessageRequired = errors.New("目标插件命令仅接受群消息")
	ErrPluginNotReady       = errors.New("插件运行状态未就绪")
	ErrPluginGroupDisabled  = errors.New("插件在当前群未启用")
	ErrCommandUnauthorized  = errors.New("当前身份无权执行命令")
)

// Admission 代表 RuntimeGate 已原子登记的一次在途插件调用。
type Admission interface {
	GroupEnabled(groupID int64) bool
	Release()
}

// RuntimeGate 原子准入 Ready 插件，并由 Admission 保护群门禁检查和禁用排空。
type RuntimeGate interface {
	Admit(pluginKey string) (Admission, bool)
}

// IdentityResolver 将群消息发送者解析为代码授权使用的封闭身份。
type IdentityResolver interface {
	Resolve(ctx context.Context, message *ws.MessageEvent) (Role, error)
}

type commandRoute struct {
	pluginKey string
	command   CommandSpec
	trigger   string
}

// Dispatcher 执行目标插件架构唯一的群命令分发链。
type Dispatcher struct {
	routes   []commandRoute
	gate     RuntimeGate
	identity IdentityResolver
}

// NewDispatcher 从只读规格目录构建纯内存命令路由。
// @param catalog：已完成冲突校验的规格目录；gate：原子运行准入；identity：群身份解析器。
// @returns 可并发只读使用的 Dispatcher，或缺少依赖及异常目录错误。
// ⚠️副作用说明：复制命令规格和标准化触发词；不访问数据库或修改 Catalog。
func NewDispatcher(catalog *SpecCatalog, gate RuntimeGate, identity IdentityResolver) (*Dispatcher, error) {
	// [决策理由] nil 目录无法区分尚未装配与合法空目录，启动时应显式失败。
	if catalog == nil {
		return nil, errors.New("Dispatcher 规格目录不能为空")
	}
	// [决策理由] 缺少运行门禁会让命令绕过 Ready 和群开关，禁止降级运行。
	if gate == nil {
		return nil, errors.New("Dispatcher RuntimeGate 不能为空")
	}
	// [决策理由] 缺少身份解析器会让代码 RoleSet 无法 fail-closed 授权。
	if identity == nil {
		return nil, errors.New("Dispatcher IdentityResolver 不能为空")
	}
	routes := make([]commandRoute, 0)
	for _, spec := range catalog.Specs() {
		for _, command := range spec.Commands {
			for _, declaredTrigger := range command.Triggers {
				trigger, err := commandregistry.Normalize(declaredTrigger, "")
				// [决策理由] Catalog 理应已验证触发词，此处防御未来非标准目录实现破坏分发边界。
				if err != nil {
					return nil, fmt.Errorf("构建命令路由 %s.%s 失败: %w", spec.Key, command.Key, err)
				}
				routes = append(routes, commandRoute{pluginKey: spec.Key, command: command, trigger: trigger})
			}
		}
	}
	result := &Dispatcher{routes: routes, gate: gate, identity: identity}

	// >>> 数据演变示例
	// 1. Catalog{echo:[" ECHO "]} -> 标准化路由echo -> 可分发实例。
	// 2. nil RuntimeGate -> 依赖校验 -> 返回错误且不构建实例。
	return result, nil
}

// Dispatch 匹配并执行一个已经通过目标门禁与代码身份授权的群命令。
// @param ctx：调用生命周期上下文；event：待处理 OneBot 消息事件。
// @returns 是否命中代码触发词，以及拒绝、解析或 Handler 错误。
// ⚠️副作用说明：命中后可能登记并释放 admission、解析身份并调用插件 Handler；panic 会原样传播但 admission 仍释放。
func (d *Dispatcher) Dispatch(ctx context.Context, event *ws.MessageEvent) (bool, error) {
	// [决策理由] nil 或私聊事件必须在匹配和群门禁前拒绝，避免普通插件绕过群开关。
	if event == nil || event.MessageType != "group" || event.GroupID == 0 {
		return false, ErrGroupMessageRequired
	}
	normalized := normalizeDispatchInput(event.RawMessage)
	// [决策理由] 空白消息没有可匹配的代码触发词，应留给后续观察链。
	if normalized == "" {
		return false, nil
	}
	route, found := d.match(normalized)
	// [决策理由] 未命中代码声明触发词时不得访问任何运行门禁或身份服务。
	if !found {
		return false, nil
	}
	admission, admitted := d.gate.Admit(route.pluginKey)
	// [决策理由] 只有 Ready 状态可原子登记在途调用，异常 nil admission 也必须 fail-closed。
	if !admitted || admission == nil {
		return true, ErrPluginNotReady
	}
	defer admission.Release()
	// [决策理由] 群状态必须在已取得的 admission 内检查，消除预检与在途登记之间的窗口。
	if !admission.GroupEnabled(event.GroupID) {
		return true, ErrPluginGroupDisabled
	}
	role, err := d.identity.Resolve(ctx, event)
	// [决策理由] 身份未知或解析失败必须停止授权，不能回退到默认身份。
	if err != nil {
		return true, fmt.Errorf("解析命令 %s.%s 身份失败: %w", route.pluginKey, route.command.Key, err)
	}
	// [决策理由] 超级管理员等身份均须由命令 RoleSet 显式声明，禁止隐式层级继承或绕过。
	if !route.command.AllowedRoles.Contains(role) {
		return true, ErrCommandUnauthorized
	}
	arguments := commandregistry.ExtractArguments(event.RawMessage, "", route.trigger)
	err = route.command.Handler(CommandContext{Context: ctx, Message: event, Trigger: route.trigger, Arguments: arguments, Role: role})

	// >>> 数据演变示例
	// 1. 群消息"ECHO  Hello" -> 最长匹配echo -> Ready -> 群开启 -> member允许 -> Handler(arguments="Hello")。
	// 2. 群消息"admin run" -> 命中 -> admission -> 群关闭 -> Release -> ErrPluginGroupDisabled。
	return true, err
}

// match 按完整字段边界选择最长的标准化代码触发词。
// @param normalized：Normalize 处理后的消息全文。
// @returns 最长匹配路由及是否命中。
// ⚠️副作用说明：无；只读取构建后的不可变路由。
func (d *Dispatcher) match(normalized string) (commandRoute, bool) {
	var matched commandRoute
	found := false
	for _, route := range d.routes {
		wholeCommand := normalized == route.trigger
		withArguments := strings.HasPrefix(normalized, route.trigger+" ")
		// [决策理由] 触发词必须覆盖完整字段，且更长声明优先于其前缀命令。
		if (wholeCommand || withArguments) && (!found || len(route.trigger) > len(matched.trigger)) {
			matched = route
			found = true
		}
	}

	// >>> 数据演变示例
	// 1. routes=["查","查 状态"]+"查 状态 now" -> 最长路由"查 状态"。
	// 2. route="echo"+"echoes text" -> 无字段边界 -> 未匹配。
	return matched, found
}

// normalizeDispatchInput 标准化待匹配消息但不限制业务参数长度。
// @param input：包含触发词和可选业务参数的原始消息。
// @returns 去首尾、合并空白并转小写的完整消息；空白输入返回空字符串。
// ⚠️副作用说明：无；不套用旧命令字段的 128 字符存储限制。
func normalizeDispatchInput(input string) string {
	result := strings.ToLower(strings.Join(strings.Fields(input), " "))

	// >>> 数据演变示例
	// 1. " ECHO   Keep CASE " -> "echo keep case"。
	// 2. "echo "+200字符参数 -> 保留完整参数用于匹配，不因旧命令字段上限失败。
	return result
}
