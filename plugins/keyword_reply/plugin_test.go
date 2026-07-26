// 📌 影响范围：使用内存 fake 仓库与 Messenger 验证关键词回复规格、观察器与管理服务；不访问 NapCat、PostgreSQL 或网络。
package keywordreply

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

var testActor = management.Actor{ID: "10001", Role: "super_admin", Channel: management.ChannelWebUI, RequestID: "req-1"}

type fakeMessenger struct {
	reply     string
	messageID int64
	calls     int
	err       error
}

func (f *fakeMessenger) Reply(context.Context, *ws.MessageEvent, any) (int64, error) { return 1, f.err }

func (f *fakeMessenger) ReplyToMessage(_ context.Context, _ *ws.MessageEvent, messageID int64, message string) (int64, error) {
	f.calls++
	f.messageID = messageID
	f.reply = message
	return 1, f.err
}

type fakeStore struct {
	snapshot    map[int64]map[string]string
	page        RulePage
	rule        Rule
	err         error
	loadErr     error
	loadCalls   int
	lastGroupID int64
	lastInput   RuleInput
	lastID      int64
	lastVersion int64
	calls       []string
}

func (f *fakeStore) LoadEnabled(context.Context) (map[int64]map[string]string, error) {
	f.loadCalls++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.snapshot, nil
}

func (f *fakeStore) List(_ context.Context, groupID int64, _, _ int) (RulePage, error) {
	f.lastGroupID, f.calls = groupID, append(f.calls, "list")
	return f.page, f.err
}

func (f *fakeStore) Create(_ context.Context, _ management.Actor, groupID int64, input RuleInput) (Rule, error) {
	f.lastGroupID, f.lastInput, f.calls = groupID, input, append(f.calls, "create")
	return f.rule, f.err
}

func (f *fakeStore) Update(_ context.Context, _ management.Actor, groupID, id, version int64, input RuleInput) (Rule, error) {
	f.lastGroupID, f.lastID, f.lastVersion, f.lastInput = groupID, id, version, input
	f.calls = append(f.calls, "update")
	return f.rule, f.err
}

func (f *fakeStore) Delete(_ context.Context, _ management.Actor, groupID, id, version int64) error {
	f.lastGroupID, f.lastID, f.lastVersion = groupID, id, version
	f.calls = append(f.calls, "delete")
	return f.err
}

type fakeAuthorizer struct {
	err error
}

func (a *fakeAuthorizer) Authorize(management.Actor) error { return a.err }

func newTestService(t *testing.T, store *fakeStore, messenger *fakeMessenger, authorizer *fakeAuthorizer) *Service {
	t.Helper()
	service := &Service{messenger: messenger, repository: store, authorizer: authorizer}
	empty := make(map[int64]map[string]string)
	service.rules.Store(&empty)
	return service
}

func TestSpecDeclaresGroupObserverContract(t *testing.T) {
	service := newTestService(t, &fakeStore{}, &fakeMessenger{}, &fakeAuthorizer{})
	spec := service.Spec()
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	if spec.Key != "keyword_reply" || spec.AdminPageKey != "keyword_reply" {
		t.Fatalf("spec = %+v", spec)
	}
	// 关键词匹配没有触发词语义，必须声明为观察器而不是命令。
	if len(spec.Commands) != 0 || len(spec.Observers) != 1 {
		t.Fatalf("commands=%d observers=%d", len(spec.Commands), len(spec.Observers))
	}
	observer := spec.Observers[0]
	if observer.Key != "keyword_match" || len(observer.EventKinds) != 1 || observer.EventKinds[0] != plugin.ObserverGroupMessage {
		t.Fatalf("observer = %+v", observer)
	}
	// 快照来自数据库，必须声明生命周期以便启用时加载、禁用时清空。
	if spec.Lifecycle == nil {
		t.Fatal("lifecycle missing")
	}
}

