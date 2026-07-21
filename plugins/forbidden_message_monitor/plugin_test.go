// 📌 影响范围：纯内存验证群内违禁消息监控插件的Manifest、Factory、生命周期与空处理行为；不访问外部服务。
package forbiddenmessagemonitor

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/w1ndys/w1ndys-bot/internal/onebot"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeActions struct {
	banParams     []onebot.SetGroupBanParams
	history       onebot.GetGroupMessageHistoryResult
	historyParams []onebot.GetGroupMessageHistoryParams
	messageInfo   onebot.MessageInfo
	deleted       []any
	err           error
}

type blockingMaintenanceRepository struct {
	*fakeMonitorRepository
	started chan struct{}
	release chan struct{}
}

type fakeLLMEvaluator struct {
	calls  int
	result LLMEvaluationResult
	err    error
}

// Evaluate 返回预设模型结论并记录调用次数。
// @param ctx/request：模型调用参数。
// @returns 预设结果与错误。
// ⚠️副作用说明：递增calls。
func (f *fakeLLMEvaluator) Evaluate(context.Context, LLMEvaluationRequest) (LLMEvaluationResult, error) {
	f.calls++

	// >>> 数据演变示例
	// 1. Low/pass -> calls1 -> 返回安全结论。
	// 2. 预设error -> calls+1 -> 返回error。
	return f.result, f.err
}

// ListObservedGroups 模拟不及时响应取消的外部维护调用。
// @param ctx：维护上下文。
// @returns release关闭后返回空群集合。
// ⚠️副作用说明：关闭started并阻塞到release关闭。
func (f *blockingMaintenanceRepository) ListObservedGroups(context.Context) ([]int64, error) {
	select {
	case <-f.started:
	default:
		close(f.started)
	}
	<-f.release

	// >>> 数据演变示例
	// 1. 首次维护 -> started关闭 -> release前阻塞。
	// 2. release关闭 -> 返回空群集合并允许worker退出。
	return []int64{}, nil
}

// SetGroupBan 记录测试禁言参数。
// @param ctx/params：Action参数。
// @returns 预设错误。
// ⚠️副作用说明：追加banParams。
func (f *fakeActions) SetGroupBan(_ context.Context, params onebot.SetGroupBanParams) error {
	f.banParams = append(f.banParams, params)

	// >>> 数据演变示例
	// 1. duration2592000 -> 记录 -> nil。
	// 2. 预设error -> 记录 -> error。
	return f.err
}

// GetGroupMemberList 返回空测试成员列表。
// @param ctx/params：Action参数。
// @returns 空列表与预设错误。
// ⚠️副作用说明：无。
func (f *fakeActions) GetGroupMemberList(context.Context, onebot.GetGroupMemberListParams) ([]onebot.GroupMemberInfo, error) {
	// >>> 数据演变示例
	// 1. group100 -> []。
	// 2. 预设error -> [],error。
	return []onebot.GroupMemberInfo{}, f.err
}

// GetGroupMessageHistory 返回预设测试历史。
// @param ctx/params：Action参数。
// @returns 预设历史与错误。
// ⚠️副作用说明：无。
func (f *fakeActions) GetGroupMessageHistory(_ context.Context, params onebot.GetGroupMessageHistoryParams) (onebot.GetGroupMessageHistoryResult, error) {
	f.historyParams = append(f.historyParams, params)
	// >>> 数据演变示例
	// 1. history含2条 -> 原样返回。
	// 2. 预设error -> history,error。
	return f.history, f.err
}

// GetMessage 返回用于补齐历史锚点的消息详情。
// @param ctx/messageID：查询参数。
// @returns 预设消息详情和错误。
// ⚠️副作用说明：无。
func (f *fakeActions) GetMessage(context.Context, any) (onebot.MessageInfo, error) {
	// >>> 数据演变示例
	// 1. messageInfo序号222 -> 原样返回。
	// 2. 预设错误 -> messageInfo,error。
	return f.messageInfo, f.err
}

