package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

const (
	// DefaultVersionedTable is the default table name used by NewVersionedBackend.
	DefaultVersionedTable = "gopact_turnloop_versioned_states"
)

// VersionedQueries contains the SQL statements used by VersionedBackend.
//
// InsertState must accept versioned row columns in default order and return
// RowsAffected=0 when the key already exists. UpdateState must accept
// state_version, schema_version, state_json, updated_at, metadata_json,
// state_key, expected_state_version and return RowsAffected=0 on CAS conflict.
// GetState must return versioned row columns in the same default order.
type VersionedQueries struct {
	InsertState string
	UpdateState string
	GetState    string
}

// VersionedBackend persists TurnLoop queue state through database/sql-compatible CAS calls.
type VersionedBackend struct {
	db      DBTX
	queries VersionedQueries
}

var _ gopact.TurnLoopVersionedBackend = (*VersionedBackend)(nil)

// VersionedOption configures a SQL TurnLoop versioned backend.
type VersionedOption func(*versionedBackendConfig) error

type versionedBackendConfig struct {
	queries VersionedQueries
}

// NewVersionedBackend creates a TurnLoop versioned backend backed by db.
func NewVersionedBackend(db DBTX, opts ...VersionedOption) (*VersionedBackend, error) {
	if db == nil {
		return nil, ErrDBRequired
	}
	queries, err := DefaultVersionedQueries(DefaultVersionedTable, DialectSQLite)
	if err != nil {
		return nil, err
	}
	cfg := versionedBackendConfig{queries: queries}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if err := validateVersionedQueries(cfg.queries); err != nil {
		return nil, err
	}
	return &VersionedBackend{db: db, queries: cfg.queries}, nil
}

// WithVersionedQueries replaces the generated SQL with host-owned CAS queries.
func WithVersionedQueries(queries VersionedQueries) VersionedOption {
	return func(cfg *versionedBackendConfig) error {
		if err := validateVersionedQueries(queries); err != nil {
			return err
		}
		cfg.queries = queries
		return nil
	}
}

// WithVersionedTable uses generated CAS queries for table and dialect.
func WithVersionedTable(table string, dialect Dialect) VersionedOption {
	return func(cfg *versionedBackendConfig) error {
		queries, err := DefaultVersionedQueries(table, dialect)
		if err != nil {
			return err
		}
		cfg.queries = queries
		return nil
	}
}

// DefaultVersionedQueries returns parameterized CAS SQL for a known dialect.
func DefaultVersionedQueries(table string, dialect Dialect) (VersionedQueries, error) {
	if !safeIdentifier(table) {
		return VersionedQueries{}, fmt.Errorf("%w: %q", ErrUnsafeTableName, table)
	}
	insertPlaceholders, err := placeholders(dialect, len(versionedRowColumns))
	if err != nil {
		return VersionedQueries{}, err
	}
	updatePlaceholders, err := placeholders(dialect, 7)
	if err != nil {
		return VersionedQueries{}, err
	}
	insertConflict, err := insertConflictClause(dialect)
	if err != nil {
		return VersionedQueries{}, err
	}
	keyPlaceholder, err := keyPlaceholder(dialect)
	if err != nil {
		return VersionedQueries{}, err
	}
	insertKeyword := "INSERT INTO"
	if dialect == DialectMySQL {
		insertKeyword = "INSERT IGNORE INTO"
	}
	columns := strings.Join(versionedRowColumns, ", ")
	return VersionedQueries{
		InsertState: fmt.Sprintf(
			"%s %s (%s) VALUES (%s) %s",
			insertKeyword,
			table,
			columns,
			strings.Join(insertPlaceholders, ", "),
			insertConflict,
		),
		UpdateState: fmt.Sprintf(
			"UPDATE %s SET state_version = %s, schema_version = %s, state_json = %s, updated_at = %s, metadata_json = %s WHERE state_key = %s AND state_version = %s",
			table,
			updatePlaceholders[0],
			updatePlaceholders[1],
			updatePlaceholders[2],
			updatePlaceholders[3],
			updatePlaceholders[4],
			updatePlaceholders[5],
			updatePlaceholders[6],
		),
		GetState: fmt.Sprintf(
			"SELECT %s FROM %s WHERE state_key = %s",
			columns,
			table,
			keyPlaceholder,
		),
	}, nil
}

