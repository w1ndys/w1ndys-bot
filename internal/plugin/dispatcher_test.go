// 📌 影响范围：验证纯内存 Dispatcher 的群作用域、最长匹配、运行门禁、代码身份授权、错误与资源释放；无数据库或旧权限依赖。
package plugin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type dispatcherTestAdmission struct {
	groupEnabled bool
	groupChecks  []int64
	releases     int
}

// GroupEnabled 记录并返回测试群门禁结果。
// @param groupID：Dispatcher 请求检查的群号。
// @returns fake 配置的群启用状态。
// ⚠️副作用说明：追加一次群门禁调用记录。
func (a *dispatcherTestAdmission) GroupEnabled(groupID int64) bool {
	a.groupChecks = append(a.groupChecks, groupID)

	// >>> 数据演变示例
	// 1. enabled=true+group=100 -> checks=[100] -> true。
	// 2. enabled=false+group=200 -> checks=[200] -> false。
	return a.groupEnabled
}

// Release 记录一次 admission 释放。
// @param 无。
// @returns 无。
// ⚠️副作用说明：递增 releases 计数。
func (a *dispatcherTestAdmission) Release() {
	a.releases++

	// >>> 数据演变示例
	// 1. releases=0 -> Release -> 1。
	// 2. releases=1 -> Release -> 2。
}

type dispatcherTestGate struct {
	ready       bool
	admission   *dispatcherTestAdmission
	pluginCalls []string
}

// Admit 模拟 Ready 原子准入并记录插件 Key。
// @param pluginKey：命中路由所属插件 Key。
// @returns fake admission 与 Ready 结果。
// ⚠️副作用说明：追加一次准入调用记录。
func (g *dispatcherTestGate) Admit(pluginKey string) (Admission, bool) {
	g.pluginCalls = append(g.pluginCalls, pluginKey)
	// [决策理由] 未 Ready 时不得把测试 admission 暴露给 Dispatcher。
	if !g.ready {
		return nil, false
	}

	// >>> 数据演变示例
	// 1. ready=true+echo -> calls=[echo] -> admission,true。
	// 2. ready=false+echo -> calls=[echo] -> nil,false。
	return g.admission, true
}

type dispatcherTestIdentity struct {
	role    Role
	err     error
	calls   int
	groupID int64
	userID  int64
}

type concurrentDispatcherGate struct {
	admissions atomic.Int64
	releases   atomic.Int64
}

// Admit 为每次并发调用创建独立 admission。
// @param pluginKey：命中路由所属插件 Key。
// @returns 始终 Ready 的独立 admission。
// ⚠️副作用说明：原子递增 admissions，并让 Release 原子记录释放次数。
func (g *concurrentDispatcherGate) Admit(_ string) (Admission, bool) {
	g.admissions.Add(1)
	result := &concurrentDispatcherAdmission{releases: &g.releases}

	// >>> 数据演变示例
	// 1. admissions=0 -> Admit -> 1,独立admission。
	// 2. 两个并发Admit -> admissions=2 -> 两个不共享状态的admission。
	return result, true
}

type concurrentDispatcherAdmission struct {
	releases *atomic.Int64
}

// GroupEnabled 为并发测试开放所有有效群。
// @param groupID：待检查群号。
// @returns 群号大于零时为 true。
// ⚠️副作用说明：无。
func (a *concurrentDispatcherAdmission) GroupEnabled(groupID int64) bool {
	result := groupID > 0

	// >>> 数据演变示例
	// 1. group=100 -> true。
	// 2. group=0 -> false。
	return result
}

// Release 原子记录并发 admission 释放。
// @param 无。
// @returns 无。
// ⚠️副作用说明：递增共享 releases 原子计数。
func (a *concurrentDispatcherAdmission) Release() {
	a.releases.Add(1)

	// >>> 数据演变示例
	// 1. releases=0 -> Release -> 1。
	// 2. 两次并发Release -> releases=2。
}

type concurrentDispatcherIdentity struct{}

