// 📌 影响范围：维护进程内编译时插件 Registration 注册表；不访问数据库或环境变量。
package plugin

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

// Messenger 是插件发送回复所需的最小 BotAPI 能力。
type Messenger interface {
	Reply(context.Context, *ws.MessageEvent, any) (int64, error)
	ReplyToMessage(context.Context, *ws.MessageEvent, int64, string) (int64, error)
}

// Runtime 提供插件实例化所需的运行时依赖。
type Runtime struct {
	Messenger  Messenger
	Management management.Controller
}

// Factory 使用运行时依赖创建插件实例。
type Factory func(Runtime) (Plugin, error)

// Registration 将管理元数据与运行时插件实现绑定。
type Registration struct {
	Manifest Manifest
	Factory  Factory
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
