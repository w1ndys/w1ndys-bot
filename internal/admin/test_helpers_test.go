package admin

import (
	"context"
	"errors"
)

type fakeRepository struct {
	settings         []SettingState
	setting          SettingState
	setActor         Actor
	deletedKey       string
	deleteActor      Actor
	listSettingsErr  error
	setSettingErr    error
	deleteSettingErr error
	auditPage        AuditPage
	auditState       AuditState
	auditErr         error
}

func (f *fakeRepository) ListSystemSettings(context.Context) ([]SettingState, error) {
	if f.listSettingsErr != nil {
		return nil, f.listSettingsErr
	}
	return append([]SettingState(nil), f.settings...), nil
}

func (f *fakeRepository) SetSystemSetting(_ context.Context, actor Actor, setting SettingState) (SettingState, error) {
	f.setActor = actor
	f.setting = setting
	if f.setSettingErr != nil {
		return SettingState{}, f.setSettingErr
	}
	return setting, nil
}

func (f *fakeRepository) DeleteSystemSetting(_ context.Context, actor Actor, key string) error {
	f.deleteActor = actor
	f.deletedKey = key
	return f.deleteSettingErr
}

func (f *fakeRepository) ListAuditLogs(context.Context, AuditQuery) (AuditPage, error) {
	return f.auditPage, f.auditErr
}

func (f *fakeRepository) GetAuditLog(context.Context, int64) (AuditState, error) {
	return f.auditState, f.auditErr
}

type fakeAuthorizer struct {
	allowed map[string]bool
}

func (f *fakeAuthorizer) IsSuperAdmin(id string) bool {
	return f != nil && f.allowed[id]
}

type fakeSettingsRefresher struct {
	loads int
	err   error
}

func (f *fakeSettingsRefresher) Load(context.Context) error {
	f.loads++
	return f.err
}

var errTestRepository = errors.New("测试仓库错误")
