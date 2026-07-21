// 📌 影响范围：调用插件自有业务资源处理器；平台负责授权、路由键校验与分页边界。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

// ResourceRuntime 定义平台发现与定位插件业务资源的能力。
type ResourceRuntime interface {
	AdminResources(string) ([]plugin.AdminResource, error)
	AdminResourceHandler(string, string) (plugin.AdminResource, plugin.AdminResourceHandler, error)
}

var _ ResourceRuntime = (*plugin.Manager)(nil)

// ListPluginResources 返回插件的通用管理资源声明。
// @param ctx：请求上下文；actor：可信操作者；pluginName：插件名。
// @returns 资源声明，或授权与发现错误。
// ⚠️副作用说明：读取管理员快照并调用插件声明回调。
func (s *Service) ListPluginResources(_ context.Context, actor Actor, pluginName string) ([]plugin.AdminResource, error) {
	// [决策理由] 资源结构可暴露业务能力，读取也必须授权。
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	runtime, ok := s.runtime.(ResourceRuntime)
	// [决策理由] 缺少资源运行时时不能返回伪造空集合。
	if !ok {
		return nil, ErrPluginResourceNotSupported
	}
	resources, err := runtime.AdminResources(pluginName)
	// [决策理由] Manager 错误需转换为稳定管理语义。
	if err != nil {
		return nil, mapResourceError(err)
	}

	// >>> 数据演变示例
	// 1. 管理员+keyword_reply -> Manager[rules] -> 返回声明。
	// 2. 非管理员 -> ErrForbidden -> 不调用插件。
	return resources, nil
}

// ListPluginResourceRecords 委派有界分页查询。
// @param ctx：请求上下文；actor：操作者；pluginName/resourceKey：固定路由键；query：分页。
// @returns 记录页，或授权、边界、查找与插件错误。
// ⚠️副作用说明：调用插件 List，插件可读取自有数据库表。
func (s *Service) ListPluginResourceRecords(ctx context.Context, actor Actor, pluginName, resourceKey string, query ResourceQuery) (ResourcePage, error) {
	descriptor, handler, err := s.resourceHandler(actor, pluginName, resourceKey)
	// [决策理由] 只能对经授权且已注册的资源执行查询。
	if err != nil {
		return ResourcePage{}, err
	}
	// [决策理由] page 从 1 开始且 page_size 不能越过资源声明上限。
	if query.PageSize == 0 {
		query.PageSize = 20
		// [决策理由] 资源声明上限小于平台默认值时，无参数请求应自动收敛而非失败。
		if query.PageSize > descriptor.MaxPageSize {
			query.PageSize = descriptor.MaxPageSize
		}
	}
	// [决策理由] 显式越界值仍必须拒绝，不得静默扩大插件查询。
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > descriptor.MaxPageSize || query.Page > math.MaxInt/query.PageSize {
		return ResourcePage{}, ErrInvalidResourceData
	}
	page, err := callResourceList(ctx, handler, actor, query)
	// [决策理由] 插件业务错误不能绕过稳定 API 错误映射。
	if err != nil {
		return ResourcePage{}, mapResourceError(err)
	}

	// >>> 数据演变示例
	// 1. page1,size20,max50 -> handler.List -> 返回记录页。
	// 2. size51,max50 -> 边界拒绝 -> 不调用插件。
	return page, nil
}

// CreatePluginResourceRecord 新增插件业务记录。
// @param ctx：请求上下文；actor：操作者；pluginName/resourceKey：路由键；data：业务 JSON。
// @returns 新记录或稳定管理错误。
// ⚠️副作用说明：插件处理器可在同一事务写业务表与审计表。
func (s *Service) CreatePluginResourceRecord(ctx context.Context, actor Actor, pluginName, resourceKey string, data json.RawMessage) (ResourceRecord, error) {
	descriptor, handler, err := s.resourceHandler(actor, pluginName, resourceKey)
	// [决策理由] 查找或授权失败时不得进入插件事务。
	if err != nil {
		return ResourceRecord{}, err
	}
	// [决策理由] 声明为只读的资源不允许通过手工 HTTP 请求写入。
	if !descriptor.CanCreate {
		return ResourceRecord{}, ErrInvalidResourceData
	}
	normalized, err := plugin.NormalizeResourceData(descriptor, data)
	// [决策理由] 平台必须在进入插件前拒绝 null、数组、未知字段和类型错误。
	if err != nil {
		return ResourceRecord{}, fmt.Errorf("%w: %v", ErrInvalidResourceData, err)
	}
	record, err := callResourceCreate(ctx, handler, actor, normalized)
	// [决策理由] 领域校验、唯一约束与数据库错误由统一映射处理。
	if err != nil {
		return ResourceRecord{}, mapResourceError(err)
	}

	// >>> 数据演变示例
	// 1. rules可新增+合法JSON -> 插件事务 -> id1/version1。
	// 2. 只读资源 -> 能力拒绝 -> 不调用Create。
	return record, nil
}

