// 📌 影响范围：引用 context 与 encoding/json；仅定义插件配置契约并执行纯内存校验、合并和脱敏。
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// FieldType 是通用 WebUI 明确支持的最小配置字段类型。
type FieldType string

const (
	FieldString               FieldType = "string"
	FieldMultiline            FieldType = "multiline"
	FieldInteger              FieldType = "integer"
	FieldBoolean              FieldType = "boolean"
	FieldEnum                 FieldType = "enum"
	FieldSecret               FieldType = "secret"
	FieldStringListJSON       FieldType = "string_list_json"
	FieldWeightedTermsJSON    FieldType = "weighted_terms_json"
	FieldCombinationRulesJSON FieldType = "combination_rules_json"
)

// ConfigField 描述一个稳定、扁平的插件配置字段。
type ConfigField struct {
	Key         string          `json:"key"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description,omitempty"`
	Type        FieldType       `json:"type"`
	Required    bool            `json:"required"`
	Default     json.RawMessage `json:"default,omitempty"`
	Options     []string        `json:"options,omitempty"`
}

// ConfigSchema 描述通用 WebUI 可渲染的插件配置。
type ConfigSchema struct {
	Fields []ConfigField `json:"fields"`
}

// Configurable 是插件可选实现的配置校验与热应用能力。
// ConfigSchema 和 ValidateConfig 必须并发安全且不得修改运行状态；ApplyConfig 必须原子发布不可变快照，返回错误或 panic 时不得改变旧快照。
type Configurable interface {
	ConfigSchema() ConfigSchema
	ValidateConfig(context.Context, json.RawMessage) error
	ApplyConfig(context.Context, json.RawMessage) error
}

// Validate 校验字段描述、枚举选项与默认值的一致性。
// @param 无。
// @returns 首个 Schema 结构错误。
// ⚠️副作用说明：无。
func (s ConfigSchema) Validate() error {
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		// [决策理由] 字段键会进入 JSON、审计与前端表单状态，必须使用稳定机器标识。
		if !identifierPattern.MatchString(field.Key) {
			return fmt.Errorf("无效配置字段键 %q", field.Key)
		}
		// [决策理由] 重复键会让默认值、校验和渲染结果产生歧义。
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("配置字段 %q 重复", field.Key)
		}
		seen[field.Key] = struct{}{}
		// [决策理由] 通用表单必须有可读标签。
		if strings.TrimSpace(field.DisplayName) == "" {
			return fmt.Errorf("配置字段 %s 展示名称为空", field.Key)
		}
		// [决策理由] 仅允许平台明确支持的类型，避免客户端选择任意组件。
		if !validFieldType(field.Type) {
			return fmt.Errorf("配置字段 %s 类型 %q 不受支持", field.Key, field.Type)
		}
		// [决策理由] enum 必须提供无重复的固定选项，其他类型不能携带无效选项。
		if err := validateOptions(field); err != nil {
			return err
		}
		// [决策理由] secret 默认值可能通过 Schema API 泄露，必须禁止声明。
		if field.Type == FieldSecret && len(field.Default) > 0 {
			return fmt.Errorf("secret 字段 %s 不能声明默认值", field.Key)
		}
		// [决策理由] 默认值必须与字段类型一致，避免首次读取生成无效配置。
		if len(field.Default) > 0 {
			if err := validateFieldValue(field, field.Default); err != nil {
				return fmt.Errorf("配置字段 %s 默认值无效: %w", field.Key, err)
			}
		}
	}

	// >>> 数据演变示例
	// 1. enabled:boolean + 默认true -> 类型与默认值一致 -> nil。
	// 2. token:secret + 默认"明文" -> 敏感值可能出现在Schema -> 返回错误。
	return nil
}

// NormalizeConfig 严格校验配置对象，并补齐 Schema 声明的默认值。
// @param schema：配置字段描述；raw：待校验 JSON 对象。
// @returns 字段顺序无关的规范 JSON 对象或错误。
// ⚠️副作用说明：无。
func NormalizeConfig(schema ConfigSchema, raw json.RawMessage) (json.RawMessage, error) {
	result, err := normalizeConfig(schema, raw, true)

	// >>> 数据演变示例
	// 1. schema{enabled默认true}+{} -> values{enabled:true} -> JSON对象。
	// 2. schema{mode枚举[a,b]}+{mode:"c"} -> 枚举校验 -> 返回错误。
	return result, err
}

