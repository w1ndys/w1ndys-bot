// 📌 影响范围：读取群消息与PostgreSQL监控数据；可调用大模型、禁言用户、撤回消息，并注册违禁消息监控插件。
package forbiddenmessagemonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/onebot"
	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
	projectlogger "github.com/w1ndys/w1ndys-bot/pkg/logger"
)

const (
	banDurationSeconds       = 30 * 24 * 60 * 60
	activeMemberDays         = 7
	activeSpeechMinimum      = 2
	maintenanceInterval      = 24 * time.Hour
	maintenanceRetryInterval = time.Minute
	eventMatchWindow         = 31 * 24 * time.Hour
	maxLLMExamples           = 8
	actionTimeout            = 15 * time.Second
)

var manifest = plugin.Manifest{
	Name: pluginName, DisplayName: pluginDisplayName, Description: pluginDescription,
	Priority: pluginPriority, System: false, GroupControllable: true,
}

type implementation struct {
	actions      plugin.ActionAPI
	repository   monitorRepository
	httpClient   *http.Client
	snapshot     atomic.Pointer[runtimeSnapshot]
	offsets      atomic.Pointer[map[string]float64]
	resources    []plugin.AdminResourceRegistration
	transitionMu sync.Mutex
	lifecycleMu  sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	desiredOn    bool
	now          func() time.Time
}

type moderationOutcome struct {
	BanSucceeded      bool `json:"ban_succeeded"`
	HistoryLoaded     bool `json:"history_loaded"`
	WithdrawnCount    int  `json:"withdrawn_count"`
	WithdrawalFailure int  `json:"withdrawal_failure_count"`
}

// Name 返回插件稳定名称。
// @param 无。
// @returns forbidden_message_monitor。
// ⚠️副作用说明：无。
func (p *implementation) Name() string {
	// >>> 数据演变示例
	// 1. 新建实例 -> Name -> forbidden_message_monitor。
	// 2. 实例完成启停 -> Name -> forbidden_message_monitor。
	return pluginName
}

// Handle 处理群消息检测以及管理员解禁、踢出通知。
// @param ctx：事件处理上下文；event：OneBot事件。
// @returns 数据、模型或处置失败错误；安全及无关事件返回nil。
// ⚠️副作用说明：群消息可写计数/审计、调用大模型并执行禁言撤回；管理通知可更新复核状态。
func (p *implementation) Handle(ctx context.Context, event ws.Event) error {
	switch typed := event.(type) {
	case *ws.MessageEvent:
		return p.handleMessage(ctx, typed)
	case *ws.GroupBanNotice:
		return p.handleGroupBanNotice(ctx, typed)
	case *ws.NoticeEvent:
		return p.handleNotice(ctx, typed)
	default:
		return nil
	}

	// >>> 数据演变示例
	// 1. 入站群消息 -> 白名单/规则/评分/LLM -> 放行、人工复核或自动处置。
	// 2. group_ban lift_ban -> 时间窗关联 -> 误判终态与反馈样本。
}

// handleMessage 执行群消息完整分层检测。
// @param ctx：事件上下文；message：入站消息。
// @returns 检测或持久化错误，未违规时nil。
// ⚠️副作用说明：维护有效发言计数；必要时调用模型并执行自动处置。
func (p *implementation) handleMessage(ctx context.Context, message *ws.MessageEvent) error {
	// [决策理由] 仅处理入站群消息，避免私聊和机器人自身message_sent形成循环。
	if message == nil || message.PostType != "message" || message.MessageType != "group" || message.GroupID <= 0 || message.UserID <= 0 {
		return nil
	}
	// [决策理由] 空白消息不包含可审计或可研判的文本。
	if strings.TrimSpace(message.RawMessage) == "" {
		return nil
	}
	now := eventTime(message.Time, p.now())
	// [决策理由] 有效发言必须在白名单短路前计数，才能持续满足七日活跃条件。
	if IsValidSpeech(message.RawMessage) {
		if err := p.repository.IncrementValidSpeech(ctx, message.GroupID, message.UserID, now); err != nil {
			return fmt.Errorf("记录有效发言: %w", err)
		}
	}
	whitelisted, err := p.repository.IsWhitelisted(ctx, message.GroupID, message.UserID)
	// [决策理由] 白名单查询失败时安全失败进入错误链路，不能误将活跃用户送去处罚。
	if err != nil {
		return fmt.Errorf("查询活跃白名单: %w", err)
	}
	// [决策理由] 已满足入群和发言条件的用户直接放行，跳过全部检测成本。
	if whitelisted {
		return nil
	}
	snapshot := p.snapshot.Load()
	// [决策理由] 启用前配置尚未发布时不得使用空引擎误判。
	if snapshot == nil || snapshot.engine == nil {
		return fmt.Errorf("违禁消息检测配置尚未初始化")
	}
	exact := snapshot.engine.CheckExactText(message.RawMessage)
	// [决策理由] 管理员明确配置的硬词属于确定性证据，可直接处置。
	if exact.Blocked {
		return p.moderateAndRecord(ctx, message, now, "precise_rule", nil, strings.Join(exact.Reasons, ","), learnableExactFeatures(exact.Reasons))
	}
	score := snapshot.engine.Score(message.RawMessage)
	// [决策理由] 高分消息无需承担大模型成本，直接按加权证据处置。
	if score.Band == RiskBandHigh {
		riskScore := boundedScore(score.Score)
		return p.moderateAndRecord(ctx, message, now, "weighted_score", &riskScore, "加权关键词评分达到高风险阈值", score.MatchedRisk)
	}
	// [决策理由] 低分消息直接放行，保持成本优先原则。
	if score.Band == RiskBandLow {
		return nil
	}
	return p.handleMediumRisk(ctx, message, now, snapshot, score)

	// >>> 数据演变示例
	// 1. 白名单命中 -> 计数后直接nil，不运行规则。
	// 2. 中分 -> 行为摘要+Few-shot+LLM；未配置或失败则仅登记人工复核。
}

