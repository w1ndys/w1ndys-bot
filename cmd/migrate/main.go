// 📌 影响范围：读取数据库环境变量和 CLI 参数；执行或查询 PostgreSQL 迁移；写入结构化日志。
package main

import (
	"os"
	"strconv"

	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/migration"
	"github.com/w1ndys/w1ndys-bot/pkg/logger"
)

// main 执行 up、down 或 version 迁移命令。
// @param 无；首个位置参数为命令，down 的第二个参数为回滚步数。
// @returns 无。
// ⚠️副作用说明：读取配置、连接并可能修改 PostgreSQL schema，向标准输出写日志。
func main() {
	cfg, err := config.Load()
	// [决策理由] 数据库配置不完整时迁移无法安全执行。
	if err != nil {
		fail("加载配置失败", "error", err)
	}
	configuredLogger, err := logger.New(cfg.LogLevel, cfg.LogFormat)
	// [决策理由] 日志器配置错误时不应进入数据库操作。
	if err != nil {
		fail("初始化日志器失败", "error", err)
	}
	logger.SetDefault(configuredLogger)
	defer configuredLogger.Sync()
	runner, err := migration.New(cfg.Database)
	// [决策理由] Runner 初始化失败时没有可用迁移连接。
	if err != nil {
		fail("初始化迁移失败", "error", err)
	}
	defer runner.Close()
	command := "up"
	// [决策理由] 未提供命令时默认执行安全且幂等的 up。
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "up":
		err = runner.Up()
	case "down":
		steps := 1
		// [决策理由] 提供回滚步数时必须转换为正整数。
		if len(os.Args) > 2 {
			steps, err = strconv.Atoi(os.Args[2])
		}
		// [决策理由] 参数解析失败时不能执行可能破坏数据的回滚。
		if err == nil {
			err = runner.Down(steps)
		}
	case "version":
		var version uint
		var dirty bool
		version, dirty, err = runner.Version()
		// [决策理由] 仅在查询成功时输出可信版本。
		if err == nil {
			logger.Info("数据库迁移版本", "version", version, "dirty", dirty)
		}
	default:
		fail("未知迁移命令", "command", command)
	}
	// [决策理由] 命令执行错误必须记录并以当前流程结束。
	if err != nil {
		fail("数据库迁移命令失败", "command", command, "error", err)
	}
	logger.Info("数据库迁移命令完成", "command", command)

	// >>> 数据演变示例
	// 1. args=[up] -> Runner.Up -> 输出完成日志。
	// 2. args=[down,abc] -> Atoi 失败 -> 输出错误且不回滚。
}

// fail 记录迁移错误、刷新日志并以失败状态终止进程。
// @param message：错误描述；fields：结构化上下文字段。
// @returns 不返回。
// ⚠️副作用说明：写入日志、刷新日志器并调用 os.Exit(1)。
func fail(message string, fields ...any) {
	logger.Error(message, fields...)
	_ = logger.Default().Sync()
	os.Exit(1)

	// >>> 数据演变示例
	// 1. 数据库连接失败 -> Error 日志 -> Sync -> 退出码 1。
	// 2. 未知命令 -> command 字段日志 -> 退出码 1。
}
