// Package sqlstore adapts database/sql-compatible stores to checkpoint.RowBackend.
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
	"github.com/gopact-ai/gopact/checkpoint"
)

const (
	// DefaultTable is the default table name used by NewBackend.
	DefaultTable = "gopact_checkpoints"
)

// Dialect selects the placeholder and upsert syntax for generated queries.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

var (
	ErrDBRequired         = errors.New("checkpoint sqlstore: db is required")
	ErrQueriesRequired    = errors.New("checkpoint sqlstore: queries are required")
	ErrUnsupportedDialect = errors.New("checkpoint sqlstore: unsupported dialect")
	ErrUnsafeTableName    = errors.New("checkpoint sqlstore: unsafe table name")
)

// DBTX is implemented by *sql.DB, *sql.Tx, and small wrappers around them.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Queries contains the SQL statements used by Backend.
//
// The upsert query must accept arguments in the default record column order.
// GetRecord and ListRecords must return the same columns in the same order.
type Queries struct {
	UpsertRecord string
	GetRecord    string
	ListRecords  string
}

// Backend persists checkpoint records through database/sql-compatible calls.
type Backend struct {
	db      DBTX
	queries Queries
}

var _ checkpoint.RowBackend = (*Backend)(nil)

// Option configures a SQL checkpoint backend.
type Option func(*backendConfig) error

type backendConfig struct {
	queries Queries
}

// NewBackend creates a checkpoint row backend backed by db.
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
	columns := strings.Join(recordColumns, ", ")
	placeholders, err := placeholders(dialect, len(recordColumns))
	if err != nil {
		return Queries{}, err
	}
	assignments, err := upsertAssignments(dialect)
	if err != nil {
		return Queries{}, err
	}
	idPlaceholder, threadPlaceholder, err := readPlaceholders(dialect)
	if err != nil {
		return Queries{}, err
	}
	return Queries{
		UpsertRecord: fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) %s",
			table,
			columns,
			strings.Join(placeholders, ", "),
			assignments,
		),
		GetRecord: fmt.Sprintf(
			"SELECT %s FROM %s WHERE id = %s",
			columns,
			table,
			idPlaceholder,
		),
		ListRecords: fmt.Sprintf(
			"SELECT %s FROM %s WHERE thread_id = %s ORDER BY created_at ASC, step ASC, id ASC",
			columns,
			table,
			threadPlaceholder,
		),
	}, nil
}

// UpsertRecord stores or replaces one checkpoint record.
func (b *Backend) UpsertRecord(ctx context.Context, record checkpoint.Record) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.ID == "" {
		return errors.New("checkpoint sqlstore: record id is required")
	}
	args, err := encodeRecordArgs(record)
	if err != nil {
		return err
	}
	if _, err := b.db.ExecContext(ctx, b.queries.UpsertRecord, args...); err != nil {
		return fmt.Errorf("checkpoint sqlstore: upsert record: %w", err)
	}
	return nil
}

// GetRecord returns one checkpoint record by id.
func (b *Backend) GetRecord(ctx context.Context, id string) (checkpoint.Record, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return checkpoint.Record{}, false, err
	}
	rows, err := b.db.QueryContext(ctx, b.queries.GetRecord, id)
	if err != nil {
		return checkpoint.Record{}, false, fmt.Errorf("checkpoint sqlstore: get record: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return checkpoint.Record{}, false, fmt.Errorf("checkpoint sqlstore: get record rows: %w", err)
		}
		return checkpoint.Record{}, false, nil
	}
	record, err := scanRecord(rows)
	if err != nil {
		return checkpoint.Record{}, false, err
	}
	if err := rows.Err(); err != nil {
		return checkpoint.Record{}, false, fmt.Errorf("checkpoint sqlstore: get record rows: %w", err)
	}
	return record, true, nil
}

