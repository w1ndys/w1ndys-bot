// 📌 影响范围：管理WebUI主动投喂的违规训练样本；保存时调用已配置LLM并写PostgreSQL，不执行QQ处置。
package forbiddenmessagemonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

type trainingSampleResourceHandler struct{ owner *implementation }

type trainingSamplePayload struct {
	MessageContent string `json:"msg_content"`
	TrialID        string `json:"trial_id"`
}

// List 返回管理员投喂的违规训练样本。
// @param ctx：请求上下文；actor：管理员身份；query：分页参数。
// @returns 训练样本资源页或数据库错误。
// ⚠️副作用说明：查询PostgreSQL。
func (h *trainingSampleResourceHandler) List(ctx context.Context, _ management.Actor, query management.ResourceQuery) (management.ResourcePage, error) {
	result, err := h.owner.repository.ListTrainingSamples(ctx, query)

	// >>> 数据演变示例
	// 1. page1/size20 -> 返回最近20条投喂样本。
	// 2. 数据库失败 -> 空页+error。
	return result, err
}

// Create 将管理员明确标记的违规文本保存为训练正例。
// @param ctx：请求上下文；actor：管理员身份；raw：含msg_content与trial_id的JSON。
// @returns 新训练样本记录或凭证、输入及数据库错误。
// ⚠️副作用说明：复用服务端短时试判特征并原子写入样本与候选证据，不再次调用LLM。
func (h *trainingSampleResourceHandler) Create(ctx context.Context, actor management.Actor, raw json.RawMessage) (management.ResourceRecord, error) {
	// [决策理由] 训练样本是可影响生产判定的管理数据，缺少可审计身份时必须拒绝写入。
	if strings.TrimSpace(actor.ID) == "" {
		return management.ResourceRecord{}, fmt.Errorf("%w: 管理员身份不能为空", management.ErrInvalidResourceData)
	}
	payload, err := decodeTrainingSamplePayload(raw)
	// [决策理由] 无效文本不能消耗模型额度或进入训练库。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	exists, err := h.owner.repository.TrainingSampleExists(ctx, payload.MessageContent)
	// [决策理由] 重复样本应在预占额度和调用模型前拒绝，避免无效外部成本。
	if err != nil {
		return management.ResourceRecord{}, fmt.Errorf("检查重复训练样本: %w", err)
	}
	// [决策理由] 相同原文只能贡献一份正向证据，直接返回资源冲突供统一Toast提示。
	if exists {
		return management.ResourceRecord{}, management.ErrResourceConflict
	}
	trialID, err := strconv.ParseInt(payload.TrialID, 10, 64)
	// [决策理由] 字符串凭证必须严格还原为正整数，避免平台文本字段绕过类型边界。
	if err != nil || trialID < 1 {
		return management.ResourceRecord{}, fmt.Errorf("%w: trial_id必须为有效试判标识", management.ErrInvalidResourceData)
	}
	h.owner.trialMu.Lock()
	trial, trusted := h.owner.trials[trialID]
	h.owner.trialMu.Unlock()
	now := time.Now().UTC()
	// [决策理由] 测试与生产使用插件统一时钟校验同源有效期。
	if h.owner.now != nil {
		now = h.owner.now()
	}
	// [决策理由] 只接受同一管理员、同一原文且未过期的服务端试判凭证，不能信任浏览器自报分词。
	if !trusted || trial.ActorID != actor.ID || trial.Text != payload.MessageContent || now.After(trial.Expires) {
		return management.ResourceRecord{}, fmt.Errorf("%w: 试判结果已失效，请重新试判", management.ErrInvalidResourceData)
	}
	features := trial.Features
	record, err := h.owner.repository.CreateTrainingSample(ctx, actor, payload.MessageContent, features)
	// [决策理由] 成功持久化后消费凭证，阻止同一试判重复投喂。
	if err == nil {
		h.owner.trialMu.Lock()
		delete(h.owner.trials, trialID)
		h.owner.trialMu.Unlock()
	}

	// >>> 数据演变示例
	// 1. 管理员投喂广告文本 -> LLM提取[免费,扫码] -> 样本+候选证据。
	// 2. Safe误判且无短词 -> 保存Few-shot正例但不增加候选权重。
	return record, err
}

