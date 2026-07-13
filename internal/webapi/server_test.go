// 📌 影响范围：执行 Argon2id 哈希；使用内存 HTTP 测试请求，不访问数据库或网络。
package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
)

type fakeAdmins struct {
	accounts map[string]admin.SystemAdmin
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
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins)
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
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	// [决策理由] 登录响应必须符合统一JSON结构并包含Token。
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &response); err != nil || response.Data.Token == "" {
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
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins)
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
	server, err := New("correct-horse-battery-staple", strings.Repeat("s", 32), admins)
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
