// 📌 影响范围：读取系统时间和 crypto/rand；校验管理员快照；签发及验证 HMAC-SHA256 JWT；写入 HTTP 响应与访问日志。
package webapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	projectauth "github.com/w1ndys/w1ndys-bot/internal/auth"
	"github.com/w1ndys/w1ndys-bot/internal/management"
	projectlogger "github.com/w1ndys/w1ndys-bot/pkg/logger"
)

const tokenLifetime = 12 * time.Hour

// ApplicationName 是 WebUI 固定展示名称，不开放运行时修改。
const ApplicationName = "w1ndys-bot-webui"

type contextKey string

const actorContextKey contextKey = "webui_actor"
const requestIDContextKey contextKey = "request_id"
const managementActorContextKey contextKey = "management_actor"

// AdminResolver 定义 WebUI 登录及请求期间所需的管理员快照能力。
type AdminResolver interface {
	Resolve(string) (admin.SystemAdmin, bool)
}

// ManagementController 定义 WebUI 当前开放的插件、触发词与权限管理能力。
type ManagementController interface {
	ListPlugins(context.Context, management.Actor) ([]management.PluginState, error)
	SetPluginEnabled(context.Context, management.Actor, string, bool) (management.PluginState, error)
	SetPluginPriority(context.Context, management.Actor, string, int) (management.PluginState, error)
	ListCommands(context.Context, management.Actor) ([]management.CommandState, error)
	CreateCommand(context.Context, management.Actor, management.CommandCreate) (management.CommandState, error)
	RenameCommand(context.Context, management.Actor, int64, string) (management.CommandState, error)
	DeleteCommand(context.Context, management.Actor, int64) error
	ListPermissions(context.Context, management.Actor) ([]management.PermissionState, error)
	SetPermission(context.Context, management.Actor, management.PermissionSet) (management.PermissionState, error)
	DeletePermission(context.Context, management.Actor, int64) error
	ListSettings(context.Context, management.Actor) ([]management.SettingState, error)
	SetSetting(context.Context, management.Actor, string, json.RawMessage) (management.SettingState, error)
	DeleteSetting(context.Context, management.Actor, string) error
	ListAuditLogs(context.Context, management.Actor, management.AuditQuery) (management.AuditPage, error)
	GetAuditLog(context.Context, management.Actor, int64) (management.AuditState, error)
}

// Server 提供 WebUI 认证 HTTP 接口。
type Server struct {
	passwordHash string
	jwtSecret    []byte
	admins       AdminResolver
	management   ManagementController
	now          func() time.Time
}

type loginRequest struct {
	QQ       string `json:"qq"`
	Password string `json:"password"`
}

type tokenClaims struct {
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

type responseEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type pluginPatchRequest struct {
	Enabled  *bool `json:"enabled"`
	Priority *int  `json:"priority"`
}

type pluginResponse struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	Available   bool            `json:"available"`
	Enabled     bool            `json:"enabled"`
	Priority    int             `json:"priority"`
	Config      json.RawMessage `json:"config"`
}

type commandCreateRequest struct {
	ScopeType  string `json:"scope_type"`
	ScopeID    string `json:"scope_id"`
	PluginName string `json:"plugin_name"`
	FeatureKey string `json:"feature_key"`
	Command    string `json:"command"`
}

type commandPatchRequest struct {
	Command string `json:"command"`
}

type commandResponse struct {
	ID                int64  `json:"id"`
	ScopeType         string `json:"scope_type"`
	ScopeID           string `json:"scope_id"`
	PluginName        string `json:"plugin_name"`
	FeatureKey        string `json:"feature_key"`
	Command           string `json:"command"`
	NormalizedCommand string `json:"normalized_command"`
	Enabled           bool   `json:"enabled"`
}

type permissionSetRequest struct {
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
	PluginName  string `json:"plugin_name"`
	FeatureKey  string `json:"feature_key"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Effect      string `json:"effect"`
}

type permissionResponse struct {
	ID          int64  `json:"id"`
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
	PluginName  string `json:"plugin_name"`
	FeatureKey  string `json:"feature_key"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Effect      string `json:"effect"`
}

type settingSetRequest struct {
	Value json.RawMessage `json:"value"`
}

type settingResponse struct {
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
	Overridden  bool            `json:"overridden"`
}

type auditResponse struct {
	ID           int64           `json:"id"`
	ActorID      string          `json:"actor_id"`
	ActorRole    string          `json:"actor_role"`
	Channel      string          `json:"channel"`
	Action       string          `json:"action"`
	TargetType   string          `json:"target_type"`
	TargetID     string          `json:"target_id"`
	Before       json.RawMessage `json:"before"`
	After        json.RawMessage `json:"after"`
	Success      bool            `json:"success"`
	ErrorMessage string          `json:"error_message"`
	RequestID    string          `json:"request_id"`
	CreatedAt    time.Time       `json:"created_at"`
}

