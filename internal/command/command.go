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
