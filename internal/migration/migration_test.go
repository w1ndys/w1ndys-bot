package migration

import (
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// TestEmbeddedMigrations 验证迁移文件已正确内嵌，且能被 iofs 以 migrations 为根读取。
func TestEmbeddedMigrations(t *testing.T) {
	d, err := iofs.New(Files, "migrations")
	if err != nil {
		t.Fatalf("iofs.New 出错 = %v，期望 nil", err)
	}
	first, err := d.First()
	if err != nil {
		t.Fatalf("First() 出错 = %v，期望 nil", err)
	}
	if first != 1 {
		t.Errorf("首个迁移版本 = %d，期望 1", first)
	}
}
