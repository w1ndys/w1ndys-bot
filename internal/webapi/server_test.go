// 📌 影响范围：执行 Argon2id 哈希；使用内存 HTTP 测试请求，不访问数据库或网络。
package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

type fakeAdmins struct {
	accounts map[string]admin.SystemAdmin
}

type fakePlugins struct {
	states          []management.PluginState
	features        []management.FeatureState
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
	auditPage       management.AuditPage
	audit           management.AuditState
	auditQuery      management.AuditQuery
	auditID         int64
	configSchema    plugin.ConfigSchema
	configState     management.PluginConfigState
	configUpdated   management.PluginConfigState
	resources       []plugin.AdminResource
	resourcePage    management.ResourcePage
	resourceRecord  management.ResourceRecord
	resourceKey     string
	recordID        int64
	recordVersion   int64
	resourceData    json.RawMessage
	groupControl    management.PluginGroupControlState
	groupOverride   management.PluginGroupOverride
}

// GetPluginGroupControl 返回测试群控制快照。
// @param ctx：未使用；actor：操作者；name：插件名。
// @returns 预设快照或错误。
// ⚠️副作用说明：记录 Actor 和插件名。
func (f *fakePlugins) GetPluginGroupControl(_ context.Context, actor management.Actor, name string) (management.PluginGroupControlState, error) {
	f.actor, f.name = actor, name
	// >>> 数据演变示例
	// 1. keyword_reply -> groupControl。
	// 2. err -> error。
	return f.groupControl, f.err
}

// SetPluginGroupDefault 记录测试默认策略更新。
// @param ctx：未使用；actor：操作者；name：插件名；enabled/version：更新。
// @returns 预设快照或错误。
// ⚠️副作用说明：记录目标。
func (f *fakePlugins) SetPluginGroupDefault(_ context.Context, actor management.Actor, name string, _ bool, _ int64) (management.PluginGroupControlState, error) {
	f.actor, f.name = actor, name
	// >>> 数据演变示例
	// 1. false/v1 -> groupControl。
	// 2. conflict -> error。
	return f.groupControl, f.err
}

// SetPluginGroupOverride 记录测试单群覆盖。
// @param ctx：未使用；actor：操作者；name/groupID：目标；enabled/version：更新。
// @returns 预设覆盖或错误。
// ⚠️副作用说明：记录目标。
func (f *fakePlugins) SetPluginGroupOverride(_ context.Context, actor management.Actor, name, groupID string, _ bool, _ int64) (management.PluginGroupOverride, error) {
	f.actor, f.name, f.resourceKey = actor, name, groupID
	// >>> 数据演变示例
	// 1. group100 -> groupOverride。
	// 2. conflict -> error。
	return f.groupOverride, f.err
}

// DeletePluginGroupOverride 记录测试覆盖删除。
// @param ctx：未使用；actor：操作者；name/groupID：目标；version：版本。
// @returns 预设错误。
// ⚠️副作用说明：记录目标。
func (f *fakePlugins) DeletePluginGroupOverride(_ context.Context, actor management.Actor, name, groupID string, _ int64) error {
	f.actor, f.name, f.resourceKey = actor, name, groupID
	// >>> 数据演变示例
	// 1. group100 -> nil。
	// 2. conflict -> error。
	return f.err
}

// ListPluginResources 返回测试资源声明。
// @param ctx：未使用；actor：操作者；name：插件名。
// @returns 预设声明或错误。
// ⚠️副作用说明：记录 Actor 和插件名。
func (f *fakePlugins) ListPluginResources(_ context.Context, actor management.Actor, name string) ([]plugin.AdminResource, error) {
	f.actor, f.name = actor, name

	// >>> 数据演变示例
	// 1. keyword_reply -> [rules]。
	// 2. err=not supported -> error。
	return f.resources, f.err
}

