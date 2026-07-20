// 📌 影响范围：读写 PostgreSQL keyword_reply_rules 与 admin_audit_logs；CRUD成功后触发运行快照刷新。
package keywordreply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/w1ndys/w1ndys-bot/internal/management"
)

type repository interface {
	ruleLoader
	List(context.Context, management.ResourceQuery) (management.ResourcePage, error)
	Create(context.Context, management.Actor, ruleInput) (management.ResourceRecord, ruleData, error)
	Update(context.Context, management.Actor, int64, int64, ruleInput) (management.ResourceRecord, ruleData, ruleData, error)
	Delete(context.Context, management.Actor, int64, int64) (ruleData, error)
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

type ruleInput struct {
	Keyword      string `json:"keyword"`
	ReplyContent string `json:"reply_content"`
	Enabled      bool   `json:"enabled"`
}

type rulePayload struct {
	Keyword      string `json:"keyword"`
	ReplyContent string `json:"reply_content"`
	Enabled      *bool  `json:"enabled"`
}

type ruleData struct {
	Keyword      string `json:"keyword"`
	ReplyContent string `json:"reply_content"`
	Enabled      bool   `json:"enabled"`
}

type storedRule struct {
	ID      int64
	Version int64
	Data    ruleData
}

type resourceHandler struct {
	repository  repository
	applyChange func(*ruleData, *ruleData)
	mutationMu  sync.Mutex
}

// newPostgresRepository 创建固定表名的关键词规则仓库。
// @param pool：应用共享PostgreSQL连接池。
// @returns 关键词规则仓库。
// ⚠️副作用说明：仅保存连接池引用，不立即访问数据库。
func newPostgresRepository(pool *pgxpool.Pool) *postgresRepository {
	result := &postgresRepository{pool: pool}

	// >>> 数据演变示例
	// 1. pool实例 -> postgresRepository{pool}。
	// 2. 启动注入pool -> 后续OnEnable复用连接池。
	return result
}

// LoadEnabled 加载所有已启用规则为完全匹配映射。
// @param ctx：查询上下文。
// @returns keyword到reply的独立映射或查询错误。
// ⚠️副作用说明：查询keyword_reply_rules。
func (r *postgresRepository) LoadEnabled(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT keyword,reply_content FROM keyword_reply_rules WHERE enabled=TRUE`)
	// [决策理由] 查询未建立时无法构造完整快照，必须原样失败。
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var keyword string
		var reply string
		// [决策理由] 任一行扫描失败表示快照不完整，不得发布部分规则。
		if err := rows.Scan(&keyword, &reply); err != nil {
			return nil, err
		}
		result[keyword] = reply
	}
	// [决策理由] 迭代期间的连接或协议错误只有Rows.Err能完整报告。
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// >>> 数据演变示例
	// 1. enabled(a->b),disabled(c->d) -> {a:b}。
	// 2. 空表 -> 空map,nil。
	return result, nil
}

// List 分页读取规则并转换为通用资源记录。
// @param ctx：查询上下文；query：平台校验后的页码和页大小。
// @returns 按ID倒序的资源页或数据库错误。
// ⚠️副作用说明：执行两次只读数据库查询。
func (r *postgresRepository) List(ctx context.Context, query management.ResourceQuery) (management.ResourcePage, error) {
	var total int64
	// [决策理由] 通用分页响应需要精确总数供前端计算页数。
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM keyword_reply_rules`).Scan(&total); err != nil {
		return management.ResourcePage{}, err
	}
	offset := (query.Page - 1) * query.PageSize
	rows, err := r.pool.Query(ctx, `SELECT id,keyword,reply_content,enabled,version FROM keyword_reply_rules ORDER BY id DESC LIMIT $1 OFFSET $2`, query.PageSize, offset)
	// [决策理由] 列表查询失败时不能返回只有total的误导性页面。
	if err != nil {
		return management.ResourcePage{}, err
	}
	defer rows.Close()
	items := make([]management.ResourceRecord, 0)
	for rows.Next() {
		record, err := scanRule(rows)
		// [决策理由] 单行损坏应使整个页面失败，避免管理端编辑错误对象。
		if err != nil {
			return management.ResourcePage{}, err
		}
		items = append(items, record)
	}
	// [决策理由] 迭代错误必须在返回成功页前检查。
	if err := rows.Err(); err != nil {
		return management.ResourcePage{}, err
	}
	result := management.ResourcePage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}

	// >>> 数据演变示例
	// 1. 3行+page1,size2 -> 最新2条,total3。
	// 2. 空表+page1 -> items空,total0。
	return result, nil
}

