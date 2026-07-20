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

// AdminResource 描述通用 WebUI 可渲染的插件业务资源。
type AdminResource struct {
	Key         string        `json:"key"`
	DisplayName string        `json:"display_name"`
	Description string        `json:"description,omitempty"`
	Fields      []ConfigField `json:"fields"`
	CanCreate   bool          `json:"can_create"`
	CanUpdate   bool          `json:"can_update"`
	CanDelete   bool          `json:"can_delete"`
	MaxPageSize int           `json:"max_page_size"`
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
	// [决策理由] 复用已验证的字段词汇，避免任意前端组件或未知类型。
	if err := (ConfigSchema{Fields: r.Fields}).Validate(); err != nil {
		return fmt.Errorf("资源 %s 字段无效: %w", r.Key, err)
	}
	for _, field := range r.Fields {
		// [决策理由] MVP 通用资源表格只实现文本、多行文本和布尔控件，必须拒绝未安全渲染的类型及secret列表泄露。
		if field.Type != FieldString && field.Type != FieldMultiline && field.Type != FieldBoolean {
			return fmt.Errorf("资源 %s 字段 %s 类型 %s 不受支持", r.Key, field.Key, field.Type)
		}
	}

	// >>> 数据演变示例
	// 1. rules+合法字段+max50 -> 全部校验通过 -> nil。
	// 2. key="rules/table" -> URL不安全 -> 返回声明错误。
	return nil
}
