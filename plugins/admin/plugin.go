// 📌 影响范围：提供 QQ 应急管理入口；通过 RuntimeService 查询和切换目标插件，并引用回复 QQ 消息。
package admin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type EmergencyHandler struct {
	messenger plugin.Messenger
	runtimes  plugin.RuntimeManagement
}

func NewEmergencyHandler(messenger plugin.Messenger, runtimes plugin.RuntimeManagement) (*EmergencyHandler, error) {
	if messenger == nil || runtimes == nil {
		return nil, errors.New("QQ 应急管理依赖不能为空")
	}
	return &EmergencyHandler{messenger: messenger, runtimes: runtimes}, nil
}

// Handle 执行最高管理员的固定 QQ 应急命令。
// @returns 是否命中应急命令及处理错误。
// ⚠️副作用说明：可能修改插件状态、刷新运行时并引用回复 QQ 消息。
func (p *EmergencyHandler) Handle(ctx context.Context, message *ws.MessageEvent, prefix string) (bool, error) {
	if message == nil {
		return false, nil
	}
	feature, matched := emergencyFeature(message.RawMessage, prefix)
	if !matched {
		return false, nil
	}
	actor := management.Actor{ID: strconv.FormatInt(message.UserID, 10), Role: "super_admin", Channel: management.ChannelQQ, RequestID: strconv.FormatInt(message.MessageID, 10)}
	var response string
	var err error
	switch feature {
	case featureList:
		response, err = p.list(ctx, actor)
	case featureEnable:
		response, err = p.setEnabled(ctx, actor, message.RawMessage, true)
	case featureDisable:
		response, err = p.setEnabled(ctx, actor, message.RawMessage, false)
	case featureStatus:
		response, err = p.status(ctx, actor, message.RawMessage)
	case featureGroupEnable:
		response, err = p.setGroupEnabled(ctx, actor, message, true)
	case featureGroupDisable:
		response, err = p.setGroupEnabled(ctx, actor, message, false)
	default:
		return false, nil
	}
	// [决策理由] 管理错误应转换为用户可见结果，同时仍由回复错误进入服务日志。
	if err != nil {
		response = runtimeErrorMessage(err)
	}
	_, replyErr := p.messenger.ReplyToMessage(ctx, message, message.MessageID, response)
	// [决策理由] NapCat 回复失败必须带管理上下文返回日志链路。
	if replyErr != nil {
		return true, fmt.Errorf("回复管理结果: %w", replyErr)
	}

	// >>> 数据演变示例
	// 1. /启用插件 ping -> AdminService启用 -> 引用回复成功。
	// 2. 非最高管理员 -> ErrForbidden -> 引用回复操作失败。
	return true, nil
}

func emergencyFeature(raw, prefix string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", false
	}
	commands := map[string]string{
		featureListCommand: featureList, featureEnableCommand: featureEnable, featureDisableCommand: featureDisable,
		featureStatusCommand: featureStatus, featureGroupEnableCommand: featureGroupEnable, featureGroupDisableCommand: featureGroupDisable,
	}
	feature, ok := commands[strings.TrimPrefix(fields[0], prefix)]
	return feature, ok && strings.HasPrefix(fields[0], prefix)
}