// ListPluginResourceRecords 返回测试资源分页。
// @param ctx：未使用；actor：操作者；name/key：路由键；query：分页。
// @returns 预设记录页或错误。
// ⚠️副作用说明：记录路由键。
func (f *fakePlugins) ListPluginResourceRecords(_ context.Context, actor management.Actor, name, key string, _ management.ResourceQuery) (management.ResourcePage, error) {
	f.actor, f.name, f.resourceKey = actor, name, key

	// >>> 数据演变示例
	// 1. rules+page1 -> resourcePage。
	// 2. unknown -> err。
	return f.resourcePage, f.err
}

// CreatePluginResourceRecord 记录测试新增输入。
// @param ctx：未使用；actor：操作者；name/key：路由键；data：业务数据。
// @returns 预设记录或错误。
// ⚠️副作用说明：记录业务数据。
func (f *fakePlugins) CreatePluginResourceRecord(_ context.Context, actor management.Actor, name, key string, data json.RawMessage) (management.ResourceRecord, error) {
	f.actor, f.name, f.resourceKey, f.resourceData = actor, name, key, data

	// >>> 数据演变示例
	// 1. data{keyword:x} -> resourceRecord。
	// 2. duplicate -> err。
	return f.resourceRecord, f.err
}

// UpdatePluginResourceRecord 记录测试 CAS 更新。
// @param ctx：未使用；actor：操作者；name/key：路由键；id/version：CAS；data：业务数据。
// @returns 预设记录或错误。
// ⚠️副作用说明：记录更新参数。
func (f *fakePlugins) UpdatePluginResourceRecord(_ context.Context, actor management.Actor, name, key string, id, version int64, data json.RawMessage) (management.ResourceRecord, error) {
	f.actor, f.name, f.resourceKey, f.recordID, f.recordVersion, f.resourceData = actor, name, key, id, version, data

	// >>> 数据演变示例
	// 1. id1/v1 -> resourceRecord v2。
	// 2. stale -> err。
	return f.resourceRecord, f.err
}

// DeletePluginResourceRecord 记录测试 CAS 删除。
// @param ctx：未使用；actor：操作者；name/key：路由键；id/version：CAS。
// @returns 预设错误。
// ⚠️副作用说明：记录删除参数。
func (f *fakePlugins) DeletePluginResourceRecord(_ context.Context, actor management.Actor, name, key string, id, version int64) error {
	f.actor, f.name, f.resourceKey, f.recordID, f.recordVersion = actor, name, key, id, version

	// >>> 数据演变示例
	// 1. id1/v1 -> nil。
	// 2. stale -> err。
	return f.err
}

// GetPluginConfig 返回测试 Schema 与脱敏配置。
// @param ctx：未使用；actor：操作者；name：插件名。
// @returns 最小 Schema、空配置版本1或预设错误。
// ⚠️副作用说明：记录 Actor 和插件名。
func (f *fakePlugins) GetPluginConfig(_ context.Context, actor management.Actor, name string) (plugin.ConfigSchema, management.PluginConfigState, error) {
	f.actor, f.name = actor, name

	// >>> 数据演变示例
	// 1. echo -> 空Schema+{}:v1。
	// 2. err=boom -> 返回预设错误。
	state := f.configState
	// [决策理由] 零值替身保持通用合法配置响应。
	if state.Version == 0 {
		state = management.PluginConfigState{PluginName: name, ConfigJSON: json.RawMessage(`{}`), Version: 1}
	}
	return f.configSchema, state, f.err
}

