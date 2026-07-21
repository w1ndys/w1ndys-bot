// 📌 影响范围：无外部服务；使用内存 fake 验证违规复核输入、状态机与管理资源边界。
package forbiddenmessagemonitor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

type fakeMonitorRepository struct {
	reviewCalls      int
	lastStatus       string
	record           management.ResourceRecord
	err              error
	whitelisted      bool
	created          []violationCreate
	stored           storedViolation
	reserveCalls     int
	completeCalls    int
	reserveErr       error
	reserveDenied    bool
	trainingMessage  string
	trainingFeatures []string
	trainingExists   bool
}

// TrainingSampleExists 返回测试预置的重复样本状态。
// @param ctx/message：查询上下文与样本原文。
// @returns 预置存在标记或错误。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) TrainingSampleExists(context.Context, string) (bool, error) {
	// [决策理由] 仓储错误优先于存在标记，模拟数据库检查失败。
	if f.err != nil {
		return false, f.err
	}

	// >>> 数据演变示例
	// 1. trainingExists=true -> true,nil。
	// 2. 默认fake -> false,nil。
	return f.trainingExists, nil
}

// IncrementValidSpeech 实现测试仓储契约。
// @param 仓储接口参数。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) IncrementValidSpeech(context.Context, int64, int64, time.Time) error {
	// >>> 数据演变示例
	// 1. 任意输入 -> nil。
	// 2. 取消上下文 -> nil。
	return nil
}

// ListObservedGroups 实现测试仓储契约。
// @param ctx：查询上下文。
// @returns 空群集合。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) ListObservedGroups(context.Context) ([]int64, error) {
	// >>> 数据演变示例
	// 1. 空fake -> []。
	// 2. 取消上下文 -> []。
	return []int64{}, nil
}

// RecentValidCounts 实现测试仓储契约。
// @param ctx/group/since：聚合参数。
// @returns 空计数映射。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) RecentValidCounts(context.Context, int64, time.Time) (map[int64]int, error) {
	// >>> 数据演变示例
	// 1. 群1 -> 空map。
	// 2. 群2 -> 空map。
	return map[int64]int{}, nil
}

// IsWhitelisted 实现测试仓储契约。
// @param 仓储接口参数。
// @returns false,nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) IsWhitelisted(context.Context, int64, int64) (bool, error) {
	// >>> 数据演变示例
	// 1. 用户存在 -> fake仍返回false。
	// 2. 用户不存在 -> false,nil。
	return f.whitelisted, f.err
}

// RemoveWhitelist 实现测试仓储契约。
// @param ctx/group/user：删除参数。
// @returns 预设错误。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) RemoveWhitelist(context.Context, int64, int64) error {
	// >>> 数据演变示例
	// 1. group100/user200 -> nil。
	// 2. 预设错误 -> error。
	return f.err
}

// ReplaceWhitelist 实现测试仓储契约。
// @param 仓储接口参数。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) ReplaceWhitelist(context.Context, int64, []int64, time.Time) error {
	// >>> 数据演变示例
	// 1. [1,2] -> nil。
	// 2. [] -> nil。
	return nil
}

// CreateViolation 实现测试仓储契约。
// @param 仓储接口参数。
// @returns id1,nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) CreateViolation(_ context.Context, input violationCreate) (int64, error) {
	f.created = append(f.created, input)
	// >>> 数据演变示例
	// 1. 合法证据 -> 1,nil。
	// 2. 空证据 -> 1,nil。
	return 1, f.err
}

// ReserveViolation 模拟首次审计预留。
// @param ctx/input：预留参数。
// @returns id1、成功标记和预设错误。
// ⚠️副作用说明：追加created证据。
func (f *fakeMonitorRepository) ReserveViolation(_ context.Context, input violationCreate) (int64, bool, error) {
	f.reserveCalls++
	f.created = append(f.created, input)
	// [决策理由] 预设错误模拟数据库不可用，重复调用模拟唯一键已被首次事件占有。
	if f.reserveErr != nil || f.reserveCalls > 1 {
		return 0, false, f.reserveErr
	}
	// >>> 数据演变示例
	// 1. 正常fake -> {1,true,nil}。
	// 2. 预设错误 -> {1,true,error}。
	return 1, true, f.err
}

