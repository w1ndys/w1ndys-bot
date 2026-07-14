// 📌 影响范围：定义命令作用域与纯内存标准化规则；不访问数据库或环境变量。
package command

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ScopeType 表示命令生效范围。
type ScopeType string

const (
	ScopeGlobal ScopeType = "global"
	ScopeGroup  ScopeType = "group"
)

// Binding 表示一条命令到插件功能的映射。
type Binding struct {
	ID                int64
	ScopeType         ScopeType
	ScopeID           string
	PluginName        string
	FeatureKey        string
	Command           string
	NormalizedCommand string
	Enabled           bool
}

// Target 返回稳定的 plugin_name.feature_key 功能地址。
// @param 无。
// @returns 插件与功能组成的目标字符串。
// ⚠️副作用说明：无。
func (b Binding) Target() string {
	result := b.PluginName + "." + b.FeatureKey

	// >>> 数据演变示例
	// 1. score+check_in -> score.check_in。
	// 2. ping+ping -> ping.ping。
	return result
}

// Normalize 标准化用户命令以执行重复检测和匹配。
// @param input：原始命令；prefix：当前系统命令前缀。
// @returns 去前缀、合并空白并转小写的命令，或空命令/超长错误。
// ⚠️副作用说明：无。
func Normalize(input string, prefix string) (string, error) {
	value := strings.TrimSpace(input)
	// [决策理由] 前缀属于系统入口语法，不应参与命令唯一性。
	if prefix != "" && strings.HasPrefix(value, prefix) {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	// [决策理由] 空命令无法注册或匹配具体功能。
	if value == "" {
		return "", errors.New("命令不能为空")
	}
	// [决策理由] 与数据库 VARCHAR(128) 保持一致，并按 Unicode 字符而非字节计数。
	if utf8.RuneCountInString(value) > 128 {
		return "", errors.New("命令长度不能超过 128 个字符")
	}

	// >>> 数据演变示例
	// 1. " /每日   签到 " + prefix=/ -> "每日 签到"。
	// 2. " PING " + prefix=/ -> "ping"。
	return value, nil
}

// ExtractArguments 从已匹配命令的原始输入中提取参数并保留参数大小写。
// @param input：用户原始消息；prefix：系统命令前缀；normalizedCommand：Registry已匹配的标准命令。
// @returns 合并空白后的参数文本；无参数或输入不一致时为空。
// ⚠️副作用说明：无。
func ExtractArguments(input string, prefix string, normalizedCommand string) string {
	value := strings.TrimSpace(input)
	// [决策理由] 系统前缀不属于触发词或业务参数，必须先移除。
	if prefix != "" && strings.HasPrefix(value, prefix) {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	inputFields := strings.Fields(value)
	commandFields := strings.Fields(normalizedCommand)
	// [决策理由] 精确命令没有剩余字段；异常短输入也应安全返回空参数。
	if len(inputFields) <= len(commandFields) {
		return ""
	}
	// [决策理由] 调用方通常来自Registry匹配，但独立使用时仍需防止错误命令截取用户文本。
	for index, commandField := range commandFields {
		// [决策理由] 命令匹配不区分大小写，参数提取必须采用相同语义。
		if strings.ToLower(inputFields[index]) != strings.ToLower(commandField) {
			return ""
		}
	}
	result := strings.Join(inputFields[len(commandFields):], " ")

	// >>> 数据演变示例
	// 1. "/echo Hello World"+prefix=/+command=echo -> 去命令 -> "Hello World"。
	// 2. "回声 文本"+command="自定义 回声" -> 命令不一致 -> ""。
	return result
}