// Resolve 为并发测试返回群成员身份。
// @param ctx：调用上下文；groupID：群号；userID：发送者 QQ。
// @returns 固定群成员身份和 nil。
// ⚠️副作用说明：无。
func (concurrentDispatcherIdentity) Resolve(context.Context, *ws.MessageEvent) (Role, error) {
	// >>> 数据演变示例
	// 1. group=100,user=1 -> group_member,nil。
	// 2. group=200,user=2 -> group_member,nil。
	return RoleGroupMember, nil
}

// Resolve 模拟群身份解析并记录可信群与发送者标识。
// @param ctx：Dispatcher 调用上下文；message：已通过群门禁的消息事件。
// @returns fake 身份或错误。
// ⚠️副作用说明：记录调用次数及最后的群号、用户号。
func (r *dispatcherTestIdentity) Resolve(_ context.Context, message *ws.MessageEvent) (Role, error) {
	r.calls++
	r.groupID = message.GroupID
	r.userID = message.UserID

	// >>> 数据演变示例
	// 1. member+group=100+user=200 -> 记录标识 -> member,nil。
	// 2. err=lookup failed -> 记录调用 -> 空身份,error。
	return r.role, r.err
}

// newDispatcherTestSubject 构建单命令 Dispatcher 及可观测 fake。
// @param handler：测试 Handler；roles：代码允许身份；triggers：代码触发词。
// @returns Dispatcher、运行门禁、身份解析 fake 和构建错误。
// ⚠️副作用说明：分配纯内存 Catalog、fake 和 Dispatcher。
func newDispatcherTestSubject(handler CommandHandler, roles RoleSet, triggers ...string) (*Dispatcher, *dispatcherTestGate, *dispatcherTestIdentity, error) {
	spec := validPluginSpec("dispatcher_test", triggers[0])
	spec.Commands[0].Triggers = append([]string(nil), triggers...)
	spec.Commands[0].AllowedRoles = roles
	spec.Commands[0].Handler = handler
	catalog, err := NewSpecCatalog([]PluginSpec{spec})
	// [决策理由] Catalog 失败时不能继续构造依赖，以免测试隐藏规格错误。
	if err != nil {
		return nil, nil, nil, err
	}
	admission := &dispatcherTestAdmission{groupEnabled: true}
	gate := &dispatcherTestGate{ready: true, admission: admission}
	identity := &dispatcherTestIdentity{role: RoleGroupMember}
	dispatcher, err := NewDispatcher(catalog, gate, identity)

	// >>> 数据演变示例
	// 1. trigger=echo+member -> 合法Catalog -> Ready fake -> Dispatcher。
	// 2. trigger为空白 -> Catalog校验失败 -> 返回错误。
	return dispatcher, gate, identity, err
}

// TestDispatcherRejectsPrivateAndUnmatchedMessages 验证私聊前置拒绝及未匹配消息不访问依赖。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用纯内存 Dispatcher。
func TestDispatcherRejectsPrivateAndUnmatchedMessages(t *testing.T) {
	dispatcher, gate, identity, err := newDispatcherTestSubject(func(CommandContext) error { return nil }, Roles(RoleGroupMember), "echo")
	// [决策理由] 成功构建是路由行为断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	matched, err := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "private", RawMessage: "echo"})
	// [决策理由] 私聊必须在代码触发词和群门禁前被明确拒绝。
	if matched || !errors.Is(err, ErrGroupMessageRequired) {
		t.Fatalf("private Dispatch() = %t,%v", matched, err)
	}
	matched, err = dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, RawMessage: "echoes text"})
	// [决策理由] 字段前缀不是完整触发词，必须作为未匹配处理。
	if matched || err != nil {
		t.Fatalf("unmatched Dispatch() = %t,%v", matched, err)
	}
	// [决策理由] 私聊和未匹配消息均不得探测插件状态或发送者身份。
	if len(gate.pluginCalls) != 0 || identity.calls != 0 {
		t.Fatalf("dependencies called: gate=%v identity=%d", gate.pluginCalls, identity.calls)
	}

	// >>> 数据演变示例
	// 1. private+echo -> 群作用域拒绝 -> gate未调用。
	// 2. group+echoes -> 字段边界不匹配 -> identity未调用。
}

