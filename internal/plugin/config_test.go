// 📌 影响范围：仅测试插件配置契约的纯内存校验、合并与脱敏；不访问数据库或外部服务。
package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

// testConfigSchema 构造覆盖 MVP 全部字段类型的测试 Schema。
// @param 无。
// @returns 合法测试 Schema。
// ⚠️副作用说明：分配测试切片。
func testConfigSchema() ConfigSchema {
	result := ConfigSchema{Fields: []ConfigField{
		{Key: "name", DisplayName: "名称", Type: FieldString, Required: true},
		{Key: "template", DisplayName: "模板", Type: FieldMultiline},
		{Key: "timeout", DisplayName: "超时", Type: FieldInteger, Default: json.RawMessage(`30`)},
		{Key: "enabled", DisplayName: "启用", Type: FieldBoolean, Default: json.RawMessage(`true`)},
		{Key: "mode", DisplayName: "模式", Type: FieldEnum, Options: []string{"fast", "safe"}, Required: true},
		{Key: "token", DisplayName: "令牌", Type: FieldSecret, Required: true},
	}}

	// >>> 数据演变示例
	// 1. 调用一次 -> 六种字段类型 -> 独立Schema值。
	// 2. 再次调用 -> 新切片 -> 不共享字段修改。
	return result
}

// decodeTestConfig 将测试结果转换为通用对象便于断言。
// @param t：测试上下文；raw：规范配置 JSON。
// @returns 解码后的字段映射。
// ⚠️副作用说明：失败时终止当前测试。
func decodeTestConfig(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var result map[string]any
	// [决策理由] 被测函数承诺返回合法 JSON，对解码失败应立即报告。
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解码配置失败: %v", err)
	}

	// >>> 数据演变示例
	// 1. {"enabled":true} -> map[enabled:true]。
	// 2. 非法JSON -> Fatal终止测试。
	return result
}

// TestConfigSchemaValidate 验证字段描述、选项和默认值约束。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestConfigSchemaValidate(t *testing.T) {
	valid := testConfigSchema()
	// [决策理由] 覆盖全部 MVP 类型的基线 Schema 必须合法。
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法 Schema 校验失败: %v", err)
	}
	tests := []struct {
		name  string
		field ConfigField
		want  string
	}{
		{name: "非法键", field: ConfigField{Key: "Bad-Key", DisplayName: "坏键", Type: FieldString}, want: "无效"},
		{name: "未知类型", field: ConfigField{Key: "value", DisplayName: "值", Type: "object"}, want: "不受支持"},
		{name: "枚举无选项", field: ConfigField{Key: "mode", DisplayName: "模式", Type: FieldEnum}, want: "至少"},
		{name: "枚举重复", field: ConfigField{Key: "mode", DisplayName: "模式", Type: FieldEnum, Options: []string{"a", "a"}}, want: "重复"},
		{name: "错误默认类型", field: ConfigField{Key: "count", DisplayName: "数量", Type: FieldInteger, Default: json.RawMessage(`1.5`)}, want: "64 位整数"},
		{name: "敏感默认值", field: ConfigField{Key: "token", DisplayName: "令牌", Type: FieldSecret, Default: json.RawMessage(`"x"`)}, want: "不能声明默认值"},
	}
	for _, test := range tests {
		err := (ConfigSchema{Fields: []ConfigField{test.field}}).Validate()
		// [决策理由] 每种不安全或不可渲染描述都应返回可定位错误。
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error=%v, want contains %q", test.name, err, test.want)
		}
	}
	duplicate := ConfigSchema{Fields: []ConfigField{{Key: "name", DisplayName: "名称", Type: FieldString}, {Key: "name", DisplayName: "别名", Type: FieldString}}}
	// [决策理由] 重复字段必须单独覆盖，避免后项覆盖前项。
	if err := duplicate.Validate(); err == nil {
		t.Fatal("重复字段未返回错误")
	}

	// >>> 数据演变示例
	// 1. 六种合法字段 -> Validate -> nil。
	// 2. secret默认明文 -> Validate -> 返回敏感默认值错误。
}

// TestStructuredJSONConfigFields 验证结构化编辑器字段保持字符串存储并严格校验内部数组。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestStructuredJSONConfigFields(t *testing.T) {
	tests := []struct {
		name      string
		fieldType FieldType
		value     string
		valid     bool
	}{
		{name: "字符串列表", fieldType: FieldStringListJSON, value: `["广告","引流"]`, valid: true},
		{name: "权重词条", fieldType: FieldWeightedTermsJSON, value: `[{"text":"免费","weight":25}]`, valid: true},
		{name: "组合规则", fieldType: FieldCombinationRulesJSON, value: `[{"terms":["免费","加群"],"bonus":20}]`, valid: true},
		{name: "未知字段", fieldType: FieldWeightedTermsJSON, value: `[{"text":"免费","weight":25,"extra":1}]`},
		{name: "错误根类型", fieldType: FieldStringListJSON, value: `{"text":"广告"}`},
		{name: "空根节点", fieldType: FieldStringListJSON, value: `null`},
		{name: "尾随值", fieldType: FieldCombinationRulesJSON, value: `[] {}`},
	}
	for _, test := range tests {
		field := ConfigField{Key: "rules", DisplayName: "规则", Type: test.fieldType}
		raw, _ := json.Marshal(test.value)
		err := validateFieldValue(field, raw)
		// [决策理由] 内部JSON形状必须与字段类型的稳定契约一致。
		if (err == nil) != test.valid {
			t.Errorf("%s: error=%v valid=%v", test.name, err, test.valid)
		}
	}

	// >>> 数据演变示例
	// 1. 外层字符串+合法规则数组 -> nil并保持字符串存储。
	// 2. 未知字段/错误根/尾随值 -> error。
}