// CompleteViolationActions 模拟处置摘要写回。
// @param ctx/id/outcome：写回参数。
// @returns 预设错误。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) CompleteViolationActions(context.Context, int64, moderationOutcome) error {
	f.completeCalls++
	// >>> 数据演变示例
	// 1. id1+成功结果 -> nil。
	// 2. 预设错误 -> error。
	return f.err
}

// GetViolation 返回用于复核动作定位的fake记录。
// @param ctx/id：查询参数。
// @returns 群100、用户200的记录。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) GetViolation(context.Context, int64) (storedViolation, error) {
	result := f.stored
	// [决策理由] 未预置记录时提供稳定的合法定位目标。
	if result.ID == 0 {
		result = storedViolation{ID: 1, Version: 1, Data: violationData{GroupID: 100, UserID: 200, Status: statusPendingReview}}
	}

	// >>> 数据演变示例
	// 1. id1 -> group100/user200。
	// 2. 任意id -> 同一fake记录。
	return result, f.err
}

// TransitionByEvent 实现测试仓储契约。
// @param 仓储接口参数。
// @returns false,nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) TransitionByEvent(context.Context, management.Actor, int64, int64, time.Time, time.Time, string) (bool, error) {
	// >>> 数据演变示例
	// 1. 解禁 -> false,nil。
	// 2. 踢出 -> false,nil。
	return false, nil
}

// RecentExamples 实现测试仓储契约。
// @param ctx/group/limit：案例参数。
// @returns 空案例集合。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) RecentExamples(context.Context, int64, string, int) ([]reviewExample, error) {
	// >>> 数据演变示例
	// 1. limit10 -> []。
	// 2. limit1 -> []。
	return []reviewExample{}, nil
}

// BehaviorSummary 实现测试仓储契约。
// @param ctx/group/user/since：摘要参数。
// @returns 零值摘要。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) BehaviorSummary(context.Context, int64, int64, time.Time) (behaviorSummary, error) {
	// >>> 数据演变示例
	// 1. 用户1 -> 零值摘要。
	// 2. 用户2 -> 零值摘要。
	return behaviorSummary{}, nil
}

// FeedbackKeywordCounts 实现测试仓储契约。
// @param ctx/since：反馈聚合参数。
// @returns 空关键词计数。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) FeedbackKeywordCounts(context.Context, time.Time) (map[string]int, error) {
	// >>> 数据演变示例
	// 1. 今日 -> 空map。
	// 2. 昨日 -> 空map。
	return map[string]int{}, nil
}

// RefreshWeightOffsets 实现测试仓储契约。
// @param ctx/from/until/offsets：刷新参数。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) RefreshWeightOffsets(context.Context, time.Time, time.Time, []weightOffset) error {
	// >>> 数据演变示例
	// 1. 一个偏移 -> nil。
	// 2. 空偏移 -> nil。
	return nil
}

// ActiveWeightOffsets 实现测试仓储契约。
// @param ctx/at：查询参数。
// @returns 空偏移映射。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) ActiveWeightOffsets(context.Context, time.Time) (map[string]float64, error) {
	// >>> 数据演变示例
	// 1. 今日 -> 空map。
	// 2. 明日 -> 空map。
	return map[string]float64{}, nil
}

// LearnedKeywordWeights 实现测试仓储契约。
// @param ctx：查询上下文。
// @returns 空学习权重。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) LearnedKeywordWeights(context.Context) (map[string]float64, error) {
	// >>> 数据演变示例
	// 1. 空fake -> {}。
	// 2. 重复调用 -> 仍为空。
	return map[string]float64{}, nil
}

// TryReserveLLMRequest 实现测试仓储契约。
// @param ctx/at/limit：额度参数。
// @returns 始终预占成功。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) TryReserveLLMRequest(context.Context, time.Time, int) (bool, error) {
	// [决策理由] 显式配置错误优先返回，用于验证外部调用前的失败边界。
	if f.reserveErr != nil {
		return false, f.reserveErr
	}
	// [决策理由] 测试设置过额度调用时返回拒绝，默认fake仍保持成功。
	if f.reserveDenied {
		return false, nil
	}
	// >>> 数据演变示例
	// 1. limit500 -> true。
	// 2. 次日调用 -> true。
	return true, nil
}

