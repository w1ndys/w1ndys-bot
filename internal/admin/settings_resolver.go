// 📌 影响范围：从管理 Repository 读取 system_settings；原子发布进程内业务设置快照。
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// SettingDefinition 定义受支持系统设置的默认值和说明。
type SettingDefinition struct {
	Key         string
	Description string
	Default     json.RawMessage
}

var settingDefinitions = map[string]SettingDefinition{
	"command_prefix":       {Key: "command_prefix", Description: "机器人命令前缀", Default: json.RawMessage(`"/"`)},
	"audit_retention_days": {Key: "audit_retention_days", Description: "审计日志保留天数", Default: json.RawMessage(`90`)},
	"default_page_size":    {Key: "default_page_size", Description: "管理列表默认分页大小", Default: json.RawMessage(`20`)},
}

type settingsSnapshot struct {
	values map[string]json.RawMessage
}

// SettingsResolver 提供并发安全的系统设置读取和热刷新。
type SettingsResolver struct {
	repository Repository
	snapshot   atomic.Pointer[settingsSnapshot]
}

// NewSettingsResolver 创建包含默认值的设置解析器。
// @param repository：系统设置数据源。
// @returns 已发布默认设置快照的解析器。
// ⚠️副作用说明：仅分配内存，不访问数据库。
func NewSettingsResolver(repository Repository) *SettingsResolver {
	resolver := &SettingsResolver{repository: repository}
	resolver.snapshot.Store(&settingsSnapshot{values: defaultSettings()})

	// >>> 数据演变示例
	// 1. Repository -> Resolver{command_prefix:"/"} -> Load后覆盖数据库值。
	// 2. nil Repository -> 默认快照可读 -> Load返回配置错误。
	return resolver
}

// Load 合并默认值和数据库设置并原子发布完整快照。
// @param ctx：控制数据库查询生命周期。
// @returns 仓库查询或未知、无效设置错误。
// ⚠️副作用说明：读取 system_settings 并替换进程内设置快照。
func (r *SettingsResolver) Load(ctx context.Context) error {
	// [决策理由] 无仓库时不能生成可信数据库覆盖快照。
	if r.repository == nil {
		return fmt.Errorf("系统设置仓库未配置")
	}
	states, err := r.repository.ListSystemSettings(ctx)
	// [决策理由] 查询失败时保留上一份完整快照。
	if err != nil {
		return fmt.Errorf("加载系统设置: %w", err)
	}
	next := defaultSettings()
	for _, state := range states {
		definition, exists := settingDefinitions[state.Key]
		// [决策理由] 未注册键可能来自旧版本，不能发布给当前业务逻辑。
		if !exists {
			return fmt.Errorf("%w: %s", ErrUnknownSetting, state.Key)
		}
		// [决策理由] 数据库值也必须通过当前版本定义校验，避免旧数据破坏运行时。
		if err := validateSetting(definition.Key, state.Value); err != nil {
			return fmt.Errorf("设置 %s 无效: %w", state.Key, err)
		}
		next[state.Key] = append(json.RawMessage(nil), state.Value...)
	}
	r.snapshot.Store(&settingsSnapshot{values: next})

	// >>> 数据演变示例
	// 1. DB command_prefix="!" -> defaults合并 -> 快照前缀!。
	// 2. DB空 -> 发布完整默认值快照。
	return nil
}

// CommandPrefix 返回当前命令前缀。
// @param 无。
// @returns 已校验命令前缀；零值解析器回退为斜杠。
// ⚠️副作用说明：无；仅读取原子快照。
func (r *SettingsResolver) CommandPrefix() string {
	current := r.snapshot.Load()
	// [决策理由] 防御未通过构造器创建的零值 Resolver。
	if current == nil {
		return "/"
	}
	var prefix string
	// [决策理由] 快照由校验后数据构建，解析异常仍应安全回退而非中断消息路由。
	if err := json.Unmarshal(current.values["command_prefix"], &prefix); err != nil || prefix == "" {
		return "/"
	}

	// >>> 数据演变示例
	// 1. snapshot command_prefix="!" -> 返回!。
	// 2. 零值或损坏快照 -> 回退/。
	return prefix
}