type auditPageResponse struct {
	Items    []auditResponse `json:"items"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Total    int64           `json:"total"`
}

// New 创建 WebUI API 服务并在内存中准备环境密码哈希。
// @param password：环境变量中的共享密码；jwtSecret：JWT 签名密钥；admins：管理员快照解析器；controller：管理业务服务。
// @returns 可注册到 HTTP 路由的 Server，或配置强度错误。
// ⚠️副作用说明：读取系统加密随机源并执行 Argon2id 哈希。
func New(password string, jwtSecret string, admins AdminResolver, controller ManagementController) (*Server, error) {
	// [决策理由] JWT 密钥过短会降低离线伪造成本，必须在监听端口前拒绝启动。
	if len([]byte(jwtSecret)) < 32 {
		return nil, errors.New("JWT_SECRET 不能少于32字节")
	}
	// [决策理由] 管理员解析器缺失时无法完成服务端授权确认。
	if admins == nil {
		return nil, errors.New("管理员解析器不能为空")
	}
	// [决策理由] 插件控制器缺失时管理路由会在鉴权后崩溃，必须在组装阶段拒绝。
	if controller == nil {
		return nil, errors.New("管理服务不能为空")
	}
	passwordHash, err := projectauth.HashPassword(password)
	// [决策理由] 环境密码不符合强度要求或随机源失败时禁止开放登录接口。
	if err != nil {
		return nil, fmt.Errorf("准备 WebUI 密码: %w", err)
	}
	server := &Server{passwordHash: passwordHash, jwtSecret: []byte(jwtSecret), admins: admins, management: controller, now: time.Now}

	// >>> 数据演变示例
	// 1. 强密码+32字节密钥+Resolver -> Argon2id哈希 -> Server,nil。
	// 2. 短JWT密钥 -> 强度检查 -> nil,error。
	return server, nil
}

// Handler 返回包含登录、当前身份和通用中间件的 HTTP 路由。
// @param 无。
// @returns 可挂载在 /api/ 路径下的 http.Handler。
// ⚠️副作用说明：创建内存路由；请求到达后会写响应和访问日志。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.Handle("GET /api/auth/me", s.authenticate(http.HandlerFunc(s.me)))
	mux.Handle("GET /api/plugins", s.authenticate(http.HandlerFunc(s.listPlugins)))
	mux.Handle("PATCH /api/plugins/{plugin_name}", s.authenticate(http.HandlerFunc(s.patchPlugin)))
	mux.Handle("GET /api/commands", s.authenticate(http.HandlerFunc(s.listCommands)))
	mux.Handle("POST /api/commands", s.authenticate(http.HandlerFunc(s.createCommand)))
	mux.Handle("PATCH /api/commands/{command_id}", s.authenticate(http.HandlerFunc(s.renameCommand)))
	mux.Handle("DELETE /api/commands/{command_id}", s.authenticate(http.HandlerFunc(s.deleteCommand)))
	mux.Handle("GET /api/permissions", s.authenticate(http.HandlerFunc(s.listPermissions)))
	mux.Handle("POST /api/permissions", s.authenticate(http.HandlerFunc(s.setPermission)))
	mux.Handle("DELETE /api/permissions/{permission_id}", s.authenticate(http.HandlerFunc(s.deletePermission)))
	mux.Handle("GET /api/settings", s.authenticate(http.HandlerFunc(s.listSettings)))
	mux.Handle("PUT /api/settings/{setting_key}", s.authenticate(http.HandlerFunc(s.setSetting)))
	mux.Handle("DELETE /api/settings/{setting_key}", s.authenticate(http.HandlerFunc(s.deleteSetting)))
	mux.Handle("GET /api/audit-logs", s.authenticate(http.HandlerFunc(s.listAuditLogs)))
	mux.Handle("GET /api/audit-logs/{audit_id}", s.authenticate(http.HandlerFunc(s.getAuditLog)))
	handler := s.middleware(mux)

	// >>> 数据演变示例
	// 1. POST /api/auth/login -> login处理器。
	// 2. GET /api/auth/me无Token -> authenticate -> 401。
	return handler
}

// login 校验管理员 QQ 与共享密码并签发 JWT。
// @param writer：响应写入器；request：JSON 登录请求。
// @returns 无。
// ⚠️副作用说明：读取请求体、执行 Argon2id 校验并写入 JSON 响应。
func (s *Server) login(writer http.ResponseWriter, request *http.Request) {
	var input loginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] 登录载荷必须是单个、字段明确的 JSON 对象，避免歧义输入。
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "登录参数格式无效")
		return
	}
	account, exists := s.admins.Resolve(input.QQ)
	matched, verifyErr := projectauth.VerifyPassword(input.Password, s.passwordHash)
	// [决策理由] QQ 与密码使用同一模糊错误，避免暴露管理员账号是否存在。
	if verifyErr != nil || !matched || !exists {
		writeError(writer, http.StatusUnauthorized, "invalid_credentials", "QQ号或密码错误")
		return
	}
	token, err := s.sign(input.QQ)
	// [决策理由] 签名失败表示服务器无法建立可信会话，不能返回部分登录结果。
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal_error", "签发登录凭证失败")
		return
	}
	writeSuccess(writer, map[string]any{"token": token, "expires_in": int64(tokenLifetime.Seconds()), "admin": account})

	// >>> 数据演变示例
	// 1. 启用管理员+正确密码 -> JWT -> 200及管理员信息。
	// 2. 非管理员+正确密码 -> 固定认证失败响应 -> 401。
}

// me 返回 JWT 对应的当前启用管理员身份。
// @param writer：响应写入器；request：已通过鉴权且携带 Actor 的请求。
// @returns 无。
// ⚠️副作用说明：读取请求上下文并写入 JSON 响应。
func (s *Server) me(writer http.ResponseWriter, request *http.Request) {
	actor, exists := request.Context().Value(actorContextKey).(admin.SystemAdmin)
	// [决策理由] 鉴权中间件未注入身份表示路由装配错误，按未授权安全失败。
	if !exists {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "登录凭证无效")
		return
	}
	writeSuccess(writer, actor)

	// >>> 数据演变示例
	// 1. context含管理员100 -> 200 data{user_id:100}。
	// 2. context无身份 -> 401 unauthorized。
}

// listPlugins 返回当前二进制插件元数据和运行配置。
// @param writer：响应写入器；request：已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 插件配置并写入 JSON 响应。
func (s *Server) listPlugins(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	states, err := s.management.ListPlugins(request.Context(), actor)
	// [决策理由] 业务错误必须映射成稳定 API 响应，不能暴露数据库细节。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	result := make([]pluginResponse, 0, len(states))
	for _, state := range states {
		result = append(result, pluginView(state))
	}
	writeSuccess(writer, result)

	// >>> 数据演变示例
	// 1. Service[ping,admin] -> DTO转换 -> 200插件数组。
	// 2. Repository失败 -> management error -> 500统一错误。
}

// patchPlugin 修改一个插件的启用状态或优先级。
// @param writer：响应写入器；request：携带插件名和单字段 JSON Patch 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：可能更新 PostgreSQL、写入审计并热刷新 PluginManager。
func (s *Server) patchPlugin(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	name := request.PathValue("plugin_name")
	var input pluginPatchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] 插件变更只接受字段明确且尺寸受限的 JSON 对象。
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "插件修改参数格式无效")
		return
	}
	// [决策理由] 每次仅允许修改一个字段，避免两个独立业务事务产生半成功状态。
	if (input.Enabled == nil) == (input.Priority == nil) {
		writeError(writer, http.StatusBadRequest, "invalid_request", "必须且只能修改 enabled 或 priority")
		return
	}
	var state management.PluginState
	var err error
	// [决策理由] 非空 enabled 表示显式启停操作，应走受保护插件校验链路。
	if input.Enabled != nil {
		state, err = s.management.SetPluginEnabled(request.Context(), actor, name, *input.Enabled)
	} else {
		// [决策理由] 优先级限制在可读范围，避免极端整数影响排序和前端输入。
		if *input.Priority < -10000 || *input.Priority > 10000 {
			writeError(writer, http.StatusBadRequest, "invalid_request", "priority 必须在 -10000 至 10000 之间")
			return
		}
		state, err = s.management.SetPluginPriority(request.Context(), actor, name, *input.Priority)
	}
	// [决策理由] 更新或刷新失败时按领域错误返回，不能伪造成功状态。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, pluginView(state))

	// >>> 数据演变示例
	// 1. ping+enabled=true -> 事务审计+Load -> 200 ping启用状态。
	// 2. enabled与priority同时出现 -> 参数冲突 -> 400且不写数据库。
}

// listCommands 返回全部全局及群级功能触发词。
// @param writer：响应写入器；request：已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 命令配置并写入 JSON 响应。
func (s *Server) listCommands(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	states, err := s.management.ListCommands(request.Context(), actor)
	// [决策理由] 管理服务错误必须通过统一领域映射返回。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	result := make([]commandResponse, 0, len(states))
	for _, state := range states {
		result = append(result, commandView(state))
	}
	writeSuccess(writer, result)

	// >>> 数据演变示例
	// 1. Service[全局ping,群级测试] -> DTO数组 -> 200。
	// 2. Repository失败 -> 500统一错误且不返回部分列表。
}

// createCommand 新增一条功能触发词。
// @param writer：响应写入器；request：已鉴权 JSON 请求。
// @returns 无。
// ⚠️副作用说明：可能插入命令与审计记录并热刷新命令注册表。
func (s *Server) createCommand(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	var input commandCreateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] 未知字段或非法 JSON 可能表示前后端版本不一致，必须明确拒绝。
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "命令新增参数格式无效")
		return
	}
	state, err := s.management.CreateCommand(request.Context(), actor, management.CommandCreate{ScopeType: input.ScopeType, ScopeID: input.ScopeID, PluginName: input.PluginName, FeatureKey: input.FeatureKey, Command: input.Command})
	// [决策理由] 重复、字段校验和数据库错误需要映射为不同 HTTP 语义。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, commandView(state))

	// >>> 数据演变示例
	// 1. global,0,ping.ping,“测试” -> 标准化+审计+热刷新 -> 200新命令。
	// 2. 同作用域重复“测试” -> ErrCommandConflict -> 409。
}

// renameCommand 修改指定命令的显示及匹配文本。
// @param writer：响应写入器；request：携带命令 ID 和新文本的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：可能更新命令与审计记录并热刷新命令注册表。
func (s *Server) renameCommand(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	id, err := parsePositiveID(request.PathValue("command_id"))
	// [决策理由] 非正整数路径不可能对应数据库主键，应作为请求错误而非查询未找到。
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "命令 ID 格式无效")
		return
	}
	var input commandPatchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] 改名接口只接受 command 字段，避免产生未实现的部分更新语义。
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "命令修改参数格式无效")
		return
	}
	state, err := s.management.RenameCommand(request.Context(), actor, id, input.Command)
	// [决策理由] 领域错误应保持稳定业务码供前端定位字段或刷新列表。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, commandView(state))

	// >>> 数据演变示例
	// 1. id=1+“延迟” -> Normalize+UPDATE+审计+Load -> 200。
	// 2. id=abc -> 路径校验 -> 400零数据库访问。
}

// deleteCommand 删除指定功能触发词。
// @param writer：响应写入器；request：携带命令 ID 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：可能删除命令、写入审计并热刷新命令注册表。
func (s *Server) deleteCommand(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	id, err := parsePositiveID(request.PathValue("command_id"))
	// [决策理由] 非正整数 ID 应在进入事务前拒绝。
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "命令 ID 格式无效")
		return
	}
	// [决策理由] 删除失败时必须保留领域错误，不能返回虚假成功。
	if err := s.management.DeleteCommand(request.Context(), actor, id); err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, nil)

	// >>> 数据演变示例
	// 1. id=1存在 -> DELETE+审计+Load -> 200 data:null。
	// 2. id=404不存在 -> ErrCommandNotFound -> 404。
}

// commandView 将内部命令状态转换成稳定 API 模型。
// @param state：管理服务命令状态。
// @returns snake_case 命令响应。
// ⚠️副作用说明：无。
func commandView(state management.CommandState) commandResponse {
	view := commandResponse{ID: state.ID, ScopeType: state.ScopeType, ScopeID: state.ScopeID, PluginName: state.PluginName, FeatureKey: state.FeatureKey, Command: state.Command, NormalizedCommand: state.NormalizedCommand, Enabled: state.Enabled}

	// >>> 数据演变示例
	// 1. CommandState{ID:1,Command:Ping} -> commandResponse{id:1,command:Ping}。
	// 2. 群级命令scope_id=123 -> DTO保留作用域字段。
	return view
}

// parsePositiveID 解析 REST 路径中的正整数主键。
// @param value：路径参数原文。
// @returns 正 int64 或格式错误。
// ⚠️副作用说明：无。
func parsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	// [决策理由] 数据库 BIGSERIAL 主键只能是正整数。
	if err != nil || id <= 0 {
		return 0, errors.New("ID必须是正整数")
	}

	// >>> 数据演变示例
	// 1. "42" -> ParseInt -> 42,nil。
	// 2. "abc"或"0" -> 校验失败 -> 0,error。
	return id, nil
}

// listPermissions 返回全部显式权限覆盖策略。
// @param writer：响应写入器；request：已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 权限策略并写入 JSON 响应。
func (s *Server) listPermissions(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	states, err := s.management.ListPermissions(request.Context(), actor)
	// [决策理由] 权限列表必须完整返回，查询失败时不能展示部分策略。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	result := make([]permissionResponse, 0, len(states))
	for _, state := range states {
		result = append(result, permissionView(state))
	}
	writeSuccess(writer, result)

	// >>> 数据演变示例
	// 1. Service[群用户allow,全局角色deny] -> DTO数组 -> 200。
	// 2. Repository失败 -> 500统一错误且不泄露部分数据。
}

// setPermission 新增权限策略或更新同一唯一维度的效果。
// @param writer：响应写入器；request：已鉴权 JSON 请求。
// @returns 无。
// ⚠️副作用说明：可能新增或更新权限、写入审计并热刷新 Permission Resolver。
func (s *Server) setPermission(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	var input permissionSetRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] 权限维度必须来源于结构明确且尺寸受限的 JSON 请求。
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "权限策略参数格式无效")
		return
	}
	state, err := s.management.SetPermission(request.Context(), actor, management.PermissionSet{ScopeType: input.ScopeType, ScopeID: input.ScopeID, PluginName: input.PluginName, FeatureKey: input.FeatureKey, SubjectType: input.SubjectType, SubjectID: input.SubjectID, Effect: input.Effect})
	// [决策理由] 字段、目标或持久化错误必须映射为稳定业务响应。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, permissionView(state))

	// >>> 数据演变示例
	// 1. group123+ping插件+user200+allow -> UPSERT+审计+热刷新 -> 200。
	// 2. subject_type=user+subject_id=abc -> invalid_permission -> 400。
}

// deletePermission 删除显式权限策略并恢复后续回退链。
// @param writer：响应写入器；request：携带权限 ID 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：可能删除权限、写入审计并热刷新 Permission Resolver。
func (s *Server) deletePermission(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	id, err := parsePositiveID(request.PathValue("permission_id"))
	// [决策理由] 非正整数 ID 无法定位权限主键，应在事务前拒绝。
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "权限策略 ID 格式无效")
		return
	}
	// [决策理由] 删除失败时不能返回虚假成功，否则前端会误判权限已回退。
	if err := s.management.DeletePermission(request.Context(), actor, id); err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, nil)

	// >>> 数据演变示例
	// 1. id=8存在 -> DELETE+审计+Resolver.Load -> 200 data:null。
	// 2. id=404不存在 -> permission_not_found -> 404。
}

// permissionView 将内部权限状态转换成稳定 API 模型。
// @param state：管理服务权限状态。
// @returns snake_case 权限响应。
// ⚠️副作用说明：无。
func permissionView(state management.PermissionState) permissionResponse {
	view := permissionResponse{ID: state.ID, ScopeType: state.ScopeType, ScopeID: state.ScopeID, PluginName: state.PluginName, FeatureKey: state.FeatureKey, SubjectType: state.SubjectType, SubjectID: state.SubjectID, Effect: state.Effect}

	// >>> 数据演变示例
	// 1. user200+allow -> DTO保留主体与效果字段。
	// 2. FeatureKey空 -> DTO空字符串，表示插件全部功能。
	return view
}

// listSettings 返回全部受控系统设置及当前有效值。
// @param writer：响应写入器；request：已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 设置覆盖并写入 JSON 响应。
func (s *Server) listSettings(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	states, err := s.management.ListSettings(request.Context(), actor)
	// [决策理由] 设置列表需要合并完整默认值，查询失败时不能返回部分结果。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	result := make([]settingResponse, 0, len(states))
	for _, state := range states {
		result = append(result, settingView(state))
	}
	writeSuccess(writer, result)

	// >>> 数据演变示例
	// 1. DB覆盖prefix=!+其余默认 -> 合并DTO数组 -> 200。
	// 2. Repository失败 -> 500统一错误且不返回部分设置。
}

// setSetting 保存一个已注册系统设置的 JSON 值。
// @param writer：响应写入器；request：携带设置键与 value 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：可能 UPSERT 设置、写入审计并热刷新 SettingsResolver。
func (s *Server) setSetting(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	key := request.PathValue("setting_key")
	var input settingSetRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] value 必须保持原始 JSON 类型，未知包装字段应明确拒绝。
	if err := decoder.Decode(&input); err != nil || len(input.Value) == 0 {
		writeError(writer, http.StatusBadRequest, "invalid_request", "系统设置参数格式无效")
		return
	}
	state, err := s.management.SetSetting(request.Context(), actor, key, input.Value)
	// [决策理由] 未知键、类型范围和持久化错误需要稳定区分。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, settingView(state))

	// >>> 数据演变示例
	// 1. command_prefix+value="!" -> 校验+UPSERT+审计+热刷新 -> 200。
	// 2. default_page_size+value=500 -> invalid_setting -> 400。
}

// deleteSetting 删除数据库设置覆盖并回退定义默认值。
// @param writer：响应写入器；request：携带设置键的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：可能删除设置覆盖、写入审计并热刷新 SettingsResolver。
func (s *Server) deleteSetting(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	key := request.PathValue("setting_key")
	// [决策理由] 删除失败时不能声称已恢复默认值。
	if err := s.management.DeleteSetting(request.Context(), actor, key); err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, nil)

	// >>> 数据演变示例
	// 1. command_prefix存在覆盖 -> DELETE+审计+Load -> 200 data:null并回退/。
	// 2. 未覆盖设置 -> setting_override_not_found -> 404。
}

// settingView 将内部设置状态转换成稳定 API 模型。
// @param state：管理服务设置状态。
// @returns 包含当前值和是否覆盖的 snake_case 设置响应。
// ⚠️副作用说明：复制 JSON 值，避免响应持有共享切片。
func settingView(state management.SettingState) settingResponse {
	value := append(json.RawMessage(nil), state.Value...)
	// [决策理由] 设置值理论上始终合法，空值仍安全输出 null 而非破坏整个响应编码。
	if len(value) == 0 {
		value = json.RawMessage(`null`)
	}
	view := settingResponse{Key: state.Key, Value: value, Description: state.Description, Overridden: state.Overridden}

	// >>> 数据演变示例
	// 1. prefix="!"+Overridden=true -> DTO保留JSON字符串和覆盖标记。
	// 2. 空Value -> null -> 保持合法统一响应。
	return view
}

// listAuditLogs 返回支持分页和筛选的只读审计列表。
// @param writer：响应写入器；request：携带可选查询参数的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：执行 PostgreSQL 审计计数与分页查询并写入 JSON 响应。
func (s *Server) listAuditLogs(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	query, err := parseAuditQuery(request)
	// [决策理由] 查询参数格式错误应在访问数据库前返回400。
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_audit_query", "审计查询参数无效")
		return
	}
	page, err := s.management.ListAuditLogs(request.Context(), actor, query)
	// [决策理由] 授权、参数边界或数据库错误需要统一领域映射。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	items := make([]auditResponse, 0, len(page.Items))
	for _, state := range page.Items {
		items = append(items, auditView(state))
	}
	writeSuccess(writer, auditPageResponse{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})

	// >>> 数据演变示例
	// 1. page=1&page_size=20&actor_id=100 -> 筛选分页 -> 200 AuditPage。
	// 2. start_time=bad -> 参数解析失败 -> 400零数据库访问。
}

// getAuditLog 返回指定审计记录的完整前后快照。
// @param writer：响应写入器；request：携带审计 ID 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：执行 PostgreSQL 单条只读查询并写入 JSON 响应。
func (s *Server) getAuditLog(writer http.ResponseWriter, request *http.Request) {
	actor := actorFromRequest(request)
	id, err := parsePositiveID(request.PathValue("audit_id"))
	// [决策理由] 非正整数路径不能定位审计主键，应在查询前拒绝。
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "审计日志 ID 格式无效")
		return
	}
	state, err := s.management.GetAuditLog(request.Context(), actor, id)
	// [决策理由] 未找到和数据库错误必须返回不同业务码。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, auditView(state))

	// >>> 数据演变示例
	// 1. id8存在 -> 查询完整before/after -> 200。
	// 2. id404 -> ErrAuditNotFound -> 404 audit_not_found。
}

// parseAuditQuery 解析审计分页、精确筛选和时间范围。
// @param request：HTTP 查询请求。
// @returns 带默认分页的 AuditQuery 或格式错误。
// ⚠️副作用说明：无；仅读取 URL 查询参数。
func parseAuditQuery(request *http.Request) (management.AuditQuery, error) {
	values := request.URL.Query()
	query := management.AuditQuery{Page: 1, PageSize: 20, ActorID: values.Get("actor_id"), Action: values.Get("action"), TargetType: values.Get("target_type"), TargetID: values.Get("target_id")}
	// [决策理由] 非空 page 必须是十进制整数。
	if raw := values.Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		// [决策理由] 解析失败无法形成稳定分页偏移。
		if err != nil {
			return management.AuditQuery{}, err
		}
		query.Page = value
	}
	// [决策理由] 非空 page_size 必须是十进制整数。
	if raw := values.Get("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		// [决策理由] 解析失败无法形成稳定 LIMIT。
		if err != nil {
			return management.AuditQuery{}, err
		}
		query.PageSize = value
	}
	startTime, err := parseOptionalTime(values.Get("start_time"))
	// [决策理由] 起始时间必须使用 RFC3339，避免时区歧义。
	if err != nil {
		return management.AuditQuery{}, err
	}
	endTime, err := parseOptionalTime(values.Get("end_time"))
	// [决策理由] 结束时间必须使用 RFC3339，避免服务器本地时区猜测。
	if err != nil {
		return management.AuditQuery{}, err
	}
	query.StartTime, query.EndTime = startTime, endTime

	// >>> 数据演变示例
	// 1. 无参数 -> page1,size20,空筛选。
	// 2. page=2&start_time=RFC3339 -> page2+时间指针。
	return query, nil
}

// parseOptionalTime 解析可为空的 RFC3339 时间。
// @param value：查询参数时间原文。
// @returns 空值对应 nil，非空对应 UTC 含时区时间或错误。
// ⚠️副作用说明：无。
func parseOptionalTime(value string) (*time.Time, error) {
	// [决策理由] 空筛选表示不限制该时间边界。
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	// [决策理由] 非 RFC3339 时间缺少稳定时区语义，必须拒绝。
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()

	// >>> 数据演变示例
	// 1. "" -> nil,nil。
	// 2. 2026-07-13T10:00:00+08:00 -> 2026-07-13T02:00:00Z。
	return &utc, nil
}

// auditView 将内部审计状态转换成稳定 API 模型。
// @param state：管理服务审计状态。
// @returns JSON 快照为 null 或原值的 snake_case 审计响应。
// ⚠️副作用说明：复制前后 JSON 字节。
func auditView(state management.AuditState) auditResponse {
	before := append(json.RawMessage(nil), state.BeforeJSON...)
	after := append(json.RawMessage(nil), state.AfterJSON...)
	// [决策理由] 数据库 NULL 快照应编码成 JSON null，而不是省略或破坏响应。
	if len(before) == 0 {
		before = json.RawMessage(`null`)
	}
	// [决策理由] 数据库 NULL 后快照通常表示删除操作，应明确输出 null。
	if len(after) == 0 {
		after = json.RawMessage(`null`)
	}
	view := auditResponse{ID: state.ID, ActorID: state.ActorID, ActorRole: state.ActorRole, Channel: state.Channel, Action: state.Action, TargetType: state.TargetType, TargetID: state.TargetID, Before: before, After: after, Success: state.Success, ErrorMessage: state.ErrorMessage, RequestID: state.RequestID, CreatedAt: state.CreatedAt.UTC()}

	// >>> 数据演变示例
	// 1. update含before/after+东八区时间 -> DTO保留JSON并转UTC。
	// 2. delete的after为空 -> after:null。
	return view
}

// actorFromRequest 将认证身份与请求 ID 转换成管理服务 Actor。
// @param request：已通过 authenticate 和 middleware 的请求。
// @returns 认证中间件注入的 WebUI 最高管理员 Actor。
// ⚠️副作用说明：无；仅读取请求上下文。
func actorFromRequest(request *http.Request) management.Actor {
	actor, _ := request.Context().Value(managementActorContextKey).(management.Actor)

	// >>> 数据演变示例
	// 1. authenticate注入Actor100:req-abc -> 返回可信Actor。
	// 2. 未经路由中间件直接调用Handler -> 返回零值并由Service再次拒绝。
	return actor
}

// pluginView 将内部插件状态转换成稳定的 snake_case API 模型。
// @param state：管理服务插件状态。
// @returns 不暴露内部字段命名的插件响应。
// ⚠️副作用说明：复制 JSON 配置字节，避免响应持有共享切片。
func pluginView(state management.PluginState) pluginResponse {
	config := append(json.RawMessage(nil), state.ConfigJSON...)
	// [决策理由] 数据库历史空值不是合法 JSON，API 应稳定返回空对象。
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	view := pluginResponse{Name: state.Name, DisplayName: state.DisplayName, Description: state.Description, Version: state.Version, Available: state.Available, Enabled: state.Enabled, Priority: state.Priority, Config: config}

	// >>> 数据演变示例
	// 1. ping+config{} -> snake_case DTO+独立JSON副本。
	// 2. config空 -> 默认{} -> 保持合法JSON响应。
	return view
}

// writeManagementError 将管理领域错误映射为稳定 HTTP 状态和错误码。
// @param writer：响应写入器；err：管理服务返回错误。
// @returns 无。
// ⚠️副作用说明：写入 JSON 错误响应，并可能记录服务端错误日志。
func writeManagementError(writer http.ResponseWriter, err error) {
	// [决策理由] 领域错误使用 errors.Is 穿透服务层上下文包装。
	if errors.Is(err, admin.ErrPluginNotFound) {
		writeError(writer, http.StatusNotFound, "plugin_not_found", "插件不存在")
		return
	}
	// [决策理由] 受保护插件冲突属于可预期业务拒绝。
	if errors.Is(err, admin.ErrProtectedPlugin) {
		writeError(writer, http.StatusConflict, "protected_plugin", "系统管理插件不可禁用")
		return
	}
	// [决策理由] 命令字段校验失败属于客户端输入错误。
	if errors.Is(err, admin.ErrInvalidCommand) {
		writeError(writer, http.StatusBadRequest, "invalid_command", "命令参数无效")
		return
	}
	// [决策理由] 命令主键不存在时前端应刷新或移除陈旧列表项。
	if errors.Is(err, admin.ErrCommandNotFound) {
		writeError(writer, http.StatusNotFound, "command_not_found", "命令不存在")
		return
	}
	// [决策理由] 同作用域标准化命令重复是资源冲突，不是服务器异常。
	if errors.Is(err, admin.ErrCommandConflict) {
		writeError(writer, http.StatusConflict, "command_conflict", "命令在当前作用域内重复")
		return
	}
	// [决策理由] 权限维度或主体格式错误属于客户端输入问题。
	if errors.Is(err, admin.ErrInvalidPermission) {
		writeError(writer, http.StatusBadRequest, "invalid_permission", "权限策略参数无效")
		return
	}
	// [决策理由] 不存在的权限 ID 应提示前端刷新策略列表。
	if errors.Is(err, admin.ErrPermissionNotFound) {
		writeError(writer, http.StatusNotFound, "permission_not_found", "权限策略不存在")
		return
	}
	// [决策理由] 功能外键不存在表示页面提交了陈旧 Manifest 目标。
	if errors.Is(err, admin.ErrFeatureNotFound) {
		writeError(writer, http.StatusNotFound, "feature_not_found", "插件功能不存在")
		return
	}
	// [决策理由] 设置 JSON 类型或范围错误属于客户端字段问题。
	if errors.Is(err, admin.ErrInvalidSetting) {
		writeError(writer, http.StatusBadRequest, "invalid_setting", "系统设置值无效")
		return
	}
	// [决策理由] 未注册设置键不属于可管理资源。
	if errors.Is(err, admin.ErrUnknownSetting) {
		writeError(writer, http.StatusNotFound, "unknown_setting", "未知系统设置")
		return
	}
	// [决策理由] 删除不存在的数据库覆盖表示当前设置已经在使用默认值。
	if errors.Is(err, admin.ErrSettingNotFound) {
		writeError(writer, http.StatusNotFound, "setting_override_not_found", "系统设置当前没有覆盖值")
		return
	}
	// [决策理由] 审计分页边界或时间区间错误属于客户端查询问题。
	if errors.Is(err, admin.ErrInvalidAuditQuery) {
		writeError(writer, http.StatusBadRequest, "invalid_audit_query", "审计查询参数无效")
		return
	}
	// [决策理由] 不存在的审计主键应返回404而非通用服务故障。
	if errors.Is(err, admin.ErrAuditNotFound) {
		writeError(writer, http.StatusNotFound, "audit_not_found", "审计日志不存在")
		return
	}
	// [决策理由] 授权失败不得被误报成服务器故障。
	if errors.Is(err, admin.ErrForbidden) || errors.Is(err, admin.ErrInvalidActor) || errors.Is(err, admin.ErrInvalidChannel) {
		writeError(writer, http.StatusForbidden, "forbidden", "无权执行该管理操作")
		return
	}
	projectlogger.Error("WebAPI管理操作失败", "error", err)
	writeError(writer, http.StatusInternalServerError, "internal_error", "管理操作执行失败")

	// >>> 数据演变示例
	// 1. ErrCommandConflict -> 409 command_conflict。
	// 2. 数据库连接错误 -> 记录内部错误 -> 500通用消息。
}

// authenticate 验证 Bearer JWT 并再次确认管理员仍处于启用状态。
// @param next：鉴权成功后调用的处理器。
// @returns 包装后的 HTTP 处理器。
// ⚠️副作用说明：读取请求头和管理员快照；失败时写入401响应。
func (s *Server) authenticate(next http.Handler) http.Handler {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("Authorization")
		// [决策理由] 仅接受标准 Bearer 方案，避免把其他认证头误作 JWT。
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "缺少登录凭证")
			return
		}
		claims, err := s.verify(strings.TrimPrefix(header, "Bearer "))
		// [决策理由] 签名、结构或有效期任一失败都不能建立可信身份。
		if err != nil {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "登录凭证无效或已过期")
			return
		}
		account, exists := s.admins.Resolve(claims.Subject)
		// [决策理由] 每次请求查询内存快照，使管理员禁用后已签发 Token 立即失效。
		if !exists {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "管理员账号已停用")
			return
		}
		ctx := context.WithValue(request.Context(), actorContextKey, account)
		requestID, _ := request.Context().Value(requestIDContextKey).(string)
		actor := management.Actor{ID: account.UserID, Role: "super_admin", Channel: management.ChannelWebUI, RequestID: requestID}
		ctx = context.WithValue(ctx, managementActorContextKey, actor)
		next.ServeHTTP(writer, request.WithContext(ctx))

		// >>> 数据演变示例
		// 1. 有效Token+启用管理员 -> 注入account和审计Actor -> next。
		// 2. 有效Token+管理员已禁用 -> 401 -> 不调用next。
	})

	// >>> 数据演变示例
	// 1. me处理器 -> authenticate包装 -> 受保护处理器。
	// 2. nil身份请求 -> 包装器校验失败 -> 401。
	return handler
}

// sign 为管理员签发固定12小时有效的 HMAC JWT。
// @param subject：管理员 QQ 号。
// @returns JWT 字符串或 JSON 编码错误。
// ⚠️副作用说明：读取当前时间。
func (s *Server) sign(subject string) (string, error) {
	now := s.now()
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	// [决策理由] 虽然静态头通常不会失败，仍需保持签名链路错误完整。
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(tokenClaims{Subject: subject, IssuedAt: now.Unix(), ExpiresAt: now.Add(tokenLifetime).Unix()})
	// [决策理由] Claims 编码失败时不存在可安全签发的载荷。
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := s.signature(unsigned)

	// >>> 数据演变示例
	// 1. QQ100+时间0 -> header.payload+HMAC -> 三段JWT。
	// 2. QQ200+时间1 -> 不同payload和签名 -> 独立JWT。
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// verify 校验 JWT 结构、HMAC 签名和时间范围。
// @param token：Bearer Token 原文。
// @returns 可信 Claims 或校验错误。
// ⚠️副作用说明：读取当前时间。
func (s *Server) verify(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	// [决策理由] JWT 必须严格包含 header、payload、signature 三段。
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("JWT结构无效")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	// [决策理由] 无法解码的签名不能进行可信比较。
	if err != nil || !hmac.Equal(provided, s.signature(parts[0]+"."+parts[1])) {
		return tokenClaims{}, errors.New("JWT签名无效")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	// [决策理由] 必须固定算法为HS256，防止算法替换攻击。
	if err != nil || string(headerBytes) != `{"alg":"HS256","typ":"JWT"}` {
		return tokenClaims{}, errors.New("JWT头无效")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	// [决策理由] Claims 必须可解析并拒绝未知字段。
	if err != nil {
		return tokenClaims{}, errors.New("JWT载荷无效")
	}
	var claims tokenClaims
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	// [决策理由] 身份和时间字段缺失时 Token 不能用于授权。
	if err := decoder.Decode(&claims); err != nil || claims.Subject == "" || claims.ExpiresAt == 0 || claims.IssuedAt == 0 {
		return tokenClaims{}, errors.New("JWT声明无效")
	}
	now := s.now().Unix()
	// [决策理由] 已过期 Token 或签发时间明显位于未来都不可信。
	if claims.ExpiresAt <= now || claims.IssuedAt > now+60 {
		return tokenClaims{}, errors.New("JWT已过期或时间无效")
	}

	// >>> 数据演变示例
	// 1. 三段Token+正确HMAC+未过期 -> Claims,nil。
	// 2. payload被修改 -> HMAC不匹配 -> error。
	return claims, nil
}

// signature 计算 JWT 未签名内容的 HMAC-SHA256。
// @param unsigned：JWT 的 header.payload 部分。
// @returns 32字节消息认证码。
// ⚠️副作用说明：无。
func (s *Server) signature(unsigned string) []byte {
	hash := hmac.New(sha256.New, s.jwtSecret)
	_, _ = hash.Write([]byte(unsigned))
	result := hash.Sum(nil)

	// >>> 数据演变示例
	// 1. secretA+payloadA -> HMAC A。
	// 2. secretA+payloadB -> HMAC B。
	return result
}

// middleware 注入请求ID、安全响应头及访问日志。
// @param next：业务路由处理器。
// @returns 包装后的 HTTP 处理器。
// ⚠️副作用说明：读取随机源、修改响应头并写入访问日志。
func (s *Server) middleware(next http.Handler) http.Handler {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID := request.Header.Get("X-Request-ID")
		// [决策理由] 客户端未提供追踪标识时生成本地随机ID，便于关联审计和故障日志。
		if requestID == "" {
			buffer := make([]byte, 12)
			// [决策理由] 随机源异常时仍使用明确占位符，不影响 API 可用性。
			if _, err := rand.Read(buffer); err != nil {
				requestID = "unavailable"
			} else {
				requestID = hex.EncodeToString(buffer)
			}
		}
		writer.Header().Set("X-Request-ID", requestID)
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		ctx := context.WithValue(request.Context(), requestIDContextKey, requestID)
		next.ServeHTTP(writer, request.WithContext(ctx))
		projectlogger.Info("WebAPI请求", "method", request.Method, "path", request.URL.Path, "request_id", requestID, "duration", time.Since(started))

		// >>> 数据演变示例
		// 1. 无X-Request-ID请求 -> 随机ID+安全头 -> 业务响应+访问日志。
		// 2. X-Request-ID=abc -> 保留abc -> 业务响应+关联日志。
	})

	// >>> 数据演变示例
	// 1. mux -> middleware包装 -> 带安全头处理器。
	// 2. login请求 -> 包装器 -> mux -> login -> 日志。
	return handler
}

// writeJSON 写入统一 JSON 响应。
// @param writer：响应写入器；status：HTTP 状态码；value：响应对象。
// @returns 无。
// ⚠️副作用说明：设置响应头并写入响应体。
func writeJSON(writer http.ResponseWriter, status int, value responseEnvelope) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	// [决策理由] 响应头已发送后编码错误无法改写状态，仅记录故障供排查。
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		projectlogger.Error("编码WebAPI响应失败", "error", err)
	}

	// >>> 数据演变示例
	// 1. status=200+code=ok -> application/json -> 200统一JSON。
	// 2. status=401+code=unauthorized -> application/json -> 401统一JSON。
}

// writeSuccess 写入统一成功响应。
// @param writer：响应写入器；data：业务响应数据。
// @returns 无。
// ⚠️副作用说明：写入状态码200和 JSON HTTP 响应。
func writeSuccess(writer http.ResponseWriter, data any) {
	writeJSON(writer, http.StatusOK, responseEnvelope{Code: "ok", Message: "操作成功", Data: data})

	// >>> 数据演变示例
	// 1. plugin DTO -> code=ok,message=操作成功,data=plugin。
	// 2. 空列表 -> code=ok,message=操作成功,data=[]。
}

// writeError 写入统一 API 错误结构。
// @param writer：响应写入器；status：HTTP 状态码；code：稳定错误码；message：用户消息。
// @returns 无。
// ⚠️副作用说明：写入 JSON HTTP 响应。
func writeError(writer http.ResponseWriter, status int, code string, message string) {
	writeJSON(writer, status, responseEnvelope{Code: code, Message: message, Data: nil})

	// >>> 数据演变示例
	// 1. 401+unauthorized -> code/message/data:null -> JSON响应。
	// 2. 400+invalid_request -> code/message/data:null -> JSON响应。
}
