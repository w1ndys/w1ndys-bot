// 📌 影响范围：读取 PostgreSQL 关键词规则、通过 Messenger 引用回复群消息，并向目标插件架构提供规格与管理服务。
package keywordreply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// ErrInvalidRule 表示规则输入未通过领域校验。
var ErrInvalidRule = errors.New("关键词规则无效")

// ErrInvalidGroup 表示群号不是正整数。
var ErrInvalidGroup = errors.New("群号无效")

type ruleStore interface {
	LoadEnabled(context.Context) (map[int64]map[string]string, error)
	List(context.Context, int64, int, int) (RulePage, error)
	Create(context.Context, management.Actor, int64, RuleInput) (Rule, error)
	Update(context.Context, management.Actor, int64, int64, int64, RuleInput) (Rule, error)
	Delete(context.Context, management.Actor, int64, int64, int64) error
}

// Service 同时承担关键词回复的运行快照发布与管理端 CRUD。
type Service struct {
	messenger  plugin.Messenger
	repository ruleStore
	authorizer plugin.RuntimeAuthorizer
	rules      atomic.Pointer[map[int64]map[string]string]
	snapshotMu sync.Mutex
}

// New 创建关键词回复服务。
func New(messenger plugin.Messenger, pool *pgxpool.Pool, authorizer plugin.RuntimeAuthorizer) (*Service, error) {
	if messenger == nil {
		return nil, fmt.Errorf("%s 缺少 Messenger", pluginKey)
	}
	if authorizer == nil {
		return nil, fmt.Errorf("%s 缺少管理授权器", pluginKey)
	}
	repository, err := newPostgresRepository(pool)
	if err != nil {
		return nil, err
	}
	service := &Service{messenger: messenger, repository: repository, authorizer: authorizer}
	empty := make(map[int64]map[string]string)
	service.rules.Store(&empty)
	return service, nil
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
			EventKinds:  []plugin.ObserverEventKind{plugin.ObserverGroupMessage},
			Handler:     s.observe,
		}},
		Lifecycle: s,
	}
}

// OnEnable 从数据库发布全部群已启用规则的不可变快照。
func (s *Service) OnEnable(ctx context.Context) error {
	return s.reload(ctx)
}

// OnDisable 清空运行时规则快照；不修改数据库。
func (s *Service) OnDisable(context.Context) error {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	empty := make(map[int64]map[string]string)
	s.rules.Store(&empty)
	return nil
}

// observe 对已通过全局与群门禁的群消息执行本群关键词完全匹配。
func (s *Service) observe(observed plugin.ObserverContext) error {
	message, isMessage := observed.Event.(*ws.MessageEvent)
	if !isMessage {
		return nil
	}
	// OneBot 的 message_sent 可能是机器人自己发出的消息，必须排除以避免自动回复循环。
	if message.PostType != "message" {
		return nil
	}
	snapshot := s.rules.Load()
	if snapshot == nil {
		return nil
	}
	group, found := (*snapshot)[observed.GroupID]
	if !found {
		return nil
	}
	// 直接用原始文本查表，保持完全相等语义，不做 trim 或大小写折叠。
	reply, matched := group[message.RawMessage]
	if !matched {
		return nil
	}
	if _, err := s.messenger.ReplyToMessage(observed.Context, message, message.MessageID, reply); err != nil {
		return fmt.Errorf("发送关键词回复: %w", err)
	}
	return nil
}

// ListRules 按可信群号分页读取规则；插件关闭时仍允许离线查看。
func (s *Service) ListRules(ctx context.Context, actor management.Actor, groupID int64, page, pageSize int) (RulePage, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RulePage{}, err
	}
	if groupID <= 0 {
		return RulePage{}, ErrInvalidGroup
	}
	if page < 1 || page > 1_000_000 {
		return RulePage{}, fmt.Errorf("%w: page 必须在 1 至 1000000 之间", ErrInvalidRule)
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return RulePage{}, fmt.Errorf("%w: page_size 必须在 1 至 %d 之间", ErrInvalidRule, maxPageSize)
	}
	return s.repository.List(ctx, groupID, page, pageSize)
}

