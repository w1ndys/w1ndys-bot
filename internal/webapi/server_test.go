// 📌 影响范围：执行 Argon2id 哈希；使用内存 HTTP 测试请求，不访问数据库或网络。
package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	"github.com/w1ndys/w1ndys-bot/internal/management"
)

type fakeAdmins struct {
	accounts map[string]admin.SystemAdmin
}

type fakePlugins struct {
	states   []management.PluginState
	updated  management.PluginState
	actor    management.Actor
	name     string
	enabled  *bool
	priority *int
	err      error
}

// ListPlugins 返回测试插件列表并记录 Actor。
// @param ctx：未使用的请求上下文；actor：WebUI 操作者。
// @returns 预设插件列表或错误。
// ⚠️副作用说明：记录最近一次 Actor。
func (f *fakePlugins) ListPlugins(_ context.Context, actor management.Actor) ([]management.PluginState, error) {
	f.actor = actor

	// >>> 数据演变示例
	// 1. states=[ping]+actor100 -> 记录actor -> 返回列表。
	// 2. err=boom -> 返回预设错误。
	return f.states, f.err
}

// SetPluginEnabled 记录插件启停操作。
// @param ctx：未使用的请求上下文；actor：操作者；name：插件名；enabled：目标状态。
// @returns 预设更新状态或错误。
// ⚠️副作用说明：记录 Actor、插件名及启用状态。
func (f *fakePlugins) SetPluginEnabled(_ context.Context, actor management.Actor, name string, enabled bool) (management.PluginState, error) {
	f.actor, f.name, f.enabled = actor, name, &enabled

	// >>> 数据演变示例
	// 1. ping+true -> 记录 -> updated,nil。
	// 2. admin+false+ErrProtected -> 记录 -> error。
	return f.updated, f.err
}

// SetPluginPriority 记录插件优先级操作。
// @param ctx：未使用的请求上下文；actor：操作者；name：插件名；priority：目标优先级。
// @returns 预设更新状态或错误。
// ⚠️副作用说明：记录 Actor、插件名及优先级。
func (f *fakePlugins) SetPluginPriority(_ context.Context, actor management.Actor, name string, priority int) (management.PluginState, error) {
	f.actor, f.name, f.priority = actor, name, &priority

	// >>> 数据演变示例
	// 1. ping+100 -> 记录 -> updated,nil。
	// 2. missing+10+error -> 记录 -> error。
	return f.updated, f.err
}

// Resolve 从测试映射返回管理员。
// @param userID：测试 QQ 号。
// @returns 管理员状态及是否存在。
// ⚠️副作用说明：无。
func (f *fakeAdmins) Resolve(userID string) (admin.SystemAdmin, bool) {
	account, exists := f.accounts[userID]

	// >>> 数据演变示例
	// 1. map{100}+100 -> account,true。
	// 2. map{100}+200 -> 零值,false。
	return account, exists
}

