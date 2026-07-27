// 📌 影响范围：执行 Argon2id 哈希；使用内存 HTTP 测试请求，不访问数据库或网络。
package webapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	"github.com/w1ndys/w1ndys-bot/internal/management"
	keywordreply "github.com/w1ndys/w1ndys-bot/plugins/keyword_reply"
)

type fakeKeywordReply struct {
	page    keywordreply.RulePage
	rule    keywordreply.Rule
	err     error
	actor   management.Actor
	groupID int64
	ruleID  int64
	version int64
	input   keywordreply.RuleInput
	page1   int
	size    int
	calls   []string
}

func (f *fakeKeywordReply) ListRules(_ context.Context, actor management.Actor, groupID int64, page, pageSize int) (keywordreply.RulePage, error) {
	f.actor, f.groupID, f.page1, f.size = actor, groupID, page, pageSize
	f.calls = append(f.calls, "list")
	return f.page, f.err
}

func (f *fakeKeywordReply) CreateRule(_ context.Context, actor management.Actor, groupID int64, input keywordreply.RuleInput) (keywordreply.Rule, error) {
	f.actor, f.groupID, f.input = actor, groupID, input
	f.calls = append(f.calls, "create")
	return f.rule, f.err
}

func (f *fakeKeywordReply) UpdateRule(_ context.Context, actor management.Actor, groupID, id, version int64, input keywordreply.RuleInput) (keywordreply.Rule, error) {
	f.actor, f.groupID, f.ruleID, f.version, f.input = actor, groupID, id, version, input
	f.calls = append(f.calls, "update")
	return f.rule, f.err
}

func (f *fakeKeywordReply) DeleteRule(_ context.Context, actor management.Actor, groupID, id, version int64) error {
	f.actor, f.groupID, f.ruleID, f.version = actor, groupID, id, version
	f.calls = append(f.calls, "delete")
	return f.err
}

func newKeywordTestServer(t *testing.T, rules *fakeKeywordReply) (*Server, string) {
	t.Helper()
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{}, &fakeRuntimeStates{}, rules, &fakeForbiddenMonitor{})
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	if err != nil {
		t.Fatal(err)
	}
	return server, token
}

func TestKeywordRuleRoutesReadAndWrite(t *testing.T) {
	rules := &fakeKeywordReply{
		page: keywordreply.RulePage{Items: []keywordreply.Rule{{ID: 1, GroupID: 100, Keyword: "你好", ReplyContent: "你好呀", Enabled: true, Version: 1}}, Page: 1, PageSize: 20, Total: 1},
		rule: keywordreply.Rule{ID: 1, GroupID: 100, Keyword: "你好", ReplyContent: "你好呀", Enabled: true, Version: 2},
	}
	server, token := newKeywordTestServer(t, rules)

	listRecorder := runtimeRequest(t, server, token, http.MethodGet, "/api/plugins/keyword_reply/groups/100/rules?page=2&page_size=50", "", "req-list")
	if listRecorder.Code != http.StatusOK || rules.groupID != 100 || rules.page1 != 2 || rules.size != 50 {
		t.Fatalf("list status=%d fake=%+v", listRecorder.Code, rules)
	}
	if !strings.Contains(listRecorder.Body.String(), `"keyword":"你好"`) {
		t.Fatalf("list body=%s", listRecorder.Body.String())
	}

	createRecorder := runtimeRequest(t, server, token, http.MethodPost, "/api/plugins/keyword_reply/groups/100/rules", `{"keyword":"你好","reply_content":"你好呀","enabled":true}`, "req-create")
	if createRecorder.Code != http.StatusOK || rules.input.Keyword != "你好" || !rules.input.Enabled {
		t.Fatalf("create status=%d input=%+v", createRecorder.Code, rules.input)
	}

	updateRecorder := runtimeRequest(t, server, token, http.MethodPut, "/api/plugins/keyword_reply/groups/100/rules/1", `{"keyword":"你好","reply_content":"改了","enabled":false,"expected_version":1}`, "req-update")
	if updateRecorder.Code != http.StatusOK || rules.ruleID != 1 || rules.version != 1 || rules.input.ReplyContent != "改了" {
		t.Fatalf("update status=%d fake=%+v", updateRecorder.Code, rules)
	}

	deleteRecorder := runtimeRequest(t, server, token, http.MethodDelete, "/api/plugins/keyword_reply/groups/100/rules/1", `{"expected_version":2}`, "req-delete")
	if deleteRecorder.Code != http.StatusOK || rules.version != 2 {
		t.Fatalf("delete status=%d fake=%+v", deleteRecorder.Code, rules)
	}
	// 管理通道与请求 ID 必须由平台注入，不能由请求体伪造。
	if rules.actor.Channel != management.ChannelWebUI || rules.actor.RequestID != "req-delete" {
		t.Fatalf("actor = %+v", rules.actor)
	}
}

func TestKeywordRuleRoutesRejectInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "群号非数字", method: http.MethodGet, path: "/api/plugins/keyword_reply/groups/abc/rules"},
		{name: "群号为零", method: http.MethodGet, path: "/api/plugins/keyword_reply/groups/0/rules"},
		{name: "新增携带版本", method: http.MethodPost, path: "/api/plugins/keyword_reply/groups/100/rules", body: `{"keyword":"你好","reply_content":"x","expected_version":1}`},
		{name: "新增未知字段", method: http.MethodPost, path: "/api/plugins/keyword_reply/groups/100/rules", body: `{"keyword":"你好","reply_content":"x","extra":1}`},
		{name: "更新缺少版本", method: http.MethodPut, path: "/api/plugins/keyword_reply/groups/100/rules/1", body: `{"keyword":"你好","reply_content":"x"}`},
		{name: "更新规则 ID 非法", method: http.MethodPut, path: "/api/plugins/keyword_reply/groups/100/rules/0", body: `{"keyword":"你好","reply_content":"x","expected_version":1}`},
		{name: "删除缺少版本", method: http.MethodDelete, path: "/api/plugins/keyword_reply/groups/100/rules/1", body: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rules := &fakeKeywordReply{}
			server, token := newKeywordTestServer(t, rules)
			recorder := runtimeRequest(t, server, token, test.method, test.path, test.body, "req-invalid")
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			// 输入校验失败不得触达服务层。
			if len(rules.calls) != 0 {
				t.Fatalf("service called: %v", rules.calls)
			}
		})
	}
}

func TestKeywordRuleRoutesMapDomainErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "规则不存在", err: keywordreply.ErrRuleNotFound, want: http.StatusNotFound, code: "keyword_rule_not_found"},
		{name: "关键词冲突", err: keywordreply.ErrRuleConflict, want: http.StatusConflict, code: "keyword_rule_conflict"},
		{name: "输入无效", err: keywordreply.ErrInvalidRule, want: http.StatusBadRequest, code: "invalid_keyword_rule"},
		{name: "群号无效", err: keywordreply.ErrInvalidGroup, want: http.StatusBadRequest, code: "invalid_keyword_rule"},
		{name: "无权限", err: admin.ErrForbidden, want: http.StatusForbidden, code: "forbidden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rules := &fakeKeywordReply{err: test.err}
			server, token := newKeywordTestServer(t, rules)
			recorder := runtimeRequest(t, server, token, http.MethodPost, "/api/plugins/keyword_reply/groups/100/rules", `{"keyword":"你好","reply_content":"x"}`, "req-error")
			if recorder.Code != test.want || !strings.Contains(recorder.Body.String(), test.code) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestKeywordRuleRoutesRequireAuthentication(t *testing.T) {
	rules := &fakeKeywordReply{}
	server, _ := newKeywordTestServer(t, rules)
	targets := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/plugins/keyword_reply/groups/100/rules"},
		{method: http.MethodPost, path: "/api/plugins/keyword_reply/groups/100/rules"},
		{method: http.MethodPut, path: "/api/plugins/keyword_reply/groups/100/rules/1"},
		{method: http.MethodDelete, path: "/api/plugins/keyword_reply/groups/100/rules/1"},
	}
	for _, target := range targets {
		recorder := runtimeRequest(t, server, "", target.method, target.path, `{"expected_version":1}`, "req-anon")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", target.method, target.path, recorder.Code)
		}
	}
	if len(rules.calls) != 0 {
		t.Fatalf("unauthenticated call reached service: %v", rules.calls)
	}
}

func TestNewRejectsMissingKeywordReplyController(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{}, &fakeRuntimeStates{}, nil, &fakeForbiddenMonitor{})
	if server != nil || err == nil {
		t.Fatalf("New() = %v,%v", server, err)
	}
}
