// 📌 影响范围：在进程内检测群消息风险并为Few-shot案例计算文本相似度；不执行禁言、撤回或网络调用。
package forbiddenmessagemonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var meaninglessSingleRunes = map[rune]struct{}{'嗯': {}, '哦': {}, '好': {}, '哈': {}, '啊': {}}
var cqSegmentPattern = regexp.MustCompile(`\[CQ:[^\]]+\]`)

type compiledRule struct {
	text   string
	weight float64
}

// ExactMatch 描述确定性规则命中结果。
type ExactMatch struct {
	Blocked bool
	Reasons []string
}

// ScoreResult 描述加权评分和分流结果。
type ScoreResult struct {
	Score       float64
	Band        RiskBand
	MatchedRisk []string
	MatchedSafe []string
}

// LLMEvaluationRequest 是供应商无关的大模型研判输入。
type LLMEvaluationRequest struct {
	Message         string       `json:"message"`
	BehaviorSummary string       `json:"behavior_summary"`
	Examples        []LLMExample `json:"examples"`
}

// LLMExample 表示已人工确认的动态正反案例。
type LLMExample struct {
	Message  string `json:"message"`
	Violated bool   `json:"violated"`
}

// LLMEvaluationResult 是严格校验的大模型研判输出。
type LLMEvaluationResult struct {
	RiskLevel       string   `json:"risk_level"`
	TotalScore      int      `json:"total_score"`
	Reason          string   `json:"reason"`
	Violations      []string `json:"violations"`
	SuggestedAction string   `json:"suggested_action"`
}

// LLMEvaluator 定义可替换的大模型研判边界，具体供应商由调用方注入。
type LLMEvaluator interface {
	Evaluate(context.Context, LLMEvaluationRequest) (LLMEvaluationResult, error)
}

// Engine 是构造后只读、可并发使用的纯内存检测引擎。
type Engine struct {
	config       EngineConfig
	hardKeywords []string
	riskRules    []compiledRule
	safeRules    []compiledRule
	combinations []CombinationRule
}

// NewEngine 编译并创建检测引擎。
// @param config：检测阈值、词库及组合规则。
// @returns 并发安全的引擎；配置非法时返回错误。
// ⚠️副作用说明：仅分配进程内内存，不启动协程或访问外部资源。
func NewEngine(config EngineConfig) (*Engine, error) {
	// [决策理由] 在接收流量前拒绝危险配置，防止运行时出现无界增长或错误分流。
	if err := config.validate(); err != nil {
		return nil, err
	}
	result := &Engine{
		config:       config,
		hardKeywords: normalizeTerms(config.HardKeywords),
		riskRules:    compileKeywords(config.WeightedKeywords),
		safeRules:    compileKeywords(config.SafeKeywords),
		combinations: config.Combinations,
	}

	// >>> 数据演变示例
	// 1. 合法默认配置 -> 编译关键词与组合规则 -> Engine,nil。
	// 2. 长度归一化基准0 -> validate拒绝 -> nil,error。
	return result, nil
}

// IsValidSpeech 判断消息是否计入活跃用户有效发言。
// @param message：群消息纯文本表示。
// @returns 包含实质Unicode字母或数字且不是无意义单字时为true。
// ⚠️副作用说明：无。
func IsValidSpeech(message string) bool {
	message = cqSegmentPattern.ReplaceAllString(message, "")
	content := make([]rune, 0, len(message))
	for _, current := range []rune(strings.TrimSpace(message)) {
		// [决策理由] 标点、空白、控制字符和符号（含大多数emoji）不构成实质内容。
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			content = append(content, current)
		}
	}
	// [决策理由] 没有字母数字的消息属于纯表情或纯标点。
	if len(content) == 0 {
		return false
	}
	// [决策理由] 方案明确排除常见无意义单字，其余单字仍可能有实质语义。
	if len(content) == 1 {
		_, meaningless := meaninglessSingleRunes[content[0]]
		return !meaningless
	}

	// >>> 数据演变示例
	// 1. "[CQ:face,id=1]👍！！" -> 去除消息段与符号后为空 -> false。
	// 2. "资料" -> 保留两个汉字 -> true。
	return true
}

// CheckExactText 执行确定性硬词检测。
// @param message：待测试消息文本。
// @returns 确定性硬词拦截结果及静态证据。
// ⚠️副作用说明：无。
func (engine *Engine) CheckExactText(message string) ExactMatch {
	lower := strings.ToLower(message)
	reasons := make([]string, 0, 1)
	for _, keyword := range engine.hardKeywords {
		// [决策理由] 硬词由管理员认定为零误报，命中即可确定性拦截。
		if strings.Contains(lower, keyword) {
			reasons = append(reasons, "hard_keyword:"+keyword)
		}
	}
	result := ExactMatch{Blocked: len(reasons) > 0, Reasons: reasons}

	// >>> 数据演变示例
	// 1. "固定广告"且配置为硬词 -> Blocked=true。
	// 2. 媒体file_id或联系方式文本 -> 未配置硬词 -> Blocked=false。
	return result
}