// UpdatePluginResourceRecord 按版本更新插件业务记录。
// @param ctx：请求上下文；actor：操作者；pluginName/resourceKey：路由键；id/expectedVersion：主键与版本；data：业务 JSON。
// @returns 更新记录或稳定管理错误。
// ⚠️副作用说明：插件处理器可在同一事务更新业务表与审计表。
func (s *Service) UpdatePluginResourceRecord(ctx context.Context, actor Actor, pluginName, resourceKey string, id, expectedVersion int64, data json.RawMessage) (ResourceRecord, error) {
	descriptor, handler, err := s.resourceHandler(actor, pluginName, resourceKey)
	// [决策理由] 只委派可信路由定位到的处理器。
	if err != nil {
		return ResourceRecord{}, err
	}
	// [决策理由] 主键、版本和更新能力都必须在插件事务前验证。
	if !descriptor.CanUpdate || id <= 0 || expectedVersion <= 0 {
		return ResourceRecord{}, ErrInvalidResourceData
	}
	normalized, err := plugin.NormalizeResourceData(descriptor, data)
	// [决策理由] 更新与新增必须共享相同的严格字段契约。
	if err != nil {
		return ResourceRecord{}, fmt.Errorf("%w: %v", ErrInvalidResourceData, err)
	}
	record, err := callResourceUpdate(ctx, handler, actor, id, expectedVersion, normalized)
	// [决策理由] 陈旧版本必须保持可判定冲突语义。
	if err != nil {
		return ResourceRecord{}, mapResourceError(err)
	}

	// >>> 数据演变示例
	// 1. id1/v2+合法数据 -> CAS -> id1/v3。
	// 2. id1/v1而当前v2 -> plugin.ErrResourceConflict -> 管理冲突。
	return record, nil
}

// DeletePluginResourceRecord 按版本删除插件业务记录。
// @param ctx：请求上下文；actor：操作者；pluginName/resourceKey：路由键；id/expectedVersion：主键与版本。
// @returns 删除结果错误，成功为 nil。
// ⚠️副作用说明：插件处理器可在同一事务删除业务记录并写审计。
func (s *Service) DeletePluginResourceRecord(ctx context.Context, actor Actor, pluginName, resourceKey string, id, expectedVersion int64) error {
	descriptor, handler, err := s.resourceHandler(actor, pluginName, resourceKey)
	// [决策理由] 只对授权且注册的资源执行删除。
	if err != nil {
		return err
	}
	// [决策理由] 必须显式开放删除能力并携带有效 CAS 参数。
	if !descriptor.CanDelete || id <= 0 || expectedVersion <= 0 {
		return ErrInvalidResourceData
	}
	err = callResourceDelete(ctx, handler, actor, id, expectedVersion)
	// [决策理由] 删除的不存在与版本冲突需为 WebUI 保留不同恢复策略。
	if err != nil {
		return mapResourceError(err)
	}

	// >>> 数据演变示例
	// 1. id1/v3 -> CAS DELETE+审计 -> nil。
	// 2. id0 -> 输入拒绝 -> 不调用Delete。
	return nil
}

// resourceHandler 授权并定位固定资源处理器。
// @param actor：操作者；pluginName/resourceKey：服务端路由键。
// @returns 资源声明、处理器或授权/发现错误。
// ⚠️副作用说明：读取管理员快照并调用插件资源声明。
func (s *Service) resourceHandler(actor Actor, pluginName, resourceKey string) (plugin.AdminResource, plugin.AdminResourceHandler, error) {
	// [决策理由] Actor 只来自认证上下文，仍由服务层重新授权。
	if err := s.authorize(actor); err != nil {
		return plugin.AdminResource{}, nil, err
	}
	runtime, ok := s.runtime.(ResourceRuntime)
	// [决策理由] 运行时不具备资源能力时安全失败。
	if !ok {
		return plugin.AdminResource{}, nil, ErrPluginResourceNotSupported
	}
	descriptor, handler, err := runtime.AdminResourceHandler(pluginName, resourceKey)
	// [决策理由] 不将 Manager 内部细节直接暴露给 API。
	if err != nil {
		return plugin.AdminResource{}, nil, mapResourceError(err)
	}

	// >>> 数据演变示例
	// 1. 管理员+keyword_reply/rules -> descriptor+handler。
	// 2. missing/rules -> Manager ErrNotRegistered -> ErrPluginNotFound。
	return descriptor, handler, nil
}

