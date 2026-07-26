package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

var runtimeServiceActor = management.Actor{ID: "10001", Role: "super_admin", Channel: management.ChannelWebUI, RequestID: "req-1"}

// runtimeServiceStore 是记录调用顺序和操作者的内存状态仓库。
type runtimeServiceStore struct {
	mu            sync.Mutex
	states        map[string]PersistedPluginState
	configs       map[string]PersistedPluginConfig
	events        []string
	actors        []management.Actor
	loadErr       error
	findErr       error
	updateErr     error
	groupErr      error
	findConfigErr error
	saveConfigErr error
}

func (s *runtimeServiceStore) FindConfig(_ context.Context, pluginKey string) (PersistedPluginConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findConfigErr != nil {
		return PersistedPluginConfig{}, s.findConfigErr
	}
	config, found := s.configs[pluginKey]
	if !found {
		return PersistedPluginConfig{}, ErrRuntimeConfigNotFound
	}
	return config, nil
}

func (s *runtimeServiceStore) SaveConfig(_ context.Context, actor management.Actor, pluginKey string, configJSON json.RawMessage, expectedVersion int64) (PersistedPluginConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "persist-config")
	s.actors = append(s.actors, actor)
	if s.saveConfigErr != nil {
		return PersistedPluginConfig{}, s.saveConfigErr
	}
	config, found := s.configs[pluginKey]
	if !found {
		return PersistedPluginConfig{}, ErrRuntimeConfigNotFound
	}
	if config.Version != expectedVersion {
		return PersistedPluginConfig{}, ErrRuntimeStateConflict
	}
	config.ConfigJSON, config.Version, config.UpdatedAt = append(json.RawMessage(nil), configJSON...), config.Version+1, time.Now().UTC()
	s.configs[pluginKey] = config
	return config, nil
}

func newRuntimeServiceStore(states ...PersistedPluginState) *runtimeServiceStore {
	store := &runtimeServiceStore{states: make(map[string]PersistedPluginState, len(states)), configs: make(map[string]PersistedPluginConfig)}
	for _, state := range states {
		if state.Groups == nil {
			state.Groups = make([]PersistedGroupState, 0)
		}
		store.states[state.PluginKey] = state
	}
	return store
}

func (s *runtimeServiceStore) LoadSnapshot(context.Context) ([]PersistedPluginState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	states := make([]PersistedPluginState, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}
	sort.Slice(states, func(first, second int) bool { return states[first].PluginKey < states[second].PluginKey })
	return states, nil
}

func (s *runtimeServiceStore) FindState(_ context.Context, pluginKey string) (PersistedPluginState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findErr != nil {
		return PersistedPluginState{}, s.findErr
	}
	state, found := s.states[pluginKey]
	if !found {
		return PersistedPluginState{}, ErrRuntimeStateNotFound
	}
	state.Groups = append(make([]PersistedGroupState, 0, len(state.Groups)), state.Groups...)
	return state, nil
}

func (s *runtimeServiceStore) UpdateDesiredEnabled(_ context.Context, actor management.Actor, pluginKey string, enabled bool, expectedVersion int64) (PersistedPluginState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "persist")
	s.actors = append(s.actors, actor)
	if s.updateErr != nil {
		return PersistedPluginState{}, s.updateErr
	}
	state, found := s.states[pluginKey]
	if !found {
		return PersistedPluginState{}, ErrRuntimeStateNotFound
	}
	if state.Version != expectedVersion {
		return PersistedPluginState{}, ErrRuntimeStateConflict
	}
	state.DesiredEnabled, state.Version, state.UpdatedAt = enabled, state.Version+1, time.Now().UTC()
	s.states[pluginKey] = state
	return PersistedPluginState{PluginKey: pluginKey, DesiredEnabled: enabled, Version: state.Version, UpdatedAt: state.UpdatedAt}, nil
}

func (s *runtimeServiceStore) SetGroupEnabled(_ context.Context, actor management.Actor, pluginKey string, groupID int64, enabled bool, expectedVersion int64) (PersistedGroupState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "persist-group")
	s.actors = append(s.actors, actor)
	if s.groupErr != nil {
		return PersistedGroupState{}, s.groupErr
	}
	state := s.states[pluginKey]
	saved := PersistedGroupState{GroupID: groupID, Enabled: enabled, Version: expectedVersion + 1, UpdatedAt: time.Now().UTC()}
	state.Groups = append(state.Groups, saved)
	s.states[pluginKey] = state
	return saved, nil
}

