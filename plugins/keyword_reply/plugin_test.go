// 📌 影响范围：无外部依赖；使用内存 fake 验证关键词回复运行快照、匹配语义和资源输入边界。
package keywordreply

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type fakeMessenger struct {
	replies []string
	err     error
}

// Reply 实现测试所需Messenger接口的非引用回复分支。
// @param context.Context：上下文；event：消息事件；content：回复内容。
// @returns 固定消息ID和预设错误。
// ⚠️副作用说明：无；关键词插件不会调用此方法。
func (m *fakeMessenger) Reply(context.Context, *ws.MessageEvent, any) (int64, error) {
	// >>> 数据演变示例
	// 1. 未调用 -> replies不变。
	// 2. 意外调用 -> 返回1和预设错误。
	return 1, m.err
}

// ReplyToMessage 记录引用回复文本。
// @param context.Context：上下文；event：消息事件；messageID：引用ID；content：回复文本。
// @returns 固定消息ID和预设错误。
// ⚠️副作用说明：追加content到replies切片。
func (m *fakeMessenger) ReplyToMessage(_ context.Context, _ *ws.MessageEvent, _ int64, content string) (int64, error) {
	m.replies = append(m.replies, content)

	// >>> 数据演变示例
	// 1. content=world -> replies=[world] -> 1,nil。
	// 2. err=send failed -> 仍记录文本 -> 1,error。
	return 1, m.err
}

type fakeRepository struct {
	rules       map[string]string
	loadErr     error
	createCalls int
	updateCalls int
	deleteCalls int
	before      ruleData
	updateGate  chan struct{}
	updateEnter chan struct{}
}

// LoadEnabled 返回当前规则的独立副本。
// @param context.Context：查询上下文。
// @returns 规则副本或预设错误。
// ⚠️副作用说明：无。
func (r *fakeRepository) LoadEnabled(context.Context) (map[string]string, error) {
	// [决策理由] 预设故障用于验证旧快照保留语义。
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	result := make(map[string]string, len(r.rules))
	for keyword, reply := range r.rules {
		result[keyword] = reply
	}

	// >>> 数据演变示例
	// 1. rules{a:b} -> 独立{a:b},nil。
	// 2. loadErr -> nil,error。
	return result, nil
}

// List 返回空的有界测试页。
// @param context.Context：上下文；query：分页参数。
// @returns 保留页码和页大小的空页。
// ⚠️副作用说明：无。
func (r *fakeRepository) List(_ context.Context, query management.ResourceQuery) (management.ResourcePage, error) {
	result := management.ResourcePage{Page: query.Page, PageSize: query.PageSize, Items: []management.ResourceRecord{}}

	// >>> 数据演变示例
	// 1. page1,size20 -> 空page1,size20。
	// 2. page2,size5 -> 空page2,size5。
	return result, nil
}

// Create 记录创建调用并更新fake启用规则。
// @param context.Context：上下文；actor：操作者；input：规则输入。
// @returns 固定新记录。
// ⚠️副作用说明：递增createCalls并可能修改rules。
func (r *fakeRepository) Create(_ context.Context, _ management.Actor, input ruleInput) (management.ResourceRecord, ruleData, error) {
	r.createCalls++
	// [决策理由] 只有启用规则应进入运行快照的数据源。
	if input.Enabled {
		r.rules[input.Keyword] = input.ReplyContent
	}
	raw, _ := json.Marshal(ruleData(input))
	result := management.ResourceRecord{ID: 1, Version: 1, Data: raw}
	r.before = ruleData(input)

	// >>> 数据演变示例
	// 1. enabled a->b -> rules[a]=b -> record v1。
	// 2. disabled a->b -> rules不变 -> record v1。
	return result, ruleData(input), nil
}

