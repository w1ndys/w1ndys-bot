package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type runtimeBootstrapRepository struct {
	states      []PersistedPluginState
	configs     []PersistedPluginConfig
	syncErr     error
	loadErr     error
	configErr   error
	syncCalls   int
	loadCalls   int
	configCalls int
}

func (r *runtimeBootstrapRepository) SyncCatalog(context.Context, *SpecCatalog) error {
	r.syncCalls++
	return r.syncErr
}

func (r *runtimeBootstrapRepository) LoadSnapshot(context.Context) ([]PersistedPluginState, error) {
	r.loadCalls++
	return r.states, r.loadErr
}

func (r *runtimeBootstrapRepository) LoadConfigs(context.Context) ([]PersistedPluginConfig, error) {
	r.configCalls++
	return r.configs, r.configErr
}

func runtimeBootstrapSpec(key string, trigger string, lifecycle Lifecycle) PluginSpec {
	spec := validPluginSpec(key, trigger)
	spec.Lifecycle = lifecycle
	return spec
}

func newRuntimeBootstrapTestSubject(t *testing.T, repository RuntimeSnapshotRepository, specs ...PluginSpec) (*RuntimeBootstrap, *RuntimeController) {
	t.Helper()
	catalog, err := NewSpecCatalog(specs)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewRuntimeController(catalog)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := NewRuntimeBootstrap(catalog, controller, repository)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrap, controller
}

func TestRuntimeBootstrapRestoresKnownStatesFailClosed(t *testing.T) {
	enabledLifecycle := &runtimeTestLifecycle{}
	disabledLifecycle := &runtimeTestLifecycle{}
	repository := &runtimeBootstrapRepository{states: []PersistedPluginState{
		{PluginKey: "disabled", DesiredEnabled: false, Groups: []PersistedGroupState{{GroupID: 100, Enabled: true}}},
		{PluginKey: "enabled", DesiredEnabled: true, Groups: []PersistedGroupState{{GroupID: 100, Enabled: true}, {GroupID: 200, Enabled: false}}},
		{PluginKey: "stale", DesiredEnabled: true},
	}}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository,
		runtimeBootstrapSpec("disabled", "disabled", disabledLifecycle),
		runtimeBootstrapSpec("enabled", "enabled", enabledLifecycle),
		runtimeBootstrapSpec("missing", "missing", nil),
	)
	if err := bootstrap.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.syncCalls != 1 || repository.loadCalls != 1 || enabledLifecycle.enableCalls != 1 || disabledLifecycle.enableCalls != 0 {
		t.Fatalf("calls sync=%d load=%d enabled=%d disabled=%d", repository.syncCalls, repository.loadCalls, enabledLifecycle.enableCalls, disabledLifecycle.enableCalls)
	}
	state, _ := controller.State("enabled")
	if state.Status != RuntimeReady {
		t.Fatalf("enabled State() = %+v", state)
	}
	admission, admitted := controller.Admit("enabled")
	if !admitted || !admission.GroupEnabled(100) || admission.GroupEnabled(200) {
		t.Fatal("enabled group snapshot not restored")
	}
	admission.Release()
	for _, key := range []string{"disabled", "missing"} {
		state, _ := controller.State(key)
		if state.Status != RuntimeDisabled {
			t.Fatalf("%s State() = %+v", key, state)
		}
	}
}

func TestRuntimeBootstrapStopsBeforeLoadWhenSyncFails(t *testing.T) {
	syncFailure := errors.New("sync failed")
	repository := &runtimeBootstrapRepository{syncErr: syncFailure}
	bootstrap, _ := newRuntimeBootstrapTestSubject(t, repository, runtimeBootstrapSpec("echo", "echo", nil))
	if err := bootstrap.Initialize(context.Background()); !errors.Is(err, syncFailure) {
		t.Fatalf("Initialize() error = %v", err)
	}
	if repository.loadCalls != 0 {
		t.Fatalf("load calls = %d", repository.loadCalls)
	}
}

func TestRuntimeBootstrapPropagatesLoadFailure(t *testing.T) {
	loadFailure := errors.New("load failed")
	repository := &runtimeBootstrapRepository{loadErr: loadFailure}
	bootstrap, _ := newRuntimeBootstrapTestSubject(t, repository, runtimeBootstrapSpec("echo", "echo", nil))
	if err := bootstrap.Initialize(context.Background()); !errors.Is(err, loadFailure) {
		t.Fatalf("Initialize() error = %v", err)
	}
}