// CreateRule 在指定群下新增规则并刷新运行快照。
func (s *Service) CreateRule(ctx context.Context, actor management.Actor, groupID int64, input RuleInput) (Rule, error) {
	normalized, err := s.prepare(actor, groupID, input)
	if err != nil {
		return Rule{}, err
	}
	rule, err := s.repository.Create(ctx, actor, groupID, normalized)
	if err != nil {
		return Rule{}, err
	}
	return rule, s.refresh(ctx)
}

// UpdateRule 按乐观锁更新指定群下的规则并刷新运行快照。
func (s *Service) UpdateRule(ctx context.Context, actor management.Actor, groupID, id, expectedVersion int64, input RuleInput) (Rule, error) {
	normalized, err := s.prepare(actor, groupID, input)
	if err != nil {
		return Rule{}, err
	}
	if id <= 0 || expectedVersion <= 0 {
		return Rule{}, fmt.Errorf("%w: 规则 ID 与版本必须为正数", ErrInvalidRule)
	}
	rule, err := s.repository.Update(ctx, actor, groupID, id, expectedVersion, normalized)
	if err != nil {
		return Rule{}, err
	}
	return rule, s.refresh(ctx)
}

// DeleteRule 按乐观锁删除指定群下的规则并刷新运行快照。
func (s *Service) DeleteRule(ctx context.Context, actor management.Actor, groupID, id, expectedVersion int64) error {
	if err := s.authorizer.Authorize(actor); err != nil {
		return err
	}
	if groupID <= 0 {
		return ErrInvalidGroup
	}
	if id <= 0 || expectedVersion <= 0 {
		return fmt.Errorf("%w: 规则 ID 与版本必须为正数", ErrInvalidRule)
	}
	if err := s.repository.Delete(ctx, actor, groupID, id, expectedVersion); err != nil {
		return err
	}
	return s.refresh(ctx)
}

// prepare 校验管理身份、群号并规范化规则输入。
func (s *Service) prepare(actor management.Actor, groupID int64, input RuleInput) (RuleInput, error) {
	if err := s.authorizer.Authorize(actor); err != nil {
		return RuleInput{}, err
	}
	if groupID <= 0 {
		return RuleInput{}, ErrInvalidGroup
	}
	// 关键词按原文匹配，只拒绝首尾空白导致的不可见差异，不静默改写内容。
	if strings.TrimSpace(input.Keyword) != input.Keyword {
		return RuleInput{}, fmt.Errorf("%w: 关键词首尾不能包含空白", ErrInvalidRule)
	}
	if input.Keyword == "" || utf8.RuneCountInString(input.Keyword) > maxKeywordLength {
		return RuleInput{}, fmt.Errorf("%w: 关键词长度必须在 1 至 %d 之间", ErrInvalidRule, maxKeywordLength)
	}
	if strings.TrimSpace(input.ReplyContent) == "" || utf8.RuneCountInString(input.ReplyContent) > maxReplyLength {
		return RuleInput{}, fmt.Errorf("%w: 回复内容长度必须在 1 至 %d 之间", ErrInvalidRule, maxReplyLength)
	}
	return input, nil
}

// refresh 写入成功后按数据库权威结果重建运行快照。
func (s *Service) refresh(ctx context.Context) error {
	if err := s.reload(ctx); err != nil {
		return fmt.Errorf("刷新关键词规则快照: %w", err)
	}
	return nil
}

// reload 加载并原子发布规则快照；读取失败时保留旧快照。
func (s *Service) reload(ctx context.Context) error {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	loaded, err := s.repository.LoadEnabled(ctx)
	if err != nil {
		return fmt.Errorf("加载关键词回复规则: %w", err)
	}
	cloned := make(map[int64]map[string]string, len(loaded))
	for groupID, rules := range loaded {
		group := make(map[string]string, len(rules))
		for keyword, reply := range rules {
			group[keyword] = reply
		}
		cloned[groupID] = group
	}
	s.rules.Store(&cloned)
	return nil
}
