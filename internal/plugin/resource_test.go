// 📌 影响范围：仅构造内存插件与资源处理器，不访问数据库或网络。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	candidate := &resourceTestPlugin{registrations: []AdminResourceRegistration{{Descriptor: AdminResource{Key: "rules", DisplayName: "规则", Fields: []ResourceField{{Key: "keyword", DisplayName: "关键词", Type: ResourceFieldString}, {Key: "status", DisplayName: "状态", Type: ResourceFieldEnum, Options: []string{"pending", "confirmed"}}}, CanCreate: true, MaxPageSize: 50}, Handler: resourceTestHandler{}}}}
	// [决策理由] 测试前必须成功注册 Provider。
	if err := manager.Register(candidate); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	resources, err := manager.AdminResources("resource_test")
	// [决策理由] 合法声明应被完整返回。
	if err != nil || len(resources) != 1 || resources[0].Key != "rules" || resources[0].Fields[1].Type != ResourceFieldEnum {
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

// TestManagerAdminResourcesPreservesHidden 验证隐藏标记仅影响前端入口，不移除服务端资源能力。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：注册内存插件。
func TestManagerAdminResourcesPreservesHidden(t *testing.T) {
	manager := NewManager(nil)
	candidate := &resourceTestPlugin{registrations: []AdminResourceRegistration{{Descriptor: AdminResource{Key: "trials", DisplayName: "试判", Fields: []ResourceField{{Key: "text", DisplayName: "文本", Type: ResourceFieldMultiline}}, CanCreate: true, Hidden: true, MaxPageSize: 50}, Handler: resourceTestHandler{}}}}
	// [决策理由] 隐藏资源仍需正常注册，供专用WebUI组件调用POST端点。
	if err := manager.Register(candidate); err != nil {
		t.Fatalf("Register() error=%v", err)
	}
	resources, err := manager.AdminResources("resource_test")
	// [决策理由] 管理API必须保留hidden元数据，前端才能过滤且专用端点仍可路由。
	if err != nil || len(resources) != 1 || !resources[0].Hidden {
		t.Fatalf("AdminResources()=%+v,error=%v", resources, err)
	}
	_, _, err = manager.AdminResourceHandler("resource_test", "trials")
	// [决策理由] hidden 不能等同禁用，否则专用文本试判面板会失效。
	if err != nil {
		t.Fatalf("AdminResourceHandler() error=%v", err)
	}

	// >>> 数据演变示例
	// 1. hidden trials -> 描述保留hidden=true且handler可用。
	// 2. 通用前端读取描述 -> 过滤入口但不影响POST。
}

// TestAdminResourceDateTime 验证资源时间字段只能作为只读展示语义。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestAdminResourceDateTime(t *testing.T) {
	valid := AdminResource{Key: "samples", DisplayName: "样本", Fields: []ResourceField{{Key: "created_at", DisplayName: "创建时间", Type: ResourceFieldDateTime}}, ReadOnlyFields: []string{"created_at"}, MaxPageSize: 50}
	// [决策理由] 只读 datetime 是资源表格支持的合法展示字段。
	if err := valid.Validate(); err != nil {
		t.Fatalf("只读 datetime 校验失败: %v", err)
	}
	tests := []struct {
		name     string
		field    ResourceField
		readOnly []string
	}{
		{name: "可编辑时间", field: ResourceField{Key: "created_at", DisplayName: "创建时间", Type: ResourceFieldDateTime}},
		{name: "必填时间", field: ResourceField{Key: "created_at", DisplayName: "创建时间", Type: ResourceFieldDateTime, Required: true}, readOnly: []string{"created_at"}},
		{name: "默认时间", field: ResourceField{Key: "created_at", DisplayName: "创建时间", Type: ResourceFieldDateTime, Default: json.RawMessage(`"2026-07-21T08:00:00Z"`)}, readOnly: []string{"created_at"}},
	}
	for _, test := range tests {
		candidate := AdminResource{Key: "samples", DisplayName: "样本", Fields: []ResourceField{test.field}, ReadOnlyFields: test.readOnly, MaxPageSize: 50}
		// [决策理由] datetime 进入用户载荷或声明表单约束都必须被拒绝。
		if err := candidate.Validate(); err == nil {
			t.Errorf("%s: Validate() 未返回错误", test.name)
		}
	}
	config := ConfigSchema{Fields: []ConfigField{{Key: "created_at", DisplayName: "创建时间", Type: FieldType("datetime")}}}
	// [决策理由] 配置 Schema 必须继续拒绝资源专用 datetime 类型。
	if err := config.Validate(); err == nil {
		t.Fatal("配置 Schema 接受了资源专用 datetime")
	}

	// >>> 数据演变示例
	// 1. readonly datetime -> Validate nil。
	// 2. editable/required/default datetime或配置datetime -> Validate error。
}

// TestNormalizeResourceData 验证资源载荷只接受可编辑字段并保留严格类型校验。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestNormalizeResourceData(t *testing.T) {
	descriptor := AdminResource{Key: "rules", DisplayName: "规则", Fields: []ResourceField{{Key: "keyword", DisplayName: "关键词", Type: ResourceFieldString, Required: true}, {Key: "enabled", DisplayName: "启用", Type: ResourceFieldBoolean, Default: json.RawMessage(`true`)}, {Key: "created_at", DisplayName: "创建时间", Type: ResourceFieldDateTime}}, ReadOnlyFields: []string{"created_at"}, MaxPageSize: 50}
	normalized, err := NormalizeResourceData(descriptor, json.RawMessage(`{"keyword":"hello"}`))
	// [决策理由] 合法可编辑字段应补齐默认值并通过规范化。
	if err != nil || !strings.Contains(string(normalized), `"enabled":true`) {
		t.Fatalf("NormalizeResourceData()=%s,error=%v", normalized, err)
	}
	tests := []string{`{"keyword":"hello","created_at":"2026-07-21T08:00:00Z"}`, `{"keyword":"hello","enabled":"true"}`, `{"created_at":"2026-07-21T08:00:00Z"}`}
	for _, raw := range tests {
		_, err := NormalizeResourceData(descriptor, json.RawMessage(raw))
		// [决策理由] 只读字段、错误类型和缺失必填都必须在插件处理器前拒绝。
		if err == nil {
			t.Errorf("NormalizeResourceData(%s) 未返回错误", raw)
		}
	}

	// >>> 数据演变示例
	// 1. {keyword:"hello"} -> 补enabled=true -> 合法对象。
	// 2. 提交created_at或错误boolean -> 输入错误。
}