// Create 在事务内新增规则并写成功审计。
// @param ctx：事务上下文；actor：操作者审计身份；input：已校验完整规则。
// @returns 新记录或唯一冲突及数据库错误。
// ⚠️副作用说明：插入keyword_reply_rules和admin_audit_logs并提交事务。
func (r *postgresRepository) Create(ctx context.Context, actor management.Actor, input ruleInput) (management.ResourceRecord, ruleData, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 无事务时无法保证业务行和审计日志原子提交。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, err
	}
	defer tx.Rollback(ctx)
	var stored storedRule
	stored.Data = ruleData(input)
	err = tx.QueryRow(ctx, `INSERT INTO keyword_reply_rules(keyword,reply_content,enabled) VALUES($1,$2,$3) RETURNING id,version`, input.Keyword, input.ReplyContent, input.Enabled).Scan(&stored.ID, &stored.Version)
	// [决策理由] keyword唯一约束需映射为平台稳定冲突语义。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, mapWriteError(err)
	}
	record, err := stored.resourceRecord()
	// [决策理由] 审计和响应共用同一序列化结果，序列化失败不得提交业务行。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, err
	}
	// [决策理由] 创建审计必须与业务插入位于同一事务。
	if err := insertAudit(ctx, tx, actor, "plugin.resource.create", stored.ID, nil, record.Data); err != nil {
		return management.ResourceRecord{}, ruleData{}, err
	}
	// [决策理由] 只有业务与审计均成功才可对外确认创建。
	if err := tx.Commit(ctx); err != nil {
		return management.ResourceRecord{}, ruleData{}, err
	}

	// >>> 数据演变示例
	// 1. 新keyword=a -> id7,version1+审计 -> 提交并返回。
	// 2. 重复keyword=a -> 23505 -> ErrResourceConflict且事务回滚。
	return record, stored.Data, nil
}

// Update 使用CAS在事务内更新规则并写成功审计。
// @param ctx：事务上下文；actor：操作者；id：规则ID；expectedVersion：期望版本；input：完整规则。
// @returns 递增版本后的记录、未找到、版本冲突或数据库错误。
// ⚠️副作用说明：更新keyword_reply_rules和admin_audit_logs并提交事务。
func (r *postgresRepository) Update(ctx context.Context, actor management.Actor, id int64, expectedVersion int64, input ruleInput) (management.ResourceRecord, ruleData, ruleData, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 更新前像、业务写和审计必须处于同一事务快照。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, err
	}
	defer tx.Rollback(ctx)
	before, err := getRuleForUpdate(ctx, tx, id)
	// [决策理由] 不存在必须与陈旧版本区分，供前端给出准确提示。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, err
	}
	// [决策理由] 先比较锁定行版本可明确拒绝覆盖其他管理员的更新。
	if before.Version != expectedVersion {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, management.ErrResourceConflict
	}
	var after storedRule
	after.Data = ruleData(input)
	err = tx.QueryRow(ctx, `UPDATE keyword_reply_rules SET keyword=$1,reply_content=$2,enabled=$3,version=version+1,updated_at=NOW() WHERE id=$4 AND version=$5 RETURNING id,version`, input.Keyword, input.ReplyContent, input.Enabled, id, expectedVersion).Scan(&after.ID, &after.Version)
	// [决策理由] 唯一约束或极端并发CAS失败必须映射为稳定冲突。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, mapWriteError(err)
	}
	beforeRecord, err := before.resourceRecord()
	// [决策理由] 无法生成可信前像时不得写不完整审计。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, err
	}
	afterRecord, err := after.resourceRecord()
	// [决策理由] 无法生成后像时不得提交业务更新。
	if err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, err
	}
	// [决策理由] 更新审计需同时保留前后业务字段。
	if err := insertAudit(ctx, tx, actor, "plugin.resource.update", id, beforeRecord.Data, afterRecord.Data); err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, err
	}
	// [决策理由] 提交成功是更新对外可见的唯一完成点。
	if err := tx.Commit(ctx); err != nil {
		return management.ResourceRecord{}, ruleData{}, ruleData{}, err
	}

	// >>> 数据演变示例
	// 1. id7 v1+a -> 更新b -> v2+前后审计。
	// 2. id7当前v2+expected1 -> ErrResourceConflict且不更新。
	return afterRecord, before.Data, after.Data, nil
}