func TestObserverRepliesOnlyWithinOwnGroup(t *testing.T) {
	store := &fakeStore{snapshot: map[int64]map[string]string{
		100: {"你好": "你好呀"},
		200: {"你好": "别的群"},
	}}
	messenger := &fakeMessenger{}
	service := newTestService(t, store, messenger, &fakeAuthorizer{})
	if err := service.OnEnable(context.Background()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		groupID int64
		raw     string
		post    string
		want    string
	}{
		{name: "本群命中", groupID: 100, raw: "你好", post: "message", want: "你好呀"},
		{name: "按群隔离", groupID: 200, raw: "你好", post: "message", want: "别的群"},
		{name: "未配置的群", groupID: 300, raw: "你好", post: "message"},
		{name: "完全相等语义", groupID: 100, raw: " 你好", post: "message"},
		{name: "机器人自身消息", groupID: 100, raw: "你好", post: "message_sent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messenger.reply, messenger.calls = "", 0
			event := &ws.MessageEvent{MessageType: "group", GroupID: test.groupID, UserID: 300, MessageID: 20, RawMessage: test.raw}
			event.PostType = test.post
			err := service.Spec().Observers[0].Handler(plugin.ObserverContext{
				Context: context.Background(), GroupID: test.groupID, Event: event,
			})
			if err != nil {
				t.Fatal(err)
			}
			if messenger.reply != test.want {
				t.Fatalf("reply = %q", messenger.reply)
			}
			if test.want != "" && messenger.messageID != 20 {
				t.Fatalf("引用的消息 ID = %d", messenger.messageID)
			}
		})
	}
}

func TestDisableClearsSnapshot(t *testing.T) {
	store := &fakeStore{snapshot: map[int64]map[string]string{100: {"你好": "你好呀"}}}
	messenger := &fakeMessenger{}
	service := newTestService(t, store, messenger, &fakeAuthorizer{})
	if err := service.OnEnable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.OnDisable(context.Background()); err != nil {
		t.Fatal(err)
	}
	event := &ws.MessageEvent{MessageType: "group", GroupID: 100, MessageID: 20, RawMessage: "你好"}
	event.PostType = "message"
	if err := service.Spec().Observers[0].Handler(plugin.ObserverContext{Context: context.Background(), GroupID: 100, Event: event}); err != nil {
		t.Fatal(err)
	}
	// 禁用后必须停止回复，即使数据库仍有规则。
	if messenger.calls != 0 {
		t.Fatalf("calls = %d", messenger.calls)
	}
}

