// 📌 影响范围：读写 PostgreSQL 关键词规则表并在同一事务写入审计；所有查询与写入按可信群号隔离。
package keywordreply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

var (
	// ErrRuleNotFound 表示目标群下不存在该规则。
	ErrRuleNotFound = errors.New("关键词规则不存在")
	// ErrRuleConflict 表示同群关键词重复或乐观锁版本陈旧。
	ErrRuleConflict = errors.New("关键词规则冲突")
)

// Rule 是一条群关键词回复规则的管理视图。
type Rule struct {
	ID           int64     `json:"id"`
	GroupID      int64     `json:"group_id"`
	Keyword      string    `json:"keyword"`
	ReplyContent string    `json:"reply_content"`
	Enabled      bool      `json:"enabled"`
	Version      int64     `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RulePage 是按群分页的规则列表。
type RulePage struct {
	Items    []Rule `json:"items"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Total    int64  `json:"total"`
}

// RuleInput 是新增或更新规则的领域输入。
type RuleInput struct {
	Keyword      string `json:"keyword"`
	ReplyContent string `json:"reply_content"`
	Enabled      bool   `json:"enabled"`
}

// ruleAudit 是规则变更写入审计的有界前后快照。
type ruleAudit struct {
	ID           int64  `json:"id"`
	GroupID      int64  `json:"group_id"`
	Keyword      string `json:"keyword"`
	ReplyContent string `json:"reply_content"`
	Enabled      bool   `json:"enabled"`
	Version      int64  `json:"version"`
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

func newPostgresRepository(pool *pgxpool.Pool) (*postgresRepository, error) {
	if pool == nil {
		return nil, errors.New("关键词规则数据库连接池不能为空")
	}
	return &postgresRepository{pool: pool}, nil
}

// LoadEnabled 读取全部群的已启用规则，供生命周期发布运行快照。
func (r *postgresRepository) LoadEnabled(ctx context.Context) (map[int64]map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT group_id,keyword,reply_content FROM keyword_reply_rules WHERE enabled ORDER BY group_id,id`)
	if err != nil {
		return nil, fmt.Errorf("查询已启用关键词规则: %w", err)
	}
	defer rows.Close()

	snapshot := make(map[int64]map[string]string)
	for rows.Next() {
		var groupID int64
		var keyword, reply string
		if err := rows.Scan(&groupID, &keyword, &reply); err != nil {
			return nil, fmt.Errorf("扫描关键词规则: %w", err)
		}
		group, found := snapshot[groupID]
		if !found {
			group = make(map[string]string)
			snapshot[groupID] = group
		}
		group[keyword] = reply
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历关键词规则: %w", err)
	}
	return snapshot, nil
}

// List 按可信群号分页读取规则。
func (r *postgresRepository) List(ctx context.Context, groupID int64, page, pageSize int) (RulePage, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM keyword_reply_rules WHERE group_id=$1`, groupID).Scan(&total); err != nil {
		return RulePage{}, fmt.Errorf("统计群 %d 关键词规则: %w", groupID, err)
	}
	offset := (page - 1) * pageSize
	rows, err := r.pool.Query(ctx, `SELECT id,group_id,keyword,reply_content,enabled,version,updated_at
FROM keyword_reply_rules WHERE group_id=$1 ORDER BY id LIMIT $2 OFFSET $3`, groupID, pageSize, offset)
	if err != nil {
		return RulePage{}, fmt.Errorf("查询群 %d 关键词规则: %w", groupID, err)
	}
	defer rows.Close()

	items := make([]Rule, 0)
	for rows.Next() {
		var rule Rule
		if err := rows.Scan(&rule.ID, &rule.GroupID, &rule.Keyword, &rule.ReplyContent, &rule.Enabled, &rule.Version, &rule.UpdatedAt); err != nil {
			return RulePage{}, fmt.Errorf("扫描群 %d 关键词规则: %w", groupID, err)
		}
		rule.UpdatedAt = rule.UpdatedAt.UTC()
		items = append(items, rule)
	}
	if err := rows.Err(); err != nil {
		return RulePage{}, fmt.Errorf("遍历群 %d 关键词规则: %w", groupID, err)
	}
	return RulePage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

// Create 在指定群下新增规则并写入审计。
func (r *postgresRepository) Create(ctx context.Context, actor management.Actor, groupID int64, input RuleInput) (Rule, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Rule{}, fmt.Errorf("开启关键词规则事务: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	var rule Rule
	err = tx.QueryRow(ctx, `INSERT INTO keyword_reply_rules(group_id,keyword,reply_content,enabled)
VALUES($1,$2,$3,$4)
RETURNING id,group_id,keyword,reply_content,enabled,version,updated_at`, groupID, input.Keyword, input.ReplyContent, input.Enabled).
		Scan(&rule.ID, &rule.GroupID, &rule.Keyword, &rule.ReplyContent, &rule.Enabled, &rule.Version, &rule.UpdatedAt)
	if err != nil {
		return Rule{}, mapWriteError(err)
	}
	if err := recordAudit(ctx, tx, actor, "plugin.keyword_reply.rule.create", groupID, rule.ID, nil, auditOf(rule)); err != nil {
		return Rule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("提交关键词规则事务: %w", err)
	}
	rule.UpdatedAt = rule.UpdatedAt.UTC()
	return rule, nil
}

// Update 按乐观锁更新指定群下的规则并写入审计。
func (r *postgresRepository) Update(ctx context.Context, actor management.Actor, groupID, id, expectedVersion int64, input RuleInput) (Rule, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Rule{}, fmt.Errorf("开启关键词规则事务: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	// 锁行时带上群号，避免跨群通过 ID 猜测改写他群规则。
	before, err := lockRule(ctx, tx, groupID, id)
	if err != nil {
		return Rule{}, err
	}
	if before.Version != expectedVersion {
		return Rule{}, ErrRuleConflict
	}
	var rule Rule
	err = tx.QueryRow(ctx, `UPDATE keyword_reply_rules
SET keyword=$4,reply_content=$5,enabled=$6,version=version+1,updated_at=NOW()
WHERE group_id=$1 AND id=$2 AND version=$3
RETURNING id,group_id,keyword,reply_content,enabled,version,updated_at`, groupID, id, expectedVersion, input.Keyword, input.ReplyContent, input.Enabled).
		Scan(&rule.ID, &rule.GroupID, &rule.Keyword, &rule.ReplyContent, &rule.Enabled, &rule.Version, &rule.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, ErrRuleConflict
	}
	if err != nil {
		return Rule{}, mapWriteError(err)
	}
	if err := recordAudit(ctx, tx, actor, "plugin.keyword_reply.rule.update", groupID, id, auditOf(before), auditOf(rule)); err != nil {
		return Rule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("提交关键词规则事务: %w", err)
	}
	rule.UpdatedAt = rule.UpdatedAt.UTC()
	return rule, nil
}

// Delete 按乐观锁删除指定群下的规则并写入审计。
func (r *postgresRepository) Delete(ctx context.Context, actor management.Actor, groupID, id, expectedVersion int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("开启关键词规则事务: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	before, err := lockRule(ctx, tx, groupID, id)
	if err != nil {
		return err
	}
	if before.Version != expectedVersion {
		return ErrRuleConflict
	}
	tag, err := tx.Exec(ctx, `DELETE FROM keyword_reply_rules WHERE group_id=$1 AND id=$2 AND version=$3`, groupID, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("删除关键词规则: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrRuleConflict
	}
	if err := recordAudit(ctx, tx, actor, "plugin.keyword_reply.rule.delete", groupID, id, auditOf(before), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交关键词规则事务: %w", err)
	}
	return nil
}

func lockRule(ctx context.Context, tx pgx.Tx, groupID, id int64) (Rule, error) {
	var rule Rule
	err := tx.QueryRow(ctx, `SELECT id,group_id,keyword,reply_content,enabled,version,updated_at
FROM keyword_reply_rules WHERE group_id=$1 AND id=$2 FOR UPDATE`, groupID, id).
		Scan(&rule.ID, &rule.GroupID, &rule.Keyword, &rule.ReplyContent, &rule.Enabled, &rule.Version, &rule.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, fmt.Errorf("%w: 群 %d 规则 %d", ErrRuleNotFound, groupID, id)
	}
	if err != nil {
		return Rule{}, fmt.Errorf("锁定关键词规则: %w", err)
	}
	return rule, nil
}

func auditOf(rule Rule) any {
	return ruleAudit{
		ID: rule.ID, GroupID: rule.GroupID, Keyword: rule.Keyword,
		ReplyContent: rule.ReplyContent, Enabled: rule.Enabled, Version: rule.Version,
	}
}

func mapWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		// 同群关键词唯一约束冲突属于可预期业务拒绝。
		case "23505":
			return ErrRuleConflict
		// 长度与群号 CHECK 由数据库兜底，客户端应修正输入。
		case "23514":
			return fmt.Errorf("%w: 违反数据库约束 %s", ErrInvalidRule, postgresError.ConstraintName)
		}
	}
	return fmt.Errorf("保存关键词规则: %w", err)
}

// recordAudit 在规则写入所在事务追加审计；before 或 after 为 nil 表示新增或删除。
func recordAudit(ctx context.Context, tx pgx.Tx, actor management.Actor, action string, groupID, id int64, before, after any) error {
	encode := func(value any) ([]byte, error) {
		if value == nil {
			return nil, nil
		}
		return json.Marshal(value)
	}
	beforeJSON, err := encode(before)
	if err != nil {
		return fmt.Errorf("序列化 %s 审计旧值: %w", action, err)
	}
	afterJSON, err := encode(after)
	if err != nil {
		return fmt.Errorf("序列化 %s 审计新值: %w", action, err)
	}
	target := fmt.Sprintf("%d:%d", groupID, id)
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id)
VALUES($1,$2,$3,$4,'keyword_reply_rule',$5,$6,$7,TRUE,NULLIF($8,''))`,
		actor.ID, actor.Role, actor.Channel, action, target, beforeJSON, afterJSON, actor.RequestID)
	// 审计失败必须回滚规则变更，避免出现无法追溯的业务写入。
	if err != nil {
		return fmt.Errorf("写入 %s 审计: %w", action, err)
	}
	return nil
}