// Update 拒绝修改既有训练样本，避免原文与候选证据分叉。
// @param ctx/actor/id/version/raw：通用资源更新参数。
// @returns ErrInvalidResourceData。
// ⚠️副作用说明：无。
func (h *trainingSampleResourceHandler) Update(context.Context, management.Actor, int64, int64, json.RawMessage) (management.ResourceRecord, error) {
	// >>> 数据演变示例
	// 1. PATCH样本文本 -> 拒绝。
	// 2. PATCH关键词 -> 拒绝，需删除后重新投喂。
	return management.ResourceRecord{}, management.ErrInvalidResourceData
}

// Delete 删除错误投喂的训练样本并回退候选证据。
// @param ctx：请求上下文；actor：管理员；id/version：样本标识与版本。
// @returns 冲突、未找到或数据库错误。
// ⚠️副作用说明：删除训练样本并回退其候选计数和学习权重。
func (h *trainingSampleResourceHandler) Delete(ctx context.Context, actor management.Actor, id, version int64) error {
	// [决策理由] 删除会回退生产候选权重，缺少可审计身份时必须拒绝操作。
	if strings.TrimSpace(actor.ID) == "" {
		return fmt.Errorf("%w: 管理员身份不能为空", management.ErrInvalidResourceData)
	}
	// [决策理由] 无效标识不得进入事务或影响候选统计。
	if id < 1 || version < 1 {
		return management.ErrInvalidResourceData
	}
	err := h.owner.repository.DeleteTrainingSample(ctx, actor, id, version)

	// >>> 数据演变示例
	// 1. id7/v1 -> 删除样本并回退对应词计数。
	// 2. stale版本 -> conflict且事务回滚。
	return err
}

// decodeTrainingSamplePayload 严格解析训练样本文本。
// @param raw：仅允许msg_content字段的JSON对象。
// @returns 去首尾空白的1到4000字符文本或输入错误。
// ⚠️副作用说明：无。
func decodeTrainingSamplePayload(raw json.RawMessage) (trainingSamplePayload, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload trainingSamplePayload
	// [决策理由] 未知字段和类型错误不能进入模型或训练库。
	if err := decoder.Decode(&payload); err != nil {
		return trainingSamplePayload{}, fmt.Errorf("%w: %v", management.ErrInvalidResourceData, err)
	}
	var trailing any
	err := decoder.Decode(&trailing)
	// [决策理由] 仅EOF表示请求中只有一个JSON对象。
	if !errors.Is(err, io.EOF) {
		return trainingSamplePayload{}, fmt.Errorf("%w: 必须仅提交一个JSON对象", management.ErrInvalidResourceData)
	}
	payload.MessageContent = strings.TrimSpace(payload.MessageContent)
	// [决策理由] 空文本没有训练意义，长度上限与文本试判和数据库约束一致。
	if payload.MessageContent == "" || len([]rune(payload.MessageContent)) > maxTextTestRunes {
		return trainingSamplePayload{}, fmt.Errorf("%w: msg_content必须为1到%d个字符", management.ErrInvalidResourceData, maxTextTestRunes)
	}
	// [决策理由] 投喂必须引用本次服务端试判记录，禁止浏览器直接构造未经试判的样本。
	if strings.TrimSpace(payload.TrialID) == "" {
		return trainingSamplePayload{}, fmt.Errorf("%w: trial_id必须为有效试判标识", management.ErrInvalidResourceData)
	}

	// >>> 数据演变示例
	// 1. {msg_content:" 广告 "} -> "广告"。
	// 2. 未知字段或空文本 -> ErrInvalidResourceData。
	return payload, nil
}