// Delete 使用CAS在事务内删除规则并写成功审计。
// @param ctx：事务上下文；actor：操作者；id：规则ID；expectedVersion：期望版本。
// @returns 未找到、版本冲突或数据库错误。
// ⚠️副作用说明：删除keyword_reply_rules并插入admin_audit_logs后提交。
func (r *postgresRepository) Delete(ctx context.Context, actor management.Actor, id int64, expectedVersion int64) (ruleData, error) {
	tx, err := r.pool.Begin(ctx)
	// [决策理由] 删除和审计必须原子提交。
	if err != nil {
		return ruleData{}, err
	}
	defer tx.Rollback(ctx)
	before, err := getRuleForUpdate(ctx, tx, id)
	// [决策理由] 目标不存在应返回稳定未找到错误。
	if err != nil {
		return ruleData{}, err
	}
	// [决策理由] 删除也必须防止旧页面误删他人刚更新的规则。
	if before.Version != expectedVersion {
		return ruleData{}, management.ErrResourceConflict
	}
	commandTag, err := tx.Exec(ctx, `DELETE FROM keyword_reply_rules WHERE id=$1 AND version=$2`, id, expectedVersion)
	// [决策理由] SQL执行故障不能继续写成功审计。
	if err != nil {
		return ruleData{}, err
	}
	// [决策理由] CAS未删除行表示事务内出现异常版本变化，应按冲突处理。
	if commandTag.RowsAffected() != 1 {
		return ruleData{}, management.ErrResourceConflict
	}
	beforeRecord, err := before.resourceRecord()
	// [决策理由] 删除审计必须保留可追溯前像。
	if err != nil {
		return ruleData{}, err
	}
	// [决策理由] 删除成功审计与业务删除必须在同一事务。
	if err := insertAudit(ctx, tx, actor, "plugin.resource.delete", id, beforeRecord.Data, nil); err != nil {
		return ruleData{}, err
	}
	// [决策理由] 仅提交后删除才对外完成。
	if err := tx.Commit(ctx); err != nil {
		return ruleData{}, err
	}

	// >>> 数据演变示例
	// 1. id7 v2+expected2 -> 删除+前像审计 -> nil。
	// 2. id7 v2+expected1 -> ErrResourceConflict -> 保留规则。
	return before.Data, nil
}

// List 委派分页查询给插件自有仓库。
// @param ctx：查询上下文；actor：已授权操作者；query：平台校验后的分页参数。
// @returns 资源页或仓库错误。
// ⚠️副作用说明：查询数据库；actor仅由平台完成授权，本层不重复审计读取。
func (h *resourceHandler) List(ctx context.Context, actor management.Actor, query management.ResourceQuery) (management.ResourcePage, error) {
	result, err := h.repository.List(ctx, query)

	// >>> 数据演变示例
	// 1. page1,size20 -> repository.List -> 资源页。
	// 2. 数据库错误 -> 空页,error。
	return result, err
}

