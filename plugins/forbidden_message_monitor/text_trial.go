// 📌 影响范围：读取违禁监控运行时规则并可调用已配置的大模型；不访问QQ、不写数据库。
package forbiddenmessagemonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

const maxTextTestRunes = 4000
const trustedTextTrialTTL = 10 * time.Minute
const maxTrustedTextTrials = 1000
const maxJavaScriptSafeInteger int64 = 9007199254740991

type trustedTextTrial struct {
	ActorID  string
	Text     string
	Features []string
	Expires  time.Time
}

type textTestResourceHandler struct{ owner *implementation }

type textTestPayload struct {
	Text string `json:"text"`
}

type textTestResult struct {
	Decision        string   `json:"decision"`
	Stage           string   `json:"stage"`
	RiskBand        RiskBand `json:"risk_band"`
	LocalScore      float64  `json:"local_score"`
	Reason          string   `json:"reason"`
	Violations      []string `json:"violations"`
	LLMUsed         bool     `json:"llm_used"`
	LLMRiskLevel    string   `json:"llm_risk_level,omitempty"`
	LLMTotalScore   int      `json:"llm_total_score,omitempty"`
	SuggestedAction string   `json:"suggested_action"`
}

// List 返回无持久化试判记录的空分页。
// @param ctx/actor/query：通用资源查询参数。
// @returns 与请求分页一致的空结果。
// ⚠️副作用说明：无。
func (h *textTestResourceHandler) List(_ context.Context, _ management.Actor, query management.ResourceQuery) (management.ResourcePage, error) {
	result := management.ResourcePage{Items: []management.ResourceRecord{}, Page: query.Page, PageSize: query.PageSize, Total: 0}
	// >>> 数据演变示例
	// 1. page1,size20 -> 空items,total0。
	// 2. page2,size10 -> 空items,page2。
	return result, nil
}

// Create 对输入文本执行无副作用试判。
// @param ctx：请求上下文；actor：已授权管理员；raw：仅含text的JSON。
// @returns 包含本地规则与可选LLM结论的临时资源记录。
// ⚠️副作用说明：需要模型时会占用并发与当日请求额度并发送测试文本；不执行群管理或写违规审计。
func (h *textTestResourceHandler) Create(ctx context.Context, actor management.Actor, raw json.RawMessage) (management.ResourceRecord, error) {
	payload, err := decodeTextTestPayload(raw)
	// [决策理由] 无效或过长文本不得进入规则与外部模型。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	snapshot := h.owner.snapshot.Load()
	// [决策理由] 配置尚未发布时不存在权威试判规则。
	if snapshot == nil || snapshot.engine == nil {
		return management.ResourceRecord{}, fmt.Errorf("违禁消息检测配置尚未初始化")
	}
	exact := snapshot.engine.CheckExactText(payload.Text)
	score := snapshot.engine.Score(payload.Text)
	// [决策理由] 硬词和本地高风险先于模型长度门槛，短消息也不能绕过确定性规则。
	if exact.Blocked {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "违规", Stage: "precise_rule", RiskBand: RiskBandHigh, Reason: strings.Join(exact.Reasons, ","), Violations: learnableExactFeatures(exact.Reasons), SuggestedAction: "block"})
	}
	// [决策理由] 本地高风险无需调用模型，冷启动模式也保留该零成本处置。
	if score.Band == RiskBandHigh {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "违规", Stage: "weighted_score", RiskBand: score.Band, LocalScore: score.Score, Reason: "加权关键词评分达到高风险阈值", Violations: score.MatchedRisk, SuggestedAction: "block"})
	}
	// [决策理由] 仅即将进入模型的消息应用长度门槛。
	if messageBelowLLMMinimum(payload.Text, snapshot.minLLMMessageLength) {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "放行", Stage: "llm_length_filter", RiskBand: score.Band, LocalScore: score.Score, Reason: fmt.Sprintf("消息短于大模型最短检测长度%d", snapshot.minLLMMessageLength), Violations: score.MatchedRisk, SuggestedAction: "pass"})
	}
	// [决策理由] 常规模式低风险无需调用模型，冷启动模式则继续模型研判。
	if snapshot.detectionMode != detectionModeColdStart && score.Band == RiskBandLow {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "放行", Stage: "weighted_score", RiskBand: score.Band, LocalScore: score.Score, Reason: "本地评分低于低风险阈值", Violations: score.MatchedRisk, SuggestedAction: "pass"})
	}
	// [决策理由] 模型未启用时中风险只能展示人工复核，不模拟自动处罚。
	if snapshot.evaluator == nil {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "人工复核", Stage: "weighted_score", RiskBand: score.Band, LocalScore: score.Score, Reason: "大模型未启用", Violations: score.MatchedRisk, SuggestedAction: "manual_review"})
	}
	// [决策理由] 文本试判与真实流量共享并发上限，防止管理页面绕过资源保护。
	if !h.owner.tryAcquireLLM(snapshot.llmMaxConcurrency) {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "人工复核", Stage: "llm", RiskBand: score.Band, LocalScore: score.Score, Reason: "大模型并发已满", Violations: score.MatchedRisk, SuggestedAction: "manual_review"})
	}
	defer h.owner.releaseLLM()
	now := time.Now().UTC()
	// [决策理由] 测试实例可注入稳定时钟，生产实例使用同一插件时钟计算UTC日额度。
	if h.owner.now != nil {
		now = h.owner.now().UTC()
	}
	reserved, err := h.owner.repository.TryReserveLLMRequest(ctx, now, snapshot.llmDailyRequestLimit)
	// [决策理由] 额度状态不可确认或已耗尽时不得绕过预算调用外部模型。
	if err != nil || !reserved {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "人工复核", Stage: "llm", RiskBand: score.Band, LocalScore: score.Score, Reason: "大模型每日额度不可用", Violations: score.MatchedRisk, SuggestedAction: "manual_review"})
	}
	llmContext, cancel := context.WithTimeout(ctx, snapshot.llmTimeout)
	defer cancel()
	llmResult, err := snapshot.evaluator.Evaluate(llmContext, LLMEvaluationRequest{Message: payload.Text, BehaviorSummary: "WebUI文本试判，无真实用户行为上下文", Examples: []LLMExample{}})
	// [决策理由] 模型失败不能伪造安全结论，应把测试结果标为人工复核并保留本地证据。
	if err != nil {
		return h.trustedRecord(actor, payload.Text, textTestResult{Decision: "人工复核", Stage: "llm", RiskBand: score.Band, LocalScore: score.Score, Reason: "大模型研判失败", Violations: score.MatchedRisk, LLMUsed: true, SuggestedAction: "manual_review"})
	}
	decision := textTestDecision(llmResult.SuggestedAction)
	result := textTestResult{Decision: decision, Stage: "llm", RiskBand: score.Band, LocalScore: score.Score, Reason: llmResult.Reason, Violations: llmResult.Violations, LLMUsed: true, LLMRiskLevel: llmResult.RiskLevel, LLMTotalScore: llmResult.TotalScore, SuggestedAction: llmResult.SuggestedAction}
	// >>> 数据演变示例
	// 1. 硬词文本 -> precise_rule -> 违规/block且不调用QQ。
	// 2. 中风险+LLM Safe/pass -> llm -> 放行/pass。
	return h.trustedRecord(actor, payload.Text, result)
}