func (s *runtimeServiceStore) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.events...)
}

// runtimeServiceLifecycle 把生命周期调用写入与仓库共享的顺序日志。
type runtimeServiceLifecycle struct {
	store      *runtimeServiceStore
	enableErr  error
	disableErr error
}

func (l *runtimeServiceLifecycle) OnEnable(context.Context) error {
	l.store.mu.Lock()
	l.store.events = append(l.store.events, "lifecycle-enable")
	l.store.mu.Unlock()
	return l.enableErr
}

func (l *runtimeServiceLifecycle) OnDisable(context.Context) error {
	l.store.mu.Lock()
	l.store.events = append(l.store.events, "lifecycle-disable")
	l.store.mu.Unlock()
	return l.disableErr
}

type runtimeServiceAuthorizer struct {
	err error
}

func (a *runtimeServiceAuthorizer) Authorize(management.Actor) error {
	return a.err
}

func newRuntimeServiceSubject(t *testing.T, store *runtimeServiceStore, authorizer *runtimeServiceAuthorizer, specs ...PluginSpec) (*RuntimeService, *RuntimeController) {
	t.Helper()
	catalog, err := NewSpecCatalog(specs)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewRuntimeController(catalog)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRuntimeService(catalog, controller, store, authorizer)
	if err != nil {
		t.Fatal(err)
	}
	return service, controller
}

func TestNewRuntimeServiceRejectsMissingDependencies(t *testing.T) {
	catalog, err := NewSpecCatalog([]PluginSpec{validPluginSpec("echo", "echo")})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewRuntimeController(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var typedNilStore *runtimeServiceStore
	tests := []struct {
		name       string
		catalog    *SpecCatalog
		controller *RuntimeController
		store      RuntimeStateStore
		authorizer RuntimeAuthorizer
	}{
		{name: "catalog", controller: controller, store: newRuntimeServiceStore(), authorizer: &runtimeServiceAuthorizer{}},
		{name: "controller", catalog: catalog, store: newRuntimeServiceStore(), authorizer: &runtimeServiceAuthorizer{}},
		{name: "store", catalog: catalog, controller: controller, authorizer: &runtimeServiceAuthorizer{}},
		{name: "typed nil store", catalog: catalog, controller: controller, store: typedNilStore, authorizer: &runtimeServiceAuthorizer{}},
		{name: "authorizer", catalog: catalog, controller: controller, store: newRuntimeServiceStore()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewRuntimeService(test.catalog, test.controller, test.store, test.authorizer)
			if service != nil || err == nil {
				t.Fatalf("NewRuntimeService() = %v,%v", service, err)
			}
		})
	}
}

func TestRuntimeServiceReadsRequireAuthorization(t *testing.T) {
	forbidden := errors.New("forbidden")
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	authorizer := &runtimeServiceAuthorizer{err: forbidden}
	service, _ := newRuntimeServiceSubject(t, store, authorizer, validPluginSpec("echo", "echo"))

	if _, err := service.List(context.Background(), runtimeServiceActor); !errors.Is(err, forbidden) {
		t.Fatalf("List() error = %v", err)
	}
	if _, err := service.Get(context.Background(), runtimeServiceActor, "echo"); !errors.Is(err, forbidden) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 1); !errors.Is(err, forbidden) {
		t.Fatalf("SetGlobalEnabled() error = %v", err)
	}
	if _, err := service.SetGroupEnabled(context.Background(), runtimeServiceActor, "echo", 100, true, 0); !errors.Is(err, forbidden) {
		t.Fatalf("SetGroupEnabled() error = %v", err)
	}
	if len(store.log()) != 0 {
		t.Fatalf("unauthorized calls touched store: %v", store.log())
	}
}