// SetPluginConfig 记录并返回测试配置更新。
// @param ctx：未使用；actor：操作者；name：插件名；update：配置更新。
// @returns 版本递增的配置状态或预设错误。
// ⚠️副作用说明：记录 Actor 和插件名。
func (f *fakePlugins) SetPluginConfig(_ context.Context, actor management.Actor, name string, update management.PluginConfigUpdate) (management.PluginConfigState, error) {
	f.actor, f.name = actor, name

	// >>> 数据演变示例
	// 1. echo:v1 -> 保存 -> echo:v2。
	// 2. err=conflict -> 返回预设错误。
	state := f.configUpdated
	// [决策理由] 未预设更新结果时按期望版本构造通用成功响应。
	if state.Version == 0 {
		state = management.PluginConfigState{PluginName: name, ConfigJSON: update.ConfigJSON, Version: update.ExpectedVersion + 1}
	}
	return state, f.err
}

// ListPluginFeatures 返回测试功能元数据并记录 Actor 与插件名。
// @param ctx：未使用的上下文；actor：操作者；pluginName：插件名。
// @returns 预设功能列表或错误。
// ⚠️副作用说明：记录 Actor 和插件名。
func (f *fakePlugins) ListPluginFeatures(_ context.Context, actor management.Actor, pluginName string) ([]management.FeatureState, error) {
	f.actor, f.name = actor, pluginName

	// >>> 数据演变示例
	// 1. ping -> 记录目标 -> features,nil。
	// 2. missing+error -> 记录 -> error。
	return f.features, f.err
}

// ListAuditLogs 返回测试审计分页并记录 Actor 与查询条件。
// @param ctx：未使用的请求上下文；actor：WebUI 操作者；query：筛选分页条件。
// @returns 预设审计分页或错误。
// ⚠️副作用说明：记录 Actor 和查询条件。
func (f *fakePlugins) ListAuditLogs(_ context.Context, actor management.Actor, query management.AuditQuery) (management.AuditPage, error) {
	f.actor, f.auditQuery = actor, query

	// >>> 数据演变示例
	// 1. page1+actor100 -> 记录 -> auditPage,nil。
	// 2. err=boom -> 返回预设错误。
	return f.auditPage, f.err
}

