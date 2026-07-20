// 📌 影响范围：引用 OneBot 事件模型；定义插件生命周期契约，不读写外部状态。
package plugin

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// ErrStopPropagation 表示当前事件已处理完毕，不再传递给后续插件。
var ErrStopPropagation = errors.New("停止事件传播")

// Plugin 定义编译时集成插件必须实现的行为。
type Plugin interface {
	Name() string
	Handle(context.Context, ws.Event) error
	OnEnable(context.Context) error
	OnDisable(context.Context) error
}

type featureContextKey struct{}
type invocationContextKey struct{}

// Invocation 描述一次由Command Registry定向到插件功能的调用。
type Invocation struct {
	FeatureKey string
	Command    string
	Arguments  string
}

// WithFeature 将已匹配的稳定功能键写入插件调用上下文。
// @param ctx：原上下文；featureKey：Command Registry 匹配的功能键。
// @returns 携带功能键的新上下文。
// ⚠️副作用说明：无。
func WithFeature(ctx context.Context, featureKey string) context.Context {
	result := context.WithValue(ctx, featureContextKey{}, featureKey)

	// >>> 数据演变示例
	// 1. ctx + ping -> FeatureFromContext=ping。
	// 2. ctx + rank -> FeatureFromContext=rank。
	return result
}

// FeatureFromContext 读取当前定向路由功能键。
// @param ctx：插件调用上下文。
// @returns 功能键；未定向路由时为空。
// ⚠️副作用说明：无。
func FeatureFromContext(ctx context.Context) string {
	invocation, found := InvocationFromContext(ctx)
	// [决策理由] 新路由上下文携带更完整调用信息，应优先读取其中的功能键。
	if found {
		return invocation.FeatureKey
	}
	value, _ := ctx.Value(featureContextKey{}).(string)

	// >>> 数据演变示例
	// 1. WithFeature(ctx,ping) -> ping。
	// 2. 原始 ctx -> 空字符串。
	return value
}

// WithInvocation 将已匹配命令、功能和参数写入插件调用上下文。
// @param ctx：原上下文；invocation：Command Registry形成的调用信息。
// @returns 携带不可变Invocation值的新上下文。
// ⚠️副作用说明：无。
func WithInvocation(ctx context.Context, invocation Invocation) context.Context {
	result := context.WithValue(ctx, invocationContextKey{}, invocation)

	// >>> 数据演变示例
	// 1. hello+echo+"Hi" -> WithInvocation -> 插件读取完整调用。
	// 2. 空Arguments -> 仍保留FeatureKey与Command。
	return result
}

// InvocationFromContext 读取当前定向命令调用信息。
// @param ctx：插件调用上下文。
// @returns Invocation与是否存在；广播或旧式WithFeature上下文返回false。
// ⚠️副作用说明：无。
func InvocationFromContext(ctx context.Context) (Invocation, bool) {
	value, ok := ctx.Value(invocationContextKey{}).(Invocation)

	// >>> 数据演变示例
	// 1. WithInvocation(ctx,{hello,echo,Hi}) -> 完整值,true。
	// 2. context.Background() -> 零值,false。
	return value, ok
}

// State 表示数据库持久化的插件运行状态。
type State struct {
	Name          string
	Enabled       bool
	Priority      int
	ConfigJSON    json.RawMessage
	ConfigVersion int64
}

// StateStore 定义插件状态持久化能力。
type StateStore interface {
	Load(context.Context) ([]State, error)
	SaveEnabled(context.Context, string, bool) error
}
