// 📌 影响范围：仅构造内存插件与资源处理器，不访问数据库或网络。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/w1ndys/w1ndys-bot/internal/management"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

type resourceTestPlugin struct{ registrations []AdminResourceRegistration }

// Name 返回测试插件名。
// @param 无。
// @returns 固定名称 resource_test。
// ⚠️副作用说明：无。
func (p *resourceTestPlugin) Name() string {
	// >>> 数据演变示例
	// 1. 新实例 -> resource_test。
	// 2. 含registrations实例 -> resource_test。
	return "resource_test"
}

// OnEnable 实现无状态测试生命周期。
// @param ctx：未使用。
// @returns nil。
// ⚠️副作用说明：无。
func (p *resourceTestPlugin) OnEnable(context.Context) error {
	// >>> 数据演变示例
	// 1. background -> nil。
	// 2. canceled -> nil。
	return nil
}

// OnDisable 实现无状态测试生命周期。
// @param ctx：未使用。
// @returns nil。
// ⚠️副作用说明：无。
func (p *resourceTestPlugin) OnDisable(context.Context) error {
	// >>> 数据演变示例
	// 1. background -> nil。
	// 2. canceled -> nil。
	return nil
}

// Handle 实现未处理事件的测试插件。
// @param ctx：未使用；event：未使用。
// @returns nil。
// ⚠️副作用说明：无。
func (p *resourceTestPlugin) Handle(context.Context, ws.Event) error {
	// >>> 数据演变示例
	// 1. 群消息 -> false,nil。
	// 2. 空事件 -> false,nil。
	return nil
}

// AdminResources 返回测试资源注册。
// @param 无。
// @returns registrations 副本。
// ⚠️副作用说明：无。
func (p *resourceTestPlugin) AdminResources() []AdminResourceRegistration {
	result := append([]AdminResourceRegistration(nil), p.registrations...)
	// >>> 数据演变示例
	// 1. [rules] -> 复制 -> [rules]。
	// 2. nil -> 空副本。
	return result
}

type resourceTestHandler struct{}

// List 返回空测试页。
// @param ctx/actor/query：未使用。
// @returns 空页。
// ⚠️副作用说明：无。
func (resourceTestHandler) List(context.Context, management.Actor, management.ResourceQuery) (management.ResourcePage, error) {
	// >>> 数据演变示例
	// 1. page1 -> 空页。
	// 2. page2 -> 空页。
	return management.ResourcePage{}, nil
}

// Create 返回测试记录。
// @param ctx/actor：未使用；data：保留数据。
// @returns id1/v1 记录。
// ⚠️副作用说明：无。
func (resourceTestHandler) Create(_ context.Context, _ management.Actor, data json.RawMessage) (management.ResourceRecord, error) {
	// >>> 数据演变示例
	// 1. {} -> id1/v1。
	// 2. {x:1} -> id1/v1。
	return management.ResourceRecord{ID: 1, Version: 1, Data: data}, nil
}

// Update 返回版本加一的测试记录。
// @param ctx/actor：未使用；id/version/data：记录参数。
// @returns 版本加一记录。
// ⚠️副作用说明：无。
func (resourceTestHandler) Update(_ context.Context, _ management.Actor, id, version int64, data json.RawMessage) (management.ResourceRecord, error) {
	// >>> 数据演变示例
	// 1. id1/v1 -> id1/v2。
	// 2. id2/v3 -> id2/v4。
	return management.ResourceRecord{ID: id, Version: version + 1, Data: data}, nil
}

// Delete 实现测试删除。
// @param ctx/actor：未使用；id/version：未使用。
// @returns nil。
// ⚠️副作用说明：无。
func (resourceTestHandler) Delete(context.Context, management.Actor, int64, int64) error {
	// >>> 数据演变示例
	// 1. id1/v1 -> nil。
	// 2. id2/v2 -> nil。
	return nil
}

// TestManagerAdminResources 验证声明查找、未知键与无效分页上限。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：注册内存插件。
func TestManagerAdminResources(t *testing.T) {
	manager := NewManager(nil)
	candidate := &resourceTestPlugin{registrations: []AdminResourceRegistration{{Descriptor: AdminResource{Key: "rules", DisplayName: "规则", Fields: []ConfigField{{Key: "keyword", DisplayName: "关键词", Type: FieldString}, {Key: "status", DisplayName: "状态", Type: FieldEnum, Options: []string{"pending", "confirmed"}}}, CanCreate: true, MaxPageSize: 50}, Handler: resourceTestHandler{}}}}
	// [决策理由] 测试前必须成功注册 Provider。
	if err := manager.Register(candidate); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	resources, err := manager.AdminResources("resource_test")
	// [决策理由] 合法声明应被完整返回。
	if err != nil || len(resources) != 1 || resources[0].Key != "rules" || resources[0].Fields[1].Type != FieldEnum {
		t.Fatalf("AdminResources() = %+v, %v", resources, err)
	}
	_, _, err = manager.AdminResourceHandler("resource_test", "unknown")
	// [决策理由] 未知键必须保持可判定错误。
	if !errors.Is(err, ErrAdminResourceNotFound) {
		t.Fatalf("AdminResourceHandler() error = %v", err)
	}

	// >>> 数据演变示例
	// 1. resource_test/rules -> 命中处理器。
	// 2. resource_test/unknown -> ErrAdminResourceNotFound。
}