// mapResourceError 转换插件资源错误为稳定管理语义。
// @param err：Manager 或插件处理器错误。
// @returns 可供 HTTP 层 errors.Is 的管理错误。
// ⚠️副作用说明：无。
func mapResourceError(err error) error {
	// [决策理由] 每种预期错误都需要稳定 HTTP 恢复策略。
	if errors.Is(err, plugin.ErrNotRegistered) {
		return fmt.Errorf("%w: %v", ErrPluginNotFound, err)
	}
	// [决策理由] 能力缺失与资源键不存在是不同子资源状态。
	if errors.Is(err, plugin.ErrAdminResourceNotSupported) {
		return fmt.Errorf("%w: %v", ErrPluginResourceNotSupported, err)
	}
	// [决策理由] 未注册资源键不得下沉为任意 SQL 对象。
	if errors.Is(err, plugin.ErrAdminResourceNotFound) {
		return fmt.Errorf("%w: %v", ErrPluginResourceNotFound, err)
	}
	// [决策理由] 记录缺失需要前端移除陈旧行。
	if errors.Is(err, plugin.ErrResourceRecordNotFound) {
		return fmt.Errorf("%w: %v", ErrResourceRecordNotFound, err)
	}
	// [决策理由] 领域输入错误属于客户端可修正请求。
	if errors.Is(err, plugin.ErrInvalidResourceData) {
		return fmt.Errorf("%w: %v", ErrInvalidResourceData, err)
	}
	// [决策理由] 唯一约束和乐观锁冲突均要求前端刷新后重试。
	if errors.Is(err, plugin.ErrResourceConflict) {
		return fmt.Errorf("%w: %v", ErrPluginResourceConflict, err)
	}

	// >>> 数据演变示例
	// 1. plugin.ErrResourceConflict -> ErrPluginResourceConflict包装。
	// 2. PostgreSQL连接错误 -> 原样保留供服务端诊断。
	return err
}

// callResourceList 隔离插件 List 回调 panic。
// @param ctx：请求上下文；handler：资源处理器；actor：操作者；query：分页。
// @returns 记录页或稳定回调错误。
// ⚠️副作用说明：调用插件 List 并捕获 panic。
func callResourceList(ctx context.Context, handler plugin.AdminResourceHandler, actor Actor, query ResourceQuery) (ResourcePage, error) {
	result, err := callResource(func() (ResourcePage, error) { return handler.List(ctx, actor, query) })
	// >>> 数据演变示例
	// 1. List返回page -> page,nil。
	// 2. List panic -> 稳定error。
	return result, err
}

// callResourceCreate 隔离插件 Create 回调 panic。
// @param ctx：请求上下文；handler：处理器；actor：操作者；data：已校验数据。
// @returns 新记录或稳定回调错误。
// ⚠️副作用说明：调用插件 Create 并捕获 panic。
func callResourceCreate(ctx context.Context, handler plugin.AdminResourceHandler, actor Actor, data json.RawMessage) (ResourceRecord, error) {
	result, err := callResource(func() (ResourceRecord, error) { return handler.Create(ctx, actor, data) })
	// >>> 数据演变示例
	// 1. Create返回id1 -> record,nil。
	// 2. Create panic -> 稳定error。
	return result, err
}

// callResourceUpdate 隔离插件 Update 回调 panic。
// @param ctx：请求上下文；handler：处理器；actor：操作者；id/version/data：已校验 CAS 参数。
// @returns 更新记录或稳定回调错误。
// ⚠️副作用说明：调用插件 Update 并捕获 panic。
func callResourceUpdate(ctx context.Context, handler plugin.AdminResourceHandler, actor Actor, id, version int64, data json.RawMessage) (ResourceRecord, error) {
	result, err := callResource(func() (ResourceRecord, error) { return handler.Update(ctx, actor, id, version, data) })
	// >>> 数据演变示例
	// 1. Update返回v2 -> record,nil。
	// 2. Update panic -> 稳定error。
	return result, err
}

// callResourceDelete 隔离插件 Delete 回调 panic。
// @param ctx：请求上下文；handler：处理器；actor：操作者；id/version：CAS 参数。
// @returns 删除错误，成功为 nil。
// ⚠️副作用说明：调用插件 Delete 并捕获 panic。
func callResourceDelete(ctx context.Context, handler plugin.AdminResourceHandler, actor Actor, id, version int64) error {
	_, err := callResource(func() (struct{}, error) { return struct{}{}, handler.Delete(ctx, actor, id, version) })
	// >>> 数据演变示例
	// 1. Delete返回nil -> nil。
	// 2. Delete panic -> 稳定error。
	return err
}

// callResource 执行任意带结果的插件资源回调。
// @param callback：封闭后的插件回调。
// @returns 回调结果与错误；panic 转换为不含外部数据的固定错误。
// ⚠️副作用说明：执行回调并捕获 panic。
func callResource[T any](callback func() (T, error)) (result T, err error) {
	defer func() {
		// [决策理由] 插件 panic 值可能包含业务数据，只返回固定安全错误。
		if recover() != nil {
			err = errors.New("插件资源回调失败")
		}

		// >>> 数据演变示例
		// 1. callback返回value,nil -> 保留结果。
		// 2. callback panic(secret) -> 丢弃panic值 -> 固定error。
	}()
	result, err = callback()

	// >>> 数据演变示例
	// 1. 正常回调 -> result,error原样返回。
	// 2. panic回调 -> defer恢复 -> 零值+固定error。
	return result, err
}