// TestLoginAndMe 验证有效管理员登录后可使用 JWT 查询当前身份。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestLoginAndMe(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Nickname: "root", Enabled: true}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{})
	// [决策理由] 合法配置必须成功构造服务。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"qq":"100","password":"correct-horse-battery-staple"}`))
	loginRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(loginRecorder, login)
	// [决策理由] 正确身份必须得到令牌而不是认证错误。
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var response struct {
		Code string `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	// [决策理由] 登录响应必须符合统一JSON结构并包含Token。
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &response); err != nil || response.Code != "ok" || response.Data.Token == "" {
		t.Fatalf("login response = %#v error=%v", response, err)
	}
	me := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	me.Header.Set("Authorization", "Bearer "+response.Data.Token)
	meRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(meRecorder, me)
	// [决策理由] 有效Token必须能读取当前管理员且带安全头。
	if meRecorder.Code != http.StatusOK || !strings.Contains(meRecorder.Body.String(), `"UserID":"100"`) || meRecorder.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("me status=%d body=%s headers=%v", meRecorder.Code, meRecorder.Body.String(), meRecorder.Header())
	}

	// >>> 数据演变示例
	// 1. 100+正确密码 -> JWT -> /me 200及管理员100。
	// 2. 响应缺Token -> JSON断言失败 -> 测试失败。
}

// TestLoginRejectsInvalidCredentials 验证错误密码和非管理员使用相同失败响应。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行多次 Argon2id 校验并创建内存 HTTP 请求。
func TestLoginRejectsInvalidCredentials(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{})
	// [决策理由] 测试前置服务必须构造成功。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	bodies := []string{
		`{"qq":"100","password":"wrong-password-value"}`,
		`{"qq":"200","password":"correct-horse-battery-staple"}`,
	}
	var firstBody string
	for index, body := range bodies {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body)))
		// [决策理由] 账号或密码任一无效都必须返回401。
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("case %d status=%d body=%s", index, recorder.Code, recorder.Body.String())
		}
		// [决策理由] 两类失败内容必须一致，避免枚举管理员QQ。
		if index == 0 {
			firstBody = recorder.Body.String()
		} else if recorder.Body.String() != firstBody {
			t.Fatalf("credential responses differ: %q != %q", recorder.Body.String(), firstBody)
		}
	}

	// >>> 数据演变示例
	// 1. 管理员+错误密码 -> 401 invalid_credentials。
	// 2. 非管理员+正确密码 -> 相同401响应。
}

// TestMeRejectsExpiredAndRevokedToken 验证过期 Token 与管理员热禁用均立即失效。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并修改测试内存管理员映射。
func TestMeRejectsExpiredAndRevokedToken(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{})
	// [决策理由] 测试前置服务必须构造成功。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	current := time.Unix(1_700_000_000, 0)
	server.now = func() time.Time { return current }
	token, err := server.sign("100")
	// [决策理由] 合法管理员必须可签发测试Token。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	delete(admins.accounts, "100")
	revoked := requestMe(server, token)
	// [决策理由] 管理员从快照移除后旧Token必须被拒绝。
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked status = %d", revoked.Code)
	}
	admins.accounts["100"] = admin.SystemAdmin{UserID: "100", Enabled: true}
	current = current.Add(tokenLifetime + time.Second)
	expired := requestMe(server, token)
	// [决策理由] 超过exp后即使管理员恢复启用也必须重新登录。
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired status = %d", expired.Code)
	}

	// >>> 数据演变示例
	// 1. 有效Token+管理员删除 -> 快照复核失败 -> 401。
	// 2. 有效管理员+Token超过12小时 -> exp校验失败 -> 401。
}

// requestMe 使用指定 Token 调用当前身份接口。
// @param server：测试服务；token：Bearer Token。
// @returns 已完成的响应记录器。
// ⚠️副作用说明：创建并执行内存 HTTP 请求。
func requestMe(server *Server, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	// >>> 数据演变示例
	// 1. 有效Token -> Handler -> 200记录器。
	// 2. 过期Token -> Handler -> 401记录器。
	return recorder
}

// TestPluginRoutesListAndPatch 验证插件管理路由传递审计身份并返回稳定 JSON。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPluginRoutesListAndPatch(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	plugins := &fakePlugins{
		states:  []management.PluginState{{Name: "ping", DisplayName: "Ping", Version: "1.0.0", Available: true, ConfigJSON: json.RawMessage(`{}`)}},
		updated: management.PluginState{Name: "ping", DisplayName: "Ping", Version: "1.0.0", Available: true, Enabled: true, ConfigJSON: json.RawMessage(`{}`)},
	}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, plugins)
	// [决策理由] 合法依赖必须构造插件 API 服务。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 受保护路由测试需要有效管理员 Token。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	listRequest.Header.Set("Authorization", "Bearer "+token)
	listRequest.Header.Set("X-Request-ID", "req-list")
	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, listRequest)
	// [决策理由] 列表接口必须返回 snake_case 元数据并传递 WebUI Actor。
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), `"display_name":"Ping"`) || plugins.actor.ID != "100" || plugins.actor.RequestID != "req-list" {
		t.Fatalf("list status=%d body=%s actor=%+v", listRecorder.Code, listRecorder.Body.String(), plugins.actor)
	}
	patchRequest := httptest.NewRequest(http.MethodPatch, "/api/plugins/ping", strings.NewReader(`{"enabled":true}`))
	patchRequest.Header.Set("Authorization", "Bearer "+token)
	patchRequest.Header.Set("X-Request-ID", "req-patch")
	patchRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(patchRecorder, patchRequest)
	// [决策理由] PATCH 必须调用启停服务并把请求ID带入审计 Actor。
	if patchRecorder.Code != http.StatusOK || plugins.name != "ping" || plugins.enabled == nil || !*plugins.enabled || plugins.actor.RequestID != "req-patch" {
		t.Fatalf("patch status=%d body=%s plugin=%s enabled=%v actor=%+v", patchRecorder.Code, patchRecorder.Body.String(), plugins.name, plugins.enabled, plugins.actor)
	}

	// >>> 数据演变示例
	// 1. GET+Token -> Actor100:req-list -> Service列表 -> 200 DTO。
	// 2. PATCH ping enabled=true -> Actor100:req-patch -> Service热更新 -> 200。
}

// TestPatchPluginRejectsAmbiguousAndProtectedChanges 验证冲突字段与受保护插件错误映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPatchPluginRejectsAmbiguousAndProtectedChanges(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	plugins := &fakePlugins{}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, plugins)
	// [决策理由] 测试服务必须使用完整依赖构造成功。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 受保护路由测试必须先签发有效 Token。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	ambiguous := requestPluginPatch(server, token, "ping", `{"enabled":true,"priority":10}`)
	// [决策理由] 双字段请求可能半成功，必须在调用管理服务前拒绝。
	if ambiguous.Code != http.StatusBadRequest || plugins.name != "" {
		t.Fatalf("ambiguous status=%d plugin=%q", ambiguous.Code, plugins.name)
	}
	plugins.err = admin.ErrProtectedPlugin
	protected := requestPluginPatch(server, token, "admin", `{"enabled":false}`)
	// [决策理由] 系统插件禁用应稳定映射为409冲突。
	if protected.Code != http.StatusConflict || !strings.Contains(protected.Body.String(), `"code":"protected_plugin"`) || !strings.Contains(protected.Body.String(), `"data":null`) {
		t.Fatalf("protected status=%d body=%s", protected.Code, protected.Body.String())
	}

	// >>> 数据演变示例
	// 1. enabled+priority -> 参数歧义 -> 400零调用。
	// 2. admin enabled=false -> ErrProtectedPlugin -> 409。
}

// requestPluginPatch 调用受保护插件 PATCH 接口。
// @param server：测试服务；token：管理员Token；name：插件名；body：JSON请求体。
// @returns 已完成的 HTTP 响应记录器。
// ⚠️副作用说明：创建并执行内存 HTTP 请求。
func requestPluginPatch(server *Server, token string, name string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPatch, "/api/plugins/"+name, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	// >>> 数据演变示例
	// 1. ping+enabled=true -> PATCH处理 -> 200记录器。
	// 2. admin+enabled=false -> 领域拒绝 -> 409记录器。
	return recorder
}