// DeleteMessage 记录测试撤回ID。
// @param ctx/messageID：Action参数。
// @returns 预设错误。
// ⚠️副作用说明：追加deleted。
func (f *fakeActions) DeleteMessage(_ context.Context, messageID any) error {
	f.deleted = append(f.deleted, messageID)

	// >>> 数据演变示例
	// 1. id9 -> deleted=[9]。
	// 2. 预设error -> 仍记录并返回error。
	return f.err
}

// testImplementation 创建具备默认引擎与fake依赖的测试实例。
// @param 无。
// @returns 可执行消息与生命周期的实例。
// ⚠️副作用说明：仅分配内存。
func testImplementation() *implementation {
	engineConfig := DefaultEngineConfig()
	engine, _ := NewEngine(engineConfig)
	result := &implementation{actions: &fakeActions{}, repository: &fakeMonitorRepository{}, httpClient: &http.Client{}, now: func() time.Time { return time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) }}
	result.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: engineConfig, llmTimeout: time.Second})

	// >>> 数据演变示例
	// 1. 默认配置 -> Engine -> 可执行实例。
	// 2. fake仓储/Action -> 无外部访问。
	return result
}

// TestManifest 验证监控插件的稳定身份和群控制属性。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestManifest(t *testing.T) {
	// [决策理由] Manifest必须先通过平台校验，才能安全进入启动同步流程。
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() error = %v", err)
	}
	// [决策理由] 插件名是数据库、群门禁和审计的永久引用，必须保持约定值。
	if manifest.Name != pluginName {
		t.Fatalf("Manifest.Name = %q", manifest.Name)
	}
	// [决策理由] 群消息监控必须交由平台统一逐群启停。
	if !manifest.GroupControllable {
		t.Fatal("Manifest.GroupControllable = false")
	}
	// [决策理由] 当前尚无可定向调用功能，不应暴露占位命令或权限项。
	if manifest.System || len(manifest.Features) != 0 {
		t.Fatalf("Manifest.System/Features = %v/%+v", manifest.System, manifest.Features)
	}

	// >>> 数据演变示例
	// 1. 普通观察插件+GroupControllable=true -> Validate通过 -> 可逐群控制。
	// 2. 无业务功能 -> Features为空 -> 不生成占位命令。
}

// TestFactoryAndName 验证Factory依赖边界和稳定名称。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：仅分配内存实例。
func TestFactoryAndName(t *testing.T) {
	_, err := newPlugin(plugin.Runtime{})
	// [决策理由] 自动处置插件缺少Action和数据库时必须拒绝启动。
	if err == nil {
		t.Fatal("newPlugin() missing dependencies error=nil")
	}
	instance, err := newPlugin(plugin.Runtime{Actions: &fakeActions{}, Database: &pgxpool.Pool{}})
	// [决策理由] Factory成功时必须返回可注册的非空实现。
	if err != nil || instance == nil {
		t.Fatalf("newPlugin() instance/error = %v/%v", instance, err)
	}
	// [决策理由] 运行实例名称必须与Manifest一致，Manager才能注册实例。
	if instance.Name() != manifest.Name {
		t.Fatalf("newPlugin() name = %q", instance.Name())
	}

	// >>> 数据演变示例
	// 1. Runtime{} -> 缺依赖错误。
	// 2. Runtime{Actions,Database} -> implementation且Name匹配。
}

// TestLifecycleIsIdempotent 验证无资源生命周期可安全重复调用。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：仅调用无状态实例的生命周期方法。
func TestLifecycleIsIdempotent(t *testing.T) {
	instance := testImplementation()
	for call := 0; call < 2; call++ {
		// [决策理由] 启用可能由启动恢复和热切换重复触发，必须保持幂等。
		if err := instance.OnEnable(context.Background()); err != nil {
			t.Fatalf("OnEnable() call %d error = %v", call+1, err)
		}
		// [决策理由] 禁用也可能在资源已释放后重复触发，必须安全返回。
		if err := instance.OnDisable(context.Background()); err != nil {
			t.Fatalf("OnDisable() call %d error = %v", call+1, err)
		}
	}

	// >>> 数据演变示例
	// 1. enable->disable -> 均无资源变更 -> nil。
	// 2. 再次enable->disable -> 状态不累积 -> nil。
}

