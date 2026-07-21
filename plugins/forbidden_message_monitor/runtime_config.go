// 📌 影响范围：声明违禁消息监控配置并调用管理员指定的 OpenAI-compatible HTTPS/HTTP 大模型端点；API Key 仅用于请求鉴权。
package forbiddenmessagemonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

const maxLLMResponseBytes = 1 << 20

const (
	detectionModeStandard  = "standard"
	detectionModeColdStart = "cold_start"
)

type pluginConfig struct {
	HardKeywordsJSON     string `json:"hard_keywords_json"`
	WeightedKeywordsJSON string `json:"weighted_keywords_json"`
	SafeKeywordsJSON     string `json:"safe_keywords_json"`
	CombinationsJSON     string `json:"combinations_json"`
	LowThreshold         int    `json:"low_threshold"`
	HighThreshold        int    `json:"high_threshold"`
	DetectionMode        string `json:"detection_mode"`
	LLMEnabled           bool   `json:"llm_enabled"`
	LLMEndpoint          string `json:"llm_endpoint"`
	LLMModel             string `json:"llm_model"`
	LLMAPIKey            string `json:"llm_api_key"`
	LLMTimeoutSeconds    int    `json:"llm_timeout_seconds"`
	LLMMaxConcurrency    int    `json:"llm_max_concurrency"`
	LLMDailyRequestLimit int    `json:"llm_daily_request_limit"`
	MinLLMMessageLength  int    `json:"min_llm_message_length"`
}

type runtimeSnapshot struct {
	engine               *Engine
	engineConfig         EngineConfig
	evaluator            LLMEvaluator
	llmTimeout           time.Duration
	detectionMode        string
	llmMaxConcurrency    int
	llmDailyRequestLimit int
	minLLMMessageLength  int
}

type openAICompatibleEvaluator struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

// ConfigSchema 声明检测规则、分流阈值和可选大模型设置。
// @param 无。
// @returns 可由通用 WebUI 渲染且密钥 write-only 的配置 Schema。
// ⚠️副作用说明：无。
func (p *implementation) ConfigSchema() plugin.ConfigSchema {
	result := plugin.ConfigSchema{Fields: []plugin.ConfigField{
		{Key: "hard_keywords_json", DisplayName: "硬性关键词", Description: "确定性零误报词；逐条添加，无需手写JSON", Type: plugin.FieldStringListJSON, Default: json.RawMessage(`"[]"`)},
		{Key: "weighted_keywords_json", DisplayName: "风险词权重", Description: "逐条设置风险词及其加分", Type: plugin.FieldWeightedTermsJSON, Default: json.RawMessage(`"[]"`)},
		{Key: "safe_keywords_json", DisplayName: "安全词抵扣", Description: "逐条设置安全词及其抵扣分值", Type: plugin.FieldWeightedTermsJSON, Default: json.RawMessage(`"[]"`)},
		{Key: "combinations_json", DisplayName: "组合加成", Description: "设置需同时出现的关键词（逗号分隔）及组合加分", Type: plugin.FieldCombinationRulesJSON, Default: json.RawMessage(`"[]"`)},
		{Key: "low_threshold", DisplayName: "低风险阈值", Description: "低于此分值直接放行", Type: plugin.FieldInteger, Default: json.RawMessage(`20`)},
		{Key: "high_threshold", DisplayName: "高风险阈值", Description: "达到此分值直接处置", Type: plugin.FieldInteger, Default: json.RawMessage(`60`)},
		{Key: "min_llm_message_length", DisplayName: "大模型最短消息长度", Description: "仅在消息即将进入大模型时生效；短于该Unicode字符数则直接放行，不影响硬关键词和本地高风险处置", Type: plugin.FieldInteger, Default: json.RawMessage(`30`)},
		{Key: "detection_mode", DisplayName: "检测模式", Description: "常规模式仅中风险调用模型；冷启动模式将所有未被白名单或本地高风险规则处理的消息提交模型", Type: plugin.FieldEnum, Default: json.RawMessage(`"standard"`), Options: []string{detectionModeStandard, detectionModeColdStart}},
		{Key: "llm_enabled", DisplayName: "启用大模型研判", Description: "启用后会将群消息原文、近期行为摘要及近期人工正反案例发送到所配置的外部服务", Type: plugin.FieldBoolean, Default: json.RawMessage(`false`)},
		{Key: "llm_endpoint", DisplayName: "大模型接口地址", Description: "OpenAI-compatible chat/completions 完整 URL；远程服务必须使用 HTTPS，HTTP 仅允许本机回环地址", Type: plugin.FieldString, Default: json.RawMessage(`""`)},
		{Key: "llm_model", DisplayName: "大模型名称", Type: plugin.FieldString, Default: json.RawMessage(`""`)},
		{Key: "llm_api_key", DisplayName: "大模型 API Key", Description: "只写字段，留空保留原值", Type: plugin.FieldSecret},
		{Key: "llm_timeout_seconds", DisplayName: "大模型超时秒数", Type: plugin.FieldInteger, Default: json.RawMessage(`20`)},
		{Key: "llm_max_concurrency", DisplayName: "大模型最大并发", Description: "达到并发上限时新消息安全放行", Type: plugin.FieldInteger, Default: json.RawMessage(`2`)},
		{Key: "llm_daily_request_limit", DisplayName: "大模型每日请求上限", Description: "按UTC自然日跨重启累计，达到上限后消息安全放行", Type: plugin.FieldInteger, Default: json.RawMessage(`500`)},
	}}

	// >>> 数据演变示例
	// 1. 新插件 -> 默认空词库+阈值20/60+LLM关闭 -> 可启用本地规则。
	// 2. WebUI读取 -> llm_api_key被平台脱敏 -> 其余配置可编辑。
	return result
}