// Create 严格解码、校验并创建规则，提交后增量发布运行快照。
// @param ctx：请求上下文；actor：审计操作者；raw：JSON规则对象。
// @returns 新资源记录或校验、事务错误。
// ⚠️副作用说明：写数据库和审计；成功提交后基于事务后像替换运行快照。
func (h *resourceHandler) Create(ctx context.Context, actor management.Actor, raw json.RawMessage) (management.ResourceRecord, error) {
	input, err := decodeRuleInput(raw)
	// [决策理由] 无效输入不得进入数据库事务。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	record, after, err := h.repository.Create(ctx, actor, input)
	// [决策理由] 持久化失败时数据库没有新状态，无需刷新快照。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	// [决策理由] 使用已提交事务返回的权威后像直接发布，避免二次查询失败造成持久化与运行态分叉。
	h.applyChange(nil, &after)

	// >>> 数据演变示例
	// 1. 合法a->b -> DB提交 -> 后像加入快照 -> 返回新记录。
	// 2. 未知字段 -> 严格解码失败 -> 不访问仓库。
	return record, nil
}

// Update 严格解码、CAS更新规则并按前后像发布运行快照。
// @param ctx：请求上下文；actor：操作者；id：规则ID；expectedVersion：期望版本；raw：完整JSON对象。
// @returns 更新记录或校验、冲突、事务错误。
// ⚠️副作用说明：写数据库和审计；提交后替换运行快照。
func (h *resourceHandler) Update(ctx context.Context, actor management.Actor, id int64, expectedVersion int64, raw json.RawMessage) (management.ResourceRecord, error) {
	input, err := decodeRuleInput(raw)
	// [决策理由] 校验必须先于任何写事务。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	record, before, after, err := h.repository.Update(ctx, actor, id, expectedVersion, input)
	// [决策理由] CAS或事务失败时保持旧运行快照与数据库一致。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	// [决策理由] 前后像可同时处理更名和enabled切换，无需提交后再次访问数据库。
	h.applyChange(&before, &after)

	// >>> 数据演变示例
	// 1. id7 v1更新 -> DB v2 -> 前后像切换快照 -> 返回v2。
	// 2. expectedVersion陈旧 -> conflict -> 不修改快照。
	return record, nil
}