// TestNormalizeConfig 验证严格字段、默认值、required 和类型校验。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestNormalizeConfig(t *testing.T) {
	schema := testConfigSchema()
	normalized, err := NormalizeConfig(schema, json.RawMessage(`{"name":"bot","mode":"safe","token":"secret"}`))
	// [决策理由] 合法最小输入应补齐默认值并通过。
	if err != nil {
		t.Fatalf("NormalizeConfig() error = %v", err)
	}
	values := decodeTestConfig(t, normalized)
	// [决策理由] integer 由 JSON 解码为 float64，值仍应精确等于声明默认值。
	if values["timeout"] != float64(30) || values["enabled"] != true {
		t.Fatalf("默认值未补齐: %s", normalized)
	}
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "未知字段", raw: `{"name":"bot","mode":"safe","token":"x","extra":1}`, want: "未知"},
		{name: "缺少必填", raw: `{"mode":"safe","token":"x"}`, want: "name"},
		{name: "错误整数", raw: `{"name":"bot","mode":"safe","token":"x","timeout":1.2}`, want: "整数"},
		{name: "错误布尔", raw: `{"name":"bot","mode":"safe","token":"x","enabled":"true"}`, want: "布尔"},
		{name: "枚举越界", raw: `{"name":"bot","mode":"other","token":"x"}`, want: "允许选项"},
		{name: "空值", raw: `{"name":null,"mode":"safe","token":"x"}`, want: "null"},
		{name: "非对象", raw: `[]`, want: "对象"},
		{name: "尾随值", raw: `{} {}`, want: "多余"},
	}
	for _, test := range tests {
		_, err := NormalizeConfig(schema, json.RawMessage(test.raw))
		// [决策理由] 严格服务端校验必须拒绝每种歧义或类型错误输入。
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error=%v, want contains %q", test.name, err, test.want)
		}
	}

	// >>> 数据演变示例
	// 1. 最小合法对象 -> 补timeout=30和enabled=true -> 完整配置。
	// 2. 带extra字段 -> 严格字段检查 -> 返回错误。
}

// TestMergeConfigUpdateAndRedact 验证敏感字段省略保留、替换及输出脱敏。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestMergeConfigUpdateAndRedact(t *testing.T) {
	schema := testConfigSchema()
	current := json.RawMessage(`{"name":"old","mode":"safe","token":"old-secret","timeout":10,"enabled":false}`)
	merged, err := MergeConfigUpdate(schema, current, json.RawMessage(`{"name":"new","mode":"fast"}`))
	// [决策理由] secret 省略必须保留，其他省略字段则按照完整更新语义恢复默认值。
	if err != nil {
		t.Fatalf("MergeConfigUpdate() error = %v", err)
	}
	values := decodeTestConfig(t, merged)
	// [决策理由] 保留 secret 是 write-only 表单往返更新的核心安全语义。
	if values["token"] != "old-secret" || values["timeout"] != float64(30) || values["enabled"] != true {
		t.Fatalf("合并结果错误: %s", merged)
	}
	replaced, err := MergeConfigUpdate(schema, current, json.RawMessage(`{"name":"new","mode":"fast","token":"new-secret"}`))
	// [决策理由] 显式提供新 secret 时必须允许轮换凭据。
	if err != nil || decodeTestConfig(t, replaced)["token"] != "new-secret" {
		t.Fatalf("替换 secret 失败: raw=%s error=%v", replaced, err)
	}
	redacted, err := RedactConfig(schema, merged)
	// [决策理由] 对外读取必须成功删除敏感字段。
	if err != nil {
		t.Fatalf("RedactConfig() error = %v", err)
	}
	public := decodeTestConfig(t, redacted)
	_, leaked := public["token"]
	// [决策理由] secret 即使存在内部快照也不得出现在输出对象。
	if leaked || public["name"] != "new" {
		t.Fatalf("脱敏结果错误: %s", redacted)
	}
	_, err = RedactConfig(schema, json.RawMessage(`{"name":"bot","mode":"safe","token":"x","unknown_secret":"leak"}`))
	// [决策理由] 脱敏不能放行 Schema 未声明字段，否则未知敏感数据可能原样返回。
	if err == nil {
		t.Fatal("含未知字段的配置脱敏未返回错误")
	}

	// >>> 数据演变示例
	// 1. 省略token更新 -> 保留old-secret -> 输出时删除token。
	// 2. 显式token=new-secret -> 内部替换 -> 输出仍删除token。
}

// TestRedactConfigAllowsMissingRequiredForBootstrap 验证首次配置前仍可安全读取公开快照。
// @param t：测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestRedactConfigAllowsMissingRequiredForBootstrap(t *testing.T) {
	schema := ConfigSchema{Fields: []ConfigField{{Key: "endpoint", DisplayName: "地址", Type: FieldString, Required: true}, {Key: "token", DisplayName: "令牌", Type: FieldSecret, Required: true}}}
	redacted, err := RedactConfig(schema, json.RawMessage(`{}`))
	// [决策理由] disabled 新插件的空配置必须可读取，WebUI 才能呈现首次填写表单。
	if err != nil || string(redacted) != `{}` {
		t.Fatalf("RedactConfig() = %s, %v", redacted, err)
	}
	_, err = NormalizeConfig(schema, json.RawMessage(`{}`))
	// [决策理由] 真正应用前仍必须拒绝缺少 required 的不完整快照。
	if err == nil {
		t.Fatal("NormalizeConfig() error = nil")
	}

	// >>> 数据演变示例
	// 1. disabled+{} -> Redact读取{} -> WebUI可首次填写。
	// 2. enabled前Normalize{} -> required缺失 -> 拒绝应用。
}