// ValidateConfig 校验完整插件配置且不发布运行状态。
// @param ctx：校验上下文；raw：平台规范化后的完整 JSON。
// @returns 取消、JSON、URL、阈值或检测引擎配置错误。
// ⚠️副作用说明：无；不发起大模型请求。
func (p *implementation) ValidateConfig(ctx context.Context, raw json.RawMessage) error {
	// [决策理由] 已取消的管理请求不应继续解析配置。
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := buildRuntimeSnapshot(raw, p.httpClient)

	// >>> 数据演变示例
	// 1. 空词库+20/60+LLM关闭 -> Engine构造成功 -> nil。
	// 2. LLM启用但endpoint为空 -> 领域校验 -> error。
	return err
}

// ApplyConfig 原子发布检测引擎和大模型客户端快照。
// @param ctx：应用上下文；raw：平台规范化后的完整 JSON。
// @returns 取消或配置构造错误。
// ⚠️副作用说明：成功时替换后续群消息读取的运行快照；不立即访问外部服务。
func (p *implementation) ApplyConfig(ctx context.Context, raw json.RawMessage) error {
	// [决策理由] 调用方取消后不得发布已经放弃的配置。
	if err := ctx.Err(); err != nil {
		return err
	}
	next, err := buildRuntimeSnapshot(raw, p.httpClient)
	// [决策理由] 只有完整解析和构造成功才能替换旧快照。
	if err != nil {
		return err
	}
	p.snapshot.Store(next)
	currentOffsets := p.offsets.Load()
	// [决策理由] 配置热更新后必须重新叠加当日反馈补丁，避免偏移提前失效。
	if currentOffsets != nil {
		negativeFeatures := map[string]struct{}{}
		// [决策理由] 热更新必须保留哪些词已有误报证据，不能仅凭正负净权重恢复硬拦截。
		if currentNegative := p.negative.Load(); currentNegative != nil {
			negativeFeatures = *currentNegative
		}
		return p.publishWeightOffsets(*currentOffsets, negativeFeatures)
	}

	// >>> 数据演变示例
	// 1. 旧阈值20/60+新阈值30/70 -> Store新快照 -> 后续消息使用30/70。
	// 2. 新词库JSON损坏 -> 不Store -> 旧引擎继续服务。
	return nil
}

