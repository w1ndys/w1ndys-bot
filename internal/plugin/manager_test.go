// 📌 影响范围：调用内存测试插件和状态仓库，不访问外部数据库或网络。
package plugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakePlugin struct {
	callMu            sync.Mutex
	name              string
	calls             *[]string
	stop              bool
	enableHits        int
	disableHits       int
	enableErr         error
	disableErr        error
	onEnable          func()
	handleStart       chan struct{}
	handleDone        chan struct{}
	disableContextErr error
	onHandle          func(context.Context)
	handleEntered     chan struct{}
	handleGate        chan struct{}
	panicHandle       bool
	panicEnable       bool
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
func (p *fakePlugin) Handle(ctx context.Context, _ ws.Event) error {
	p.callMu.Lock()
	*p.calls = append(*p.calls, p.name)
	p.callMu.Unlock()
	// [决策理由] 可选钩子用于验证 Handle 内同步管理操作受到死锁防护。
	if p.onHandle != nil {
		p.onHandle(ctx)
	}
	// [决策理由] 可选通道用于构造稳定的在途处理窗口，验证禁用会等待资源使用结束。
	if p.handleStart != nil {
		close(p.handleStart)
		<-p.handleDone
	}
	// [决策理由] 计数并发测试需要允许多个 Handle 分别报告进入并等待同一个释放信号。
	if p.handleEntered != nil {
		p.handleEntered <- struct{}{}
		<-p.handleGate
	}
	// [决策理由] panic 路径用于验证 Manager 的 defer 仍会释放在途计数。
	if p.panicHandle {
		panic("handle panic")
	}
	// [决策理由] stop 用于模拟插件消费事件并终止后续传播。
	if p.stop {
		return ErrStopPropagation
	}

	// >>> 数据演变示例
	// 1. calls=[] + name=A -> calls=[A] -> nil。
	// 2. calls=[] + name=A,stop=true -> calls=[A] -> ErrStopPropagation。
	return nil
}

// TestManagerRejectsSynchronousSelfDisable 验证插件不能在自身处理调用中同步禁用自己。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存插件路由和状态管理。
func TestManagerRejectsSynchronousSelfDisable(t *testing.T) {
	calls := make([]string, 0)
	manager := NewManager(nil)
	candidate := &fakePlugin{name: "ping", calls: &calls}
	var selfDisableErr error
	// 自禁用测试钩子。
	// @param ctx：Manager 标记当前插件名的处理上下文。
	// @returns 无。
	// ⚠️副作用说明：尝试同步禁用当前插件并记录错误。
	candidate.onHandle = func(ctx context.Context) {
		selfDisableErr = manager.SetEnabled(ctx, "ping", false)

		// >>> 数据演变示例
		// 1. handlingPlugin=ping+target=ping -> 返回自切换错误。
		// 2. 不进入在途等待 -> Handle可正常结束。
	}
	// [决策理由] 插件必须先注册并启用才能进入带身份标记的处理上下文。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 启用失败时无法验证自禁用保护。
	if err := manager.SetEnabled(context.Background(), "ping", true); err != nil {
		t.Fatal(err)
	}
	// [决策理由] Handle 必须完成，证明同步自禁用没有等待自身在途计数。
	if err := manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{}); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 自禁用请求必须返回明确错误，并保持插件启用。
	if selfDisableErr == nil || !strings.Contains(selfDisableErr.Error(), "自身 Handle") {
		t.Fatalf("self disable error = %v", selfDisableErr)
	}

	// >>> 数据演变示例
	// 1. ping.Handle→SetEnabled(ping,false) -> 拒绝 -> Handle结束。
	// 2. inFlight归零 -> ping仍enabled -> 后续路由继续可用。
}

// OnEnable 记录启用回调次数。
// @param context.Context：测试上下文。
// @returns nil。
// ⚠️副作用说明：enableHits 加一。
func (p *fakePlugin) OnEnable(context.Context) error {
	p.enableHits++
	// [决策理由] 生命周期 panic 用于验证 Manager 会隔离资源状态未知的插件。
	if p.panicEnable {
		panic("enable panic")
	}
	// [决策理由] 可选钩子用于验证生命周期回调期间重入 Manager 不会死锁。
	if p.onEnable != nil {
		p.onEnable()
	}

	// >>> 数据演变示例
	// 1. enableHits=0 -> OnEnable -> 1,nil。
	// 2. enableErr=失败 -> enableHits加一 -> 返回失败。
	return p.enableErr
}

