// 📌 影响范围：临时替换测试进程全局日志器；不写入外部文件或网络。
package logger

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestCallerPointsToBusinessCode 验证包级和子日志器均跳过封装层。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：测试期间替换全局默认 Logger。
func TestCallerPointsToBusinessCode(t *testing.T) {
	core, observed := observer.New(zapcore.DebugLevel)
	testLogger := &zapLogger{inner: zap.New(core, zap.AddCaller())}
	previous := Default()
	SetDefault(testLogger)
	defer SetDefault(previous)
	Info("包级调用")
	With("event_type", "test").Info("子日志器调用")
	entries := observed.All()
	// [决策理由] 两种入口必须各产生一条日志才能验证 caller。
	if len(entries) != 2 {
		t.Fatalf("日志数量 = %d，期望 2", len(entries))
	}
	for _, entry := range entries {
		// [决策理由] caller 应落在业务测试文件，而不是 logger.go 封装实现。
		if !strings.HasSuffix(entry.Caller.File, "logger_test.go") {
			t.Errorf("caller = %s:%d，期望 logger_test.go", entry.Caller.File, entry.Caller.Line)
		}
	}

	// >>> 数据演变示例
	// 1. logger.Info -> skip=2 -> caller=logger_test.go。
	// 2. logger.With(...).Info -> skip=1 -> caller=logger_test.go。
}
