// 📌 影响范围：读取插件业务资源声明与记录；委派通用 WebUI CRUD 请求。
package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

// listPluginResources 返回插件声明的通用业务资源。
// @param writer：响应写入器；request：携带插件名的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取插件声明并写入 JSON 响应。
func (s *Server) listPluginResources(writer http.ResponseWriter, request *http.Request) {
	resources, err := s.management.ListPluginResources(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"))
	// [决策理由] 授权、插件和能力错误需使用稳定管理响应。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, resources)

	// >>> 数据演变示例
	// 1. GET keyword_reply/resources -> [rules descriptor] -> 200。
	// 2. GET echo/resources -> 不支持 -> 404。
}

// listPluginResourceRecords 返回插件业务资源分页记录。
// @param writer：响应写入器；request：携带路由键与分页查询的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取插件自有表并写入 JSON 响应。
func (s *Server) listPluginResourceRecords(writer http.ResponseWriter, request *http.Request) {
	query, err := parseResourceQuery(request)
	// [决策理由] 分页参数必须是正整数，资源上限由服务层二次校验。
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_resource_query", "资源分页参数无效")
		return
	}
	page, err := s.management.ListPluginResourceRecords(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), request.PathValue("resource_key"), query)
	// [决策理由] 查询错误由统一领域映射转换。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, resourcePageView(page))

	// >>> 数据演变示例
	// 1. page=2&page_size=20 -> Service -> 返回第2页。
	// 2. page=x -> 解析失败 -> 400。
}

// createPluginResourceRecord 新增插件业务记录。
// @param writer：响应写入器；request：携带严格 data 对象的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：委派插件事务写业务表和审计表。
func (s *Server) createPluginResourceRecord(writer http.ResponseWriter, request *http.Request) {
	var input resourceDataRequest
	// [决策理由] 外层 JSON 严格限制为 data，业务字段由插件领域校验。
	if err := decodeJSON(writer, request, &input); err != nil || len(input.Data) == 0 {
		writeError(writer, http.StatusBadRequest, "invalid_resource_data", "插件资源数据无效")
		return
	}
	record, err := s.management.CreatePluginResourceRecord(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), request.PathValue("resource_key"), input.Data)
	// [决策理由] 领域校验、冲突与数据库错误必须区分响应。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, resourceRecordView(record))

	// >>> 数据演变示例
	// 1. data{keyword:"hi"} -> 插件Create -> id1/v1。
	// 2. 外层含unknown -> strict decoder -> 400。
}

// updatePluginResourceRecord 按版本更新插件业务记录。
// @param writer：响应写入器；request：携带记录 ID、data 和 expected_version 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：委派插件 CAS 事务更新和审计写入。
func (s *Server) updatePluginResourceRecord(writer http.ResponseWriter, request *http.Request) {
	id, err := strconv.ParseInt(request.PathValue("record_id"), 10, 64)
	// [决策理由] 记录主键必须是正 int64，不将任意字符串传给插件。
	if err != nil || id <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_resource_data", "插件资源记录 ID 无效")
		return
	}
	var input resourceUpdateRequest
	// [决策理由] 更新必须显式携带业务数据与正版本号。
	if err := decodeJSON(writer, request, &input); err != nil || len(input.Data) == 0 || input.ExpectedVersion <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_resource_data", "插件资源数据或版本无效")
		return
	}
	record, err := s.management.UpdatePluginResourceRecord(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), request.PathValue("resource_key"), id, input.ExpectedVersion, input.Data)
	// [决策理由] 版本冲突需返回 409 供 WebUI 刷新后重试。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, resourceRecordView(record))

	// >>> 数据演变示例
	// 1. id1+expected2 -> CAS成功 -> version3。
	// 2. id1+expected1而当前2 -> 409 conflict。
}

// deletePluginResourceRecord 按查询版本删除插件业务记录。
// @param writer：响应写入器；request：携带记录 ID 与 expected_version 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：委派插件 CAS 删除与审计写入。
func (s *Server) deletePluginResourceRecord(writer http.ResponseWriter, request *http.Request) {
	id, idErr := strconv.ParseInt(request.PathValue("record_id"), 10, 64)
	version, versionErr := strconv.ParseInt(request.URL.Query().Get("expected_version"), 10, 64)
	// [决策理由] DELETE 无请求体，统一使用必填 expected_version 查询参数执行 CAS。
	if idErr != nil || versionErr != nil || id <= 0 || version <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_resource_data", "插件资源记录 ID 或版本无效")
		return
	}
	err := s.management.DeletePluginResourceRecord(request.Context(), actorFromRequest(request), request.PathValue("plugin_name"), request.PathValue("resource_key"), id, version)
	// [决策理由] 不存在、冲突与服务端错误需使用稳定映射。
	if err != nil {
		writeManagementError(writer, err)
		return
	}
	writeSuccess(writer, map[string]bool{"deleted": true})

	// >>> 数据演变示例
	// 1. id1?expected_version=3 -> CAS DELETE -> deleted:true。
	// 2. 缺少expected_version -> 400 -> 不调用插件。
}

// parseResourceQuery 解析默认值与正整数分页参数。
// @param request：HTTP 请求。
// @returns 默认 page=1/page_size=20 的查询或解析错误。
// ⚠️副作用说明：无。
func parseResourceQuery(request *http.Request) (management.ResourceQuery, error) {
	query := management.ResourceQuery{Page: 1}
	var err error
	// [决策理由] 缺省 page 使用第一页，显式值必须是十进制整数。
	if raw := request.URL.Query().Get("page"); raw != "" {
		query.Page, err = strconv.Atoi(raw)
		// [决策理由] 解析失败时不能使用零值 OFFSET。
		if err != nil {
			return management.ResourceQuery{}, err
		}
	}
	// [决策理由] 缺省 page_size 使用 20，最终上限由资源 descriptor 决定。
	if raw := request.URL.Query().Get("page_size"); raw != "" {
		query.PageSize, err = strconv.Atoi(raw)
		// [决策理由] 非整数限制无法安全委派。
		if err != nil {
			return management.ResourceQuery{}, err
		}
	}
	// [决策理由] 非正分页参数无法形成稳定页面语义。
	if query.Page < 1 || query.PageSize < 0 {
		return management.ResourceQuery{}, strconv.ErrSyntax
	}

	// >>> 数据演变示例
	// 1. 无query -> page1,size20。
	// 2. page=0 -> 正数检查 -> error。
	return query, nil
}

// resourceRecordView 转换资源记录为 snake_case JSON DTO。
// @param record：插件返回的记录。
// @returns 复制 data 字节的响应。
// ⚠️副作用说明：无。
func resourceRecordView(record management.ResourceRecord) resourceRecordResponse {
	data := append(json.RawMessage(nil), record.Data...)
	view := resourceRecordResponse{ID: record.ID, Version: record.Version, Data: data}

	// >>> 数据演变示例
	// 1. id1/v2/data{} -> 复制 -> JSON DTO。
	// 2. data nil -> nil副本 -> JSON null。
	return view
}

// resourcePageView 转换插件分页结果。
// @param page：插件返回的记录页。
// @returns 带 DTO 记录、分页和总数的响应。
// ⚠️副作用说明：复制所有 data 字节。
func resourcePageView(page management.ResourcePage) resourcePageResponse {
	items := make([]resourceRecordResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, resourceRecordView(item))
	}
	view := resourcePageResponse{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}

	// >>> 数据演变示例
	// 1. 2条记录+total5 -> 2条DTO+total5。
	// 2. 空页 -> []+total0。
	return view
}