// OnDisable 记录禁用回调次数。
// @param context.Context：测试上下文。
// @returns nil。
// ⚠️副作用说明：disableHits 加一。
func (p *fakePlugin) OnDisable(ctx context.Context) error {
	p.disableHits++
	p.disableContextErr = ctx.Err()

	// >>> 数据演变示例
	// 1. disableHits=0 -> OnDisable -> 1,nil。
	// 2. disableErr=失败 -> disableHits加一 -> 返回失败。
	return p.disableErr
}

type fakeStore struct {
	states      []State
	saved       map[string]bool
	loads       int
	saveErr     error
	saveEntered chan struct{}
	saveRelease chan struct{}
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
	// [决策理由] 可选通道用于稳定构造两个 priority-only apply 的重叠窗口。
	if s.saveEntered != nil {
		s.saveEntered <- struct{}{}
		<-s.saveRelease
	}
	// [决策理由] 预设错误用于验证持久化失败时生命周期能够反向补偿。
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved[name] = enabled

	// >>> 数据演变示例
	// 1. saved={} + A=true -> saved[A]=true -> nil。
	// 2. saved[A]=true + A=false -> saved[A]=false -> nil。
	return nil
}

// TestManagerSerializesPriorityOnlyApply 验证仅优先级更新也占用插件迁移标记。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动一个 apply goroutine，并通过通道阻塞内存 Store 保存。
func TestManagerSerializesPriorityOnlyApply(t *testing.T) {
	calls := make([]string, 0)
	store := &fakeStore{saved: make(map[string]bool), saveEntered: make(chan struct{}, 1), saveRelease: make(chan struct{})}
	manager := NewManager(store)
	candidate := &fakePlugin{name: "ping", calls: &calls}
	// [决策理由] 注册项提供稳定的当前enabled=false，使两个apply都只修改priority。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	firstResult := make(chan error, 1)
	// 异步执行第一个 priority-only apply 并阻塞在持久化阶段。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 apply 并发送结果。
	go func() {
		firstResult <- manager.apply(context.Background(), "ping", false, 10, true)

		// >>> 数据演变示例
		// 1. priority 0→10 -> transitioning=true -> 等待Store释放。
		// 2. Store完成 -> priority提交10 -> transitioning=false。
	}()
	<-store.saveEntered
	err := manager.apply(context.Background(), "ping", false, 20, true)
	// [决策理由] 第二个同插件快照不能穿过迁移窗口覆盖第一个更新。
	if err == nil || !strings.Contains(err.Error(), "正在切换") {
		t.Fatalf("concurrent apply error = %v", err)
	}
	close(store.saveRelease)
	// [决策理由] 释放持久化后第一个更新必须正常完成。
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	priority, err := manager.priority("ping")
	// [决策理由] 被拒绝的第二次更新不得覆盖首个已提交优先级。
	if err != nil || priority != 10 {
		t.Fatalf("priority/error = %d/%v", priority, err)
	}

	// >>> 数据演变示例
	// 1. apply10持有迁移标记 + apply20 -> apply20快速拒绝 -> 最终priority10。
	// 2. 若priority-only不排他 -> 两次Save交错 -> 旧新快照可能互相覆盖。
}

// TestManagerLifecycleDoesNotDeadlockOnStateReentry 验证生命周期回调重入状态管理会快速失败而非死锁。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动 goroutine 并修改内存插件状态。
func TestManagerLifecycleDoesNotDeadlockOnStateReentry(t *testing.T) {
	calls := make([]string, 0)
	manager := NewManager(nil)
	candidate := &fakePlugin{name: "ping", calls: &calls}
	// 生命周期重入测试钩子。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：定向读取 Manager 中的 ping 状态。
	candidate.onEnable = func() {
		_ = manager.SetEnabled(context.Background(), "ping", false)

		// >>> 数据演变示例
		// 1. ping切换中 -> SetEnabled(false) -> 立即返回切换状态错误。
		// 2. Manager写锁未持有 -> 回调完成 -> SetEnabled继续。
	}
	// [决策理由] 注册成功是验证生命周期重入的前提。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	// 异步执行状态切换以检测潜在死锁。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 Manager.SetEnabled 并向 done 通道发送结果。
	go func() {
		done <- manager.SetEnabled(context.Background(), "ping", true)

		// >>> 数据演变示例
		// 1. SetEnabled完成 -> error写入done -> 主测试收到。
		// 2. 发生锁死 -> 超时分支使测试失败。
	}()
	select {
	case err := <-done:
		// [决策理由] 无错误完成证明重入没有破坏状态迁移。
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("生命周期回调重入 Manager 发生死锁")
	}

	// >>> 数据演变示例
	// 1. OnEnable重入SetEnabled -> 检测transitioning -> 外层SetEnabled成功。
	// 2. 非重入全局迁移锁实现 -> 内层SetEnabled等待 -> 一秒后测试失败。
}