// Update 记录更新调用并替换fake规则。
// @param context.Context：上下文；actor：操作者；id：ID；version：版本；input：规则输入。
// @returns 版本递增的固定记录。
// ⚠️副作用说明：递增updateCalls并修改rules。
func (r *fakeRepository) Update(_ context.Context, _ management.Actor, id int64, version int64, input ruleInput) (management.ResourceRecord, ruleData, ruleData, error) {
	r.updateCalls++
	// [决策理由] 可选测试门用于验证处理器将事务与快照发布整体串行化。
	if r.updateEnter != nil {
		r.updateEnter <- struct{}{}
		<-r.updateGate
	}
	before := r.before
	// [决策理由] 更名或禁用时fake数据源也需移除旧启用关键词。
	if before.Enabled {
		delete(r.rules, before.Keyword)
	}
	// [决策理由] 测试fake仅发布启用输入，足以验证handler刷新。
	if input.Enabled {
		r.rules[input.Keyword] = input.ReplyContent
	}
	raw, _ := json.Marshal(ruleData(input))
	result := management.ResourceRecord{ID: id, Version: version + 1, Data: raw}
	r.before = ruleData(input)

	// >>> 数据演变示例
	// 1. id1,v1,a->c -> rules更新 -> record v2。
	// 2. disabled -> 不加入rules -> record仍递增。
	return result, before, ruleData(input), nil
}

// TestResourceMutationsSerializeCommitAndSnapshot 验证后一事务不能越过前一事务的快照发布。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：启动两个更新 goroutine 并使用通道控制 fake Repository 返回顺序。
func TestResourceMutationsSerializeCommitAndSnapshot(t *testing.T) {
	repository := &fakeRepository{rules: map[string]string{"key": "v1"}, before: ruleData{Keyword: "key", ReplyContent: "v1", Enabled: true}, updateGate: make(chan struct{}), updateEnter: make(chan struct{}, 2)}
	instance := newImplementation(&fakeMessenger{}, repository)
	handler := instance.resources[0].Handler
	var wait sync.WaitGroup
	wait.Add(2)
	errorsChannel := make(chan error, 2)
	go func() {
		defer wait.Done()
		_, err := handler.Update(context.Background(), management.Actor{}, 1, 1, json.RawMessage(`{"keyword":"key","reply_content":"v2","enabled":true}`))
		errorsChannel <- err
	}()
	<-repository.updateEnter
	go func() {
		defer wait.Done()
		_, err := handler.Update(context.Background(), management.Actor{}, 1, 2, json.RawMessage(`{"keyword":"key","reply_content":"v3","enabled":true}`))
		errorsChannel <- err
	}()
	select {
	case <-repository.updateEnter:
		t.Fatal("第二个事务在第一个快照发布前进入仓库")
	case <-time.After(50 * time.Millisecond):
	}
	repository.updateGate <- struct{}{}
	<-repository.updateEnter
	repository.updateGate <- struct{}{}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		// [决策理由] 两次串行更新都应成功完成。
		if err != nil {
			t.Fatal(err)
		}
	}
	messenger := &fakeMessenger{}
	instance.messenger = messenger
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "key"})
	// [决策理由] 最终快照必须保持最后提交的v3，不能被v2倒序覆盖。
	if !reflect.DeepEqual(messenger.replies, []string{"v3"}) {
		t.Fatalf("replies = %v", messenger.replies)
	}

	// >>> 数据演变示例
	// 1. v2事务阻塞+v3请求到达 -> v3等待 -> v2发布后v3提交发布。
	// 2. 两次完成 -> DB顺序v2,v3 -> 运行快照回复v3。
}

// Delete 记录删除调用。
// @param context.Context：上下文；actor：操作者；id：ID；version：版本。
// @returns nil。
// ⚠️副作用说明：递增deleteCalls。
func (r *fakeRepository) Delete(context.Context, management.Actor, int64, int64) (ruleData, error) {
	r.deleteCalls++
	before := r.before
	// [决策理由] 删除fake记录时同步移除其启用关键词。
	if before.Enabled {
		delete(r.rules, before.Keyword)
	}

	// >>> 数据演变示例
	// 1. 第一次删除 -> deleteCalls=1 -> nil。
	// 2. 第二次删除 -> deleteCalls=2 -> nil。
	return before, nil
}

