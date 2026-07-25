package plugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type runtimeStateTestDatabase struct {
	execSQL   string
	execArgs  []any
	execErr   error
	execCalls int
	querySQL  string
	queryErr  error
	rows      pgx.Rows
}

func (d *runtimeStateTestDatabase) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	d.execCalls++
	d.execSQL = sql
	d.execArgs = args
	return pgconn.NewCommandTag("INSERT 0 1"), d.execErr
}

func (d *runtimeStateTestDatabase) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	d.querySQL = sql
	return d.rows, d.queryErr
}

type runtimeStateTestRows struct {
	values  [][]any
	index   int
	err     error
	scanErr error
	closed  bool
}

func (r *runtimeStateTestRows) Close()                                       { r.closed = true }
func (r *runtimeStateTestRows) Err() error                                   { return r.err }
func (r *runtimeStateTestRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *runtimeStateTestRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *runtimeStateTestRows) Values() ([]any, error)                       { return nil, errors.New("unused") }
func (r *runtimeStateTestRows) RawValues() [][]byte                          { return nil }
func (r *runtimeStateTestRows) Conn() *pgx.Conn                              { return nil }

func (r *runtimeStateTestRows) Next() bool {
	if r.index >= len(r.values) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *runtimeStateTestRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.values[r.index-1]
	if len(dest) != len(row) {
		return errors.New("scan destination count mismatch")
	}
	for index, value := range row {
		target := reflect.ValueOf(dest[index]).Elem()
		if value == nil {
			target.SetZero()
			continue
		}
		if target.Kind() == reflect.Pointer {
			pointer := reflect.New(target.Type().Elem())
			pointer.Elem().Set(reflect.ValueOf(value))
			target.Set(pointer)
			continue
		}
		target.Set(reflect.ValueOf(value))
	}
	return nil
}

func TestRuntimeStateRepositorySyncCatalogDefaultsMissingPluginsOff(t *testing.T) {
	catalog, err := NewSpecCatalog([]PluginSpec{validPluginSpec("tools", "tools"), validPluginSpec("echo", "echo")})
	if err != nil {
		t.Fatal(err)
	}
	database := &runtimeStateTestDatabase{}
	repository := &PostgresRuntimeStateRepository{database: database}
	if err := repository.SyncCatalog(context.Background(), catalog); err != nil {
		t.Fatal(err)
	}
	wantSQL := `INSERT INTO plugin_states(plugin_key,desired_enabled)
SELECT plugin_key,FALSE FROM unnest($1::text[]) AS plugin_key
ON CONFLICT (plugin_key) DO NOTHING`
	if database.execCalls != 1 || normalizeRuntimeStateSQL(database.execSQL) != normalizeRuntimeStateSQL(wantSQL) {
		t.Fatalf("Exec() calls=%d sql=%q", database.execCalls, database.execSQL)
	}
	keys, ok := database.execArgs[0].([]string)
	if !ok || !reflect.DeepEqual(keys, []string{"echo", "tools"}) {
		t.Fatalf("keys = %#v", database.execArgs)
	}
}

func TestRuntimeStateRepositorySyncEmptyCatalogSkipsDatabase(t *testing.T) {
	catalog, err := NewSpecCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	database := &runtimeStateTestDatabase{}
	repository := &PostgresRuntimeStateRepository{database: database}
	if err := repository.SyncCatalog(context.Background(), catalog); err != nil || database.execCalls != 0 {
		t.Fatalf("SyncCatalog() error=%v calls=%d", err, database.execCalls)
	}
}