func TestRuntimeServiceListExposesIntentAndRuntimeStatus(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{
		PluginKey: "echo", DesiredEnabled: true, Version: 2, UpdatedAt: time.Now(),
		Groups: []PersistedGroupState{{GroupID: 100, Enabled: true, Version: 1, UpdatedAt: time.Now()}},
	})
	lifecycle := &runtimeServiceLifecycle{store: store, enableErr: errors.New("boom")}
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = lifecycle
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo, validPluginSpec("tools", "tools"))
	if err := controller.Enable(context.Background(), "echo"); err == nil {
		t.Fatal("expected lifecycle failure")
	}

	views, err := service.List(context.Background(), runtimeServiceActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].PluginKey != "echo" || views[1].PluginKey != "tools" {
		t.Fatalf("views = %+v", views)
	}
	if !views[0].DesiredEnabled || views[0].Status != RuntimeFailed || views[0].LastError == "" {
		t.Fatalf("echo view = %+v", views[0])
	}
	if views[0].UpdatedAt.Location() != time.UTC || len(views[0].Groups) != 1 || views[0].Groups[0].GroupID != 100 {
		t.Fatalf("echo groups = %+v", views[0])
	}
	// 目录中存在但尚无状态行的插件必须按默认关闭展示。
	if views[1].DesiredEnabled || views[1].Status != RuntimeDisabled || views[1].Version != 0 {
		t.Fatalf("tools view = %+v", views[1])
	}
	// 代码持有的命令声明必须只读暴露，且身份集合顺序稳定。
	command := views[0].Commands[0]
	if len(views[0].Commands) != 1 || command.Key != "run" || command.Scope != string(CommandScopeGroup) {
		t.Fatalf("commands = %+v", views[0].Commands)
	}
	if !equalStrings(command.AllowedRoles, []string{"group_member"}) || !equalStrings(command.Triggers, []string{"echo"}) {
		t.Fatalf("command roles=%v triggers=%v", command.AllowedRoles, command.Triggers)
	}
}

func TestRuntimeServiceGetReturnsViewAndPropagatesStoreErrors(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{
		PluginKey: "echo", DesiredEnabled: true, Version: 2,
		Groups: []PersistedGroupState{{GroupID: 100, Enabled: true, Version: 1}},
	})
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, validPluginSpec("echo", "echo"))

	view, err := service.Get(context.Background(), runtimeServiceActor, "echo")
	if err != nil {
		t.Fatal(err)
	}
	if view.PluginKey != "echo" || view.DisplayName == "" || !view.DesiredEnabled || view.Version != 2 || len(view.Groups) != 1 {
		t.Fatalf("view = %+v", view)
	}

	storeFailure := errors.New("store failed")
	store.findErr, store.loadErr = storeFailure, storeFailure
	if _, err := service.Get(context.Background(), runtimeServiceActor, "echo"); !errors.Is(err, storeFailure) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := service.List(context.Background(), runtimeServiceActor); !errors.Is(err, storeFailure) {
		t.Fatalf("List() error = %v", err)
	}
	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", false, 2); !errors.Is(err, storeFailure) {
		t.Fatalf("SetGlobalEnabled() error = %v", err)
	}
	if _, err := service.SetGroupEnabled(context.Background(), runtimeServiceActor, "echo", 100, false, 1); !errors.Is(err, storeFailure) {
		t.Fatalf("SetGroupEnabled() error = %v", err)
	}
}

func TestRuntimeServiceGetRejectsUnknownPluginBeforeStore(t *testing.T) {
	store := newRuntimeServiceStore()
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, validPluginSpec("echo", "echo"))
	if _, err := service.Get(context.Background(), runtimeServiceActor, "missing"); !errors.Is(err, ErrRuntimePluginNotFound) {
		t.Fatalf("Get(missing) error = %v", err)
	}
	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "missing", true, 1); !errors.Is(err, ErrRuntimePluginNotFound) {
		t.Fatalf("SetGlobalEnabled(missing) error = %v", err)
	}
	if len(store.log()) != 0 {
		t.Fatalf("unknown plugin touched store: %v", store.log())
	}
}

func TestRuntimeServiceEnablePersistsIntentBeforeLifecycle(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = &runtimeServiceLifecycle{store: store}
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)

	view, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"persist", "lifecycle-enable"}; !equalStrings(store.log(), want) {
		t.Fatalf("order = %v", store.log())
	}
	if !view.DesiredEnabled || view.Version != 2 || view.Status != RuntimeReady {
		t.Fatalf("view = %+v", view)
	}
	if state, _ := controller.State("echo"); state.Status != RuntimeReady {
		t.Fatalf("controller status = %v", state.Status)
	}
	if len(store.actors) != 1 || store.actors[0].ID != "10001" || store.actors[0].RequestID != "req-1" {
		t.Fatalf("actor not forwarded for audit: %+v", store.actors)
	}
}