// TestHandleExactGroupMatch 验证群消息仅按原始文本完全匹配。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：运行内存fake并记录引用回复。
func TestHandleExactGroupMatch(t *testing.T) {
	messenger := &fakeMessenger{}
	repository := &fakeRepository{rules: map[string]string{"Hello": "World"}}
	instance := newImplementation(messenger, repository)
	// [决策理由] Handle前需模拟真实启用生命周期加载数据库快照。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		event       ws.Event
		wantStop    bool
		wantReplies int
	}{
		{name: "完全相等", event: &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "Hello", MessageID: 8}, wantStop: true, wantReplies: 1},
		{name: "大小写不同", event: &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "hello", MessageID: 9}, wantReplies: 1},
		{name: "额外空格", event: &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: " Hello", MessageID: 10}, wantReplies: 1},
		{name: "私聊忽略", event: &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "private", RawMessage: "Hello", MessageID: 11}, wantReplies: 1},
		{name: "自身消息忽略", event: &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message_sent"}, MessageType: "group", RawMessage: "Hello", MessageID: 12}, wantReplies: 1},
		{name: "非消息忽略", event: ws.LifecycleEvent{}, wantReplies: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := instance.Handle(context.Background(), test.event)
			// [决策理由] 仅完全命中应返回停止传播信号。
			if errors.Is(err, plugin.ErrStopPropagation) != test.wantStop {
				t.Fatalf("Handle error = %v, wantStop %v", err, test.wantStop)
			}
			// [决策理由] 未命中分支不得发送任何额外回复。
			if len(messenger.replies) != test.wantReplies {
				t.Fatalf("replies = %v, want count %d", messenger.replies, test.wantReplies)
			}
		})
	}
	// [决策理由] 回复内容必须来自快照值而不是关键词本身。
	if !reflect.DeepEqual(messenger.replies, []string{"World"}) {
		t.Fatalf("replies = %v", messenger.replies)
	}

	// >>> 数据演变示例
	// 1. group Hello -> 命中World -> ErrStopPropagation。
	// 2. private/hello/空格 -> 未命中 -> nil且不发送。
}

// TestHandleReplyFailure 验证发送失败不会伪装为停止传播。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：调用fake Messenger。
func TestHandleReplyFailure(t *testing.T) {
	messenger := &fakeMessenger{err: errors.New("send failed")}
	instance := newImplementation(messenger, &fakeRepository{rules: map[string]string{"a": "b"}})
	// [决策理由] 模拟启用后再验证发送路径。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "a"})
	// [决策理由] 错误链必须包含发送根因供诊断。
	if err == nil || err.Error() != "发送关键词回复: send failed" {
		t.Fatalf("Handle error = %v", err)
	}

	// >>> 数据演变示例
	// 1. a命中b+发送失败 -> 包装send failed。
	// 2. 未命中时不调用Messenger -> 由精确匹配测试覆盖。
}

// TestLifecycleSnapshotReload 验证加载失败保留旧快照且禁用清空快照。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：切换内存规则快照。
func TestLifecycleSnapshotReload(t *testing.T) {
	messenger := &fakeMessenger{}
	repository := &fakeRepository{rules: map[string]string{"a": "old"}}
	instance := newImplementation(messenger, repository)
	// [决策理由] 首次加载建立已知旧快照。
	if err := instance.OnEnable(context.Background()); err != nil {
		t.Fatal(err)
	}
	repository.rules = map[string]string{"a": "new"}
	repository.loadErr = errors.New("db failed")
	// [决策理由] 故障刷新应返回错误供生命周期阻止错误切换。
	if err := instance.OnEnable(context.Background()); err == nil {
		t.Fatal("OnEnable should fail")
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "a"})
	// [决策理由] 加载失败不得用新值或空值覆盖old。
	if !reflect.DeepEqual(messenger.replies, []string{"old"}) {
		t.Fatalf("replies = %v", messenger.replies)
	}
	// [决策理由] 禁用后广播路径即使被误调用也不得回复。
	if err := instance.OnDisable(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "a"})
	// [决策理由] 禁用后的回复数必须保持不变。
	if len(messenger.replies) != 1 {
		t.Fatalf("replies after disable = %v", messenger.replies)
	}

	// >>> 数据演变示例
	// 1. old快照+reload失败 -> 仍回复old。
	// 2. OnDisable -> 空快照 -> 不回复。
}

