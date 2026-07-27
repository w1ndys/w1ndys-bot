// 📌 影响范围：读写 PostgreSQL 违禁监控词库业务表并在同一事务写入审计；不访问外部模型。
package forbiddenmessagemonitor

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
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

var (
	// ErrLexiconNotFound 表示词条或组合规则不存在。
	ErrLexiconNotFound = errors.New("违禁词库记录不存在")
	// ErrLexiconConflict 表示词条重复或乐观锁版本陈旧。
	ErrLexiconConflict = errors.New("违禁词库记录冲突")
	// ErrInvalidLexicon 表示词库输入未通过领域校验。
	ErrInvalidLexicon = errors.New("违禁词库输入无效")
)

// 词条分类：hard 为零误报硬拦截，risk 为加分风险词，safe 为抵扣安全词。
const (
	TermKindHard = "hard"
	TermKindRisk = "risk"
	TermKindSafe = "safe"
)

const (
	maxTermRunes       = 100
	maxTermWeight      = 1000
	maxCombinationSize = 8
	maxLexiconPageSize = 200
)

// Term 是一条词库词条的管理视图。
type Term struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	Weight    float64   `json:"weight"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Combination 是一条组合加成规则的管理视图。
type Combination struct {
	ID        int64     `json:"id"`
	Terms     []string  `json:"terms"`
	Bonus     float64   `json:"bonus"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TermInput 是新增或更新词条的领域输入。
type TermInput struct {
	Kind   string  `json:"kind"`
	Text   string  `json:"text"`
	Weight float64 `json:"weight"`
}

// CombinationInput 是新增或更新组合规则的领域输入。
type CombinationInput struct {
	Terms []string `json:"terms"`
	Bonus float64  `json:"bonus"`
}

// Lexicon 是发布到检测引擎的完整词库快照。
type Lexicon struct {
	Hard         []string
	Risk         []WeightedKeyword
	Safe         []WeightedKeyword
	Combinations []CombinationRule
}

// lexiconLoader 是运行时重建检测引擎所需的最小词库读取能力。
type lexiconLoader interface {
	Load(context.Context) (Lexicon, error)
}

type lexiconRepository struct {
	pool *pgxpool.Pool
}

func newLexiconRepository(pool *pgxpool.Pool) (*lexiconRepository, error) {
	if pool == nil {
		return nil, errors.New("违禁词库数据库连接池不能为空")
	}
	return &lexiconRepository{pool: pool}, nil
}

// Load 读取完整词库，供生命周期与写入后重建检测引擎。
func (r *lexiconRepository) Load(ctx context.Context) (Lexicon, error) {
	lexicon := Lexicon{Hard: []string{}, Risk: []WeightedKeyword{}, Safe: []WeightedKeyword{}, Combinations: []CombinationRule{}}
	rows, err := r.pool.Query(ctx, `SELECT kind,text,weight FROM forbidden_monitor_terms ORDER BY kind,id`)
	if err != nil {
		return Lexicon{}, fmt.Errorf("查询违禁词条: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, text string
		var weight float64
		if err := rows.Scan(&kind, &text, &weight); err != nil {
			return Lexicon{}, fmt.Errorf("扫描违禁词条: %w", err)
		}
		switch kind {
		case TermKindHard:
			lexicon.Hard = append(lexicon.Hard, text)
		case TermKindRisk:
			lexicon.Risk = append(lexicon.Risk, WeightedKeyword{Text: text, Weight: weight})
		case TermKindSafe:
			lexicon.Safe = append(lexicon.Safe, WeightedKeyword{Text: text, Weight: weight})
		}
	}
	if err := rows.Err(); err != nil {
		return Lexicon{}, fmt.Errorf("遍历违禁词条: %w", err)
	}

	combinationRows, err := r.pool.Query(ctx, `SELECT terms,bonus FROM forbidden_monitor_combinations ORDER BY id`)
	if err != nil {
		return Lexicon{}, fmt.Errorf("查询组合加成: %w", err)
	}
	defer combinationRows.Close()
	for combinationRows.Next() {
		var terms []string
		var bonus float64
		if err := combinationRows.Scan(&terms, &bonus); err != nil {
			return Lexicon{}, fmt.Errorf("扫描组合加成: %w", err)
		}
		lexicon.Combinations = append(lexicon.Combinations, CombinationRule{Terms: terms, Bonus: bonus})
	}
	if err := combinationRows.Err(); err != nil {
		return Lexicon{}, fmt.Errorf("遍历组合加成: %w", err)
	}
	return lexicon, nil
}

// ListTerms 按分类分页读取词条；kind 为空表示全部分类。
func (r *lexiconRepository) ListTerms(ctx context.Context, kind string, page, pageSize int) ([]Term, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM forbidden_monitor_terms WHERE ($1='' OR kind=$1)`, kind).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计违禁词条: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT id,kind,text,weight,version,updated_at FROM forbidden_monitor_terms
WHERE ($1='' OR kind=$1) ORDER BY kind,id LIMIT $2 OFFSET $3`, kind, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("查询违禁词条: %w", err)
	}
	defer rows.Close()
	items := make([]Term, 0)
	for rows.Next() {
		var term Term
		if err := rows.Scan(&term.ID, &term.Kind, &term.Text, &term.Weight, &term.Version, &term.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("扫描违禁词条: %w", err)
		}
		term.UpdatedAt = term.UpdatedAt.UTC()
		items = append(items, term)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历违禁词条: %w", err)
	}
	return items, total, nil
}