func TestRuntimeServiceEnableKeepsIntentWhenLifecycleFails(t *testing.T) {
	lifecycleFailure := errors.New("enable failed")
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = &runtimeServiceLifecycle{store: store, enableErr: lifecycleFailure}
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)

	view, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 1)
	if !errors.Is(err, lifecycleFailure) {
		t.Fatalf("SetGlobalEnabled() error = %v", err)
	}
	// 意图保留，运行时停在 failed 并拒绝流量，管理员可看到分歧。
	if !view.DesiredEnabled || view.Status != RuntimeFailed || view.LastError == "" {
		t.Fatalf("view = %+v", view)
	}
	if _, admitted := controller.Admit("echo"); admitted {
		t.Fatal("failed plugin admitted traffic")
	}
	// 故障插件必须先关闭清理才能重新启用。
	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", false, 2); err != nil {
		t.Fatal(err)
	}
	if state, _ := controller.State("echo"); state.Status != RuntimeDisabled {
		t.Fatalf("controller status = %v", state.Status)
	}
}

func TestRuntimeServiceDisableStopsRuntimeBeforePersist(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", DesiredEnabled: true, Version: 3})
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = &runtimeServiceLifecycle{store: store}
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)
	if err := controller.Enable(context.Background(), "echo"); err != nil {
		t.Fatal(err)
	}
	store.events = nil

	view, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"lifecycle-disable", "persist"}; !equalStrings(store.log(), want) {
		t.Fatalf("order = %v", store.log())
	}
	if view.DesiredEnabled || view.Version != 4 || view.Status != RuntimeDisabled {
		t.Fatalf("view = %+v", view)
	}
}

func TestRuntimeServiceDisablePersistFailureLeavesRuntimeStopped(t *testing.T) {
	persistFailure := errors.New("persist failed")
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", DesiredEnabled: true, Version: 3})
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = &runtimeServiceLifecycle{store: store}
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)
	if err := controller.Enable(context.Background(), "echo"); err != nil {
		t.Fatal(err)
	}
	store.updateErr = persistFailure

	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", false, 3); !errors.Is(err, persistFailure) {
		t.Fatalf("SetGlobalEnabled() error = %v", err)
	}
	// 落库失败停在安全侧：运行时已经不再接收流量。
	if _, admitted := controller.Admit("echo"); admitted {
		t.Fatal("runtime still admitted traffic after failed persist")
	}
}

func TestRuntimeServiceDisablePersistsIntentWhenCleanupFails(t *testing.T) {
	cleanupFailure := errors.New("disable failed")
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", DesiredEnabled: true, Version: 3})
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = &runtimeServiceLifecycle{store: store, disableErr: cleanupFailure}
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)
	if err := controller.Enable(context.Background(), "echo"); err != nil {
		t.Fatal(err)
	}

	view, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", false, 3)
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("SetGlobalEnabled() error = %v", err)
	}
	// 清理失败仍需落库关闭意图，避免重启后复活管理员已关停的插件。
	if view.DesiredEnabled || view.Status != RuntimeFailed {
		t.Fatalf("view = %+v", view)
	}
	if state, _ := store.FindState(context.Background(), "echo"); state.DesiredEnabled {
		t.Fatalf("persisted state = %+v", state)
	}
	if _, admitted := controller.Admit("echo"); admitted {
		t.Fatal("failed cleanup still admitted traffic")
	}
}

func TestRuntimeServiceGlobalWriteRejectsStaleVersionAndIsIdempotent(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", DesiredEnabled: true, Version: 3})
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = &runtimeServiceLifecycle{store: store}
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)

	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", false, 2); !errors.Is(err, ErrRuntimeStateConflict) {
		t.Fatalf("stale version error = %v", err)
	}
	view, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 已是目标意图时不写库、不审计、不驱动生命周期。
	if view.Version != 3 || len(store.log()) != 0 {
		t.Fatalf("idempotent call wrote: view=%+v events=%v", view, store.log())
	}
}

