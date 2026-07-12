// 📌 影响范围：启动本机临时 HTTP/WebSocket 服务；不访问真实 NapCat 或数据库。
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestActionClientCallMatchesEcho 验证同步 Call 通过 echo 匹配交错连接上的响应。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动临时服务、建立 WebSocket 并交换 Action JSON。
func TestActionClientCallMatchesEcho(t *testing.T) {
	server := NewServer("secret", func(context.Context, Event) error {
		// >>> 数据演变示例
		// 1. 测试 Action 响应 -> 不进入事件处理器。
		// 2. 普通事件 -> 空处理器 -> nil。
		return nil
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	header := http.Header{"Authorization": []string{"Bearer secret"}}
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+endpoint, header)
	// [决策理由] 无连接时无法验证同一通道上的请求响应关联。
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	resultChannel := make(chan actionResult, 1)
	go func() {
		response, callErr := server.Actions().Call(context.Background(), "get_group_list", map[string]any{})
		resultChannel <- actionResult{response: response, err: callErr}

		// >>> 数据演变示例
		// 1. Call 收到响应 -> 通道写入 response,nil。
		// 2. Call 失败 -> 通道写入零值 response,error。
	}()
	var request ActionRequest
	// [决策理由] 必须读取服务端写出的 Action，才能取得动态 echo 并构造对应响应。
	if err := connection.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	// [决策理由] Action 名与 echo 格式共同验证序列化和生成器规则。
	if request.Action != "get_group_list" || !strings.HasPrefix(request.Echo, "w1ndys-bot:") {
		t.Fatalf("请求错误: %+v", request)
	}
	response := map[string]any{"status": "ok", "retcode": 0, "data": []any{}, "echo": request.Echo}
	// [决策理由] 写入相同 echo 才能验证 pending 精确匹配。
	if err := connection.WriteJSON(response); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-resultChannel:
		// [决策理由] 同步调用必须返回成功响应且保留 echo。
		if result.err != nil || !result.response.OK() || result.response.Echo != request.Echo {
			t.Fatalf("调用结果错误: %+v err=%v", result.response, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("等待 Action 响应超时")
	}

	// >>> 数据演变示例
	// 1. Call -> echo=A -> NapCat response echo=A -> Call 返回成功。
	// 2. 普通事件无 echo -> 不消费 pending -> 继续走事件处理器。
}

// TestActionClientRejectsDisconnectedAndTimesOut 验证未连接和取消行为。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：创建内存 Client 并修改 pending 表。
func TestActionClientRejectsDisconnectedAndTimesOut(t *testing.T) {
	client, err := NewActionClient()
	// [决策理由] Client 构造失败后无法验证连接状态错误。
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Call(context.Background(), "get_login_info", nil)
	// [决策理由] 未连接必须立即返回稳定哨兵错误。
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("错误 = %v，期望 ErrNotConnected", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Call(ctx, "", nil)
	// [决策理由] 空 Action 校验优先于上下文，确保无效请求不进入 pending。
	if err == nil {
		t.Fatal("空 Action 未返回错误")
	}

	// >>> 数据演变示例
	// 1. connection=nil -> Call -> ErrNotConnected。
	// 2. action="" -> Call -> 名称不能为空错误。
}

// TestActionResponseJSON 验证响应 data 保持延迟解析能力。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestActionResponseJSON(t *testing.T) {
	var response ActionResponse
	err := json.Unmarshal([]byte(`{"status":"ok","retcode":0,"data":{"user_id":1},"echo":"e1"}`), &response)
	// [决策理由] 基础响应解析失败时插件无法按需解析 data。
	if err != nil {
		t.Fatal(err)
	}
	// [决策理由] RawMessage 必须原样保留具体 Action 的差异化 data。
	if string(response.Data) != `{"user_id":1}` {
		t.Fatalf("data = %s", response.Data)
	}

	// >>> 数据演变示例
	// 1. data={user_id:1} -> RawMessage -> 插件可二次解析。
	// 2. data=[] -> RawMessage -> 列表 Action 可二次解析。
}
