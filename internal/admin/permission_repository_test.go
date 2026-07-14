// 📌 影响范围：无；仅验证权限事务锁键编码。
package admin

import (
	"strings"
	"testing"
)

// TestPermissionLockKey 验证锁键不含PostgreSQL禁止的NUL且维度边界无歧义。
// @param t：Go测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestPermissionLockKey(t *testing.T) {
	first := PermissionSet{ScopeType: "group", ScopeID: "1", PluginName: "a,b", FeatureKey: "c", SubjectType: "user", SubjectID: "2"}
	second := PermissionSet{ScopeType: "group", ScopeID: "1", PluginName: "a", FeatureKey: "b,c", SubjectType: "user", SubjectID: "2"}
	firstKey := permissionLockKey(first)
	secondKey := permissionLockKey(second)
	// [决策理由] PostgreSQL text拒绝NUL字节，锁键必须在进入hashtextextended前拦截回归。
	if strings.ContainsRune(firstKey, '\x00') || strings.ContainsRune(secondKey, '\x00') {
		t.Fatalf("permissionLockKey contains NUL: %q / %q", firstKey, secondKey)
	}
	// [决策理由] 不同唯一维度必须获得不同锁键，否则无关策略会被错误串行化。
	if firstKey == secondKey {
		t.Fatalf("permissionLockKey collision: %q", firstKey)
	}

	// >>> 数据演变示例
	// 1. 两组含逗号维度 -> JSON数组保留边界 -> 锁键不同。
	// 2. 任一锁键含NUL -> PostgreSQL会拒绝 -> 测试立即失败。
}