// GetTurnLoopVersionedState returns one versioned TurnLoop state row by key.
func (b *VersionedBackend) GetTurnLoopVersionedState(ctx context.Context, key string) (gopact.TurnLoopVersionedRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	rows, err := b.db.QueryContext(ctx, b.queries.GetState, key)
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, fmt.Errorf("turnloop sqlstore: get versioned state: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return gopact.TurnLoopVersionedRecord{}, false, fmt.Errorf("turnloop sqlstore: get versioned state rows: %w", err)
		}
		return gopact.TurnLoopVersionedRecord{}, false, nil
	}
	record, err := scanVersionedRecord(rows)
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	if err := rows.Err(); err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, fmt.Errorf("turnloop sqlstore: get versioned state rows: %w", err)
	}
	return record, true, nil
}

// CompareAndSwapTurnLoopState writes record when expectedVersion matches.
func (b *VersionedBackend) CompareAndSwapTurnLoopState(ctx context.Context, record gopact.TurnLoopVersionedRecord, expectedVersion string) (string, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Key == "" {
		return "", errors.New("turnloop sqlstore: versioned row key is required")
	}
	version, err := newVersionToken()
	if err != nil {
		return "", err
	}
	record.Version = version

	var result sql.Result
	if expectedVersion == "" {
		args, err := encodeVersionedRecordArgs(record)
		if err != nil {
			return "", err
		}
		result, err = b.db.ExecContext(ctx, b.queries.InsertState, args...)
		if err != nil {
			return "", fmt.Errorf("turnloop sqlstore: insert versioned state: %w", err)
		}
	} else {
		args, err := encodeVersionedUpdateArgs(record, expectedVersion)
		if err != nil {
			return "", err
		}
		result, err = b.db.ExecContext(ctx, b.queries.UpdateState, args...)
		if err != nil {
			return "", fmt.Errorf("turnloop sqlstore: update versioned state: %w", err)
		}
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("turnloop sqlstore: versioned rows affected: %w", err)
	}
	if affected == 0 {
		return "", gopact.ErrTurnLoopStoreConflict
	}
	return version, nil
}

var versionedRowColumns = []string{
	"state_key",
	"state_version",
	"schema_version",
	"state_json",
	"updated_at",
	"metadata_json",
}

func validateVersionedQueries(queries VersionedQueries) error {
	if strings.TrimSpace(queries.InsertState) == "" ||
		strings.TrimSpace(queries.UpdateState) == "" ||
		strings.TrimSpace(queries.GetState) == "" {
		return ErrQueriesRequired
	}
	return nil
}

func insertConflictClause(dialect Dialect) (string, error) {
	switch dialect {
	case DialectSQLite, DialectPostgres:
		return "ON CONFLICT(state_key) DO NOTHING", nil
	case DialectMySQL:
		return "", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func encodeVersionedRecordArgs(record gopact.TurnLoopVersionedRecord) ([]any, error) {
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
		record.Version,
		record.SchemaVersion,
		stateJSON,
		formatTime(record.UpdatedAt),
		metadataJSON,
	}, nil
}

func encodeVersionedUpdateArgs(record gopact.TurnLoopVersionedRecord, expectedVersion string) ([]any, error) {
	stateJSON, err := encodeJSON(record.State, "state")
	if err != nil {
		return nil, err
	}
	metadataJSON, err := encodeJSON(record.Metadata, "metadata")
	if err != nil {
		return nil, err
	}
	return []any{
		record.Version,
		record.SchemaVersion,
		stateJSON,
		formatTime(record.UpdatedAt),
		metadataJSON,
		record.Key,
		expectedVersion,
	}, nil
}

func scanVersionedRecord(scanner recordScanner) (gopact.TurnLoopVersionedRecord, error) {
	var record gopact.TurnLoopVersionedRecord
	var stateJSON []byte
	var updatedAt string
	var metadataJSON []byte
	if err := scanner.Scan(
		&record.Key,
		&record.Version,
		&record.SchemaVersion,
		&stateJSON,
		&updatedAt,
		&metadataJSON,
	); err != nil {
		return gopact.TurnLoopVersionedRecord{}, fmt.Errorf("turnloop sqlstore: scan versioned state: %w", err)
	}
	parsedAt, err := parseTime(updatedAt)
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, err
	}
	record.UpdatedAt = parsedAt
	if err := decodeJSON(stateJSON, &record.State, "state"); err != nil {
		return gopact.TurnLoopVersionedRecord{}, err
	}
	if err := decodeJSON(metadataJSON, &record.Metadata, "metadata"); err != nil {
		return gopact.TurnLoopVersionedRecord{}, err
	}
	return record, nil
}

func newVersionToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("turnloop sqlstore: generate version token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