// handleMediumRisk 对边界消息执行大模型研判或安全降级人工复核。
// @param ctx：事件上下文；message/now：消息与时间；snapshot：运行快照；score：本地评分证据。
// @returns 模型、记录或自动处置错误。
// ⚠️副作用说明：可能向外部模型发送消息内容，也可能写人工复核记录或执行处罚。
func (p *implementation) handleMediumRisk(ctx context.Context, message *ws.MessageEvent, now time.Time, snapshot *runtimeSnapshot, score ScoreResult) error {
	// [决策理由] 未配置模型时不能把不确定消息误当安全或违规，应仅进入人工复核。
	if snapshot.evaluator == nil {
		return p.recordManualReview(ctx, message, now, score, "大模型未启用")
	}
	behavior, err := p.repository.BehaviorSummary(ctx, message.GroupID, message.UserID, speechWindowStart(now))
	// [决策理由] 行为摘要不完整时不应提交缺失上下文的模型判断。
	if err != nil {
		return p.recordManualReview(ctx, message, now, score, "读取近期行为失败")
	}
	examples, err := p.repository.RecentExamples(ctx, message.GroupID, message.RawMessage, maxLLMExamples)
	// [决策理由] Few-shot读取失败时降级人工，避免模型在缺少动态反馈时过度处罚。
	if err != nil {
		return p.recordManualReview(ctx, message, now, score, "读取人工案例失败")
	}
	request := LLMEvaluationRequest{Message: message.RawMessage, BehaviorSummary: formatBehaviorSummary(behavior), Examples: make([]LLMExample, 0, len(examples))}
	for _, example := range examples {
		request.Examples = append(request.Examples, LLMExample{Message: example.MessageContent, Violated: example.IsViolation})
	}
	llmContext, cancel := context.WithTimeout(ctx, snapshot.llmTimeout)
	defer cancel()
	result, err := snapshot.evaluator.Evaluate(llmContext, request)
	// [决策理由] 模型不可用或协议漂移时只能转人工，绝不能默认处罚。
	if err != nil {
		return p.recordManualReview(ctx, message, now, score, "大模型研判失败")
	}
	// [决策理由] pass是模型明确安全结论，直接放行。
	if result.SuggestedAction == "pass" {
		return nil
	}
	// [决策理由] manual_review明确要求人工决定，不执行禁言或撤回。
	if result.SuggestedAction == "manual_review" {
		return p.createReviewRecord(ctx, message, now, "llm", &result.TotalScore, result.Reason, result.Violations, moderationOutcome{})
	}
	return p.moderateAndRecord(ctx, message, now, "llm", &result.TotalScore, result.Reason, result.Violations)

	// >>> 数据演变示例
	// 1. 模型Safe/pass -> nil放行。
	// 2. 模型High/block -> 禁言+撤回+pending_review审计。
}

// recordManualReview 为模型不可用的中风险消息创建无自动处罚记录。
// @param ctx/message/now：事件信息；score：本地证据；reason：降级原因。
// @returns 审计持久化错误。
// ⚠️副作用说明：写入待人工复核记录，不禁言或撤回。
func (p *implementation) recordManualReview(ctx context.Context, message *ws.MessageEvent, now time.Time, score ScoreResult, reason string) error {
	riskScore := boundedScore(score.Score)
	result := p.createReviewRecord(ctx, message, now, "weighted_score", &riskScore, reason, score.MatchedRisk, moderationOutcome{})

	// >>> 数据演变示例
	// 1. 中分+LLM关闭 -> pending_review+零处置结果。
	// 2. 数据库失败 -> 返回错误且不处罚用户。
	return result
}

