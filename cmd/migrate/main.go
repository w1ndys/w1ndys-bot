// Command migrate 应用或回滚数据库迁移。
//
// 用法：migrate <up|down>
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
	"github.com/w1ndys/w1ndys-bot/internal/logger"
)

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}
	direction := db.Direction(os.Args[1])
	if direction != db.DirectionUp && direction != db.DirectionDown {
		usage()
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel, cfg.LogFormat)

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		log.Error("连接数据库", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	runner, err := db.NewRunner(pool)
	if err != nil {
		log.Error("创建迁移执行器", "err", err)
		os.Exit(1)
	}

	changed, err := db.Migrate(runner, direction)
	if err != nil {
		log.Error("迁移", "direction", direction, "err", err)
		os.Exit(1)
	}
	if changed {
		log.Info("迁移已应用", "direction", direction)
	} else {
		log.Info("迁移无变更", "direction", direction)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "用法: migrate <up|down>\n")
}