// CreateTrainingSample 实现测试仓储契约。
// @param ctx/actor/message/features：训练样本参数。
// @returns fake记录或预设错误。
// ⚠️副作用说明：复用record保存返回值。
func (f *fakeMonitorRepository) CreateTrainingSample(_ context.Context, _ management.Actor, message string, features []string) (management.ResourceRecord, error) {
	f.trainingMessage = message
	f.trainingFeatures = append([]string(nil), features...)
	// >>> 数据演变示例
	// 1. 合法样本 -> 返回fake record。
	// 2. 预设错误 -> record,error。
	return f.record, f.err
}

// ListTrainingSamples 实现测试仓储契约。
// @param ctx/query：分页参数。
// @returns 含fake记录的分页。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) ListTrainingSamples(_ context.Context, query management.ResourceQuery) (management.ResourcePage, error) {
	result := management.ResourcePage{Items: []management.ResourceRecord{f.record}, Page: query.Page, PageSize: query.PageSize, Total: 1}
	// >>> 数据演变示例
	// 1. page1 -> fake记录页。
	// 2. 预设错误 -> 页+error。
	return result, f.err
}

// DeleteTrainingSample 实现测试仓储契约。
// @param ctx/actor/id/version：删除参数。
// @returns 预设错误。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) DeleteTrainingSample(context.Context, management.Actor, int64, int64) error {
	// >>> 数据演变示例
	// 1. id1/v1 -> nil。
	// 2. 预设错误 -> error。
	return f.err
}

// ListPending 返回fake分页。
// @param ctx/query：查询参数。
// @returns 包含fake记录的页或预置错误。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) ListPending(_ context.Context, query management.ResourceQuery) (management.ResourcePage, error) {
	result := management.ResourcePage{Items: []management.ResourceRecord{f.record}, Page: query.Page, PageSize: query.PageSize, Total: 1}
	// >>> 数据演变示例
	// 1. page1,size20 -> fake页。
	// 2. 预置错误 -> fake页,error。
	return result, f.err
}

// Review 记录复核调用并返回fake结果。
// @param ctx/actor/id/version/status：复核参数。
// @returns 预置记录或错误。
// ⚠️副作用说明：递增reviewCalls并保存status。
func (f *fakeMonitorRepository) Review(_ context.Context, _ management.Actor, _, _ int64, status string) (management.ResourceRecord, error) {
	f.reviewCalls++
	f.lastStatus = status
	// >>> 数据演变示例
	// 1. confirmed -> calls+1,返回记录。
	// 2. 预置错误 -> calls+1,error。
	return f.record, f.err
}

// BeginFalsePositive 模拟误报CAS预占。
// @param ctx/id/version：预占参数。
// @returns 可定位群用户的v2记录或预设错误。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) BeginFalsePositive(context.Context, int64, int64) (storedViolation, error) {
	result := f.stored
	// [决策理由] 未预置时提供稳定的处理中记录供handler测试。
	if result.ID == 0 {
		result = storedViolation{ID: 1, Version: 2, Data: violationData{GroupID: 100, UserID: 200, Status: statusFalsePositivePending}}
	}
	// >>> 数据演变示例
	// 1. pending v1 -> fake pending_unban v2。
	// 2. 预设错误 -> 记录,error。
	return result, f.err
}

// FinishFalsePositive 模拟解禁后终态提交。
// @param ctx/actor/id/version：完成参数。
// @returns 预置资源或错误。
// ⚠️副作用说明：递增reviewCalls并记录误报状态。
func (f *fakeMonitorRepository) FinishFalsePositive(context.Context, management.Actor, int64, int64) (management.ResourceRecord, error) {
	f.reviewCalls++
	f.lastStatus = statusFalsePositive
	// >>> 数据演变示例
	// 1. 解禁成功 -> calls+1+false_positive。
	// 2. 预设错误 -> calls+1,error。
	return f.record, f.err
}