// moderateAndRecord 先预留审计，再执行禁言和历史撤回并写回动作结果。
// @param ctx/message/now：事件；source/score/reason/violations：检测证据。
// @returns 审计预留或结果写回错误；单项OneBot失败记录在action_result中。
// ⚠️副作用说明：有效消息先写审计，再禁言目标用户30天、读取30条群历史并尝试撤回该用户消息。
func (p *implementation) moderateAndRecord(ctx context.Context, message *ws.MessageEvent, now time.Time, source string, score *int, reason string, violations []string) error {
	// [决策理由] 缺少稳定消息ID时无法保证重复事件幂等，只能降级为不处罚的人工复核。
	if message.MessageID <= 0 {
		return p.createReviewRecord(ctx, message, now, source, score, reason+"；缺少消息ID，未执行自动处置", violations, moderationOutcome{})
	}
	messageID := message.MessageID
	reservationID, reserved, err := p.repository.ReserveViolation(ctx, violationCreate{MessageID: &messageID, MessageContent: message.RawMessage, GroupID: message.GroupID, UserID: message.UserID, DetectionSource: source, RiskScore: score, Reason: reason, Violations: violations, MessageTime: now})
	// [决策理由] 数据库不可用时不得执行无法审计的外部处罚。
	if err != nil {
		return fmt.Errorf("预留违禁消息审计: %w", err)
	}
	// [决策理由] 重复消息已由首次处理占有，当前调用直接结束以保证外部动作至多执行一次。
	if !reserved {
		return nil
	}
	outcome := moderationOutcome{}
	banContext, cancelBan := context.WithTimeout(ctx, actionTimeout)
	banErr := p.actions.SetGroupBan(banContext, onebot.SetGroupBanParams{GroupID: strconv.FormatInt(message.GroupID, 10), UserID: strconv.FormatInt(message.UserID, 10), Duration: banDurationSeconds})
	cancelBan()
	// [决策理由] 禁言失败仍需继续撤回和审计，供管理员看到部分失败并人工处理。
	if banErr == nil {
		outcome.BanSucceeded = true
	}
	messageSeq := message.MessageSeq
	// [决策理由] 少数兼容事件缺少序号时可用稳定message_id补取，避免无锚点读取最新页。
	if messageSeq <= 0 {
		messageContext, cancelMessage := context.WithTimeout(ctx, actionTimeout)
		messageInfo, messageErr := p.actions.GetMessage(messageContext, message.MessageID)
		cancelMessage()
		// [决策理由] 仅可信的正序号可作为历史页锚点；补取失败时安全跳过历史撤回。
		if messageErr == nil && messageInfo.MessageSeq > 0 {
			messageSeq = messageInfo.MessageSeq
		}
	}
	var history onebot.GetGroupMessageHistoryResult
	var historyErr error
	// [决策理由] 无可靠序号时禁止读取“当前最新30条”，避免并发消息挤掉违规消息之前的记录。
	if messageSeq <= 0 {
		historyErr = fmt.Errorf("缺少历史消息锚点")
	} else {
		historyContext, cancelHistory := context.WithTimeout(ctx, actionTimeout)
		history, historyErr = p.actions.GetGroupMessageHistory(historyContext, onebot.GetGroupMessageHistoryParams{GroupID: strconv.FormatInt(message.GroupID, 10), MessageSeq: strconv.FormatInt(messageSeq, 10), Count: 30, DisableGetURL: true, ParseMultMsg: true})
		cancelHistory()
	}
	// [决策理由] 历史读取失败不能阻止违规证据落库。
	if historyErr == nil {
		outcome.HistoryLoaded = true
		for _, historical := range history.Messages {
			// [决策理由] 仅撤回锚点时刻之前同一发送者且有有效ID的消息。
			if historical.UserID != message.UserID || historical.MessageID <= 0 || historical.Time > now.Unix() {
				continue
			}
			// [决策理由] 单条已超时或已删除不应中断其余撤回尝试。
			deleteContext, cancelDelete := context.WithTimeout(ctx, actionTimeout)
			deleteErr := p.actions.DeleteMessage(deleteContext, historical.MessageID)
			cancelDelete()
			// [决策理由] 每条撤回使用独立超时，单条迟滞不能耗尽整个事件worker。
			if deleteErr != nil {
				outcome.WithdrawalFailure++
			} else {
				outcome.WithdrawnCount++
			}
		}
	}
	// [决策理由] 外部动作无论部分成功或失败都必须写回已存在审计，便于人工补偿。
	if err := p.repository.CompleteViolationActions(ctx, reservationID, outcome); err != nil {
		return fmt.Errorf("记录违禁消息处置结果: %w", err)
	}
	return nil

	// >>> 数据演变示例
	// 1. 禁言成功+历史中用户3条 -> withdrawn=3 -> 写pending_review。
	// 2. 禁言失败+历史失败 -> outcome均false -> 仍保存证据供人工处理。
}

