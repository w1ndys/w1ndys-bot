// 📌 影响范围：引用 management 稳定 DTO；定义插件业务资源的声明式管理契约。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

// ErrResourceRecordNotFound 表示插件业务记录不存在。
var ErrResourceRecordNotFound = management.ErrResourceRecordNotFound

// ErrInvalidResourceData 表示插件业务数据未通过领域校验。
var ErrInvalidResourceData = management.ErrInvalidResourceData

// ErrResourceConflict 表示唯一约束或乐观锁版本冲突。
var ErrResourceConflict = management.ErrResourceConflict

// ResourceFieldType 是通用资源表格明确支持的字段类型。
type ResourceFieldType string

const (
	ResourceFieldString    ResourceFieldType = "string"
	ResourceFieldMultiline ResourceFieldType = "multiline"
	ResourceFieldBoolean   ResourceFieldType = "boolean"
	ResourceFieldEnum      ResourceFieldType = "enum"
	ResourceFieldDateTime  ResourceFieldType = "datetime"
)

// ResourceField 描述资源列表与编辑表单中的稳定字段。
type ResourceField struct {
	Key         string            `json:"key"`
	DisplayName string            `json:"display_name"`
	Description string            `json:"description,omitempty"`
	Type        ResourceFieldType `json:"type"`
	Required    bool              `json:"required"`
	Default     json.RawMessage   `json:"default,omitempty"`
	Options     []string          `json:"options,omitempty"`
}

// AdminResource 描述通用 WebUI 可渲染的插件业务资源。
type AdminResource struct {
	Key            string          `json:"key"`
	DisplayName    string          `json:"display_name"`
	Description    string          `json:"description,omitempty"`
	Fields         []ResourceField `json:"fields"`
	ReadOnlyFields []string        `json:"read_only_fields,omitempty"`
	CanCreate      bool            `json:"can_create"`
	CanUpdate      bool            `json:"can_update"`
	CanDelete      bool            `json:"can_delete"`
	Hidden         bool            `json:"hidden"`
	MaxPageSize    int             `json:"max_page_size"`
}

// AdminResourceRegistration 将安全的声明与插件自有处理器绑定。
type AdminResourceRegistration struct {
	Descriptor AdminResource
	Handler    AdminResourceHandler
}

// AdminResourceProvider 是插件可选实现的业务资源声明能力。
type AdminResourceProvider interface {
	AdminResources() []AdminResourceRegistration
}

// AdminResourceHandler 由插件实现固定 SQL、领域校验、事务与审计。
type AdminResourceHandler interface {
	List(context.Context, management.Actor, management.ResourceQuery) (management.ResourcePage, error)
	Create(context.Context, management.Actor, json.RawMessage) (management.ResourceRecord, error)
	Update(context.Context, management.Actor, int64, int64, json.RawMessage) (management.ResourceRecord, error)
	Delete(context.Context, management.Actor, int64, int64) error
}

// NormalizeResourceData 严格规范化资源新增或编辑载荷。
// @param descriptor：已验证的资源声明；raw：客户端提交的JSON对象。
// @returns 仅包含可编辑字段且完成默认值补齐的规范JSON，或输入错误。
// ⚠️副作用说明：无。
func NormalizeResourceData(descriptor AdminResource, raw json.RawMessage) (json.RawMessage, error) {
	readOnly := make(map[string]struct{}, len(descriptor.ReadOnlyFields))
	for _, key := range descriptor.ReadOnlyFields {
		readOnly[key] = struct{}{}
	}
	fields := make([]ConfigField, 0, len(descriptor.Fields))
	for _, field := range descriptor.Fields {
		_, blocked := readOnly[field.Key]
		// [决策理由] 只读字段由后端权威生成，客户端提交必须作为未知字段拒绝。
		if blocked || field.Type == ResourceFieldDateTime {
			continue
		}
		fields = append(fields, ConfigField{Key: field.Key, DisplayName: field.DisplayName, Description: field.Description, Type: FieldType(field.Type), Required: field.Required, Default: field.Default, Options: field.Options})
	}
	result, err := NormalizeConfig(ConfigSchema{Fields: fields}, raw)

	// >>> 数据演变示例
	// 1. fields=[keyword,created_at只读]+{keyword:"a"} -> {keyword:"a"}。
	// 2. 客户端提交created_at -> 未知字段错误。
	return result, err
}