// TestDisableTimeoutKeepsWorkerGeneration 验证停用超时不会遗失旧worker句柄或启动第二个worker。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：启动并取消一个受控阻塞的维护协程。
func TestDisableTimeoutKeepsWorkerGeneration(t *testing.T) {
	repository := &blockingMaintenanceRepository{fakeMonitorRepository: &fakeMonitorRepository{}, started: make(chan struct{}), release: make(chan struct{})}
	instance := testImplementation()
	instance.repository = repository
	// [决策理由] 启用不能等待首次维护，否则NapCat尚未连接时会阻塞服务启动。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatalf("OnEnable() error = %v", err)
	}
	select {
	case <-repository.started:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not start")
	}
	instance.lifecycleMu.Lock()
	firstDone := instance.done
	instance.lifecycleMu.Unlock()
	disableContext, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	// [决策理由] 外部维护尚未返回时停用必须按调用方截止时间返回，同时保留生命周期代次。
	if err := instance.OnDisable(disableContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OnDisable() error = %v, want deadline exceeded", err)
	}
	// [决策理由] 超时后的重复启用必须记录恢复意图，但不能在旧代次退出前创建第二个worker。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatalf("second OnEnable() error = %v", err)
	}
	instance.lifecycleMu.Lock()
	secondDone := instance.done
	instance.lifecycleMu.Unlock()
	// [决策理由] done通道身份代表worker代次，变化即说明错误启动了第二个worker。
	if secondDone != firstDone {
		t.Fatal("OnEnable() replaced worker after disable timeout")
	}
	close(repository.release)
	deadline := time.After(time.Second)
	for {
		instance.lifecycleMu.Lock()
		restartedDone := instance.done
		instance.lifecycleMu.Unlock()
		// [决策理由] 停用失败后管理器仍视为启用，旧代次退出必须自动恢复为新worker。
		if restartedDone != nil && restartedDone != firstDone {
			break
		}
		select {
		case <-deadline:
			t.Fatal("worker did not restart after failed disable")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()
	// [决策理由] 旧worker退出后再次停用必须能够继续等待并清理保留的句柄。
	if err := instance.OnDisable(cleanupContext); err != nil {
		t.Fatalf("cleanup OnDisable() error = %v", err)
	}
	instance.lifecycleMu.Lock()
	workerCleared := instance.cancel == nil && instance.done == nil
	instance.lifecycleMu.Unlock()
	// [决策理由] 成功等待退出后必须清空句柄，允许未来重新启用。
	if !workerCleared {
		t.Fatal("worker handles were not cleared")
	}

	// >>> 数据演变示例
	// 1. enable->维护阻塞->disable超时->enable -> 旧代次退出前仍为同一done。
	// 2. release->自动新代次->disable成功 -> cancel/done清空。
}

// TestHandleIgnoresUnsupportedEvents 验证私聊、机器人自发消息和元事件安静返回。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestHandleIgnoresUnsupportedEvents(t *testing.T) {
	instance := testImplementation()
	events := []ws.Event{
		&ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "private", UserID: 1, RawMessage: "私聊"},
		&ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message_sent"}, MessageType: "group", GroupID: 100, UserID: 1, RawMessage: "机器人消息"},
		&ws.HeartbeatEvent{BaseEvent: ws.BaseEvent{PostType: "meta_event"}, MetaEventType: "heartbeat"},
	}
	for _, event := range events {
		// [决策理由] 非入站群消息和无关元事件不得进入计数、检测或处置。
		if err := instance.Handle(context.Background(), event); err != nil {
			t.Fatalf("Handle(%T) error = %v", event, err)
		}
	}

	// >>> 数据演变示例
	// 1. private/message_sent -> nil且不访问仓储。
	// 2. heartbeat -> nil且不访问Action。
}