// GetAuditLog 返回测试审计详情并记录 ID。
// @param ctx：未使用的请求上下文；actor：WebUI 操作者；id：审计ID。
// @returns 预设审计记录或错误。
// ⚠️副作用说明：记录 Actor 和审计ID。
func (f *fakePlugins) GetAuditLog(_ context.Context, actor management.Actor, id int64) (management.AuditState, error) {
	f.actor, f.auditID = actor, id

	// >>> 数据演变示例
	// 1. id8+actor100 -> 记录 -> audit,nil。
	// 2. id404+ErrAuditNotFound -> 记录 -> error。
	return f.audit, f.err
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
		states:  []management.PluginState{{Name: "ping", DisplayName: "Ping", Available: true, ConfigJSON: json.RawMessage(`{}`)}},
		updated: management.PluginState{Name: "ping", DisplayName: "Ping", Available: true, Enabled: true, ConfigJSON: json.RawMessage(`{}`)},
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
	// [决策理由] 个人源码内置插件不维护独立版本，API 不应重新暴露已移除字段。
	if strings.Contains(listRecorder.Body.String(), `"version"`) {
		t.Fatalf("插件列表仍暴露 version: body=%s", listRecorder.Body.String())
	}
	// [决策理由] 通用插件列表不得携带可能含 secret 的原始配置。
	if strings.Contains(listRecorder.Body.String(), `"config"`) {
		t.Fatalf("插件列表仍暴露 config: body=%s", listRecorder.Body.String())
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

// TestPluginConfigRoutes 验证配置 Schema、脱敏快照、更新与冲突错误映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPluginConfigRoutes(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{
		configSchema:  plugin.ConfigSchema{Fields: []plugin.ConfigField{{Key: "token", DisplayName: "令牌", Type: plugin.FieldSecret}}},
		configState:   management.PluginConfigState{PluginName: "echo", ConfigJSON: json.RawMessage(`{}`), Version: 2},
		configUpdated: management.PluginConfigState{PluginName: "echo", ConfigJSON: json.RawMessage(`{"response_prefix":"x"}`), Version: 3},
	}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 合法依赖必须成功构造配置 API。
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 配置端点必须使用有效管理员凭证。
	if err != nil {
		t.Fatal(err)
	}
	schema := requestAPI(server, token, http.MethodGet, "/api/plugins/echo/config/schema", "", "req-schema")
	// [决策理由] Schema 应返回 write-only 类型且不能携带 secret 默认值。
	if schema.Code != http.StatusOK || !strings.Contains(schema.Body.String(), `"type":"secret"`) || strings.Contains(schema.Body.String(), `"default"`) {
		t.Fatalf("schema status=%d body=%s", schema.Code, schema.Body.String())
	}
	config := requestAPI(server, token, http.MethodGet, "/api/plugins/echo/config", "", "req-config")
	// [决策理由] 配置读取必须携带版本且不出现秘密值。
	if config.Code != http.StatusOK || !strings.Contains(config.Body.String(), `"version":2`) || strings.Contains(config.Body.String(), "secret-value") {
		t.Fatalf("config status=%d body=%s", config.Code, config.Body.String())
	}
	updated := requestAPI(server, token, http.MethodPut, "/api/plugins/echo/config", `{"config":{"response_prefix":"x"},"expected_version":2}`, "req-put")
	// [决策理由] 合法 PUT 应返回热应用后的递增版本。
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"version":3`) {
		t.Fatalf("updated status=%d body=%s", updated.Code, updated.Body.String())
	}
	controller.err = admin.ErrPluginConfigConflict
	conflict := requestAPI(server, token, http.MethodPut, "/api/plugins/echo/config", `{"config":{},"expected_version":2}`, "req-conflict")
	// [决策理由] 陈旧版本必须稳定映射 409 而非通用 500。
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"plugin_config_conflict"`) {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	// >>> 数据演变示例
	// 1. GET schema/config -> 200且secret write-only -> 无秘密。
	// 2. PUT stale version -> ErrPluginConfigConflict -> 409。
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

// TestAuditRoutesListAndDetail 验证审计分页筛选、详情快照和管理员请求上下文。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestAuditRoutesListAndDetail(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	createdAt := time.Date(2026, 7, 13, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	record := management.AuditState{ID: 8, ActorID: "100", ActorRole: "super_admin", Channel: "webui", Action: "setting.set", TargetType: "system_setting", TargetID: "command_prefix", BeforeJSON: json.RawMessage(`{"Value":"/","token":"old-secret"}`), AfterJSON: json.RawMessage(`{"Value":"!","nested":{"password":"new-secret"}}`), Success: true, RequestID: "req-write", CreatedAt: createdAt}
	controller := &fakePlugins{audit: record, auditPage: management.AuditPage{Items: []management.AuditState{record}, Page: 2, PageSize: 10, Total: 21}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 完整控制器必须成功构造审计 API 服务。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 审计接口只允许已认证最高管理员访问。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	list := requestAPI(server, token, http.MethodGet, "/api/audit-logs?page=2&page_size=10&actor_id=100&action=setting.set&start_time=2026-07-13T00:00:00%2B08:00", "", "req-list-audit")
	// [决策理由] 分页、筛选和审计 Actor 必须完整传入服务，响应保留总数。
	if list.Code != http.StatusOK || controller.auditQuery.Page != 2 || controller.auditQuery.PageSize != 10 || controller.auditQuery.ActorID != "100" || controller.auditQuery.StartTime == nil || controller.actor.RequestID != "req-list-audit" || !strings.Contains(list.Body.String(), `"total":21`) || strings.Contains(list.Body.String(), `"before"`) {
		t.Fatalf("list status=%d query=%+v actor=%+v body=%s", list.Code, controller.auditQuery, controller.actor, list.Body.String())
	}
	detail := requestAPI(server, token, http.MethodGet, "/api/audit-logs/8", "", "req-detail-audit")
	// [决策理由] 详情接口必须传递ID、保留非敏感值并在服务端脱敏凭据字段。
	if detail.Code != http.StatusOK || controller.auditID != 8 || !strings.Contains(detail.Body.String(), `"Value":"/"`) || !strings.Contains(detail.Body.String(), `"token":"[已脱敏]"`) || !strings.Contains(detail.Body.String(), `"password":"[已脱敏]"`) || strings.Contains(detail.Body.String(), "old-secret") || strings.Contains(detail.Body.String(), "new-secret") || !strings.Contains(detail.Body.String(), `"created_at":"2026-07-13T02:00:00Z"`) {
		t.Fatalf("detail status=%d id=%d body=%s", detail.Code, controller.auditID, detail.Body.String())
	}

	// >>> 数据演变示例
	// 1. GET page2+actor100 -> AuditQuery -> 200 items,total21。
	// 2. GET id8 -> 完整before/after JSON -> 200详情。
}

// TestRedactAuditJSON 验证常见敏感键、嵌套数组和异常JSON均安全闭合。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestRedactAuditJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "common keys", raw: `{"API_Key":"a","authorization":"b","cookie":"c","database_dsn":"d"}`, want: `{"API_Key":"[已脱敏]","authorization":"[已脱敏]","cookie":"[已脱敏]","database_dsn":"[已脱敏]"}`},
		{name: "nested array", raw: `[{"Private-Key":"a"},{"session_id":"b"},{"enabled":true}]`, want: `[{"Private-Key":"[已脱敏]"},{"session_id":"[已脱敏]"},{"enabled":true}]`},
		{name: "invalid json", raw: `token=plain-secret`, want: `null`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := string(redactAuditJSON(json.RawMessage(test.raw)))
			// [决策理由] 脱敏输出必须结构和值完全匹配安全预期。
			if got != test.want {
				t.Fatalf("redactAuditJSON() = %s, want %s", got, test.want)
			}

			// >>> 数据演变示例
			// 1. API_Key输入 -> 规范化命中 -> [已脱敏]。
			// 2. 非法JSON -> 解析失败 -> null。
		})
	}

	// >>> 数据演变示例
	// 1. 三组用例依次执行 -> 全部匹配 -> 测试通过。
	// 2. 任一秘密未替换 -> 字符串不匹配 -> 测试失败。
}

