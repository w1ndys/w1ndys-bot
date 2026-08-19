// Package config 负责从 .env 文件与进程环境变量加载并校验应用配置。
//
// 优先级：进程环境变量覆盖 .env 中的值。
// 校验采用 fail-fast 策略：必填项缺失或取值非法时，带着明确错误中止启动。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 保存强类型化的应用配置。
type Config struct {
	// 数据库连接。
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// 管理 HTTP 服务（当前承载健康检查，后续 spec 承载 WebUI）。
	WebUIPort int

	// 密钥与管理员身份：尚未被业务使用，但启动时即强制校验。
	JWTSecret     string
	WebUIPassword string
	SuperAdminQQ  string

	// 日志。
	LogLevel  string
	LogFormat string
}

// Load 读取 .env（若存在），再叠加进程环境变量，返回校验通过的 Config。
// 进程环境变量优先于 .env 中的同名变量。
func Load() (*Config, error) {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			return nil, fmt.Errorf("加载 .env: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("检查 .env: %w", err)
	}

	return parse()
}

func parse() (*Config, error) {
	cfg := &Config{}
	var err error

	if cfg.DBHost, err = required("DB_HOST"); err != nil {
		return nil, err
	}
	if cfg.DBPort, err = portEnv("DB_PORT", 5432); err != nil {
		return nil, err
	}
	if cfg.DBUser, err = required("DB_USER"); err != nil {
		return nil, err
	}
	if cfg.DBPassword, err = required("DB_PASSWORD"); err != nil {
		return nil, err
	}
	if cfg.DBName, err = required("DB_NAME"); err != nil {
		return nil, err
	}
	cfg.DBSSLMode = optional("DB_SSLMODE", "disable")

	if cfg.WebUIPort, err = portEnv("WEBUI_PORT", 8080); err != nil {
		return nil, err
	}

	if cfg.JWTSecret, err = required("JWT_SECRET"); err != nil {
		return nil, err
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET 至少需要 32 字节，当前为 %d", len(cfg.JWTSecret))
	}

	if cfg.WebUIPassword, err = required("WEBUI_PASSWORD"); err != nil {
		return nil, err
	}
	if len(cfg.WebUIPassword) < 12 {
		return nil, fmt.Errorf("WEBUI_PASSWORD 至少需要 12 个字符，当前为 %d", len(cfg.WebUIPassword))
	}

	if cfg.SuperAdminQQ, err = required("SUPER_ADMIN_QQ"); err != nil {
		return nil, err
	}

	cfg.LogLevel = optional("LOG_LEVEL", "info")
	if !validLogLevel(cfg.LogLevel) {
		return nil, fmt.Errorf("LOG_LEVEL 只能是 debug|info|warn|error 之一，当前为 %q", cfg.LogLevel)
	}

	cfg.LogFormat = optional("LOG_FORMAT", "json")
	if !validLogFormat(cfg.LogFormat) {
		return nil, fmt.Errorf("LOG_FORMAT 只能是 text 或 json，当前为 %q", cfg.LogFormat)
	}

	return cfg, nil
}

// required 返回 name 对应的环境变量值；未设置或为空时返回错误。
func required(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s 为必填项", name)
	}
	return v, nil
}

// optional 返回 name 对应的环境变量值；未设置或为空时返回默认值 def。
func optional(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// portEnv 将 name 解析为 TCP 端口；未设置时返回默认值 def。
func portEnv(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s 必须是整数，当前为 %q", name, v)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s 必须在 1..65535 范围内，当前为 %d", name, n)
	}
	return n, nil
}

func validLogLevel(s string) bool {
	switch s {
	case "debug", "info", "warn", "error":
		return true
	}
	return false
}

func validLogFormat(s string) bool {
	return s == "text" || s == "json"
}
