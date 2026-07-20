// 📌 影响范围：调用群控制 Repository；成功写入后热刷新插件运行快照，失败时补偿。
package admin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

var positiveGroupIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

// validGroupID 校验 QQ 群号可无损存入 PostgreSQL BIGINT。
// @param value：手工输入的十进制群号。
// @returns 格式正确、可解析为正 int64 时 true。
// ⚠️副作用说明：无。
func validGroupID(value string) bool {
	// [决策理由] 先限制字符集和长度，再使用 ParseInt 防止超过 BIGINT 上限进入数据库变成 500。
	if !positiveGroupIDPattern.MatchString(value) {
		return false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	valid := err == nil && parsed > 0

	// >>> 数据演变示例
	// 1. "123456" -> ParseInt -> true。
	// 2. "99999999999999999999" -> overflow -> false。
	return valid
}

// GroupControlRepository 定义可选群控制持久化能力。
type GroupControlRepository interface {
	GetPluginGroupControl(context.Context, string) (PluginGroupControlState, error)
	SetPluginGroupDefault(context.Context, Actor, string, bool, int64) (PluginGroupControlState, error)
	SetPluginGroupOverride(context.Context, Actor, string, string, bool, int64) (PluginGroupOverride, error)
	DeletePluginGroupOverride(context.Context, Actor, string, string, int64) error
}

// GroupControlFailureAuditor 定义热刷新失败的固定脱敏审计能力。
type GroupControlFailureAuditor interface {
	RecordPluginGroupRefreshFailure(context.Context, Actor, string) error
}

// GetPluginGroupControl 返回插件群控制快照。
// @param ctx：请求上下文；actor：操作者；name：插件名。
// @returns 群控制快照或授权、能力、仓库错误。
// ⚠️副作用说明：读取管理员快照与 PostgreSQL。
func (s *Service) GetPluginGroupControl(ctx context.Context, actor Actor, name string) (PluginGroupControlState, error) {
	// [决策理由] 群策略是管理信息，读取也必须以服务端快照鉴权。
	if err := s.authorize(actor); err != nil {
		return PluginGroupControlState{}, err
	}
	repository, ok := s.repository.(GroupControlRepository)
	// [决策理由] 旧仓库实现不可伪造群控制快照。
	if !ok {
		return PluginGroupControlState{}, ErrGroupControlNotSupported
	}
	state, err := repository.GetPluginGroupControl(ctx, name)
	// [决策理由] 查询失败时不返回部分默认状态。
	if err != nil {
		return PluginGroupControlState{}, err
	}

	// >>> 数据演变示例
	// 1. 管理员+keyword_reply -> default+覆盖列表。
	// 2. 非管理员 -> ErrForbidden。
	return state, nil
}

// SetPluginGroupDefault 修改群默认策略并热刷新。
// @param ctx：请求上下文；actor：操作者；name：插件名；enabled：目标值；version：期望版本。
// @returns 新快照或授权、冲突、刷新/补偿错误。
// ⚠️副作用说明：写入策略和审计；刷新失败时 CAS 写回旧值并再刷新。
func (s *Service) SetPluginGroupDefault(ctx context.Context, actor Actor, name string, enabled bool, version int64) (PluginGroupControlState, error) {
	// [决策理由] 写操作必须先授权并校验正版本。
	if err := s.authorize(actor); err != nil || version <= 0 {
		// [决策理由] 授权错误比参数错误优先返回，保留准确的安全语义。
		if err != nil {
			return PluginGroupControlState{}, err
		}
		return PluginGroupControlState{}, ErrInvalidGroupControl
	}
	repository, ok := s.repository.(GroupControlRepository)
	// [决策理由] 缺失持久化能力时不能只更新运行态。
	if !ok {
		return PluginGroupControlState{}, ErrGroupControlNotSupported
	}
	// [决策理由] ReloadGroupGate 每次发布所有插件的全量快照，因此写前读取、写入、发布与补偿必须跨插件全局串行。
	s.groupControlMu.Lock()
	defer s.groupControlMu.Unlock()
	before, err := repository.GetPluginGroupControl(ctx, name)
	// [决策理由] 补偿需要完整旧快照。
	if err != nil {
		return PluginGroupControlState{}, err
	}
	after, err := repository.SetPluginGroupDefault(ctx, actor, name, enabled, version)
	// [决策理由] 持久化失败时运行态未变，无需刷新。
	if err != nil {
		return PluginGroupControlState{}, err
	}
	after.Overrides = append([]PluginGroupOverride(nil), before.Overrides...)
	// [决策理由] 群 gate 由 Manager Load 从数据库重建不可变快照。
	if err := s.reloadGroupControl(ctx); err != nil {
		refreshErr := fmt.Errorf("刷新插件 %s 群控制: %w", name, err)
		failureAuditErr := s.recordGroupControlRefreshFailure(ctx, actor, name)
		databaseContext, cancelDatabase := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
		rollback, rollbackErr := repository.SetPluginGroupDefault(databaseContext, actor, name, before.DefaultEnabled, after.DefaultVersion)
		cancelDatabase()
		// [决策理由] 数据库补偿失败时必须保留双重根因。
		if rollbackErr != nil {
			return after, errors.Join(refreshErr, failureAuditErr, fmt.Errorf("补偿群默认策略: %w", rollbackErr))
		}
		// [决策理由] 持久化恢复后必须再刷新运行态。
		runtimeContext, cancelRuntime := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
		rollbackLoadErr := s.reloadGroupControl(runtimeContext)
		cancelRuntime()
		// [决策理由] 数据库补偿与运行态恢复使用独立预算，前者耗尽时不影响后者。
		if rollbackLoadErr != nil {
			return rollback, errors.Join(refreshErr, failureAuditErr, fmt.Errorf("恢复群控制运行态: %w", rollbackLoadErr))
		}
		return rollback, errors.Join(refreshErr, failureAuditErr)
	}

	// >>> 数据演变示例
	// 1. default=true/v2 -> false/v3 -> Load成功。
	// 2. Load失败 -> CAS写回true/v4 -> 再Load。
	return after, nil
}

// SetPluginGroupOverride 新增或更新单群覆盖并热刷新。
// @param ctx：请求上下文；actor：操作者；name/groupID：目标；enabled：值；version：0 新增，正数 CAS 更新。
// @returns 新覆盖或授权、输入、冲突与刷新错误。
// ⚠️副作用说明：写入覆盖与审计后刷新 Manager；刷新失败时尝试恢复旧覆盖。
func (s *Service) SetPluginGroupOverride(ctx context.Context, actor Actor, name, groupID string, enabled bool, version int64) (PluginGroupOverride, error) {
	// [决策理由] 群号必须是手工输入的正十进制 QQ 群号，拒绝空值、负数和分隔符。
	if err := s.authorize(actor); err != nil || !validGroupID(groupID) || version < 0 {
		// [决策理由] 授权失败时不应被后续群号或版本校验错误覆盖。
		if err != nil {
			return PluginGroupOverride{}, err
		}
		return PluginGroupOverride{}, ErrInvalidGroupControl
	}
	repository, ok := s.repository.(GroupControlRepository)
	// [决策理由] 持久化能力缺失时不能接受覆盖。
	if !ok {
		return PluginGroupOverride{}, ErrGroupControlNotSupported
	}
	// [决策理由] 群控制全局锁是本路径唯一管理锁，不再获取 pluginLocks，因此与生命周期/配置写无反向锁顺序。
	s.groupControlMu.Lock()
	defer s.groupControlMu.Unlock()
	before, readErr := repository.GetPluginGroupControl(ctx, name)
	// [决策理由] 补偿必须知道旧覆盖是缺失还是具体值。
	if readErr != nil {
		return PluginGroupOverride{}, readErr
	}
	item, err := repository.SetPluginGroupOverride(ctx, actor, name, groupID, enabled, version)
	// [决策理由] 写入失败时不刷新。
	if err != nil {
		return PluginGroupOverride{}, err
	}
	// [决策理由] 成功写入必须立即更新运行 gate。
	if err := s.reloadGroupControl(ctx); err != nil {
		refreshErr := fmt.Errorf("刷新群覆盖: %w", err)
		failureAuditErr := s.recordGroupControlRefreshFailure(ctx, actor, name)
		databaseContext, cancelDatabase := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
		var old *PluginGroupOverride
		for index := range before.Overrides {
			// [决策理由] 只恢复当前群的旧覆盖。
			if before.Overrides[index].GroupID == groupID {
				copy := before.Overrides[index]
				old = &copy
			}
		}
		var rollbackErr error
		// [决策理由] 原先缺失时通过删除新版本补偿。
		if old == nil {
			rollbackErr = repository.DeletePluginGroupOverride(databaseContext, actor, name, groupID, item.Version)
		} else {
			_, rollbackErr = repository.SetPluginGroupOverride(databaseContext, actor, name, groupID, old.Enabled, item.Version)
		}
		cancelDatabase()
		// [决策理由] 补偿失败需与刷新错误合并。
		if rollbackErr != nil {
			return item, errors.Join(refreshErr, failureAuditErr, fmt.Errorf("补偿群覆盖: %w", rollbackErr))
		}
		// [决策理由] 数据库恢复后必须再刷新运行快照。
		runtimeContext, cancelRuntime := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
		reloadErr := s.reloadGroupControl(runtimeContext)
		cancelRuntime()
		// [决策理由] 覆盖补偿写入耗时不得吞掉 gate 恢复的独立超时预算。
		if reloadErr != nil {
			return item, errors.Join(refreshErr, failureAuditErr, fmt.Errorf("恢复群覆盖运行态: %w", reloadErr))
		}
		return item, errors.Join(refreshErr, failureAuditErr)
	}

	// >>> 数据演变示例
	// 1. group100无覆盖 -> true/v1 -> Load。
	// 2. Load失败 -> 删除v1 -> 再Load。
	return item, nil
}

// DeletePluginGroupOverride 删除覆盖以恢复继承默认值。
// @param ctx：请求上下文；actor：操作者；name/groupID：目标；version：期望版本。
// @returns 成功 nil，或授权、输入、冲突与刷新/补偿错误。
// ⚠️副作用说明：删除覆盖与写审计后刷新；失败时重建旧覆盖。
func (s *Service) DeletePluginGroupOverride(ctx context.Context, actor Actor, name, groupID string, version int64) error {
	// [决策理由] 删除必须提供合法群号和正版本。
	if err := s.authorize(actor); err != nil || !validGroupID(groupID) || version <= 0 {
		// [决策理由] 授权拒绝必须保留原错误，不得降级为普通输入错误。
		if err != nil {
			return err
		}
		return ErrInvalidGroupControl
	}
	repository, ok := s.repository.(GroupControlRepository)
	// [决策理由] 缺失群控制仓库时不得伪造删除成功。
	if !ok {
		return ErrGroupControlNotSupported
	}
	// [决策理由] 删除也会发布全量 gate 快照，必须与其他插件的默认/覆盖写入串行到最终恢复结束。
	s.groupControlMu.Lock()
	defer s.groupControlMu.Unlock()
	before, err := repository.GetPluginGroupControl(ctx, name)
	// [决策理由] 恢复失败时需要旧覆盖值。
	if err != nil {
		return err
	}
	err = repository.DeletePluginGroupOverride(ctx, actor, name, groupID, version)
	// [决策理由] 删除失败时运行态仍继续使用旧覆盖。
	if err != nil {
		return err
	}
	// [决策理由] 删除后必须刷新以使群继承默认值。
	if err := s.reloadGroupControl(ctx); err != nil {
		refreshErr := fmt.Errorf("刷新删除后群控制: %w", err)
		failureAuditErr := s.recordGroupControlRefreshFailure(ctx, actor, name)
		databaseContext, cancelDatabase := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
		var old *PluginGroupOverride
		for index := range before.Overrides {
			// [决策理由] 仅目标群的旧覆盖可用于补偿。
			if before.Overrides[index].GroupID == groupID {
				copy := before.Overrides[index]
				old = &copy
			}
		}
		// [决策理由] 理论上删除成功必有旧值，缺失时无法安全补偿。
		if old == nil {
			cancelDatabase()
			return errors.Join(refreshErr, failureAuditErr, ErrGroupOverrideNotFound)
		}
		_, rollbackErr := repository.SetPluginGroupOverride(databaseContext, actor, name, groupID, old.Enabled, 0)
		cancelDatabase()
		// [决策理由] 重建失败必须保留双重根因。
		if rollbackErr != nil {
			return errors.Join(refreshErr, failureAuditErr, fmt.Errorf("补偿已删除群覆盖: %w", rollbackErr))
		}
		// [决策理由] 补偿后再刷新以恢复旧运行态。
		runtimeContext, cancelRuntime := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
		reloadErr := s.reloadGroupControl(runtimeContext)
		cancelRuntime()
		// [决策理由] 重建已删覆盖与恢复 gate 分别获得完整 10 秒预算。
		if reloadErr != nil {
			return errors.Join(refreshErr, failureAuditErr, fmt.Errorf("恢复删除前群控制: %w", reloadErr))
		}
		return errors.Join(refreshErr, failureAuditErr)
	}

	// >>> 数据演变示例
	// 1. group100/v2 -> DELETE -> Load -> 继承default。
	// 2. Load失败 -> INSERT旧值 -> 再Load。
	return nil
}

// recordGroupControlRefreshFailure 尝试写入固定脱敏失败审计。
// @param ctx：原请求上下文；actor：操作者；name：插件名。
// @returns 审计错误；仓库不支持时返回能力错误。
// ⚠️副作用说明：写入 success=false 审计；失败不会阻止后续数据库补偿。
func (s *Service) recordGroupControlRefreshFailure(ctx context.Context, actor Actor, name string) error {
	auditor, ok := s.repository.(GroupControlFailureAuditor)
	// [决策理由] 无失败审计能力本身就是需要与刷新根因一起暴露的一致性缺口。
	if !ok {
		return errors.New("群控制仓库不支持失败审计")
	}
	auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), pluginRefreshRollbackTimeout)
	defer cancel()
	err := auditor.RecordPluginGroupRefreshFailure(auditContext, actor, name)

	// >>> 数据演变示例
	// 1. Reload失败 -> success=false审计 -> nil。
	// 2. 审计失败 -> 返回error，服务仍继续补偿。
	return err
}

// reloadGroupControl 重新加载包含群 gate 的插件运行快照。
// @param ctx：刷新生命周期。
// @returns 刷新错误；无运行时时 nil。
// ⚠️副作用说明：可读 PostgreSQL 并替换 Manager 运行快照。
func (s *Service) reloadGroupControl(ctx context.Context) error {
	// [决策理由] 迁移工具等无运行时进程只需持久化。
	if s.runtime == nil {
		return nil
	}
	reloader, ok := s.runtime.(interface{ ReloadGroupGate(context.Context) error })
	// [决策理由] 群策略只应替换 gate 快照，不能通过全量 Load 重跑插件生命周期。
	if !ok {
		return ErrGroupControlNotSupported
	}
	err := reloader.ReloadGroupGate(ctx)

	// >>> 数据演变示例
	// 1. Manager -> Load新gate -> nil。
	// 2. nil runtime -> 无刷新 -> nil。
	return err
}