// CancelFalsePositive 模拟Action失败后的预占补偿。
// @param ctx/id/version：补偿参数。
// @returns nil。
// ⚠️副作用说明：无。
func (f *fakeMonitorRepository) CancelFalsePositive(context.Context, int64, int64) error {
	// >>> 数据演变示例
	// 1. pending_unban -> pending -> nil。
	// 2. 已终态 -> 不变 -> nil。
	return nil
}

// TestDecodeReviewStatus 验证复核输入严格解码与允许目标。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestDecodeReviewStatus(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "确认", raw: `{"status":"确认"}`, want: statusConfirmedPendingKick},
		{name: "误报", raw: `{"status":"误报"}`, want: statusFalsePositive},
		{name: "禁止直接踢出", raw: `{"status":"已确认-已踢出"}`, wantErr: true},
		{name: "未知字段", raw: `{"status":"确认","msg_content":"x"}`, wantErr: true},
		{name: "尾随载荷", raw: `{"status":"确认"}{}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodeReviewStatus(json.RawMessage(test.raw))
			// [决策理由] 错误用例必须统一映射为领域输入错误。
			if test.wantErr {
				if !errors.Is(err, management.ErrInvalidResourceData) {
					t.Fatalf("decodeReviewStatus() error = %v", err)
				}
				return
			}
			// [决策理由] 合法输入必须精确保留稳定状态值。
			if err != nil || got != test.want {
				t.Fatalf("decodeReviewStatus() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	// >>> 数据演变示例
	// 1. 确认/误报 -> 解码成功。
	// 2. 终态/未知字段/尾随值 -> ErrInvalidResourceData。
}

// TestTransitionRules 验证WebUI与群事件各自的状态迁移边界。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestTransitionRules(t *testing.T) {
	// [决策理由] 普通复核事务处理确认，也允许从未禁言的Medium记录直接沉淀误报。
	if !reviewAllowedTransition(statusPendingReview, statusConfirmedPendingKick) || !reviewAllowedTransition(statusPendingReview, statusFalsePositive) {
		t.Fatal("reviewAllowedTransition() rejected supported review")
	}
	// [决策理由] 已终结记录不能通过WebUI被改写。
	if reviewAllowedTransition(statusConfirmedKicked, statusFalsePositive) {
		t.Fatal("reviewAllowedTransition() accepted terminal transition")
	}
	// [决策理由] 群踢出事件应完成等待踢出的记录。
	if !eventAllowedTransition(statusConfirmedPendingKick, statusConfirmedKicked) {
		t.Fatal("eventAllowedTransition() rejected kick event")
	}
	// [决策理由] 误判终态不得被后续噪声踢出事件翻转。
	if eventAllowedTransition(statusFalsePositive, statusConfirmedKicked) {
		t.Fatal("eventAllowedTransition() accepted terminal transition")
	}
	// >>> 数据演变示例
	// 1. pending->复核结果 -> true。
	// 2. 终态->另一终态 -> false。
}

// TestViolationResourceHandlerUpdate 验证非法载荷不访问仓储，合法载荷委派复核。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：仅修改fake调用计数。
func TestViolationResourceHandlerUpdate(t *testing.T) {
	fake := &fakeMonitorRepository{record: management.ResourceRecord{ID: 7, Version: 2, Data: json.RawMessage(`{"status":"confirmed_pending_kick"}`)}}
	handler := &violationResourceHandler{repository: fake}
	_, err := handler.Update(context.Background(), management.Actor{}, 7, 1, json.RawMessage(`{"msg_content":"tamper"}`))
	// [决策理由] 原始证据篡改必须在仓储之前被拒绝。
	if !errors.Is(err, management.ErrInvalidResourceData) || fake.reviewCalls != 0 {
		t.Fatalf("invalid Update() error=%v calls=%d", err, fake.reviewCalls)
	}
	record, err := handler.Update(context.Background(), management.Actor{}, 7, 1, json.RawMessage(`{"status":"确认"}`))
	// [决策理由] 合法确认必须且只能委派一次。
	if err != nil || record.Version != 2 || fake.reviewCalls != 1 || fake.lastStatus != statusConfirmedPendingKick {
		t.Fatalf("valid Update() record=%+v error=%v calls=%d status=%q", record, err, fake.reviewCalls, fake.lastStatus)
	}
	// >>> 数据演变示例
	// 1. 篡改msg_content -> 0次仓储调用。
	// 2. 确认status -> 1次Review并返回v2。
}

// TestViolationResourceHandlerUnbansBeforeFalsePositive 验证误报状态以成功解禁为前提。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：修改fake Action与仓储调用记录。
func TestViolationResourceHandlerUnbansBeforeFalsePositive(t *testing.T) {
	fake := &fakeMonitorRepository{record: management.ResourceRecord{ID: 7, Version: 2}, stored: storedViolation{ID: 7, Version: 1, Data: violationData{GroupID: 100, UserID: 200, Status: statusPendingReview, ActionResult: json.RawMessage(`{"ban_succeeded":true}`)}}}
	actions := &fakeActions{}
	handler := &violationResourceHandler{repository: fake, actions: actions}
	_, err := handler.Update(context.Background(), management.Actor{}, 7, 1, json.RawMessage(`{"status":"误报"}`))
	// [决策理由] 解禁成功后才可委派误报事务。
	if err != nil || len(actions.banParams) != 1 || actions.banParams[0].Duration != 0 || fake.lastStatus != statusFalsePositive {
		t.Fatalf("Update() error=%v bans=%+v status=%q", err, actions.banParams, fake.lastStatus)
	}
	actions.err = errors.New("unban failed")
	fake.reviewCalls = 0
	_, err = handler.Update(context.Background(), management.Actor{}, 7, 1, json.RawMessage(`{"status":"误报"}`))
	// [决策理由] Action失败不得写入“已解禁”数据库终态。
	if err == nil || fake.reviewCalls != 0 {
		t.Fatalf("failed Update() error=%v reviewCalls=%d", err, fake.reviewCalls)
	}

	// >>> 数据演变示例
	// 1. set_group_ban duration0成功 -> Review(false_positive)。
	// 2. 解禁失败 -> Review不调用 -> 保留pending。
}

// TestViolationResourceHandlerDoesNotUnbanManualReview 验证未自动禁言记录的误报不发送解禁Action。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：仅修改fake仓储调用记录。
func TestViolationResourceHandlerDoesNotUnbanManualReview(t *testing.T) {
	fake := &fakeMonitorRepository{record: management.ResourceRecord{ID: 7, Version: 2}, stored: storedViolation{ID: 7, Version: 1, Data: violationData{GroupID: 100, UserID: 200, Status: statusPendingReview, ActionResult: json.RawMessage(`{"ban_succeeded":false}`)}}}
	actions := &fakeActions{}
	handler := &violationResourceHandler{repository: fake, actions: actions}
	_, err := handler.Update(context.Background(), management.Actor{}, 7, 1, json.RawMessage(`{"status":"误报"}`))
	// [决策理由] Medium人工复核未执行过禁言，误报应直接写反馈而不调用OneBot解禁。
	if err != nil || len(actions.banParams) != 0 || fake.lastStatus != statusFalsePositive {
		t.Fatalf("Update() error=%v bans=%+v status=%q", err, actions.banParams, fake.lastStatus)
	}

	// >>> 数据演变示例
	// 1. ban_succeeded=false+误报 -> Review(false_positive)且0次Action。
	// 2. ban_succeeded=true+误报 -> 由另一测试验证先解禁后终态。
}

// TestBaseLearningFeaturesFiltersModelLabels 验证正向学习只接受原文中的具体有界词。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestBaseLearningFeaturesFiltersModelLabels(t *testing.T) {
	features, err := baseLearningFeatures("免费资料，扫码领取", json.RawMessage(`["免费资料","身份伪装","扫码领取","免费资料"]`))
	// [决策理由] 重复词应去重，不在原文的模型维度标签必须过滤。
	if err != nil || len(features) != 2 || features[0] != "免费资料" || features[1] != "扫码领取" {
		t.Fatalf("baseLearningFeatures() = %+v, %v", features, err)
	}

	// >>> 数据演变示例
	// 1. 原文词+重复+模型标签 -> 两个具体去重词。
	// 2. 非数组JSON -> 返回解析错误。
}
