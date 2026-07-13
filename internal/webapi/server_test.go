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
	states          []management.PluginState
	updated         management.PluginState
	actor           management.Actor
	name            string
	enabled         *bool
	priority        *int
	err             error
	commands        []management.CommandState
	command         management.CommandState
	commandInput    management.CommandCreate
	commandID       int64
	commandName     string
	permissions     []management.PermissionState
	permission      management.PermissionState
	permissionInput management.PermissionSet
	permissionID    int64
	settings        []management.SettingState
	setting         management.SettingState
	settingKey      string
	settingValue    json.RawMessage
}

// ListSettings 返回测试设置列表并记录 Actor。
// @param ctx：未使用的请求上下文；actor：WebUI 操作者。
// @returns 预设设置列表或错误。
// ⚠️副作用说明：记录最近一次 Actor。
func (f *fakePlugins) ListSettings(_ context.Context, actor management.Actor) ([]management.SettingState, error) {
	f.actor = actor

	// >>> 数据演变示例
	// 1. settings=[prefix]+actor100 -> 记录actor -> 返回列表。
	// 2. err=boom -> 返回预设错误。
	return f.settings, f.err
}

// SetSetting 记录系统设置保存操作。
// @param ctx：未使用的上下文；actor：操作者；key：设置键；value：JSON值。
// @returns 预设设置状态或错误。
// ⚠️副作用说明：记录 Actor、设置键和 JSON 值副本。
func (f *fakePlugins) SetSetting(_ context.Context, actor management.Actor, key string, value json.RawMessage) (management.SettingState, error) {
	f.actor, f.settingKey = actor, key
	f.settingValue = append(json.RawMessage(nil), value...)

	// >>> 数据演变示例
	// 1. prefix+"!" -> 记录副本 -> setting,nil。
	// 2. page_size+500+ErrInvalidSetting -> 记录 -> error。
	return f.setting, f.err
}

// DeleteSetting 记录系统设置覆盖删除操作。
// @param ctx：未使用的上下文；actor：操作者；key：设置键。
// @returns 预设错误。
// ⚠️副作用说明：记录 Actor 和设置键。
func (f *fakePlugins) DeleteSetting(_ context.Context, actor management.Actor, key string) error {
	f.actor, f.settingKey = actor, key

	// >>> 数据演变示例
	// 1. prefix -> 记录 -> nil。
	// 2. 未覆盖prefix -> 记录 -> ErrSettingNotFound。
	return f.err
}

// ListPermissions 返回测试权限列表并记录 Actor。
// @param ctx：未使用的请求上下文；actor：WebUI 操作者。
// @returns 预设权限列表或错误。
// ⚠️副作用说明：记录最近一次 Actor。
func (f *fakePlugins) ListPermissions(_ context.Context, actor management.Actor) ([]management.PermissionState, error) {
	f.actor = actor

	// >>> 数据演变示例
	// 1. permissions=[user200 allow]+actor100 -> 记录actor -> 返回列表。
	// 2. err=boom -> 返回预设错误。
	return f.permissions, f.err
}

// SetPermission 记录权限新增或更新操作。
// @param ctx：未使用的上下文；actor：操作者；input：权限唯一维度与效果。
// @returns 预设权限状态或错误。
// ⚠️副作用说明：记录 Actor 和权限输入。
func (f *fakePlugins) SetPermission(_ context.Context, actor management.Actor, input management.PermissionSet) (management.PermissionState, error) {
	f.actor, f.permissionInput = actor, input

	// >>> 数据演变示例
	// 1. group123+user200+allow -> 记录 -> permission,nil。
	// 2. user=abc+ErrInvalidPermission -> 记录 -> error。
	return f.permission, f.err
}

// DeletePermission 记录权限删除操作。
// @param ctx：未使用的上下文；actor：操作者；id：权限ID。
// @returns 预设错误。
// ⚠️副作用说明：记录 Actor 和权限ID。
func (f *fakePlugins) DeletePermission(_ context.Context, actor management.Actor, id int64) error {
	f.actor, f.permissionID = actor, id

	// >>> 数据演变示例
	// 1. id8 -> 记录 -> nil。
	// 2. id404 -> 记录 -> ErrPermissionNotFound。
	return f.err
}

