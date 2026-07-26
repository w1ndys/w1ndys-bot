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

	"github.com/w1ndys/w1ndys-bot/internal/management"
)

type runtimeStateTestDatabase struct {
	execSQL   string
	execArgs  []any
	execErr   error
	execCalls int
	querySQL  string
	queryArgs []any
	queryErr  error
	rows      pgx.Rows
	rowSQL    string
	rowArgs   []any
	row       pgx.Row
	tx        *runtimeStateTestTx
	beginErr  error
}

// runtimeStateTestTx 按调用顺序回放事务内的 QueryRow 结果并记录审计写入。
type runtimeStateTestTx struct {
	rows       []pgx.Row
	rowSQL     []string
	rowArgs    [][]any
	execSQL    string
	execArgs   []any
	execErr    error
	commitErr  error
	committed  bool
	rolledBack bool
}

func (d *runtimeStateTestDatabase) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	if d.tx == nil {
		d.tx = &runtimeStateTestTx{}
	}
	return d.tx, nil
}

func (t *runtimeStateTestTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.rowSQL = append(t.rowSQL, sql)
	t.rowArgs = append(t.rowArgs, args)
	if len(t.rows) == 0 {
		return runtimeStateTestRow{err: errors.New("unexpected QueryRow")}
	}
	row := t.rows[0]
	t.rows = t.rows[1:]
	return row
}

func (t *runtimeStateTestTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execSQL, t.execArgs = sql, args
	return pgconn.NewCommandTag("INSERT 0 1"), t.execErr
}

func (t *runtimeStateTestTx) Commit(context.Context) error {
	if t.commitErr != nil {
		return t.commitErr
	}
	t.committed = true
	return nil
}

func (t *runtimeStateTestTx) Rollback(context.Context) error {
	t.rolledBack = true
	return nil
}

func (t *runtimeStateTestTx) Begin(context.Context) (pgx.Tx, error) { return t, nil }
func (t *runtimeStateTestTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}
func (t *runtimeStateTestTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unused")
}
func (t *runtimeStateTestTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *runtimeStateTestTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (t *runtimeStateTestTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unused")
}
func (t *runtimeStateTestTx) Conn() *pgx.Conn { return nil }

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

func (d *runtimeStateTestDatabase) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.querySQL = sql
	d.queryArgs = args
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

var runtimeStateTestActor = management.Actor{ID: "10001", Role: "super_admin", Channel: management.ChannelWebUI, RequestID: "req-1"}

func TestRuntimeStateRepositoryFindsSinglePluginState(t *testing.T) {
	groupTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	rows := &runtimeStateTestRows{values: [][]any{
		{"monitor", true, int64(3), groupTime, int64(200), false, int64(4), groupTime},
		{"monitor", true, int64(3), groupTime, int64(100), true, int64(2), groupTime},
	}}
	database := &runtimeStateTestDatabase{rows: rows}
	repository := &PostgresRuntimeStateRepository{database: database}
	state, err := repository.FindState(context.Background(), "monitor")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalizeRuntimeStateSQL(database.querySQL), "WHERE s.plugin_key=$1 ORDER BY g.group_id") {
		t.Fatalf("Query() sql = %q", database.querySQL)
	}
	if !reflect.DeepEqual(database.queryArgs, []any{"monitor"}) {
		t.Fatalf("Query() args = %#v", database.queryArgs)
	}
	if state.PluginKey != "monitor" || state.Version != 3 || len(state.Groups) != 2 || state.UpdatedAt.Location() != time.UTC {
		t.Fatalf("state = %+v", state)
	}
}

func TestRuntimeStateRepositoryFindStateRejectsUnknownAndInvalidKeys(t *testing.T) {
	repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{rows: &runtimeStateTestRows{}}}
	if _, err := repository.FindState(context.Background(), "missing"); !errors.Is(err, ErrRuntimeStateNotFound) {
		t.Fatalf("FindState(missing) error = %v", err)
	}
	if _, err := repository.FindState(context.Background(), "bad-key"); err == nil {
		t.Fatal("invalid plugin key accepted")
	}
}