// TestColdStartSendsLowRiskMessageToLLM 验证冷启动模式不会因空词库零分绕过模型。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存fake模型一次。
func TestColdStartSendsLowRiskMessageToLLM(t *testing.T) {
	instance := testImplementation()
	evaluator := &fakeLLMEvaluator{result: LLMEvaluationResult{RiskLevel: "Low", TotalScore: 5, Reason: "普通交流", Violations: []string{}, SuggestedAction: "pass"}}
	current := instance.snapshot.Load()
	instance.snapshot.Store(&runtimeSnapshot{engine: current.engine, engineConfig: current.engineConfig, evaluator: evaluator, llmTimeout: time.Second, detectionMode: detectionModeColdStart, llmMaxConcurrency: 2, llmDailyRequestLimit: 500})
	event := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message", Time: time.Now().Unix()}, MessageType: "group", GroupID: 100, UserID: 200, MessageID: 9, RawMessage: "普通聊天内容"}
	err := instance.Handle(context.Background(), event)
	// [决策理由] 冷启动低分消息必须调用模型，Safe/Low结论直接放行且不生成违规记录。
	if err != nil || evaluator.calls != 1 {
		t.Fatalf("Handle() error=%v calls=%d", err, evaluator.calls)
	}
	repository := instance.repository.(*fakeMonitorRepository)
	// [决策理由] 模型明确pass时不得污染人工复核队列。
	if len(repository.created) != 0 {
		t.Fatalf("created violations = %+v", repository.created)
	}

	// >>> 数据演变示例
	// 1. 空词库score0+cold_start -> LLM Low/pass -> 放行。
	// 2. 同消息standard -> 本地low直接放行且不调用LLM。
}

// TestLLMMinimumLengthOnlySkipsModel 验证长度门槛不影响本地硬词且会跳过短消息模型调用。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：使用内存fake执行一次本地处置并检查模型调用次数。
func TestLLMMinimumLengthOnlySkipsModel(t *testing.T) {
	instance := testImplementation()
	evaluator := &fakeLLMEvaluator{result: LLMEvaluationResult{RiskLevel: "Low", TotalScore: 5, SuggestedAction: "pass"}}
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"短广告"}
	engine, err := NewEngine(config)
	// [决策理由] 测试规则必须成功构造才能验证检测顺序。
	if err != nil {
		t.Fatal(err)
	}
	instance.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: config, evaluator: evaluator, llmTimeout: time.Second, detectionMode: detectionModeColdStart, llmMaxConcurrency: 2, llmDailyRequestLimit: 500, minLLMMessageLength: 30})
	shortSafe := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message", Time: time.Now().Unix()}, MessageType: "group", GroupID: 100, UserID: 200, MessageID: 10, RawMessage: "普通短消息"}
	// [决策理由] 未命中本地高风险且不足30字符时必须在外部调用前放行。
	if err := instance.Handle(context.Background(), shortSafe); err != nil || evaluator.calls != 0 {
		t.Fatalf("short Handle() error=%v calls=%d", err, evaluator.calls)
	}
	shortBlocked := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message", Time: time.Now().Unix()}, MessageType: "group", GroupID: 100, UserID: 201, MessageID: 11, RawMessage: "短广告"}
	// [决策理由] 长度门槛不能绕过位于前面的硬关键词处置。
	if err := instance.Handle(context.Background(), shortBlocked); err != nil {
		t.Fatalf("blocked Handle() error=%v", err)
	}
	actions := instance.actions.(*fakeActions)
	// [决策理由] 短硬词应执行本地禁言且仍不调用模型。
	if len(actions.banParams) != 1 || evaluator.calls != 0 {
		t.Fatalf("bans=%d calls=%d", len(actions.banParams), evaluator.calls)
	}

	// >>> 数据演变示例
	// 1. 5字符普通消息+minimum30 -> 跳过LLM放行。
	// 2. 3字符硬词消息+minimum30 -> 本地block，LLM仍为0次。
}

