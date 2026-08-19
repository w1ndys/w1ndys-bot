package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate 将测试切换到不含 .env 的空工作目录，避免开发者的真实 .env 污染断言。
func isolate(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

// setRequired 设置一次成功 Load 所需的所有变量。
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "user")
	t.Setenv("DB_PASSWORD", "password")
	t.Setenv("DB_NAME", "db")
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("WEBUI_PASSWORD", strings.Repeat("b", 12))
	t.Setenv("SUPER_ADMIN_QQ", "10001")
}

func TestLoadValidDefaults(t *testing.T) {
	isolate(t)
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() 出错 = %v，期望 nil", err)
	}
	if cfg.DBPort != 5432 {
		t.Errorf("DBPort = %d，期望 5432", cfg.DBPort)
	}
	if cfg.DBSSLMode != "disable" {
		t.Errorf("DBSSLMode = %q，期望 disable", cfg.DBSSLMode)
	}
	if cfg.WebUIPort != 8080 {
		t.Errorf("WebUIPort = %d，期望 8080", cfg.WebUIPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q，期望 info", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q，期望 json", cfg.LogFormat)
	}
}

func TestLoadMissingJWTSecret(t *testing.T) {
	isolate(t)
	setRequired(t)
	t.Setenv("JWT_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() 出错 = nil，期望缺少 JWT_SECRET 的错误")
	}
}

func TestLoadShortWebUIPassword(t *testing.T) {
	isolate(t)
	setRequired(t)
	t.Setenv("WEBUI_PASSWORD", "short")

	if _, err := Load(); err == nil {
		t.Fatal("Load() 出错 = nil，期望 WEBUI_PASSWORD 过短的错误")
	}
}

func TestLoadEmptySuperAdminQQ(t *testing.T) {
	isolate(t)
	setRequired(t)
	t.Setenv("SUPER_ADMIN_QQ", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() 出错 = nil，期望 SUPER_ADMIN_QQ 为空的错误")
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	isolate(t)
	setRequired(t)
	t.Setenv("LOG_LEVEL", "verbose")

	if _, err := Load(); err == nil {
		t.Fatal("Load() 出错 = nil，期望 LOG_LEVEL 非法的错误")
	}
}

func TestLoadEnvOverridesDotEnv(t *testing.T) {
	dir := t.TempDir()
	content := "WEBUI_PORT=9090\nDB_HOST=from-dotenv\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	setRequired(t)
	t.Setenv("WEBUI_PORT", "7070")
	t.Setenv("DB_HOST", "from-env")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() 出错 = %v，期望 nil", err)
	}
	if cfg.WebUIPort != 7070 {
		t.Errorf("WebUIPort = %d，期望 7070（环境变量必须覆盖 .env）", cfg.WebUIPort)
	}
	if cfg.DBHost != "from-env" {
		t.Errorf("DBHost = %q，期望 from-env（环境变量必须覆盖 .env）", cfg.DBHost)
	}
}
