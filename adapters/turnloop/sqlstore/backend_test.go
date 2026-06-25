package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBackendPersistsTurnLoopStateWithDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	backend, err := NewBackend(db)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := gopact.NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}
	state := gopact.TurnLoopState{
		Pending: []gopact.TurnInputRecord{
			{
				ID:    "turn-input:1",
				Kind:  gopact.TurnInputUser,
				Input: "queued",
				IDs:   gopact.RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
			},
		},
		PendingEvents: []gopact.Event{
			{Type: gopact.EventTurnInputReceived, IDs: gopact.RuntimeIDs{ThreadID: "thread-1"}},
		},
		Interrupted: &gopact.TurnInputRecord{
			ID:    "turn-input:2",
			Kind:  gopact.TurnInputUser,
			Input: "question",
		},
		InputSeq:  2,
		UpdatedAt: time.Unix(10, 0).UTC(),
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	restoredBackend, err := NewBackend(db)
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restoredStore, err := gopact.NewRowTurnLoopStore(restoredBackend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restoredStore.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if got.InputSeq != 2 || len(got.Pending) != 1 || got.Pending[0].Input != "queued" {
		t.Fatalf("Load() = %+v, want queued pending state", got)
	}
	if len(got.PendingEvents) != 1 || got.PendingEvents[0].Type != gopact.EventTurnInputReceived {
		t.Fatalf("Load().PendingEvents = %+v, want turn_input_received", got.PendingEvents)
	}
	if got.Interrupted == nil || got.Interrupted.Input != "question" {
		t.Fatalf("Load().Interrupted = %+v, want question", got.Interrupted)
	}
}

func TestBackendReplacesStateForSameKey(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(openTestDB(t))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := gopact.NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}

	if err := store.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := store.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	got, ok, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("Load() ok=%v state=%+v, want latest state", ok, got)
	}
}

func TestBackendReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(openTestDB(t))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	record, ok, err := backend.GetTurnLoopState(ctx, "missing")
	if err != nil {
		t.Fatalf("GetTurnLoopState() error = %v", err)
	}
	if ok || record.Key != "" {
		t.Fatalf("GetTurnLoopState() = %+v, %v; want zero false", record, ok)
	}
}

func TestNewBackendRejectsNilDB(t *testing.T) {
	backend, err := NewBackend(nil)
	if !errors.Is(err, ErrDBRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrDBRequired", backend, err)
	}
}