// ListCommands 返回测试命令列表并记录 Actor。
// @param ctx：未使用的请求上下文；actor：WebUI 操作者。
// @returns 预设命令列表或错误。
// ⚠️副作用说明：记录最近一次 Actor。
func (f *fakePlugins) ListCommands(_ context.Context, actor management.Actor) ([]management.CommandState, error) {
	f.actor = actor

	// >>> 数据演变示例
	// 1. commands=[ping]+actor100 -> 记录actor -> 返回列表。
	// 2. err=boom -> 返回预设错误。
	return f.commands, f.err
}

// CreateCommand 记录命令新增操作。
// @param ctx：未使用的上下文；actor：操作者；input：命令输入。
// @returns 预设命令状态或错误。
// ⚠️副作用说明：记录 Actor 和命令输入。
func (f *fakePlugins) CreateCommand(_ context.Context, actor management.Actor, input management.CommandCreate) (management.CommandState, error) {
	f.actor, f.commandInput = actor, input

	// >>> 数据演变示例
	// 1. ping.ping+测试 -> 记录 -> command,nil。
	// 2. 重复输入+ErrCommandConflict -> 记录 -> error。
	return f.command, f.err
}

// RenameCommand 记录命令改名操作。
// @param ctx：未使用的上下文；actor：操作者；id：命令ID；command：新文本。
// @returns 预设命令状态或错误。
// ⚠️副作用说明：记录 Actor、ID 和命令文本。
func (f *fakePlugins) RenameCommand(_ context.Context, actor management.Actor, id int64, command string) (management.CommandState, error) {
	f.actor, f.commandID, f.commandName = actor, id, command

	// >>> 数据演变示例
	// 1. id1+延迟 -> 记录 -> command,nil。
	// 2. id404+ErrCommandNotFound -> 记录 -> error。
	return f.command, f.err
}

// DeleteCommand 记录命令删除操作。
// @param ctx：未使用的上下文；actor：操作者；id：命令ID。
// @returns 预设错误。
// ⚠️副作用说明：记录 Actor 和命令ID。
func (f *fakePlugins) DeleteCommand(_ context.Context, actor management.Actor, id int64) error {
	f.actor, f.commandID = actor, id

	// >>> 数据演变示例
	// 1. id1 -> 记录 -> nil。
	// 2. id404 -> 记录 -> ErrCommandNotFound。
	return f.err
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

// TestCommandRoutesCRUD 验证命令 REST 接口传递字段、审计身份及统一响应。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestCommandRoutesCRUD(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{command: management.CommandState{ID: 7, ScopeType: "group", ScopeID: "123", PluginName: "ping", FeatureKey: "ping", Command: "测试", NormalizedCommand: "测试", Enabled: true}}
	controller.commands = []management.CommandState{controller.command}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 完整管理控制器必须成功构造服务。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] CRUD 路由均要求有效管理员 Token。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	list := requestAPI(server, token, http.MethodGet, "/api/commands", "", "req-list-command")
	// [决策理由] 列表必须返回 snake_case 命令数据。
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"normalized_command":"测试"`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	create := requestAPI(server, token, http.MethodPost, "/api/commands", `{"scope_type":"group","scope_id":"123","plugin_name":"ping","feature_key":"ping","command":"测试"}`, "req-create-command")
	// [决策理由] 新增字段和请求 ID 必须完整进入管理服务。
	if create.Code != http.StatusOK || controller.commandInput.ScopeID != "123" || controller.actor.RequestID != "req-create-command" {
		t.Fatalf("create status=%d input=%+v actor=%+v", create.Code, controller.commandInput, controller.actor)
	}
	rename := requestAPI(server, token, http.MethodPatch, "/api/commands/7", `{"command":"延迟"}`, "req-rename-command")
	// [决策理由] 路径 ID 和新命令文本必须精确传递。
	if rename.Code != http.StatusOK || controller.commandID != 7 || controller.commandName != "延迟" {
		t.Fatalf("rename status=%d id=%d name=%q", rename.Code, controller.commandID, controller.commandName)
	}
	deleted := requestAPI(server, token, http.MethodDelete, "/api/commands/7", "", "req-delete-command")
	// [决策理由] 删除成功应返回统一成功结构和 data:null。
	if deleted.Code != http.StatusOK || controller.commandID != 7 || !strings.Contains(deleted.Body.String(), `"data":null`) {
		t.Fatalf("delete status=%d id=%d body=%s", deleted.Code, controller.commandID, deleted.Body.String())
	}

	// >>> 数据演变示例
	// 1. POST命令 -> Controller.Create+审计Actor -> 200命令DTO。
	// 2. DELETE id7 -> Controller.Delete+热刷新 -> 200 data:null。
}

// TestCommandRoutesMapValidationAndConflict 验证非法 ID 和命令冲突错误映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestCommandRoutesMapValidationAndConflict(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 错误路径测试也需要完整服务依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 需要通过认证后才能到达路径和领域校验。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	invalidID := requestAPI(server, token, http.MethodDelete, "/api/commands/abc", "", "")
	// [决策理由] 非数字 ID 应返回400且不调用控制器。
	if invalidID.Code != http.StatusBadRequest || controller.commandID != 0 {
		t.Fatalf("invalid id status=%d commandID=%d", invalidID.Code, controller.commandID)
	}
	controller.err = admin.ErrCommandConflict
	conflict := requestAPI(server, token, http.MethodPost, "/api/commands", `{"scope_type":"global","scope_id":"0","plugin_name":"ping","feature_key":"ping","command":"ping"}`, "")
	// [决策理由] 同作用域重复命令必须映射为409稳定业务码。
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"command_conflict"`) {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	// >>> 数据演变示例
	// 1. DELETE abc -> ID解析失败 -> 400零调用。
	// 2. POST重复命令 -> ErrCommandConflict -> 409 command_conflict。
}