// TestManagerDisableWaitsForInFlightHandle 验证禁用生命周期不会与在途处理并发释放资源。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动两个 goroutine，并通过通道控制内存插件处理时序。
func TestManagerDisableWaitsForInFlightHandle(t *testing.T) {
	calls := make([]string, 0)
	store := &fakeStore{saved: make(map[string]bool)}
	manager := NewManager(store)
	candidate := &fakePlugin{name: "ping", calls: &calls, handleStart: make(chan struct{}), handleDone: make(chan struct{})}
	// [决策理由] 插件必须先注册并启用，才能构造禁用与处理并发。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 初始启用失败会使后续 Handle 无法进入在途状态。
	if err := manager.SetEnabled(context.Background(), "ping", true); err != nil {
		t.Fatal(err)
	}
	handleResult := make(chan error, 1)
	// 异步启动一次阻塞的定向处理。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 HandleNamed 并向 handleResult 发送结果。
	go func() {
		handleResult <- manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{})

		// >>> 数据演变示例
		// 1. Handle进入 -> 等待handleDone -> 返回结果。
		// 2. 禁用等待期间 -> Handle保持在途计数为1。
	}()
	<-candidate.handleStart
	disableResult := make(chan error, 1)
	// 异步禁用以验证 OnDisable 等待在途处理结束。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 SetEnabled 并向 disableResult 发送结果。
	go func() {
		disableResult <- manager.SetEnabled(context.Background(), "ping", false)

		// >>> 数据演变示例
		// 1. inFlight=1 -> SetEnabled等待 -> 尚不调用OnDisable。
		// 2. inFlight=0 -> OnDisable -> 返回结果。
	}()
	select {
	case err := <-disableResult:
		t.Fatalf("在途 Handle 结束前禁用已返回: %v", err)
	case <-time.After(20 * time.Millisecond):
		// [决策理由] 短暂未返回证明禁用正在等待，而非提前释放资源。
	}
	// [决策理由] Handle 未结束前不得调用 OnDisable。
	if candidate.disableHits != 0 {
		t.Fatalf("Handle在途时 disableHits=%d", candidate.disableHits)
	}
	close(candidate.handleDone)
	// [决策理由] 释放处理后 Handle 必须正常完成。
	if err := <-handleResult; err != nil {
		t.Fatal(err)
	}
	// [决策理由] 在途调用归零后禁用必须继续完成并执行一次清理。
	if err := <-disableResult; err != nil || candidate.disableHits != 1 {
		t.Fatalf("disable error/hits = %v/%d", err, candidate.disableHits)
	}

	// >>> 数据演变示例
	// 1. Handle inFlight=1 -> Disable等待 -> Handle结束 -> OnDisable一次。
	// 2. transitioning=true期间新路由 -> beginHandling拒绝 -> 不新增资源使用者。
}

// TestManagerCountsConcurrentHandlesBeforeDisable 验证多个并发处理均退出后才执行禁用。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动三个 goroutine 并通过通道控制并发处理与禁用时序。
func TestManagerCountsConcurrentHandlesBeforeDisable(t *testing.T) {
	calls := make([]string, 0, 2)
	manager := NewManager(nil)
	candidate := &fakePlugin{name: "ping", calls: &calls, handleEntered: make(chan struct{}, 2), handleGate: make(chan struct{})}
	// [决策理由] 注册和启用是并发 Handle 进入计数器的前提。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 禁用状态不会进入 Handle，必须先启用。
	if err := manager.SetEnabled(context.Background(), "ping", true); err != nil {
		t.Fatal(err)
	}
	handleResults := make(chan error, 2)
	for index := 0; index < 2; index++ {
		// 并发执行定向插件处理。
		// @param 无。
		// @returns 无。
		// ⚠️副作用说明：调用 HandleNamed 并向结果通道发送错误。
		go func() {
			handleResults <- manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{})

			// >>> 数据演变示例
			// 1. 第一个调用 -> inFlight 0→1 -> 等待gate。
			// 2. 第二个调用 -> inFlight 1→2 -> 等待gate。
		}()
	}
	<-candidate.handleEntered
	<-candidate.handleEntered
	disableResult := make(chan error, 1)
	// 异步禁用以观察两个在途调用的统一等待。
	// @param 无。
	// @returns 无。
	// ⚠️副作用说明：调用 SetEnabled 并发送结果。
	go func() {
		disableResult <- manager.SetEnabled(context.Background(), "ping", false)

		// >>> 数据演变示例
		// 1. inFlight=2 -> 等待idle。
		// 2. 两次finishHandling -> inFlight=0 -> 执行OnDisable。
	}()
	select {
	case err := <-disableResult:
		t.Fatalf("并发 Handle 退出前禁用已返回: %v", err)
	case <-time.After(20 * time.Millisecond):
		// [决策理由] 两个处理均阻塞时禁用必须保持等待。
	}
	close(candidate.handleGate)
	for index := 0; index < 2; index++ {
		// [决策理由] 两个 Handle 都必须无错误退出并各自释放一次计数。
		if err := <-handleResults; err != nil {
			t.Fatal(err)
		}
	}
	// [决策理由] 最后一个处理退出后禁用应成功且生命周期只执行一次。
	if err := <-disableResult; err != nil || candidate.disableHits != 1 {
		t.Fatalf("disable error/hits = %v/%d", err, candidate.disableHits)
	}

	// >>> 数据演变示例
	// 1. 两个Handle -> inFlight=2 -> 禁用等待 -> 两者退出 -> OnDisable一次。
	// 2. 排他锁保护计数 -> 不覆盖idle通道 -> 无负计数或提前清理。
}