func TestReloadKeepsOldSnapshotOnFailure(t *testing.T) {
	loadFailure := errors.New("load failed")
	store := &fakeStore{snapshot: map[int64]map[string]string{100: {"你好": "你好呀"}}}
	messenger := &fakeMessenger{}
	service := newTestService(t, store, messenger, &fakeAuthorizer{})
	if err := service.OnEnable(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.loadErr = loadFailure
	if err := service.OnEnable(context.Background()); !errors.Is(err, loadFailure) {
		t.Fatalf("OnEnable() error = %v", err)
	}
	event := &ws.MessageEvent{MessageType: "group", GroupID: 100, MessageID: 20, RawMessage: "你好"}
	event.PostType = "message"
	if err := service.Spec().Observers[0].Handler(plugin.ObserverContext{Context: context.Background(), GroupID: 100, Event: event}); err != nil {
		t.Fatal(err)
	}
	// 加载失败不得清空已可用的运行快照。
	if messenger.reply != "你好呀" {
		t.Fatalf("reply = %q", messenger.reply)
	}
}

func TestManagementRequiresAuthorization(t *testing.T) {
	forbidden := errors.New("forbidden")
	store := &fakeStore{}
	service := newTestService(t, store, &fakeMessenger{}, &fakeAuthorizer{err: forbidden})
	if _, err := service.ListRules(context.Background(), testActor, 100, 1, 20); !errors.Is(err, forbidden) {
		t.Fatalf("ListRules() error = %v", err)
	}
	if _, err := service.CreateRule(context.Background(), testActor, 100, RuleInput{Keyword: "你好", ReplyContent: "你好呀"}); !errors.Is(err, forbidden) {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if _, err := service.UpdateRule(context.Background(), testActor, 100, 1, 1, RuleInput{Keyword: "你好", ReplyContent: "你好呀"}); !errors.Is(err, forbidden) {
		t.Fatalf("UpdateRule() error = %v", err)
	}
	if err := service.DeleteRule(context.Background(), testActor, 100, 1, 1); !errors.Is(err, forbidden) {
		t.Fatalf("DeleteRule() error = %v", err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("unauthorized calls reached store: %v", store.calls)
	}
}

func TestManagementValidatesGroupAndInput(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, &fakeMessenger{}, &fakeAuthorizer{})
	valid := RuleInput{Keyword: "你好", ReplyContent: "你好呀"}
	tests := []struct {
		name    string
		groupID int64
		input   RuleInput
		want    error
	}{
		{name: "群号非正", groupID: 0, input: valid, want: ErrInvalidGroup},
		{name: "空关键词", groupID: 100, input: RuleInput{Keyword: "", ReplyContent: "x"}, want: ErrInvalidRule},
		{name: "关键词首尾空白", groupID: 100, input: RuleInput{Keyword: " 你好", ReplyContent: "x"}, want: ErrInvalidRule},
		{name: "关键词超长", groupID: 100, input: RuleInput{Keyword: strings.Repeat("词", maxKeywordLength+1), ReplyContent: "x"}, want: ErrInvalidRule},
		{name: "空回复", groupID: 100, input: RuleInput{Keyword: "你好", ReplyContent: "   "}, want: ErrInvalidRule},
		{name: "回复超长", groupID: 100, input: RuleInput{Keyword: "你好", ReplyContent: strings.Repeat("字", maxReplyLength+1)}, want: ErrInvalidRule},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.CreateRule(context.Background(), testActor, test.groupID, test.input); !errors.Is(err, test.want) {
				t.Fatalf("CreateRule() error = %v", err)
			}
		})
	}
	if _, err := service.ListRules(context.Background(), testActor, 100, 1, maxPageSize+1); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("page size error = %v", err)
	}
	if _, err := service.UpdateRule(context.Background(), testActor, 100, 0, 1, valid); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("invalid id error = %v", err)
	}
	if err := service.DeleteRule(context.Background(), testActor, 100, 1, 0); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("invalid version error = %v", err)
	}
	// 校验失败不得触达仓库。
	if len(store.calls) != 0 {
		t.Fatalf("invalid input reached store: %v", store.calls)
	}
}

func TestWritesRefreshSnapshotFromDatabase(t *testing.T) {
	store := &fakeStore{
		rule:     Rule{ID: 1, GroupID: 100, Keyword: "你好", ReplyContent: "你好呀", Enabled: true, Version: 1},
		snapshot: map[int64]map[string]string{100: {"你好": "你好呀"}},
	}
	messenger := &fakeMessenger{}
	service := newTestService(t, store, messenger, &fakeAuthorizer{})
	if _, err := service.CreateRule(context.Background(), testActor, 100, RuleInput{Keyword: "你好", ReplyContent: "你好呀"}); err != nil {
		t.Fatal(err)
	}
	// 写入成功后必须按数据库权威结果重建快照，而不是本地拼接。
	if store.loadCalls != 1 || store.lastGroupID != 100 {
		t.Fatalf("store = %+v", store)
	}
	event := &ws.MessageEvent{MessageType: "group", GroupID: 100, MessageID: 20, RawMessage: "你好"}
	event.PostType = "message"
	if err := service.Spec().Observers[0].Handler(plugin.ObserverContext{Context: context.Background(), GroupID: 100, Event: event}); err != nil {
		t.Fatal(err)
	}
	if messenger.reply != "你好呀" {
		t.Fatalf("reply = %q", messenger.reply)
	}
	if err := service.DeleteRule(context.Background(), testActor, 100, 1, 1); err != nil {
		t.Fatal(err)
	}
	if store.loadCalls != 2 || store.lastID != 1 || store.lastVersion != 1 {
		t.Fatalf("store = %+v", store)
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if service, err := New(nil, nil, &fakeAuthorizer{}); service != nil || err == nil {
		t.Fatalf("New(nil messenger) = %v,%v", service, err)
	}
	if service, err := New(&fakeMessenger{}, nil, nil); service != nil || err == nil {
		t.Fatalf("New(nil authorizer) = %v,%v", service, err)
	}
	if service, err := New(&fakeMessenger{}, nil, &fakeAuthorizer{}); service != nil || err == nil {
		t.Fatalf("New(nil pool) = %v,%v", service, err)
	}
}