// CreateTerm 新增词条并写入审计。
func (r *lexiconRepository) CreateTerm(ctx context.Context, actor management.Actor, input TermInput) (Term, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Term{}, fmt.Errorf("开启违禁词条事务: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	var term Term
	err = tx.QueryRow(ctx, `INSERT INTO forbidden_monitor_terms(kind,text,weight)
VALUES($1,$2,$3) RETURNING id,kind,text,weight,version,updated_at`, input.Kind, input.Text, input.Weight).
		Scan(&term.ID, &term.Kind, &term.Text, &term.Weight, &term.Version, &term.UpdatedAt)
	if err != nil {
		return Term{}, mapLexiconError(err)
	}
	if err := recordLexiconAudit(ctx, tx, actor, "plugin.forbidden_monitor.term.create", "forbidden_monitor_term", term.ID, nil, term); err != nil {
		return Term{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Term{}, fmt.Errorf("提交违禁词条事务: %w", err)
	}
	term.UpdatedAt = term.UpdatedAt.UTC()
	return term, nil
}

// UpdateTerm 按乐观锁更新词条并写入审计。
func (r *lexiconRepository) UpdateTerm(ctx context.Context, actor management.Actor, id, expectedVersion int64, input TermInput) (Term, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Term{}, fmt.Errorf("开启违禁词条事务: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	var before Term
	err = tx.QueryRow(ctx, `SELECT id,kind,text,weight,version,updated_at FROM forbidden_monitor_terms WHERE id=$1 FOR UPDATE`, id).
		Scan(&before.ID, &before.Kind, &before.Text, &before.Weight, &before.Version, &before.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, fmt.Errorf("%w: 词条 %d", ErrLexiconNotFound, id)
	}
	if err != nil {
		return Term{}, fmt.Errorf("锁定违禁词条: %w", err)
	}
	if before.Version != expectedVersion {
		return Term{}, ErrLexiconConflict
	}
	var term Term
	err = tx.QueryRow(ctx, `UPDATE forbidden_monitor_terms
SET kind=$3,text=$4,weight=$5,version=version+1,updated_at=NOW()
WHERE id=$1 AND version=$2 RETURNING id,kind,text,weight,version,updated_at`, id, expectedVersion, input.Kind, input.Text, input.Weight).
		Scan(&term.ID, &term.Kind, &term.Text, &term.Weight, &term.Version, &term.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, ErrLexiconConflict
	}
	if err != nil {
		return Term{}, mapLexiconError(err)
	}
	if err := recordLexiconAudit(ctx, tx, actor, "plugin.forbidden_monitor.term.update", "forbidden_monitor_term", id, before, term); err != nil {
		return Term{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Term{}, fmt.Errorf("提交违禁词条事务: %w", err)
	}
	term.UpdatedAt = term.UpdatedAt.UTC()
	return term, nil
}

// DeleteTerm 按乐观锁删除词条并写入审计。
func (r *lexiconRepository) DeleteTerm(ctx context.Context, actor management.Actor, id, expectedVersion int64) error {
	return r.deleteRecord(ctx, actor, "forbidden_monitor_terms", "plugin.forbidden_monitor.term.delete", "forbidden_monitor_term", id, expectedVersion)
}

// ListCombinations 分页读取组合加成规则。
func (r *lexiconRepository) ListCombinations(ctx context.Context, page, pageSize int) ([]Combination, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM forbidden_monitor_combinations`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计组合加成: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT id,terms,bonus,version,updated_at FROM forbidden_monitor_combinations
ORDER BY id LIMIT $1 OFFSET $2`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("查询组合加成: %w", err)
	}
	defer rows.Close()
	items := make([]Combination, 0)
	for rows.Next() {
		var combination Combination
		if err := rows.Scan(&combination.ID, &combination.Terms, &combination.Bonus, &combination.Version, &combination.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("扫描组合加成: %w", err)
		}
		combination.UpdatedAt = combination.UpdatedAt.UTC()
		items = append(items, combination)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历组合加成: %w", err)
	}
	return items, total, nil
}

// CreateCombination 新增组合规则并写入审计。
func (r *lexiconRepository) CreateCombination(ctx context.Context, actor management.Actor, input CombinationInput) (Combination, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Combination{}, fmt.Errorf("开启组合加成事务: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	var combination Combination
	err = tx.QueryRow(ctx, `INSERT INTO forbidden_monitor_combinations(terms,bonus)
VALUES($1,$2) RETURNING id,terms,bonus,version,updated_at`, input.Terms, input.Bonus).
		Scan(&combination.ID, &combination.Terms, &combination.Bonus, &combination.Version, &combination.UpdatedAt)
	if err != nil {
		return Combination{}, mapLexiconError(err)
	}
	if err := recordLexiconAudit(ctx, tx, actor, "plugin.forbidden_monitor.combination.create", "forbidden_monitor_combination", combination.ID, nil, combination); err != nil {
		return Combination{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Combination{}, fmt.Errorf("提交组合加成事务: %w", err)
	}
	combination.UpdatedAt = combination.UpdatedAt.UTC()
	return combination, nil
}

// DeleteCombination 按乐观锁删除组合规则并写入审计。
func (r *lexiconRepository) DeleteCombination(ctx context.Context, actor management.Actor, id, expectedVersion int64) error {
	return r.deleteRecord(ctx, actor, "forbidden_monitor_combinations", "plugin.forbidden_monitor.combination.delete", "forbidden_monitor_combination", id, expectedVersion)
}

// deleteRecord 按乐观锁删除词库记录；表名来自包内常量，不接受调用方输入。
func (r *lexiconRepository) deleteRecord(ctx context.Context, actor management.Actor, table, action, targetType string, id, expectedVersion int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("开启 %s 删除事务: %w", targetType, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	var currentVersion int64
	// 表名是包内固定字符串，不来自客户端输入，因此可以安全拼接。
	err = tx.QueryRow(ctx, `SELECT version FROM `+table+` WHERE id=$1 FOR UPDATE`, id).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s %d", ErrLexiconNotFound, targetType, id)
	}
	if err != nil {
		return fmt.Errorf("锁定 %s: %w", targetType, err)
	}
	if currentVersion != expectedVersion {
		return ErrLexiconConflict
	}
	tag, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE id=$1 AND version=$2`, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("删除 %s: %w", targetType, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLexiconConflict
	}
	if err := recordLexiconAudit(ctx, tx, actor, action, targetType, id, map[string]any{"id": id, "version": expectedVersion}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交 %s 删除事务: %w", targetType, err)
	}
	return nil
}

func mapLexiconError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ErrLexiconConflict
		case "23514":
			return fmt.Errorf("%w: 违反数据库约束 %s", ErrInvalidLexicon, postgresError.ConstraintName)
		}
	}
	return fmt.Errorf("保存违禁词库记录: %w", err)
}

// recordLexiconAudit 在词库写入所在事务追加审计；before 或 after 为 nil 表示新增或删除。
func recordLexiconAudit(ctx context.Context, tx pgx.Tx, actor management.Actor, action, targetType string, id int64, before, after any) error {
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
	_, err = tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_id,actor_role,channel,action,target_type,target_id,before_json,after_json,success,request_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,TRUE,NULLIF($9,''))`,
		actor.ID, actor.Role, actor.Channel, action, targetType, fmt.Sprintf("%d", id), beforeJSON, afterJSON, actor.RequestID)
	// 审计失败必须回滚词库变更，避免检测规则被无法追溯地修改。
	if err != nil {
		return fmt.Errorf("写入 %s 审计: %w", action, err)
	}
	return nil
}

// currentConfigJSON 返回最近一次成功应用的配置；尚未应用时回退到 Schema 默认值。
func (p *implementation) currentConfigJSON() json.RawMessage {
	current := p.configJSON.Load()
	if current != nil {
		return *current
	}
	// 平台会在插件进入 Ready 前应用配置；此处回退保证直接构造的实例也能得到可运行阈值。
	defaults, err := plugin.NormalizeConfig(p.ConfigSchema(), json.RawMessage(`{}`))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return defaults
}

// currentLexicon 返回当前已发布的词库快照；尚未加载时返回空词库。
func (p *implementation) currentLexicon() Lexicon {
	current := p.lexicon.Load()
	if current == nil {
		return Lexicon{Hard: []string{}, Risk: []WeightedKeyword{}, Safe: []WeightedKeyword{}, Combinations: []CombinationRule{}}
	}
	return *current
}

// reloadLexicon 从业务表重建词库并按当前配置重新发布检测引擎。
// 读取或构造失败时保留旧词库与旧引擎，避免部分词库静默失效。
func (p *implementation) reloadLexicon(ctx context.Context) error {
	p.publicationMu.Lock()
	defer p.publicationMu.Unlock()
	raw := p.currentConfigJSON()
	if p.lexiconStore == nil {
		return errors.New("违禁词库仓库未初始化")
	}
	loaded, err := p.lexiconStore.Load(ctx)
	if err != nil {
		return err
	}
	next, err := buildRuntimeSnapshot(raw, loaded, p.httpClient)
	if err != nil {
		return fmt.Errorf("按新词库重建检测引擎: %w", err)
	}
	if currentOffsets := p.offsets.Load(); currentOffsets != nil {
		negativeFeatures := map[string]struct{}{}
		if currentNegative := p.negative.Load(); currentNegative != nil {
			negativeFeatures = *currentNegative
		}
		adjusted, _, _, buildErr := buildSnapshotWithWeightOffsets(next, *currentOffsets, negativeFeatures)
		if buildErr != nil {
			return fmt.Errorf("按反馈偏移重建检测引擎: %w", buildErr)
		}
		next = adjusted
	}
	p.lexicon.Store(&loaded)
	p.snapshot.Store(next)
	return nil
}