// TestPublishWeightOffsetsKeepsHardKeywordWithoutNegativeEvidence 验证正向晋级不会被误认为硬词误报。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：替换测试实例的内存检测快照。
func TestPublishWeightOffsetsKeepsHardKeywordWithoutNegativeEvidence(t *testing.T) {
	instance := testImplementation()
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"内部渠道"}
	engine, err := NewEngine(config)
	// [决策理由] 测试基础硬词引擎必须成功构造。
	if err != nil {
		t.Fatal(err)
	}
	instance.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: config, minLLMMessageLength: 30})
	// [决策理由] 仅有正向学习权重时不得移除管理员设置的零容错硬词。
	if err := instance.publishWeightOffsets(map[string]float64{"内部渠道": 10}, map[string]struct{}{}); err != nil {
		t.Fatalf("publishWeightOffsets() error = %v", err)
	}
	result := instance.snapshot.Load().engine.CheckExactText("内部渠道")
	// [决策理由] 发布后硬词仍应在精准层直接命中。
	if !result.Blocked {
		t.Fatalf("CheckExactText() = %+v", result)
	}
	// [决策理由] 每日学习发布只替换检测引擎，不得清空已热应用的模型调用长度门槛。
	if got := instance.snapshot.Load().minLLMMessageLength; got != 30 {
		t.Fatalf("minLLMMessageLength=%d", got)
	}

	// >>> 数据演变示例
	// 1. hard词+learned10+无误报 -> 保留hard拦截。
	// 2. minimum30+发布学习权重 -> 新快照仍保持minimum30。
}

// TestHandleExactViolationModeratesAndRecords 验证精准规则执行禁言、筛选撤回和审计。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：修改fake Action与仓储记录。
func TestHandleExactViolationModeratesAndRecords(t *testing.T) {
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	repository := &fakeMonitorRepository{}
	actions := &fakeActions{history: onebot.GetGroupMessageHistoryResult{Messages: []onebot.GroupHistoryMessage{
		{Time: now.Unix(), MessageID: 9, UserID: 200},
		{Time: now.Add(-time.Minute).Unix(), MessageID: 8, UserID: 200},
		{Time: now.Unix(), MessageID: 7, UserID: 300},
		{Time: now.Add(time.Minute).Unix(), MessageID: 10, UserID: 200},
	}}}
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"内部渠道"}
	engine, err := NewEngine(config)
	// [决策理由] 测试规则必须成功构造才能验证处置链路。
	if err != nil {
		t.Fatal(err)
	}
	instance := &implementation{actions: actions, repository: repository, now: func() time.Time { return now }}
	instance.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: config, llmTimeout: time.Second})
	event := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message", Time: now.Unix()}, MessageType: "group", GroupID: 100, UserID: 200, MessageID: 9, MessageSeq: 222, RawMessage: "内部渠道"}
	err = instance.Handle(context.Background(), event)
	// [决策理由] 成功处置和审计后事件允许后续插件继续处理且不返回错误。
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	// [决策理由] 禁言必须固定30天且精确定位群与用户。
	if len(actions.banParams) != 1 || actions.banParams[0].Duration != banDurationSeconds || actions.banParams[0].GroupID != "100" || actions.banParams[0].UserID != "200" {
		t.Fatalf("ban params = %+v", actions.banParams)
	}
	// [决策理由] 仅撤回锚点前同一用户的两条消息，其他用户和未来消息必须保留。
	if len(actions.deleted) != 2 || actions.deleted[0] != int64(9) || actions.deleted[1] != int64(8) {
		t.Fatalf("deleted = %+v", actions.deleted)
	}
	// [决策理由] 历史页必须以违规消息序号为锚点，不能读取执行时的最新消息页。
	if len(actions.historyParams) != 1 || actions.historyParams[0].MessageSeq != "222" {
		t.Fatalf("history params = %+v", actions.historyParams)
	}
	// [决策理由] 自动动作必须对应唯一待复核证据。
	if len(repository.created) != 1 || repository.created[0].DetectionSource != "precise_rule" || repository.created[0].MessageContent != "内部渠道" {
		t.Fatalf("created = %+v", repository.created)
	}

	// >>> 数据演变示例
	// 1. 硬词命中+历史含本人2条 -> 禁言30天+撤回2条+审计1条。
	// 2. 他人消息或锚点后消息 -> 筛选跳过。
}

