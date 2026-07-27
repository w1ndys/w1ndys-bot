package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSetSettingAuthorizesBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, nil, &fakeAuthorizer{})

	_, err := service.SetSetting(context.Background(), Actor{ID: "200", Channel: ChannelWebUI}, "command_prefix", json.RawMessage(`"!"`))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("SetSetting() error = %v, want ErrForbidden", err)
	}
	if repository.setting.Key != "" {
		t.Fatalf("repository setting = %+v, want no write", repository.setting)
	}
}

func TestSetSettingPersistsAndRefreshes(t *testing.T) {
	repository := &fakeRepository{}
	refresher := &fakeSettingsRefresher{}
	service := NewService(repository, refresher, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	actor := Actor{ID: "100", Role: "super_admin", Channel: ChannelWebUI, RequestID: "req-1"}

	saved, err := service.SetSetting(context.Background(), actor, "command_prefix", json.RawMessage(`"!"`))
	if err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	if saved.Key != "command_prefix" || string(saved.Value) != `"!"` || !saved.Overridden {
		t.Fatalf("SetSetting() = %+v", saved)
	}
	if repository.setActor != actor || repository.setting.Description == "" {
		t.Fatalf("repository actor=%+v setting=%+v", repository.setActor, repository.setting)
	}
	if refresher.loads != 1 {
		t.Fatalf("refresher loads = %d, want 1", refresher.loads)
	}
}

func TestSetSettingDoesNotRefreshAfterRepositoryFailure(t *testing.T) {
	repository := &fakeRepository{setSettingErr: errTestRepository}
	refresher := &fakeSettingsRefresher{}
	service := NewService(repository, refresher, &fakeAuthorizer{allowed: map[string]bool{"100": true}})

	_, err := service.SetSetting(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, "command_prefix", json.RawMessage(`"!"`))
	if !errors.Is(err, errTestRepository) {
		t.Fatalf("SetSetting() error = %v, want repository error", err)
	}
	if refresher.loads != 0 {
		t.Fatalf("refresher loads = %d, want 0", refresher.loads)
	}
}

func TestSetSettingReturnsSavedStateWhenRefreshFails(t *testing.T) {
	repository := &fakeRepository{}
	refresher := &fakeSettingsRefresher{err: errTestRepository}
	service := NewService(repository, refresher, &fakeAuthorizer{allowed: map[string]bool{"100": true}})

	saved, err := service.SetSetting(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, "default_page_size", json.RawMessage(`50`))
	if !errors.Is(err, errTestRepository) {
		t.Fatalf("SetSetting() error = %v, want refresh error", err)
	}
	if saved.Key != "default_page_size" || string(saved.Value) != "50" {
		t.Fatalf("SetSetting() saved = %+v", saved)
	}
}

func TestDeleteSettingPersistsAndRefreshes(t *testing.T) {
	repository := &fakeRepository{}
	refresher := &fakeSettingsRefresher{}
	service := NewService(repository, refresher, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	actor := Actor{ID: "100", Channel: ChannelQQ}

	if err := service.DeleteSetting(context.Background(), actor, "command_prefix"); err != nil {
		t.Fatalf("DeleteSetting() error = %v", err)
	}
	if repository.deletedKey != "command_prefix" || repository.deleteActor != actor {
		t.Fatalf("repository key=%q actor=%+v", repository.deletedKey, repository.deleteActor)
	}
	if refresher.loads != 1 {
		t.Fatalf("refresher loads = %d, want 1", refresher.loads)
	}
}