// TestAuditRoutesRejectInvalidQueryAndMapNotFound 验证时间格式、ID和未找到错误映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestAuditRoutesRejectInvalidQueryAndMapNotFound(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 错误路径测试需要完整服务依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 必须通过认证后才能验证查询参数和领域错误。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	invalidTime := requestAPI(server, token, http.MethodGet, "/api/audit-logs?start_time=bad", "", "")
	// [决策理由] 非 RFC3339 时间应在调用控制器前返回400。
	if invalidTime.Code != http.StatusBadRequest || !strings.Contains(invalidTime.Body.String(), `"code":"invalid_audit_query"`) || controller.auditQuery.Page != 0 {
		t.Fatalf("invalid time status=%d body=%s query=%+v", invalidTime.Code, invalidTime.Body.String(), controller.auditQuery)
	}
	invalidID := requestAPI(server, token, http.MethodGet, "/api/audit-logs/abc", "", "")
	// [决策理由] 非数字审计 ID 应返回400且不调用详情服务。
	if invalidID.Code != http.StatusBadRequest || controller.auditID != 0 {
		t.Fatalf("invalid id status=%d auditID=%d", invalidID.Code, controller.auditID)
	}
	controller.err = admin.ErrAuditNotFound
	notFound := requestAPI(server, token, http.MethodGet, "/api/audit-logs/404", "", "")
	// [决策理由] 不存在审计记录应映射为404稳定业务码。
	if notFound.Code != http.StatusNotFound || !strings.Contains(notFound.Body.String(), `"code":"audit_not_found"`) {
		t.Fatalf("not found status=%d body=%s", notFound.Code, notFound.Body.String())
	}

	// >>> 数据演变示例
	// 1. start_time=bad -> 400 invalid_audit_query零控制器调用。
	// 2. id404+ErrAuditNotFound -> 404 audit_not_found。
}

