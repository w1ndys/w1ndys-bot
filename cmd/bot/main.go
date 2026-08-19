// Command bot 启动 W1ndys Bot 服务：加载配置、建立数据库连接池、提供健康检查，
// 直至收到退出信号才优雅关闭。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/w1ndys/w1ndys-bot/internal/config"
	"github.com/w1ndys/w1ndys-bot/internal/db"
	"github.com/w1ndys/w1ndys-bot/internal/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel, cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		log.Error("连接数据库", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.WebUIPort),
		Handler: newMux(pool),
	}

	go func() {
		log.Info("HTTP 服务开始监听", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP 服务", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("收到退出信号")

	// 优雅关闭：先停 HTTP，再关连接池，带硬超时兜底。
	log.Info("关闭: 停止 HTTP 服务")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("关闭: HTTP 服务", "err", err)
	} else {
		log.Info("关闭: HTTP 服务已停止")
	}

	log.Info("关闭: 关闭数据库连接池")
	pool.Close()
	log.Info("关闭: 完成")
}

// newMux 组装 HTTP 路由。
func newMux(pool *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(pool))
	return mux
}

// healthz 上报服务与数据库的存活状态。
func healthz(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unhealthy","db":"down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","db":"up"}`))
	}
}