// createReviewRecord 保存不可变消息证据和处置摘要。
// @param ctx/message/now：事件；source/score/reason/violations/outcome：检测与动作结果。
// @returns 插入错误。
// ⚠️副作用说明：写入违规审计表；不记录外部服务错误文本或密钥。
func (p *implementation) createReviewRecord(ctx context.Context, message *ws.MessageEvent, now time.Time, source string, score *int, reason string, violations []string, outcome moderationOutcome) error {
	actionResult, err := json.Marshal(outcome)
	// [决策理由] 处置摘要无法编码时不能写入不一致证据。
	if err != nil {
		return fmt.Errorf("编码处置结果: %w", err)
	}
	var messageID *int64
	// [决策理由] 无效消息ID必须存NULL，避免(group_id,0)唯一键把不同人工复核消息错误合并。
	if message.MessageID > 0 {
		value := message.MessageID
		messageID = &value
	}
	_, err = p.repository.CreateViolation(ctx, violationCreate{MessageID: messageID, MessageContent: message.RawMessage, GroupID: message.GroupID, UserID: message.UserID, DetectionSource: source, RiskScore: score, Reason: reason, Violations: violations, ActionResult: actionResult, MessageTime: now})
	// [决策理由] 所有自动或人工复核决策都必须有审计记录。
	if err != nil {
		return fmt.Errorf("记录违禁消息审计: %w", err)
	}

	// >>> 数据演变示例
	// 1. message9+High/block -> audit{pending_review,action_result}。
	// 2. 唯一消息重复处理 -> 数据库冲突 -> error。
	return nil
}

// handleGroupBanNotice 将管理员解禁同步为误判终态。
// @param ctx：事件上下文；notice：群禁言通知。
// @returns 状态同步错误或nil。
// ⚠️副作用说明：解禁事件可更新最新关联违规记录、写反馈样本和管理审计。
func (p *implementation) handleGroupBanNotice(ctx context.Context, notice *ws.GroupBanNotice) error {
	// [决策理由] 仅duration=0的有效群用户通知代表解除禁言。
	if notice == nil || notice.Duration != 0 || notice.GroupID <= 0 || notice.UserID <= 0 {
		return nil
	}
	now := eventTime(notice.Time, p.now())
	actor := management.Actor{ID: strconv.FormatInt(notice.OperatorID, 10), Role: "group_operator", Channel: management.ChannelSystem}
	_, err := p.repository.TransitionByEvent(ctx, actor, notice.GroupID, notice.UserID, now.Add(-eventMatchWindow), now.Add(time.Minute), statusFalsePositive)
	// [决策理由] 关联失败需进入统一错误日志，未匹配则仓储返回nil。
	if err != nil {
		return fmt.Errorf("同步群解禁复核状态: %w", err)
	}

	// >>> 数据演变示例
	// 1. group_ban duration0+窗内pending -> false_positive+反馈。
	// 2. ban duration60 -> 忽略，不改变审计状态。
	return nil
}

// handleNotice 处理新成员白名单清理和管理员踢出状态同步。
// @param ctx：事件上下文；notice：通用群通知。
// @returns 状态同步错误或nil。
// ⚠️副作用说明：kick事件可更新最新关联违规记录并写管理审计。
func (p *implementation) handleNotice(ctx context.Context, notice *ws.NoticeEvent) error {
	// [决策理由] 缺少有效群和用户时不能安全关联白名单或违规记录。
	if notice == nil || notice.GroupID <= 0 || notice.UserID <= 0 {
		return nil
	}
	// [决策理由] 入群事件必须立即清除旧白名单，保证重新入群后的第一条消息强制过检。
	if notice.NoticeType == "group_increase" {
		if err := p.repository.RemoveWhitelist(ctx, notice.GroupID, notice.UserID); err != nil {
			return fmt.Errorf("清理新成员白名单: %w", err)
		}
		return nil
	}
	// [决策理由] 只接受管理员踢人子类型，主动退群和机器人被踢不代表违规确认。
	if notice.NoticeType != "group_decrease" || notice.SubType != "kick" {
		return nil
	}
	now := eventTime(notice.Time, p.now())
	actor := management.Actor{ID: strconv.FormatInt(notice.OperatorID, 10), Role: "group_operator", Channel: management.ChannelSystem}
	_, err := p.repository.TransitionByEvent(ctx, actor, notice.GroupID, notice.UserID, now.Add(-eventMatchWindow), now.Add(time.Minute), statusConfirmedKicked)
	// [决策理由] 数据库故障不得被当成无匹配事件忽略。
	if err != nil {
		return fmt.Errorf("同步群踢出复核状态: %w", err)
	}

	// >>> 数据演变示例
	// 1. group_decrease.kick+窗内待踢出 -> confirmed_kicked。
	// 2. group_increase -> 删除旧白名单；group_decrease.leave -> 忽略。
	return nil
}

// AdminResources 声明只允许修改审核结论的违规记录资源。
// @param 无。
// @returns 违规审核资源声明与处理器副本。
// ⚠️副作用说明：无。
func (p *implementation) AdminResources() []plugin.AdminResourceRegistration {
	result := append([]plugin.AdminResourceRegistration(nil), p.resources...)

	// >>> 数据演变示例
	// 1. 新实例 -> [violations]且仅status可编辑。
	// 2. 调用方修改返回切片 -> 内部注册保持不变。
	return result
}

