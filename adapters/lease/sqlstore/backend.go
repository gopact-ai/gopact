// Package sqlstore adapts database/sql-compatible stores to gopact.LeaseBackend.
package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// DefaultTable is the default table name used by NewBackend.
	DefaultTable = "gopact_leases"
)

// Dialect selects placeholder syntax for generated queries.
type Dialect string

const (
	// Lease SQL dialects supported by the query builder.
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

var (
	// ErrDBRequired is returned when a backend is created without a DB handle.
	ErrDBRequired = errors.New("lease sqlstore: db is required")
	// ErrQueriesRequired is returned when required SQL statements are missing.
	ErrQueriesRequired = errors.New("lease sqlstore: queries are required")
	// ErrUnsupportedDialect is returned when a query builder receives an unknown dialect.
	ErrUnsupportedDialect = errors.New("lease sqlstore: unsupported dialect")
	// ErrUnsafeTableName is returned when a generated table name is not safe to interpolate.
	ErrUnsafeTableName = errors.New("lease sqlstore: unsafe table name")
)

// DBTX is implemented by *sql.DB, *sql.Tx, and small wrappers around them.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Queries contains the SQL statements used by Backend.
//
// InsertLease must insert lease_key, owner, token, acquired_at, expires_at,
// metadata_json in that order. AcquireExpiredLease must update owner, token,
// acquired_at, expires_at, metadata_json for lease_key when expires_at <= now.
// RenewLease must update token and expires_at when key, owner, token match and
// expires_at > now. ReleaseLease must delete only the current non-expired
// holder. GetLease must return leaseColumns in the same order.
type Queries struct {
	InsertLease         string
	AcquireExpiredLease string
	RenewLease          string
	ReleaseLease        string
	GetLease            string
}

// Backend persists worker ownership leases through database/sql-compatible calls.
type Backend struct {
	db      DBTX
	queries Queries
	now     func() time.Time
}

var _ gopact.LeaseBackend = (*Backend)(nil)

// Option configures a SQL lease backend.
type Option func(*backendConfig) error

type backendConfig struct {
	queries Queries
	now     func() time.Time
}

// NewBackend creates a lease backend backed by db.
func NewBackend(db DBTX, opts ...Option) (*Backend, error) {
	if db == nil {
		return nil, ErrDBRequired
	}
	queries, err := DefaultQueries(DefaultTable, DialectSQLite)
	if err != nil {
		return nil, err
	}
	cfg := backendConfig{
		queries: queries,
		now:     time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if err := validateQueries(cfg.queries); err != nil {
		return nil, err
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Backend{db: db, queries: cfg.queries, now: cfg.now}, nil
}

// WithQueries replaces the generated SQL with host-owned queries.
func WithQueries(queries Queries) Option {
	return func(cfg *backendConfig) error {
		if err := validateQueries(queries); err != nil {
			return err
		}
		cfg.queries = queries
		return nil
	}
}

// WithTable uses generated queries for table and dialect.
func WithTable(table string, dialect Dialect) Option {
	return func(cfg *backendConfig) error {
		queries, err := DefaultQueries(table, dialect)
		if err != nil {
			return err
		}
		cfg.queries = queries
		return nil
	}
}

// WithClock overrides the clock used for lease expiry checks.
func WithClock(now func() time.Time) Option {
	return func(cfg *backendConfig) error {
		if now != nil {
			cfg.now = now
		}
		return nil
	}
}

// DefaultQueries returns parameterized SQL for a known dialect.
func DefaultQueries(table string, dialect Dialect) (Queries, error) {
	if !safeIdentifier(table) {
		return Queries{}, fmt.Errorf("%w: %q", ErrUnsafeTableName, table)
	}
	insertPlaceholders, err := placeholders(dialect, len(leaseColumns))
	if err != nil {
		return Queries{}, err
	}
	acquirePlaceholders, err := placeholders(dialect, 7)
	if err != nil {
		return Queries{}, err
	}
	renewPlaceholders, err := placeholders(dialect, 6)
	if err != nil {
		return Queries{}, err
	}
	releasePlaceholders, err := placeholders(dialect, 4)
	if err != nil {
		return Queries{}, err
	}
	getPlaceholders, err := placeholders(dialect, 2)
	if err != nil {
		return Queries{}, err
	}
	columns := strings.Join(leaseColumns, ", ")
	return Queries{
		InsertLease: fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			table,
			columns,
			strings.Join(insertPlaceholders, ", "),
		),
		AcquireExpiredLease: fmt.Sprintf(
			"UPDATE %s SET owner = %s, token = %s, acquired_at = %s, expires_at = %s, metadata_json = %s WHERE lease_key = %s AND expires_at <= %s",
			table,
			acquirePlaceholders[0],
			acquirePlaceholders[1],
			acquirePlaceholders[2],
			acquirePlaceholders[3],
			acquirePlaceholders[4],
			acquirePlaceholders[5],
			acquirePlaceholders[6],
		),
		RenewLease: fmt.Sprintf(
			"UPDATE %s SET token = %s, expires_at = %s WHERE lease_key = %s AND owner = %s AND token = %s AND expires_at > %s",
			table,
			renewPlaceholders[0],
			renewPlaceholders[1],
			renewPlaceholders[2],
			renewPlaceholders[3],
			renewPlaceholders[4],
			renewPlaceholders[5],
		),
		ReleaseLease: fmt.Sprintf(
			"DELETE FROM %s WHERE lease_key = %s AND owner = %s AND token = %s AND expires_at > %s",
			table,
			releasePlaceholders[0],
			releasePlaceholders[1],
			releasePlaceholders[2],
			releasePlaceholders[3],
		),
		GetLease: fmt.Sprintf(
			"SELECT %s FROM %s WHERE lease_key = %s AND expires_at > %s",
			columns,
			table,
			getPlaceholders[0],
			getPlaceholders[1],
		),
	}, nil
}

// AcquireLease acquires key for owner unless a non-expired lease is held.
func (b *Backend) AcquireLease(ctx context.Context, request gopact.LeaseRequest) (gopact.LeaseRecord, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.LeaseRecord{}, err
	}
	if err := validateAcquire(request); err != nil {
		return gopact.LeaseRecord{}, err
	}
	now := b.currentTime()
	record, err := b.newLeaseRecord(request, now)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	args, err := encodeLeaseArgs(record)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	insertResult, insertErr := b.db.ExecContext(ctx, b.queries.InsertLease, args...)
	if insertErr == nil {
		affected, err := insertResult.RowsAffected()
		if err != nil {
			return gopact.LeaseRecord{}, fmt.Errorf("lease sqlstore: insert lease rows affected: %w", err)
		}
		if affected > 0 {
			return copyLeaseRecord(record), nil
		}
	}

	acquireArgs, err := encodeAcquireExpiredArgs(record, now)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	result, err := b.db.ExecContext(ctx, b.queries.AcquireExpiredLease, acquireArgs...)
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease sqlstore: acquire expired lease: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease sqlstore: acquire expired rows affected: %w", err)
	}
	if affected == 0 {
		return gopact.LeaseRecord{}, gopact.ErrLeaseConflict
	}
	return copyLeaseRecord(record), nil
}