// normalizeConfig 按用途规范配置，并可允许首次配置前缺少 required 字段。
// @param schema：配置字段描述；raw：待校验对象；requireComplete：是否强制必填字段完整。
// @returns 规范 JSON 对象或错误。
// ⚠️副作用说明：无。
func normalizeConfig(schema ConfigSchema, raw json.RawMessage, requireComplete bool) (json.RawMessage, error) {
	// [决策理由] 无效 Schema 不能用于接受配置，否则服务端与 WebUI 会产生不同解释。
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("配置 Schema 无效: %w", err)
	}
	values, err := decodeConfigObject(raw)
	// [决策理由] 配置只接受单一 JSON 对象，拒绝未知结构和尾随内容。
	if err != nil {
		return nil, err
	}
	fields := make(map[string]ConfigField, len(schema.Fields))
	for _, field := range schema.Fields {
		fields[field.Key] = field
	}
	for key := range values {
		// [决策理由] 拒绝未知字段可防止拼写错误和废弃设置被静默保存。
		if _, exists := fields[key]; !exists {
			return nil, fmt.Errorf("未知配置字段 %q", key)
		}
	}
	for _, field := range schema.Fields {
		value, exists := values[field.Key]
		// [决策理由] 缺失值优先使用声明默认值，以形成可直接应用的完整快照。
		if !exists && len(field.Default) > 0 {
			values[field.Key] = append(json.RawMessage(nil), field.Default...)
			value, exists = values[field.Key]
		}
		// [决策理由] required 字段既无输入也无默认值时不能产生有效运行配置。
		if !exists && field.Required && requireComplete {
			return nil, fmt.Errorf("必填配置字段 %q 缺失", field.Key)
		}
		// [决策理由] 可选且缺失的字段无需类型校验。
		if !exists {
			continue
		}
		// [决策理由] 每个已提供字段必须满足 Schema 类型及枚举约束。
		if err := validateFieldValue(field, value); err != nil {
			return nil, fmt.Errorf("配置字段 %s 无效: %w", field.Key, err)
		}
	}
	result, err := json.Marshal(values)

	// >>> 数据演变示例
	// 1. requireComplete=true+缺required -> 返回错误。
	// 2. requireComplete=false+缺required -> 保留缺失供首次配置表单读取。
	return result, err
}

// MergeConfigUpdate 合并配置更新；省略 secret 时保留旧值，再执行完整严格校验。
// @param schema：配置字段描述；current：当前完整配置；update：新的配置对象。
// @returns 合并并规范化的完整配置或错误。
// ⚠️副作用说明：无。
func MergeConfigUpdate(schema ConfigSchema, current, update json.RawMessage) (json.RawMessage, error) {
	currentValues, err := decodeConfigObject(current)
	// [决策理由] 旧快照损坏时不能盲目复制敏感值，应阻止更新并暴露一致性问题。
	if err != nil {
		return nil, fmt.Errorf("当前配置无效: %w", err)
	}
	updateValues, err := decodeConfigObject(update)
	// [决策理由] 更新必须是严格对象，避免非对象输入绕过字段语义。
	if err != nil {
		return nil, err
	}
	for _, field := range schema.Fields {
		_, supplied := updateValues[field.Key]
		// [决策理由] write-only secret 在表单读取后不可回填，省略代表保留而不是清空。
		if field.Type == FieldSecret && !supplied {
			if existing, exists := currentValues[field.Key]; exists {
				updateValues[field.Key] = append(json.RawMessage(nil), existing...)
			}
		}
	}
	merged, err := json.Marshal(updateValues)
	// [决策理由] 内存 map 理论上可编码；仍传播错误以保持 API 完整错误语义。
	if err != nil {
		return nil, err
	}
	result, err := NormalizeConfig(schema, merged)

	// >>> 数据演变示例
	// 1. current{token:"x"}+update{enabled:true} -> 补回token -> 完整配置。
	// 2. current{token:"x"}+update{token:"y"} -> 使用y -> 完整配置。
	return result, err
}