// OnEnable 启动每日白名单与反馈权重刷新。
// @param ctx：首次刷新上下文。
// @returns nil；维护错误由后台记录并在下一周期重试。
// ⚠️副作用说明：启动一个可取消的维护协程，后台访问数据库和NapCat。
func (p *implementation) OnEnable(_ context.Context) error {
	p.transitionMu.Lock()
	defer p.transitionMu.Unlock()
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	p.desiredOn = true
	// [决策理由] 热切换可能重复启用，已有worker时保持幂等。
	if p.cancel != nil {
		return nil
	}
	p.startMaintenanceLocked()

	// >>> 数据演变示例
	// 1. 启用 -> 启动单worker -> 后台立即尝试首次维护。
	// 2. 停用收尾中再次启用 -> desiredOn=true -> 旧代次退出后自动重启。
	return nil
}

// startMaintenanceLocked 启动一个新维护代次。
// @param 无；调用方必须持有lifecycleMu。
// @returns 无。
// ⚠️副作用说明：创建后台上下文并启动维护协程。
func (p *implementation) startMaintenanceLocked() {
	workerContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.cancel = cancel
	p.done = done
	go p.maintenanceLoop(workerContext, done)

	// >>> 数据演变示例
	// 1. cancel=nil -> 创建context/done -> worker立即维护。
	// 2. 旧代次已清理 -> 新done代次替换 -> 可独立取消。
}

// OnDisable 停止每日维护并等待资源释放。
// @param ctx：等待上下文。
// @returns 等待取消错误或nil。
// ⚠️副作用说明：取消后台协程并等待退出；不修改数据库内容。
func (p *implementation) OnDisable(ctx context.Context) error {
	p.transitionMu.Lock()
	defer p.transitionMu.Unlock()
	p.lifecycleMu.Lock()
	p.desiredOn = false
	cancel := p.cancel
	done := p.done
	p.lifecycleMu.Unlock()
	// [决策理由] 从未启用或重复禁用时没有资源需要释放。
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		p.lifecycleMu.Lock()
		// [决策理由] 停用未完成时管理器保持原启用状态，因此旧代次退出后必须自动恢复维护worker。
		p.desiredOn = true
		// [决策理由] worker若恰在截止时刻完成收割，超时分支必须立即补启动，避免启用状态没有维护任务。
		if p.cancel == nil {
			p.startMaintenanceLocked()
		}
		p.lifecycleMu.Unlock()
		return ctx.Err()
	}

	// >>> 数据演变示例
	// 1. worker运行 -> cancel -> done关闭 -> nil。
	// 2. 等待超时 -> 恢复启用意图 -> 旧worker退出后自动启动新代次。
}

// maintenanceLoop 每24小时执行一次维护并遵守取消。
// @param ctx：worker上下文；done：退出通知通道。
// @returns 无。
// ⚠️副作用说明：周期访问数据库和NapCat；错误仅记录不终止后续周期。
func (p *implementation) maintenanceLoop(ctx context.Context, done chan struct{}) {
	defer p.finishMaintenance(done)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			next := maintenanceInterval
			// [决策理由] 启动阶段NapCat可能尚未连接，失败时保持服务可用并缩短重试等待。
			if err := p.runMaintenance(ctx); err != nil {
				projectlogger.Error("违禁消息监控维护失败，将自动重试", "error", err)
				next = maintenanceRetryInterval
			}
			timer.Reset(next)
		}
	}

	// >>> 数据演变示例
	// 1. 启动立即维护失败 -> 记录错误 -> 1分钟后重试。
	// 2. 维护成功 -> 24小时后再运行；disable -> 关闭done退出。
}

// finishMaintenance 收割退出代次并按最新启用意图恢复维护。
// @param done：当前退出代次的完成通道。
// @returns 无。
// ⚠️副作用说明：关闭完成通道、更新生命周期句柄，必要时启动下一代worker。
func (p *implementation) finishMaintenance(done chan struct{}) {
	p.lifecycleMu.Lock()
	// [决策理由] 过期代次不得清理或重启当前已替换的worker。
	if p.done != done {
		p.lifecycleMu.Unlock()
		close(done)
		return
	}
	p.cancel = nil
	p.done = nil
	// [决策理由] 停用失败或并发重新启用时，退出旧代次后必须恢复实际运行状态。
	if p.desiredOn {
		p.startMaintenanceLocked()
	}
	p.lifecycleMu.Unlock()
	close(done)

	// >>> 数据演变示例
	// 1. 正常停用desiredOn=false -> 清空句柄 -> 不重启。
	// 2. 停用超时desiredOn=true -> 清空旧代次 -> 启动新代次。
}