func TestRuntimeStateRepositoryUpdatesDesiredEnabledWithCASAndAudit(t *testing.T) {
	zone := time.FixedZone("test", 8*60*60)
	updatedAt := time.Date(2026, 7, 25, 20, 0, 0, 0, zone)
	transaction := &runtimeStateTestTx{rows: []pgx.Row{
		runtimeStateTestRow{values: []any{false, int64(2)}},
		runtimeStateTestRow{values: []any{"echo", true, int64(3), updatedAt}},
	}}
	repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
	state, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(transaction.rowSQL) != 2 || !strings.Contains(transaction.rowSQL[0], "FOR UPDATE") {
		t.Fatalf("transaction QueryRow sql = %#v", transaction.rowSQL)
	}
	wantSQL := `UPDATE plugin_states
SET desired_enabled=$2,version=version+1,updated_at=NOW()
WHERE plugin_key=$1 AND version=$3
RETURNING plugin_key,desired_enabled,version,updated_at`
	if normalizeRuntimeStateSQL(transaction.rowSQL[1]) != normalizeRuntimeStateSQL(wantSQL) || !reflect.DeepEqual(transaction.rowArgs[1], []any{"echo", true, int64(2)}) {
		t.Fatalf("QueryRow() sql=%q args=%#v", transaction.rowSQL[1], transaction.rowArgs[1])
	}
	if !strings.Contains(transaction.execSQL, "admin_audit_logs") {
		t.Fatalf("audit sql = %q", transaction.execSQL)
	}
	wantAudit := []any{"10001", "super_admin", management.ChannelWebUI, "plugin.runtime.global.update", "plugin_runtime_state", "echo"}
	if !reflect.DeepEqual(transaction.execArgs[:6], wantAudit) || transaction.execArgs[8] != "req-1" {
		t.Fatalf("audit args = %#v", transaction.execArgs)
	}
	if before, ok := transaction.execArgs[6].([]byte); !ok || !strings.Contains(string(before), `"version":2`) {
		t.Fatalf("audit before = %#v", transaction.execArgs[6])
	}
	if after, ok := transaction.execArgs[7].([]byte); !ok || !strings.Contains(string(after), `"version":3`) {
		t.Fatalf("audit after = %#v", transaction.execArgs[7])
	}
	if !transaction.committed {
		t.Fatal("transaction not committed")
	}
	if state.PluginKey != "echo" || !state.DesiredEnabled || state.Version != 3 || state.UpdatedAt.Location() != time.UTC || len(state.Groups) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

func TestRuntimeStateRepositoryGlobalWriteRejectsStaleAndMissingRows(t *testing.T) {
	tests := []struct {
		name string
		lock pgx.Row
		want error
	}{
		{name: "stale", lock: runtimeStateTestRow{values: []any{false, int64(5)}}, want: ErrRuntimeStateConflict},
		{name: "missing", lock: runtimeStateTestRow{err: pgx.ErrNoRows}, want: ErrRuntimeStateNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &runtimeStateTestTx{rows: []pgx.Row{test.lock}}
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
			if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 2); !errors.Is(err, test.want) {
				t.Fatalf("UpdateDesiredEnabled() error = %v", err)
			}
			if transaction.execSQL != "" || transaction.committed || !transaction.rolledBack {
				t.Fatalf("transaction wrote audit or committed: %+v", transaction)
			}
		})
	}
}

func TestRuntimeStateRepositorySetsGroupWithInsertOrCASUpdate(t *testing.T) {
	updatedAt := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		expectedVersion int64
		lockRows        []pgx.Row
		wantSQL         string
		wantArgs        []any
		returnedVersion int64
		wantBefore      bool
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
			name: "update", expectedVersion: 2, returnedVersion: 3, wantBefore: true,
			lockRows: []pgx.Row{runtimeStateTestRow{values: []any{false, int64(2)}}},
			wantSQL: `UPDATE plugin_group_states
SET enabled=$3,version=version+1,updated_at=NOW()
WHERE plugin_key=$1 AND group_id=$2 AND version=$4
RETURNING group_id,enabled,version,updated_at`,
			wantArgs: []any{"echo", int64(100), true, int64(2)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := append(append([]pgx.Row{}, test.lockRows...), runtimeStateTestRow{values: []any{int64(100), true, test.returnedVersion, updatedAt}})
			transaction := &runtimeStateTestTx{rows: rows}
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
			state, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "echo", 100, true, test.expectedVersion)
			if err != nil {
				t.Fatal(err)
			}
			writeIndex := len(test.lockRows)
			if normalizeRuntimeStateSQL(transaction.rowSQL[writeIndex]) != normalizeRuntimeStateSQL(test.wantSQL) || !reflect.DeepEqual(transaction.rowArgs[writeIndex], test.wantArgs) {
				t.Fatalf("QueryRow() sql=%q args=%#v", transaction.rowSQL[writeIndex], transaction.rowArgs[writeIndex])
			}
			wantAudit := []any{"plugin.runtime.group.update", "plugin_runtime_group_state", "echo:100"}
			if !reflect.DeepEqual(transaction.execArgs[3:6], wantAudit) || !transaction.committed {
				t.Fatalf("audit args = %#v committed=%v", transaction.execArgs, transaction.committed)
			}
			// 新增记录没有可信旧值，审计 before 必须为空。
			before, _ := transaction.execArgs[6].([]byte)
			if (len(before) > 0) != test.wantBefore {
				t.Fatalf("audit before = %q", before)
			}
			if state.GroupID != 100 || !state.Enabled || state.Version != test.returnedVersion || state.UpdatedAt.Location() != time.UTC {
				t.Fatalf("state = %+v", state)
			}
		})
	}
}