// Delete 使用CAS删除规则并按事务前像更新运行快照。
// @param ctx：请求上下文；actor：操作者；id：规则ID；expectedVersion：期望版本。
// @returns 未找到、冲突或事务错误。
// ⚠️副作用说明：删除数据库行并写审计；提交后替换运行快照。
func (h *resourceHandler) Delete(ctx context.Context, actor management.Actor, id int64, expectedVersion int64) error {
	// [决策理由] 非正ID和版本不可能指向合法业务记录，应在事务前拒绝。
	if id < 1 || expectedVersion < 1 {
		return management.ErrInvalidResourceData
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	// [决策理由] 删除失败时数据库未变化，无需刷新。
	before, err := h.repository.Delete(ctx, actor, id, expectedVersion)
	// [决策理由] 删除失败时数据库未变化，不得修改运行快照。
	if err != nil {
		return err
	}
	// [决策理由] 使用删除事务返回的前像精确移除旧关键词。
	h.applyChange(&before, nil)

	// >>> 数据演变示例
	// 1. id7 v2 -> 删除提交 -> 前像从快照移除 -> nil。
	// 2. id0 -> ErrInvalidResourceData -> 不访问仓库。
	return nil
}

// decodeRuleInput 严格解码并验证完整规则对象。
// @param raw：客户端JSON载荷。
// @returns 规范化输入或ErrInvalidResourceData包装错误。
// ⚠️副作用说明：无。
func decodeRuleInput(raw json.RawMessage) (ruleInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload rulePayload
	// [决策理由] 未知字段、类型错误和非对象JSON均不能进入领域层。
	if err := decoder.Decode(&payload); err != nil {
		return ruleInput{}, fmt.Errorf("%w: %v", management.ErrInvalidResourceData, err)
	}
	// [决策理由] 单个请求只允许一个JSON值，防止尾随载荷绕过签名语义。
	if err := ensureJSONEOF(decoder); err != nil {
		return ruleInput{}, fmt.Errorf("%w: %v", management.ErrInvalidResourceData, err)
	}
	// [决策理由] 关键词空白或过长会造成不可维护规则及无界消息索引。
	if strings.TrimSpace(payload.Keyword) == "" || utf8.RuneCountInString(payload.Keyword) > maxKeywordLength {
		return ruleInput{}, fmt.Errorf("%w: keyword必须为1至%d个字符且不能全为空白", management.ErrInvalidResourceData, maxKeywordLength)
	}
	// [决策理由] 回复空白或过长既无业务意义也可能放大OneBot负载。
	if strings.TrimSpace(payload.ReplyContent) == "" || utf8.RuneCountInString(payload.ReplyContent) > maxReplyLength {
		return ruleInput{}, fmt.Errorf("%w: reply_content必须为1至%d个字符且不能全为空白", management.ErrInvalidResourceData, maxReplyLength)
	}
	enabled := true
	// [决策理由] enabled是可选布尔字段，省略时必须兑现资源Schema声明的默认true。
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	input := ruleInput{Keyword: payload.Keyword, ReplyContent: payload.ReplyContent, Enabled: enabled}

	// >>> 数据演变示例
	// 1. {keyword:" Hi ",reply_content:"ok",enabled:true} -> 保留空格的合法输入。
	// 2. {keyword:"",extra:1} -> 空值或未知字段 -> ErrInvalidResourceData。
	return input, nil
}

// ensureJSONEOF 验证解码器后不存在第二个JSON值。
// @param decoder：已读取首个值的JSON解码器。
// @returns 仅遇到EOF时返回nil，否则返回尾随值错误。
// ⚠️副作用说明：继续读取解码器输入流。
func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	// [决策理由] EOF表示首个对象已完整消耗输入，是唯一合法终态。
	if errors.Is(err, io.EOF) {
		return nil
	}
	// [决策理由] 第二个值或解析错误都代表载荷不是单一JSON对象。
	if err == nil {
		return errors.New("存在多余JSON值")
	}

	// >>> 数据演变示例
	// 1. 单个{} -> EOF -> nil。
	// 2. {}{} -> 解出第二个对象 -> 多余值错误。
	return err
}

// scanRule 扫描一行并生成通用资源记录。
// @param row：提供固定五列的扫描源。
// @returns JSON资源记录或扫描、序列化错误。
// ⚠️副作用说明：读取当前数据库行。
func scanRule(row pgx.Row) (management.ResourceRecord, error) {
	var stored storedRule
	// [决策理由] 固定列必须整体扫描成功才能形成可信记录。
	if err := row.Scan(&stored.ID, &stored.Data.Keyword, &stored.Data.ReplyContent, &stored.Data.Enabled, &stored.Version); err != nil {
		return management.ResourceRecord{}, err
	}
	result, err := stored.resourceRecord()

	// >>> 数据演变示例
	// 1. 7,a,b,true,2 -> ResourceRecord{7,2,JSON}。
	// 2. 列类型错误 -> Scan error。
	return result, err
}

// resourceRecord 将数据库规则转换为管理层通用记录。
// @param 无；接收者包含ID、版本和业务数据。
// @returns JSON序列化后的资源记录。
// ⚠️副作用说明：分配JSON字节。
func (r storedRule) resourceRecord() (management.ResourceRecord, error) {
	raw, err := json.Marshal(r.Data)
	// [决策理由] 虽然当前字段均可序列化，仍保留错误防止未来字段扩展静默提交空数据。
	if err != nil {
		return management.ResourceRecord{}, err
	}
	result := management.ResourceRecord{ID: r.ID, Version: r.Version, Data: raw}

	// >>> 数据演变示例
	// 1. stored{7,v2,a->b} -> record{7,v2,{keyword:a...}}。
	// 2. stored零值 -> record{id0,v0,零值字段JSON}。
	return result, nil
}

