// 📌 影响范围：调用内存测试插件和状态仓库，不访问外部数据库或网络。
package plugin

import (
	"context"
	"reflect"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakePlugin struct {
	name        string
	calls       *[]string
	stop        bool
	enableHits  int
	disableHits int
}

// Name 返回测试插件名。
// @param 无。
// @returns 测试配置的名称。
// ⚠️副作用说明：无。
func (p *fakePlugin) Name() string {
	// >>> 数据演变示例
	// 1. name=A -> Name -> A。
	// 2. name="" -> Name -> 空字符串。
	return p.name
}

// Handle 记录测试插件执行顺序。
// @param context.Context：测试上下文；ws.MessageEvent：测试事件。
// @returns stop=true 时返回 ErrStopPropagation，否则返回 nil。
// ⚠️副作用说明：向 calls 追加插件名。
func (p *fakePlugin) Handle(context.Context, ws.Event) error {
	*p.calls = append(*p.calls, p.name)
	// [决策理由] stop 用于模拟插件消费事件并终止后续传播。
	if p.stop {
		return ErrStopPropagation
	}

	// >>> 数据演变示例
	// 1. calls=[] + name=A -> calls=[A] -> nil。
	// 2. calls=[] + name=A,stop=true -> calls=[A] -> ErrStopPropagation。
	return nil
}

// OnEnable 记录启用回调次数。
// @param context.Context：测试上下文。
// @returns nil。
// ⚠️副作用说明：enableHits 加一。
func (p *fakePlugin) OnEnable(context.Context) error {
	p.enableHits++

	// >>> 数据演变示例
	// 1. enableHits=0 -> OnEnable -> 1,nil。
	// 2. enableHits=1 -> OnEnable -> 2,nil。
	return nil
}

// OnDisable 记录禁用回调次数。
// @param context.Context：测试上下文。
// @returns nil。
// ⚠️副作用说明：disableHits 加一。
func (p *fakePlugin) OnDisable(context.Context) error {
	p.disableHits++

	// >>> 数据演变示例
	// 1. disableHits=0 -> OnDisable -> 1,nil。
	// 2. disableHits=1 -> OnDisable -> 2,nil。
	return nil
}

type fakeStore struct {
	states []State
	saved  map[string]bool
	loads  int
}

// Load 返回测试状态快照。
// @param context.Context：测试上下文。
// @returns 预设状态与 nil。
// ⚠️副作用说明：loads 加一。
func (s *fakeStore) Load(context.Context) ([]State, error) {
	s.loads++

	// >>> 数据演变示例
	// 1. states=[A] -> loads 0→1 -> 返回 [A],nil。
	// 2. states=[] -> loads 1→2 -> 返回 [],nil。
	return s.states, nil
}

// SaveEnabled 记录测试状态写入。
// @param context.Context：测试上下文；name：插件名；enabled：目标状态。
// @returns nil。
// ⚠️副作用说明：修改 saved 映射。
func (s *fakeStore) SaveEnabled(_ context.Context, name string, enabled bool) error {
	s.saved[name] = enabled

	// >>> 数据演变示例
	// 1. saved={} + A=true -> saved[A]=true -> nil。
	// 2. saved[A]=true + A=false -> saved[A]=false -> nil。
	return nil
}

// TestManagerRoutesByPriorityAndStopsPropagation 验证优先级与传播中止。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存插件并修改调用记录。
func TestManagerRoutesByPriorityAndStopsPropagation(t *testing.T) {
	calls := make([]string, 0)
	store := &fakeStore{states: []State{{Name: "low", Enabled: true, Priority: 1}, {Name: "high", Enabled: true, Priority: 10}}, saved: make(map[string]bool)}
	manager := NewManager(store)
	low := &fakePlugin{name: "low", calls: &calls}
	high := &fakePlugin{name: "high", calls: &calls, stop: true}
	// [决策理由] 注册失败会使后续路由断言失去意义，应立即终止测试。
	if err := manager.Register(low); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 两个插件是验证优先级与中止传播的最小集合。
	if err := manager.Register(high); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 必须先加载优先级和启用状态才能验证数据库驱动路由。
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// [决策理由] Handle 返回错误表示传播中止未按成功消费处理。
	if err := manager.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}}); err != nil {
		t.Fatal(err)
	}
	// [决策理由] high 应先执行并阻止 low，精确顺序是核心行为。
	if !reflect.DeepEqual(calls, []string{"high"}) {
		t.Fatalf("调用顺序 = %v，期望 [high]", calls)
	}

	// >>> 数据演变示例
	// 1. low=1,high=10 -> 排序 [high,low] -> high 中止 -> calls=[high]。
	// 2. high 返回 ErrStopPropagation -> Manager 返回 nil -> low 不执行。
}

// TestManagerSetEnabledAndRejectsDuplicate 验证热开关、生命周期和重名检查。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改内存插件、仓库和管理器状态。
func TestManagerSetEnabledAndRejectsDuplicate(t *testing.T) {
	calls := make([]string, 0)
	store := &fakeStore{saved: make(map[string]bool)}
	manager := NewManager(store)
	candidate := &fakePlugin{name: "ping", calls: &calls}
	// [决策理由] 首次注册必须成功才能测试后续状态迁移。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 重名必须被拒绝，防止数据库状态映射到多个实现。
	if err := manager.Register(candidate); err == nil {
		t.Fatal("重复注册未返回错误")
	}
	// [决策理由] 热启用应同时触发生命周期和持久化。
	if err := manager.SetEnabled(context.Background(), "ping", true); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 回调次数和保存值共同证明状态迁移完整。
	if candidate.enableHits != 1 || !store.saved["ping"] {
		t.Fatalf("enableHits=%d saved=%v", candidate.enableHits, store.saved)
	}

	// >>> 数据演变示例
	// 1. ping disabled -> SetEnabled(true) -> OnEnable 1 次 -> saved=true。
	// 2. ping 已注册 -> 再次 Register -> 返回重复错误。
}

// TestManagerWithoutPluginsSkipsStore 验证空管理器不访问数据库。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：读取并检查 fakeStore.loads。
func TestManagerWithoutPluginsSkipsStore(t *testing.T) {
	store := &fakeStore{saved: make(map[string]bool)}
	manager := NewManager(store)
	// [决策理由] 空插件集在迁移表尚不存在时仍需安全启动。
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// [决策理由] loads=0 证明 Manager 没有触发状态表查询。
	if store.loads != 0 {
		t.Fatalf("Load 调用次数 = %d，期望 0", store.loads)
	}

	// >>> 数据演变示例
	// 1. entries=[] + store -> Load -> 不访问 store -> nil。
	// 2. store.loads=0 -> Load 完成 -> 仍为 0。
}
