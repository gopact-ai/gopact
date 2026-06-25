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

func TestBackendAcquiresRenewsAndReleasesLease(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(500, 0).UTC())
	backend, err := NewBackend(openTestDB(t), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	lease, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:      "turns/main",
		Owner:    "worker-a",
		TTL:      10 * time.Second,
		Metadata: map[string]any{"thread_id": "thread-1"},
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if lease.Owner != "worker-a" || lease.Token == "" || !lease.ExpiresAt.Equal(clock.Now().Add(10*time.Second)) {
		t.Fatalf("AcquireLease() = %+v, want worker-a lease", lease)
	}

	if _, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   lease.Key,
		Owner: "worker-b",
		TTL:   10 * time.Second,
	}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("AcquireLease(conflict) error = %v, want ErrLeaseConflict", err)
	}

	clock.Advance(2 * time.Second)
	renewed, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   20 * time.Second,
	})
	if err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if renewed.Token == lease.Token || !renewed.ExpiresAt.Equal(clock.Now().Add(20*time.Second)) {
		t.Fatalf("RenewLease() = %+v, want rotated token and new ttl", renewed)
	}
	if _, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   20 * time.Second,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(stale token) error = %v, want ErrLeaseNotHeld", err)
	}

	got, ok, err := backend.GetLease(ctx, lease.Key)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if !ok || got.Metadata["thread_id"] != "thread-1" {
		t.Fatalf("GetLease() ok=%v lease=%+v, want current metadata", ok, got)
	}

	if err := backend.ReleaseLease(ctx, gopact.LeaseReleaseRequest{
		Key:   renewed.Key,
		Owner: "worker-b",
		Token: renewed.Token,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("ReleaseLease(wrong owner) error = %v, want ErrLeaseNotHeld", err)
	}
	if err := backend.ReleaseLease(ctx, gopact.LeaseReleaseRequest{
		Key:   renewed.Key,
		Owner: renewed.Owner,
		Token: renewed.Token,
	}); err != nil {
		t.Fatalf("ReleaseLease() error = %v", err)
	}
	if _, ok, err := backend.GetLease(ctx, lease.Key); err != nil || ok {
		t.Fatalf("GetLease(after release) ok=%v err=%v, want no lease", ok, err)
	}
}

func TestBackendTransfersExpiredLease(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(600, 0).UTC())
	backend, err := NewBackend(openTestDB(t), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	first, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease(first) error = %v", err)
	}

	clock.Advance(11 * time.Second)
	second, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease(second) error = %v", err)
	}
	if second.Owner != "worker-b" || second.Token == "" || second.Token == first.Token {
		t.Fatalf("AcquireLease(second) = %+v, want transferred worker-b lease", second)
	}
}

func TestBackendComposesWithLeasedTurnLoopStore(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(700, 0).UTC())
	leases, err := NewBackend(openTestDB(t), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	base := gopact.NewMemoryTurnLoopStore()
	first, err := gopact.NewLeasedTurnLoopStore(base, leases, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(first) error = %v", err)
	}
	second, err := gopact.NewLeasedTurnLoopStore(base, leases, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(second) error = %v", err)
	}

	if err := first.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := second.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("second Save(conflict) error = %v, want ErrLeaseConflict", err)
	}
	clock.Advance(11 * time.Second)
	if err := second.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("second Save(after expiry) error = %v", err)
	}
	if err := first.Save(ctx, gopact.TurnLoopState{InputSeq: 3}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("first Save(stale) error = %v, want ErrLeaseConflict", err)
	}
	got, ok, err := base.Load(ctx)
	if err != nil {
		t.Fatalf("base Load() error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("base Load() ok=%v state=%+v, want worker-b state", ok, got)
	}
}

