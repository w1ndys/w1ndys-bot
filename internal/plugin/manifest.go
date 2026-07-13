// 📌 影响范围：定义编译时插件元数据并执行纯内存校验；不访问数据库或外部变量。
package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	Name          string
	DisplayName   string
	Description   string
	Version       string
	SchemaVersion int
	Priority      int
	System        bool
	Features      []FeatureManifest
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
	// [决策理由] 版本用于判断二进制元数据变化，不能为空。
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("插件版本不能为空")
	}
	// [决策理由] Schema 版本必须从 1 开始，便于配置升级判断。
	if m.SchemaVersion < 1 {
		return errors.New("插件 Schema 版本必须大于 0")
	}
	seenFeatures := make(map[string]struct{}, len(m.Features))
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
	}

	// >>> 数据演变示例
	// 1. score + [check_in,rank] -> 标识唯一且完整 -> nil。
	// 2. score + [rank,rank] -> 重复检测 -> 返回错误。
	return nil
}
