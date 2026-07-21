// 📌 影响范围：无外部服务；验证WebUI违规样本投喂的严格输入、模型提取与持久化边界。
package forbiddenmessagemonitor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

// TestDecodeTrainingSamplePayload 验证训练样本只接受单一合法文本字段。
// @param t：Go测试上下文。
// @returns 无；断言失败时终止子测试。
// ⚠️副作用说明：无。
func TestDecodeTrainingSamplePayload(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		fail bool
	}{{name: "trim", raw: `{"msg_content":" 广告样本 ","trial_id":"7"}`, want: "广告样本"}, {name: "unknown", raw: `{"msg_content":"广告","trial_id":"7","violations":[]}`, fail: true}, {name: "empty", raw: `{"msg_content":"  ","trial_id":"7"}`, fail: true}, {name: "trailing", raw: `{"msg_content":"广告","trial_id":"7"}{}`, fail: true}, {name: "missing trial", raw: `{"msg_content":"广告"}`, fail: true}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := decodeTrainingSamplePayload(json.RawMessage(test.raw))
			// [决策理由] 错误期望必须与解析结果一致。
			if (err != nil) != test.fail {
				t.Fatalf("decodeTrainingSamplePayload() error=%v fail=%v", err, test.fail)
			}
			// [决策理由] 成功路径必须完成首尾空白规范化。
			if !test.fail && payload.MessageContent != test.want {
				t.Fatalf("MessageContent=%q want=%q", payload.MessageContent, test.want)
			}
		})
	}

	// >>> 数据演变示例
	// 1. 带空白合法文本 -> 去空白文本。
	// 2. 未知字段/空文本/尾随对象 -> 输入错误。
}

// TestTrainingSampleCreate 验证管理员权威标签复用服务端试判特征进入学习仓储。
// @param t：Go测试上下文。
// @returns 无；断言失败时终止子测试。
// ⚠️副作用说明：仅调用内存fake模型与仓储。
func TestTrainingSampleCreate(t *testing.T) {
	repository := &fakeMonitorRepository{record: management.ResourceRecord{ID: 9, Version: 1}}
	owner := &implementation{repository: repository, trials: map[int64]trustedTextTrial{7: {ActorID: "admin", Text: "扫码领取资料", Features: []string{"扫码"}, Expires: time.Now().Add(time.Minute)}}}
	handler := &trainingSampleResourceHandler{owner: owner}
	record, err := handler.Create(context.Background(), management.Actor{ID: "admin", Role: "admin", Channel: management.ChannelWebUI}, json.RawMessage(`{"msg_content":"扫码领取资料","trial_id":"7"}`))
	// [决策理由] 合法投喂必须返回仓储创建的记录。
	if err != nil || record.ID != 9 {
		t.Fatalf("Create() record=%+v error=%v", record, err)
	}
	// [决策理由] 只有确实存在于原文中的模型短词可以形成正向证据。
	if repository.trainingMessage != "扫码领取资料" || len(repository.trainingFeatures) != 1 || repository.trainingFeatures[0] != "扫码" {
		t.Fatalf("stored message=%q features=%v", repository.trainingMessage, repository.trainingFeatures)
	}
	// [决策理由] 成功保存后必须消费试判凭证以阻止重复投喂。
	if _, exists := owner.trials[7]; exists {
		t.Fatal("trusted trial was not consumed")
	}

	// >>> 数据演变示例
	// 1. 试判凭证[扫码] -> 保存扫码证据且不再调用模型。
	// 2. 保存成功 -> trial7被消费。
}

// TestTrainingSampleCreateRejectsUnsafeStates 验证缺身份、伪造或过期试判均不持久化。
// @param t：Go测试上下文。
// @returns 无；断言失败时终止子测试。
// ⚠️副作用说明：仅调用内存fake。
func TestTrainingSampleCreateRejectsUnsafeStates(t *testing.T) {
	tests := []struct {
		name    string
		actorID string
		trial   trustedTextTrial
	}{
		{name: "missing actor", actorID: "", trial: trustedTextTrial{ActorID: "admin", Text: "扫码领取", Features: []string{"扫码"}, Expires: time.Now().Add(time.Minute)}},
		{name: "wrong actor", actorID: "other", trial: trustedTextTrial{ActorID: "admin", Text: "扫码领取", Features: []string{"扫码"}, Expires: time.Now().Add(time.Minute)}},
		{name: "expired", actorID: "admin", trial: trustedTextTrial{ActorID: "admin", Text: "扫码领取", Features: []string{"扫码"}, Expires: time.Now().Add(-time.Minute)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeMonitorRepository{}
			owner := &implementation{repository: repository, trials: map[int64]trustedTextTrial{7: test.trial}}
			_, err := (&trainingSampleResourceHandler{owner: owner}).Create(context.Background(), management.Actor{ID: test.actorID}, json.RawMessage(`{"msg_content":"扫码领取","trial_id":"7"}`))
			// [决策理由] 任一不可信状态都必须显式失败且不写仓储。
			if err == nil || repository.trainingMessage != "" {
				t.Fatalf("Create() error=%v stored=%q", err, repository.trainingMessage)
			}
		})
	}

	// >>> 数据演变示例
	// 1. 无身份/身份不匹配/过期 -> 凭证拒绝。
	// 2. 过期凭证 -> 不保存训练样本。
}

// TestTrainingSampleCreateRejectsDuplicateBeforeLLM 验证重复原文不会再次消耗模型额度。
// @param t：Go测试上下文。
// @returns 无；断言失败时终止测试。
// ⚠️副作用说明：仅调用内存fake仓储。
func TestTrainingSampleCreateRejectsDuplicateBeforeLLM(t *testing.T) {
	repository := &fakeMonitorRepository{trainingExists: true}
	owner := &implementation{repository: repository, trials: map[int64]trustedTextTrial{7: {ActorID: "admin", Text: "扫码领取", Features: []string{"扫码"}, Expires: time.Now().Add(time.Minute)}}}
	_, err := (&trainingSampleResourceHandler{owner: owner}).Create(context.Background(), management.Actor{ID: "admin"}, json.RawMessage(`{"msg_content":"扫码领取","trial_id":"7"}`))
	// [决策理由] 重复样本必须映射为通用资源冲突。
	if !errors.Is(err, management.ErrResourceConflict) {
		t.Fatalf("Create() error=%v", err)
	}
	// >>> 数据演变示例
	// 1. 相同哈希已存在 -> conflict。
	// 2. 重复点击保存 -> 不消费凭证且不写候选证据。
}