// RedactConfig 删除 Schema 中所有 secret 字段，供读取和审计输出使用。
// @param schema：配置字段描述；raw：内部完整配置。
// @returns 不含敏感字段的 JSON 对象或错误。
// ⚠️副作用说明：无。
func RedactConfig(schema ConfigSchema, raw json.RawMessage) (json.RawMessage, error) {
	normalized, err := normalizeConfig(schema, raw, false)
	// [决策理由] 脱敏前必须拒绝未知字段和无效值，避免只删除已知 secret 后泄露未声明数据。
	if err != nil {
		return nil, err
	}
	values, err := decodeConfigObject(normalized)
	// [决策理由] 规范化结果理论上是对象；仍采用安全失败策略，绝不回退返回原始配置。
	if err != nil {
		return nil, err
	}
	for _, field := range schema.Fields {
		// [决策理由] secret 是 write-only 字段，读取和审计快照都必须完全省略。
		if field.Type == FieldSecret {
			delete(values, field.Key)
		}
	}
	result, err := json.Marshal(values)

	// >>> 数据演变示例
	// 1. {endpoint:"a",token:"x"} -> 删除token -> {endpoint:"a"}。
	// 2. {enabled:true} -> 无secret -> 保持公开字段。
	return result, err
}

// validFieldType 判断字段类型是否属于 MVP 词汇表。
// @param fieldType：待判断字段类型。
// @returns 是否受支持。
// ⚠️副作用说明：无。
func validFieldType(fieldType FieldType) bool {
	valid := fieldType == FieldString || fieldType == FieldMultiline || fieldType == FieldInteger || fieldType == FieldBoolean || fieldType == FieldEnum || fieldType == FieldSecret || fieldType == FieldStringListJSON || fieldType == FieldWeightedTermsJSON || fieldType == FieldCombinationRulesJSON

	// >>> 数据演变示例
	// 1. boolean -> true。
	// 2. object -> false。
	return valid
}

// validateOptions 校验 enum 选项约束。
// @param field：配置字段。
// @returns 选项约束错误。
// ⚠️副作用说明：无。
func validateOptions(field ConfigField) error {
	// [决策理由] 非 enum 字段携带选项没有明确语义，应拒绝而非静默忽略。
	if field.Type != FieldEnum {
		// [决策理由] 其他字段类型不允许携带枚举专属选项。
		if len(field.Options) > 0 {
			return fmt.Errorf("非 enum 配置字段 %s 不能声明选项", field.Key)
		}
		return nil
	}
	// [决策理由] 空枚举无法提供任何合法输入。
	if len(field.Options) == 0 {
		return fmt.Errorf("enum 配置字段 %s 至少需要一个选项", field.Key)
	}
	seen := make(map[string]struct{}, len(field.Options))
	for _, option := range field.Options {
		// [决策理由] 空白选项无法在表单和持久化值中稳定区分。
		if strings.TrimSpace(option) == "" {
			return fmt.Errorf("enum 配置字段 %s 包含空选项", field.Key)
		}
		// [决策理由] 重复选项会产生不可区分的 UI 选择。
		if _, exists := seen[option]; exists {
			return fmt.Errorf("enum 配置字段 %s 的选项 %q 重复", field.Key, option)
		}
		seen[option] = struct{}{}
	}

	// >>> 数据演变示例
	// 1. enum+[fast,safe] -> 唯一非空 -> nil。
	// 2. string+[fast] -> 类型不支持选项 -> 返回错误。
	return nil
}