// TestDispatcherUsesLongestTriggerAndExtractsArguments 验证大小写、空白、最长触发词和参数保真。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用 Handler 并捕获命令上下文。
func TestDispatcherUsesLongestTriggerAndExtractsArguments(t *testing.T) {
	var received CommandContext
	dispatcher, gate, identity, err := newDispatcherTestSubject(func(ctx CommandContext) error { received = ctx; return nil }, Roles(RoleGroupMember), "查", "查 状态")
	// [决策理由] 成功构建是最长匹配断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	event := &ws.MessageEvent{MessageType: "group", GroupID: 101, UserID: 202, RawMessage: "  查   状态   Keep CASE  "}
	matched, err := dispatcher.Dispatch(context.Background(), event)
	// [决策理由] 多字段触发词应在规范化后胜过短前缀并成功处理。
	if !matched || err != nil {
		t.Fatalf("Dispatch() = %t,%v", matched, err)
	}
	// [决策理由] Handler 应收到标准触发词、合并空白但保留大小写的参数和解析身份。
	if received.Trigger != "查 状态" || received.Arguments != "Keep CASE" || received.Role != RoleGroupMember || received.Message != event {
		t.Fatalf("CommandContext = %+v", received)
	}
	// [决策理由] 门禁和身份解析必须使用命中插件及消息中的可信标识。
	if strings.Join(gate.pluginCalls, ",") != "dispatcher_test" || identity.groupID != 101 || identity.userID != 202 {
		t.Fatalf("gate=%v identity=%+v", gate.pluginCalls, identity)
	}
	// [决策理由] 成功路径必须且只能释放一次 admission。
	if gate.admission.releases != 1 {
		t.Fatalf("releases = %d", gate.admission.releases)
	}

	// >>> 数据演变示例
	// 1. "查 状态 Keep CASE" -> 最长触发词"查 状态" -> arguments="Keep CASE"。
	// 2. group=101,user=202 -> identity(member) -> Handler -> Release一次。
}

// TestDispatcherAllowsArgumentsBeyondTriggerStorageLimit 验证旧触发词字段长度限制不误伤 Handler 参数。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用 Handler 并捕获长参数。
func TestDispatcherAllowsArgumentsBeyondTriggerStorageLimit(t *testing.T) {
	longArgument := strings.Repeat("长", 200)
	var received string
	dispatcher, gate, _, err := newDispatcherTestSubject(func(ctx CommandContext) error { received = ctx.Arguments; return nil }, Roles(RoleGroupMember), "echo")
	// [决策理由] 成功构建是长业务参数分发断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	matched, dispatchErr := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: " ECHO  " + longArgument})
	// [决策理由] 128 字符限制属于触发词存储契约，不能让合法短触发词因参数较长而失配。
	if !matched || dispatchErr != nil || received != longArgument || gate.admission.releases != 1 {
		t.Fatalf("Dispatch() = %t,%v arguments=%d releases=%d", matched, dispatchErr, len([]rune(received)), gate.admission.releases)
	}

	// >>> 数据演变示例
	// 1. echo+200字符参数 -> 触发词命中 -> Handler收到完整200字符参数。
	// 2. Ready且群开启 -> 长参数不影响门禁 -> Release一次。
}