// RenewLease extends a lease only if owner and token match the current holder.
func (b *Backend) RenewLease(ctx context.Context, request gopact.LeaseRenewRequest) (gopact.LeaseRecord, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.LeaseRecord{}, err
	}
	if err := validateRenew(request); err != nil {
		return gopact.LeaseRecord{}, err
	}
	token, err := newLeaseToken()
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	now := b.currentTime()
	result, err := b.db.ExecContext(ctx, b.queries.RenewLease,
		token,
		formatTime(now.Add(request.TTL)),
		request.Key,
		request.Owner,
		request.Token,
		formatTime(now),
	)
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease sqlstore: renew lease: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease sqlstore: renew rows affected: %w", err)
	}
	if affected == 0 {
		return gopact.LeaseRecord{}, gopact.ErrLeaseNotHeld
	}
	record, ok, err := b.GetLease(ctx, request.Key)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	if !ok {
		return gopact.LeaseRecord{}, gopact.ErrLeaseNotHeld
	}
	return record, nil
}

// ReleaseLease releases a lease only if owner and token match the current holder.
func (b *Backend) ReleaseLease(ctx context.Context, request gopact.LeaseReleaseRequest) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRelease(request); err != nil {
		return err
	}
	result, err := b.db.ExecContext(ctx, b.queries.ReleaseLease,
		request.Key,
		request.Owner,
		request.Token,
		formatTime(b.currentTime()),
	)
	if err != nil {
		return fmt.Errorf("lease sqlstore: release lease: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("lease sqlstore: release rows affected: %w", err)
	}
	if affected == 0 {
		return gopact.ErrLeaseNotHeld
	}
	return nil
}

// GetLease returns the current non-expired lease for key.
func (b *Backend) GetLease(ctx context.Context, key string) (gopact.LeaseRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	if key == "" {
		return gopact.LeaseRecord{}, false, gopact.ErrLeaseKeyRequired
	}
	rows, err := b.db.QueryContext(ctx, b.queries.GetLease, key, formatTime(b.currentTime()))
	if err != nil {
		return gopact.LeaseRecord{}, false, fmt.Errorf("lease sqlstore: get lease: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return gopact.LeaseRecord{}, false, fmt.Errorf("lease sqlstore: get lease rows: %w", err)
		}
		return gopact.LeaseRecord{}, false, nil
	}
	record, err := scanLease(rows)
	if err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	if err := rows.Err(); err != nil {
		return gopact.LeaseRecord{}, false, fmt.Errorf("lease sqlstore: get lease rows: %w", err)
	}
	return record, true, nil
}

