// 📌 影响范围：读取进程环境变量和命令行参数；连接 PostgreSQL；监听 TCP 端口；写入标准日志；监听进程信号。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// main 启动机器人基础设施。
// @param 无。
// @returns 无。
// ⚠️副作用说明：读取运行参数、创建数据库连接、注册信号监听并输出日志；启动失败时终止进程。
func main() {
	cfg, err := config.Load()
	// [决策理由] 配置不完整时继续启动会产生含糊的连接错误，因此立即终止。
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.Database)
	// [决策理由] 数据库是基础依赖，连接不可用时服务不具备可运行条件。
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()
	wsServer := ws.NewServer(cfg.NapCatToken, func(_ context.Context, event ws.Event) error {
		logEvent(event)

		// >>> 数据演变示例
		// 1. message.group.normal -> 提取消息、群和用户字段 -> 写入“收到消息事件”日志。
		// 2. notice.group_ban.ban -> 提取群、操作者和时长 -> 写入“收到通知事件”日志。
		return nil
	})
	httpServer := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", cfg.WSPort),
		Handler: wsServer.Handler(),
	}
	go func() {
		err := httpServer.ListenAndServe()
		// [决策理由] 主动关闭会返回 ErrServerClosed，属于正常退出而非服务故障。
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("WebSocket 服务异常退出: %v", err)
			stop()
		}

		// >>> 数据演变示例
		// 1. 监听成功 -> 持续接收连接 -> Shutdown -> ErrServerClosed -> 静默结束。
		// 2. 端口被占用 -> ListenAndServe 错误 -> 记录日志 -> 通知主流程退出。
	}()

	log.Printf("基础框架已启动，WS 端口=%d，日志级别=%s", cfg.WSPort, cfg.LogLevel)
	<-ctx.Done()
	// [决策理由] 收到退出信号后停止接受新连接，并等待活跃请求结束。
	if err := httpServer.Shutdown(context.Background()); err != nil {
		log.Printf("关闭 WebSocket 服务失败: %v", err)
	}
	log.Print("基础框架正在关闭")

	// >>> 数据演变示例
	// 1. 有效环境变量 + 可连接数据库 -> Config -> pgxpool -> WS 服务 -> 等待退出信号 -> 正常关闭。
	// 2. 缺少 DB_PASSWORD -> 配置校验错误 -> 输出错误日志 -> 进程终止。
}

// logEvent 按强类型事件输出其专属关键字段。
// @param event：已解析的 OneBot 事件。
// @returns 无。
// ⚠️副作用说明：向标准日志写入一条事件记录。
func logEvent(event ws.Event) {
	switch current := event.(type) {
	case *ws.MessageEvent:
		log.Printf("收到消息事件 type=%s self_id=%d group_id=%d user_id=%d message_id=%d raw_message=%q", current.Name(), current.SelfID, current.GroupID, current.UserID, current.MessageID, current.RawMessage)
	case *ws.HeartbeatEvent:
		log.Printf("收到元事件 type=%s self_id=%d interval=%d status={%s}", current.Name(), current.SelfID, current.Interval, current.Status.String())
	case *ws.LifecycleEvent:
		log.Printf("收到元事件 type=%s self_id=%d", current.Name(), current.SelfID)
	case *ws.FriendRequestEvent:
		log.Printf("收到请求事件 type=%s user_id=%d comment=%q flag=%q", current.Name(), current.UserID, current.Comment, current.Flag)
	case *ws.GroupRequestEvent:
		log.Printf("收到请求事件 type=%s group_id=%d user_id=%d comment=%q flag=%q", current.Name(), current.GroupID, current.UserID, current.Comment, current.Flag)
	case *ws.GroupBanNotice:
		log.Printf("收到通知事件 type=%s group_id=%d user_id=%d operator_id=%d duration=%d", current.Name(), current.GroupID, current.UserID, current.OperatorID, current.Duration)
	case *ws.GroupCardNotice:
		log.Printf("收到通知事件 type=%s group_id=%d user_id=%d card_old=%q card_new=%q", current.Name(), current.GroupID, current.UserID, current.CardOld, current.CardNew)
	case *ws.GroupUploadNotice:
		log.Printf("收到通知事件 type=%s group_id=%d user_id=%d file=%+v", current.Name(), current.GroupID, current.UserID, current.File)
	case *ws.EmojiLikeNotice:
		log.Printf("收到通知事件 type=%s group_id=%d message_id=%d likes=%+v is_add=%t", current.Name(), current.GroupID, current.MessageID, current.Likes, current.IsAdd)
	case *ws.EssenceNotice:
		log.Printf("收到通知事件 type=%s group_id=%d message_id=%d sender_id=%d operator_id=%d", current.Name(), current.GroupID, current.MessageID, current.SenderID, current.OperatorID)
	case *ws.OnlineFileNotice:
		log.Printf("收到通知事件 type=%s peer_id=%d", current.Name(), current.PeerID)
	case *ws.BotOfflineNotice:
		log.Printf("收到通知事件 type=%s user_id=%d tag=%q message=%q", current.Name(), current.UserID, current.Tag, current.Message)
	case *ws.NotifyNotice:
		log.Printf("收到通知事件 type=%s group_id=%d user_id=%d target_id=%d", current.Name(), current.GroupID, current.UserID, current.TargetID)
	case *ws.NoticeEvent:
		log.Printf("收到通知事件 type=%s group_id=%d user_id=%d operator_id=%d", current.Name(), current.GroupID, current.UserID, current.OperatorID)
	default:
		log.Printf("收到未知事件 type=%s self_id=%d", event.Name(), event.Base().SelfID)
	}

	// >>> 数据演变示例
	// 1. *GroupBanNotice -> 类型分支 -> 输出 duration 和 operator_id。
	// 2. *HeartbeatEvent -> 类型分支 -> 输出 interval 和 status。
}
