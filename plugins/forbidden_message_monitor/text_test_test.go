// 📌 影响范围：纯内存验证文本试判资源；不访问QQ、数据库或真实大模型。
package forbiddenmessagemonitor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

// TestTextTestResourceUsesRulesWithoutActions 验证试判复用规则但不触发外部处置。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：仅修改内存fake调用记录。
func TestTextTestResourceUsesRulesWithoutActions(t *testing.T) {
	instance := testImplementation()
	actions := instance.actions.(*fakeActions)
	repository := instance.repository.(*fakeMonitorRepository)
	config := DefaultEngineConfig()
	config.HardKeywords = []string{"固定广告"}
	engine, err := NewEngine(config)
	// [决策理由] 测试规则必须成功构造才能验证精准试判。
	if err != nil {
		t.Fatal(err)
	}
	instance.snapshot.Store(&runtimeSnapshot{engine: engine, engineConfig: config, llmTimeout: time.Second})
	handler := &textTestResourceHandler{owner: instance}
	record, err := handler.Create(context.Background(), management.Actor{}, json.RawMessage(`{"text":"固定广告"}`))
	// [决策理由] 硬词试判应返回可解析的违规结论。
	if err != nil {
		t.Fatal(err)
	}
	var result textTestResult
	// [决策理由] 返回数据必须满足前端稳定结构。
	if err := json.Unmarshal(record.Data, &result); err != nil {
		t.Fatal(err)
	}
	// [决策理由] 文本试判只能展示block，不得执行禁言、历史读取、撤回或审计写入。
	if result.Decision != "违规" || result.SuggestedAction != "block" || len(actions.banParams) != 0 || len(actions.historyParams) != 0 || len(actions.deleted) != 0 || len(repository.created) != 0 {
		t.Fatalf("result=%+v bans=%d history=%d deleted=%d audits=%d", result, len(actions.banParams), len(actions.historyParams), len(actions.deleted), len(repository.created))
	}
	// >>> 数据演变示例
	// 1. 固定广告 -> precise_rule/block -> 仅返回结果。
	// 2. fake Action/仓储 -> 调用计数均为0。
}

// TestTextTestResourceRejectsInvalidPayload 验证文本试判输入边界。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestTextTestResourceRejectsInvalidPayload(t *testing.T) {
	handler := &textTestResourceHandler{owner: testImplementation()}
	tests := []json.RawMessage{json.RawMessage(`{"text":""}`), json.RawMessage(`{"text":"x","extra":true}`), json.RawMessage(`{"text":1}`)}
	for _, raw := range tests {
		_, err := handler.Create(context.Background(), management.Actor{}, raw)
		// [决策理由] 空值、未知字段与类型错误必须统一映射为领域输入错误。
		if !errors.Is(err, management.ErrInvalidResourceData) {
			t.Fatalf("Create(%s) error=%v", raw, err)
		}
	}
	// >>> 数据演变示例
	// 1. {text:""} -> 空文本 -> ErrInvalidResourceData。
	// 2. {text:"x",extra:true} -> 未知字段 -> ErrInvalidResourceData。
}