// ListRecords returns checkpoint records for one thread.
func (b *Backend) ListRecords(ctx context.Context, threadID string) ([]checkpoint.Record, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := b.db.QueryContext(ctx, b.queries.ListRecords, threadID)
	if err != nil {
		return nil, fmt.Errorf("checkpoint sqlstore: list records: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var records []checkpoint.Record
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkpoint sqlstore: list records rows: %w", err)
	}
	return records, nil
}

type recordScanner interface {
	Scan(dest ...any) error
}

var recordColumns = []string{
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

var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateQueries(queries Queries) error {
	if strings.TrimSpace(queries.UpsertRecord) == "" ||
		strings.TrimSpace(queries.GetRecord) == "" ||
		strings.TrimSpace(queries.ListRecords) == "" {
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

func readPlaceholders(dialect Dialect) (id string, thread string, err error) {
	switch dialect {
	case DialectSQLite, DialectMySQL:
		return "?", "?", nil
	case DialectPostgres:
		return "$1", "$1", nil
	default:
		return "", "", fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func upsertAssignments(dialect Dialect) (string, error) {
	updateColumns := recordColumns[1:]
	assignments := make([]string, 0, len(updateColumns))
	switch dialect {
	case DialectSQLite, DialectPostgres:
		for _, column := range updateColumns {
			assignments = append(assignments, fmt.Sprintf("%s = excluded.%s", column, column))
		}
		return "ON CONFLICT(id) DO UPDATE SET " + strings.Join(assignments, ", "), nil
	case DialectMySQL:
		for _, column := range updateColumns {
			assignments = append(assignments, fmt.Sprintf("%s = VALUES(%s)", column, column))
		}
		return "ON DUPLICATE KEY UPDATE " + strings.Join(assignments, ", "), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func encodeRecordArgs(record checkpoint.Record) ([]any, error) {
	idsJSON, err := encodeJSON(record.IDs, "ids")
	if err != nil {
		return nil, err
	}
	queueJSON, err := encodeJSON(record.Queue, "queue")
	if err != nil {
		return nil, err
	}
	pendingJSON, err := encodeJSON(record.Pending, "pending")
	if err != nil {
		return nil, err
	}
	effectsJSON, err := encodeJSON(record.Effects, "effects")
	if err != nil {
		return nil, err
	}
	metadataJSON, err := encodeJSON(record.Metadata, "metadata")
	if err != nil {
		return nil, err
	}
	return []any{
		record.ID,
		record.SchemaVersion,
		idsJSON,
		record.ThreadID,
		record.Step,
		record.Node,
		string(record.Phase),
		append([]byte(nil), record.State...),
		record.StateCodec,
		record.StateHash,
		queueJSON,
		pendingJSON,
		effectsJSON,
		record.ConfigVersion,
		formatTime(record.CreatedAt),
		metadataJSON,
	}, nil
}

func encodeJSON(value any, name string) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("checkpoint sqlstore: encode %s: %w", name, err)
	}
	return raw, nil
}

func scanRecord(scanner recordScanner) (checkpoint.Record, error) {
	var record checkpoint.Record
	var idsJSON []byte
	var phase string
	var queueJSON []byte
	var pendingJSON []byte
	var effectsJSON []byte
	var createdAt string
	var metadataJSON []byte

	if err := scanner.Scan(
		&record.ID,
		&record.SchemaVersion,
		&idsJSON,
		&record.ThreadID,
		&record.Step,
		&record.Node,
		&phase,
		&record.State,
		&record.StateCodec,
		&record.StateHash,
		&queueJSON,
		&pendingJSON,
		&effectsJSON,
		&record.ConfigVersion,
		&createdAt,
		&metadataJSON,
	); err != nil {
		return checkpoint.Record{}, fmt.Errorf("checkpoint sqlstore: scan record: %w", err)
	}
	record.Phase = gopact.StepPhase(phase)
	parsedAt, err := parseTime(createdAt)
	if err != nil {
		return checkpoint.Record{}, err
	}
	record.CreatedAt = parsedAt
	if err := decodeJSON(idsJSON, &record.IDs, "ids"); err != nil {
		return checkpoint.Record{}, err
	}
	if err := decodeJSON(queueJSON, &record.Queue, "queue"); err != nil {
		return checkpoint.Record{}, err
	}
	if err := decodeJSON(pendingJSON, &record.Pending, "pending"); err != nil {
		return checkpoint.Record{}, err
	}
	if err := decodeJSON(effectsJSON, &record.Effects, "effects"); err != nil {
		return checkpoint.Record{}, err
	}
	if err := decodeJSON(metadataJSON, &record.Metadata, "metadata"); err != nil {
		return checkpoint.Record{}, err
	}
	return record, nil
}

func decodeJSON(raw []byte, dest any, name string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("checkpoint sqlstore: decode %s: %w", name, err)
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
		return time.Time{}, fmt.Errorf("checkpoint sqlstore: parse created_at: %w", err)
	}
	return parsed, nil
}
