// 📌 影响范围：维护进程内编译时插件 Registration 注册表；不访问数据库或环境变量。
package plugin

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Registration 将管理元数据与运行时插件实现绑定。
type Registration struct {
	Manifest Manifest
	Plugin   Plugin
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
	// [决策理由] 接口可能包含 typed nil，必须通过反射识别才能避免后续方法调用 panic。
	if isNilPlugin(registration.Plugin) {
		return fmt.Errorf("插件 %s 的运行实现不能为空", registration.Manifest.Name)
	}
	// [决策理由] Manifest 和实现名称必须一致，确保数据库状态准确映射到运行实例。
	if registration.Plugin.Name() != registration.Manifest.Name {
		return fmt.Errorf("Manifest 名称 %q 与插件实现名称 %q 不一致", registration.Manifest.Name, registration.Plugin.Name())
	}
	registrationCatalog.Lock()
	defer registrationCatalog.Unlock()
	// [决策理由] 同一二进制只能存在一个同名插件实现。
	if _, exists := registrationCatalog.items[registration.Manifest.Name]; exists {
		return fmt.Errorf("插件 %q 已注册", registration.Manifest.Name)
	}
	registrationCatalog.items[registration.Manifest.Name] = registration

	// >>> 数据演变示例
	// 1. ping Manifest + ping Plugin -> Catalog[ping] -> nil。
	// 2. score Manifest + ping Plugin -> 名称不一致 -> 返回错误。
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

// isNilPlugin 识别 nil 接口和包含 nil 指针的 Plugin 接口。
// @param candidate：待检查插件接口。
// @returns 无可用实现时返回 true。
// ⚠️副作用说明：无。
func isNilPlugin(candidate Plugin) bool {
	// [决策理由] 直接 nil 接口可立即判断，避免对无效 Value 调用 Kind。
	if candidate == nil {
		return true
	}
	value := reflect.ValueOf(candidate)
	// [决策理由] Plugin 常由指针实现，typed nil 仅能通过 Value.IsNil 识别。
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}

	// >>> 数据演变示例
	// 1. (*Ping)(nil) 装入 Plugin -> true。
	// 2. &Ping{} -> false。
	return false
}
