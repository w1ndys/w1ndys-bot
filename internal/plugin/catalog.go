// 📌 影响范围：维护进程内编译时插件 Registration 注册表；不访问数据库或环境变量。
package plugin

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/onebot"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// Messenger 是插件发送回复所需的最小 BotAPI 能力。
type Messenger interface {
	Reply(context.Context, *ws.MessageEvent, any) (int64, error)
	ReplyToMessage(context.Context, *ws.MessageEvent, int64, string) (int64, error)
}

// ActionAPI 是插件调用非消息类 OneBot Action 所需的最小能力。
type ActionAPI interface {
	SetGroupBan(context.Context, onebot.SetGroupBanParams) error
	GetGroupMemberList(context.Context, onebot.GetGroupMemberListParams) ([]onebot.GroupMemberInfo, error)
	GetGroupMessageHistory(context.Context, onebot.GetGroupMessageHistoryParams) (onebot.GetGroupMessageHistoryResult, error)
	GetMessage(context.Context, any) (onebot.MessageInfo, error)
	DeleteMessage(context.Context, any) error
}

// Runtime 提供插件实例化所需的运行时依赖。
type Runtime struct {
	Messenger  Messenger
	Actions    ActionAPI
	Management management.Controller
	Database   *pgxpool.Pool
}

// Factory 使用运行时依赖创建插件实例。
type Factory func(Runtime) (Plugin, error)

// Registration 将管理元数据与运行时插件实现绑定。
type Registration struct {
	Manifest Manifest
	Factory  Factory
}

// New 创建并校验与 Manifest 绑定的插件运行实例。
// @param runtime：插件实例化所需的运行时依赖。
// @returns 名称与 Manifest 一致的插件实例，或工厂及绑定校验错误。
// ⚠️副作用说明：调用插件 Factory，可能触发具体工厂声明的依赖检查或初始化行为。
func (r Registration) New(runtime Runtime) (Plugin, error) {
	// [决策理由] 未注册或手工构造的 Registration 也可能调用 New，必须防止 nil 工厂引发 panic。
	if r.Factory == nil {
		return nil, fmt.Errorf("插件 %s 的工厂不能为空", r.Manifest.Name)
	}
	implementation, err := r.Factory(runtime)
	// [决策理由] 工厂错误应保留插件上下文，方便启动日志定位具体注册项。
	if err != nil {
		return nil, fmt.Errorf("创建插件 %s: %w", r.Manifest.Name, err)
	}
	// [决策理由] nil 实例无法进入运行路由，接口调用 Name 还可能触发 panic。
	if implementation == nil || isNilPlugin(implementation) {
		return nil, fmt.Errorf("插件 %s 的工厂返回空实例", r.Manifest.Name)
	}
	// [决策理由] Manifest 名称用于数据库与权限，实例名称用于运行路由，两者不一致会造成配置错位。
	if implementation.Name() != r.Manifest.Name {
		return nil, fmt.Errorf("插件实例名称 %q 与 Manifest 名称 %q 不一致", implementation.Name(), r.Manifest.Name)
	}

	// >>> 数据演变示例
	// 1. Manifest=echo + Factory实例Name=echo -> 返回实例,nil。
	// 2. Manifest=echo + Factory实例Name=other -> 返回名称不一致错误。
	return implementation, nil
}

// isNilPlugin 检查插件接口是否封装了可为 nil 的具体值。
// @param candidate：Factory 返回的插件接口。
// @returns 接口底层为 nil 指针、映射、切片、函数、通道或接口时返回 true。
// ⚠️副作用说明：无；仅反射读取接口动态值。
func isNilPlugin(candidate Plugin) bool {
	value := reflect.ValueOf(candidate)
	// [决策理由] 只有 Go 允许为 nil 的动态类型才能调用 IsNil，其他类型调用会 panic。
	if value.Kind() == reflect.Chan || value.Kind() == reflect.Func || value.Kind() == reflect.Interface || value.Kind() == reflect.Map || value.Kind() == reflect.Ptr || value.Kind() == reflect.Slice {
		return value.IsNil()
	}

	// >>> 数据演变示例
	// 1. Plugin接口封装(*implementation)(nil) -> Ptr+IsNil -> true。
	// 2. Plugin接口封装implementation值 -> Struct -> false。
	return false
}

