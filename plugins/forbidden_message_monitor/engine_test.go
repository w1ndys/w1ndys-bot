// 📌 影响范围：验证违禁消息纯内存检测、分流与严格LLM模型；无外部变量。
package forbiddenmessagemonitor

import (
	"math"
	"testing"
)

// TestIsValidSpeech 验证Unicode有效发言边界。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：失败时标记当前测试。
func TestIsValidSpeech(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "substantive Chinese", message: "资料", want: true},
		{name: "substantive single rune", message: "行", want: true},
		{name: "meaningless single rune", message: "嗯", want: false},
		{name: "emoji and punctuation", message: "👍！！！", want: false},
		{name: "CQ face only", message: "[CQ:face,id=123]", want: false},
		{name: "Unicode letters", message: "Привет", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := IsValidSpeech(test.message)
			// [决策理由] 每个表项声明独立预期，差异表示有效发言口径回归。
			if got != test.want {
				t.Fatalf("IsValidSpeech(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}

	// >>> 数据演变示例
	// 1. "资料" -> 保留实质汉字 -> true。
	// 2. "👍！！！" -> 无字母数字 -> false。
}

// TestNewEngineRejectsUnsafeConfig 验证非有限数与空组合不会进入运行期。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：仅尝试构造测试引擎。
func TestNewEngineRejectsUnsafeConfig(t *testing.T) {
	t.Parallel()
	tests := []EngineConfig{DefaultEngineConfig(), DefaultEngineConfig()}
	tests[0].WeightedKeywords = []WeightedKeyword{{Text: "风险", Weight: math.NaN()}}
	tests[1].Combinations = []CombinationRule{{Terms: []string{""}, Bonus: 1}}
	for _, config := range tests {
		_, err := NewEngine(config)
		// [决策理由] 非有限权重或空组合词均可绕过评分语义，必须在构造阶段失败。
		if err == nil {
			t.Fatal("NewEngine() unexpectedly accepted unsafe config")
		}
	}

	// >>> 数据演变示例
	// 1. risk weight=NaN -> finite=false -> error。
	// 2. combination term="" -> 空词校验命中 -> error。
}

// TestEngineExactRules 验证联系方式不拦截且仅显式硬词生效。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：仅创建测试内存引擎。
func TestEngineExactRules(t *testing.T) {
	t.Parallel()
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"内部渠道"}
	engine, err := NewEngine(config)
	// [决策理由] 默认配置应始终可构造，错误表示配置契约回归。
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	contact := engine.CheckExactText("联系微信 vxabc123 或 QQ:12345，https://example.com，file_id=13800138000")
	// [决策理由] 联系方式与媒体文件ID已明确移除内置识别，未配置硬词时不得精准拦截。
	if contact.Blocked {
		t.Fatalf("contact result = %#v", contact)
	}
	hard := engine.CheckExactText("这是内部渠道")
	// [决策理由] 管理员配置的硬性词命中必须直接拦截。
	if !hard.Blocked || !hasString(hard.Reasons, "hard_keyword:内部渠道") {
		t.Fatalf("hard result = %#v", hard)
	}
	repeated := engine.CheckExactText("免费资料 请来看看")
	// [决策理由] 删除刷屏机制后，重复语义文本本身不能在未配置硬词时触发拦截。
	if repeated.Blocked {
		t.Fatalf("repeated result = %#v", repeated)
	}

	// >>> 数据演变示例
	// 1. 微信+QQ+URL+file_id -> 未配置硬词 -> 不拦截。
	// 2. 重复普通文本但未配置硬词 -> 不拦截。
}

// TestEngineScoring 验证组合、安全抵扣、长度归一化和三分流。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：仅分配测试引擎。
func TestEngineScoring(t *testing.T) {
	t.Parallel()
	config := DefaultEngineConfig()
	config.WeightedKeywords = []WeightedKeyword{{Text: "免费", Weight: 25}, {Text: "加群", Weight: 20}, {Text: "暴利", Weight: 80}}
	config.SafeKeywords = []WeightedKeyword{{Text: "课程讨论", Weight: 30}}
	config.Combinations = []CombinationRule{{Terms: []string{"免费", "加群"}, Bonus: 20}}
	engine, err := NewEngine(config)
	// [决策理由] 测试配置合法，构造失败意味着无法验证评分契约。
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	medium := engine.Score("这是免费信息")
	// [决策理由] 25分位于默认20到60之间，应进入大模型区间。
	if medium.Band != RiskBandMedium {
		t.Fatalf("medium band = %s, score = %v", medium.Band, medium.Score)
	}
	high := engine.Score("免费加群")
	// [决策理由] 两词权重与组合加成合计65，应直接进入高风险区间。
	if high.Band != RiskBandHigh || high.Score != 65 {
		t.Fatalf("high result = %#v", high)
	}
	low := engine.Score("免费课程讨论")
	// [决策理由] 安全上下文抵扣后归零，应直接放行。
	if low.Band != RiskBandLow || low.Score != 0 {
		t.Fatalf("low result = %#v", low)
	}
	long := engine.Score("暴利" + repeatRune('文', 158))
	// [决策理由] 160字符消息按80字符基准折半，80分归一化后应为40分中风险。
	if long.Band != RiskBandMedium || long.Score != 40 {
		t.Fatalf("normalized result = %#v", long)
	}
	contact := engine.Score("vxabc123 file_id=13800138000 https://example.com")
	// [决策理由] 联系方式与文件ID不再参与内置评分，空词库下必须保持零分低风险。
	if contact.Score != 0 || contact.Band != RiskBandLow {
		t.Fatalf("contact result = %#v", contact)
	}

	// >>> 数据演变示例
	// 1. 免费25+加群20+组合20 -> 65 -> high。
	// 2. 暴利80且长度160 -> 乘0.5 -> 40 -> medium。
}

// TestDecodeLLMEvaluationResult 验证严格JSON协议和值域。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：失败时标记当前测试。
func TestDecodeLLMEvaluationResult(t *testing.T) {
	t.Parallel()
	valid := []byte(`{"risk_level":"High","total_score":91,"reason":"引流","violations":["微信"],"suggested_action":"block"}`)
	result, err := DecodeLLMEvaluationResult(valid)
	// [决策理由] 完整合法对象必须可被调用方消费。
	if err != nil || result.TotalScore != 91 {
		t.Fatalf("valid result = %#v, error = %v", result, err)
	}
	invalidPayloads := [][]byte{
		[]byte(`{"risk_level":"high","total_score":91,"reason":"x","violations":[],"suggested_action":"block"}`),
		[]byte(`{"risk_level":"Safe","total_score":101,"reason":"x","violations":[],"suggested_action":"pass"}`),
		[]byte(`{"risk_level":"Safe","total_score":1,"reason":"x","violations":[],"suggested_action":"pass","unknown":true}`),
		[]byte(`{"risk_level":"Safe","total_score":1,"reason":"x","violations":[],"suggested_action":"pass"} {}`),
	}
	for _, payload := range invalidPayloads {
		_, decodeErr := DecodeLLMEvaluationResult(payload)
		// [决策理由] 枚举、范围、未知字段及尾随JSON任一异常都必须失败关闭。
		if decodeErr == nil {
			t.Fatalf("DecodeLLMEvaluationResult(%s) unexpectedly succeeded", payload)
		}
	}

	// >>> 数据演变示例
	// 1. High/91/block完整对象 -> 严格校验 -> 成功。
	// 2. 小写high或unknown字段 -> 协议不符 -> error。
}

// hasString 判断切片是否含指定字符串。
// @param values：字符串切片；target：目标值。
// @returns 完全匹配时true。
// ⚠️副作用说明：无。
func hasString(values []string, target string) bool {
	for _, value := range values {
		// [决策理由] 测试证据要求稳定的完全匹配。
		if value == target {
			return true
		}
	}

	// >>> 数据演变示例
	// 1. ["a","b"]+"b" -> 命中 -> true。
	// 2. []+"b" -> 无命中 -> false。
	return false
}

// repeatRune 构造固定长度Unicode测试文本。
// @param value：重复字符；count：重复次数。
// @returns 指定字符重复count次的字符串。
// ⚠️副作用说明：仅分配测试字符串。
func repeatRune(value rune, count int) string {
	runes := make([]rune, count)
	for index := range runes {
		runes[index] = value
	}
	result := string(runes)

	// >>> 数据演变示例
	// 1. '文'+2 -> [文,文] -> "文文"。
	// 2. '文'+0 -> [] -> ""。
	return result
}