// validateFieldValue 校验单个 JSON 值的类型和枚举范围。
// @param field：字段描述；raw：字段 JSON 值。
// @returns 类型或范围错误。
// ⚠️副作用说明：无。
func validateFieldValue(field ConfigField, raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	// [决策理由] 字段值必须是一个完整合法的 JSON 值。
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	// [决策理由] null 不等同于缺失，会破坏所有 MVP 字段的运行类型约束。
	if value == nil {
		return errors.New("不能为 null")
	}
	switch field.Type {
	case FieldString, FieldMultiline, FieldSecret:
		// [决策理由] 文本和敏感字段只接受 JSON string。
		if _, ok := value.(string); !ok {
			return errors.New("必须是字符串")
		}
	case FieldStringListJSON, FieldWeightedTermsJSON, FieldCombinationRulesJSON:
		text, ok := value.(string)
		// [决策理由] 结构化编辑器仍以JSON字符串兼容既有config_json，禁止客户端改写存储形态。
		if !ok {
			return errors.New("必须是结构化JSON字符串")
		}
		// [决策理由] 服务端必须独立校验编辑器输出，不能信任前端生成的内部JSON。
		if err := validateStructuredJSONText(field.Type, text); err != nil {
			return err
		}
	case FieldInteger:
		number, ok := value.(json.Number)
		// [决策理由] integer 不接受字符串、浮点数或指数产生的非整数。
		if !ok {
			return errors.New("必须是整数")
		}
		// [决策理由] Int64 同时验证整数语法和明确的可持久化范围。
		if _, err := number.Int64(); err != nil {
			return errors.New("必须是 64 位整数")
		}
	case FieldBoolean:
		// [决策理由] boolean 不接受 0/1 或字符串形式，避免多端解释漂移。
		if _, ok := value.(bool); !ok {
			return errors.New("必须是布尔值")
		}
	case FieldEnum:
		text, ok := value.(string)
		// [决策理由] MVP enum 的持久化形式固定为字符串。
		if !ok {
			return errors.New("必须是枚举字符串")
		}
		matched := false
		for _, option := range field.Options {
			// [决策理由] 仅接受 Schema 明确列出的固定值。
			if text == option {
				matched = true
				break
			}
		}
		// [决策理由] 未匹配值不能由插件自行猜测或降级。
		if !matched {
			return fmt.Errorf("值 %q 不在允许选项中", text)
		}
	default:
		return fmt.Errorf("类型 %q 不受支持", field.Type)
	}

	// >>> 数据演变示例
	// 1. integer+42 -> json.Number.Int64成功 -> nil。
	// 2. enum[fast]+"slow" -> 未匹配 -> 返回错误。
	return nil
}

type weightedTermValue struct {
	Text   string  `json:"text"`
	Weight float64 `json:"weight"`
}

type combinationRuleValue struct {
	Terms []string `json:"terms"`
	Bonus float64  `json:"bonus"`
}

// validateStructuredJSONText 严格校验三种结构化编辑器的内部JSON形状。
// @param fieldType：结构化字段类型；text：外层配置字符串中的JSON文本。
// @returns 结构、未知字段或尾随值错误。
// ⚠️副作用说明：无。
func validateStructuredJSONText(fieldType FieldType, text string) error {
	trimmed := strings.TrimSpace(text)
	// [决策理由] 结构化配置必须是有界数组，拒绝null、对象和过大文本占用解析资源。
	if len(trimmed) > 65536 {
		return errors.New("结构化JSON不能超过65536字节")
	}
	// [决策理由] 三种编辑器根节点都固定为数组，先验检查防止null被类型化解码为nil切片。
	if trimmed == "" || trimmed[0] != '[' {
		return errors.New("结构化JSON根节点必须是数组")
	}
	var target any
	switch fieldType {
	case FieldStringListJSON:
		target = &[]string{}
	case FieldWeightedTermsJSON:
		target = &[]weightedTermValue{}
	case FieldCombinationRulesJSON:
		target = &[]combinationRuleValue{}
	default:
		return fmt.Errorf("类型 %q 不是结构化JSON字段", fieldType)
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	// [决策理由] 内部JSON必须严格匹配声明结构，避免拼写错误被静默忽略。
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("结构化JSON无效: %w", err)
	}
	var trailing any
	// [决策理由] 单个数组后不允许尾随第二个JSON值。
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("结构化JSON包含多余内容")
	}

	// >>> 数据演变示例
	// 1. weighted_terms_json+[{"text":"免费","weight":10}] -> nil。
	// 2. combination_rules_json含unknown或尾随对象 -> error。
	return nil
}

// decodeConfigObject 严格解码一个 JSON 对象并拒绝尾随值。
// @param raw：配置 JSON。
// @returns 字段原始值映射或错误。
// ⚠️副作用说明：无。
func decodeConfigObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var values map[string]json.RawMessage
	// [决策理由] 配置根节点必须是合法 JSON 对象。
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("配置必须是 JSON 对象: %w", err)
	}
	// [决策理由] JSON null 会解码为 nil map，但不代表有效配置对象。
	if values == nil {
		return nil, errors.New("配置必须是 JSON 对象")
	}
	var trailing any
	// [决策理由] 第二个 JSON 值属于模糊或恶意尾随输入，必须拒绝。
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("配置包含多余 JSON 值")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("配置包含无效尾随内容: %w", err)
	}

	// >>> 数据演变示例
	// 1. {"enabled":true} -> map[enabled:true]。
	// 2. null 或 {}{} -> 非对象/尾随值 -> 返回错误。
	return values, nil
}
