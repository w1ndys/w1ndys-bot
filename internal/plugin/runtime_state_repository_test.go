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
	rowSQL    string
	rowArgs   []any
	row       pgx.Row
}

func (d *runtimeStateTestDatabase) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	d.rowSQL = sql
	d.rowArgs = args
	return d.row
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

type runtimeStateTestRow struct {
	values []any
	err    error
}

func (r runtimeStateTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return assignRuntimeStateValues(dest, r.values)
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
	return assignRuntimeStateValues(dest, r.values[r.index-1])
}

func assignRuntimeStateValues(dest []any, values []any) error {
	if len(dest) != len(values) {
		return errors.New("scan destination count mismatch")
	}
	for index, value := range values {
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

func TestRuntimeStateRepositoryUpdatesDesiredEnabledWithCAS(t *testing.T) {
	zone := time.FixedZone("test", 8*60*60)
	updatedAt := time.Date(2026, 7, 25, 20, 0, 0, 0, zone)
	database := &runtimeStateTestDatabase{row: runtimeStateTestRow{values: []any{"echo", true, int64(3), updatedAt}}}
	repository := &PostgresRuntimeStateRepository{database: database}
	state, err := repository.UpdateDesiredEnabled(context.Background(), "echo", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `UPDATE plugin_states
SET desired_enabled=$2,version=version+1,updated_at=NOW()
WHERE plugin_key=$1 AND version=$3
RETURNING plugin_key,desired_enabled,version,updated_at`
	if normalizeRuntimeStateSQL(database.rowSQL) != normalizeRuntimeStateSQL(wantSQL) || !reflect.DeepEqual(database.rowArgs, []any{"echo", true, int64(2)}) {
		t.Fatalf("QueryRow() sql=%q args=%#v", database.rowSQL, database.rowArgs)
	}
	if state.PluginKey != "echo" || !state.DesiredEnabled || state.Version != 3 || state.UpdatedAt.Location() != time.UTC || len(state.Groups) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

func TestRuntimeStateRepositorySetsGroupWithInsertOrCASUpdate(t *testing.T) {
	updatedAt := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		expectedVersion int64
		wantSQL         string
		wantArgs        []any
		returnedVersion int64
	}{
		{
			name: "insert", expectedVersion: 0, returnedVersion: 1,
			wantSQL: `INSERT INTO plugin_group_states(plugin_key,group_id,enabled)
VALUES($1,$2,$3)
ON CONFLICT (plugin_key,group_id) DO NOTHING
RETURNING group_id,enabled,version,updated_at`,
			wantArgs: []any{"echo", int64(100), true},
		},
		{
			name: "update", expectedVersion: 2, returnedVersion: 3,
			wantSQL: `UPDATE plugin_group_states
SET enabled=$3,version=version+1,updated_at=NOW()
WHERE plugin_key=$1 AND group_id=$2 AND version=$4
RETURNING group_id,enabled,version,updated_at`,
			wantArgs: []any{"echo", int64(100), true, int64(2)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := &runtimeStateTestDatabase{row: runtimeStateTestRow{values: []any{int64(100), true, test.returnedVersion, updatedAt}}}
			repository := &PostgresRuntimeStateRepository{database: database}
			state, err := repository.SetGroupEnabled(context.Background(), "echo", 100, true, test.expectedVersion)
			if err != nil {
				t.Fatal(err)
			}
			if normalizeRuntimeStateSQL(database.rowSQL) != normalizeRuntimeStateSQL(test.wantSQL) || !reflect.DeepEqual(database.rowArgs, test.wantArgs) {
				t.Fatalf("QueryRow() sql=%q args=%#v", database.rowSQL, database.rowArgs)
			}
			if state.GroupID != 100 || !state.Enabled || state.Version != test.returnedVersion || state.UpdatedAt.Location() != time.UTC {
				t.Fatalf("state = %+v", state)
			}
		})
	}
}

func TestRuntimeStateRepositoryWriteFailuresAndValidation(t *testing.T) {
	databaseFailure := errors.New("database failed")
	tests := []struct {
		name string
		row  pgx.Row
		want error
	}{
		{name: "conflict", row: runtimeStateTestRow{err: pgx.ErrNoRows}, want: ErrRuntimeStateConflict},
		{name: "database", row: runtimeStateTestRow{err: databaseFailure}, want: databaseFailure},
	}
	for _, test := range tests {
		t.Run("global "+test.name, func(t *testing.T) {
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{row: test.row}}
			if _, err := repository.UpdateDesiredEnabled(context.Background(), "echo", true, 1); !errors.Is(err, test.want) {
				t.Fatalf("UpdateDesiredEnabled() error = %v", err)
			}
		})
		t.Run("group "+test.name, func(t *testing.T) {
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{row: test.row}}
			for _, expectedVersion := range []int64{0, 1} {
				if _, err := repository.SetGroupEnabled(context.Background(), "echo", 100, true, expectedVersion); !errors.Is(err, test.want) {
					t.Fatalf("SetGroupEnabled(version=%d) error = %v", expectedVersion, err)
				}
			}
		})
	}
	foreignKeyError := &pgconn.PgError{Code: "23503", ConstraintName: "plugin_group_states_plugin_key_fkey"}
	repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{row: runtimeStateTestRow{err: foreignKeyError}}}
	if _, err := repository.SetGroupEnabled(context.Background(), "missing", 100, true, 0); !errors.Is(err, ErrRuntimeStateConflict) {
		t.Fatalf("foreign key error = %v", err)
	}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{}}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), "bad-key", true, 1); err == nil {
		t.Fatal("invalid global plugin key accepted")
	}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), "echo", true, 0); !errors.Is(err, ErrRuntimeStateInvalidVersion) {
		t.Fatalf("invalid global version error = %v", err)
	}
	if _, err := repository.SetGroupEnabled(context.Background(), "bad-key", 100, true, 0); err == nil {
		t.Fatal("invalid group plugin key accepted")
	}
	if _, err := repository.SetGroupEnabled(context.Background(), "echo", 0, true, 0); !errors.Is(err, ErrInvalidRuntimeGroupID) {
		t.Fatalf("invalid group error = %v", err)
	}
	if _, err := repository.SetGroupEnabled(context.Background(), "echo", 100, true, -1); !errors.Is(err, ErrRuntimeStateInvalidVersion) {
		t.Fatalf("invalid group version error = %v", err)
	}
}

func normalizeRuntimeStateSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
