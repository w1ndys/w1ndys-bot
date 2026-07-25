// 📌 影响范围：构建目标插件规格的纯内存不可变目录；不修改旧 Catalog、数据库或运行状态。
package plugin

import (
	"fmt"
	"sort"

	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
)

// SpecCatalog 保存按稳定 Key 索引的目标插件规格快照。
type SpecCatalog struct {
	items map[string]PluginSpec
}

// NewSpecCatalog 校验插件规格及跨插件触发词冲突并构建目录。
// @param specs：编译进当前进程的插件规格。
// @returns 与调用方切片隔离的只读目录，或首个规格及冲突错误。
// ⚠️副作用说明：分配并复制规格；不修改旧全局 Catalog。
func NewSpecCatalog(specs []PluginSpec) (*SpecCatalog, error) {
	items := make(map[string]PluginSpec, len(specs))
	triggerOwners := make(map[string]string)
	for _, spec := range specs {
		// [决策理由] 无效规格不得进入路由目录和后续数据库状态映射。
		if err := spec.Validate(); err != nil {
			return nil, err
		}
		// [决策理由] 同名插件会争用状态、配置、路由和管理页面。
		if _, found := items[spec.Key]; found {
			return nil, fmt.Errorf("插件 Key %q 重复", spec.Key)
		}
		for _, command := range spec.Commands {
			for _, trigger := range command.Triggers {
				normalized, err := commandregistry.Normalize(trigger, "")
				// [决策理由] PluginSpec.Validate 已验证触发词；此防御检查避免未来独立调用破坏构建边界。
				if err != nil {
					return nil, fmt.Errorf("插件 %s 的命令 %s 触发词无效: %w", spec.Key, command.Key, err)
				}
				owner, found := triggerOwners[normalized]
				// [决策理由] 首版代码触发词为全局唯一，跨插件冲突不能依赖运行顺序解决。
				if found {
					return nil, fmt.Errorf("触发词 %q 被 %s 和 %s.%s 重复声明", normalized, owner, spec.Key, command.Key)
				}
				triggerOwners[normalized] = spec.Key + "." + command.Key
			}
		}
		items[spec.Key] = clonePluginSpec(spec)
	}

	// >>> 数据演变示例
	// 1. [echo,monitor]且触发词唯一 -> 深复制目录 -> 返回成功。
	// 2. echo.echo和tools.echo均声明echo -> 冲突 -> 不返回目录。
	return &SpecCatalog{items: items}, nil
}

// Find 返回指定插件规格的独立快照。
// @param key：稳定插件 Key。
// @returns 规格副本及是否存在。
// ⚠️副作用说明：复制规格中的映射与切片。
func (c *SpecCatalog) Find(key string) (PluginSpec, bool) {
	// [决策理由] nil 目录可能来自尚未初始化的装配代码，读取应安全失败而非 panic。
	if c == nil {
		return PluginSpec{}, false
	}
	spec, found := c.items[key]
	// [决策理由] 未找到时返回稳定零值，避免复制不存在的数据。
	if !found {
		return PluginSpec{}, false
	}

	// >>> 数据演变示例
	// 1. Catalog[echo] -> 深复制echo,true。
	// 2. Catalog无missing -> 零值,false。
	return clonePluginSpec(spec), true
}

// Specs 返回按插件 Key 排序的独立规格快照。
// @param 无。
// @returns 不共享可变切片和映射的规格列表。
// ⚠️副作用说明：分配、复制并排序结果。
func (c *SpecCatalog) Specs() []PluginSpec {
	// [决策理由] nil 目录应表现为空目录，方便只读管理接口安全展示。
	if c == nil {
		return []PluginSpec{}
	}
	result := make([]PluginSpec, 0, len(c.items))
	for _, spec := range c.items {
		result = append(result, clonePluginSpec(spec))
	}
	sort.Slice(result, func(i int, j int) bool {
		// >>> 数据演变示例
		// 1. [monitor,echo] -> Key排序 -> [echo,monitor]。
		// 2. [echo] -> 顺序不变。
		return result[i].Key < result[j].Key
	})

	// >>> 数据演变示例
	// 1. Catalog{monitor,echo} -> [echo,monitor]。
	// 2. 空Catalog -> 空切片。
	return result
}

// clonePluginSpec 深复制规格中的全部可变集合。
// @param spec：待复制插件规格。
// @returns 与输入不共享命令、触发词、角色和观察事件切片的规格。
// ⚠️副作用说明：分配新的切片和映射；Handler 与 Lifecycle 引用保持只读绑定。
func clonePluginSpec(spec PluginSpec) PluginSpec {
	result := spec
	result.Commands = make([]CommandSpec, len(spec.Commands))
	for index, command := range spec.Commands {
		result.Commands[index] = command
		result.Commands[index].Triggers = append([]string(nil), command.Triggers...)
		result.Commands[index].AllowedRoles = make(RoleSet, len(command.AllowedRoles))
		for role := range command.AllowedRoles {
			result.Commands[index].AllowedRoles[role] = struct{}{}
		}
	}
	result.Observers = make([]ObserverSpec, len(spec.Observers))
	for index, observer := range spec.Observers {
		result.Observers[index] = observer
		result.Observers[index].EventKinds = append([]ObserverEventKind(nil), observer.EventKinds...)
	}

	// >>> 数据演变示例
	// 1. echo触发词[echo]+member角色 -> 复制后修改副本不影响输入。
	// 2. monitor事件[group_message] -> 复制事件切片 -> Handler绑定保持不变。
	return result
}