type leaseScanner interface {
	Scan(dest ...any) error
}

var leaseColumns = []string{
	"lease_key",
	"owner",
	"token",
	"acquired_at",
	"expires_at",
	"metadata_json",
}

var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateQueries(queries Queries) error {
	if strings.TrimSpace(queries.InsertLease) == "" ||
		strings.TrimSpace(queries.AcquireExpiredLease) == "" ||
		strings.TrimSpace(queries.RenewLease) == "" ||
		strings.TrimSpace(queries.ReleaseLease) == "" ||
		strings.TrimSpace(queries.GetLease) == "" {
		return ErrQueriesRequired
	}
	return nil
}

func safeIdentifier(table string) bool {
	return safeIdentifierRE.MatchString(table)
}

func placeholders(dialect Dialect, count int) ([]string, error) {
	out := make([]string, count)
	switch dialect {
	case DialectSQLite, DialectMySQL:
		for i := range out {
			out[i] = "?"
		}
	case DialectPostgres:
		for i := range out {
			out[i] = fmt.Sprintf("$%d", i+1)
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
	return out, nil
}

func validateAcquire(request gopact.LeaseRequest) error {
	if request.Key == "" {
		return gopact.ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return gopact.ErrLeaseOwnerRequired
	}
	if request.TTL <= 0 {
		return gopact.ErrLeaseTTLRequired
	}
	return nil
}

func validateRenew(request gopact.LeaseRenewRequest) error {
	if request.Key == "" {
		return gopact.ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return gopact.ErrLeaseOwnerRequired
	}
	if request.Token == "" {
		return gopact.ErrLeaseTokenRequired
	}
	if request.TTL <= 0 {
		return gopact.ErrLeaseTTLRequired
	}
	return nil
}

func validateRelease(request gopact.LeaseReleaseRequest) error {
	if request.Key == "" {
		return gopact.ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return gopact.ErrLeaseOwnerRequired
	}
	if request.Token == "" {
		return gopact.ErrLeaseTokenRequired
	}
	return nil
}

func (b *Backend) newLeaseRecord(request gopact.LeaseRequest, now time.Time) (gopact.LeaseRecord, error) {
	token, err := newLeaseToken()
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	return gopact.LeaseRecord{
		Key:        request.Key,
		Owner:      request.Owner,
		Token:      token,
		AcquiredAt: now,
		ExpiresAt:  now.Add(request.TTL),
		Metadata:   copyAnyMap(request.Metadata),
	}, nil
}

func (b *Backend) currentTime() time.Time {
	if b.now == nil {
		return time.Now().UTC()
	}
	return b.now().UTC()
}

func encodeLeaseArgs(record gopact.LeaseRecord) ([]any, error) {
	metadataJSON, err := encodeJSON(record.Metadata, "metadata")
	if err != nil {
		return nil, err
	}
	return []any{
		record.Key,
		record.Owner,
		record.Token,
		formatTime(record.AcquiredAt),
		formatTime(record.ExpiresAt),
		metadataJSON,
	}, nil
}

func encodeAcquireExpiredArgs(record gopact.LeaseRecord, now time.Time) ([]any, error) {
	metadataJSON, err := encodeJSON(record.Metadata, "metadata")
	if err != nil {
		return nil, err
	}
	return []any{
		record.Owner,
		record.Token,
		formatTime(record.AcquiredAt),
		formatTime(record.ExpiresAt),
		metadataJSON,
		record.Key,
		formatTime(now),
	}, nil
}

func encodeJSON(value any, name string) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("lease sqlstore: encode %s: %w", name, err)
	}
	return raw, nil
}

func scanLease(scanner leaseScanner) (gopact.LeaseRecord, error) {
	var record gopact.LeaseRecord
	var acquiredAt string
	var expiresAt string
	var metadataJSON []byte
	if err := scanner.Scan(
		&record.Key,
		&record.Owner,
		&record.Token,
		&acquiredAt,
		&expiresAt,
		&metadataJSON,
	); err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease sqlstore: scan lease: %w", err)
	}
	parsedAcquiredAt, err := parseTime(acquiredAt, "acquired_at")
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	parsedExpiresAt, err := parseTime(expiresAt, "expires_at")
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	record.AcquiredAt = parsedAcquiredAt
	record.ExpiresAt = parsedExpiresAt
	if err := decodeJSON(metadataJSON, &record.Metadata, "metadata"); err != nil {
		return gopact.LeaseRecord{}, err
	}
	return record, nil
}

func decodeJSON(raw []byte, dest any, name string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("lease sqlstore: decode %s: %w", name, err)
	}
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string, name string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("lease sqlstore: parse %s: %w", name, err)
	}
	return parsed, nil
}

func copyLeaseRecord(record gopact.LeaseRecord) gopact.LeaseRecord {
	record.Metadata = copyAnyMap(record.Metadata)
	return record
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func newLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("lease sqlstore: generate lease token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