// TestLoginRateLimitRejectsSixthAttempt 验证高成本密码校验具有固定窗口限流。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行多次 Argon2id 校验并创建内存 HTTP 请求。
func TestLoginRateLimitRejectsSixthAttempt(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{})
	// [决策理由] 限流测试需要合法服务实例。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	for index := 0; index < loginAttemptLimit; index++ {
		response := requestLogin(server, "100", "wrong-password-value")
		// [决策理由] 窗口内前五次应完成密码验证并返回普通认证失败。
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d", index+1, response.Code)
		}
	}
	limited := requestLogin(server, "100", "wrong-password-value")
	// [决策理由] 第六次必须在 Argon2 前返回429稳定业务码。
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), `"code":"login_rate_limited"`) {
		t.Fatalf("limited status=%d body=%s", limited.Code, limited.Body.String())
	}

	// >>> 数据演变示例
	// 1. 同IP前5次 -> 401 invalid_credentials。
	// 2. 第6次 -> 429 login_rate_limited。
}

// TestLoginRateLimitCannotBypassWithDifferentQQ 验证轮换伪造 QQ 仍共享 IP 限流窗口。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行多次 Argon2id 校验并创建内存 HTTP 请求。
func TestLoginRateLimitCannotBypassWithDifferentQQ(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, &fakePlugins{})
	// [决策理由] 限流绕过测试需要合法服务实例。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	for index := 0; index < loginAttemptLimit; index++ {
		requestLogin(server, strconv.Itoa(200+index), "wrong-password-value")
	}
	limited := requestLogin(server, "999", "wrong-password-value")
	// [决策理由] 同IP更换QQ后第六次仍必须被429拒绝。
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("rotated QQ status=%d body=%s", limited.Code, limited.Body.String())
	}

	// >>> 数据演变示例
	// 1. 同IP依次QQ200..204 -> 共用计数5。
	// 2. 同IP切换QQ999 -> 第6次 -> 429。
}

// TestLoginAttemptMapHasBoundedCleanup 验证登录限流表会淘汰过期记录并保持容量上限。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：直接填充并清理测试服务的内存限流表。
func TestLoginAttemptMapHasBoundedCleanup(t *testing.T) {
	server := &Server{loginAttempts: make(map[string]loginAttempt), loginSlots: make(chan struct{}, 2), now: time.Now}
	now := time.Unix(1_700_000_000, 0)
	for index := 0; index < loginAttemptCapacity; index++ {
		server.loginAttempts[strconv.Itoa(index)] = loginAttempt{Count: 1, WindowStart: now.Add(-2 * loginWindow)}
	}
	server.loginMu.Lock()
	server.cleanupLoginAttemptsLocked(now)
	server.loginMu.Unlock()
	// [决策理由] 所有过期窗口必须被清理，释放固定容量。
	if len(server.loginAttempts) != 0 {
		t.Fatalf("loginAttempts length=%d, want 0", len(server.loginAttempts))
	}

	// >>> 数据演变示例
	// 1. 4096条过期记录 -> cleanup -> 0条。
	// 2. 新来源随后可创建窗口且map不超过4096。
}

