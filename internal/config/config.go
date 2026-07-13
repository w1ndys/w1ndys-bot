// 📌 影响范围：读取 DB_HOST、DB_PORT、DB_USER、DB_NAME、DB_PASSWORD、DB_SSLMODE、NAPCAT_TOKEN、WS_PORT、JWT_SECRET、SUPER_ADMIN_QQ、WEBUI_PASSWORD、LOG_LEVEL、LOG_FORMAT 及对应 CLI 参数。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config 表示基础设施配置。
type Config struct {
	Database      Database
	NapCatToken   string
	WSPort        int
	JWTSecret     string
	SuperAdminQQ  string
	WebUIPassword string
	LogLevel      string
	LogFormat     string
}

// Database 表示 PostgreSQL 连接配置。
type Database struct {
	Host     string
	Port     int
	User     string
	Name     string
	Password string
	SSLMode  string
}

// Load 按 CLI 参数高于环境变量的优先级加载配置。
// @param 无；参数从 os.Args 和进程环境读取。
// @returns 完整且通过校验的 Config，以及解析或校验错误。
// ⚠️副作用说明：读取进程环境变量和命令行参数。
func Load() (Config, error) {
	flags := pflag.NewFlagSet("bot", pflag.ContinueOnError)
	flags.SetInterspersed(true)
	defineFlags(flags)
	// [决策理由] 无法解析的 CLI 参数通常表示部署配置拼写错误，必须阻止启动。
	if err := flags.Parse(os.Args[1:]); err != nil {
		return Config{}, fmt.Errorf("解析 CLI 参数: %w", err)
	}

	v := viper.New()
	v.SetEnvPrefix("")
	v.AutomaticEnv()
	bindFlags(v, flags)
	setDefaults(v)

	cfg := Config{
		Database: Database{
			Host:     v.GetString("DB_HOST"),
			Port:     v.GetInt("DB_PORT"),
			User:     v.GetString("DB_USER"),
			Name:     v.GetString("DB_NAME"),
			Password: v.GetString("DB_PASSWORD"),
			SSLMode:  v.GetString("DB_SSLMODE"),
		},
		NapCatToken:   v.GetString("NAPCAT_TOKEN"),
		WSPort:        v.GetInt("WS_PORT"),
		JWTSecret:     v.GetString("JWT_SECRET"),
		SuperAdminQQ:  v.GetString("SUPER_ADMIN_QQ"),
		WebUIPassword: v.GetString("WEBUI_PASSWORD"),
		LogLevel:      v.GetString("LOG_LEVEL"),
		LogFormat:     v.GetString("LOG_FORMAT"),
	}
	// [决策理由] 密码缺失时数据库必然无法鉴权，提前返回可提供更明确的诊断。
	if cfg.Database.Password == "" {
		return Config{}, errors.New("DB_PASSWORD 不能为空")
	}
	cfg.SuperAdminQQ = strings.TrimSpace(cfg.SuperAdminQQ)
	// [决策理由] 单管理员权限完全来自环境变量，非数字或零值会造成不可登录且难以诊断。
	if cfg.SuperAdminQQ != "" {
		value, err := strconv.ParseUint(cfg.SuperAdminQQ, 10, 64)
		// [决策理由] QQ 号必须是正十进制整数，拒绝符号、空格和溢出配置。
		if err != nil || value == 0 {
			return Config{}, fmt.Errorf("SUPER_ADMIN_QQ %q 格式无效", cfg.SuperAdminQQ)
		}
	}

	// >>> 数据演变示例
	// 1. DB_HOST=postgres, --db-host=127.0.0.1 -> CLI 覆盖环境变量 -> Database.Host=127.0.0.1。
	// 2. DB_PASSWORD="" -> 构造 Config -> 校验失败 -> 返回 DB_PASSWORD 不能为空。
	return cfg, nil
}

