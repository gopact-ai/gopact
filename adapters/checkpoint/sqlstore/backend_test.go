package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsRecordsWithDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	backend, err := NewBackend(db)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewRowStore[string](
		backend,
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}

	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		ThreadID:  "thread-1",
		Step:      1,
		Node:      "first",
		Phase:     gopact.StepCompleted,
		State:     "one",
		Queue:     []string{"second"},
		CreatedAt: time.Unix(1, 0).UTC(),
		Metadata:  map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-2",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		ThreadID:  "thread-1",
		Step:      2,
		Node:      "second",
		Phase:     gopact.StepInterrupted,
		State:     "two",
		Pending:   &gopact.InterruptRecord{ID: "interrupt-1", Reason: "approval"},
		Effects:   []gopact.EffectRecord{{ID: "effect-1", Type: "tool_call", ReplayPolicy: gopact.EffectReplayRecordOnly}},
		CreatedAt: time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}

	restoredBackend, err := NewBackend(db)
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restored, err := checkpoint.NewRowStore[string](
		restoredBackend,
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewRowStore(restored) error = %v", err)
	}
	latest, ok, err := restored.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.ID != "checkpoint-2" || latest.Step != 2 || latest.State != "two" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 step 2 state two", latest)
	}
	if latest.Pending == nil || latest.Pending.ID != "interrupt-1" {
		t.Fatalf("Latest().Pending = %+v, want interrupt-1", latest.Pending)
	}
	if len(latest.Effects) != 1 || latest.Effects[0].ID != "effect-1" {
		t.Fatalf("Latest().Effects = %+v, want effect-1", latest.Effects)
	}

	first, ok, err := restored.Get(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if first.Node != "first" || first.State != "one" || first.Metadata["source"] != "test" {
		t.Fatalf("Get() = %+v, want first checkpoint with metadata", first)
	}

	list, err := restored.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() count = %d, want 2", len(list))
	}
	if list[0].ID != "checkpoint-1" || list[1].ID != "checkpoint-2" {
		t.Fatalf("List() ids = %q, %q; want created-time order", list[0].ID, list[1].ID)
	}
}

func TestBackendReplacesExistingRecord(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	backend, err := NewBackend(db)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewRowStore[string](backend)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}

	for _, state := range []string{"old", "new"} {
		err = store.Put(ctx, graph.Checkpoint[string]{
			ID:        "checkpoint-1",
			ThreadID:  "thread-1",
			Step:      1,
			Node:      "node",
			State:     state,
			CreatedAt: time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("Put(%s) error = %v", state, err)
		}
	}

	list, err := store.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() count = %d, want 1", len(list))
	}
	if list[0].State != "new" {
		t.Fatalf("List()[0].State = %q, want new", list[0].State)
	}
}

func TestBackendReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(openTestDB(t))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	record, ok, err := backend.GetRecord(ctx, "missing")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if ok || record.ID != "" {
		t.Fatalf("GetRecord() = %+v, %v; want zero false", record, ok)
	}
}

func TestNewBackendRejectsNilDB(t *testing.T) {
	backend, err := NewBackend(nil)
	if !errors.Is(err, ErrDBRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrDBRequired", backend, err)
	}
}

func TestDefaultQueriesRejectsUnsafeTableName(t *testing.T) {
	queries, err := DefaultQueries("gopact_checkpoints; drop table users", DialectSQLite)
	if err == nil || queries != (Queries{}) {
		t.Fatalf("DefaultQueries(unsafe) queries=%+v err=%v, want error", queries, err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	registerCheckpointSQLTestDriver()
	db, err := sql.Open(checkpointSQLTestDriverName, fmt.Sprintf("db-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error = %v", err)
		}
	})
	return db
}

const checkpointSQLTestDriverName = "gopact_checkpoint_sqlstore_test"

var registerCheckpointSQLTestDriverOnce sync.Once
var checkpointSQLStores sync.Map

func registerCheckpointSQLTestDriver() {
	registerCheckpointSQLTestDriverOnce.Do(func() {
		sql.Register(checkpointSQLTestDriverName, checkpointSQLDriver{})
	})
}

type checkpointSQLDriver struct{}

func (checkpointSQLDriver) Open(name string) (driver.Conn, error) {
	value, _ := checkpointSQLStores.LoadOrStore(name, &checkpointSQLStore{
		records: make(map[string]checkpointSQLRecord),
	})
	return &checkpointSQLConn{store: value.(*checkpointSQLStore)}, nil
}

type checkpointSQLConn struct {
	store *checkpointSQLStore
}

func (c *checkpointSQLConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not implemented")
}