var registrationCatalog = struct {
	sync.RWMutex
	items map[string]Registration
}{items: make(map[string]Registration)}

// Register 注册编译时插件元数据和运行实现。
// @param registration：Manifest 与 Plugin 绑定项。
// @returns Manifest、空实现、名称不一致或重名错误。
// ⚠️副作用说明：修改进程全局 Registration 注册表。
func Register(registration Registration) error {
	// [决策理由] 无效 Manifest 不能进入数据库同步和运行时路由。
	if err := registration.Manifest.Validate(); err != nil {
		return err
	}
	// [决策理由] 工厂为空时启动阶段无法创建运行实例。
	if registration.Factory == nil {
		return fmt.Errorf("插件 %s 的工厂不能为空", registration.Manifest.Name)
	}
	registrationCatalog.Lock()
	defer registrationCatalog.Unlock()
	// [决策理由] 同一二进制只能存在一个同名插件实现。
	if _, exists := registrationCatalog.items[registration.Manifest.Name]; exists {
		return fmt.Errorf("插件 %q 已注册", registration.Manifest.Name)
	}
	registration.Manifest = cloneManifest(registration.Manifest)
	registrationCatalog.items[registration.Manifest.Name] = registration

	// >>> 数据演变示例
	// 1. ping Manifest + Factory -> Catalog[ping] -> nil。
	// 2. ping Manifest + nil Factory -> 返回错误。
	return nil
}

// MustRegister 注册插件，失败时立即 panic 终止无效二进制启动。
// @param registration：Manifest 与 Plugin 绑定项。
// @returns 无。
// ⚠️副作用说明：修改全局注册表；注册错误时触发 panic。
func MustRegister(registration Registration) {
	// [决策理由] init 阶段无法向调用者返回错误，非法编译时插件应立即暴露。
	if err := Register(registration); err != nil {
		panic(err)
	}

	// >>> 数据演变示例
	// 1. 合法 ping -> Register nil -> 正常返回。
	// 2. 重复 ping -> Register error -> panic。
}

// Registrations 返回按插件名排序的注册项快照。
// @param 无。
// @returns 独立 Registration 切片。
// ⚠️副作用说明：无；仅读取进程内注册表。
func Registrations() []Registration {
	registrationCatalog.RLock()
	result := make([]Registration, 0, len(registrationCatalog.items))
	for _, registration := range registrationCatalog.items {
		registration.Manifest = cloneManifest(registration.Manifest)
		result = append(result, registration)
	}
	registrationCatalog.RUnlock()
	sort.Slice(result, func(i int, j int) bool {
		// >>> 数据演变示例
		// 1. [score,ping] -> 名称排序 -> ping 在前。
		// 2. [ping] -> 保持不变。
		return result[i].Manifest.Name < result[j].Manifest.Name
	})

	// >>> 数据演变示例
	// 1. Catalog{score,ping} -> [ping,score]。
	// 2. 空 Catalog -> 空切片。
	return result
}

// cloneManifest 深复制 Manifest 中的可变切片。
// @param manifest：待复制的插件元数据。
// @returns 不共享 Features 和 DefaultCommands 底层数组的 Manifest。
// ⚠️副作用说明：分配新的切片；不修改输入。
func cloneManifest(manifest Manifest) Manifest {
	result := manifest
	result.Features = make([]FeatureManifest, len(manifest.Features))
	for index, feature := range manifest.Features {
		result.Features[index] = feature
		result.Features[index].DefaultCommands = append([]string(nil), feature.DefaultCommands...)
	}

	// >>> 数据演变示例
	// 1. Features=[echo{[echo]}] -> 深复制 -> 修改副本命令不影响原值。
	// 2. Features=[] -> 深复制 -> 独立空切片。
	return result
}

// Manifests 从统一注册项提取元数据快照。
// @param 无。
// @returns 按插件名排序的 Manifest 切片。
// ⚠️副作用说明：无。
func Manifests() []Manifest {
	registrations := Registrations()
	result := make([]Manifest, 0, len(registrations))
	for _, registration := range registrations {
		result = append(result, registration.Manifest)
	}

	// >>> 数据演变示例
	// 1. Registration{ping Plugin+Manifest} -> ping Manifest。
	// 2. 空 Registration -> 空 Manifest 切片。
	return result
}