func TestRuntimeServiceGroupSwitchOrdersWritesAndKeepsIntentOffline(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, validPluginSpec("echo", "echo"))

	// 全局关闭时仍允许维护群意图。
	view, err := service.SetGroupEnabled(context.Background(), runtimeServiceActor, "echo", 100, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Groups) != 1 || view.Groups[0].GroupID != 100 || !view.Groups[0].Enabled || view.Groups[0].Version != 1 {
		t.Fatalf("view groups = %+v", view.Groups)
	}
	if view.Status != RuntimeDisabled {
		t.Fatalf("group switch changed runtime status: %+v", view)
	}
	if len(store.actors) != 1 || store.actors[0].ID != "10001" {
		t.Fatalf("actor not forwarded for audit: %+v", store.actors)
	}

	if err := controller.Enable(context.Background(), "echo"); err != nil {
		t.Fatal(err)
	}
	admission, admitted := controller.Admit("echo")
	if !admitted || !admission.GroupEnabled(100) || admission.GroupEnabled(200) {
		t.Fatal("group gate not applied to runtime")
	}
	admission.Release()

	if _, err := service.SetGroupEnabled(context.Background(), runtimeServiceActor, "echo", 100, false, 1); err != nil {
		t.Fatal(err)
	}
	admission, admitted = controller.Admit("echo")
	if !admitted || admission.GroupEnabled(100) {
		t.Fatal("group gate not closed after disable")
	}
	admission.Release()
}

func TestRuntimeServiceGroupWriteValidatesVersionAndGroupID(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{
		PluginKey: "echo", Version: 1,
		Groups: []PersistedGroupState{{GroupID: 100, Enabled: true, Version: 2}},
	})
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, validPluginSpec("echo", "echo"))

	tests := []struct {
		name            string
		groupID         int64
		enabled         bool
		expectedVersion int64
		want            error
	}{
		{name: "invalid group", groupID: 0, enabled: true, want: ErrInvalidRuntimeGroupID},
		{name: "stale version", groupID: 100, enabled: false, expectedVersion: 1, want: ErrRuntimeStateConflict},
		{name: "insert over existing", groupID: 100, enabled: false, expectedVersion: 0, want: ErrRuntimeStateConflict},
		{name: "update missing record", groupID: 200, enabled: true, expectedVersion: 1, want: ErrRuntimeStateConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.SetGroupEnabled(context.Background(), runtimeServiceActor, "echo", test.groupID, test.enabled, test.expectedVersion); !errors.Is(err, test.want) {
				t.Fatalf("SetGroupEnabled() error = %v", err)
			}
		})
	}
	// 已是目标值时不产生写入。
	if _, err := service.SetGroupEnabled(context.Background(), runtimeServiceActor, "echo", 100, true, 2); err != nil {
		t.Fatal(err)
	}
	if len(store.log()) != 0 {
		t.Fatalf("rejected or idempotent calls wrote: %v", store.log())
	}
}

func TestRuntimeServiceRejectsTransitionAndRecoveryBeforePersist(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	lifecycle := &runtimeTestLifecycle{enableStarted: make(chan struct{}), enableBlock: make(chan struct{})}
	echo := validPluginSpec("echo", "echo")
	echo.Lifecycle = lifecycle
	service, controller := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, echo)

	go func() { _ = controller.Enable(context.Background(), "echo") }()
	<-lifecycle.enableStarted
	if _, err := service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 1); !errors.Is(err, ErrRuntimeTransition) {
		t.Fatalf("transition error = %v", err)
	}
	close(lifecycle.enableBlock)
	eventually(t, func() bool {
		state, _ := controller.State("echo")
		return state.Status == RuntimeReady
	})

	failing := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	failingSpec := validPluginSpec("echo", "echo")
	failingSpec.Lifecycle = &runtimeServiceLifecycle{store: failing, enableErr: errors.New("boom")}
	failingService, failingController := newRuntimeServiceSubject(t, failing, &runtimeServiceAuthorizer{}, failingSpec)
	if err := failingController.Enable(context.Background(), "echo"); err == nil {
		t.Fatal("expected lifecycle failure")
	}
	failing.events = nil
	if _, err := failingService.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 1); !errors.Is(err, ErrRuntimeRecoveryNeeded) {
		t.Fatalf("recovery error = %v", err)
	}
	if len(store.log()) != 0 || len(failing.log()) != 0 {
		t.Fatal("rejected transition persisted intent")
	}
}

