// 📌 影响范围：读写关键词回复插件的群规则；委派鉴权、领域校验、事务审计与运行快照刷新。
package webapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	keywordreply "github.com/w1ndys/w1ndys-bot/plugins/keyword_reply"
)

// KeywordReplyController 定义关键词回复插件对 WebUI 开放的群规则管理能力。
type KeywordReplyController interface {
	ListRules(context.Context, management.Actor, int64, int, int) (keywordreply.RulePage, error)
	CreateRule(context.Context, management.Actor, int64, keywordreply.RuleInput) (keywordreply.Rule, error)
	UpdateRule(context.Context, management.Actor, int64, int64, int64, keywordreply.RuleInput) (keywordreply.Rule, error)
	DeleteRule(context.Context, management.Actor, int64, int64, int64) error
}

type keywordRuleWriteRequest struct {
	Keyword         string `json:"keyword"`
	ReplyContent    string `json:"reply_content"`
	Enabled         bool   `json:"enabled"`
	ExpectedVersion int64  `json:"expected_version"`
}

type keywordRuleDeleteRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

// listKeywordRules 按可信群号分页返回关键词规则。
// @param writer：响应写入器；request：携带群号的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：读取 PostgreSQL 并写 JSON 响应。
func (s *Server) listKeywordRules(writer http.ResponseWriter, request *http.Request) {
	groupID, ok := keywordGroupID(writer, request)
	if !ok {
		return
	}
	page, pageSize := paginationFromQuery(request)
	result, err := s.keywordReply.ListRules(request.Context(), actorFromRequest(request), groupID, page, pageSize)
	if err != nil {
		writeKeywordReplyError(writer, err)
		return
	}
	writeSuccess(writer, result)

	// >>> 数据演变示例
	// 1. 群100+page1 -> 该群规则分页 -> 200。
	// 2. 群号非法 -> 400。
}

// createKeywordRule 在指定群下新增关键词规则。
// @param writer：响应写入器；request：已鉴权严格 JSON 请求。
// @returns 无。
// ⚠️副作用说明：写入规则与审计，并刷新插件运行快照。
func (s *Server) createKeywordRule(writer http.ResponseWriter, request *http.Request) {
	groupID, ok := keywordGroupID(writer, request)
	if !ok {
		return
	}
	var input keywordRuleWriteRequest
	// [决策理由] 新增不接受版本字段，避免客户端误以为可指定初始版本。
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion != 0 {
		writeError(writer, http.StatusBadRequest, "invalid_keyword_rule", "关键词规则参数无效")
		return
	}
	rule, err := s.keywordReply.CreateRule(request.Context(), actorFromRequest(request), groupID, keywordreply.RuleInput{
		Keyword: input.Keyword, ReplyContent: input.ReplyContent, Enabled: input.Enabled,
	})
	if err != nil {
		writeKeywordReplyError(writer, err)
		return
	}
	writeSuccess(writer, rule)

	// >>> 数据演变示例
	// 1. 群100+"你好" -> 新增 v1 -> 200。
	// 2. 同群重复关键词 -> 409。
}

// updateKeywordRule 按乐观锁更新指定群下的关键词规则。
// @param writer：响应写入器；request：携带群号与规则 ID 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：写入规则与审计，并刷新插件运行快照。
func (s *Server) updateKeywordRule(writer http.ResponseWriter, request *http.Request) {
	groupID, ok := keywordGroupID(writer, request)
	if !ok {
		return
	}
	ruleID, ok := keywordRuleID(writer, request)
	if !ok {
		return
	}
	var input keywordRuleWriteRequest
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_keyword_rule", "关键词规则参数无效")
		return
	}
	rule, err := s.keywordReply.UpdateRule(request.Context(), actorFromRequest(request), groupID, ruleID, input.ExpectedVersion, keywordreply.RuleInput{
		Keyword: input.Keyword, ReplyContent: input.ReplyContent, Enabled: input.Enabled,
	})
	if err != nil {
		writeKeywordReplyError(writer, err)
		return
	}
	writeSuccess(writer, rule)

	// >>> 数据演变示例
	// 1. 群100规则1+v1 -> 更新 -> v2。
	// 2. 陈旧版本 -> 409。
}