// TestDispatcherSupportsConcurrentAdmissions 验证并发只读分发为每次调用独立准入并完整释放。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：并发调用 Handler，并原子记录处理、准入和释放次数。
func TestDispatcherSupportsConcurrentAdmissions(t *testing.T) {
	const calls = 32
	var handled atomic.Int64
	spec := validPluginSpec("concurrent", "echo")
	spec.Commands[0].Handler = func(CommandContext) error { handled.Add(1); return nil }
	catalog, err := NewSpecCatalog([]PluginSpec{spec})
	// [决策理由] 合法 Catalog 是并发分发测试的前提。
	if err != nil {
		t.Fatal(err)
	}
	gate := &concurrentDispatcherGate{}
	dispatcher, err := NewDispatcher(catalog, gate, concurrentDispatcherIdentity{})
	// [决策理由] 安全依赖装配成功后才能测试并发调用边界。
	if err != nil {
		t.Fatal(err)
	}
	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, calls)
	for index := 0; index < calls; index++ {
		waitGroup.Add(1)
		go func(userID int64) {
			defer waitGroup.Done()
			matched, dispatchErr := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: userID, RawMessage: "echo value"})
			// [决策理由] 任一并发调用未匹配或失败都应汇总给主测试协程报告。
			if !matched || dispatchErr != nil {
				errorsFound <- errors.New("并发 Dispatch 未成功")
			}

			// >>> 数据演变示例
			// 1. user=1 -> 独立Admit -> Handler -> Release -> 无错误。
			// 2. user=32 -> 与其他调用并行 -> 相同完整链路。
		}(int64(index + 1))
	}
	waitGroup.Wait()
	close(errorsFound)
	// [决策理由] 错误通道非空表示至少一个并发链未完整执行。
	if len(errorsFound) != 0 {
		t.Fatalf("concurrent errors = %d", len(errorsFound))
	}
	// [决策理由] Handler、准入和释放数量必须一一对应，证明没有跨调用共享或资源遗漏。
	if handled.Load() != calls || gate.admissions.Load() != calls || gate.releases.Load() != calls {
		t.Fatalf("handled=%d admissions=%d releases=%d", handled.Load(), gate.admissions.Load(), gate.releases.Load())
	}

	// >>> 数据演变示例
	// 1. 32次并发命中 -> handled=32,admissions=32,releases=32。
	// 2. 任一路径漏Release -> releases<32 -> 测试失败。
}

// TestDispatcherFailsClosedAcrossGatesAndIdentity 验证 Ready、群开关、身份失败和代码角色逐层 fail-closed。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：按表格修改纯内存 fake 并调用 Dispatcher。
func TestDispatcherFailsClosedAcrossGatesAndIdentity(t *testing.T) {
	identityFailure := errors.New("identity unavailable")
	tests := []struct {
		name           string
		configure      func(*dispatcherTestGate, *dispatcherTestIdentity)
		want           error
		wantReleases   int
		wantIdentities int
	}{
		{name: "全局未Ready", configure: func(g *dispatcherTestGate, _ *dispatcherTestIdentity) { g.ready = false }, want: ErrPluginNotReady},
		{name: "群未开启", configure: func(g *dispatcherTestGate, _ *dispatcherTestIdentity) { g.admission.groupEnabled = false }, want: ErrPluginGroupDisabled, wantReleases: 1},
		{name: "身份解析失败", configure: func(_ *dispatcherTestGate, r *dispatcherTestIdentity) { r.err = identityFailure }, want: identityFailure, wantReleases: 1, wantIdentities: 1},
		{name: "身份未授权", configure: func(_ *dispatcherTestGate, r *dispatcherTestIdentity) { r.role = RoleGroupAdmin }, want: ErrCommandUnauthorized, wantReleases: 1, wantIdentities: 1},
	}
	for _, test := range tests {
		dispatcher, gate, identity, err := newDispatcherTestSubject(func(CommandContext) error { t.Fatal("rejected path called Handler"); return nil }, Roles(RoleGroupMember), "echo")
		// [决策理由] 每个拒绝场景必须从相同合法 Dispatcher 起点执行。
		if err != nil {
			t.Fatal(err)
		}
		test.configure(gate, identity)
		matched, dispatchErr := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "ECHO value"})
		// [决策理由] 代码触发词已经命中，后续拒绝仍应报告 matched=true 并保留根因。
		if !matched || !errors.Is(dispatchErr, test.want) {
			t.Errorf("%s: Dispatch() = %t,%v, want %v", test.name, matched, dispatchErr, test.want)
		}
		// [决策理由] 取得 admission 的所有错误路径必须释放，未 Ready 则没有资源可释放。
		if gate.admission.releases != test.wantReleases || identity.calls != test.wantIdentities {
			t.Errorf("%s: releases=%d identities=%d", test.name, gate.admission.releases, identity.calls)
		}
	}

	// >>> 数据演变示例
	// 1. Ready=false -> 命中 -> 不取得admission -> ErrPluginNotReady。
	// 2. Ready=true+group=true+admin未声明 -> Resolve -> Release -> ErrCommandUnauthorized。
}