// TestStrictJSONAndRequestIDSanitization 验证尾随 JSON 被拒绝且非法请求ID被替换。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：创建并执行内存 HTTP 请求。
func TestStrictJSONAndRequestIDSanitization(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 请求安全测试需要完整服务依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 需要有效 Token 到达管理请求解码流程。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	trailing := requestAPI(server, token, http.MethodPatch, "/api/plugins/ping", `{"enabled":true}{}`, "")
	// [决策理由] 多个 JSON 值必须返回400且不调用控制器。
	if trailing.Code != http.StatusBadRequest || controller.enabled != nil {
		t.Fatalf("trailing status=%d enabled=%v", trailing.Code, controller.enabled)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", strings.Repeat("x", 129))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	// [决策理由] 超长客户端ID必须替换为服务端24位十六进制ID再进入Actor。
	if recorder.Code != http.StatusOK || len(controller.actor.RequestID) != 24 || controller.actor.RequestID == strings.Repeat("x", 129) {
		t.Fatalf("status=%d requestID=%q", recorder.Code, controller.actor.RequestID)
	}

	// >>> 数据演变示例
	// 1. JSON对象后追加{} -> 400 invalid_request。
	// 2. 129字符Request-ID -> 服务端随机ID -> 审计字段安全。
}

// TestPluginFeatureRouteReturnsMetadata 验证功能元数据 API 返回默认触发词与权限。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPluginFeatureRouteReturnsMetadata(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{features: []management.FeatureState{{PluginName: "ping", Key: "ping", DisplayName: "Ping", Available: true, DefaultCommands: []string{"ping"}, DefaultPermissions: json.RawMessage(`{"member":true}`)}}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 功能路由测试需要完整服务依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 功能元数据仅允许已登录管理员读取。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	response := requestAPI(server, token, http.MethodGet, "/api/plugins/ping/features", "", "req-features")
	// [决策理由] 路由必须传递插件名并返回结构化默认值。
	if response.Code != http.StatusOK || controller.name != "ping" || !strings.Contains(response.Body.String(), `"default_commands":["ping"]`) || !strings.Contains(response.Body.String(), `"default_permissions":{"member":true}`) {
		t.Fatalf("status=%d name=%s body=%s", response.Code, controller.name, response.Body.String())
	}

	// >>> 数据演变示例
	// 1. GET ping/features -> [ping默认命令和权限] -> 200。
	// 2. 控制器收到pluginName=ping -> 精确查询对应功能。
}