func TestRuntimeBootstrapCleansEnabledPluginsAfterLifecycleFailure(t *testing.T) {
	failure := errors.New("enable failed")
	first := &runtimeTestLifecycle{}
	second := &runtimeTestLifecycle{enableErr: failure}
	repository := &runtimeBootstrapRepository{states: []PersistedPluginState{
		{PluginKey: "first", DesiredEnabled: true},
		{PluginKey: "second", DesiredEnabled: true},
	}}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository,
		runtimeBootstrapSpec("first", "first", first),
		runtimeBootstrapSpec("second", "second", second),
	)
	if err := bootstrap.Initialize(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("Initialize() error = %v", err)
	}
	if first.disableCalls != 1 || second.disableCalls != 1 {
		t.Fatalf("cleanup calls first=%d second=%d", first.disableCalls, second.disableCalls)
	}
	for _, key := range []string{"first", "second"} {
		state, _ := controller.State(key)
		if state.Status != RuntimeDisabled {
			t.Fatalf("%s State() = %+v", key, state)
		}
	}
}

func TestRuntimeBootstrapShutdownDisablesReadyPlugins(t *testing.T) {
	order := make([]string, 0, 4)
	first := &orderedBootstrapLifecycle{record: func(action string) { order = append(order, "first:"+action) }}
	second := &orderedBootstrapLifecycle{record: func(action string) { order = append(order, "second:"+action) }}
	repository := &runtimeBootstrapRepository{states: []PersistedPluginState{
		{PluginKey: "first", DesiredEnabled: true},
		{PluginKey: "second", DesiredEnabled: true},
	}}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository,
		runtimeBootstrapSpec("first", "first", first),
		runtimeBootstrapSpec("second", "second", second),
	)
	if err := bootstrap.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"first:enable", "second:enable", "second:disable", "first:disable"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("lifecycle order = %#v, want %#v", order, wantOrder)
	}
	for _, key := range []string{"first", "second"} {
		state, _ := controller.State(key)
		if state.Status != RuntimeDisabled {
			t.Fatalf("%s State() = %+v", key, state)
		}
	}
}

func TestRuntimeBootstrapCleansEnabledPluginsAfterInvalidGroup(t *testing.T) {
	lifecycle := &runtimeTestLifecycle{}
	repository := &runtimeBootstrapRepository{states: []PersistedPluginState{
		{PluginKey: "first", DesiredEnabled: true},
		{PluginKey: "second", Groups: []PersistedGroupState{{GroupID: 0, Enabled: true}}},
	}}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository,
		runtimeBootstrapSpec("first", "first", lifecycle),
		runtimeBootstrapSpec("second", "second", nil),
	)
	if err := bootstrap.Initialize(context.Background()); !errors.Is(err, ErrInvalidRuntimeGroupID) {
		t.Fatalf("Initialize() error = %v", err)
	}
	state, _ := controller.State("first")
	if state.Status != RuntimeDisabled || lifecycle.enableCalls != 0 || lifecycle.disableCalls != 0 {
		t.Fatalf("first State()=%+v disableCalls=%d", state, lifecycle.disableCalls)
	}
}

func TestRuntimeBootstrapRejectsDuplicateSnapshotBeforeSideEffects(t *testing.T) {
	tests := []struct {
		name   string
		states []PersistedPluginState
	}{
		{name: "plugin", states: []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true}, {PluginKey: "echo"}}},
		{name: "group", states: []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true, Groups: []PersistedGroupState{{GroupID: 100}, {GroupID: 100}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &runtimeTestLifecycle{}
			repository := &runtimeBootstrapRepository{states: test.states}
			bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository, runtimeBootstrapSpec("echo", "echo", lifecycle))
			if err := bootstrap.Initialize(context.Background()); err == nil {
				t.Fatal("duplicate snapshot accepted")
			}
			state, _ := controller.State("echo")
			if state.Status != RuntimeDisabled || lifecycle.enableCalls != 0 {
				t.Fatalf("State()=%+v enableCalls=%d", state, lifecycle.enableCalls)
			}
		})
	}
}

func TestRuntimeBootstrapReplacesCompleteGroupSnapshot(t *testing.T) {
	repository := &runtimeBootstrapRepository{states: []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true, Groups: []PersistedGroupState{{GroupID: 100, Enabled: true}}}}}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository, runtimeBootstrapSpec("echo", "echo", nil))
	if err := bootstrap.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	repository.states = []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true}}
	if err := bootstrap.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	admission, admitted := controller.Admit("echo")
	if !admitted {
		t.Fatal("echo not ready")
	}
	defer admission.Release()
	if admission.GroupEnabled(100) {
		t.Fatal("removed group state remained enabled")
	}
}