// Score 对未被精准规则拦截的消息计算风险分值。
// @param message：待评分消息文本。
// @returns 含风险词、安全词与低中高分流的结果。
// ⚠️副作用说明：无；评分不写入引擎状态。
func (engine *Engine) Score(message string) ScoreResult {
	lower := strings.ToLower(message)
	score := 0.0
	risk := make([]string, 0)
	safe := make([]string, 0)
	for _, rule := range engine.riskRules {
		// [决策理由] 单词按是否出现计分，避免重复堆词造成无上限放大。
		if strings.Contains(lower, rule.text) {
			score += rule.weight
			risk = append(risk, rule.text)
		}
	}
	for _, rule := range engine.safeRules {
		// [决策理由] 已知安全上下文抵扣风险，但最终分数不会低于零。
		if strings.Contains(lower, rule.text) {
			score -= rule.weight
			safe = append(safe, rule.text)
		}
	}
	for _, combination := range engine.combinations {
		// [决策理由] 仅当组合中的所有特征同时出现时应用联合风险加成。
		if containsAll(lower, combination.Terms) {
			score += combination.Bonus
		}
	}
	runeCount := len([]rune(message))
	// [决策理由] 空消息使用长度1参与计算，避免除零且保持零风险。
	if runeCount == 0 {
		runeCount = 1
	}
	normalization := math.Min(1, float64(engine.config.LengthNormalizationRunes)/float64(runeCount))
	score *= normalization
	// [决策理由] 安全词抵扣后分数限定为零，保持0-正数的直观语义。
	if score < 0 {
		score = 0
	}
	band := RiskBandMedium
	// [决策理由] 低于低阈值的消息无需调用大模型。
	if score < engine.config.LowThreshold {
		band = RiskBandLow
	} else {
		// [决策理由] 达到高阈值的消息可直接处置。
		if score >= engine.config.HighThreshold {
			band = RiskBandHigh
		}
	}
	result := ScoreResult{Score: score, Band: band, MatchedRisk: risk, MatchedSafe: safe}

	// >>> 数据演变示例
	// 1. 风险词30分且低/高阈值20/60 -> score=30 -> medium送LLM。
	// 2. 风险词80分且安全词30分 -> score=50 -> medium而非直接拦截。
	return result
}

// DecodeLLMEvaluationResult 严格解析并校验供应商返回的JSON对象。
// @param payload：单个JSON对象，未知字段或尾随内容不允许。
// @returns 字段和值域合法的研判结果，否则返回错误。
// ⚠️副作用说明：无；不会执行suggested_action。
func DecodeLLMEvaluationResult(payload []byte) (LLMEvaluationResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result LLMEvaluationResult
	// [决策理由] 严格解码可阻止供应商响应漂移被静默忽略。
	if err := decoder.Decode(&result); err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("decode LLM evaluation: %w", err)
	}
	var trailing any
	// [决策理由] 只允许单个JSON值，避免拼接响应产生歧义。
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return LLMEvaluationResult{}, errors.New("LLM evaluation must contain exactly one JSON object")
	}
	// [决策理由] 风险等级必须属于协议枚举。
	if !oneOf(result.RiskLevel, "High", "Medium", "Low", "Safe") {
		return LLMEvaluationResult{}, errors.New("invalid risk_level")
	}
	// [决策理由] 标准分值严格限制在约定范围内。
	if result.TotalScore < 0 || result.TotalScore > 100 {
		return LLMEvaluationResult{}, errors.New("total_score must be between 0 and 100")
	}
	// [决策理由] 处置建议必须属于调用方可显式处理的枚举。
	if !oneOf(result.SuggestedAction, "block", "manual_review", "pass") {
		return LLMEvaluationResult{}, errors.New("invalid suggested_action")
	}
	// [决策理由] 风险等级与动作矛盾时不能选择性信任字段，必须视为模型协议失败并安全降级。
	if (result.RiskLevel == "High" && result.SuggestedAction != "block") || (result.RiskLevel == "Medium" && result.SuggestedAction != "manual_review") || ((result.RiskLevel == "Low" || result.RiskLevel == "Safe") && result.SuggestedAction != "pass") {
		return LLMEvaluationResult{}, errors.New("risk_level and suggested_action are inconsistent")
	}
	// [决策理由] 可审计研判必须提供理由且敏感词数组不能为null。
	if strings.TrimSpace(result.Reason) == "" || result.Violations == nil {
		return LLMEvaluationResult{}, errors.New("reason and violations are required")
	}

	// >>> 数据演变示例
	// 1. 合法High/90/block对象 -> 完整校验 -> result,nil。
	// 2. 含unknown字段对象 -> DisallowUnknownFields拒绝 -> zero,error。
	return result, nil
}

