// 📌 影响范围：启动本机HTTP测试服务验证大模型配置与协议；不访问真实供应商、QQ或数据库。
package forbiddenmessagemonitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

// TestDefaultRuntimeConfigBuildsWithoutLLM 验证默认配置可运行且不创建外部调用器。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：仅构造内存引擎。
func TestDefaultRuntimeConfigBuildsWithoutLLM(t *testing.T) {
	instance := &implementation{httpClient: &http.Client{}}
	raw, err := plugin.NormalizeConfig(instance.ConfigSchema(), json.RawMessage(`{}`))
	// [决策理由] 默认Schema必须生成完整可应用配置。
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := buildRuntimeSnapshot(raw, instance.httpClient)
	// [决策理由] 新安装默认关闭LLM仍应具备确定性检测引擎。
	if err != nil || snapshot.engine == nil || snapshot.evaluator != nil {
		t.Fatalf("buildRuntimeSnapshot() snapshot=%+v error=%v", snapshot, err)
	}

	// >>> 数据演变示例
	// 1. {} -> Schema默认值 -> engine+nil evaluator。
	// 2. LLM关闭 -> 不要求endpoint/model/key。
}

// TestOpenAICompatibleEvaluatorUsesAuthAndStrictResult 验证鉴权头、请求和严格模型输出。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：启动本机HTTP服务并交换不含真实隐私的测试JSON。
func TestOpenAICompatibleEvaluatorUsesAuthAndStrictResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// [决策理由] API Key只能通过Bearer头传递，不能进入URL。
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"{\"risk_level\":\"High\",\"total_score\":90,\"reason\":\"引流\",\"violations\":[\"微信\"],\"suggested_action\":\"block\"}"}}]}`))
	}))
	defer server.Close()
	evaluator := &openAICompatibleEvaluator{endpoint: server.URL, model: "test-model", apiKey: "secret", client: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := evaluator.Evaluate(ctx, LLMEvaluationRequest{Message: "测试消息", BehaviorSummary: "新用户", Examples: []LLMExample{}})
	// [决策理由] 合法兼容响应必须解码为可执行block结论。
	if err != nil || result.SuggestedAction != "block" || result.TotalScore != 90 {
		t.Fatalf("Evaluate() result=%+v error=%v", result, err)
	}

	// >>> 数据演变示例
	// 1. Bearer secret+High/90/block -> 严格结果成功。
	// 2. 模型输出未知字段 -> DecodeLLMEvaluationResult测试覆盖拒绝。
}

// TestRuntimeConfigRejectsInvalidLLMAndRuleJSON 验证外部端点与词库输入边界。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestRuntimeConfigRejectsInvalidLLMAndRuleJSON(t *testing.T) {
	base := pluginConfig{HardKeywordsJSON: "[]", WeightedKeywordsJSON: "[]", SafeKeywordsJSON: "[]", CombinationsJSON: "[]", LowThreshold: 20, HighThreshold: 60, LLMTimeoutSeconds: 20}
	base.WeightedKeywordsJSON = `[{"text":"x","weight":1,"unknown":true}]`
	raw, _ := json.Marshal(base)
	_, err := buildRuntimeSnapshot(raw, &http.Client{})
	// [决策理由] 未知规则字段必须拒绝，避免管理员以为配置生效。
	if err == nil {
		t.Fatal("unknown weighted keyword field accepted")
	}
	base.WeightedKeywordsJSON = "[]"
	base.LLMEnabled = true
	base.LLMEndpoint = "file:///tmp/model"
	base.LLMModel = "test"
	raw, _ = json.Marshal(base)
	_, err = buildRuntimeSnapshot(raw, &http.Client{})
	// [决策理由] 非HTTP协议不得成为消息外发目标。
	if err == nil {
		t.Fatal("file LLM endpoint accepted")
	}
	base.LLMEndpoint = "http://llm.example/v1/chat/completions"
	raw, _ = json.Marshal(base)
	_, err = buildRuntimeSnapshot(raw, &http.Client{})
	// [决策理由] 远程HTTP会明文泄露群消息和密钥，必须在配置阶段拒绝。
	if err == nil {
		t.Fatal("remote HTTP LLM endpoint accepted")
	}
	base.LLMEndpoint = "http://127.0.0.1:11434/v1/chat/completions"
	raw, _ = json.Marshal(base)
	_, err = buildRuntimeSnapshot(raw, &http.Client{})
	// [决策理由] 本机回环HTTP用于本地模型，且流量不离开主机，应允许配置。
	if err != nil {
		t.Fatalf("loopback HTTP LLM endpoint rejected: %v", err)
	}

	// >>> 数据演变示例
	// 1. 风险词含unknown -> DisallowUnknownFields -> error。
	// 2. 远程HTTP -> 隐私边界校验 -> error；回环HTTP -> 允许。
}

// TestSecureLLMHTTPClientRejectsUnsafeRedirect 验证重定向不能绕过远程HTTPS边界。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：仅调用客户端重定向校验器，不发起网络请求。
func TestSecureLLMHTTPClientRejectsUnsafeRedirect(t *testing.T) {
	client := secureLLMHTTPClient(&http.Client{})
	unsafeRequest, _ := http.NewRequest(http.MethodGet, "http://llm.example/v1/chat/completions", nil)
	secureRequest, _ := http.NewRequest(http.MethodGet, "https://llm.example/v1/chat/completions", nil)
	// [决策理由] HTTPS初始端点跳转到远程HTTP时必须在发送第二跳前拒绝。
	if err := client.CheckRedirect(unsafeRequest, []*http.Request{secureRequest}); err == nil {
		t.Fatal("unsafe remote HTTP redirect accepted")
	}
	loopbackRequest, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:11434/v1/chat/completions", nil)
	// [决策理由] 本机模型在回环地址内重定向仍满足相同隐私边界。
	if err := client.CheckRedirect(loopbackRequest, []*http.Request{loopbackRequest}); err != nil {
		t.Fatalf("loopback redirect rejected: %v", err)
	}
	// >>> 数据演变示例
	// 1. https://safe -> http://remote -> error且第二跳不发送。
	// 2. http://127.0.0.1/a -> http://127.0.0.1/b -> nil。
}