func TestRuntimeServiceSerializesConcurrentWritesPerPlugin(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, validPluginSpec("echo", "echo"))

	var group sync.WaitGroup
	results := make([]error, 8)
	for index := range results {
		group.Add(1)
		go func(slot int) {
			defer group.Done()
			_, results[slot] = service.SetGlobalEnabled(context.Background(), runtimeServiceActor, "echo", true, 1)
		}(index)
	}
	group.Wait()

	succeeded := 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRuntimeStateConflict):
		default:
			t.Fatalf("unexpected error = %v", err)
		}
	}
	// 乐观锁下同一版本只允许一次成功切换，其余必须是冲突。
	if succeeded != 1 {
		t.Fatalf("succeeded = %d", succeeded)
	}
}

func equalStrings(actual, want []string) bool {
	if len(actual) != len(want) {
		return false
	}
	for index := range want {
		if actual[index] != want[index] {
			return false
		}
	}
	return true
}

// runtimeConfigProbe 记录配置钩子的调用顺序与最终发布的快照。
type runtimeConfigProbe struct {
	mu          sync.Mutex
	applied     []string
	validated   []string
	applyErr    error
	applyPanic  bool
	validateErr error
}

func (p *runtimeConfigProbe) validate(_ context.Context, raw json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.validated = append(p.validated, string(raw))
	return p.validateErr
}

func (p *runtimeConfigProbe) apply(_ context.Context, raw json.RawMessage) error {
	p.mu.Lock()
	shouldPanic, err := p.applyPanic, p.applyErr
	p.applied = append(p.applied, string(raw))
	p.mu.Unlock()
	if shouldPanic {
		panic("apply panic")
	}
	return err
}

func (p *runtimeConfigProbe) snapshots() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.applied...)
}

func runtimeConfigSpec(probe *runtimeConfigProbe) PluginSpec {
	spec := validPluginSpec("echo", "echo")
	spec.Config = &ConfigSpec{
		Schema: ConfigSchema{Fields: []ConfigField{
			{Key: "response_prefix", DisplayName: "回复前缀", Type: FieldString, Default: json.RawMessage(`""`)},
			{Key: "token", DisplayName: "令牌", Type: FieldSecret},
		}},
		Validate: probe.validate,
		Apply:    probe.apply,
	}
	return spec
}

func newRuntimeConfigSubject(t *testing.T, probe *runtimeConfigProbe, stored string) (*RuntimeService, *runtimeServiceStore) {
	t.Helper()
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	store.configs["echo"] = PersistedPluginConfig{PluginKey: "echo", ConfigJSON: json.RawMessage(stored), Version: 1}
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, runtimeConfigSpec(probe))
	return service, store
}

func TestRuntimeServiceGetConfigRedactsSecretsAndFillsDefaults(t *testing.T) {
	probe := &runtimeConfigProbe{}
	service, _ := newRuntimeConfigSubject(t, probe, `{"token":"s3cret"}`)
	view, err := service.GetConfig(context.Background(), runtimeServiceActor, "echo")
	if err != nil {
		t.Fatal(err)
	}
	// secret 不得出现在管理读取结果中，缺失字段由 Schema 默认值补齐。
	if strings.Contains(string(view.Config), "s3cret") {
		t.Fatalf("secret leaked: %s", view.Config)
	}
	if !strings.Contains(string(view.Config), `"response_prefix":""`) {
		t.Fatalf("default missing: %s", view.Config)
	}
	if view.Version != 1 || len(view.Schema.Fields) != 2 || view.PluginKey != "echo" {
		t.Fatalf("view = %+v", view)
	}
}

func TestRuntimeServiceConfigRejectsUnsupportedPlugin(t *testing.T) {
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1})
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, validPluginSpec("echo", "echo"))
	if _, err := service.GetConfig(context.Background(), runtimeServiceActor, "echo"); !errors.Is(err, ErrRuntimeConfigNotSupported) {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if _, err := service.SetConfig(context.Background(), runtimeServiceActor, "echo", json.RawMessage(`{}`), 1); !errors.Is(err, ErrRuntimeConfigNotSupported) {
		t.Fatalf("SetConfig() error = %v", err)
	}
	// 未声明配置的插件不应触达存储层。
	if len(store.log()) != 0 {
		t.Fatalf("store called: %v", store.log())
	}
}