// normalizeTerms 归一化并去重词条。
// @param terms：原始词条。
// @returns 小写、去空白、稳定排序后的非空词条。
// ⚠️副作用说明：无；不修改输入切片。
func normalizeTerms(terms []string) []string {
	unique := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		normalized := strings.ToLower(strings.TrimSpace(term))
		// [决策理由] 空词会匹配任意消息，因此必须丢弃。
		if normalized != "" {
			unique[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for term := range unique {
		result = append(result, term)
	}
	sort.Strings(result)

	// >>> 数据演变示例
	// 1. [" 加群 ","加群"] -> 去空白去重 -> ["加群"]。
	// 2. [""] -> 丢弃空词 -> []。
	return result
}

// compileKeywords 编译加权词并统一大小写。
// @param keywords：已校验的加权词。
// @returns 供评分使用的内部规则副本。
// ⚠️副作用说明：无。
func compileKeywords(keywords []WeightedKeyword) []compiledRule {
	result := make([]compiledRule, 0, len(keywords))
	for _, keyword := range keywords {
		result = append(result, compiledRule{text: strings.ToLower(keyword.Text), weight: keyword.Weight})
	}

	// >>> 数据演变示例
	// 1. [{"VX",20}] -> [{"vx",20}]。
	// 2. [] -> []。
	return result
}

// containsAll 判断组合词是否全部出现。
// @param message：已小写消息；terms：组合词。
// @returns 组合非空且每个词均出现时为true。
// ⚠️副作用说明：无。
func containsAll(message string, terms []string) bool {
	// [决策理由] 空组合不应无条件获得加成。
	if len(terms) == 0 {
		return false
	}
	for _, term := range terms {
		// [决策理由] 缺少任一特征即不满足联合风险语义。
		if !strings.Contains(message, strings.ToLower(term)) {
			return false
		}
	}

	// >>> 数据演变示例
	// 1. "免费加群"+["免费","加群"] -> 全部命中 -> true。
	// 2. "免费"+["免费","加群"] -> 缺少加群 -> false。
	return true
}

// normalizeSimilarity 提取小写Unicode字母与数字用于相似比较。
// @param message：原始消息。
// @returns 移除空白、标点和符号后的标准文本。
// ⚠️副作用说明：无。
func normalizeSimilarity(message string) string {
	var builder strings.Builder
	for _, current := range strings.ToLower(message) {
		// [决策理由] 只保留有语义的字母数字，减少标点规避对相似度的影响。
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			builder.WriteRune(current)
		}
	}
	result := builder.String()

	// >>> 数据演变示例
	// 1. "免费！ 加群" -> 去空白标点 -> "免费加群"。
	// 2. "👍" -> 无字母数字 -> ""。
	return result
}

// similarity 计算两个标准文本的Unicode bigram Jaccard相似度。
// @param left、right：标准文本。
// @returns 0到1的相似度；两个空文本不视为相似案例。
// ⚠️副作用说明：无。
func similarity(left, right string) float64 {
	// [决策理由] 空文本（如纯emoji）没有可用于Few-shot案例排序的语义特征。
	if left == "" || right == "" {
		return 0
	}
	// [决策理由] 完全相同可直接返回并覆盖单rune文本场景。
	if left == right {
		return 1
	}
	leftSet := runeBigrams(left)
	rightSet := runeBigrams(right)
	intersection := 0
	for token := range leftSet {
		// [决策理由] 交集只统计双方共有的bigram。
		if _, exists := rightSet[token]; exists {
			intersection++
		}
	}
	union := len(leftSet) + len(rightSet) - intersection
	// [决策理由] 理论上的空并集作为防御边界返回0。
	if union == 0 {
		return 0
	}
	result := float64(intersection) / float64(union)

	// >>> 数据演变示例
	// 1. "免费加群"与自身 -> 快速路径 -> 1。
	// 2. "免费加群"与"天气不错" -> bigram无交集 -> 0。
	return result
}

// runeBigrams 构造Unicode字符二元组集合。
// @param value：标准文本。
// @returns 单字符时含该字符，否则含相邻二元组的集合。
// ⚠️副作用说明：无。
func runeBigrams(value string) map[string]struct{} {
	runes := []rune(value)
	result := make(map[string]struct{})
	// [决策理由] 单字符文本保留自身作为唯一特征。
	if len(runes) == 1 {
		result[string(runes)] = struct{}{}
		return result
	}
	for index := 0; index+1 < len(runes); index++ {
		result[string(runes[index:index+2])] = struct{}{}
	}

	// >>> 数据演变示例
	// 1. "加群" -> {"加群"}。
	// 2. "免费群" -> {"免费","费群"}。
	return result
}

// oneOf 判断字符串是否属于枚举集合。
// @param value：候选值；allowed：允许值列表。
// @returns 命中任一允许值时为true。
// ⚠️副作用说明：无。
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		// [决策理由] 枚举采用严格大小写匹配，避免供应商输出漂移。
		if value == candidate {
			return true
		}
	}

	// >>> 数据演变示例
	// 1. "High"+["High","Low"] -> 命中 -> true。
	// 2. "high"+["High"] -> 大小写不符 -> false。
	return false
}
