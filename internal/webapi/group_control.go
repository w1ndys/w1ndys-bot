// 📌 影响范围：读写插件群默认与单群覆盖 API；委派审计、CAS 与热刷新。
package webapi

import (
	"net/http"
	"strconv"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

type groupControlWriteRequest struct {
	Enabled         bool  `json:"enabled"`
	ExpectedVersion int64 `json:"expected_version"`
}

// getPluginGroupControl 返回群控制快照。
// @param writer：响应写入器；request：已鉴权插件请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 并写 JSON 响应。
func (s *Server) getPluginGroupControl(writer http.ResponseWriter, request *http.Request) {
	state, err := s.management.GetPluginGroupControl(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"))
	// [决策理由] 授权、不支持和查询错误必须统一映射。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, state)

	// >>> 数据演变示例
	// 1. keyword_reply -> default+覆盖 -> 200。
	// 2. 未鉴权 -> 401。
}

// patchPluginGroupDefault 使用 CAS 修改群默认值。
// @param writer：响应写入器；request：已鉴权严格 JSON 请求。
// @returns 无。
// ⚠️副作用说明：写入策略、审计并热刷新。
func (s *Server) patchPluginGroupDefault(writer http.ResponseWriter, request *http.Request) {
	var input groupControlWriteRequest
	// [决策理由] 写入必须携带正版本且拒绝未知字段。
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_group_control", "群默认策略参数无效")
		return
	}
	state, err := s.management.SetPluginGroupDefault(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), input.Enabled, input.ExpectedVersion)
	// [决策理由] CAS 冲突需返回 409 供前端刷新。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, state)

	// >>> 数据演变示例
	// 1. enabled=false,v2 -> PATCH -> v3。
	// 2. expected1,current2 -> 409。
}

// putPluginGroupOverride 新增或 CAS 更新单群覆盖。
// @param writer：响应写入器；request：携带群号的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：写入覆盖、审计并热刷新。
func (s *Server) putPluginGroupOverride(writer http.ResponseWriter, request *http.Request) {
	var input groupControlWriteRequest
	// [决策理由] version=0 表示新增，负数和未知字段必须拒绝。
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_group_control", "单群覆盖参数无效")
		return
	}
	item, err := s.management.SetPluginGroupOverride(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), request.PathValue("group_id"), input.Enabled, input.ExpectedVersion)
	// [决策理由] 群号校验、冲突和刷新失败需区分响应。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, item)

	// >>> 数据演变示例
	// 1. group100,true,v0 -> INSERT v1。
	// 2. group100,false,陈旧v1 -> 409。
}

// deletePluginGroupOverride 删除单群覆盖以恢复继承。
// @param writer：响应写入器；request：携带群号与 expected_version 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：删除覆盖、写审计并热刷新。
func (s *Server) deletePluginGroupOverride(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	versions, exists := values["expected_version"]
	// [决策理由] DELETE 查询只允许一个 expected_version，拒绝重复值和额外未知参数。
	if !exists || len(versions) != 1 || len(values) != 1 {
		writeError(writer, http.StatusBadRequest, "invalid_group_control", "单群覆盖版本无效")
		return
	}
	version, err := strconv.ParseInt(versions[0], 10, 64)
	// [决策理由] 删除必须使用正版本 CAS，避免删除他人刚修改的覆盖。
	if err != nil || version <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_group_control", "单群覆盖版本无效")
		return
	}
	err = s.management.DeletePluginGroupOverride(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), request.PathValue("group_id"), version)
	// [决策理由] 冲突和刷新错误需使用统一映射。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, map[string]bool{"deleted": true})

	// >>> 数据演变示例
	// 1. group100?v=2 -> DELETE -> 继承default。
	// 2. 缺少version -> 400。
}

var _ = management.PluginGroupControlState{}
