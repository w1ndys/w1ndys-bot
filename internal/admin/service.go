// 📌 影响范围：管理系统设置、审计查询与最高管理员授权；不再承载旧插件元数据、命令或权限矩阵。
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

type RuntimeRefresher interface {
	Load(context.Context) error
}

type AdminAuthorizer interface {
	IsSuperAdmin(string) bool
}

type Service struct {
	repository Repository
	settings   RuntimeRefresher
	authorizer AdminAuthorizer
}

func NewService(repository Repository, settings RuntimeRefresher, authorizer AdminAuthorizer) *Service {
	return &Service{repository: repository, settings: settings, authorizer: authorizer}
}

func (s *Service) ListSettings(ctx context.Context, actor Actor) ([]SettingState, error) {
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	stored, err := s.repository.ListSystemSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出系统设置: %w", err)
	}
	byKey := make(map[string]SettingState, len(stored))
	for _, state := range stored {
		byKey[state.Key] = state
	}
	definitions := Definitions()
	result := make([]SettingState, 0, len(definitions))
	for key, definition := range definitions {
		state, exists := byKey[key]
		if !exists {
			state = SettingState{Key: key, Value: append(json.RawMessage(nil), definition.Default...), Description: definition.Description}
		}
		result = append(result, state)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *Service) SetSetting(ctx context.Context, actor Actor, key string, value json.RawMessage) (SettingState, error) {
	if err := s.authorize(actor); err != nil {
		return SettingState{}, err
	}
	definition, exists := settingDefinitions[key]
	if !exists {
		return SettingState{}, fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	if err := validateSetting(key, value); err != nil {
		return SettingState{}, fmt.Errorf("%w: %v", ErrInvalidSetting, err)
	}
	setting := SettingState{Key: key, Value: append(json.RawMessage(nil), value...), Description: definition.Description, Overridden: true}
	saved, err := s.repository.SetSystemSetting(ctx, actor, setting)
	if err != nil {
		return SettingState{}, fmt.Errorf("保存系统设置: %w", err)
	}
	if err := s.reloadSettings(ctx); err != nil {
		return saved, err
	}
	return saved, nil
}

func (s *Service) DeleteSetting(ctx context.Context, actor Actor, key string) error {
	if err := s.authorize(actor); err != nil {
		return err
	}
	if _, exists := settingDefinitions[key]; !exists {
		return fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	if err := s.repository.DeleteSystemSetting(ctx, actor, key); err != nil {
		return fmt.Errorf("删除系统设置: %w", err)
	}
	return s.reloadSettings(ctx)
}

func (s *Service) reloadSettings(ctx context.Context) error {
	if s.settings == nil {
		return nil
	}
	if err := s.settings.Load(ctx); err != nil {
		return fmt.Errorf("刷新系统设置快照: %w", err)
	}
	return nil
}

func (s *Service) Authorize(actor Actor) error {
	return s.authorize(actor)
}

func (s *Service) authorize(actor Actor) error {
	if actor.ID == "" {
		return ErrInvalidActor
	}
	if !validChannel(actor.Channel) {
		return ErrInvalidChannel
	}
	if actor.Channel != ChannelSystem && (s.authorizer == nil || !s.authorizer.IsSuperAdmin(actor.ID)) {
		return ErrForbidden
	}
	return nil
}

func validChannel(channel Channel) bool {
	return channel == ChannelWebUI || channel == ChannelQQ || channel == ChannelSystem
}