// deleteKeywordRule 按乐观锁删除指定群下的关键词规则。
// @param writer：响应写入器；request：携带群号与规则 ID 的已鉴权请求。
// @returns 无。
// ⚠️副作用说明：删除规则、写审计并刷新插件运行快照。
func (s *Server) deleteKeywordRule(writer http.ResponseWriter, request *http.Request) {
	groupID, ok := keywordGroupID(writer, request)
	if !ok {
		return
	}
	ruleID, ok := keywordRuleID(writer, request)
	if !ok {
		return
	}
	var input keywordRuleDeleteRequest
	if err := decodeJSON(writer, request, &input); err != nil || input.ExpectedVersion <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_keyword_rule", "关键词规则参数无效")
		return
	}
	if err := s.keywordReply.DeleteRule(request.Context(), actorFromRequest(request), groupID, ruleID, input.ExpectedVersion); err != nil {
		writeKeywordReplyError(writer, err)
		return
	}
	writeSuccess(writer, map[string]bool{"deleted": true})

	// >>> 数据演变示例
	// 1. 群100规则1+v2 -> 删除 -> 200。
	// 2. 规则已被删除 -> 404。
}

// keywordGroupID 解析可信路径群号。
// @param writer：响应写入器；request：当前请求。
// @returns 正整数群号及是否有效；无效时已写出 400。
// ⚠️副作用说明：无效时写入 JSON 错误响应。
func keywordGroupID(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	groupID, err := strconv.ParseInt(request.PathValue("group_id"), 10, 64)
	// [决策理由] 群号只来自已验证路径参数，不接受请求体覆盖，避免跨群写入。
	if err != nil || groupID <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_keyword_rule", "群号无效")
		return 0, false
	}

	// >>> 数据演变示例
	// 1. "100" -> 100,true。
	// 2. "abc" -> 400,false。
	return groupID, true
}

// keywordRuleID 解析路径中的规则主键。
// @param writer：响应写入器；request：当前请求。
// @returns 正整数规则 ID 及是否有效；无效时已写出 400。
// ⚠️副作用说明：无效时写入 JSON 错误响应。
func keywordRuleID(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	ruleID, err := strconv.ParseInt(request.PathValue("rule_id"), 10, 64)
	if err != nil || ruleID <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_keyword_rule", "规则 ID 无效")
		return 0, false
	}

	// >>> 数据演变示例
	// 1. "8" -> 8,true。
	// 2. "0" -> 400,false。
	return ruleID, true
}

// paginationFromQuery 读取分页参数并回退到安全默认值。
// @param request：当前请求。
// @returns 页码与页大小；缺失或非法时返回默认值交由服务层再次校验。
// ⚠️副作用说明：无。
func paginationFromQuery(request *http.Request) (int, int) {
	page, err := strconv.Atoi(request.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(request.URL.Query().Get("page_size"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}

	// >>> 数据演变示例
	// 1. page=2&page_size=50 -> 2,50。
	// 2. 缺失参数 -> 1,20。
	return page, pageSize
}

// writeKeywordReplyError 将关键词规则领域错误映射为稳定 HTTP 响应。
// @param writer：响应写入器；err：服务层错误。
// @returns 无。
// ⚠️副作用说明：写入 JSON 错误响应；未识别错误记录服务端日志。
func writeKeywordReplyError(writer http.ResponseWriter, err error) {
	switch {
	// [决策理由] 规则被并发删除时前端应刷新当前页。
	case errors.Is(err, keywordreply.ErrRuleNotFound):
		writeError(writer, http.StatusNotFound, "keyword_rule_not_found", "关键词规则不存在")
	// [决策理由] 同群关键词重复与陈旧版本都要求前端刷新后重试。
	case errors.Is(err, keywordreply.ErrRuleConflict):
		writeError(writer, http.StatusConflict, "keyword_rule_conflict", "关键词规则已被其他操作更新或重复")
	// [决策理由] 群号与字段长度错误属于可修正输入。
	case errors.Is(err, keywordreply.ErrInvalidGroup), errors.Is(err, keywordreply.ErrInvalidRule):
		writeError(writer, http.StatusBadRequest, "invalid_keyword_rule", "关键词规则参数无效")
	default:
		// [决策理由] 授权失败与未识别错误复用统一管理错误映射，避免两套响应语义。
		writeManagementError(writer, err)
	}

	// >>> 数据演变示例
	// 1. ErrRuleConflict -> 409 keyword_rule_conflict。
	// 2. admin.ErrForbidden -> 交给 writeManagementError -> 403。
}
