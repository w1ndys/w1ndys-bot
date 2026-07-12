// 📌 影响范围：维护进程内编译时插件 Manifest 注册表；不访问数据库或环境变量。
package plugin

import (
	"fmt"
	"sort"
	"sync"
)

var manifestCatalog = struct {
	sync.RWMutex
	items map[string]Manifest
}{items: make(map[string]Manifest)}

// RegisterManifest 注册编译时插件元数据。
// @param manifest：已构造的插件 Manifest。
// @returns 校验或重名错误。
// ⚠️副作用说明：修改进程全局 Manifest 注册表。
func RegisterManifest(manifest Manifest) error {
	// [决策理由] 无效 Manifest 不能进入启动同步流程。
	if err := manifest.Validate(); err != nil {
		return err
	}
	manifestCatalog.Lock()
	defer manifestCatalog.Unlock()
	// [决策理由] 编译进同一二进制的插件名必须唯一。
	if _, exists := manifestCatalog.items[manifest.Name]; exists {
		return fmt.Errorf("插件 Manifest %q 已注册", manifest.Name)
	}
	manifestCatalog.items[manifest.Name] = manifest

	// >>> 数据演变示例
	// 1. 空 Catalog + ping -> items[ping]=Manifest -> nil。
	// 2. 已有 ping + ping -> 返回重名错误。
	return nil
}

// Manifests 返回按插件名排序的注册表快照。
// @param 无。
// @returns 独立 Manifest 切片。
// ⚠️副作用说明：无；仅读取进程内注册表。
func Manifests() []Manifest {
	manifestCatalog.RLock()
	result := make([]Manifest, 0, len(manifestCatalog.items))
	for _, manifest := range manifestCatalog.items {
		result = append(result, manifest)
	}
	manifestCatalog.RUnlock()
	sort.Slice(result, func(i int, j int) bool {
		// >>> 数据演变示例
		// 1. [score,ping] -> 比较名称 -> ping 在前。
		// 2. [ping] -> 无交换 -> 保持不变。
		return result[i].Name < result[j].Name
	})

	// >>> 数据演变示例
	// 1. Catalog{score,ping} -> [ping,score]。
	// 2. 空 Catalog -> 空切片。
	return result
}