func TestRuntimeServiceSetConfigPersistsThenApplies(t *testing.T) {
	probe := &runtimeConfigProbe{}
	service, store := newRuntimeConfigSubject(t, probe, `{"response_prefix":"","token":"s3cret"}`)
	view, err := service.SetConfig(context.Background(), runtimeServiceActor, "echo", json.RawMessage(`{"response_prefix":"[bot] "}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(store.log(), []string{"persist-config"}) {
		t.Fatalf("events = %v", store.log())
	}
	applied := probe.snapshots()
	// 未提交的 secret 必须保留原值，而不是被合并成空字符串。
	if len(applied) != 1 || !strings.Contains(applied[0], `"token":"s3cret"`) || !strings.Contains(applied[0], `"response_prefix":"[bot] "`) {
		t.Fatalf("applied = %v", applied)
	}
	if view.Version != 2 || strings.Contains(string(view.Config), "s3cret") {
		t.Fatalf("view = %+v", view)
	}
	if len(store.actors) != 1 || store.actors[0].RequestID != "req-1" {
		t.Fatalf("actor not forwarded for audit: %+v", store.actors)
	}
}

func TestRuntimeServiceSetConfigRejectsStaleVersionAndInvalidInput(t *testing.T) {
	probe := &runtimeConfigProbe{}
	service, store := newRuntimeConfigSubject(t, probe, `{"response_prefix":""}`)
	if _, err := service.SetConfig(context.Background(), runtimeServiceActor, "echo", json.RawMessage(`{}`), 5); !errors.Is(err, ErrRuntimeStateConflict) {
		t.Fatalf("stale version error = %v", err)
	}
	// 未知字段必须在写库前被规范化拒绝。
	if _, err := service.SetConfig(context.Background(), runtimeServiceActor, "echo", json.RawMessage(`{"unknown":1}`), 1); !errors.Is(err, ErrRuntimeConfigInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
	probe.validateErr = errors.New("domain rejected")
	if _, err := service.SetConfig(context.Background(), runtimeServiceActor, "echo", json.RawMessage(`{"response_prefix":"x"}`), 1); !errors.Is(err, ErrRuntimeConfigInvalid) {
		t.Fatalf("domain validation error = %v", err)
	}
	if len(store.log()) != 0 || len(probe.snapshots()) != 0 {
		t.Fatalf("rejected input reached persistence: events=%v applied=%v", store.log(), probe.snapshots())
	}
}

func TestRuntimeServiceSetConfigCompensatesWhenApplyFails(t *testing.T) {
	tests := []struct {
		name       string
		applyErr   error
		applyPanic bool
	}{
		{name: "返回错误", applyErr: errors.New("apply failed")},
		{name: "发生 panic", applyPanic: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := &runtimeConfigProbe{applyErr: test.applyErr, applyPanic: test.applyPanic}
			service, store := newRuntimeConfigSubject(t, probe, `{"response_prefix":"旧"}`)
			_, err := service.SetConfig(context.Background(), runtimeServiceActor, "echo", json.RawMessage(`{"response_prefix":"新"}`), 1)
			if err == nil {
				t.Fatal("expected apply failure")
			}
			// 热应用失败后数据库必须补偿回旧值，避免持久化配置与运行快照长期不一致。
			restored := store.configs["echo"]
			if !strings.Contains(string(restored.ConfigJSON), "旧") {
				t.Fatalf("config not compensated: %s", restored.ConfigJSON)
			}
			if !equalStrings(store.log(), []string{"persist-config", "persist-config"}) {
				t.Fatalf("events = %v", store.log())
			}
		})
	}
}

func TestRuntimeServiceStateViewMarksConfigurablePlugins(t *testing.T) {
	probe := &runtimeConfigProbe{}
	store := newRuntimeServiceStore(PersistedPluginState{PluginKey: "echo", Version: 1}, PersistedPluginState{PluginKey: "tools", Version: 1})
	service, _ := newRuntimeServiceSubject(t, store, &runtimeServiceAuthorizer{}, runtimeConfigSpec(probe), validPluginSpec("tools", "tools"))
	views, err := service.List(context.Background(), runtimeServiceActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || !views[0].HasConfig || views[1].HasConfig {
		t.Fatalf("has_config = %v,%v", views[0].HasConfig, views[1].HasConfig)
	}
}