// runMaintenance 刷新所有已观察群白名单并生成下一日权重补丁。
// @param ctx：维护上下文。
// @returns 首个数据库或OneBot错误。
// ⚠️副作用说明：读取群成员/计数/反馈，替换白名单与权重偏移，并原子更新检测引擎。
func (p *implementation) runMaintenance(ctx context.Context) error {
	now := p.now().UTC()
	groups, err := p.repository.ListObservedGroups(ctx)
	// [决策理由] 群集合不完整时不能声称完成全量刷新。
	if err != nil {
		return fmt.Errorf("列出监控群: %w", err)
	}
	for _, groupID := range groups {
		// [决策理由] 任一群刷新失败需保留错误，由下周期重试。
		if err := p.refreshGroupWhitelist(ctx, groupID, now); err != nil {
			return err
		}
	}
	counts, err := p.repository.FeedbackKeywordCounts(ctx, now.AddDate(0, 0, -activeMemberDays))
	// [决策理由] 不完整反馈统计不得覆盖当前周期补丁。
	if err != nil {
		return fmt.Errorf("统计误判关键词: %w", err)
	}
	offsets := make([]weightOffset, 0, len(counts))
	for keyword, count := range counts {
		delta := -math.Min(float64(count)*2, 20)
		offsets = append(offsets, weightOffset{Keyword: keyword, WeightDelta: delta, SampleCount: count})
	}
	cycleStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// [决策理由] 每日补丁只在下一周期前生效，持续误判会因七日统计提高当日偏移。
	if err := p.repository.RefreshWeightOffsets(ctx, cycleStart, cycleStart.AddDate(0, 0, 1), offsets); err != nil {
		return fmt.Errorf("刷新负向权重偏移: %w", err)
	}
	activeOffsets, err := p.repository.ActiveWeightOffsets(ctx, now)
	// [决策理由] 读取失败时保留旧引擎，避免发布无反馈修正的部分状态。
	if err != nil {
		return fmt.Errorf("读取有效权重偏移: %w", err)
	}
	return p.publishWeightOffsets(activeOffsets)

	// >>> 数据演变示例
	// 1. 活跃成员满足7天+2次 -> 新白名单；误判词2次 -> 当日delta=-4。
	// 2. 无群无反馈 -> 空集合刷新 -> 保持基础引擎。
}

// refreshGroupWhitelist 依据成员入群时间和七日发言数替换单群白名单。
// @param ctx：维护上下文；groupID：群号；now：刷新时刻。
// @returns OneBot、计数或事务错误。
// ⚠️副作用说明：读取全群成员和发言计数，原子替换该群白名单。
func (p *implementation) refreshGroupWhitelist(ctx context.Context, groupID int64, now time.Time) error {
	actionContext, cancel := context.WithTimeout(ctx, actionTimeout)
	members, err := p.actions.GetGroupMemberList(actionContext, onebot.GetGroupMemberListParams{GroupID: strconv.FormatInt(groupID, 10), NoCache: true})
	cancel()
	// [决策理由] 成员列表不完整时不得误删现有白名单。
	if err != nil {
		return fmt.Errorf("读取群%d成员: %w", groupID, err)
	}
	counts, err := p.repository.RecentValidCounts(ctx, groupID, speechWindowStart(now))
	// [决策理由] 发言计数失败时不得发布空白名单。
	if err != nil {
		return fmt.Errorf("读取群%d活跃计数: %w", groupID, err)
	}
	joinedBefore := now.AddDate(0, 0, -activeMemberDays).Unix()
	eligible := make([]int64, 0)
	for _, member := range members {
		// [决策理由] 机器人、新成员或七日有效发言不足者不满足双重条件。
		if member.IsRobot || member.UserID <= 0 || member.JoinTime > joinedBefore || counts[member.UserID] < activeSpeechMinimum {
			continue
		}
		eligible = append(eligible, member.UserID)
	}
	// [决策理由] 新用户首条消息不可能满足七日入群条件，因此不会被提前加入。
	if err := p.repository.ReplaceWhitelist(ctx, groupID, eligible, now); err != nil {
		return fmt.Errorf("替换群%d白名单: %w", groupID, err)
	}

	// >>> 数据演变示例
	// 1. join8天+count2 -> eligible -> 白名单保留/加入。
	// 2. join6天或count1 -> 排除 -> 白名单移除。
	return nil
}

