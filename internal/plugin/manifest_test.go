// 📌 影响范围：仅构造并校验内存 Manifest；不修改全局 Catalog 或外部状态。
package plugin

import "testing"

// TestManifestValidate 验证稳定标识、版本和功能唯一性规则。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		Name: "score", DisplayName: "积分", Version: "1.0.0", SchemaVersion: 1,
		Features: []FeatureManifest{{Key: "check_in", DisplayName: "签到"}, {Key: "rank", DisplayName: "排行"}},
	}
	// [决策理由] 合法 Manifest 是后续错误用例的基线。
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法 Manifest 校验失败: %v", err)
	}
	duplicate := valid
	duplicate.Features = append(duplicate.Features, FeatureManifest{Key: "rank", DisplayName: "重复排行"})
	// [决策理由] 重复 feature_key 会让命令和权限引用产生歧义，必须失败。
	if err := duplicate.Validate(); err == nil {
		t.Fatal("重复功能未返回错误")
	}
	invalidName := valid
	invalidName.Name = "Score-Plugin"
	// [决策理由] 不稳定标识格式不能进入数据库主键。
	if err := invalidName.Validate(); err == nil {
		t.Fatal("非法插件名未返回错误")
	}

	// >>> 数据演变示例
	// 1. score+[check_in,rank] -> Validate -> nil。
	// 2. score+[rank,rank] -> Validate -> 重复错误。
}
