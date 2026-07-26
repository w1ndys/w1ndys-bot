// 📌 影响范围：读取进程环境变量、命令行参数和 web/dist；连接 PostgreSQL；监听 TCP 端口；写入标准日志；监听进程信号。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
	"github.com/w1ndys/w1ndys-bot/internal/migration"
	"github.com/w1ndys/w1ndys-bot/internal/onebot"
	"github.com/w1ndys/w1ndys-bot/internal/permission"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/webapi"
	"github.com/w1ndys/w1ndys-bot/internal/webui"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
	projectlogger "github.com/w1ndys/w1ndys-bot/pkg/logger"
	_ "github.com/w1ndys/w1ndys-bot/plugins/admin"
	"github.com/w1ndys/w1ndys-bot/plugins/echo"
	_ "github.com/w1ndys/w1ndys-bot/plugins/forbidden_message_monitor"
	_ "github.com/w1ndys/w1ndys-bot/plugins/keyword_reply"
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
	logSupportedPlugins(registrations)
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
	// [决策理由] 空配置允许纯事件模式启动，但需要明确提示所有管理入口均不可用。
	if cfg.SuperAdminQQ == "" {
		projectlogger.Warn("未配置 SUPER_ADMIN_QQ，QQ 与 WebUI 管理操作将无可用最高管理员")
	}
	adminResolver := admin.NewAdminResolver(cfg.SuperAdminQQ)
	settingsResolver := admin.NewSettingsResolver(adminRepository)
	// [决策理由] 系统业务设置必须在消息路由启动前完成校验和快照发布。
	if err := settingsResolver.Load(ctx); err != nil {
		projectlogger.Error("加载系统设置失败", "error", err)
		return
	}
	pluginManager := plugin.NewManager(plugin.NewPostgresStore(pool), plugin.NewPostgresGroupGate(pool))
	adminService := admin.NewService(adminRepository, pluginManager, commands, permissions, settingsResolver, adminResolver)
	webServer, err := webapi.New(cfg.WebUIPassword, cfg.JWTSecret, adminResolver, adminService)
	// [决策理由] WebUI 认证配置不安全时不得开放包含管理能力的 HTTP 服务。
	if err != nil {
		projectlogger.Error("初始化WebAPI失败", "error", err)
		return
	}
	// [决策理由] 目标架构入口依赖只能在 botAPI 就绪后构建，但 WS 回调必须更早注册；监听开始前完成赋值，回调不会读到 nil。
	var targetDispatcher *plugin.EventDispatcher
	wsServer := ws.NewServer(cfg.NapCatToken, func(_ context.Context, event ws.Event) error {
		logEvent(event)
		message, isMessage := event.(*ws.MessageEvent)
		// [决策理由] 目标架构插件只接收群消息和群事件，私聊仍由旧链路承担 QQ 管理入口。
		if targetDispatcher != nil && (!isMessage || message.MessageType == "group") {
			result, dispatchErr := targetDispatcher.Dispatch(ctx, event)
			// [决策理由] 命中目标架构命令后不再进入旧链路，避免同一消息被两套体系重复处理。
			if result.CommandMatched {
				return targetCommandError(dispatchErr)
			}
			// [决策理由] 观察器失败不应阻断旧链路对同一事件的处理。
			if dispatchErr != nil {
				projectlogger.Error("目标插件观察链处理事件失败", "error", dispatchErr)
			}
		}
		// [决策理由] 只有消息事件参与命令匹配，其他事件继续广播给观察型插件。
		if !isMessage {
			return pluginManager.Handle(ctx, event)
		}
		binding, matched := commands.Resolve(strconv.FormatInt(message.GroupID, 10), message.RawMessage, settingsResolver.CommandPrefix())
		// [决策理由] 未匹配命令的消息仍可由观察型插件处理。
		if !matched {
			return pluginManager.Handle(ctx, event)
		}
		defaults, found := featureDefaults(registrations, binding.PluginName, binding.FeatureKey)
		// [决策理由] 命令指向当前二进制不存在的功能时拒绝执行，避免陈旧数据库映射。
		if !found {
			return fmt.Errorf("命令目标 %s 不存在", binding.Target())
		}
		allowed, routeErr := pluginManager.RouteAllowed(binding.PluginName, event)
		// [决策理由] 命令在权限解析前统一执行全局状态和群策略预检，保持全局→群→权限→处理顺序。
		if routeErr != nil {
			return routeErr
		}
		// [决策理由] 群策略关闭是安静忽略命令，不进入身份与权限计算。
		if !allowed {
			return nil
		}
		role := messageRole(message)
		// [决策理由] NapCat 群角色不包含系统最高管理员，必须用服务端身份快照提升对应 QQ 权限角色。
		if adminResolver.IsSuperAdmin(strconv.FormatInt(message.UserID, 10)) {
			role = permission.RoleSuperAdmin
		}
		// [决策理由] 权限拒绝时不得调用插件实现。
		if !permissions.Allowed(strconv.FormatInt(message.GroupID, 10), binding.PluginName, binding.FeatureKey, strconv.FormatInt(message.UserID, 10), role, defaults) {
			projectlogger.Warn("命令权限不足", "target", binding.Target(), "user_id", message.UserID, "role", role)
			return nil
		}
		arguments := commandregistry.ExtractArguments(message.RawMessage, settingsResolver.CommandPrefix(), binding.NormalizedCommand)
		routedContext := plugin.WithInvocation(ctx, plugin.Invocation{FeatureKey: binding.FeatureKey, Command: binding.Command, Arguments: arguments})
		handleErr := pluginManager.HandleNamed(routedContext, binding.PluginName, event)

		// >>> 数据演变示例
		// 1. /echo Hello -> Command Binding -> Invocation{echo,Hello} -> echo.HandleNamed。
		// 2. 未匹配消息 -> PluginManager 广播给观察型插件。
		return handleErr
	})
	botAPI := onebot.New(wsServer.Actions())
	echoSpec, err := echo.Spec(botAPI)
	// [决策理由] 规格构建失败表示插件依赖缺失，不能带着不完整目录继续启动。
	if err != nil {
		projectlogger.Error("构建目标插件规格失败", "error", err)
		return
	}
	specCatalog, err := plugin.NewSpecCatalog([]plugin.PluginSpec{echoSpec})
	// [决策理由] 重复 Key 或触发词冲突必须在启动期暴露，而不是运行期产生不确定路由。
	if err != nil {
		projectlogger.Error("构建目标插件目录失败", "error", err)
		return
	}
	logTargetPlugins(specCatalog)
	runtimeController, err := plugin.NewRuntimeController(specCatalog)
	// [决策理由] 缺少运行控制器时命令无法通过 Ready 与群门禁，禁止降级运行。
	if err != nil {
		projectlogger.Error("构建插件运行控制器失败", "error", err)
		return
	}
	runtimeStateRepository, err := plugin.NewPostgresRuntimeStateRepository(pool)
	// [决策理由] 无状态仓库则管理员意图不可持久化，重启后开关全部丢失。
	if err != nil {
		projectlogger.Error("构建插件状态仓库失败", "error", err)
		return
	}
	runtimeBootstrap, err := plugin.NewRuntimeBootstrap(specCatalog, runtimeController, runtimeStateRepository)
	if err != nil {
		projectlogger.Error("构建插件运行恢复服务失败", "error", err)
		return
	}
	// [决策理由] 恢复失败时进程内状态与管理员意图不一致，继续启动会让插件在未知状态下接流量。
	if err := runtimeBootstrap.Initialize(ctx); err != nil {
		projectlogger.Error("恢复插件运行状态失败", "error", err)
		return
	}
	superAdminID, err := parseSuperAdminID(cfg.SuperAdminQQ)
	// [决策理由] 无法解析的最高管理员配置会静默降级授权范围，必须启动期失败。
	if err != nil {
		projectlogger.Error("解析最高管理员 QQ 失败", "error", err)
		return
	}
	identityResolver, err := plugin.NewCodeIdentityResolver(superAdminID)
	if err != nil {
		projectlogger.Error("构建代码身份解析器失败", "error", err)
		return
	}
	commandDispatcher, err := plugin.NewDispatcher(specCatalog, runtimeController, identityResolver)
	if err != nil {
		projectlogger.Error("构建目标插件命令分发器失败", "error", err)
		return
	}
	observerDispatcher, err := plugin.NewObserverDispatcher(specCatalog, runtimeController)
	if err != nil {
		projectlogger.Error("构建目标插件观察分发器失败", "error", err)
		return
	}
	targetDispatcher, err = plugin.NewEventDispatcher(commandDispatcher, observerDispatcher)
	if err != nil {
		projectlogger.Error("构建目标插件事件入口失败", "error", err)
		return
	}
	for _, registration := range registrations {
		implementation, err := registration.New(plugin.Runtime{Messenger: botAPI, Actions: botAPI, Management: adminService, Database: pool})
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
	rootMux := http.NewServeMux()
	webUIHandler, err := webui.New("web/dist")
	// [决策理由] 生产镜像缺少 WebUI 入口文件表示构建不完整，应在开放管理端口前终止。
	if err != nil {
		projectlogger.Error("初始化WebUI静态资源失败", "error", err)
		return
	}
	rootMux.Handle("/onebot/v11/ws", wsServer.Handler())
	apiHandler := http.TimeoutHandler(webServer.Handler(), 30*time.Second, `{"code":"request_timeout","message":"请求处理超时","data":null}`)
	rootMux.Handle("/api", apiHandler)
	rootMux.Handle("/api/", apiHandler)
	rootMux.Handle("/", webUIHandler)
	httpServer := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", cfg.WSPort),
		Handler:           rootMux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
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

	projectlogger.Info("基础框架已启动", "http_port", cfg.WSPort, "webui", "/", "webapi", "/api", "onebot_ws", "/onebot/v11/ws", "log_level", cfg.LogLevel, "log_format", cfg.LogFormat)
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

// targetCommandError 过滤目标插件命令链路中属于正常拒绝的门禁结果。
// @param err：EventDispatcher 命中命令后返回的错误。
// @returns 需要上报的错误；运行门禁与授权拒绝返回 nil。
// ⚠️副作用说明：向标准日志写入门禁拒绝记录。
func targetCommandError(err error) error {
	switch {
	case err == nil:
		return nil
	// [决策理由] 插件未启用或未在本群开启是预期状态，不应作为错误噪音上报。
	case errors.Is(err, plugin.ErrPluginNotReady), errors.Is(err, plugin.ErrPluginGroupDisabled):
		projectlogger.Debug("目标插件命令被运行门禁拒绝", "error", err)
		return nil
	// [决策理由] 身份不足属于日常拒绝，记录后安静结束，与旧链路行为一致。
	case errors.Is(err, plugin.ErrCommandUnauthorized):
		projectlogger.Warn("目标插件命令身份不足", "error", err)
		return nil
	default:
		return err
	}

	// >>> 数据演变示例
	// 1. ErrPluginGroupDisabled -> Debug 日志 -> nil。
	// 2. Handler 返回发送失败 -> 原样返回 -> WS 层记录错误。
}

// parseSuperAdminID 将最高管理员配置解析为代码身份解析器使用的 QQ 号。
// @param configured：SUPER_ADMIN_QQ 配置值，允许为空。
// @returns 正数 QQ 号；未配置时返回 0，非法值返回错误。
// ⚠️副作用说明：无。
func parseSuperAdminID(configured string) (int64, error) {
	// [决策理由] 未配置最高管理员时目标插件仍可按群身份运行，不应阻断启动。
	if configured == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(configured, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("SUPER_ADMIN_QQ %q 无效", configured)
	}

	// >>> 数据演变示例
	// 1. "10001" -> 10001,nil。
	// 2. "abc" -> 0,错误 -> 启动终止。
	return value, nil
}

// logTargetPlugins 输出目标插件架构目录中的插件、命令与允许身份。
// @param catalog：已完成冲突校验的规格目录。
// @returns 无。
// ⚠️副作用说明：向标准日志写入目标插件元数据。
func logTargetPlugins(catalog *plugin.SpecCatalog) {
	specs := catalog.Specs()
	projectlogger.Info("已装载目标架构插件", "plugin_count", len(specs))
	for _, spec := range specs {
		commands := make([]map[string]any, 0, len(spec.Commands))
		for _, command := range spec.Commands {
			roles := make([]string, 0, len(command.AllowedRoles))
			for role := range command.AllowedRoles {
				roles = append(roles, string(role))
			}
			sort.Strings(roles)
			commands = append(commands, map[string]any{
				"key": command.Key, "display_name": command.DisplayName,
				"triggers": command.Triggers, "scope": string(command.Scope), "allowed_roles": roles,
			})
		}
		projectlogger.Info("目标架构插件",
			"plugin", spec.Key,
			"display_name", spec.DisplayName,
			"command_count", len(commands),
			"commands", commands,
			"observer_count", len(spec.Observers),
		)
	}

	// >>> 数据演变示例
	// 1. [echo{1命令}] -> plugin_count=1 -> 输出触发词与允许身份。
	// 2. 空目录 -> plugin_count=0 -> 不输出插件明细。
}

// logSupportedPlugins 输出当前二进制编译包含的插件及功能元数据。
// @param registrations：按插件名排序的编译时插件注册快照。
// @returns 无。
// ⚠️副作用说明：向标准日志写入插件名称、优先级、系统标记、描述和功能元数据。
func logSupportedPlugins(registrations []plugin.Registration) {
	projectlogger.Info("已发现当前支持的插件", "plugin_count", len(registrations))
	for _, registration := range registrations {
		manifest := registration.Manifest
		features := make([]map[string]any, 0, len(manifest.Features))
		for _, feature := range manifest.Features {
			features = append(features, map[string]any{
				"key":                 feature.Key,
				"display_name":        feature.DisplayName,
				"description":         feature.Description,
				"default_commands":    feature.DefaultCommands,
				"default_permissions": feature.DefaultPermissions,
			})
		}
		projectlogger.Info("当前支持插件",
			"plugin", manifest.Name,
			"display_name", manifest.DisplayName,
			"description", manifest.Description,
			"priority", manifest.Priority,
			"system", manifest.System,
			"feature_count", len(features),
			"features", features,
		)
	}

	// >>> 数据演变示例
	// 1. [admin{3项功能},echo{1项功能}] -> 汇总 plugin_count=2 -> 分别输出两条完整插件日志。
	// 2. [] -> 汇总 plugin_count=0 -> 不输出插件明细。
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
	// 1. echo.echo -> Manifest Feature -> Defaults,true。
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
