// Package sqlstore adapts database/sql-compatible stores to gopact.TurnLoopRowBackend.
package sqlstore

import (
	"context"
	"database/sql"
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
	DefaultTable = "gopact_turnloop_states"
)

// Dialect selects the placeholder and upsert syntax for generated queries.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

var (
	ErrDBRequired         = errors.New("turnloop sqlstore: db is required")
	ErrQueriesRequired    = errors.New("turnloop sqlstore: queries are required")
	ErrUnsupportedDialect = errors.New("turnloop sqlstore: unsupported dialect")
	ErrUnsafeTableName    = errors.New("turnloop sqlstore: unsafe table name")
)

// DBTX is implemented by *sql.DB, *sql.Tx, and small wrappers around them.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Queries contains the SQL statements used by Backend.
//
// The upsert query must accept arguments in the default row column order.
// GetState must return the same columns in the same order.
type Queries struct {
	UpsertState string
	GetState    string
}

// Backend persists TurnLoop queue state through database/sql-compatible calls.
type Backend struct {
	db      DBTX
	queries Queries
}

var _ gopact.TurnLoopRowBackend = (*Backend)(nil)

// Option configures a SQL TurnLoop backend.
type Option func(*backendConfig) error

type backendConfig struct {
	queries Queries
}

// NewBackend creates a TurnLoop row backend backed by db.
func NewBackend(db DBTX, opts ...Option) (*Backend, error) {
	if db == nil {
		return nil, ErrDBRequired
	}
	queries, err := DefaultQueries(DefaultTable, DialectSQLite)
	if err != nil {
		return nil, err
	}
	cfg := backendConfig{queries: queries}
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
	return &Backend{db: db, queries: cfg.queries}, nil
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

// DefaultQueries returns parameterized SQL for a known dialect.
func DefaultQueries(table string, dialect Dialect) (Queries, error) {
	if !safeIdentifier(table) {
		return Queries{}, fmt.Errorf("%w: %q", ErrUnsafeTableName, table)
	}
	columns := strings.Join(rowColumns, ", ")
	placeholders, err := placeholders(dialect, len(rowColumns))
	if err != nil {
		return Queries{}, err
	}
	assignments, err := upsertAssignments(dialect)
	if err != nil {
		return Queries{}, err
	}
	keyPlaceholder, err := keyPlaceholder(dialect)
	if err != nil {
		return Queries{}, err
	}
	return Queries{
		UpsertState: fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) %s",
			table,
			columns,
			strings.Join(placeholders, ", "),
			assignments,
		),
		GetState: fmt.Sprintf(
			"SELECT %s FROM %s WHERE state_key = %s",
			columns,
			table,
			keyPlaceholder,
		),
	}, nil
}

// UpsertTurnLoopState stores or replaces one TurnLoop state row.
func (b *Backend) UpsertTurnLoopState(ctx context.Context, record gopact.TurnLoopRowRecord) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Key == "" {
		return errors.New("turnloop sqlstore: row key is required")
	}
	args, err := encodeRecordArgs(record)
	if err != nil {
		return err
	}
	if _, err := b.db.ExecContext(ctx, b.queries.UpsertState, args...); err != nil {
		return fmt.Errorf("turnloop sqlstore: upsert state: %w", err)
	}
	return nil
}

// GetTurnLoopState returns one TurnLoop state row by key.
func (b *Backend) GetTurnLoopState(ctx context.Context, key string) (gopact.TurnLoopRowRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopRowRecord{}, false, err
	}
	rows, err := b.db.QueryContext(ctx, b.queries.GetState, key)
	if err != nil {
		return gopact.TurnLoopRowRecord{}, false, fmt.Errorf("turnloop sqlstore: get state: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return gopact.TurnLoopRowRecord{}, false, fmt.Errorf("turnloop sqlstore: get state rows: %w", err)
		}
		return gopact.TurnLoopRowRecord{}, false, nil
	}
	record, err := scanRecord(rows)
	if err != nil {
		return gopact.TurnLoopRowRecord{}, false, err
	}
	if err := rows.Err(); err != nil {
		return gopact.TurnLoopRowRecord{}, false, fmt.Errorf("turnloop sqlstore: get state rows: %w", err)
	}
	return record, true, nil
}

type recordScanner interface {
	Scan(dest ...any) error
}

var rowColumns = []string{
	"state_key",
	"schema_version",
	"state_json",
	"updated_at",
	"metadata_json",
}

var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateQueries(queries Queries) error {
	if strings.TrimSpace(queries.UpsertState) == "" ||
		strings.TrimSpace(queries.GetState) == "" {
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

func keyPlaceholder(dialect Dialect) (string, error) {
	switch dialect {
	case DialectSQLite, DialectMySQL:
		return "?", nil
	case DialectPostgres:
		return "$1", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func upsertAssignments(dialect Dialect) (string, error) {
	updateColumns := rowColumns[1:]
	assignments := make([]string, 0, len(updateColumns))
	switch dialect {
	case DialectSQLite, DialectPostgres:
		for _, column := range updateColumns {
			assignments = append(assignments, fmt.Sprintf("%s = excluded.%s", column, column))
		}
		return "ON CONFLICT(state_key) DO UPDATE SET " + strings.Join(assignments, ", "), nil
	case DialectMySQL:
		for _, column := range updateColumns {
			assignments = append(assignments, fmt.Sprintf("%s = VALUES(%s)", column, column))
		}
		return "ON DUPLICATE KEY UPDATE " + strings.Join(assignments, ", "), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func encodeRecordArgs(record gopact.TurnLoopRowRecord) ([]any, error) {
	stateJSON, err := encodeJSON(record.State, "state")
	if err != nil {
		return nil, err
	}
	metadataJSON, err := encodeJSON(record.Metadata, "metadata")
	if err != nil {
		return nil, err
	}
	return []any{
		record.Key,
		record.SchemaVersion,
		stateJSON,
		formatTime(record.UpdatedAt),
		metadataJSON,
	}, nil
}

func encodeJSON(value any, name string) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("turnloop sqlstore: encode %s: %w", name, err)
	}
	return raw, nil
}

func scanRecord(scanner recordScanner) (gopact.TurnLoopRowRecord, error) {
	var record gopact.TurnLoopRowRecord
	var stateJSON []byte
	var updatedAt string
	var metadataJSON []byte
	if err := scanner.Scan(
		&record.Key,
		&record.SchemaVersion,
		&stateJSON,
		&updatedAt,
		&metadataJSON,
	); err != nil {
		return gopact.TurnLoopRowRecord{}, fmt.Errorf("turnloop sqlstore: scan state: %w", err)
	}
	parsedAt, err := parseTime(updatedAt)
	if err != nil {
		return gopact.TurnLoopRowRecord{}, err
	}
	record.UpdatedAt = parsedAt
	if err := decodeJSON(stateJSON, &record.State, "state"); err != nil {
		return gopact.TurnLoopRowRecord{}, err
	}
	if err := decodeJSON(metadataJSON, &record.Metadata, "metadata"); err != nil {
		return gopact.TurnLoopRowRecord{}, err
	}
	return record, nil
}

func decodeJSON(raw []byte, dest any, name string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("turnloop sqlstore: decode %s: %w", name, err)
	}
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("turnloop sqlstore: parse updated_at: %w", err)
	}
	return parsed, nil
}