// buildRuntimeSnapshot 严格解析配置并构造不可变运行依赖。
// @param raw：完整配置 JSON；client：共享且带全局连接复用的 HTTP Client。
// @returns 可原子发布的快照或配置错误。
// ⚠️副作用说明：构造规则快照并分配内存；不发起网络请求。
func buildRuntimeSnapshot(raw json.RawMessage, client *http.Client) (*runtimeSnapshot, error) {
	var config pluginConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	// [决策理由] 插件自身必须防御绕过平台直接调用时的未知字段。
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("解析违禁监控配置: %w", err)
	}
	// [决策理由] 兼容引入检测模式前保存的配置和包内构造调用，缺失值按常规模式解释。
	if config.DetectionMode == "" {
		config.DetectionMode = detectionModeStandard
	}
	// [决策理由] 兼容新增限额字段前的包内配置和历史快照，缺失值采用保守默认值。
	if config.LLMMaxConcurrency == 0 {
		config.LLMMaxConcurrency = 2
	}
	// [决策理由] 历史配置没有每日额度字段时使用明确有限的默认请求数。
	if config.LLMDailyRequestLimit == 0 {
		config.LLMDailyRequestLimit = 500
	}
	// [决策理由] 历史配置缺少最短模型消息长度时采用新功能默认值30。
	if config.MinLLMMessageLength == 0 {
		config.MinLLMMessageLength = 30
	}
	engineConfig := DefaultEngineConfig()
	engineConfig.LowThreshold = float64(config.LowThreshold)
	engineConfig.HighThreshold = float64(config.HighThreshold)
	// [决策理由] 四个 JSON 文本必须全部严格解码，避免部分词库静默失效。
	if err := decodeStrictJSONText(config.HardKeywordsJSON, &engineConfig.HardKeywords); err != nil {
		return nil, fmt.Errorf("解析硬性关键词: %w", err)
	}
	// [决策理由] 风险词权重决定自动处置，结构错误必须安全失败。
	if err := decodeStrictJSONText(config.WeightedKeywordsJSON, &engineConfig.WeightedKeywords); err != nil {
		return nil, fmt.Errorf("解析风险词权重: %w", err)
	}
	// [决策理由] 安全词影响误报率，结构错误不能回退为空列表。
	if err := decodeStrictJSONText(config.SafeKeywordsJSON, &engineConfig.SafeKeywords); err != nil {
		return nil, fmt.Errorf("解析安全词权重: %w", err)
	}
	// [决策理由] 组合规则参与高风险分流，必须作为完整数组校验。
	if err := decodeStrictJSONText(config.CombinationsJSON, &engineConfig.Combinations); err != nil {
		return nil, fmt.Errorf("解析组合加成: %w", err)
	}
	engine, err := NewEngine(engineConfig)
	// [决策理由] 引擎阈值或容量非法时不能发布不可运行快照。
	if err != nil {
		return nil, fmt.Errorf("构造检测引擎: %w", err)
	}
	// [决策理由] 外部请求必须具有有限超时，避免占满事件 worker。
	if config.LLMTimeoutSeconds < 1 || config.LLMTimeoutSeconds > 120 {
		return nil, fmt.Errorf("llm_timeout_seconds 必须在1到120之间")
	}
	// [决策理由] 并发必须为小型正整数，避免外部模型迟滞耗尽事件处理资源。
	if config.LLMMaxConcurrency < 1 || config.LLMMaxConcurrency > 32 {
		return nil, fmt.Errorf("llm_max_concurrency 必须在1到32之间")
	}
	// [决策理由] 每日额度必须为正且有合理上界，防止误配置导致无界费用。
	if config.LLMDailyRequestLimit < 1 || config.LLMDailyRequestLimit > 1000000 {
		return nil, fmt.Errorf("llm_daily_request_limit 必须在1到1000000之间")
	}
	// [决策理由] 模型长度门槛必须有界，避免误配置让全部合法消息永久绕过研判。
	if config.MinLLMMessageLength < 1 || config.MinLLMMessageLength > maxTextTestRunes {
		return nil, fmt.Errorf("min_llm_message_length 必须在1到%d之间", maxTextTestRunes)
	}
	// [决策理由] 检测模式决定低风险消息是否会携带群消息原文发往外部模型，必须显式限制为已声明值。
	if config.DetectionMode != detectionModeStandard && config.DetectionMode != detectionModeColdStart {
		return nil, fmt.Errorf("detection_mode 必须为 standard 或 cold_start")
	}
	// [决策理由] 冷启动模式承诺所有非白名单消息接受模型研判，关闭模型会造成静默全量放行。
	if config.DetectionMode == detectionModeColdStart && !config.LLMEnabled {
		return nil, fmt.Errorf("冷启动模式必须启用大模型")
	}
	result := &runtimeSnapshot{engine: engine, engineConfig: engineConfig, llmTimeout: time.Duration(config.LLMTimeoutSeconds) * time.Second, detectionMode: config.DetectionMode, llmMaxConcurrency: config.LLMMaxConcurrency, llmDailyRequestLimit: config.LLMDailyRequestLimit, minLLMMessageLength: config.MinLLMMessageLength}
	// [决策理由] LLM关闭时不得要求凭据或创建外部调用器。
	if !config.LLMEnabled {
		return result, nil
	}
	endpoint, err := validateLLMEndpoint(config.LLMEndpoint)
	// [决策理由] 无效端点可能导致请求泄漏到意外协议或带用户信息的地址。
	if err != nil {
		return nil, err
	}
	// [决策理由] 模型名为空时服务端无法选择研判模型。
	if strings.TrimSpace(config.LLMModel) == "" {
		return nil, fmt.Errorf("启用大模型时 llm_model 不能为空")
	}
	result.evaluator = &openAICompatibleEvaluator{endpoint: endpoint, model: config.LLMModel, apiKey: config.LLMAPIKey, client: secureLLMHTTPClient(client)}

	// >>> 数据演变示例
	// 1. LLM关闭 -> engine+nil evaluator -> 中风险转人工。
	// 2. LLM开启+合法endpoint/model -> engine+HTTP evaluator -> 中风险调用模型。
	return result, nil
}

