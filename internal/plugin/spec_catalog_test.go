// 📌 影响范围：验证目标插件纯内存 Catalog 的冲突检测、排序和快照隔离；无外部变量。
package plugin

import (
	"strings"
	"testing"
)

// TestNewSpecCatalogRejectsPluginAndTriggerConflicts 验证跨插件稳定 Key 与触发词全局唯一。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestNewSpecCatalogRejectsPluginAndTriggerConflicts(t *testing.T) {
	tests := []struct {
		name  string
		specs []PluginSpec
		want  string
	}{
		{name: "插件Key重复", specs: []PluginSpec{validPluginSpec("echo", "echo"), validPluginSpec("echo", "other")}, want: "插件 Key \"echo\" 重复"},
		{name: "触发词重复", specs: []PluginSpec{validPluginSpec("echo", " ECHO "), validPluginSpec("tools", "echo")}, want: "触发词 \"echo\""},
	}
	for _, test := range tests {
		catalog, err := NewSpecCatalog(test.specs)
		// [决策理由] 冲突目录不能部分返回，否则调用方可能误用不完整路由。
		if catalog != nil || err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: NewSpecCatalog() = %v, %v, want contains %q", test.name, catalog, err, test.want)
		}
	}

	// >>> 数据演变示例
	// 1. echo与echo同Key -> 构建失败且目录为nil。
	// 2. " ECHO "与echo标准化冲突 -> 构建失败。
}

// TestSpecCatalogReturnsSortedIsolatedSnapshots 验证目录复制输入并隔离所有可变集合。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestSpecCatalogReturnsSortedIsolatedSnapshots(t *testing.T) {
	monitor := PluginSpec{Key: "monitor", DisplayName: "监控", Description: "观察群消息", Observers: []ObserverSpec{{Key: "messages", Description: "观察消息", EventKinds: []ObserverEventKind{ObserverGroupMessage}, Handler: func(ObserverContext) error { return nil }}}}
	echo := validPluginSpec("echo", "echo")
	catalog, err := NewSpecCatalog([]PluginSpec{monitor, echo})
	// [决策理由] 合法规格是后续排序和隔离断言的前提。
	if err != nil {
		t.Fatal(err)
	}
	echo.Commands[0].Triggers[0] = "changed"
	echo.Commands[0].AllowedRoles[RoleGroupAdmin] = struct{}{}
	monitor.Observers[0].EventKinds[0] = ObserverGroupNotice
	specs := catalog.Specs()
	// [决策理由] Catalog 对管理界面和测试提供稳定按 Key 排序结果。
	if len(specs) != 2 || specs[0].Key != "echo" || specs[1].Key != "monitor" {
		t.Fatalf("Specs() = %+v", specs)
	}
	// [决策理由] 构建后修改输入不能污染目录触发词和角色集合。
	if specs[0].Commands[0].Triggers[0] != "echo" || specs[0].Commands[0].AllowedRoles.Contains(RoleGroupAdmin) {
		t.Fatalf("echo目录被输入修改污染: %+v", specs[0])
	}
	// [决策理由] 观察器事件切片也必须与调用方输入隔离。
	if specs[1].Observers[0].EventKinds[0] != ObserverGroupMessage {
		t.Fatalf("monitor目录被输入修改污染: %+v", specs[1])
	}
	specs[0].Commands[0].Triggers[0] = "snapshot_changed"
	specs[0].Commands[0].AllowedRoles[RoleGroupAdmin] = struct{}{}
	specs[1].Observers[0].EventKinds[0] = ObserverGroupNotice
	found, ok := catalog.Find("echo")
	// [决策理由] 修改列表快照的触发词和角色不能反向污染后续单项读取。
	if !ok || found.Commands[0].Triggers[0] != "echo" || found.Commands[0].AllowedRoles.Contains(RoleGroupAdmin) {
		t.Fatalf("Find(echo) = %+v,%t", found, ok)
	}
	foundMonitor, monitorFound := catalog.Find("monitor")
	// [决策理由] 修改列表快照的观察事件不能污染目录内原始声明。
	if !monitorFound || foundMonitor.Observers[0].EventKinds[0] != ObserverGroupMessage {
		t.Fatalf("Find(monitor) = %+v,%t", foundMonitor, monitorFound)
	}
	_, missing := catalog.Find("missing")
	// [决策理由] 未注册插件必须返回不存在，不得合成零值规格为有效项。
	if missing {
		t.Fatal("Find(missing) found=true")
	}

	// >>> 数据演变示例
	// 1. 输入[monitor,echo] -> Catalog排序 -> [echo,monitor]。
	// 2. 修改输入和返回快照 -> 再次Find -> 原触发词、角色和观察事件不变。
}

// TestNilSpecCatalogReadsAsEmpty 验证未初始化目录只读操作安全失败。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestNilSpecCatalogReadsAsEmpty(t *testing.T) {
	var catalog *SpecCatalog
	specs := catalog.Specs()
	// [决策理由] nil Catalog 应表现为空集合，不能让只读管理接口 panic。
	if specs == nil || len(specs) != 0 {
		t.Fatalf("nil Specs() = %#v", specs)
	}
	_, found := catalog.Find("echo")
	// [决策理由] nil Catalog 不包含任何插件。
	if found {
		t.Fatal("nil Find(echo) found=true")
	}

	// >>> 数据演变示例
	// 1. nil Catalog -> Specs -> 非nil空切片。
	// 2. nil Catalog -> Find(echo) -> false。
}
