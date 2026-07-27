// 📌 影响范围：向目标插件架构提供违禁监控规格、观察器与类型化管理服务；管理写操作会修改数据库、审计并可能调用 NapCat 与外部模型。
package forbiddenmessagemonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

const (
	pluginKey    = "forbidden_message_monitor"
	adminPageKey = "forbidden_message_monitor"
	observerKey  = "moderation"
	observerDesc = "对通过群门禁的群消息与群通知执行分层检测、自动处置与误判回收"
)

// ErrInvalidInput 表示管理输入未通过领域校验。
var ErrInvalidInput = errors.New("违禁监控输入无效")

// Record 是一条带乐观锁版本的管理记录；Data 由插件按各自领域编码。
type Record struct {
	ID      int64           `json:"id"`
	Version int64           `json:"version"`
	Data    json.RawMessage `json:"data"`
}

// RecordPage 是管理记录的分页结果。
type RecordPage struct {
	Items    []Record `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int64    `json:"total"`
}

// Service 是违禁监控插件对平台暴露的运行规格与管理能力。
type Service struct {
	implementation *implementation
	lexicon        *lexiconRepository
	authorizer     plugin.RuntimeAuthorizer
	runtime        *plugin.RuntimeController
	lexiconMu      sync.Mutex
}

var ErrRuntimeUnavailable = errors.New("违禁消息监控当前未启用")

// New 创建违禁监控服务。
func New(actions plugin.ActionAPI, pool *pgxpool.Pool, authorizer plugin.RuntimeAuthorizer) (*Service, error) {
	if actions == nil {
		return nil, fmt.Errorf("%s 缺少 Actions", pluginKey)
	}
	if authorizer == nil {
		return nil, fmt.Errorf("%s 缺少管理授权器", pluginKey)
	}
	instance, err := newImplementation(actions, pool)
	if err != nil {
		return nil, err
	}
	lexicon, err := newLexiconRepository(pool)
	if err != nil {
		return nil, err
	}
	return &Service{implementation: instance, lexicon: lexicon, authorizer: authorizer}, nil
}

// Spec 返回编译期插件规格，由 cmd/bot 装入 SpecCatalog。
func (s *Service) Spec() plugin.PluginSpec {
	return plugin.PluginSpec{
		Key:          pluginKey,
		DisplayName:  pluginDisplayName,
		Description:  pluginDescription,
		AdminPageKey: adminPageKey,
		Observers: []plugin.ObserverSpec{{
			Key:         observerKey,
			Description: observerDesc,
			EventKinds:  []plugin.ObserverEventKind{plugin.ObserverGroupMessage, plugin.ObserverGroupNotice},
			Handler:     s.observe,
		}},
		Config: &plugin.ConfigSpec{
			Schema:   s.implementation.ConfigSchema(),
			Validate: s.implementation.ValidateConfig,
			Apply:    s.implementation.ApplyConfig,
		},
		Lifecycle: s.implementation,
	}
}

// BindRuntimeController 绑定平台运行门禁；必须在开放管理 API 前完成。
func (s *Service) BindRuntimeController(controller *plugin.RuntimeController) error {
	if controller == nil {
		return errors.New("违禁消息监控缺少运行控制器")
	}
	s.runtime = controller
	return nil
}

// observe 处理已通过全局与群门禁的群消息和群通知。
func (s *Service) observe(observed plugin.ObserverContext) error {
	switch typed := observed.Event.(type) {
	case *ws.MessageEvent:
		return s.implementation.handleMessage(observed.Context, typed)
	case *ws.GroupBanNotice:
		return s.implementation.handleGroupBanNotice(observed.Context, typed)
	case *ws.NoticeEvent:
		return s.implementation.handleNotice(observed.Context, typed)
	default:
		return nil
	}
}

// ListViolations 分页读取待复核的违规记录；插件关闭时仍允许离线查看历史。
func (s *Service) ListViolations(ctx context.Context, actor management.Actor, page, pageSize int) (RecordPage, error) {
	if err := s.authorize(actor, page, pageSize); err != nil {
		return RecordPage{}, err
	}
	result, err := s.implementation.repository.ListPending(ctx, management.ResourceQuery{Page: page, PageSize: pageSize})
	if err != nil {
		return RecordPage{}, err
	}
	return toRecordPage(result), nil
}

// ReviewViolation 按乐观锁提交人工复核结论。
// 结论会触发群内处置或解除禁言，因此必须重新确认运行门禁由平台在调用前完成。
func (s *Service) ReviewViolation(ctx context.Context, actor management.Actor, id, expectedVersion int64, status string) (Record, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return Record{}, err
	}
	if id < 1 || expectedVersion < 1 {
		return Record{}, fmt.Errorf("%w: 记录 ID 与版本必须为正数", ErrInvalidInput)
	}
	current, err := s.implementation.repository.GetViolation(ctx, id)
	if err != nil {
		return Record{}, err
	}
	admission, ok := s.runtimeAdmission(current.Data.GroupID)
	if !ok {
		return Record{}, ErrRuntimeUnavailable
	}
	defer admission.Release()
	payload, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return Record{}, fmt.Errorf("%w: 无法编码复核结论", ErrInvalidInput)
	}
	handler := &violationResourceHandler{repository: s.implementation.repository, actions: s.implementation.actions}
	record, err := handler.Update(ctx, actor, id, expectedVersion, payload)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: record.ID, Version: record.Version, Data: record.Data}, nil
}

// RunTextTrial 使用当前规则试判文本；不禁言、不撤回，也不写入违规审计。
func (s *Service) RunTextTrial(ctx context.Context, actor management.Actor, text string) (Record, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return Record{}, err
	}
	admission, ok := s.runtimeAdmission(0)
	if !ok {
		return Record{}, ErrRuntimeUnavailable
	}
	defer admission.Release()
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return Record{}, fmt.Errorf("%w: 无法编码试判文本", ErrInvalidInput)
	}
	handler := &textTestResourceHandler{owner: s.implementation}
	record, err := handler.Create(ctx, actor, payload)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: record.ID, Version: record.Version, Data: record.Data}, nil
}

func (s *Service) runtimeAdmission(groupID int64) (plugin.Admission, bool) {
	if s.runtime == nil {
		return nil, false
	}
	admission, ok := s.runtime.Admit(pluginKey)
	if !ok {
		return nil, false
	}
	if groupID > 0 && !admission.GroupEnabled(groupID) {
		admission.Release()
		return nil, false
	}
	return admission, true
}

// ListTrainingSamples 分页读取管理员主动投喂的违规正例。
func (s *Service) ListTrainingSamples(ctx context.Context, actor management.Actor, page, pageSize int) (RecordPage, error) {
	if err := s.authorize(actor, page, pageSize); err != nil {
		return RecordPage{}, err
	}
	result, err := s.implementation.repository.ListTrainingSamples(ctx, management.ResourceQuery{Page: page, PageSize: pageSize})
	if err != nil {
		return RecordPage{}, err
	}
	return toRecordPage(result), nil
}

// CreateTrainingSample 按试判凭证投喂违规正例；凭证由服务端在试判时签发。
func (s *Service) CreateTrainingSample(ctx context.Context, actor management.Actor, text, trialID string) (Record, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return Record{}, err
	}
	payload, err := json.Marshal(map[string]string{"msg_content": text, "trial_id": trialID})
	if err != nil {
		return Record{}, fmt.Errorf("%w: 无法编码训练样本", ErrInvalidInput)
	}
	handler := &trainingSampleResourceHandler{owner: s.implementation}
	record, err := handler.Create(ctx, actor, payload)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: record.ID, Version: record.Version, Data: record.Data}, nil
}

// DeleteTrainingSample 按乐观锁删除训练样本并回退候选词统计。
func (s *Service) DeleteTrainingSample(ctx context.Context, actor management.Actor, id, expectedVersion int64) error {
	if err := s.authorizer.Authorize(actor); err != nil {
		return err
	}
	handler := &trainingSampleResourceHandler{owner: s.implementation}
	return handler.Delete(ctx, actor, id, expectedVersion)
}

func (s *Service) authorize(actor management.Actor, page, pageSize int) error {
	if err := s.authorizer.Authorize(actor); err != nil {
		return err
	}
	if page < 1 || page > 1_000_000 {
		return fmt.Errorf("%w: page 必须在 1 至 1000000 之间", ErrInvalidInput)
	}
	if pageSize < 1 || pageSize > maxLexiconPageSize {
		return fmt.Errorf("%w: page_size 必须在 1 至 %d 之间", ErrInvalidInput, maxLexiconPageSize)
	}
	return nil
}

func toRecordPage(page management.ResourcePage) RecordPage {
	items := make([]Record, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, Record{ID: item.ID, Version: item.Version, Data: item.Data})
	}
	return RecordPage{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

// ListTerms 按分类分页读取词库词条；kind 为空表示全部分类。
func (s *Service) ListTerms(ctx context.Context, actor management.Actor, kind string, page, pageSize int) ([]Term, int64, error) {
	if err := s.authorize(actor, page, pageSize); err != nil {
		return nil, 0, err
	}
	if kind != "" && !validTermKind(kind) {
		return nil, 0, fmt.Errorf("%w: 未知词条分类 %q", ErrInvalidInput, kind)
	}
	return s.lexicon.ListTerms(ctx, kind, page, pageSize)
}

// CreateTerm 新增词条并按新词库重建检测引擎。
func (s *Service) CreateTerm(ctx context.Context, actor management.Actor, input TermInput) (Term, error) {
	s.lexiconMu.Lock()
	defer s.lexiconMu.Unlock()
	normalized, err := s.prepareTerm(actor, input)
	if err != nil {
		return Term{}, err
	}
	term, err := s.lexicon.CreateTerm(ctx, actor, normalized)
	if err != nil {
		return Term{}, err
	}
	return term, s.refreshLexicon(ctx)
}

// UpdateTerm 按乐观锁更新词条并按新词库重建检测引擎。
func (s *Service) UpdateTerm(ctx context.Context, actor management.Actor, id, expectedVersion int64, input TermInput) (Term, error) {
	s.lexiconMu.Lock()
	defer s.lexiconMu.Unlock()
	normalized, err := s.prepareTerm(actor, input)
	if err != nil {
		return Term{}, err
	}
	if id < 1 || expectedVersion < 1 {
		return Term{}, fmt.Errorf("%w: 词条 ID 与版本必须为正数", ErrInvalidInput)
	}
	term, err := s.lexicon.UpdateTerm(ctx, actor, id, expectedVersion, normalized)
	if err != nil {
		return Term{}, err
	}
	return term, s.refreshLexicon(ctx)
}

// DeleteTerm 按乐观锁删除词条并按新词库重建检测引擎。
func (s *Service) DeleteTerm(ctx context.Context, actor management.Actor, id, expectedVersion int64) error {
	s.lexiconMu.Lock()
	defer s.lexiconMu.Unlock()
	if err := s.authorizer.Authorize(actor); err != nil {
		return err
	}
	if id < 1 || expectedVersion < 1 {
		return fmt.Errorf("%w: 词条 ID 与版本必须为正数", ErrInvalidInput)
	}
	if err := s.lexicon.DeleteTerm(ctx, actor, id, expectedVersion); err != nil {
		return err
	}
	return s.refreshLexicon(ctx)
}

// ListCombinations 分页读取组合加成规则。
func (s *Service) ListCombinations(ctx context.Context, actor management.Actor, page, pageSize int) ([]Combination, int64, error) {
	if err := s.authorize(actor, page, pageSize); err != nil {
		return nil, 0, err
	}
	return s.lexicon.ListCombinations(ctx, page, pageSize)
}

// CreateCombination 新增组合规则并按新词库重建检测引擎。
func (s *Service) CreateCombination(ctx context.Context, actor management.Actor, input CombinationInput) (Combination, error) {
	s.lexiconMu.Lock()
	defer s.lexiconMu.Unlock()
	if err := s.authorizer.Authorize(actor); err != nil {
		return Combination{}, err
	}
	normalized, err := normalizeCombination(input)
	if err != nil {
		return Combination{}, err
	}
	combination, err := s.lexicon.CreateCombination(ctx, actor, normalized)
	if err != nil {
		return Combination{}, err
	}
	return combination, s.refreshLexicon(ctx)
}

// DeleteCombination 按乐观锁删除组合规则并按新词库重建检测引擎。
func (s *Service) DeleteCombination(ctx context.Context, actor management.Actor, id, expectedVersion int64) error {
	s.lexiconMu.Lock()
	defer s.lexiconMu.Unlock()
	if err := s.authorizer.Authorize(actor); err != nil {
		return err
	}
	if id < 1 || expectedVersion < 1 {
		return fmt.Errorf("%w: 组合 ID 与版本必须为正数", ErrInvalidInput)
	}
	if err := s.lexicon.DeleteCombination(ctx, actor, id, expectedVersion); err != nil {
		return err
	}
	return s.refreshLexicon(ctx)
}

// prepareTerm 校验管理身份并规范化词条输入。
func (s *Service) prepareTerm(actor management.Actor, input TermInput) (TermInput, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return TermInput{}, err
	}
	if !validTermKind(input.Kind) {
		return TermInput{}, fmt.Errorf("%w: 未知词条分类 %q", ErrInvalidInput, input.Kind)
	}
	text := strings.TrimSpace(input.Text)
	if text == "" || utf8.RuneCountInString(text) > maxTermRunes {
		return TermInput{}, fmt.Errorf("%w: 词条长度必须在 1 至 %d 之间", ErrInvalidInput, maxTermRunes)
	}
	// 硬性关键词是零误报直接拦截，不参与加权评分，权重必须归零避免误解。
	weight := input.Weight
	if input.Kind == TermKindHard {
		weight = 0
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 || weight > maxTermWeight {
		return TermInput{}, fmt.Errorf("%w: 权重必须在 0 至 %d 之间", ErrInvalidInput, maxTermWeight)
	}
	return TermInput{Kind: input.Kind, Text: text, Weight: weight}, nil
}

// normalizeCombination 校验并去重组合规则的词项。
func normalizeCombination(input CombinationInput) (CombinationInput, error) {
	if math.IsNaN(input.Bonus) || math.IsInf(input.Bonus, 0) || input.Bonus < 0 || input.Bonus > maxTermWeight {
		return CombinationInput{}, fmt.Errorf("%w: 组合加分必须在 0 至 %d 之间", ErrInvalidInput, maxTermWeight)
	}
	seen := make(map[string]struct{}, len(input.Terms))
	terms := make([]string, 0, len(input.Terms))
	for _, term := range input.Terms {
		trimmed := strings.TrimSpace(term)
		// 空词项会让组合无条件命中，必须拒绝而不是静默丢弃。
		if trimmed == "" || utf8.RuneCountInString(trimmed) > maxTermRunes {
			return CombinationInput{}, fmt.Errorf("%w: 组合词项长度必须在 1 至 %d 之间", ErrInvalidInput, maxTermRunes)
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		terms = append(terms, trimmed)
	}
	if len(terms) < 2 || len(terms) > maxCombinationSize {
		return CombinationInput{}, fmt.Errorf("%w: 组合必须包含 2 至 %d 个不同词项", ErrInvalidInput, maxCombinationSize)
	}
	return CombinationInput{Terms: terms, Bonus: input.Bonus}, nil
}

// refreshLexicon 词库写入成功后按数据库权威结果重建检测引擎。
func (s *Service) refreshLexicon(ctx context.Context) error {
	if err := s.implementation.reloadLexicon(ctx); err != nil {
		refreshErr := fmt.Errorf("刷新违禁词库快照: %w", err)
		if s.runtime == nil {
			return refreshErr
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if disableErr := s.runtime.Disable(shutdownCtx, pluginKey); disableErr != nil {
			return errors.Join(refreshErr, fmt.Errorf("词库快照不一致后关闭插件: %w", disableErr))
		}
		return refreshErr
	}
	return nil
}

func validTermKind(kind string) bool {
	return kind == TermKindHard || kind == TermKindRisk || kind == TermKindSafe
}