func TestNewBackendRejectsInvalidInputs(t *testing.T) {
	if backend, err := NewBackend(nil); !errors.Is(err, ErrDBRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrDBRequired", backend, err)
	}
	queries, err := DefaultQueries("gopact_leases; drop table users", DialectSQLite)
	if err == nil || queries != (Queries{}) {
		t.Fatalf("DefaultQueries(unsafe) queries=%+v err=%v, want error", queries, err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	registerLeaseSQLTestDriver()
	db, err := sql.Open(leaseSQLTestDriverName, fmt.Sprintf("db-%d", time.Now().UnixNano()))
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

const leaseSQLTestDriverName = "gopact_lease_sqlstore_test"

var registerLeaseSQLTestDriverOnce sync.Once
var leaseSQLStores sync.Map

func registerLeaseSQLTestDriver() {
	registerLeaseSQLTestDriverOnce.Do(func() {
		sql.Register(leaseSQLTestDriverName, leaseSQLDriver{})
	})
}

type leaseSQLDriver struct{}

func (leaseSQLDriver) Open(name string) (driver.Conn, error) {
	value, _ := leaseSQLStores.LoadOrStore(name, &leaseSQLStore{
		records: make(map[string]leaseSQLRecord),
	})
	return &leaseSQLConn{store: value.(*leaseSQLStore)}, nil
}

type leaseSQLConn struct {
	store *leaseSQLStore
}

func (c *leaseSQLConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not implemented")
}

func (c *leaseSQLConn) Close() error {
	return nil
}

func (c *leaseSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c *leaseSQLConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(query, "INSERT"):
		return c.execInsert(args)
	case strings.HasPrefix(query, "UPDATE") && strings.Contains(query, "expires_at <="):
		return c.execAcquireExpired(args)
	case strings.HasPrefix(query, "UPDATE"):
		return c.execRenew(args)
	case strings.HasPrefix(query, "DELETE"):
		return c.execRelease(args)
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (c *leaseSQLConn) execInsert(args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 6 {
		return nil, fmt.Errorf("insert got %d args, want 6", len(args))
	}
	record := leaseSQLRecordFromArgs(args)
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if _, ok := c.store.records[record.Key]; ok {
		return nil, errors.New("duplicate lease key")
	}
	c.store.records[record.Key] = record
	return driver.RowsAffected(1), nil
}

func (c *leaseSQLConn) execAcquireExpired(args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 7 {
		return nil, fmt.Errorf("acquire expired got %d args, want 7", len(args))
	}
	record := leaseSQLRecord{
		Owner:        stringArg(args[0]),
		Token:        stringArg(args[1]),
		AcquiredAt:   timeArg(args[2]),
		ExpiresAt:    timeArg(args[3]),
		MetadataJSON: bytesArg(args[4]),
		Key:          stringArg(args[5]),
	}
	now := timeArg(args[6])
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	current, ok := c.store.records[record.Key]
	if !ok || now.Before(current.ExpiresAt) {
		return driver.RowsAffected(0), nil
	}
	c.store.records[record.Key] = record
	return driver.RowsAffected(1), nil
}

func (c *leaseSQLConn) execRenew(args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 6 {
		return nil, fmt.Errorf("renew got %d args, want 6", len(args))
	}
	key := stringArg(args[2])
	owner := stringArg(args[3])
	token := stringArg(args[4])
	now := timeArg(args[5])
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	current, ok := c.store.records[key]
	if !ok || current.Owner != owner || current.Token != token || !now.Before(current.ExpiresAt) {
		return driver.RowsAffected(0), nil
	}
	current.Token = stringArg(args[0])
	current.ExpiresAt = timeArg(args[1])
	c.store.records[key] = current
	return driver.RowsAffected(1), nil
}

func (c *leaseSQLConn) execRelease(args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 4 {
		return nil, fmt.Errorf("release got %d args, want 4", len(args))
	}
	key := stringArg(args[0])
	owner := stringArg(args[1])
	token := stringArg(args[2])
	now := timeArg(args[3])
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	current, ok := c.store.records[key]
	if !ok || current.Owner != owner || current.Token != token || !now.Before(current.ExpiresAt) {
		return driver.RowsAffected(0), nil
	}
	delete(c.store.records, key)
	return driver.RowsAffected(1), nil
}

func (c *leaseSQLConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("query got %d args, want 2", len(args))
	}
	if !strings.Contains(query, "WHERE lease_key =") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	key := stringArg(args[0])
	now := timeArg(args[1])
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	var records []leaseSQLRecord
	if record, ok := c.store.records[key]; ok && now.Before(record.ExpiresAt) {
		records = append(records, record)
	}
	return &leaseSQLRows{records: records}, nil
}

type leaseSQLStore struct {
	mu      sync.RWMutex
	records map[string]leaseSQLRecord
}

type leaseSQLRecord struct {
	Key          string
	Owner        string
	Token        string
	AcquiredAt   time.Time
	ExpiresAt    time.Time
	MetadataJSON []byte
}

func leaseSQLRecordFromArgs(args []driver.NamedValue) leaseSQLRecord {
	return leaseSQLRecord{
		Key:          stringArg(args[0]),
		Owner:        stringArg(args[1]),
		Token:        stringArg(args[2]),
		AcquiredAt:   timeArg(args[3]),
		ExpiresAt:    timeArg(args[4]),
		MetadataJSON: bytesArg(args[5]),
	}
}

type leaseSQLRows struct {
	records []leaseSQLRecord
	index   int
}

func (r *leaseSQLRows) Columns() []string {
	return []string{"lease_key", "owner", "token", "acquired_at", "expires_at", "metadata_json"}
}

func (r *leaseSQLRows) Close() error {
	return nil
}

func (r *leaseSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.records) {
		return io.EOF
	}
	record := r.records[r.index]
	r.index++
	values := []driver.Value{
		record.Key,
		record.Owner,
		record.Token,
		record.AcquiredAt.Format(time.RFC3339Nano),
		record.ExpiresAt.Format(time.RFC3339Nano),
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

type fakeClock struct {
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}
