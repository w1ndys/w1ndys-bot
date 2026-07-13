// 📌 影响范围：通过注入的 Messenger 回复 QQ 消息；注册 ping 插件 Manifest 和运行工厂。
package ping

import (
	"context"
	"fmt"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

const featurePing = "ping"

type implementation struct {
	messenger plugin.Messenger
}

// Name 返回插件稳定名称。
// @param 无。
// @returns ping。
// ⚠️副作用说明：无。
func (p *implementation) Name() string {
	// >>> 数据演变示例
	// 1. implementation -> Name -> ping。
	// 2. 任意运行状态 -> Name 保持 ping。
	return "ping"
}

// Handle 处理已由 Command Registry 定向的 ping 功能。
// @param ctx：包含 feature_key 的路由上下文；event：OneBot 事件。
// @returns 回复或事件类型错误。
// ⚠️副作用说明：匹配 ping 功能时通过 NapCat 回复 pong。
func (p *implementation) Handle(ctx context.Context, event ws.Event) error {
	// [决策理由] 广播事件不带功能键，ping 只响应命令定向路由。
	if plugin.FeatureFromContext(ctx) != featurePing {
		return nil
	}
	message, ok := event.(*ws.MessageEvent)
	// [决策理由] ping 功能只支持消息事件。
	if !ok {
		return fmt.Errorf("ping 收到非消息事件 %T", event)
	}
	_, err := p.messenger.ReplyToMessage(ctx, message, message.MessageID, "pong")
	// [决策理由] 回复失败必须返回给事件链路记录，不可静默吞掉。
	if err != nil {
		return fmt.Errorf("回复 pong: %w", err)
	}

	// >>> 数据演变示例
	// 1. feature=ping + group message -> ReplyToMessage(message_id,pong) -> nil。
	// 2. feature="" + message -> 不回复 -> nil。
	return plugin.ErrStopPropagation
}

// OnEnable 在插件启用时执行生命周期初始化。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (p *implementation) OnEnable(context.Context) error {
	// >>> 数据演变示例
	// 1. disabled -> OnEnable -> ready。
	// 2. 再次启动加载 enabled -> OnEnable -> ready。
	return nil
}

// OnDisable 在插件禁用时释放资源。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：无。
func (p *implementation) OnDisable(context.Context) error {
	// >>> 数据演变示例
	// 1. enabled -> OnDisable -> disabled。
	// 2. 无资源 -> OnDisable -> nil。
	return nil
}

// newPlugin 使用运行时依赖创建 ping 插件。
// @param runtime：包含 Messenger 的插件运行环境。
// @returns ping Plugin 或依赖缺失错误。
// ⚠️副作用说明：无。
func newPlugin(runtime plugin.Runtime) (plugin.Plugin, error) {
	// [决策理由] ping 必须具备回复能力才能注册为可运行实例。
	if runtime.Messenger == nil {
		return nil, fmt.Errorf("ping 缺少 Messenger")
	}
	result := &implementation{messenger: runtime.Messenger}

	// >>> 数据演变示例
	// 1. Runtime{Messenger} -> implementation -> nil error。
	// 2. Runtime{} -> 返回依赖缺失错误。
	return result, nil
}

// init 注册 ping 插件元数据和运行工厂。
// @param 无。
// @returns 无。
// ⚠️副作用说明：向全局 Plugin Catalog 注册 ping。
func init() {
	plugin.MustRegister(plugin.Registration{
		Manifest: plugin.Manifest{
			Name: "ping", DisplayName: "Ping 测试", Description: "验证完整消息与 Action 链路",
			Version: "1.0.0", SchemaVersion: 1, Priority: 100,
			Features: []plugin.FeatureManifest{{
				Key: featurePing, DisplayName: "Ping", Description: "回复 pong",
				DefaultCommands:    []string{"ping"},
				DefaultPermissions: plugin.RolePermissions{SuperAdmin: true, GroupOwner: true, GroupAdmin: true, Member: true},
			}},
		},
		Factory: newPlugin,
	})

	// >>> 数据演变示例
	// 1. 进程加载包 -> Catalog 注册 ping Manifest+Factory。
	// 2. 重复插件名 -> MustRegister panic 暴露构建错误。
}