func (p *EmergencyHandler) status(ctx context.Context, actor management.Actor, raw string) (string, error) {
	name, err := commandPluginKey(raw, "插件状态")
	if err != nil {
		return "", err
	}
	state, err := p.runtimes.Get(ctx, actor, name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("插件 %s：意图%v，实际 %s，在途 %d（版本 %d）", state.PluginKey, map[bool]string{true: "启用", false: "关闭"}[state.DesiredEnabled], state.Status, state.InFlight, state.Version), nil
}

func (p *EmergencyHandler) setGroupEnabled(ctx context.Context, actor management.Actor, message *ws.MessageEvent, enabled bool) (string, error) {
	if message.MessageType != "group" || message.GroupID <= 0 {
		return "", errors.New("当前群操作只能在群聊中使用")
	}
	name, err := commandPluginKey(message.RawMessage, map[bool]string{true: "启用当前群插件", false: "禁用当前群插件"}[enabled])
	if err != nil {
		return "", err
	}
	current, err := p.runtimes.Get(ctx, actor, name)
	if err != nil {
		return "", err
	}
	var version int64
	for _, group := range current.Groups {
		if group.GroupID == message.GroupID {
			version = group.Version
			break
		}
	}
	state, err := p.runtimes.SetGroupEnabled(ctx, actor, name, message.GroupID, enabled, version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("插件 %s 在群 %d 已%s（全局意图%s，实际 %s）", state.PluginKey, message.GroupID, map[bool]string{true: "启用", false: "禁用"}[enabled], map[bool]string{true: "启用", false: "关闭"}[state.DesiredEnabled], state.Status), nil
}

func commandPluginKey(raw, command string) (string, error) {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return "", fmt.Errorf("用法：/%s <插件名>", command)
	}
	return fields[1], nil
}

func runtimeErrorMessage(err error) string {
	switch {
	case errors.Is(err, plugin.ErrRuntimePluginNotFound):
		return "操作失败：目标插件不存在"
	case errors.Is(err, plugin.ErrRuntimeStateConflict):
		return "操作失败：状态已变化，请重试"
	case errors.Is(err, plugin.ErrInvalidRuntimeGroupID):
		return "操作失败：群号无效"
	default:
		if err != nil && (strings.HasPrefix(err.Error(), "用法：") || err.Error() == "当前群操作只能在群聊中使用") {
			return "操作失败：" + err.Error()
		}
		return "操作失败，请稍后重试"
	}
}

// list 格式化插件运行状态列表。
// @param ctx：查询生命周期；actor：操作者。
// @returns 可发送的状态文本或查询、授权错误。
// ⚠️副作用说明：读取管理 Repository。
func (p *EmergencyHandler) list(ctx context.Context, actor management.Actor) (string, error) {
	states, err := p.runtimes.List(ctx, actor)
	// [决策理由] 查询失败时没有可信列表可回复。
	if err != nil {
		return "", err
	}
	sort.Slice(states, func(i int, j int) bool {
		// [决策理由] 目录返回顺序不应影响 QQ 文本，按稳定 Key 排序。
		return states[i].PluginKey < states[j].PluginKey
	})
	lines := []string{"插件列表："}
	for _, state := range states {
		status := "关闭"
		// [决策理由] 启用状态需要转换为适合 QQ 阅读的短文本。
		if state.DesiredEnabled {
			status = "启用"
		}
		lines = append(lines, fmt.Sprintf("- %s：意图%s，实际 %s（版本 %d）", state.PluginKey, status, state.Status, state.Version))
	}

	// >>> 数据演变示例
	// 1. [ping:true:100] -> “ping：启用（优先级100）”。
	// 2. [] -> 仅返回“插件列表：”。
	return strings.Join(lines, "\n"), nil
}

// setEnabled 从消息末尾提取插件名并修改启用状态。
// @param ctx：操作生命周期；actor：操作者；raw：原始消息；enabled：目标状态。
// @returns 成功文本或参数、管理错误。
// ⚠️副作用说明：调用 RuntimeService 更新状态、审计并驱动生命周期。
func (p *EmergencyHandler) setEnabled(ctx context.Context, actor management.Actor, raw string, enabled bool) (string, error) {
	name, err := commandPluginKey(raw, map[bool]string{true: "启用插件", false: "禁用插件"}[enabled])
	if err != nil {
		return "", err
	}
	current, err := p.runtimes.Get(ctx, actor, name)
	if err != nil {
		return "", err
	}
	state, err := p.runtimes.SetGlobalEnabled(ctx, actor, name, enabled, current.Version)
	// [决策理由] 管理服务负责鉴权和事务，失败不得输出成功状态。
	if err != nil {
		return "", err
	}

	// >>> 数据演变示例
	// 1. “/启用插件 ping” -> fields[1]=ping -> enabled=true -> 成功文本。
	// 2. “/禁用插件” -> 参数缺失 -> 用法错误。
	return fmt.Sprintf("插件 %s 已%s，实际状态 %s（版本 %d）", state.PluginKey, map[bool]string{true: "启用", false: "禁用"}[enabled], state.Status, state.Version), nil
}
