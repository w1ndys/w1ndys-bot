// 📌 影响范围：读写目标插件架构的全局与群开关；委派鉴权、乐观锁、审计与生命周期。
package webapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

// RuntimeStateController 定义目标插件架构对 WebUI 开放的开关能力。
type RuntimeStateController interface {
	List(context.Context, management.Actor) ([]plugin.RuntimeStateView, error)
	Get(context.Context, management.Actor, string) (plugin.RuntimeStateView, error)
	SetGlobalEnabled(context.Context, management.Actor, string, bool, int64) (plugin.RuntimeStateView, error)
	SetGroupEnabled(context.Context, management.Actor, string, int64, bool, int64) (plugin.RuntimeStateView, error)
}

type runtimeStateWriteRequest struct {
	Enabled         bool  `json:"enabled"`
	ExpectedVersion int64 `json:"expected_version"`
}

// listPluginRuntimes 返回全部目标插件的意图、运行状态与群开关。
// @param writer：响应写入器；request：已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 与进程内运行状态并写 JSON 响应。
func (s *Server) listPluginRuntimes(writer http.ResponseWriter, request *http.Request) {
	states, err := s.runtimes.List(request.Context(), actorFromRequest(request))
	if err != nil {
		writeRuntimeError(writer, err)
		return
	}
	writeSuccess(writer, states)

	// >>> 数据演变示例
	// 1. echo已启用 -> desired_enabled=true,status=ready -> 200。
	// 2. 未鉴权 -> 401。
}

// getPluginRuntime 返回单个目标插件的意图、运行状态与群开关。
// @param writer：响应写入器；request：携带插件 Key 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 与进程内运行状态并写 JSON 响应。
func (s *Server) getPluginRuntime(writer http.ResponseWriter, request *http.Request) {
	state, err := s.runtimes.Get(request.Context(), actorFromRequest(request), request.PathValue("plugin_key"))
	if err != nil {
		writeRuntimeError(writer, err)
		return
	}
	writeSuccess(writer, state)

	// >>> 数据演变示例
	// 1. echo -> 意图+实际状态+群开关 -> 200。
	// 2. 未编译进目录的 Key -> 404。
}

// patchPluginRuntime 按乐观锁修改插件全局启用意图并驱动生命周期。
// @param writer：响应写入器；request：已鉴权严格 JSON 请求。
// @returns 无。
// ⚠️副作用说明：写入状态与审计，并可能执行插件启停生命周期。
func (s *Server) patchPluginRuntime(writer http.ResponseWriter, request *http.Request) {
	var input runtimeStateWriteRequest
	// [决策理由] 全局状态行由目录同步创建，版本必须为正，同时拒绝未知字段。
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_plugin_runtime", "插件开关参数无效")
		return
	}
	state, err := s.runtimes.SetGlobalEnabled(request.Context(), actorFromRequest(request), request.PathValue("plugin_key"), input.Enabled, input.ExpectedVersion)
	if err != nil {
		writeRuntimeError(writer, err)
		return
	}
	writeSuccess(writer, state)

	// >>> 数据演变示例
	// 1. echo,enabled=true,v1 -> 落库v2 -> OnEnable -> status=ready。
	// 2. expected1,current2 -> 409。
}

// putPluginRuntimeGroup 按乐观锁修改插件在单个群的开关。
// @param writer：响应写入器；request：携带插件 Key 与群号的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：写入群状态与审计，并刷新进程内群门禁。
func (s *Server) putPluginRuntimeGroup(writer http.ResponseWriter, request *http.Request) {
	var input runtimeStateWriteRequest
	// [决策理由] version=0 表示尚无群记录，负数和未知字段必须拒绝。
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_plugin_runtime", "插件群开关参数无效")
		return
	}
	groupID, err := strconv.ParseInt(request.PathValue("group_id"), 10, 64)
	// [决策理由] 群号来自可信路径参数，格式错误必须在调用服务前拒绝。
	if err != nil || groupID <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_plugin_runtime", "群号无效")
		return
	}
	state, err := s.runtimes.SetGroupEnabled(request.Context(), actorFromRequest(request), request.PathValue("plugin_key"), groupID, input.Enabled, input.ExpectedVersion)
	if err != nil {
		writeRuntimeError(writer, err)
		return
	}
	writeSuccess(writer, state)

	// >>> 数据演变示例
	// 1. echo,group100,true,v0 -> 新增v1 -> 该群命令可用。
	// 2. 全局关闭时写入群意图 -> 200，但命令仍被全局门禁拒绝。
}

// writeRuntimeError 将目标插件开关的领域错误映射为稳定 HTTP 响应。
// @param writer：响应写入器；err：服务层错误。
// @returns 无。
// ⚠️副作用说明：写入 JSON 错误响应；未识别错误记录服务端日志。
func writeRuntimeError(writer http.ResponseWriter, err error) {
	switch {
	// [决策理由] 未编译进目录或尚未同步状态行的插件不是可管理资源。
	case errors.Is(err, plugin.ErrRuntimePluginNotFound), errors.Is(err, plugin.ErrRuntimeStateNotFound):
		writeError(writer, http.StatusNotFound, "plugin_runtime_not_found", "插件不存在")
	// [决策理由] 陈旧版本要求前端重新读取后再提交。
	case errors.Is(err, plugin.ErrRuntimeStateConflict):
		writeError(writer, http.StatusConflict, "plugin_runtime_conflict", "插件开关已被其他操作更新")
	// [决策理由] 正在启停的插件必须等待本次切换结束，属于可重试冲突。
	case errors.Is(err, plugin.ErrRuntimeTransition):
		writeError(writer, http.StatusConflict, "plugin_runtime_transition", "插件正在切换运行状态")
	// [决策理由] 故障插件必须先关闭完成清理才能重新启用。
	case errors.Is(err, plugin.ErrRuntimeRecoveryNeeded):
		writeError(writer, http.StatusConflict, "plugin_runtime_recovery_needed", "插件故障后必须先禁用再启用")
	// [决策理由] 群号非法属于可修正输入。
	case errors.Is(err, plugin.ErrInvalidRuntimeGroupID):
		writeError(writer, http.StatusBadRequest, "invalid_plugin_runtime", "群号无效")
	default:
		// [决策理由] 授权失败与未识别错误复用统一管理错误映射，避免两套响应语义。
		writeManagementError(writer, err)
	}

	// >>> 数据演变示例
	// 1. ErrRuntimeStateConflict -> 409 plugin_runtime_conflict。
	// 2. admin.ErrForbidden -> 交给 writeManagementError -> 403。
}