// publishWeightOffsets 将反馈偏移叠加到基础配置并原子替换引擎。
// @param offsets：关键词到负向偏移。
// @returns 引擎配置错误。
// ⚠️副作用说明：成功时替换运行快照中的检测引擎，保留LLM客户端与超时。
func (p *implementation) publishWeightOffsets(offsets map[string]float64) error {
	current := p.snapshot.Load()
	// [决策理由] 配置尚未发布时无法确定基础词库。
	if current == nil {
		return fmt.Errorf("检测配置尚未初始化")
	}
	storedOffsets := make(map[string]float64, len(offsets))
	for keyword, delta := range offsets {
		storedOffsets[strings.ToLower(strings.TrimSpace(keyword))] = delta
	}
	config := current.engineConfig
	config.HardKeywords = append([]string(nil), config.HardKeywords...)
	config.WeightedKeywords = append([]WeightedKeyword(nil), config.WeightedKeywords...)
	config.SafeKeywords = append([]WeightedKeyword(nil), config.SafeKeywords...)
	config.Combinations = append([]CombinationRule(nil), config.Combinations...)
	filteredHard := config.HardKeywords[:0]
	for _, keyword := range config.HardKeywords {
		// [决策理由] 被人工确认误报的硬词不再执行零容错拦截，转由后续负向评分处理。
		if _, exists := storedOffsets[strings.ToLower(keyword)]; !exists {
			filteredHard = append(filteredHard, keyword)
		}
	}
	config.HardKeywords = filteredHard
	used := make(map[string]struct{})
	for index := range config.WeightedKeywords {
		key := strings.ToLower(config.WeightedKeywords[index].Text)
		// [决策理由] 仅命中已有风险词时降低其权重且不允许变为负数。
		if delta, exists := storedOffsets[key]; exists {
			config.WeightedKeywords[index].Weight = math.Max(0, config.WeightedKeywords[index].Weight+delta)
			used[key] = struct{}{}
		}
	}
	for feature, delta := range storedOffsets {
		terms := strings.Split(feature, "+")
		// [决策理由] 反馈二元组合必须作为负加成规则生效，不能把带加号的字面值误当安全词。
		if len(terms) == 2 && terms[0] != "" && terms[1] != "" && delta < 0 {
			config.Combinations = append(config.Combinations, CombinationRule{Terms: terms, Bonus: delta})
			used[feature] = struct{}{}
		}
	}
	for keyword, delta := range storedOffsets {
		// [决策理由] 未命中风险词的误判特征作为安全抵扣词叠加，负偏移转换为正抵扣权重。
		if _, exists := used[keyword]; !exists && delta < 0 {
			config.SafeKeywords = append(config.SafeKeywords, WeightedKeyword{Text: keyword, Weight: -delta})
		}
	}
	engine, err := NewEngine(config)
	// [决策理由] 新引擎完整构造成功前必须保留旧快照。
	if err != nil {
		return fmt.Errorf("应用反馈权重: %w", err)
	}
	next := &runtimeSnapshot{engine: engine, engineConfig: current.engineConfig, evaluator: current.evaluator, llmTimeout: current.llmTimeout}
	p.offsets.Store(&storedOffsets)
	p.snapshot.Store(next)

	// >>> 数据演变示例
	// 1. 风险词免费25+delta-4 -> 新引擎weight21。
	// 2. 新特征校内群+delta-2 -> 安全抵扣词2分。
	return nil
}

// newPlugin 使用运行时依赖创建监控实例和默认配置快照。
// @param runtime：主程序注入的Action API与数据库。
// @returns 可配置监控插件或依赖、配置错误。
// ⚠️副作用说明：分配HTTP客户端和仓储，不连接外部服务。
func newPlugin(runtime plugin.Runtime) (plugin.Plugin, error) {
	// [决策理由] 自动处置和白名单刷新必须使用受控OneBot能力。
	if runtime.Actions == nil {
		return nil, fmt.Errorf("%s 缺少 ActionAPI", pluginName)
	}
	// [决策理由] 发言、白名单、审核和反馈均要求持久化，不能以内存替代。
	if runtime.Database == nil {
		return nil, fmt.Errorf("%s 缺少 Database", pluginName)
	}
	repository := newPostgresMonitorRepository(runtime.Database)
	result := &implementation{actions: runtime.Actions, repository: repository, httpClient: &http.Client{}, now: time.Now}
	normalized, err := plugin.NormalizeConfig(result.ConfigSchema(), json.RawMessage(`{}`))
	// [决策理由] 默认Schema无法规范化表示代码和平台配置契约已失配。
	if err != nil {
		return nil, fmt.Errorf("初始化%s默认配置: %w", pluginName, err)
	}
	// [决策理由] Factory必须发布可立即读取的默认引擎，避免启用前nil快照。
	if err := result.ApplyConfig(context.Background(), normalized); err != nil {
		return nil, err
	}
	resource := plugin.AdminResource{
		Key: "violations", DisplayName: "违规消息复核", Description: "查看自动检测证据并选择确认或误报",
		Fields: []plugin.ConfigField{
			{Key: "msg_content", DisplayName: "消息内容", Type: plugin.FieldMultiline},
			{Key: "group_id", DisplayName: "群号", Type: plugin.FieldString},
			{Key: "user_id", DisplayName: "用户QQ", Type: plugin.FieldString},
			{Key: "reason", DisplayName: "判定理由", Type: plugin.FieldMultiline},
			{Key: "status", DisplayName: "审核操作", Description: "确认后等待群内踢出；误报会解除禁言并沉淀反馈", Type: plugin.FieldEnum, Options: []string{"确认", "误报"}},
		},
		ReadOnlyFields: []string{"msg_content", "group_id", "user_id", "reason"}, CanUpdate: true, MaxPageSize: 50,
	}
	testResource := plugin.AdminResource{Key: "text_tests", DisplayName: "文本试判", Description: "使用当前规则测试文本，不会禁言、撤回或写入违规审计", Fields: []plugin.ConfigField{{Key: "text", DisplayName: "测试文本", Type: plugin.FieldMultiline, Required: true}}, CanCreate: true, MaxPageSize: 50}
	result.resources = []plugin.AdminResourceRegistration{{Descriptor: resource, Handler: &violationResourceHandler{repository: repository, actions: runtime.Actions}}, {Descriptor: testResource, Handler: &textTestResourceHandler{owner: result}}}

	// >>> 数据演变示例
	// 1. Runtime{Actions,Database} -> 默认引擎+审核/文本试判资源 -> implementation。
	// 2. 缺Actions或Database -> Factory错误 -> 不注册运行实例。
	return result, nil
}