// runPanickingHandle 调用预期 panic 的插件路由并恢复测试进程。
// @param manager：待测插件管理器；done：完成通知通道。
// @returns 无。
// ⚠️副作用说明：调用 HandleNamed，恢复 panic 后关闭 done。
func runPanickingHandle(manager *Manager, done chan struct{}) {
	defer close(done)
	defer func() {
		_ = recover()

		// >>> 数据演变示例
		// 1. Handle panic -> recover -> 测试进程继续。
		// 2. Handle正常返回 -> recover=nil -> 仍关闭done。
	}()
	_ = manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{})

	// >>> 数据演变示例
	// 1. panic插件 -> Manager.defer释放计数 -> 本函数defer恢复。
	// 2. 正常插件 -> HandleNamed返回 -> done关闭。
}

// TestManagerReleasesCountAfterHandlePanic 验证处理 panic 不会阻塞后续禁用。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：触发并恢复测试插件 panic，并执行禁用生命周期。
func TestManagerReleasesCountAfterHandlePanic(t *testing.T) {
	calls := make([]string, 0)
	manager := NewManager(nil)
	candidate := &fakePlugin{name: "ping", calls: &calls, panicHandle: true}
	// [决策理由] 注册和启用是触发 Handle panic 的前提。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 插件必须处于启用状态才能登记在途计数。
	if err := manager.SetEnabled(context.Background(), "ping", true); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go runPanickingHandle(manager, done)
	<-done
	// [决策理由] invokePlugin 的 defer 必须已将计数归零，使禁用立即完成。
	if err := manager.SetEnabled(context.Background(), "ping", false); err != nil {
		t.Fatal(err)
	}

	// >>> 数据演变示例
	// 1. inFlight=1→Handle panic→defer finish -> 0 -> 禁用成功。
	// 2. 若计数泄漏 -> SetEnabled等待 -> 测试无法完成。
}