func TestRuntimeStateRepositoryGroupWriteRejectsStaleAndMissingRows(t *testing.T) {
	tests := []struct {
		name string
		lock pgx.Row
		want error
	}{
		{name: "stale", lock: runtimeStateTestRow{values: []any{false, int64(5)}}, want: ErrRuntimeStateConflict},
		{name: "missing", lock: runtimeStateTestRow{err: pgx.ErrNoRows}, want: ErrRuntimeStateConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &runtimeStateTestTx{rows: []pgx.Row{test.lock}}
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
			if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "echo", 100, true, 2); !errors.Is(err, test.want) {
				t.Fatalf("SetGroupEnabled() error = %v", err)
			}
			if transaction.execSQL != "" || transaction.committed {
				t.Fatalf("transaction wrote audit or committed: %+v", transaction)
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
			transaction := &runtimeStateTestTx{rows: []pgx.Row{runtimeStateTestRow{values: []any{false, int64(1)}}, test.row}}
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
			if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 1); !errors.Is(err, test.want) {
				t.Fatalf("UpdateDesiredEnabled() error = %v", err)
			}
			if transaction.committed || !transaction.rolledBack {
				t.Fatalf("transaction committed=%v rolledBack=%v", transaction.committed, transaction.rolledBack)
			}
		})
		t.Run("group "+test.name, func(t *testing.T) {
			transaction := &runtimeStateTestTx{rows: []pgx.Row{test.row}}
			repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
			if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "echo", 100, true, 0); !errors.Is(err, test.want) {
				t.Fatalf("SetGroupEnabled() error = %v", err)
			}
			if transaction.committed || !transaction.rolledBack {
				t.Fatalf("transaction committed=%v rolledBack=%v", transaction.committed, transaction.rolledBack)
			}
		})
	}

	// 审计写入失败必须回滚状态变更。
	auditFailure := errors.New("audit failed")
	transaction := &runtimeStateTestTx{
		rows:    []pgx.Row{runtimeStateTestRow{values: []any{false, int64(1)}}, runtimeStateTestRow{values: []any{"echo", true, int64(2), time.Now().UTC()}}},
		execErr: auditFailure,
	}
	repository := &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 1); !errors.Is(err, auditFailure) {
		t.Fatalf("audit failure error = %v", err)
	}
	if transaction.committed || !transaction.rolledBack {
		t.Fatal("audit failure did not roll back")
	}

	commitFailure := errors.New("commit failed")
	transaction = &runtimeStateTestTx{
		rows:      []pgx.Row{runtimeStateTestRow{values: []any{false, int64(1)}}, runtimeStateTestRow{values: []any{"echo", true, int64(2), time.Now().UTC()}}},
		commitErr: commitFailure,
	}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 1); !errors.Is(err, commitFailure) {
		t.Fatalf("commit failure error = %v", err)
	}

	beginFailure := errors.New("begin failed")
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{beginErr: beginFailure}}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 1); !errors.Is(err, beginFailure) {
		t.Fatalf("begin failure error = %v", err)
	}
	if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "echo", 100, true, 0); !errors.Is(err, beginFailure) {
		t.Fatalf("group begin failure error = %v", err)
	}

	// 外键失败表示插件全局状态行缺失，而不是版本冲突。
	foreignKeyError := &pgconn.PgError{Code: "23503", ConstraintName: "plugin_group_states_plugin_key_fkey"}
	transaction = &runtimeStateTestTx{rows: []pgx.Row{runtimeStateTestRow{err: foreignKeyError}}}
	repository = &PostgresRuntimeStateRepository{database: &runtimeStateTestDatabase{tx: transaction}}
	if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "missing", 100, true, 0); !errors.Is(err, ErrRuntimeStateNotFound) {
		t.Fatalf("foreign key error = %v", err)
	}

	// 参数校验必须在开启事务之前拒绝。
	database := &runtimeStateTestDatabase{}
	repository = &PostgresRuntimeStateRepository{database: database}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "bad-key", true, 1); err == nil {
		t.Fatal("invalid global plugin key accepted")
	}
	if _, err := repository.UpdateDesiredEnabled(context.Background(), runtimeStateTestActor, "echo", true, 0); !errors.Is(err, ErrRuntimeStateInvalidVersion) {
		t.Fatalf("invalid global version error = %v", err)
	}
	if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "bad-key", 100, true, 0); err == nil {
		t.Fatal("invalid group plugin key accepted")
	}
	if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "echo", 0, true, 0); !errors.Is(err, ErrInvalidRuntimeGroupID) {
		t.Fatalf("invalid group error = %v", err)
	}
	if _, err := repository.SetGroupEnabled(context.Background(), runtimeStateTestActor, "echo", 100, true, -1); !errors.Is(err, ErrRuntimeStateInvalidVersion) {
		t.Fatalf("invalid group version error = %v", err)
	}
	if database.tx != nil {
		t.Fatal("validation failure started a transaction")
	}
}

func normalizeRuntimeStateSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