// TestPluginResourceRoutes 验证资源声明、分页、严格写入、CAS 删除与冲突映射。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPluginResourceRoutes(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{resources: []plugin.AdminResource{{Key: "rules", DisplayName: "规则", MaxPageSize: 50}}, resourcePage: management.ResourcePage{Items: []management.ResourceRecord{{ID: 1, Version: 2, Data: json.RawMessage(`{"keyword":"hi"}`)}}, Page: 2, PageSize: 10, Total: 11}, resourceRecord: management.ResourceRecord{ID: 1, Version: 3, Data: json.RawMessage(`{"keyword":"hello"}`)}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 资源路由测试需要完整认证和管理依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 所有资源读写都必须使用已鉴权管理员。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	listed := requestAPI(server, token, http.MethodGet, "/api/plugins/keyword_reply/resources/rules?page=2&page_size=10", "", "req-list")
	// [决策理由] 分页路由必须传递固定插件/资源键并转换记录 DTO。
	if listed.Code != http.StatusOK || controller.name != "keyword_reply" || controller.resourceKey != "rules" || !strings.Contains(listed.Body.String(), `"version":2`) {
		t.Fatalf("list status=%d name=%s key=%s body=%s", listed.Code, controller.name, controller.resourceKey, listed.Body.String())
	}
	invalid := requestAPI(server, token, http.MethodPost, "/api/plugins/keyword_reply/resources/rules", `{"data":{},"table":"admin_audit_logs"}`, "req-invalid")
	// [决策理由] 外层未知字段可能试图控制 SQL 目标，必须严格拒绝。
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	deleted := requestAPI(server, token, http.MethodDelete, "/api/plugins/keyword_reply/resources/rules/1?expected_version=3", "", "req-delete")
	// [决策理由] DELETE 必须将 int64 ID 与查询版本传递给插件。
	if deleted.Code != http.StatusOK || controller.recordID != 1 || controller.recordVersion != 3 {
		t.Fatalf("delete status=%d id=%d version=%d body=%s", deleted.Code, controller.recordID, controller.recordVersion, deleted.Body.String())
	}
	controller.err = admin.ErrPluginResourceConflict
	conflict := requestAPI(server, token, http.MethodPatch, "/api/plugins/keyword_reply/resources/rules/1", `{"data":{"keyword":"hello"},"expected_version":2}`, "req-conflict")
	// [决策理由] 插件 CAS 冲突必须映射为 409 而非泛化 500。
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"plugin_resource_conflict"`) {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	// >>> 数据演变示例
	// 1. GET page2 -> DTO页；DELETE id1/v3 -> deleted:true。
	// 2. POST含table字段 -> 400；PATCH陈旧版本 -> 409。
}

// TestPluginGroupControlRoutes 验证群控制读写、CAS 冲突与 DELETE 严格查询。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行 Argon2id 哈希并创建内存 HTTP 请求。
func TestPluginGroupControlRoutes(t *testing.T) {
	admins := &fakeAdmins{accounts: map[string]admin.SystemAdmin{"100": {UserID: "100", Enabled: true}}}
	controller := &fakePlugins{groupControl: management.PluginGroupControlState{PluginName: "keyword_reply", PluginEnabled: true, DefaultEnabled: true, DefaultVersion: 2}, groupOverride: management.PluginGroupOverride{GroupID: "100", Enabled: false, Version: 1}}
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins, controller)
	// [决策理由] 路由测试需要完整认证依赖。
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	token, err := server.sign("100")
	// [决策理由] 群控制不得匿名访问。
	if err != nil {
		t.Fatalf("sign() error = %v", err)
	}
	read := requestAPI(server, token, http.MethodGet, "/api/plugins/keyword_reply/group-control", "", "req-group-read")
	// [决策理由] 读取应返回 snake_case 版本快照。
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"default_version":2`) {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}
	created := requestAPI(server, token, http.MethodPut, "/api/plugins/keyword_reply/group-overrides/100", `{"enabled":false,"expected_version":0}`, "req-group-create")
	// [决策理由] version=0 必须传递给新增语义。
	if created.Code != http.StatusOK || controller.resourceKey != "100" {
		t.Fatalf("create status=%d group=%s body=%s", created.Code, controller.resourceKey, created.Body.String())
	}
	invalidDelete := requestAPI(server, token, http.MethodDelete, "/api/plugins/keyword_reply/group-overrides/100?expected_version=1&expected_version=2", "", "req-group-delete")
	// [决策理由] 重复 CAS 查询参数必须严格拒绝。
	if invalidDelete.Code != http.StatusBadRequest {
		t.Fatalf("delete status=%d body=%s", invalidDelete.Code, invalidDelete.Body.String())
	}
	controller.err = admin.ErrGroupControlConflict
	conflict := requestAPI(server, token, http.MethodPatch, "/api/plugins/keyword_reply/group-control", `{"enabled":false,"expected_version":2}`, "req-group-conflict")
	// [决策理由] 默认策略 CAS 冲突必须映射 409。
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"group_control_conflict"`) {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	// >>> 数据演变示例
	// 1. GET+PUT -> 200快照。
	// 2. DELETE重复version -> 400；PATCH冲突 -> 409。
}

// requestLogin 使用固定测试远端地址执行登录请求。
// @param server：测试服务；qq：登录QQ；password：登录密码。
// @returns 已完成的登录响应记录器。
// ⚠️副作用说明：执行内存 HTTP 请求并可能运行 Argon2id。
func requestLogin(server *Server, qq string, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"qq": qq, "password": password})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(string(body)))
	request.RemoteAddr = "192.0.2.1:12345"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	// >>> 数据演变示例
	// 1. QQ100+正确密码 -> 200记录器。
	// 2. QQ100+错误密码 -> 401或限流429记录器。
	return recorder
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