// TestDispatcherReturnsHandlerErrorAndReleasesAdmission 验证 Handler 错误保持原样且释放在途调用。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用返回测试错误的 Handler。
func TestDispatcherReturnsHandlerErrorAndReleasesAdmission(t *testing.T) {
	handlerErr := errors.New("handler failed")
	dispatcher, gate, _, err := newDispatcherTestSubject(func(CommandContext) error { return handlerErr }, Roles(RoleGroupMember), "echo")
	// [决策理由] 成功构建是 Handler 错误传播断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	matched, dispatchErr := dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "echo"})
	// [决策理由] Handler 业务错误应保持 errors.Is 链且命令仍属于已匹配。
	if !matched || !errors.Is(dispatchErr, handlerErr) || gate.admission.releases != 1 {
		t.Fatalf("Dispatch() = %t,%v releases=%d", matched, dispatchErr, gate.admission.releases)
	}

	// >>> 数据演变示例
	// 1. Handler返回handler failed -> defer Release -> matched=true,error保持。
	// 2. admission releases=0 -> Handler错误返回 -> releases=1。
}

// TestDispatcherReleasesAdmissionWhenHandlerPanics 验证 panic 路径释放 admission 且不吞掉 panic。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：触发并恢复测试 Handler panic。
func TestDispatcherReleasesAdmissionWhenHandlerPanics(t *testing.T) {
	dispatcher, gate, _, err := newDispatcherTestSubject(func(CommandContext) error { panic("boom") }, Roles(RoleGroupMember), "echo")
	// [决策理由] 成功构建是 panic 资源清理断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	panicked := false
	func() {
		defer func() {
			// [决策理由] Dispatcher 只负责资源释放，panic 隔离属于更外层平台边界。
			if recover() != nil {
				panicked = true
			}
		}()
		_, _ = dispatcher.Dispatch(context.Background(), &ws.MessageEvent{MessageType: "group", GroupID: 100, UserID: 200, RawMessage: "echo"})

		// >>> 数据演变示例
		// 1. Handler panic=boom -> Dispatch defer Release -> 外层recover。
		// 2. Handler正常时 -> 无recover值 -> panicked保持false。
	}()
	// [决策理由] panic 必须传播到平台隔离层，但 admission 仍只能释放一次。
	if !panicked || gate.admission.releases != 1 {
		t.Fatalf("panicked=%t releases=%d", panicked, gate.admission.releases)
	}

	// >>> 数据演变示例
	// 1. boom -> Release -> recover -> panicked=true。
	// 2. releases=0 -> panic展开defer -> releases=1。
}

// TestNewDispatcherRejectsMissingDependencies 验证安全关键依赖缺失时拒绝装配。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：构建纯内存 Catalog 和 Dispatcher。
func TestNewDispatcherRejectsMissingDependencies(t *testing.T) {
	catalog, err := NewSpecCatalog([]PluginSpec{validPluginSpec("echo", "echo")})
	// [决策理由] 合法目录是依赖缺失断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	gate := &dispatcherTestGate{}
	identity := &dispatcherTestIdentity{}
	tests := []struct {
		name     string
		catalog  *SpecCatalog
		gate     RuntimeGate
		identity IdentityResolver
	}{
		{name: "nil catalog", gate: gate, identity: identity},
		{name: "nil gate", catalog: catalog, identity: identity},
		{name: "nil identity", catalog: catalog, gate: gate},
	}
	for _, test := range tests {
		dispatcher, buildErr := NewDispatcher(test.catalog, test.gate, test.identity)
		// [决策理由] 任一安全依赖缺失都不得返回可用 Dispatcher。
		if dispatcher != nil || buildErr == nil {
			t.Errorf("%s: NewDispatcher() = %v,%v", test.name, dispatcher, buildErr)
		}
	}

	// >>> 数据演变示例
	// 1. nil gate -> 无法校验Ready -> 构建错误。
	// 2. nil identity -> 无法授权RoleSet -> 构建错误。
}
