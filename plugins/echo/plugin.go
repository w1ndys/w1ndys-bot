// 📌 影响范围：通过注入的 Messenger 引用回复 QQ 消息；注册 echo 插件 Manifest 和运行工厂。
package echo

import (
	"context"
	"fmt"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

var manifest = plugin.Manifest{
	Name: pluginName, DisplayName: pluginDisplayName, Description: pluginDescription,
	Priority: pluginPriority,
	Features: []plugin.FeatureManifest{{
		Key: featureEcho, DisplayName: featureDisplayName, Description: featureDescription,
		DefaultCommands: []string{defaultCommandEcho, defaultCommandEchoCN},
		DefaultPermissions: plugin.RolePermissions{
			SuperAdmin: true, GroupOwner: true, GroupAdmin: true, Member: defaultMemberAvailable,
		},
	}},
}

type implementation struct {
	messenger plugin.Messenger
}

// Name 返回插件稳定名称。
// @param 无。
// @returns echo。
// ⚠️副作用说明：无。
func (p *implementation) Name() string {
	// >>> 数据演变示例
	// 1. 新实例 -> Name -> echo。
	// 2. 启停后 -> Name -> 仍为echo。
	return pluginName
}

// Handle 将命令参数原样作为引用回复发送。
// @param ctx：包含Invocation的命令上下文；event：OneBot事件。
// @returns 非目标调用为nil，非消息或发送失败时返回错误，内容或用法回复成功后停止传播。
// ⚠️副作用说明：目标echo命令会通过NapCat发送内容或用法引用回复。
func (p *implementation) Handle(ctx context.Context, event ws.Event) error {
	invocation, found := plugin.InvocationFromContext(ctx)
	// [决策理由] 广播事件或其他插件功能不属于echo，应安静忽略。
	if !found || invocation.FeatureKey != featureEcho {
		return nil
	}
	message, ok := event.(*ws.MessageEvent)
	// [决策理由] 引用回复依赖message_id，非消息事件不能执行echo。
	if !ok {
		return fmt.Errorf("echo 收到非消息事件 %T", event)
	}
	response := invocation.Arguments
	// [决策理由] 空回声没有业务内容，应向用户回复当前实际触发词的用法而非制造错误日志。
	if invocation.Arguments == "" {
		response = fmt.Sprintf("用法：%s <要重复的内容>", invocation.Command)
	}
	_, err := p.messenger.ReplyToMessage(ctx, message, message.MessageID, response)
	// [决策理由] NapCat发送失败必须带插件上下文返回统一日志链路。
	if err != nil {
		return fmt.Errorf("发送echo回复: %w", err)
	}

	// >>> 数据演变示例
	// 1. /echo Hello World -> Invocation.Arguments="Hello World" -> 引用回复同一文本。
	// 2. /echo -> Arguments为空 -> 引用回复当前命令用法并停止传播。
	return plugin.ErrStopPropagation
}

// OnEnable 初始化echo插件生命周期。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (p *implementation) OnEnable(context.Context) error {
	// >>> 数据演变示例
	// 1. WebUI启用echo -> OnEnable -> ready。
	// 2. 重启恢复enabled -> 再次OnEnable -> 幂等ready。
	return nil
}

// OnDisable 释放echo插件生命周期资源。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (p *implementation) OnDisable(context.Context) error {
	// >>> 数据演变示例
	// 1. WebUI禁用echo -> OnDisable -> disabled。
	// 2. 无后台资源 -> 重复调用 -> nil。
	return nil
}

// newPlugin 使用运行时依赖创建echo实例。
// @param runtime：主程序注入的插件运行环境。
// @returns echo插件或依赖缺失错误。
// ⚠️副作用说明：无。
func newPlugin(runtime plugin.Runtime) (plugin.Plugin, error) {
	// [决策理由] echo必须具备引用回复能力，缺少Messenger时应阻止实例启动。
	if runtime.Messenger == nil {
		return nil, fmt.Errorf("%s 缺少 Messenger", pluginName)
	}
	result := &implementation{messenger: runtime.Messenger}

	// >>> 数据演变示例
	// 1. Runtime{Messenger} -> echo implementation -> nil错误。
	// 2. Runtime{} -> nil插件 -> 依赖缺失错误。
	return result, nil
}

// init 注册正式内置echo插件。
// @param 无。
// @returns 无。
// ⚠️副作用说明：向全局Plugin Catalog注册echo；注册错误会panic。
func init() {
	plugin.MustRegister(plugin.Registration{Manifest: manifest, Factory: newPlugin})

	// >>> 数据演变示例
	// 1. cmd/bot导入plugins/echo -> Catalog注册echo Manifest与Factory。
	// 2. 重复echo名称 -> MustRegister panic -> 构建期暴露冲突。
}