// secureLLMHTTPClient 为每次重定向重新应用LLM端点传输安全规则。
// @param client：共享连接配置；nil时使用标准客户端默认值。
// @returns 浅拷贝且拒绝不安全重定向的HTTP客户端。
// ⚠️副作用说明：分配客户端副本；不修改调用方传入实例。
func secureLLMHTTPClient(client *http.Client) *http.Client {
	result := &http.Client{}
	// [决策理由] 保留调用方Transport和Timeout等配置，同时避免覆盖共享客户端的重定向策略。
	if client != nil {
		clone := *client
		result = &clone
	}
	previousCheck := result.CheckRedirect
	result.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		// [决策理由] 每一跳都必须满足HTTPS或本机回环HTTP，防止安全初始地址降级外发隐私。
		if _, err := validateLLMEndpoint(request.URL.String()); err != nil {
			return err
		}
		// [决策理由] 调用方更严格的跳数或域名策略仍需保留。
		if previousCheck != nil {
			return previousCheck(request, via)
		}
		// [决策理由] 标准库默认最多允许10次重定向，复制该边界防止循环耗尽worker。
		if len(via) >= 10 {
			return fmt.Errorf("大模型重定向次数过多")
		}
		return nil
	}
	// >>> 数据演变示例
	// 1. HTTPS -> HTTPS跳转 -> 每跳校验通过 -> 继续请求。
	// 2. localhost HTTP -> remote HTTP跳转 -> 校验拒绝且不发送消息。
	return result
}

// decodeStrictJSONText 解码 WebUI multiline 中承载的单个 JSON 值。
// @param text：JSON 文本；target：强类型目标指针。
// @returns 未知字段、尾随内容或类型错误。
// ⚠️副作用说明：修改 target 指向的临时构造值。
func decodeStrictJSONText(text string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	// [决策理由] 规则结构必须精确匹配强类型定义。
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	// [决策理由] 尾随 JSON 会让管理员看到的配置与实际解析不一致。
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("只能包含一个 JSON 值")
	}

	// >>> 数据演变示例
	// 1. [{"text":"免费","weight":20}] -> []WeightedKeyword -> nil。
	// 2. []{} -> 第二个JSON值 -> error。
	return nil
}

