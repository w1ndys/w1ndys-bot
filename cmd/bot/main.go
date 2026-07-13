// 📌 影响范围：读取进程环境变量和命令行参数；连接 PostgreSQL；监听 TCP 端口；写入标准日志；监听进程信号。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
	"github.com/w1ndys/w1ndys-bot/internal/migration"
	"github.com/w1ndys/w1ndys-bot/internal/onebot"
	"github.com/w1ndys/w1ndys-bot/internal/permission"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
	projectlogger "github.com/w1ndys/w1ndys-bot/pkg/logger"
	_ "github.com/w1ndys/w1ndys-bot/plugins/admin"
	_ "github.com/w1ndys/w1ndys-bot/plugins/ping"
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
	registrations := plugin.Registrations()
	pluginSynchronizer := plugin.NewSynchronizer(pool)
	// [决策理由] 插件定义必须在加载运行状态前与当前二进制 Manifest 保持一致。
	if err := pluginSynchronizer.Sync(ctx, plugin.Manifests()); err != nil {
		projectlogger.Error("同步插件元数据失败", "error", err)
		return
	}
	commands := commandregistry.NewRegistry(pool)
	// [决策理由] 启动时发布完整命令快照，后续消息路由无需逐条查询数据库。
	if err := commands.Load(ctx); err != nil {
		projectlogger.Error("加载命令注册表失败", "error", err)
		return
	}
	permissions := permission.NewResolver(pool)
	// [决策理由] 启动时发布完整权限快照，为后续命令路由提供无数据库查询的判断能力。
	if err := permissions.Load(ctx); err != nil {
		projectlogger.Error("加载权限策略失败", "error", err)
		return
	}
	adminRepository := admin.NewPostgresRepository(pool)
	// [决策理由] WebUI 尚未实现时需要用环境变量完成首位最高管理员的数据库引导。
	if err := adminRepository.BootstrapSystemAdmin(ctx, cfg.SuperAdminQQ); err != nil {
		projectlogger.Error("引导最高管理员失败", "error", err)
		return
	}
	// [决策理由] 空引导值允许纯事件模式启动，但需要明确提示管理命令暂不可用。
	if cfg.SuperAdminQQ == "" {
		projectlogger.Warn("未配置 SUPER_ADMIN_QQ，QQ 与 WebUI 管理操作将无可用最高管理员")
	}
	adminResolver := admin.NewAdminResolver(adminRepository)
	// [决策理由] 管理员身份属于管理入口的授权根，启动时加载失败不能以空权限继续运行。
	if err := adminResolver.Load(ctx); err != nil {
		projectlogger.Error("加载最高管理员失败", "error", err)
		return
	}
	pluginManager := plugin.NewManager(plugin.NewPostgresStore(pool))
	adminService := admin.NewService(adminRepository, pluginManager, adminResolver)
	wsServer := ws.NewServer(cfg.NapCatToken, func(_ context.Context, event ws.Event) error {
		logEvent(event)
		message, isMessage := event.(*ws.MessageEvent)
		// [决策理由] 只有消息事件参与命令匹配，其他事件继续广播给观察型插件。
		if !isMessage {
			return pluginManager.Handle(ctx, event)
		}
		binding, matched := commands.Resolve(strconv.FormatInt(message.GroupID, 10), message.RawMessage, "/")
		// [决策理由] 未匹配命令的消息仍可由观察型插件处理。
		if !matched {
			return pluginManager.Handle(ctx, event)
		}
		defaults, found := featureDefaults(registrations, binding.PluginName, binding.FeatureKey)
		// [决策理由] 命令指向当前二进制不存在的功能时拒绝执行，避免陈旧数据库映射。
		if !found {
			return fmt.Errorf("命令目标 %s 不存在", binding.Target())
		}
		role := messageRole(message)
		// [决策理由] NapCat 群角色不包含系统最高管理员，必须用服务端身份快照提升对应 QQ 权限角色。
		if adminResolver.IsSuperAdmin(strconv.FormatInt(message.UserID, 10)) {
			role = permission.RoleSuperAdmin
		}
		// [决策理由] 权限拒绝时不得调用插件实现。
		if !permissions.Allowed(strconv.FormatInt(message.GroupID, 10), binding.PluginName, binding.FeatureKey, role, defaults) {
			projectlogger.Warn("命令权限不足", "target", binding.Target(), "user_id", message.UserID, "role", role)
			return nil
		}
		routedContext := plugin.WithFeature(ctx, binding.FeatureKey)
		routeErr := pluginManager.HandleNamed(routedContext, binding.PluginName, event)

		// >>> 数据演变示例
		// 1. /ping -> Command Binding -> 权限允许 -> ping.HandleNamed。
		// 2. 未匹配消息 -> PluginManager 广播给观察型插件。
		return routeErr
	})
	botAPI := onebot.New(wsServer.Actions())
	for _, registration := range registrations {
		implementation, err := registration.Factory(plugin.Runtime{Messenger: botAPI, Management: adminService})
		// [决策理由] 工厂失败或返回错误实现时该插件不能进入运行路由。
		if err != nil {
			projectlogger.Error("创建插件运行实例失败", "plugin", registration.Manifest.Name, "error", err)
			return
		}
		// [决策理由] Manager 注册再次校验运行实例名称和重复项。
		if err := pluginManager.Register(implementation); err != nil {
			projectlogger.Error("注册插件运行实例失败", "plugin", registration.Manifest.Name, "error", err)
			return
		}
	}
	// [决策理由] 所有实例注册完成后再应用数据库启用状态和优先级。
	if err := pluginManager.Load(ctx); err != nil {
		projectlogger.Error("加载插件状态失败", "error", err)
		return
	}
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

// featureDefaults 查找功能 Manifest 并转换默认权限。
// @param registrations：插件注册快照；pluginName：插件名；featureKey：功能键。
// @returns 权限默认值及是否找到。
// ⚠️副作用说明：无。
func featureDefaults(registrations []plugin.Registration, pluginName string, featureKey string) (permission.Defaults, bool) {
	for _, registration := range registrations {
		// [决策理由] 仅在目标插件内查找功能，避免不同插件同名 feature_key 混淆。
		if registration.Manifest.Name != pluginName {
			continue
		}
		for _, feature := range registration.Manifest.Features {
			// [决策理由] 找到稳定功能键后立即转换并返回对应默认权限。
			if feature.Key == featureKey {
				value := feature.DefaultPermissions
				return permission.Defaults{SuperAdmin: value.SuperAdmin, GroupOwner: value.GroupOwner, GroupAdmin: value.GroupAdmin, Member: value.Member}, true
			}
		}
	}

	// >>> 数据演变示例
	// 1. ping.ping -> Manifest Feature -> Defaults,true。
	// 2. removed.missing -> 无匹配 -> 零值,false。
	return permission.Defaults{}, false
}

// messageRole 将 NapCat 群角色转换为权限角色。
// @param event：消息事件。
// @returns owner/admin/member 对应权限角色；私聊和未知角色按 member 处理。
// ⚠️副作用说明：无。
func messageRole(event *ws.MessageEvent) permission.Role {
	switch event.Sender.Role {
	case "owner":
		return permission.RoleGroupOwner
	case "admin":
		return permission.RoleGroupAdmin
	default:
		return permission.RoleMember
	}

	// >>> 数据演变示例
	// 1. sender.role=owner -> RoleGroupOwner。
	// 2. private sender.role="" -> RoleMember。
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
