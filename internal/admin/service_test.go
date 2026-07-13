// 📌 影响范围：无；使用内存替身验证 AdminService，不连接真实 PostgreSQL。
package admin

import (
	"context"
	"errors"
	"testing"
)

type fakeRepository struct {
	states      []PluginState
	updated     PluginState
	updateActor Actor
	updateName  string
	updatePatch PluginPatch
	err         error
}

// ListPlugins 返回测试预设插件快照。
// @param ctx：未使用的测试上下文。
// @returns 预设插件快照或错误。
// ⚠️副作用说明：无。
func (f *fakeRepository) ListPlugins(_ context.Context) ([]PluginState, error) {
	// >>> 数据演变示例
	// 1. states=[ping] -> 返回 [ping]。
	// 2. err=boom -> 返回 nil,boom。
	return f.states, f.err
}

// UpdatePlugin 记录测试调用并返回预设结果。
// @param ctx：未使用的测试上下文；actor：操作者；name：插件名；patch：变更。
// @returns 预设插件状态或错误。
// ⚠️副作用说明：修改 fakeRepository 的调用记录字段。
func (f *fakeRepository) UpdatePlugin(_ context.Context, actor Actor, name string, patch PluginPatch) (PluginState, error) {
	f.updateActor = actor
	f.updateName = name
	f.updatePatch = patch

	// >>> 数据演变示例
	// 1. ping + enabled=true -> 记录参数 -> 返回 updated。
	// 2. missing + err -> 记录参数 -> 返回 error。
	return f.updated, f.err
}

type fakeRuntime struct {
	loads int
	err   error
}

type fakeAuthorizer struct {
	allowed map[string]bool
}

// IsSuperAdmin 返回测试预设的授权判断。
// @param userID：待校验用户 ID。
// @returns allowed 中对应的布尔值。
// ⚠️副作用说明：无。
func (f *fakeAuthorizer) IsSuperAdmin(userID string) bool {
	allowed := f.allowed[userID]

	// >>> 数据演变示例
	// 1. allowed[123]=true -> IsSuperAdmin(123) -> true。
	// 2. allowed无200 -> IsSuperAdmin(200) -> false。
	return allowed
}

// Load 记录一次运行时刷新。
// @param ctx：未使用的测试上下文。
// @returns 预设刷新错误。
// ⚠️副作用说明：递增 loads 计数。
func (f *fakeRuntime) Load(_ context.Context) error {
	f.loads++

	// >>> 数据演变示例
	// 1. loads=0 -> Load -> loads=1,nil。
	// 2. err=boom -> Load -> loads+1,boom。
	return f.err
}