// TestManagerQuarantinesLifecyclePanic 验证生命周期 panic 被转换并隔离。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：触发并恢复插件 OnEnable panic，修改内存隔离状态。
func TestManagerQuarantinesLifecyclePanic(t *testing.T) {
	calls := make([]string, 0)
	manager := NewManager(nil)
	candidate := &fakePlugin{name: "ping", calls: &calls, panicEnable: true}
	// [决策理由] 注册成功后才能进入生命周期切换。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	err := manager.SetEnabled(context.Background(), "ping", true)
	// [决策理由] panic 必须转为稳定错误而不能逃逸至管理入口。
	if !errors.Is(err, errLifecyclePanic) {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	// [决策理由] 生命周期资源状态未知时后续切换必须被隔离标记拒绝。
	if err := manager.SetEnabled(context.Background(), "ping", true); err == nil || !strings.Contains(err.Error(), "正在切换") {
		t.Fatalf("隔离后 SetEnabled() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. OnEnable panic -> errLifecyclePanic -> transitioning保持true。
	// 2. 再次启用 -> 快速返回正在切换 -> 不重复初始化资源。
}

// TestManagerQuarantinesLifecycleError 验证普通生命周期错误也会隔离未知资源状态。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：触发插件 OnEnable 错误并读取内存隔离状态。
func TestManagerQuarantinesLifecycleError(t *testing.T) {
	calls := make([]string, 0)
	lifecycleErr := errors.New("部分初始化失败")
	manager := NewManager(nil)
	candidate := &fakePlugin{name: "ping", calls: &calls, enableErr: lifecycleErr}
	// [决策理由] 注册成功后才能验证生命周期普通错误路径。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	err := manager.SetEnabled(context.Background(), "ping", true)
	// [决策理由] 生命周期根因必须保留，供管理入口定位失败原因。
	if !errors.Is(err, lifecycleErr) {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	// [决策理由] 普通错误也可能伴随部分资源变更，隔离后不得再次路由或切换。
	if err := manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{}); err == nil || !strings.Contains(err.Error(), "切换") {
		t.Fatalf("隔离后 HandleNamed() error = %v", err)
	}
	if err := manager.SetEnabled(context.Background(), "ping", true); err == nil || !strings.Contains(err.Error(), "正在切换") {
		t.Fatalf("隔离后 SetEnabled() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. OnEnable部分初始化后返回error -> enabled恢复false,transitioning保持true。
	// 2. 隔离后路由或再次启用 -> 快速拒绝 -> 不接触未知状态资源。
}

// TestManagerRollsBackLifecycleWhenPersistenceFails 验证保存失败后的资源补偿和错误聚合。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存插件启停回调并读取管理器状态。
func TestManagerRollsBackLifecycleWhenPersistenceFails(t *testing.T) {
	calls := make([]string, 0)
	saveErr := errors.New("数据库不可用")
	store := &fakeStore{saved: make(map[string]bool), saveErr: saveErr}
	manager := NewManager(store)
	candidate := &fakePlugin{name: "ping", calls: &calls}
	// [决策理由] 注册失败会使补偿路径无法执行。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	err := manager.SetEnabled(canceledContext, "ping", true)
	// [决策理由] 保存根因必须可被 errors.Is 识别，便于调用方分类处理。
	if !errors.Is(err, saveErr) {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	// [决策理由] 启用后保存失败必须调用禁用补偿，使资源回到原始关闭状态。
	if candidate.enableHits != 1 || candidate.disableHits != 1 {
		t.Fatalf("enableHits=%d disableHits=%d", candidate.enableHits, candidate.disableHits)
	}
	// [决策理由] 请求取消不能阻止必要的反向生命周期补偿，否则资源状态仍会分裂。
	if candidate.disableContextErr != nil {
		t.Fatalf("补偿上下文错误 = %v", candidate.disableContextErr)
	}
	err = manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{})
	// [决策理由] 补偿完成后内存状态必须仍为禁用且不残留迁移标记。
	if err == nil || !strings.Contains(err.Error(), "未启用") {
		t.Fatalf("HandleNamed() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. disabled→OnEnable→保存失败→OnDisable -> 内存仍disabled。
	// 2. 请求上下文已取消 -> WithoutCancel补偿 -> OnDisable仍获得可用上下文。
}

// TestManagerQuarantinesPluginWhenRollbackFails 验证补偿失败后插件保持隔离。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：触发内存插件生命周期错误并读取隔离状态。
func TestManagerQuarantinesPluginWhenRollbackFails(t *testing.T) {
	calls := make([]string, 0)
	store := &fakeStore{saved: make(map[string]bool), saveErr: errors.New("保存失败")}
	candidate := &fakePlugin{name: "ping", calls: &calls, disableErr: errors.New("补偿失败")}
	manager := NewManager(store)
	// [决策理由] 注册成功是触发启用、保存失败和禁用补偿链路的前提。
	if err := manager.Register(candidate); err != nil {
		t.Fatal(err)
	}
	err := manager.SetEnabled(context.Background(), "ping", true)
	// [决策理由] 聚合错误必须明确包含补偿失败，提示运维重启恢复未知资源状态。
	if err == nil || !strings.Contains(err.Error(), "回滚插件 ping 生命周期") {
		t.Fatalf("SetEnabled() error = %v", err)
	}
	// [决策理由] 隔离状态不得继续路由，也不得接受新的状态切换覆盖故障现场。
	if err := manager.SetEnabled(context.Background(), "ping", true); err == nil || !strings.Contains(err.Error(), "正在切换") {
		t.Fatalf("隔离后 SetEnabled() error = %v", err)
	}
	if err := manager.HandleNamed(context.Background(), "ping", &ws.MessageEvent{}); err == nil || !strings.Contains(err.Error(), "切换") {
		t.Fatalf("隔离后 HandleNamed() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. OnEnable成功→保存失败→OnDisable失败 -> transitioning保持true。
	// 2. 隔离插件再次切换或路由 -> 快速返回正在切换错误。
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
