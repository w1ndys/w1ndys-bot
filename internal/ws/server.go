// 📌 影响范围：读取 HTTP Authorization 请求头；升级并维护 WebSocket 连接；调用注入的消息处理器；写入标准日志。
package ws

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

const endpoint = "/onebot/v11/ws"

// MessageHandler 处理解析完成的 OneBot 11 上报事件。
type MessageHandler func(context.Context, MessageEvent) error

// Server 表示 NapCat 反向 WebSocket 接入服务。
type Server struct {
	token    string
	handler  MessageHandler
	upgrader websocket.Upgrader
}

// NewServer 创建反向 WebSocket 服务。
// @param token：NapCat 连接鉴权 Token；handler：消息事件处理函数。
// @returns 初始化完成的 Server。
// ⚠️副作用说明：无；仅在内存中构造服务对象。
func NewServer(token string, handler MessageHandler) *Server {
	server := &Server{
		token:   token,
		handler: handler,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(_ *http.Request) bool {
				// >>> 数据演变示例
				// 1. NapCat 无 Origin 请求 -> 允许升级 -> 返回 true。
				// 2. NapCat 携带 Origin 请求 -> 依赖 Token 鉴权 -> 返回 true。
				return true
			},
		},
	}

	// >>> 数据演变示例
	// 1. token=secret + 有处理器 -> Server{token:secret, handler:fn} -> 返回服务。
	// 2. token="" + nil 处理器 -> Server{token:"", handler:nil} -> 返回服务并在请求阶段拒绝。
	return server
}

// Handler 返回服务的 HTTP 路由。
// @param 无。
// @returns 仅暴露 OneBot WebSocket 端点的 http.Handler。
// ⚠️副作用说明：在内存中创建新的 ServeMux。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(endpoint, s.serveWS)

	// >>> 数据演变示例
	// 1. GET /onebot/v11/ws -> 匹配 serveWS -> 进入鉴权流程。
	// 2. GET /unknown -> ServeMux -> 返回 404。
	return mux
}

// serveWS 鉴权并处理单个 NapCat WebSocket 连接。
// @param writer：HTTP 响应写入器；request：升级请求。
// @returns 无。
// ⚠️副作用说明：可能写入 HTTP 响应、升级网络连接、读取消息并调用外部处理器。
func (s *Server) serveWS(writer http.ResponseWriter, request *http.Request) {
	// [决策理由] 空服务端 Token 会导致任何客户端都可能通过鉴权，因此视为配置错误并拒绝连接。
	if s.token == "" {
		http.Error(writer, "server token is not configured", http.StatusServiceUnavailable)
		return
	}
	// [决策理由] 使用恒定时间比较降低 Token 长度相同时的时序侧信道风险。
	if !validToken(request.Header.Get("Authorization"), s.token) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}

	connection, err := s.upgrader.Upgrade(writer, request, nil)
	// [决策理由] 升级失败后响应已由 Gorilla 写入，继续读消息没有有效连接。
	if err != nil {
		log.Printf("升级 WebSocket 失败: %v", err)
		return
	}
	defer connection.Close()

	for {
		_, payload, err := connection.ReadMessage()
		// [决策理由] 客户端关闭或网络错误意味着本连接生命周期结束。
		if err != nil {
			// [决策理由] 正常关闭不应作为异常污染日志。
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("读取 WebSocket 消息失败: %v", err)
			}
			return
		}
		// [决策理由] 单条坏消息不应中断长连接，记录后继续读取下一条事件。
		if err := s.dispatch(request.Context(), payload); err != nil {
			log.Printf("处理 OneBot 事件失败: %v", err)
		}
	}

	// >>> 数据演变示例
	// 1. Bearer secret + 合法群消息 -> 升级连接 -> 解析事件 -> 调用 handler。
	// 2. Bearer wrong -> Token 校验失败 -> HTTP 401 -> 不升级连接。
}

// dispatch 解析并分发一条 OneBot 11 上报事件。
// @param ctx：请求上下文；payload：WebSocket JSON 消息。
// @returns JSON、事件类型或处理器返回的错误。
// ⚠️副作用说明：成功时调用注入的消息处理器。
func (s *Server) dispatch(ctx context.Context, payload []byte) error {
	var event MessageEvent
	// [决策理由] 非法 JSON 无法可靠识别事件字段，必须拒绝分发。
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.New("解析 OneBot JSON 失败")
	}
	// [决策理由] 处理器缺失表示服务组装错误，返回明确错误避免空指针调用。
	if s.handler == nil {
		return errors.New("消息处理器未配置")
	}

	// >>> 数据演变示例
	// 1. {post_type:message,group_id:1} -> MessageEvent -> handler(event) -> 返回处理结果。
	// 2. {post_type:notice,notice_type:friend_add} -> MessageEvent -> handler(event) -> 返回处理结果。
	return s.handler(ctx, event)
}

// validToken 校验 Bearer Token。
// @param authorization：Authorization 请求头；expected：服务端预共享 Token。
// @returns Token 格式正确且内容匹配时返回 true。
// ⚠️副作用说明：无。
func validToken(authorization string, expected string) bool {
	const prefix = "Bearer "
	// [决策理由] OneBot 鉴权使用 Bearer 方案，其他格式不得被当作 Token 接受。
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	actual := strings.TrimPrefix(authorization, prefix)

	// >>> 数据演变示例
	// 1. "Bearer secret" + expected=secret -> 提取 secret -> 恒定时间比较 -> true。
	// 2. "Token secret" + expected=secret -> Bearer 格式检查失败 -> false。
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