// Validate 校验资源键、字段、能力与分页上限。
// @param 无。
// @returns 首个资源声明错误。
// ⚠️副作用说明：无。
func (r AdminResource) Validate() error {
	// [决策理由] 资源键进入 URL 路由与运行时索引，必须是稳定机器标识。
	if !identifierPattern.MatchString(r.Key) {
		return fmt.Errorf("无效资源键 %q", r.Key)
	}
	// [决策理由] 通用页面需要可读标题区分业务资源。
	if strings.TrimSpace(r.DisplayName) == "" {
		return fmt.Errorf("资源 %s 展示名称为空", r.Key)
	}
	// [决策理由] MVP 将分页上限固定在可控范围，防止插件声明绕过平台资源边界。
	if r.MaxPageSize < 1 || r.MaxPageSize > 100 {
		return fmt.Errorf("资源 %s max_page_size 必须在 1 至 100 之间", r.Key)
	}
	fieldKeys := make(map[string]struct{}, len(r.Fields))
	for _, field := range r.Fields {
		// [决策理由] 字段键进入JSON和表单状态，必须使用稳定机器标识。
		if !identifierPattern.MatchString(field.Key) {
			return fmt.Errorf("资源 %s 字段键 %q 无效", r.Key, field.Key)
		}
		// [决策理由] 重复字段会让表格和提交载荷产生歧义。
		if _, exists := fieldKeys[field.Key]; exists {
			return fmt.Errorf("资源 %s 字段 %s 重复", r.Key, field.Key)
		}
		fieldKeys[field.Key] = struct{}{}
		// [决策理由] 通用表格需要可读标签。
		if strings.TrimSpace(field.DisplayName) == "" {
			return fmt.Errorf("资源 %s 字段 %s 展示名称为空", r.Key, field.Key)
		}
		// [决策理由] 资源表格只允许已安全渲染的文本、布尔、固定枚举和只读时间。
		if field.Type != ResourceFieldString && field.Type != ResourceFieldMultiline && field.Type != ResourceFieldBoolean && field.Type != ResourceFieldEnum && field.Type != ResourceFieldDateTime {
			return fmt.Errorf("资源 %s 字段 %s 类型 %s 不受支持", r.Key, field.Key, field.Type)
		}
		// [决策理由] 枚举必须提供稳定且非空的唯一选项，其他类型禁止携带选项。
		if err := validateResourceFieldOptions(r.Key, field); err != nil {
			return err
		}
		// [决策理由] datetime 是服务端权威展示值，不能声明为用户必填或表单默认值。
		if field.Type == ResourceFieldDateTime && (field.Required || len(field.Default) > 0) {
			return fmt.Errorf("资源 %s datetime 字段 %s 不能声明 required 或 default", r.Key, field.Key)
		}
		// [决策理由] 普通资源字段默认值必须符合其JSON类型，保持拆分前的严格声明边界。
		if len(field.Default) > 0 {
			configField := ConfigField{Key: field.Key, DisplayName: field.DisplayName, Type: FieldType(field.Type), Options: field.Options}
			if err := validateFieldValue(configField, field.Default); err != nil {
				return fmt.Errorf("资源 %s 字段 %s 默认值无效: %w", r.Key, field.Key, err)
			}
		}
	}
	seenReadOnly := make(map[string]struct{}, len(r.ReadOnlyFields))
	for _, key := range r.ReadOnlyFields {
		// [决策理由] 只读声明必须引用资源已有字段，避免前后端对可编辑集合产生不同理解。
		if _, exists := fieldKeys[key]; !exists {
			return fmt.Errorf("资源 %s 只读字段 %s 不存在", r.Key, key)
		}
		// [决策理由] 重复只读键没有额外语义，通常表示插件描述拼写错误。
		if _, exists := seenReadOnly[key]; exists {
			return fmt.Errorf("资源 %s 只读字段 %s 重复", r.Key, key)
		}
		seenReadOnly[key] = struct{}{}
	}
	for _, field := range r.Fields {
		// [决策理由] datetime 是后端权威时间快照，只允许展示，禁止通用表单提交用户本地时间。
		if field.Type == ResourceFieldDateTime {
			// [决策理由] 未声明只读的 datetime 会进入新增或编辑载荷并破坏 UTC 边界。
			if _, exists := seenReadOnly[field.Key]; !exists {
				return fmt.Errorf("资源 %s datetime 字段 %s 必须声明为只读", r.Key, field.Key)
			}
		}
	}

	// >>> 数据演变示例
	// 1. rules+合法字段+max50 -> 全部校验通过 -> nil。
	// 2. key="rules/table" -> URL不安全 -> 返回声明错误。
	return nil
}

// validateResourceFieldOptions 校验资源枚举及非枚举选项边界。
// @param resourceKey：资源键；field：字段声明。
// @returns 首个选项错误。
// ⚠️副作用说明：无。
func validateResourceFieldOptions(resourceKey string, field ResourceField) error {
	// [决策理由] 非枚举字段携带选项会让客户端产生不一致组件解释。
	if field.Type != ResourceFieldEnum {
		// [决策理由] 任何非枚举选项都应显式拒绝。
		if len(field.Options) > 0 {
			return fmt.Errorf("资源 %s 非枚举字段 %s 不能声明选项", resourceKey, field.Key)
		}
		return nil
	}
	// [决策理由] 空枚举无法形成合法选择。
	if len(field.Options) == 0 {
		return fmt.Errorf("资源 %s 枚举字段 %s 至少需要一个选项", resourceKey, field.Key)
	}
	seen := make(map[string]struct{}, len(field.Options))
	for _, option := range field.Options {
		// [决策理由] 空白或重复选项无法稳定区分。
		if strings.TrimSpace(option) == "" {
			return fmt.Errorf("资源 %s 枚举字段 %s 包含空选项", resourceKey, field.Key)
		}
		// [决策理由] 重复值必须拒绝，避免显示多个相同选择。
		if _, exists := seen[option]; exists {
			return fmt.Errorf("资源 %s 枚举字段 %s 的选项 %q 重复", resourceKey, field.Key, option)
		}
		seen[option] = struct{}{}
	}

	// >>> 数据演变示例
	// 1. enum+[pending,confirmed] -> nil。
	// 2. string+[pending]或enum+[] -> 返回错误。
	return nil
}