func TestDefaultQueriesRejectsUnsafeTableName(t *testing.T) {
	queries, err := DefaultQueries("gopact_turnloop_states; drop table users", DialectSQLite)
	if err == nil || queries != (Queries{}) {
		t.Fatalf("DefaultQueries(unsafe) queries=%+v err=%v, want error", queries, err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	registerTurnLoopSQLTestDriver()
	db, err := sql.Open(turnLoopSQLTestDriverName, fmt.Sprintf("db-%d", time.Now().UnixNano()))
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

const turnLoopSQLTestDriverName = "gopact_turnloop_sqlstore_test"

var registerTurnLoopSQLTestDriverOnce sync.Once
var turnLoopSQLStores sync.Map

func registerTurnLoopSQLTestDriver() {
	registerTurnLoopSQLTestDriverOnce.Do(func() {
		sql.Register(turnLoopSQLTestDriverName, turnLoopSQLDriver{})
	})
}

type turnLoopSQLDriver struct{}

func (turnLoopSQLDriver) Open(name string) (driver.Conn, error) {
	value, _ := turnLoopSQLStores.LoadOrStore(name, &turnLoopSQLStore{
		records: make(map[string]turnLoopSQLRecord),
	})
	return &turnLoopSQLConn{store: value.(*turnLoopSQLStore)}, nil
}

type turnLoopSQLConn struct {
	store *turnLoopSQLStore
}

func (c *turnLoopSQLConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not implemented")
}

func (c *turnLoopSQLConn) Close() error {
	return nil
}

func (c *turnLoopSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c *turnLoopSQLConn) ExecContext(ctx context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch len(args) {
	case 5:
		return c.execUpsert(args)
	case 6:
		return c.execVersionedInsert(args)
	case 7:
		return c.execVersionedUpdate(args)
	default:
		return nil, fmt.Errorf("got %d args, want 5, 6, or 7", len(args))
	}
}

func (c *turnLoopSQLConn) execUpsert(args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 5 {
		return nil, fmt.Errorf("got %d args, want 5", len(args))
	}
	record := turnLoopSQLRecord{
		Key:           stringArg(args[0]),
		SchemaVersion: stringArg(args[1]),
		StateJSON:     bytesArg(args[2]),
		UpdatedAt:     timeArg(args[3]),
		MetadataJSON:  bytesArg(args[4]),
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.records[record.Key] = record
	return driver.RowsAffected(1), nil
}

func (c *turnLoopSQLConn) execVersionedInsert(args []driver.NamedValue) (driver.Result, error) {
	record := turnLoopSQLRecord{
		Key:           stringArg(args[0]),
		Version:       stringArg(args[1]),
		SchemaVersion: stringArg(args[2]),
		StateJSON:     bytesArg(args[3]),
		UpdatedAt:     timeArg(args[4]),
		MetadataJSON:  bytesArg(args[5]),
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if _, ok := c.store.records[record.Key]; ok {
		return driver.RowsAffected(0), nil
	}
	c.store.records[record.Key] = record
	return driver.RowsAffected(1), nil
}

func (c *turnLoopSQLConn) execVersionedUpdate(args []driver.NamedValue) (driver.Result, error) {
	key := stringArg(args[5])
	expectedVersion := stringArg(args[6])

	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	current, ok := c.store.records[key]
	if !ok || current.Version != expectedVersion {
		return driver.RowsAffected(0), nil
	}
	c.store.records[key] = turnLoopSQLRecord{
		Key:           key,
		Version:       stringArg(args[0]),
		SchemaVersion: stringArg(args[1]),
		StateJSON:     bytesArg(args[2]),
		UpdatedAt:     timeArg(args[3]),
		MetadataJSON:  bytesArg(args[4]),
	}
	return driver.RowsAffected(1), nil
}

func (c *turnLoopSQLConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("got %d args, want 1", len(args))
	}
	if !strings.Contains(query, "WHERE state_key =") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	var records []turnLoopSQLRecord
	if record, ok := c.store.records[stringArg(args[0])]; ok {
		records = append(records, record)
	}
	return &turnLoopSQLRows{
		records:   records,
		versioned: strings.Contains(query, "state_version"),
	}, nil
}

type turnLoopSQLStore struct {
	mu      sync.RWMutex
	records map[string]turnLoopSQLRecord
}

type turnLoopSQLRecord struct {
	Key           string
	Version       string
	SchemaVersion string
	StateJSON     []byte
	UpdatedAt     time.Time
	MetadataJSON  []byte
}

type turnLoopSQLRows struct {
	records   []turnLoopSQLRecord
	index     int
	versioned bool
}

func (r *turnLoopSQLRows) Columns() []string {
	if r.versioned {
		return []string{
			"state_key",
			"state_version",
			"schema_version",
			"state_json",
			"updated_at",
			"metadata_json",
		}
	}
	return []string{
		"state_key",
		"schema_version",
		"state_json",
		"updated_at",
		"metadata_json",
	}
}

func (r *turnLoopSQLRows) Close() error {
	return nil
}

func (r *turnLoopSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.records) {
		return io.EOF
	}
	record := r.records[r.index]
	r.index++
	if r.versioned {
		values := []driver.Value{
			record.Key,
			record.Version,
			record.SchemaVersion,
			record.StateJSON,
			record.UpdatedAt.Format(time.RFC3339Nano),
			record.MetadataJSON,
		}
		copy(dest, values)
		return nil
	}
	values := []driver.Value{
		record.Key,
		record.SchemaVersion,
		record.StateJSON,
		record.UpdatedAt.Format(time.RFC3339Nano),
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