func TestRuntimeBootstrapReconcilesDisabledAndMissingPlugins(t *testing.T) {
	tests := []struct {
		name       string
		secondLoad []PersistedPluginState
	}{
		{name: "disabled", secondLoad: []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: false, Groups: []PersistedGroupState{{GroupID: 100, Enabled: true}}}}},
		{name: "missing", secondLoad: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &runtimeTestLifecycle{}
			repository := &runtimeBootstrapRepository{states: []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true, Groups: []PersistedGroupState{{GroupID: 100, Enabled: true}}}}}
			bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository, runtimeBootstrapSpec("echo", "echo", lifecycle))
			if err := bootstrap.Initialize(context.Background()); err != nil {
				t.Fatal(err)
			}
			repository.states = test.secondLoad
			if err := bootstrap.Initialize(context.Background()); err != nil {
				t.Fatal(err)
			}
			state, _ := controller.State("echo")
			if state.Status != RuntimeDisabled || lifecycle.disableCalls != 1 {
				t.Fatalf("State()=%+v disableCalls=%d", state, lifecycle.disableCalls)
			}
			if err := controller.Enable(context.Background(), "echo"); err != nil {
				t.Fatal(err)
			}
			admission, admitted := controller.Admit("echo")
			if !admitted {
				t.Fatal("echo not admitted after re-enable")
			}
			defer admission.Release()
			wantGroupEnabled := test.name == "disabled"
			if admission.GroupEnabled(100) != wantGroupEnabled {
				t.Fatalf("GroupEnabled(100) = %t", admission.GroupEnabled(100))
			}
		})
	}
}

func TestNewRuntimeBootstrapRejectsMissingDependencies(t *testing.T) {
	repository := &runtimeBootstrapRepository{}
	var typedNilRepository *runtimeBootstrapRepository
	catalog, _ := NewSpecCatalog(nil)
	controller, _ := NewRuntimeController(catalog)
	tests := []struct {
		catalog    *SpecCatalog
		controller *RuntimeController
		repository RuntimeSnapshotRepository
	}{
		{controller: controller, repository: repository},
		{catalog: catalog, repository: repository},
		{catalog: catalog, controller: controller},
		{catalog: catalog, controller: controller, repository: typedNilRepository},
	}
	for _, test := range tests {
		bootstrap, err := NewRuntimeBootstrap(test.catalog, test.controller, test.repository)
		if bootstrap != nil || err == nil {
			t.Fatalf("NewRuntimeBootstrap() = %v,%v", bootstrap, err)
		}
	}
}

func TestRuntimeBootstrapAppliesConfigBeforeEnabling(t *testing.T) {
	order := make([]string, 0, 2)
	lifecycle := &orderedBootstrapLifecycle{record: func(event string) { order = append(order, event) }}
	spec := runtimeBootstrapSpec("echo", "echo", lifecycle)
	spec.Config = &ConfigSpec{
		Schema: ConfigSchema{Fields: []ConfigField{{Key: "response_prefix", DisplayName: "回复前缀", Type: FieldString, Default: json.RawMessage(`""`)}}},
		Apply: func(_ context.Context, raw json.RawMessage) error {
			order = append(order, "apply:"+string(raw))
			return nil
		},
	}
	repository := &runtimeBootstrapRepository{
		states:  []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true, Version: 2}},
		configs: []PersistedPluginConfig{{PluginKey: "echo", ConfigJSON: json.RawMessage(`{"response_prefix":"[bot] "}`), Version: 3}},
	}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository, spec)
	if err := bootstrap.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 配置必须在生命周期启用之前发布，插件不能带着未初始化快照进入 Ready。
	if len(order) != 2 || !strings.HasPrefix(order[0], "apply:") || order[1] != "enable" {
		t.Fatalf("order = %v", order)
	}
	if !strings.Contains(order[0], `"response_prefix":"[bot] "`) {
		t.Fatalf("applied config = %q", order[0])
	}
	if state, _ := controller.State("echo"); state.Status != RuntimeReady {
		t.Fatalf("status = %v", state.Status)
	}
}

func TestRuntimeBootstrapStopsWhenConfigFails(t *testing.T) {
	applyFailure := errors.New("apply failed")
	lifecycle := &orderedBootstrapLifecycle{record: func(string) {}}
	spec := runtimeBootstrapSpec("echo", "echo", lifecycle)
	spec.Config = &ConfigSpec{
		Schema: ConfigSchema{Fields: []ConfigField{{Key: "response_prefix", DisplayName: "回复前缀", Type: FieldString, Default: json.RawMessage(`""`)}}},
		Apply:  func(context.Context, json.RawMessage) error { return applyFailure },
	}
	repository := &runtimeBootstrapRepository{states: []PersistedPluginState{{PluginKey: "echo", DesiredEnabled: true, Version: 2}}}
	bootstrap, controller := newRuntimeBootstrapTestSubject(t, repository, spec)
	if err := bootstrap.Initialize(context.Background()); !errors.Is(err, applyFailure) {
		t.Fatalf("Initialize() error = %v", err)
	}
	// 配置失败必须阻止插件进入 Ready，避免带着错误配置接收流量。
	if state, _ := controller.State("echo"); state.Status != RuntimeDisabled {
		t.Fatalf("status = %v", state.Status)
	}
}

// orderedBootstrapLifecycle 把启停调用写入共享顺序日志。
type orderedBootstrapLifecycle struct {
	record func(string)
}

func (l *orderedBootstrapLifecycle) OnEnable(context.Context) error {
	l.record("enable")
	return nil
}

func (l *orderedBootstrapLifecycle) OnDisable(context.Context) error {
	l.record("disable")
	return nil
}