// validateLLMEndpoint 校验管理员配置的 OpenAI-compatible URL。
// @param raw：端点文本。
// @returns 规范化 URL 或协议、主机、用户信息错误。
// ⚠️副作用说明：无。
func validateLLMEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	// [决策理由] 仅允许明确的HTTP(S)绝对地址，拒绝文件和自定义协议。
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", fmt.Errorf("llm_endpoint 必须是 HTTP(S) 绝对地址")
	}
	// [决策理由] URL用户信息容易在错误文本或代理日志泄露凭据。
	if parsed.User != nil {
		return "", fmt.Errorf("llm_endpoint 不能包含用户信息")
	}
	hostname := strings.ToLower(parsed.Hostname())
	loopback := hostname == "localhost"
	// [决策理由] 数字IP必须由标准库判断回环范围，覆盖127/8与IPv6 ::1。
	if address := net.ParseIP(hostname); address != nil {
		loopback = address.IsLoopback()
	}
	// [决策理由] 远程HTTP会明文传输API Key和群消息隐私，仅本机回环开发服务可例外。
	if parsed.Scheme == "http" && !loopback {
		return "", fmt.Errorf("远程 llm_endpoint 必须使用 HTTPS，HTTP 仅允许 localhost 或回环 IP")
	}
	result := parsed.String()

	// >>> 数据演变示例
	// 1. https://llm.example/v1/chat/completions -> 原样返回。
	// 2. file:///tmp/model -> 协议拒绝 -> error。
	return result, nil
}

// Evaluate 调用 OpenAI-compatible chat completions 并严格解析模型输出。
// @param ctx：已设置调用超时的上下文；request：消息、行为摘要和人工案例。
// @returns 结构化研判或网络、HTTP、协议错误。
// ⚠️副作用说明：向管理员配置的外部端点发送群消息内容和行为摘要；不记录 API Key 或消息。
func (e *openAICompatibleEvaluator) Evaluate(ctx context.Context, request LLMEvaluationRequest) (LLMEvaluationResult, error) {
	userPayload, err := json.Marshal(request)
	// [决策理由] 请求仅含可编码字段，仍需传播编码错误保持边界完整。
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("编码大模型研判输入: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"model":           e.model,
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": llmSystemPrompt},
			{"role": "user", "content": string(userPayload)},
		},
	})
	// [决策理由] 上游请求体构造失败时不得发出不完整请求。
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("编码大模型请求: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	// [决策理由] URL虽已校验，仍传播标准库构造错误。
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("创建大模型请求: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	// [决策理由] API Key可选以兼容本地模型服务，存在时仅放入Authorization头。
	if e.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	response, err := e.client.Do(httpRequest)
	// [决策理由] 网络错误必须返回检测管线，由其按失败开放策略记录并放行。
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("调用大模型: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxLLMResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	// [决策理由] 响应读取失败时不能解析部分 JSON。
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("读取大模型响应: %w", err)
	}
	// [决策理由] 限制响应体可防止异常供应商耗尽机器人内存。
	if len(responseBody) > maxLLMResponseBytes {
		return LLMEvaluationResult{}, fmt.Errorf("大模型响应超过1MiB")
	}
	// [决策理由] 非2xx响应不可信，且错误体可能含敏感内容，不回显到日志链路。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return LLMEvaluationResult{}, fmt.Errorf("大模型返回HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	// [决策理由] 缺少标准choices结构时无法取得模型JSON输出。
	if err := json.Unmarshal(responseBody, &envelope); err != nil || len(envelope.Choices) == 0 {
		return LLMEvaluationResult{}, fmt.Errorf("解析大模型响应结构失败")
	}
	result, err := DecodeLLMEvaluationResult([]byte(envelope.Choices[0].Message.Content))

	// >>> 数据演变示例
	// 1. choices[0].content含High/90/block JSON -> 严格解码 -> 研判结果。
	// 2. HTTP500或unknown字段 -> error -> 管线转人工复核。
	return result, err
}

const llmSystemPrompt = `你是QQ群广告与引流审核器。仅输出JSON对象，字段必须为risk_level、total_score、reason、violations、suggested_action。检查硬广告引流、紧迫性施压、利益诱导、身份伪装和内容真伪。risk_level只能是High/Medium/Low/Safe；High必须对应block，Medium必须对应manual_review，Low或Safe必须对应pass。violations只输出消息原文中实际出现、可用于风险学习的具体短词，不要输出抽象分类标签。不要输出Markdown。`