// TestResourceHandlerValidationAndRefresh 验证未知字段、空白和合法创建的刷新行为。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存fake CRUD并刷新快照。
func TestResourceHandlerValidationAndRefresh(t *testing.T) {
	repository := &fakeRepository{rules: map[string]string{}}
	messenger := &fakeMessenger{}
	instance := newImplementation(messenger, repository)
	handler := instance.resources[0].Handler
	invalid := []json.RawMessage{
		json.RawMessage(`{"keyword":"a","reply_content":"b","enabled":true,"extra":1}`),
		json.RawMessage(`{"keyword":"   ","reply_content":"b","enabled":true}`),
		json.RawMessage(`{"keyword":"a","reply_content":" ","enabled":true}`),
	}
	for _, raw := range invalid {
		_, err := handler.Create(context.Background(), management.Actor{}, raw)
		// [决策理由] 所有领域输入失败都需暴露稳定invalid错误。
		if !errors.Is(err, management.ErrInvalidResourceData) {
			t.Fatalf("Create(%s) error = %v", raw, err)
		}
	}
	// [决策理由] 无效输入不得触发任何仓库写调用。
	if repository.createCalls != 0 {
		t.Fatalf("createCalls = %d", repository.createCalls)
	}
	_, err := handler.Create(context.Background(), management.Actor{}, json.RawMessage(`{"keyword":" a ","reply_content":"reply"}`))
	// [决策理由] 包含首尾空格但非全空白的关键词必须保留并允许创建。
	if err != nil {
		t.Fatal(err)
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: " a "})
	// [决策理由] CRUD后的事务后像应让新规则无需重启立即命中。
	if !reflect.DeepEqual(messenger.replies, []string{"reply"}) {
		t.Fatalf("replies = %v", messenger.replies)
	}

	// >>> 数据演变示例
	// 1. extra字段/全空白 -> ErrInvalidResourceData且零写入。
	// 2. keyword=" a " -> 保留空格创建 -> 立即精确命中。
}

// TestResourceSnapshotTransitions 验证更名、禁用、重新启用和删除均按事务前后像更新快照。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：通过内存fake执行CRUD并调用消息处理。
func TestResourceSnapshotTransitions(t *testing.T) {
	repository := &fakeRepository{rules: map[string]string{}}
	messenger := &fakeMessenger{}
	instance := newImplementation(messenger, repository)
	handler := instance.resources[0].Handler
	actor := management.Actor{}
	created, err := handler.Create(context.Background(), actor, json.RawMessage(`{"keyword":"old","reply_content":"one","enabled":true}`))
	// [决策理由] 后续CAS转换依赖成功创建的初始版本。
	if err != nil {
		t.Fatal(err)
	}
	updated, err := handler.Update(context.Background(), actor, created.ID, created.Version, json.RawMessage(`{"keyword":"new","reply_content":"two","enabled":true}`))
	// [决策理由] 更名必须成功后才能验证新旧键切换。
	if err != nil {
		t.Fatal(err)
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "old"})
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "new"})
	// [决策理由] 更名后旧关键词必须消失且新关键词回复新内容。
	if !reflect.DeepEqual(messenger.replies, []string{"two"}) {
		t.Fatalf("rename replies = %v", messenger.replies)
	}
	disabled, err := handler.Update(context.Background(), actor, updated.ID, updated.Version, json.RawMessage(`{"keyword":"new","reply_content":"two","enabled":false}`))
	// [决策理由] 禁用提交成功后应立即从快照移除。
	if err != nil {
		t.Fatal(err)
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "new"})
	// [决策理由] 禁用规则不得追加回复。
	if len(messenger.replies) != 1 {
		t.Fatalf("disabled replies = %v", messenger.replies)
	}
	enabled, err := handler.Update(context.Background(), actor, disabled.ID, disabled.Version, json.RawMessage(`{"keyword":"new","reply_content":"three","enabled":true}`))
	// [决策理由] 重新启用需验证false到true的增量加入分支。
	if err != nil {
		t.Fatal(err)
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "new"})
	// [决策理由] 重新启用后必须回复最新内容。
	if !reflect.DeepEqual(messenger.replies, []string{"two", "three"}) {
		t.Fatalf("enabled replies = %v", messenger.replies)
	}
	// [决策理由] 删除使用当前版本，成功后必须移除关键词。
	if err := handler.Delete(context.Background(), actor, enabled.ID, enabled.Version); err != nil {
		t.Fatal(err)
	}
	_ = instance.Handle(context.Background(), &ws.MessageEvent{BaseEvent: ws.BaseEvent{PostType: "message"}, MessageType: "group", RawMessage: "new"})
	// [决策理由] 删除后的快照不得继续回复。
	if len(messenger.replies) != 2 {
		t.Fatalf("deleted replies = %v", messenger.replies)
	}

	// >>> 数据演变示例
	// 1. old->new -> 快照删除old并加入new(two)。
	// 2. new禁用->启用three->删除 -> 无规则、new=three、无规则。
}