// defineFlags 声明允许覆盖环境变量的 CLI 参数。
// @param flags：待注册参数的 FlagSet。
// @returns 无。
// ⚠️副作用说明：修改传入的 FlagSet，注册命令行参数。
func defineFlags(flags *pflag.FlagSet) {
	flags.String("db-host", "", "PostgreSQL 主机")
	flags.Int("db-port", 0, "PostgreSQL 端口")
	flags.String("db-user", "", "PostgreSQL 用户")
	flags.String("db-name", "", "PostgreSQL 数据库名")
	flags.String("db-password", "", "PostgreSQL 密码")
	flags.String("db-sslmode", "", "PostgreSQL TLS 模式")
	flags.String("napcat-token", "", "NapCat 鉴权 Token")
	flags.Int("ws-port", 0, "反向 WebSocket 监听端口")
	flags.String("jwt-secret", "", "WebUI JWT 密钥")
	flags.String("super-admin-qq", "", "首次引导的最高管理员 QQ 号")
	flags.String("webui-password", "", "WebUI 共享管理员密码")
	flags.String("log-level", "", "日志级别")
	flags.String("log-format", "", "日志格式")

	// >>> 数据演变示例
	// 1. 空 FlagSet -> 注册 --db-host 等 9 个参数 -> 可解析完整 CLI 配置。
	// 2. --ws-port=18800 -> FlagSet 解析 -> ws-port 值为 18800。
}

// bindFlags 将显式设置的 CLI 参数绑定到 Viper 配置键。
// @param v：配置读取器；flags：已解析的 CLI 参数集合。
// @returns 无。
// ⚠️副作用说明：修改 Viper 的参数绑定；绑定失败会触发 panic，表示程序内部定义不一致。
func bindFlags(v *viper.Viper, flags *pflag.FlagSet) {
	bindings := map[string]string{
		"DB_HOST": "db-host", "DB_PORT": "db-port", "DB_USER": "db-user",
		"DB_NAME": "db-name", "DB_PASSWORD": "db-password", "NAPCAT_TOKEN": "napcat-token",
		"DB_SSLMODE": "db-sslmode",
		"WS_PORT":    "ws-port", "JWT_SECRET": "jwt-secret", "LOG_LEVEL": "log-level",
		"SUPER_ADMIN_QQ": "super-admin-qq", "WEBUI_PASSWORD": "webui-password",
		"LOG_FORMAT": "log-format",
	}
	for key, name := range bindings {
		flag := flags.Lookup(name)
		// [决策理由] 仅绑定用户显式传入的参数，避免 CLI 空默认值覆盖环境变量。
		if flag != nil && flag.Changed {
			// [决策理由] 参数名来自同一文件内的静态映射，绑定失败表示开发期缺陷而非运行期输入问题。
			if err := v.BindPFlag(key, flag); err != nil {
				panic(fmt.Sprintf("绑定参数 %s: %v", name, err))
			}
		}
	}

	// >>> 数据演变示例
	// 1. DB_HOST=postgres + --db-host=local -> 绑定 DB_HOST -> Viper 读取 local。
	// 2. DB_HOST=postgres + 未传 --db-host -> 不绑定空值 -> Viper 读取 postgres。
}

// setDefaults 设置非敏感基础配置的默认值。
// @param v：配置读取器。
// @returns 无。
// ⚠️副作用说明：修改 Viper 默认值集合。
func setDefaults(v *viper.Viper) {
	v.SetDefault("DB_HOST", "postgres")
	v.SetDefault("DB_PORT", 5432)
	v.SetDefault("DB_USER", "bot_admin")
	v.SetDefault("DB_NAME", "w1ndys_bot")
	v.SetDefault("DB_SSLMODE", "disable")
	v.SetDefault("WS_PORT", 18800)
	v.SetDefault("LOG_LEVEL", "info")
	v.SetDefault("LOG_FORMAT", "text")

	// >>> 数据演变示例
	// 1. 未设置 DB_PORT -> 默认值集合 -> DB_PORT=5432。
	// 2. LOG_LEVEL=debug -> 环境变量覆盖默认值 info -> LOG_LEVEL=debug。
}
