// 📌 影响范围：启动本机临时 HTTP 服务；建立 WebSocket 测试连接。
package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestServerAuthentication 验证 WebSocket 端点的 Bearer Token 鉴权。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动并关闭本机临时 HTTP 服务及 WebSocket 连接。
func TestServerAuthentication(t *testing.T) {
	server := httptest.NewServer(NewServer("secret", func(context.Context, Event) error {
		// >>> 数据演变示例
		// 1. 合法消息 -> 测试空处理器 -> 返回 nil。
		// 2. 空消息 -> 测试空处理器 -> 返回 nil。
		return nil
	}).Handler())
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + endpoint

	unauthorizedHeader := http.Header{"Authorization": []string{"Bearer wrong"}}
	connection, response, err := websocket.DefaultDialer.Dial(wsURL, unauthorizedHeader)
	// [决策理由] 错误 Token 必须让握手失败，否则未授权 NapCat 可接入服务。
	if err == nil {
		connection.Close()
		t.Fatal("错误 Token 意外通过鉴权")
	}
	// [决策理由] 明确断言 401，避免其他服务故障被误认为鉴权有效。
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误 Token 状态码 = %v，期望 401", response)
	}

	authorizedHeader := http.Header{"Authorization": []string{"Bearer secret"}}
	connection, _, err = websocket.DefaultDialer.Dial(wsURL, authorizedHeader)
	// [决策理由] 正确 Token 应完成协议升级，证明鉴权与 WebSocket 链路兼容。
	if err != nil {
		t.Fatalf("正确 Token 连接失败: %v", err)
	}
	connection.Close()

	// >>> 数据演变示例
	// 1. Bearer wrong -> HTTP 握手 -> 401 -> 测试通过。
	// 2. Bearer secret -> HTTP 握手 -> 101 Switching Protocols -> 测试通过。
}

// TestServerDispatchesMessage 验证 OneBot 群消息可被解析并分发。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：启动临时 HTTP 服务、建立连接并向处理器通道写入事件。
func TestServerDispatchesMessage(t *testing.T) {
	received := make(chan Event, 1)
	server := httptest.NewServer(NewServer("secret", func(_ context.Context, event Event) error {
		received <- event

		// >>> 数据演变示例
		// 1. group_id=100 -> 通道写入 MessageEvent{GroupID:100} -> 返回 nil。
		// 2. raw_message="ping" -> 通道写入含 ping 的事件 -> 返回 nil。
		return nil
	}).Handler())
	defer server.Close()

	header := http.Header{"Authorization": []string{"Bearer secret"}}
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+endpoint, header)
	// [决策理由] 连接失败后无法验证消息分发，应立即结束测试并保留根因。
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer connection.Close()
	message := []byte(`{"post_type":"message","message_type":"group","group_id":100,"user_id":200,"raw_message":"ping"}`)
	// [决策理由] 写入失败表示测试链路不完整，等待通道只会造成无意义超时。
	if err := connection.WriteMessage(websocket.TextMessage, message); err != nil {
		t.Fatalf("写入消息失败: %v", err)
	}

	select {
	case receivedEvent := <-received:
		event, ok := receivedEvent.(*MessageEvent)
		// [决策理由] message 上报必须解析成 MessageEvent，而不是通用或其他事件类型。
		if !ok {
			t.Fatalf("事件类型 = %T，期望 *MessageEvent", receivedEvent)
		}
		// [决策理由] 同时验证关键标识与原始文本，防止 JSON tag 配置错误。
		if event.GroupID != 100 || event.UserID != 200 || event.RawMessage != "ping" {
			t.Fatalf("解析结果错误: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("等待消息分发超时")
	}

	// >>> 数据演变示例
	// 1. 群消息 JSON -> WebSocket -> MessageEvent{100,200,ping} -> 通道收到事件。
	// 2. 一秒内未分发 -> 超时通道触发 -> 测试失败。
}

// TestDispatchRejectsInvalidJSONAndDispatchesNotice 验证坏消息隔离和通知事件分发。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：调用内存处理器并更新局部计数器。
func TestDispatchRejectsInvalidJSONAndDispatchesNotice(t *testing.T) {
	called := 0
	server := NewServer("secret", func(context.Context, Event) error {
		called++

		// >>> 数据演变示例
		// 1. 消息事件 -> called 从 0 变 1 -> 返回 nil。
		// 2. 第二条消息 -> called 从 1 变 2 -> 返回 nil。
		return nil
	})

	err := server.dispatch(context.Background(), []byte(`{`))
	// [决策理由] 非法 JSON 必须产生错误，防止零值事件被误分发。
	if err == nil {
		t.Fatal("非法 JSON 未返回错误")
	}
	err = server.dispatch(context.Background(), []byte(`{"post_type":"notice","notice_type":"group_ban","sub_type":"ban"}`))
	// [决策理由] notice 是受支持的上报类别，必须进入统一处理器。
	if err != nil {
		t.Fatalf("分发 notice 失败: %v", err)
	}
	// [决策理由] 非法 JSON 不分发，合法 notice 应恰好分发一次。
	if called != 1 {
		t.Fatalf("处理器调用次数 = %d，期望 1", called)
	}

	// >>> 数据演变示例
	// 1. "{" -> JSON 解析失败 -> 返回错误 -> called=0。
	// 2. notice.group_ban.ban -> 统一事件处理器 -> 返回 nil -> called=1。
}

// TestEventName 验证各上报类别生成稳定的事件名称。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestEventName(t *testing.T) {
	tests := []struct {
		event Event
		want  string
	}{
		{&MessageEvent{BaseEvent: BaseEvent{PostType: "message"}, MessageType: "group", SubType: "normal"}, "message.group.normal"},
		{&MessageEvent{BaseEvent: BaseEvent{PostType: "message_sent"}, MessageType: "private", SubType: "friend"}, "message_sent.private.friend"},
		{&GroupRequestEvent{BaseEvent: BaseEvent{PostType: "request"}, RequestType: "group", SubType: "invite"}, "request.group.invite"},
		{&NoticeEvent{BaseEvent: BaseEvent{PostType: "notice"}, NoticeType: "group_decrease", SubType: "kick_me"}, "notice.group_decrease.kick_me"},
		{&HeartbeatEvent{BaseEvent: BaseEvent{PostType: "meta_event"}, MetaEventType: "heartbeat"}, "meta_event.heartbeat"},
	}
	for _, test := range tests {
		// [决策理由] 每个类别必须精确映射，避免日志查询与后续路由使用不一致名称。
		if got := test.event.Name(); got != test.want {
			t.Errorf("Name() = %q，期望 %q", got, test.want)
		}
	}

	// >>> 数据演变示例
	// 1. message + group + normal -> Name -> message.group.normal。
	// 2. meta_event + heartbeat -> Name -> meta_event.heartbeat。
}
