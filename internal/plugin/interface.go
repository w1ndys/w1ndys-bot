// 📌 影响范围：引用 OneBot 事件模型；定义插件生命周期契约，不读写外部状态。
package plugin

import (
	"context"
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
	value, _ := ctx.Value(featureContextKey{}).(string)

	// >>> 数据演变示例
	// 1. WithFeature(ctx,ping) -> ping。
	// 2. 原始 ctx -> 空字符串。
	return value
}

// State 表示数据库持久化的插件运行状态。
type State struct {
	Name     string
	Enabled  bool
	Priority int
}

// StateStore 定义插件状态持久化能力。
type StateStore interface {
	Load(context.Context) ([]State, error)
	SaveEnabled(context.Context, string, bool) error
}