// TestPermissionRoutesListSetAndDelete 验证权限 REST 接口的列表、幂等保存与删除链路。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPermissionRoutesListSetAndDelete(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{permission: management.PermissionState{ID: 8, ScopeType: "group", ScopeID: "123", PluginName: "ping", SubjectType: "user", SubjectID: "200", Effect: "allow"}}
	controller.permissions = []management.PermissionState{controller.permission}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 完整管理控制器必须成功构造权限 API 服务。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 权限接口均要求有效管理员 Token。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	list := requestAPI(server, token, http.MethodGet, "/api/permissions", "", "req-list-permission")
	// [决策理由] 列表必须返回主体类型、QQ 和效果字段。
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"subject_type":"user"`) || !strings.Contains(list.Body.String(), `"subject_id":"200"`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	set := requestAPI(server, token, http.MethodPost, "/api/permissions", `{"scope_type":"group","scope_id":"123","plugin_name":"ping","feature_key":"","subject_type":"user","subject_id":"200","effect":"allow"}`, "req-set-permission")
	// [决策理由] 插件级空功能键、用户主体和审计请求ID必须完整传入服务。
	if set.Code != http.StatusOK || controller.permissionInput.FeatureKey != "" || controller.permissionInput.SubjectID != "200" || controller.actor.RequestID != "req-set-permission" {
		t.Fatalf("set status=%d input=%+v actor=%+v", set.Code, controller.permissionInput, controller.actor)
	}
	deleted := requestAPI(server, token, http.MethodDelete, "/api/permissions/8", "", "req-delete-permission")
	// [决策理由] 删除应传递正整数ID并返回统一空数据成功响应。
	if deleted.Code != http.StatusOK || controller.permissionID != 8 || !strings.Contains(deleted.Body.String(), `"data":null`) {
		t.Fatalf("delete status=%d id=%d body=%s", deleted.Code, controller.permissionID, deleted.Body.String())
	}

	// >>> 数据演变示例
	// 1. POST群级插件用户权限 -> UPSERT+审计Actor -> 200权限DTO。
	// 2. DELETE id8 -> 删除+热刷新 -> 200 data:null。
}

// TestPermissionRoutesMapInvalidAndNotFound 验证权限校验与未找到错误映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPermissionRoutesMapInvalidAndNotFound(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{err: admin.ErrInvalidPermission}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 错误映射测试需要完整服务依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 需要通过认证后才能到达领域错误映射。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	invalid := requestAPI(server, token, http.MethodPost, "/api/permissions", `{"scope_type":"group","scope_id":"123","plugin_name":"ping","feature_key":"","subject_type":"user","subject_id":"abc","effect":"allow"}`, "")
	// [决策理由] 无效主体应返回400稳定业务码。
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"invalid_permission"`) {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	controller.err = admin.ErrPermissionNotFound
	notFound := requestAPI(server, token, http.MethodDelete, "/api/permissions/404", "", "")
	// [决策理由] 不存在的权限策略应映射为404。
	if notFound.Code != http.StatusNotFound || !strings.Contains(notFound.Body.String(), `"code":"permission_not_found"`) {
		t.Fatalf("not found status=%d body=%s", notFound.Code, notFound.Body.String())
	}

	// >>> 数据演变示例
	// 1. user=abc -> ErrInvalidPermission -> 400 invalid_permission。
	// 2. DELETE id404 -> ErrPermissionNotFound -> 404 permission_not_found。
}