// TestSetPluginEnabledPersistsAuditedChangeAndRefreshes 验证启停操作参数和热刷新。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledPersistsAuditedChangeAndRefreshes(t *testing.T) {
	repository := &fakeRepository{updated: PluginState{Name: "ping", Enabled: true, Priority: 100}}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, &fakeAuthorizer{allowed: map[string]bool{"123": true}})
	actor := Actor{ID: "123", Role: "super_admin", Channel: ChannelQQ, RequestID: "req-1"}
	state, err := service.SetPluginEnabled(context.Background(), actor, "ping", true)
	// [决策理由] 正常管理路径必须无错误才能继续验证结果。
	if err != nil {
		t.Fatalf("SetPluginEnabled() error = %v", err)
	}
	// [决策理由] Service 必须返回仓库提交后的真实状态。
	if !state.Enabled || state.Name != "ping" {
		t.Fatalf("state = %+v", state)
	}
	// [决策理由] 操作者和请求标识必须原样进入事务审计。
	if repository.updateActor != actor || repository.updateName != "ping" {
		t.Fatalf("update actor/name = %+v/%q", repository.updateActor, repository.updateName)
	}
	// [决策理由] 指针 patch 必须明确表达 enabled=false 与未修改的区别。
	if repository.updatePatch.Enabled == nil || !*repository.updatePatch.Enabled {
		t.Fatalf("enabled patch = %+v", repository.updatePatch.Enabled)
	}
	// [决策理由] 成功写入后运行时必须且只能刷新一次。
	if runtime.loads != 1 {
		t.Fatalf("runtime loads = %d, want 1", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. ping:false + QQ管理员启用 -> Repository调用 -> Load一次 -> ping:true。
	// 2. RequestID=req-1 -> Actor透传 -> 审计可关联请求。
}

// TestSetPluginPriorityRejectsInvalidActor 验证缺失操作者时不产生写入和刷新。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginPriorityRejectsInvalidActor(t *testing.T) {
	repository := &fakeRepository{}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, &fakeAuthorizer{})
	_, err := service.SetPluginPriority(context.Background(), Actor{Channel: ChannelWebUI}, "ping", 20)
	// [决策理由] 空 Actor ID 必须返回稳定领域错误供入口转换为拒绝响应。
	if !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("error = %v, want ErrInvalidActor", err)
	}
	// [决策理由] 校验失败发生在仓库前，因此插件名调用记录必须为空。
	if repository.updateName != "" {
		t.Fatalf("repository unexpectedly called for %q", repository.updateName)
	}
	// [决策理由] 未持久化的状态不得发布到运行时。
	if runtime.loads != 0 {
		t.Fatalf("runtime loads = %d, want 0", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. Actor.ID空 + priority=20 -> ErrInvalidActor -> 零写入。
	// 2. Actor.ID空 + runtime -> 校验提前返回 -> loads=0。
}

// TestSetPluginEnabledReturnsRefreshFailure 验证数据库成功后刷新失败会明确上报。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledReturnsRefreshFailure(t *testing.T) {
	repository := &fakeRepository{updated: PluginState{Name: "ping", Enabled: true}}
	runtime := &fakeRuntime{err: errors.New("lifecycle failed")}
	service := NewService(repository, runtime, &fakeAuthorizer{})
	state, err := service.SetPluginEnabled(context.Background(), Actor{ID: "system", Role: "system", Channel: ChannelSystem}, "ping", true)
	// [决策理由] 热刷新失败不能向管理入口报告完全成功。
	if err == nil {
		t.Fatal("SetPluginEnabled() error = nil")
	}
	// [决策理由] 返回已提交状态让上层知道数据库目标值，便于告警和重试刷新。
	if !state.Enabled {
		t.Fatalf("state = %+v, want committed state", state)
	}
	// [决策理由] 刷新失败仍应只尝试一次，避免生命周期回调被隐式重复调用。
	if runtime.loads != 1 {
		t.Fatalf("runtime loads = %d, want 1", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. DB提交true + Load失败 -> 返回 true状态和刷新错误。
	// 2. lifecycle failed -> 不自动重试 -> loads=1。
}

// TestSetPluginEnabledRejectsUntrustedRole 验证自报角色不能绕过管理员快照。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestSetPluginEnabledRejectsUntrustedRole(t *testing.T) {
	repository := &fakeRepository{}
	runtime := &fakeRuntime{}
	service := NewService(repository, runtime, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.SetPluginEnabled(context.Background(), Actor{ID: "200", Role: "super_admin", Channel: ChannelQQ}, "ping", true)
	// [决策理由] actor.Role 来自入口组装，不能替代服务端身份快照。
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	// [决策理由] 未授权操作必须在事务开始前终止。
	if repository.updateName != "" {
		t.Fatalf("repository unexpectedly called for %q", repository.updateName)
	}
	// [决策理由] 未授权操作不得改变运行时状态。
	if runtime.loads != 0 {
		t.Fatalf("runtime loads = %d, want 0", runtime.loads)
	}

	// >>> 数据演变示例
	// 1. actor.Role=super_admin但Resolver=false -> ErrForbidden。
	// 2. 拒绝发生在Repository前 -> 无数据库写入和热刷新。
}
