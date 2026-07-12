// 📌 影响范围：读取进程环境变量和命令行参数；连接 PostgreSQL；监听 TCP 端口；写入标准日志；监听进程信号。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
	"github.com/w1ndys/w1ndys-bot/internal/migration"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
	projectlogger "github.com/w1ndys/w1ndys-bot/pkg/logger"
)

// main 启动机器人基础设施。
// @param 无。
// @returns 无。
// ⚠️副作用说明：读取运行参数、创建数据库连接、注册信号监听并输出日志；启动失败时终止进程。
func main() {
	cfg, err := config.Load()
	// [决策理由] 配置不完整时继续启动会产生含糊的连接错误，因此立即终止。
	if err != nil {
		projectlogger.Error("加载配置失败", "error", err)
		return
	}
	logger, err := projectlogger.New(cfg.LogLevel, cfg.LogFormat)
	// [决策理由] 日志配置无效时继续运行会导致日志格式或过滤规则不可预测。
	if err != nil {
		projectlogger.Error("初始化日志器失败", "error", err)
		return
	}
	projectlogger.SetDefault(logger)
	defer logger.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.Database)
	// [决策理由] 数据库是基础依赖，连接不可用时服务不具备可运行条件。
	if err != nil {
		projectlogger.Error("连接数据库失败", "error", err)
		return
	}
	defer pool.Close()
	migrationRunner, err := migration.New(cfg.Database)
	// [决策理由] 迁移执行器无法初始化时不能保证插件依赖的表结构存在。
	if err != nil {
		projectlogger.Error("初始化数据库迁移失败", "error", err)
		return
	}
	defer migrationRunner.Close()
	// [决策理由] 启动前完成迁移，确保后续 Store 查询面对最新 schema。
	if err := migrationRunner.Up(); err != nil {
		projectlogger.Error("执行数据库迁移失败", "error", err)
		return
	}
	pluginSynchronizer := plugin.NewSynchronizer(pool)
	// [决策理由] 插件定义必须在加载运行状态前与当前二进制 Manifest 保持一致。
	if err := pluginSynchronizer.Sync(ctx, plugin.Manifests()); err != nil {
		projectlogger.Error("同步插件元数据失败", "error", err)
		return
	}
	pluginManager := plugin.NewManager(plugin.NewPostgresStore(pool))
	// [决策理由] 插件状态表尚由后续迁移阶段创建；当前未注册插件时不查询，避免阻断基础链路。
	if err := pluginManager.Load(ctx); err != nil {
		projectlogger.Error("加载插件状态失败", "error", err)
		return
	}

	wsServer := ws.NewServer(cfg.NapCatToken, func(_ context.Context, event ws.Event) error {
		logEvent(event)
		// [决策理由] 分类日志完成后统一进入 PluginManager，确保所有事件类别共享相同路由规则。
		if err := pluginManager.Handle(ctx, event); err != nil {
			return err
		}

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
			projectlogger.Error("WebSocket 服务异常退出", "error", err)
			stop()
		}

		// >>> 数据演变示例
		// 1. 监听成功 -> 持续接收连接 -> Shutdown -> ErrServerClosed -> 静默结束。
		// 2. 端口被占用 -> ListenAndServe 错误 -> 记录日志 -> 通知主流程退出。
	}()

	projectlogger.Info("基础框架已启动", "ws_port", cfg.WSPort, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat)
	<-ctx.Done()
	// [决策理由] 收到退出信号后停止接受新连接，并等待活跃请求结束。
	if err := httpServer.Shutdown(context.Background()); err != nil {
		projectlogger.Error("关闭 WebSocket 服务失败", "error", err)
	}
	projectlogger.Info("基础框架正在关闭")

	// >>> 数据演变示例
	// 1. 有效环境变量 + 可连接数据库 -> Config -> pgxpool -> WS 服务 -> 等待退出信号 -> 正常关闭。
	// 2. 缺少 DB_PASSWORD -> 配置校验错误 -> 输出错误日志 -> 进程终止。
}

// logEvent 按强类型事件输出其专属关键字段。
// @param event：已解析的 OneBot 事件。
// @returns 无。
// ⚠️副作用说明：向标准日志写入一条事件记录。
func logEvent(event ws.Event) {
	logger := projectlogger.With("event_type", event.Name(), "self_id", event.Base().SelfID)
	switch current := event.(type) {
	case *ws.MessageEvent:
		logger.Info("收到消息事件", "group_id", current.GroupID, "user_id", current.UserID, "message_id", current.MessageID, "raw_message", current.RawMessage)
	case *ws.HeartbeatEvent:
		logger.Debug("收到心跳事件", "interval", current.Interval, "status", current.Status.String())
	case *ws.LifecycleEvent:
		logger.Info("收到生命周期事件")
	case *ws.FriendRequestEvent:
		logger.Info("收到好友请求事件", "user_id", current.UserID, "comment", current.Comment, "flag", current.Flag)
	case *ws.GroupRequestEvent:
		logger.Info("收到群请求事件", "group_id", current.GroupID, "user_id", current.UserID, "comment", current.Comment, "flag", current.Flag)
	case *ws.GroupBanNotice:
		logger.Info("收到群禁言通知", "group_id", current.GroupID, "user_id", current.UserID, "operator_id", current.OperatorID, "duration", current.Duration)
	case *ws.GroupCardNotice:
		logger.Info("收到群名片通知", "group_id", current.GroupID, "user_id", current.UserID, "card_old", current.CardOld, "card_new", current.CardNew)
	case *ws.GroupUploadNotice:
		logger.Info("收到群文件通知", "group_id", current.GroupID, "user_id", current.UserID, "file", current.File)
	case *ws.EmojiLikeNotice:
		logger.Info("收到表情回应通知", "group_id", current.GroupID, "message_id", current.MessageID, "likes", current.Likes, "is_add", current.IsAdd)
	case *ws.EssenceNotice:
		logger.Info("收到精华消息通知", "group_id", current.GroupID, "message_id", current.MessageID, "sender_id", current.SenderID, "operator_id", current.OperatorID)
	case *ws.OnlineFileNotice:
		logger.Info("收到在线文件通知", "peer_id", current.PeerID)
	case *ws.BotOfflineNotice:
		logger.Warn("收到机器人离线通知", "user_id", current.UserID, "tag", current.Tag, "message", current.Message)
	case *ws.NotifyNotice:
		logger.Info("收到扩展通知事件", "group_id", current.GroupID, "user_id", current.UserID, "target_id", current.TargetID)
	case *ws.NoticeEvent:
		logger.Info("收到通知事件", "group_id", current.GroupID, "user_id", current.UserID, "operator_id", current.OperatorID)
	default:
		logger.Warn("收到未知事件")
	}

	// >>> 数据演变示例
	// 1. *GroupBanNotice -> 类型分支 -> 输出 duration 和 operator_id。
	// 2. *HeartbeatEvent -> 类型分支 -> 输出 interval 和 status。
}