// TestHandleExactViolationIsIdempotent 验证重复事件只执行一次外部处罚。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：修改fake仓储与Action调用记录。
func TestHandleExactViolationIsIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	repository := &fakeMonitorRepository{}
	actions := &fakeActions{}
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"固定广告"}
	engine, err := NewEngine(config)
	// [决策理由] 规则构造失败时无法验证重复处置链路。
	if err != nil {
		t.Fatal(err)
	}
	instance := &implementation{actions: actions, repository: repository, now: func() time.Time { return now }}
	instance.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: config, llmTimeout: time.Second})
	event := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message", Time: now.Unix()}, MessageType: "group", GroupID: 100, UserID: 200, MessageID: 9, MessageSeq: 222, RawMessage: "固定广告"}
	for call := 0; call < 2; call++ {
		// [决策理由] OneBot可能重复投递同一message_id，两次处理都应安全返回。
		if err := instance.Handle(context.Background(), event); err != nil {
			t.Fatalf("Handle() call%d error=%v", call+1, err)
		}
	}
	// [决策理由] 第二次预留冲突必须在禁言、历史和撤回之前短路。
	if repository.reserveCalls != 2 || repository.completeCalls != 1 || len(actions.banParams) != 1 || len(actions.historyParams) != 1 {
		t.Fatalf("reserve=%d complete=%d bans=%d history=%d", repository.reserveCalls, repository.completeCalls, len(actions.banParams), len(actions.historyParams))
	}
	// >>> 数据演变示例
	// 1. 首次message9 -> 预留成功 -> 禁言与审计完成。
	// 2. 重复message9 -> 预留未获得 -> 无第二次Action。
}

// TestHandleExactViolationSkipsActionsWhenReservationFails 验证数据库预留失败时不处罚。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：读取fake Action调用记录。
func TestHandleExactViolationSkipsActionsWhenReservationFails(t *testing.T) {
	repository := &fakeMonitorRepository{reserveErr: errors.New("db unavailable")}
	actions := &fakeActions{}
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"固定广告"}
	engine, err := NewEngine(config)
	// [决策理由] 测试必须先构造可命中的确定性规则。
	if err != nil {
		t.Fatal(err)
	}
	instance := &implementation{actions: actions, repository: repository, now: time.Now}
	instance.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: config, llmTimeout: time.Second})
	event := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", GroupID: 100, UserID: 200, MessageID: 9, MessageSeq: 222, RawMessage: "固定广告"}
	err = instance.Handle(context.Background(), event)
	// [决策理由] 无法落审计时必须返回错误且保持QQ外部状态不变。
	if err == nil || len(actions.banParams) != 0 || len(actions.historyParams) != 0 || len(actions.deleted) != 0 {
		t.Fatalf("Handle() error=%v bans=%d history=%d deleted=%d", err, len(actions.banParams), len(actions.historyParams), len(actions.deleted))
	}
	// >>> 数据演变示例
	// 1. Reserve成功 -> 可进入Action（由其他测试覆盖）。
	// 2. Reserve数据库失败 -> 返回error且Action计数全0。
}

// TestHandleWhitelistBypassesDetection 验证白名单用户只计数而不运行处罚。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：读取fake Action与仓储记录。
func TestHandleWhitelistBypassesDetection(t *testing.T) {
	instance := testImplementation()
	repository := instance.repository.(*fakeMonitorRepository)
	repository.whitelisted = true
	event := &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", GroupID: 100, UserID: 200, MessageID: 9, RawMessage: "微信 vxabc123"}
	err := instance.Handle(context.Background(), event)
	// [决策理由] 白名单必须在任何硬词或评分检测前直接放行。
	if err != nil || len(instance.actions.(*fakeActions).banParams) != 0 || len(repository.created) != 0 {
		t.Fatalf("Handle() error=%v bans=%+v audits=%+v", err, instance.actions.(*fakeActions).banParams, repository.created)
	}

	// >>> 数据演变示例
	// 1. whitelist=true+任意文本 -> 无禁言无审计。
	// 2. whitelist=false -> 其他测试验证进入精准检测。
}
