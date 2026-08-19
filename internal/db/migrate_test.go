package db

import (
	"errors"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

// fakeRunner 记录调用并返回预设错误。
type fakeRunner struct {
	upErr   error
	downErr error
}

func (f fakeRunner) Up() error   { return f.upErr }
func (f fakeRunner) Down() error { return f.downErr }

func TestMigrateUp(t *testing.T) {
	changed, err := Migrate(fakeRunner{}, DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up) 出错 = %v，期望 nil", err)
	}
	if !changed {
		t.Error("Migrate(up) changed = false，期望 true")
	}
}

func TestMigrateUpNoChange(t *testing.T) {
	changed, err := Migrate(fakeRunner{upErr: migrate.ErrNoChange}, DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up) 出错 = %v，期望 nil（无变更不应视为错误）", err)
	}
	if changed {
		t.Error("Migrate(up) changed = true，期望 false")
	}
}

func TestMigrateUpError(t *testing.T) {
	want := errors.New("迁移文件缺失")
	_, err := Migrate(fakeRunner{upErr: want}, DirectionUp)
	if !errors.Is(err, want) {
		t.Fatalf("Migrate(up) 出错 = %v，期望包含 %v", err, want)
	}
}

func TestMigrateDown(t *testing.T) {
	changed, err := Migrate(fakeRunner{}, DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down) 出错 = %v，期望 nil", err)
	}
	if !changed {
		t.Error("Migrate(down) changed = false，期望 true")
	}
}

func TestMigrateDownNoChange(t *testing.T) {
	changed, err := Migrate(fakeRunner{downErr: migrate.ErrNoChange}, DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down) 出错 = %v，期望 nil（无变更不应视为错误）", err)
	}
	if changed {
		t.Error("Migrate(down) changed = true，期望 false")
	}
}

func TestMigrateDownError(t *testing.T) {
	want := errors.New("SQL 错误")
	_, err := Migrate(fakeRunner{downErr: want}, DirectionDown)
	if !errors.Is(err, want) {
		t.Fatalf("Migrate(down) 出错 = %v，期望包含 %v", err, want)
	}
}

func TestMigrateUnknownDirection(t *testing.T) {
	if _, err := Migrate(fakeRunner{}, Direction("sideways")); err == nil {
		t.Fatal("Migrate(未知方向) 出错 = nil，期望错误")
	}
}
