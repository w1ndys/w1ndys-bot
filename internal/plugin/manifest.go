// 📌 影响范围：定义编译时插件元数据并执行纯内存校验；不访问数据库或外部变量。
package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	commandregistry "github.com/w1ndys/w1ndys-bot/internal/command"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// RolePermissions 表示功能对各 QQ 身份的默认权限。
type RolePermissions struct {
	SuperAdmin bool `json:"super_admin"`
	GroupOwner bool `json:"group_owner"`
	GroupAdmin bool `json:"group_admin"`
	Member     bool `json:"member"`
}

// FeatureManifest 描述插件中的一项稳定功能。
type FeatureManifest struct {
	Key                string
	DisplayName        string
	Description        string
	DefaultCommands    []string
	DefaultPermissions RolePermissions
}

// Manifest 描述编译进二进制的插件及其管理元数据。
type Manifest struct {
	Name              string
	DisplayName       string
	Description       string
	Priority          int
	System            bool
	GroupControllable bool
	Features          []FeatureManifest
}

// Validate 校验插件及功能标识的稳定性和唯一性。
// @param 无。
// @returns 首个 Manifest 结构错误。
// ⚠️副作用说明：无。
func (m Manifest) Validate() error {
	// [决策理由] 插件名进入主键、日志与配置引用，必须使用稳定机器标识格式。
	if !identifierPattern.MatchString(m.Name) {
		return fmt.Errorf("无效插件名 %q", m.Name)
	}
	// [决策理由] 展示名称为空会使 WebUI 无法提供可读插件列表。
	if strings.TrimSpace(m.DisplayName) == "" {
		return errors.New("插件展示名称不能为空")
	}
	// [决策理由] 插件说明是管理界面识别能力和风险的必要信息，不能以空白占位。
	if strings.TrimSpace(m.Description) == "" {
		return errors.New("插件说明不能为空")
	}
	seenFeatures := make(map[string]struct{}, len(m.Features))
	seenCommands := make(map[string]string)
	for _, feature := range m.Features {
		// [决策理由] feature_key 是命令、权限与审计的永久引用，必须采用稳定标识格式。
		if !identifierPattern.MatchString(feature.Key) {
			return fmt.Errorf("插件 %s 包含无效功能标识 %q", m.Name, feature.Key)
		}
		// [决策理由] 同一插件的 feature_key 必须唯一，避免权限和命令映射歧义。
		if _, exists := seenFeatures[feature.Key]; exists {
			return fmt.Errorf("插件 %s 的功能 %q 重复", m.Name, feature.Key)
		}
		seenFeatures[feature.Key] = struct{}{}
		// [决策理由] 功能展示名称是动态管理界面的必要字段。
		if strings.TrimSpace(feature.DisplayName) == "" {
			return fmt.Errorf("插件 %s 的功能 %s 展示名称为空", m.Name, feature.Key)
		}
		// [决策理由] 功能说明用于权限配置和功能审计，空说明会让管理员无法判断启用影响。
		if strings.TrimSpace(feature.Description) == "" {
			return fmt.Errorf("插件 %s 的功能 %s 说明为空", m.Name, feature.Key)
		}
		// [决策理由] 可路由功能至少需要一个默认入口，避免同步后形成不可触发的孤立功能。
		if len(feature.DefaultCommands) == 0 {
			return fmt.Errorf("插件 %s 的功能 %s 至少需要一个默认命令", m.Name, feature.Key)
		}
		for _, defaultCommand := range feature.DefaultCommands {
			normalized, err := commandregistry.Normalize(defaultCommand, "")
			// [决策理由] 默认命令必须在数据库事务前通过与运行路由相同的格式和长度校验。
			if err != nil {
				return fmt.Errorf("插件 %s 的功能 %s 默认命令 %q 无效: %w", m.Name, feature.Key, defaultCommand, err)
			}
			owner, exists := seenCommands[normalized]
			// [决策理由] 标准化后相同的命令会争用全局默认路由，必须在编译时注册阶段拒绝。
			if exists {
				return fmt.Errorf("插件 %s 的默认命令 %q 标准化后重复（功能 %s 与 %s）", m.Name, normalized, owner, feature.Key)
			}
			seenCommands[normalized] = feature.Key
		}
	}

	// >>> 数据演变示例
	// 1. observer + 空Features -> 作为纯广播观察插件通过校验。
	// 2. score + check_in[" PING "],rank[ping] -> 标准化均为ping -> 返回重复错误。
	return nil
}
