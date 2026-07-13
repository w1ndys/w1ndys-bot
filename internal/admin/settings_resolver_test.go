// 📌 影响范围：无；使用内存 Repository 验证系统设置默认值、覆盖和热刷新保护。
package admin

import (
	"context"
	"encoding/json"
	"testing"
)

// TestSettingsResolverLoadsDatabaseOverride 验证数据库值覆盖定义默认值。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存 Repository 并可能终止当前测试。
func TestSettingsResolverLoadsDatabaseOverride(t *testing.T) {
	repository := &fakeRepository{settings: []SettingState{{Key: "command_prefix", Value: json.RawMessage(`"!"`), Description: "机器人命令前缀"}}}
	resolver := NewSettingsResolver(repository)
	err := resolver.Load(context.Background())
	// [决策理由] 合法数据库覆盖必须成功发布。
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// [决策理由] 消息路由应读取数据库覆盖后的前缀。
	if resolver.CommandPrefix() != "!" {
		t.Fatalf("CommandPrefix() = %q, want !", resolver.CommandPrefix())
	}

	// >>> 数据演变示例
	// 1. 默认/ + DB! -> Load -> CommandPrefix=!。
	// 2. 其余设置缺失 -> 自动保留定义默认值。
}

// TestSettingsResolverKeepsSnapshotOnInvalidReload 验证无效数据库值不会替换旧快照。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：修改测试 Repository 预设并可能终止当前测试。
func TestSettingsResolverKeepsSnapshotOnInvalidReload(t *testing.T) {
	repository := &fakeRepository{settings: []SettingState{{Key: "command_prefix", Value: json.RawMessage(`"!"`)}}}
	resolver := NewSettingsResolver(repository)
	// [决策理由] 初始有效快照必须先发布才能验证失败保留语义。
	if err := resolver.Load(context.Background()); err != nil {
		t.Fatalf("initial Load() error = %v", err)
	}
	repository.settings = []SettingState{{Key: "command_prefix", Value: json.RawMessage(`"   "`)}}
	err := resolver.Load(context.Background())
	// [决策理由] 空白前缀必须被当前定义拒绝。
	if err == nil {
		t.Fatal("invalid Load() error = nil")
	}
	// [决策理由] 刷新失败必须保留上一份完整可用快照。
	if resolver.CommandPrefix() != "!" {
		t.Fatalf("CommandPrefix() = %q, want previous !", resolver.CommandPrefix())
	}

	// >>> 数据演变示例
	// 1. snapshot=! + DB空白 -> Load失败 -> 仍返回!。
	// 2. 无效数据不执行Store -> 读者不观察中间状态。
}

// TestValidateSettingRejectsOutOfRangeInteger 验证数值设置范围。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestValidateSettingRejectsOutOfRangeInteger(t *testing.T) {
	err := validateSetting("default_page_size", json.RawMessage(`500`))
	// [决策理由] 超过200的分页大小必须拒绝，避免管理查询资源失控。
	if err == nil {
		t.Fatal("validateSetting() error = nil")
	}

	// >>> 数据演变示例
	// 1. page_size=20 -> 范围10..200 -> nil。
	// 2. page_size=500 -> 超范围 -> error。
}

// TestDefinitionsExcludeWebUITitle 验证网站名称不再作为动态系统设置暴露。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestDefinitionsExcludeWebUITitle(t *testing.T) {
	definitions := Definitions()
	_, exists := definitions["webui_title"]
	// [决策理由] 固定产品名称不能被管理 API 重新开放为可修改字段。
	if exists {
		t.Fatal("Definitions contains webui_title")
	}
	// [决策理由] 移除标题后仍应保留三个实际运行设置。
	if len(definitions) != 3 {
		t.Fatalf("Definitions length = %d, want 3", len(definitions))
	}

	// >>> 数据演变示例
	// 1. Definitions -> 查找webui_title -> 不存在。
	// 2. Definitions -> command_prefix等三项 -> 长度3。
}