// getRuleForUpdate 锁定并读取规则前像。
// @param ctx：事务上下文；tx：当前事务；id：规则ID。
// @returns 锁定记录、ErrResourceRecordNotFound或数据库错误。
// ⚠️副作用说明：执行SELECT FOR UPDATE并持有行锁至事务结束。
func getRuleForUpdate(ctx context.Context, tx pgx.Tx, id int64) (storedRule, error) {
	var stored storedRule
	err := tx.QueryRow(ctx, `SELECT id,keyword,reply_content,enabled,version FROM keyword_reply_rules WHERE id=$1 FOR UPDATE`, id).Scan(&stored.ID, &stored.Data.Keyword, &stored.Data.ReplyContent, &stored.Data.Enabled, &stored.Version)
	// [决策理由] pgx无行必须映射为平台404语义。
	if errors.Is(err, pgx.ErrNoRows) {
		return storedRule{}, management.ErrResourceRecordNotFound
	}
	// [决策理由] 其他数据库错误需保留供统一500链路诊断。
	if err != nil {
		return storedRule{}, err
	}

	// >>> 数据演变示例
	// 1. id7存在 -> 加行锁 -> 返回完整前像。
	// 2. id8不存在 -> pgx.ErrNoRows -> ErrResourceRecordNotFound。
	return stored, nil
}

// mapWriteError 将固定SQL写错误映射为稳定资源错误。
// @param err：pgx写入或扫描错误。
// @returns 唯一约束和CAS空行映射后的错误。
// ⚠️副作用说明：无。
func mapWriteError(err error) error {
	var postgresError *pgconn.PgError
	// [决策理由] PostgreSQL 23505稳定表示keyword唯一键冲突。
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return management.ErrResourceConflict
	}
	// [决策理由] 带RETURNING的CAS写无行表示目标版本不再匹配。
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrResourceConflict
	}

	// >>> 数据演变示例
	// 1. PgError{23505} -> ErrResourceConflict。
	// 2. connection reset -> 保留原错误。
	return err
}

// insertAudit 插入关键词资源成功审计。
// @param ctx：事务上下文；tx：当前事务；actor：操作者；action：动作；id：目标ID；before/after：业务快照。
// @returns JSON或SQL错误。
// ⚠️副作用说明：向admin_audit_logs插入一行，随调用方事务提交或回滚。
func insertAudit(ctx context.Context, tx pgx.Tx, actor management.Actor, action string, id int64, before json.RawMessage, after json.RawMessage) error {
	_, err := tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id) VALUES($1,$2,$3,$4,'keyword_reply_rule',$5,NULLIF($6,'null')::jsonb,NULLIF($7,'null')::jsonb,TRUE,NULLIF($8,''))`, actor.ID, actor.Role, actor.Channel, action, fmt.Sprintf("%d", id), nullableJSON(before), nullableJSON(after), actor.RequestID)

	// >>> 数据演变示例
	// 1. create id7+afterJSON -> 审计before空,after规则。
	// 2. SQL失败 -> error -> 调用方回滚业务事务。
	return err
}

// nullableJSON 将空快照转换为SQL表达式可识别的null文本。
// @param raw：可为空的JSON快照。
// @returns 空值为"null"，否则返回原JSON字符串。
// ⚠️副作用说明：分配字符串。
func nullableJSON(raw json.RawMessage) string {
	// [决策理由] SQL使用NULLIF('null','null')生成真正NULL，避免审计出现JSON null占位。
	if len(raw) == 0 {
		return "null"
	}
	result := string(raw)

	// >>> 数据演变示例
	// 1. nil -> "null" -> SQL NULL。
	// 2. {keyword:a} -> 原JSON字符串。
	return result
}
