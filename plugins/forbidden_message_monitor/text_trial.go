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
// ⚠️副作用说明：中风险且已启用LLM时会向配置端点发送测试文本；不执行群管理或持久化。
func (h *textTestResourceHandler) Create(ctx context.Context, _ management.Actor, raw json.RawMessage) (management.ResourceRecord, error) {
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
	// [决策理由] 静态精准证据与真实消息流程一致，直接给出阻止结论。
	if exact.Blocked {
		return textTestRecord(textTestResult{Decision: "违规", Stage: "precise_rule", RiskBand: RiskBandHigh, Reason: strings.Join(exact.Reasons, ","), Violations: learnableExactFeatures(exact.Reasons), SuggestedAction: "block"})
	}
	score := snapshot.engine.Score(payload.Text)
	// [决策理由] 本地高风险分流与真实流程一致，无需调用模型。
	if score.Band == RiskBandHigh {
		return textTestRecord(textTestResult{Decision: "违规", Stage: "weighted_score", RiskBand: score.Band, LocalScore: score.Score, Reason: "加权关键词评分达到高风险阈值", Violations: score.MatchedRisk, SuggestedAction: "block"})
	}
	// [决策理由] 本地低风险分流直接展示放行结果。
	if score.Band == RiskBandLow {
		return textTestRecord(textTestResult{Decision: "放行", Stage: "weighted_score", RiskBand: score.Band, LocalScore: score.Score, Reason: "本地评分低于低风险阈值", Violations: score.MatchedRisk, SuggestedAction: "pass"})
	}
	// [决策理由] 模型未启用时中风险只能展示人工复核，不模拟自动处罚。
	if snapshot.evaluator == nil {
		return textTestRecord(textTestResult{Decision: "人工复核", Stage: "weighted_score", RiskBand: score.Band, LocalScore: score.Score, Reason: "大模型未启用", Violations: score.MatchedRisk, SuggestedAction: "manual_review"})
	}
	llmContext, cancel := context.WithTimeout(ctx, snapshot.llmTimeout)
	defer cancel()
	llmResult, err := snapshot.evaluator.Evaluate(llmContext, LLMEvaluationRequest{Message: payload.Text, BehaviorSummary: "WebUI文本试判，无真实用户行为上下文", Examples: []LLMExample{}})
	// [决策理由] 模型失败不能伪造安全结论，应把测试结果标为人工复核并保留本地证据。
	if err != nil {
		return textTestRecord(textTestResult{Decision: "人工复核", Stage: "llm", RiskBand: score.Band, LocalScore: score.Score, Reason: "大模型研判失败", Violations: score.MatchedRisk, LLMUsed: true, SuggestedAction: "manual_review"})
	}
	decision := textTestDecision(llmResult.SuggestedAction)
	result := textTestResult{Decision: decision, Stage: "llm", RiskBand: score.Band, LocalScore: score.Score, Reason: llmResult.Reason, Violations: llmResult.Violations, LLMUsed: true, LLMRiskLevel: llmResult.RiskLevel, LLMTotalScore: llmResult.TotalScore, SuggestedAction: llmResult.SuggestedAction}
	// >>> 数据演变示例
	// 1. 硬词文本 -> precise_rule -> 违规/block且不调用QQ。
	// 2. 中风险+LLM Safe/pass -> llm -> 放行/pass。
	return textTestRecord(result)
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