// trustedRecord 编码试判结果并缓存服务端可信特征供一次主动投喂复用。
// @param actor：管理员身份；text：试判原文；result：服务端判定结果。
// @returns 临时资源记录或编码错误。
// ⚠️副作用说明：在进程内保存十分钟、按管理员和试判ID绑定的可信特征。
func (h *textTestResourceHandler) trustedRecord(actor management.Actor, text string, result textTestResult) (management.ResourceRecord, error) {
	record, err := textTestRecord(result)
	// [决策理由] 只有成功编码且身份可审计的试判才能生成投喂凭证。
	if err != nil || strings.TrimSpace(actor.ID) == "" {
		return record, err
	}
	rawFeatures, err := json.Marshal(result.Violations)
	// [决策理由] 无法规范化的特征不得缓存为学习证据。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	features, err := baseLearningFeatures(text, rawFeatures)
	// [决策理由] 特征必须真实存在于原文才能成为服务端可信凭证。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	h.owner.trialMu.Lock()
	defer h.owner.trialMu.Unlock()
	// [决策理由] 缓存按需初始化，避免未使用文本试判的实例分配状态。
	if h.owner.trials == nil {
		h.owner.trials = make(map[int64]trustedTextTrial)
	}
	now := time.Now().UTC()
	// [决策理由] 凭证创建与消费必须使用插件统一时钟，测试注入和生产行为才能一致。
	if h.owner.now != nil {
		now = h.owner.now()
	}
	for id, trial := range h.owner.trials {
		// [决策理由] 过期凭证不再可用，写入新凭证时顺便清理以限制长期内存占用。
		if now.After(trial.Expires) {
			delete(h.owner.trials, id)
		}
	}
	// [决策理由] 管理员高频试判必须保持硬上限；满载时逐出最早到期凭证，避免已产生的试判结果因缓存容量而失败。
	if len(h.owner.trials) >= maxTrustedTextTrials {
		var oldestID int64
		var oldestExpiry time.Time
		for id, trial := range h.owner.trials {
			// [决策理由] 首项或更早到期项是容量逐出的最小影响目标。
			if oldestID == 0 || trial.Expires.Before(oldestExpiry) {
				oldestID = id
				oldestExpiry = trial.Expires
			}
		}
		delete(h.owner.trials, oldestID)
	}
	for attempts := 0; attempts <= len(h.owner.trials); attempts++ {
		h.owner.trialSequence++
		// [决策理由] 每次尝试都执行回绕，碰撞在边界值时也不会产生JavaScript不安全整数。
		if h.owner.trialSequence > maxJavaScriptSafeInteger {
			h.owner.trialSequence = 1
		}
		_, occupied := h.owner.trials[h.owner.trialSequence]
		// [决策理由] 首个未占用的安全整数即可作为进程内短时凭证ID。
		if !occupied {
			record.ID = h.owner.trialSequence
			break
		}
	}
	// [决策理由] 有界缓存理论上总能找到空ID，防御性检查避免未来约束变化时覆盖凭证。
	if record.ID == 0 {
		return management.ResourceRecord{}, fmt.Errorf("无法分配文本试判凭证")
	}
	h.owner.trials[record.ID] = trustedTextTrial{ActorID: actor.ID, Text: text, Features: features, Expires: now.Add(trustedTextTrialTTL)}

	// >>> 数据演变示例
	// 1. trial7+管理员A+[扫码] -> 缓存十分钟 -> 可保存一次。
	// 2. 空模型词 -> 缓存空特征 -> 可保存Few-shot正例但不晋级候选词。
	return record, nil
}