func (c *checkpointSQLConn) Close() error {
	return nil
}

func (c *checkpointSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c *checkpointSQLConn) ExecContext(ctx context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 16 {
		return nil, fmt.Errorf("got %d args, want 16", len(args))
	}
	record := checkpointSQLRecord{
		ID:            stringArg(args[0]),
		SchemaVersion: stringArg(args[1]),
		IDsJSON:       bytesArg(args[2]),
		ThreadID:      stringArg(args[3]),
		Step:          intArg(args[4]),
		Node:          stringArg(args[5]),
		Phase:         stringArg(args[6]),
		State:         bytesArg(args[7]),
		StateCodec:    stringArg(args[8]),
		StateHash:     stringArg(args[9]),
		QueueJSON:     bytesArg(args[10]),
		PendingJSON:   bytesArg(args[11]),
		EffectsJSON:   bytesArg(args[12]),
		ConfigVersion: stringArg(args[13]),
		CreatedAt:     timeArg(args[14]),
		MetadataJSON:  bytesArg(args[15]),
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.records[record.ID] = record
	return driver.RowsAffected(1), nil
}

func (c *checkpointSQLConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("got %d args, want 1", len(args))
	}
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	var records []checkpointSQLRecord
	switch {
	case strings.Contains(query, "WHERE id ="):
		if record, ok := c.store.records[stringArg(args[0])]; ok {
			records = append(records, record)
		}
	case strings.Contains(query, "WHERE thread_id ="):
		threadID := stringArg(args[0])
		for _, record := range c.store.records {
			if record.ThreadID == threadID {
				records = append(records, record)
			}
		}
		sort.Slice(records, func(i, j int) bool {
			if !records[i].CreatedAt.Equal(records[j].CreatedAt) {
				return records[i].CreatedAt.Before(records[j].CreatedAt)
			}
			if records[i].Step != records[j].Step {
				return records[i].Step < records[j].Step
			}
			return records[i].ID < records[j].ID
		})
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	return &checkpointSQLRows{records: records}, nil
}

type checkpointSQLStore struct {
	mu      sync.RWMutex
	records map[string]checkpointSQLRecord
}

type checkpointSQLRecord struct {
	ID            string
	SchemaVersion string
	IDsJSON       []byte
	ThreadID      string
	Step          int
	Node          string
	Phase         string
	State         []byte
	StateCodec    string
	StateHash     string
	QueueJSON     []byte
	PendingJSON   []byte
	EffectsJSON   []byte
	ConfigVersion string
	CreatedAt     time.Time
	MetadataJSON  []byte
}

type checkpointSQLRows struct {
	records []checkpointSQLRecord
	index   int
}

func (r *checkpointSQLRows) Columns() []string {
	return []string{
		"id",
		"schema_version",
		"ids_json",
		"thread_id",
		"step",
		"node",
		"phase",
		"state",
		"state_codec",
		"state_hash",
		"queue_json",
		"pending_json",
		"effects_json",
		"config_version",
		"created_at",
		"metadata_json",
	}
}

func (r *checkpointSQLRows) Close() error {
	return nil
}

func (r *checkpointSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.records) {
		return io.EOF
	}
	record := r.records[r.index]
	r.index++
	values := []driver.Value{
		record.ID,
		record.SchemaVersion,
		record.IDsJSON,
		record.ThreadID,
		int64(record.Step),
		record.Node,
		record.Phase,
		record.State,
		record.StateCodec,
		record.StateHash,
		record.QueueJSON,
		record.PendingJSON,
		record.EffectsJSON,
		record.ConfigVersion,
		record.CreatedAt,
		record.MetadataJSON,
	}
	copy(dest, values)
	return nil
}

func stringArg(arg driver.NamedValue) string {
	if arg.Value == nil {
		return ""
	}
	return arg.Value.(string)
}

func bytesArg(arg driver.NamedValue) []byte {
	if arg.Value == nil {
		return nil
	}
	return append([]byte(nil), arg.Value.([]byte)...)
}

func intArg(arg driver.NamedValue) int {
	switch value := arg.Value.(type) {
	case int64:
		return int(value)
	case int:
		return value
	default:
		panic(fmt.Sprintf("unexpected int arg type %T", arg.Value))
	}
}

func timeArg(arg driver.NamedValue) time.Time {
	switch value := arg.Value.(type) {
	case time.Time:
		return value
	case string:
		if value == "" {
			return time.Time{}
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			panic(err)
		}
		return parsed
	default:
		panic(fmt.Sprintf("unexpected time arg type %T", arg.Value))
	}
}