// eventTime 将OneBot秒级时间转换为UTC并防御零值。
// @param unixSeconds：事件时间；fallback：本地接收时间。
// @returns 事件时间有效时的UTC值，否则fallback UTC值。
// ⚠️副作用说明：无。
func eventTime(unixSeconds int64, fallback time.Time) time.Time {
	// [决策理由] 零或负时间无法作为撤回与事件关联锚点。
	if unixSeconds <= 0 {
		return fallback.UTC()
	}
	result := time.Unix(unixSeconds, 0).UTC()

	// >>> 数据演变示例
	// 1. 1720000000 -> 对应UTC事件时间。
	// 2. 0+fallback -> fallback.UTC。
	return result
}

// speechWindowStart 返回包含今天在内最近七个UTC自然日的起点。
// @param now：当前时刻。
// @returns UTC当天零点向前六日。
// ⚠️副作用说明：无。
func speechWindowStart(now time.Time) time.Time {
	utc := now.UTC()
	today := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	result := today.AddDate(0, 0, -(activeMemberDays - 1))

	// >>> 数据演变示例
	// 1. 7月21日任意时刻 -> 7月15日00:00 -> 共7个日期桶。
	// 2. 跨月7月2日 -> 6月26日00:00。
	return result
}

// boundedScore 将本地浮点评分转换为审计0到100整数。
// @param score：规则引擎原始分数。
// @returns 四舍五入且限制在0到100的整数。
// ⚠️副作用说明：无。
func boundedScore(score float64) int {
	result := int(math.Round(math.Max(0, math.Min(100, score))))

	// >>> 数据演变示例
	// 1. 65.4 -> 65。
	// 2. 120 -> 100。
	return result
}

// learnableExactFeatures 提取可安全用于反馈权重的精准规则词。
// @param reasons：精准层证据列表。
// @returns 去除hard_keyword前缀后的硬词。
// ⚠️副作用说明：无。
func learnableExactFeatures(reasons []string) []string {
	result := make([]string, 0)
	for _, reason := range reasons {
		// [决策理由] 只有具体硬词可被人工误报反馈精确降权。
		if keyword, found := strings.CutPrefix(reason, "hard_keyword:"); found && keyword != "" {
			result = append(result, keyword)
		}
	}

	// >>> 数据演变示例
	// 1. [hard_keyword:校内群] -> [校内群]。
	// 2. [其他内部标识] -> []。
	return result
}

// formatBehaviorSummary 生成不含额外身份数据的模型行为摘要。
// @param summary：七日计数和近期违规聚合。
// @returns 中文摘要文本。
// ⚠️副作用说明：无。
func formatBehaviorSummary(summary behaviorSummary) string {
	last := "无"
	// [决策理由] 没有历史违规时间时不得输出零值时间误导模型。
	if summary.LastMessageTime != nil {
		last = summary.LastMessageTime.UTC().Format(time.RFC3339)
	}
	result := fmt.Sprintf("最近7天有效发言%d次，近期违规记录%d条，最近违规时间%s", summary.ValidSpeechCount, summary.ViolationCount, last)

	// >>> 数据演变示例
	// 1. {3,1,time} -> 包含3次、1条与UTC时间。
	// 2. {0,0,nil} -> 最近违规时间无。
	return result
}

// init 注册群内违禁消息监控插件。
// @param 无。
// @returns 无。
// ⚠️副作用说明：向全局Plugin Catalog注册Manifest与Factory；注册冲突时panic。
func init() {
	plugin.MustRegister(plugin.Registration{Manifest: manifest, Factory: newPlugin})

	// >>> 数据演变示例
	// 1. cmd/bot导入插件包 -> Catalog新增forbidden_message_monitor。
	// 2. 稳定名称重复 -> MustRegister检测冲突 -> panic。
}