// Update 拒绝修改临时试判结果。
// @param ctx/actor/id/version/raw：通用资源更新参数。
// @returns ErrInvalidResourceData。
// ⚠️副作用说明：无。
func (h *textTestResourceHandler) Update(context.Context, management.Actor, int64, int64, json.RawMessage) (management.ResourceRecord, error) {
	// >>> 数据演变示例
	// 1. PATCH试判结果 -> 拒绝。
	// 2. 空载荷PATCH -> 拒绝。
	return management.ResourceRecord{}, management.ErrInvalidResourceData
}

// Delete 拒绝删除不存在的试判记录。
// @param ctx/actor/id/version：通用资源删除参数。
// @returns ErrInvalidResourceData。
// ⚠️副作用说明：无。
func (h *textTestResourceHandler) Delete(context.Context, management.Actor, int64, int64) error {
	// >>> 数据演变示例
	// 1. DELETE id1 -> 拒绝。
	// 2. DELETE id0 -> 拒绝。
	return management.ErrInvalidResourceData
}

// decodeTextTestPayload 严格解析并限制测试文本。
// @param raw：仅允许text字段的JSON对象。
// @returns 去首尾空白的合法文本或领域输入错误。
// ⚠️副作用说明：无。
func decodeTextTestPayload(raw json.RawMessage) (textTestPayload, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload textTestPayload
	// [决策理由] 类型错误和未知字段不得进入检测器。
	if err := decoder.Decode(&payload); err != nil {
		return textTestPayload{}, fmt.Errorf("%w: %v", management.ErrInvalidResourceData, err)
	}
	var trailing any
	err := decoder.Decode(&trailing)
	// [决策理由] 只有EOF表示请求仅包含一个JSON值。
	if !errors.Is(err, io.EOF) {
		return textTestPayload{}, fmt.Errorf("%w: 必须仅提交一个JSON对象", management.ErrInvalidResourceData)
	}
	payload.Text = strings.TrimSpace(payload.Text)
	// [决策理由] 空文本没有检测意义，长度上限防止模型费用与内存被滥用。
	if payload.Text == "" || len([]rune(payload.Text)) > maxTextTestRunes {
		return textTestPayload{}, fmt.Errorf("%w: text必须为1到%d个字符", management.ErrInvalidResourceData, maxTextTestRunes)
	}
	// >>> 数据演变示例
	// 1. {text:" 免费 "} -> trim -> "免费"。
	// 2. {text:"",extra:1} -> 未知字段/空值 -> ErrInvalidResourceData。
	return payload, nil
}

// textTestRecord 编码临时试判结果。
// @param result：完整试判结论。
// @returns id/version固定为1的临时资源记录或编码错误。
// ⚠️副作用说明：分配JSON内存，不持久化。
func textTestRecord(result textTestResult) (management.ResourceRecord, error) {
	raw, err := json.Marshal(result)
	// [决策理由] 编码失败时不能向前端返回缺字段结论。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	record := management.ResourceRecord{ID: time.Now().UnixNano(), Version: 1, Data: raw}
	// >>> 数据演变示例
	// 1. block结果 -> JSON记录version1。
	// 2. pass结果 -> JSON记录version1且不落库。
	return record, nil
}

// textTestDecision 把稳定动作枚举转换为中文结论。
// @param action：block、manual_review或pass。
// @returns 违规、人工复核或放行。
// ⚠️副作用说明：无。
func textTestDecision(action string) string {
	result := "人工复核"
	// [决策理由] 仅明确block视为违规。
	if action == "block" {
		result = "违规"
	} else {
		// [决策理由] 仅明确pass视为放行，其余安全回退人工复核。
		if action == "pass" {
			result = "放行"
		}
	}
	// >>> 数据演变示例
	// 1. block -> 违规。
	// 2. pass -> 放行；unknown -> 人工复核。
	return result
}