// TestSettingRoutesListSetAndDelete 验证受控系统设置的查询、覆盖和恢复默认链路。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestSettingRoutesListSetAndDelete(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{setting: management.SettingState{Key: "command_prefix", Value: json.RawMessage(`"!"`), Description: "机器人命令前缀", Overridden: true}}
	controller.settings = []management.SettingState{controller.setting, {Key: "default_page_size", Value: json.RawMessage(`20`), Description: "管理列表默认分页大小"}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 完整控制器必须成功构造设置 API 服务。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 设置接口均要求有效管理员 Token。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	list := requestAPI(server, token, http.MethodGet, "/api/settings", "", "req-list-setting")
	// [决策理由] 列表必须保留 JSON 值类型和覆盖状态。
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"value":"!"`) || !strings.Contains(list.Body.String(), `"overridden":true`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	set := requestAPI(server, token, http.MethodPut, "/api/settings/command_prefix", `{"value":"!"}`, "req-set-setting")
	// [决策理由] 设置键、原始 JSON 和请求ID必须完整传入管理服务。
	if set.Code != http.StatusOK || controller.settingKey != "command_prefix" || string(controller.settingValue) != `"!"` || controller.actor.RequestID != "req-set-setting" {
		t.Fatalf("set status=%d key=%s value=%s actor=%+v", set.Code, controller.settingKey, controller.settingValue, controller.actor)
	}
	deleted := requestAPI(server, token, http.MethodDelete, "/api/settings/command_prefix", "", "req-delete-setting")
	// [决策理由] 删除覆盖应传递设置键并返回统一空数据成功响应。
	if deleted.Code != http.StatusOK || controller.settingKey != "command_prefix" || !strings.Contains(deleted.Body.String(), `"data":null`) {
		t.Fatalf("delete status=%d key=%s body=%s", deleted.Code, controller.settingKey, deleted.Body.String())
	}

	// >>> 数据演变示例
	// 1. PUT prefix="!" -> UPSERT+审计+热刷新 -> 200 overridden=true。
	// 2. DELETE prefix -> 删除覆盖+回退默认/ -> 200 data:null。
}

// TestSettingRoutesMapInvalidAndUnknown 验证设置值、未知键及无覆盖错误映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestSettingRoutesMapInvalidAndUnknown(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{err: admin.ErrInvalidSetting}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 错误映射测试需要完整服务依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 必须通过认证后才能到达设置领域错误映射。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	invalid := requestAPI(server, token, http.MethodPut, "/api/settings/default_page_size", `{"value":500}`, "")
	// [决策理由] 超范围值应映射为400稳定业务码。
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"invalid_setting"`) {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	controller.err = admin.ErrUnknownSetting
	unknown := requestAPI(server, token, http.MethodPut, "/api/settings/db_password", `{"value":"secret"}`, "")
	// [决策理由] 未注册敏感键应映射为404且不成为动态设置。
	if unknown.Code != http.StatusNotFound || !strings.Contains(unknown.Body.String(), `"code":"unknown_setting"`) {
		t.Fatalf("unknown status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	controller.err = admin.ErrSettingNotFound
	missingOverride := requestAPI(server, token, http.MethodDelete, "/api/settings/command_prefix", "", "")
	// [决策理由] 已使用默认值时再次删除应明确返回无覆盖状态。
	if missingOverride.Code != http.StatusNotFound || !strings.Contains(missingOverride.Body.String(), `"code":"setting_override_not_found"`) {
		t.Fatalf("missing override status=%d body=%s", missingOverride.Code, missingOverride.Body.String())
	}

	// >>> 数据演变示例
	// 1. page_size=500 -> ErrInvalidSetting -> 400 invalid_setting。
	// 2. db_password -> ErrUnknownSetting -> 404 unknown_setting。
}

// requestAPI 执行携带管理员 Token 的通用内存 API 请求。
// @param server：测试服务；token：JWT；method、path、body：请求参数；requestID：审计请求ID。
// @returns 已完成的响应记录器。
// ⚠️副作用说明：创建并执行内存 HTTP 请求。
func requestAPI(server *Server, token string, method string, path string, body string, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	// [决策理由] 非空请求ID才写入请求头，覆盖客户端提供与服务端生成两种路径。
	if requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	// >>> 数据演变示例
	// 1. GET /api/commands+Token -> 200记录器。
	// 2. DELETE /api/commands/abc+Token -> 400记录器。
	return recorder
}