// Definitions 返回按键索引的设置定义副本。
// @param 无。
// @returns 独立设置定义 map。
// ⚠️副作用说明：无。
func Definitions() map[string]SettingDefinition {
	result := make(map[string]SettingDefinition, len(settingDefinitions))
	for key, definition := range settingDefinitions {
		definition.Default = append(json.RawMessage(nil), definition.Default...)
		result[key] = definition
	}

	// >>> 数据演变示例
	// 1. 内部3项定义 -> 返回3项副本。
	// 2. 调用方修改Default -> 内部定义不变。
	return result
}

// defaultSettings 复制全部定义默认值。
// @param 无。
// @returns 可独立修改的默认设置 map。
// ⚠️副作用说明：无。
func defaultSettings() map[string]json.RawMessage {
	values := make(map[string]json.RawMessage, len(settingDefinitions))
	for key, definition := range settingDefinitions {
		values[key] = append(json.RawMessage(nil), definition.Default...)
	}

	// >>> 数据演变示例
	// 1. definitions含command_prefix -> values含独立"/"副本。
	// 2. 修改values -> definitions.Default不变。
	return values
}

// validateSetting 按稳定键校验 JSON 值类型和范围。
// @param key：设置键；value：JSONB 原始值。
// @returns 合法时 nil，否则返回未知键、类型或范围错误。
// ⚠️副作用说明：无。
func validateSetting(key string, value json.RawMessage) error {
	// [决策理由] 所有值必须先是合法 JSON，才能进入类型分支。
	if !json.Valid(value) {
		return fmt.Errorf("值不是合法 JSON")
	}
	switch key {
	case "command_prefix":
		var prefix string
		// [决策理由] 命令前缀必须是字符串，避免路由器产生隐式转换。
		if err := json.Unmarshal(value, &prefix); err != nil {
			return fmt.Errorf("命令前缀必须是字符串")
		}
		// [决策理由] 空白、空值或过长前缀会造成误匹配或难以使用。
		if strings.TrimSpace(prefix) != prefix || utf8.RuneCountInString(prefix) < 1 || utf8.RuneCountInString(prefix) > 4 {
			return fmt.Errorf("命令前缀必须为1至4个非空白字符")
		}
	case "audit_retention_days":
		return validateIntegerRange(value, 1, 3650, "审计保留天数")
	case "default_page_size":
		return validateIntegerRange(value, 10, 200, "默认分页大小")
	default:
		return fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}

	// >>> 数据演变示例
	// 1. command_prefix="!" -> 字符串且长度1 -> nil。
	// 2. default_page_size=500 -> 超出10..200 -> error。
	return nil
}

// validateIntegerRange 校验 JSON 整数范围。
// @param value：JSON 原始值；minimum、maximum：闭区间；name：错误字段名。
// @returns 整数位于范围时 nil，否则返回类型或范围错误。
// ⚠️副作用说明：无。
func validateIntegerRange(value json.RawMessage, minimum int, maximum int, name string) error {
	var number int
	// [决策理由] 设置要求整数，浮点数和字符串不得隐式截断。
	if err := json.Unmarshal(value, &number); err != nil {
		return fmt.Errorf("%s必须是整数", name)
	}
	// [决策理由] 范围限制避免危险保留周期或不可用分页尺寸。
	if number < minimum || number > maximum {
		return fmt.Errorf("%s必须在%d至%d之间", name, minimum, maximum)
	}

	// >>> 数据演变示例
	// 1. value=20,range=10..200 -> nil。
	// 2. value=500,range=10..200 -> error。
	return nil
}