func TestRuntimeStateRepositoryLoadsGroupedUTCSnapshot(t *testing.T) {
	zone := time.FixedZone("test", 8*60*60)
	pluginTime := time.Date(2026, 7, 25, 20, 0, 0, 0, zone)
	groupTime := pluginTime.Add(time.Minute)
	rows := &runtimeStateTestRows{values: [][]any{
		{"echo", false, int64(1), pluginTime, nil, nil, nil, nil},
		{"monitor", true, int64(3), pluginTime, int64(100), true, int64(2), groupTime},
		{"monitor", true, int64(3), pluginTime, int64(200), false, int64(4), groupTime},
	}}
	repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{rows: rows}}
	states, err := repository.LoadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states[0].PluginKey != "echo" || states[0].DesiredEnabled || len(states[0].Groups) != 0 {
		t.Fatalf("echo state = %+v", states)
	}
	monitor := states[1]
	if monitor.PluginKey != "monitor" || !monitor.DesiredEnabled || monitor.Version != 3 || len(monitor.Groups) != 2 {
		t.Fatalf("monitor state = %+v", monitor)
	}
	if monitor.UpdatedAt.Location() != time.UTC || monitor.Groups[0].UpdatedAt.Location() != time.UTC {
		t.Fatalf("timestamps not UTC: %+v", monitor)
	}
	if monitor.Groups[0].GroupID != 100 || !monitor.Groups[0].Enabled || monitor.Groups[1].GroupID != 200 || monitor.Groups[1].Enabled {
		t.Fatalf("groups = %+v", monitor.Groups)
	}
	if !rows.closed {
		t.Fatal("rows not closed")
	}
	database := repository.database.(*runtimeStateTestDatabase)
	wantQuery := `SELECT s.plugin_key,s.desired_enabled,s.version,s.updated_at,
       g.group_id,g.enabled,g.version,g.updated_at
FROM plugin_states s
LEFT JOIN plugin_group_states g ON g.plugin_key=s.plugin_key
ORDER BY s.plugin_key,g.group_id`
	if normalizeRuntimeStateSQL(database.querySQL) != normalizeRuntimeStateSQL(wantQuery) {
		t.Fatalf("Query() sql = %q", database.querySQL)
	}
}

func TestRuntimeStateRepositoryPropagatesDatabaseErrors(t *testing.T) {
	databaseFailure := errors.New("database failed")
	catalog, _ := NewSpecCatalog([]PluginSpec{validPluginSpec("echo", "echo")})
	repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{execErr: databaseFailure}}
	if err := repository.SyncCatalog(context.Background(), catalog); !errors.Is(err, databaseFailure) {
		t.Fatalf("SyncCatalog() error = %v", err)
	}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{queryErr: databaseFailure}}
	if _, err := repository.LoadSnapshot(context.Background()); !errors.Is(err, databaseFailure) {
		t.Fatalf("LoadSnapshot query error = %v", err)
	}
	rows := &runtimeStateTestRows{values: [][]any{{"echo"}}, scanErr: databaseFailure}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{rows: rows}}
	if _, err := repository.LoadSnapshot(context.Background()); !errors.Is(err, databaseFailure) {
		t.Fatalf("LoadSnapshot scan error = %v", err)
	}
	rows = &runtimeStateTestRows{err: databaseFailure}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{rows: rows}}
	if _, err := repository.LoadSnapshot(context.Background()); !errors.Is(err, databaseFailure) {
		t.Fatalf("LoadSnapshot rows error = %v", err)
	}
}

func TestRuntimeStateRepositoryRejectsMissingDependencies(t *testing.T) {
	if repository, err := NewPostgresRuntimeStateRepository(nil); repository != nil || err == nil {
		t.Fatalf("NewPostgresRuntimeStateRepository(nil) = %v,%v", repository, err)
	}
	var repository *PostgresRuntimeStateRepository
	if err := repository.SyncCatalog(context.Background(), nil); err == nil {
		t.Fatal("nil repository SyncCatalog succeeded")
	}
	if _, err := repository.LoadSnapshot(context.Background()); err == nil {
		t.Fatal("nil repository LoadSnapshot succeeded")
	}
	repository = &PostgresRuntimeStateRepository{}
	if err := repository.SyncCatalog(context.Background(), nil); err == nil {
		t.Fatal("repository without database SyncCatalog succeeded")
	}
	if _, err := repository.LoadSnapshot(context.Background()); err == nil {
		t.Fatal("repository without database LoadSnapshot succeeded")
	}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{}}
	if err := repository.SyncCatalog(context.Background(), nil); err == nil {
		t.Fatal("SyncCatalog with nil catalog succeeded")
	}
}

func normalizeRuntimeStateSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
