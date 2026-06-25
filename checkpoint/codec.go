package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

const (
	// SchemaVersion is the current checkpoint record schema.
	SchemaVersion = "checkpoint.v1"

	// MetadataConfigDrift marks checkpoints decoded with an allowed config drift.
	MetadataConfigDrift = "checkpoint_config_drift"
)

var (
	// ErrCodecMismatch is returned when a checkpoint record was encoded with a different codec.
	ErrCodecMismatch = errors.New("checkpoint: codec mismatch")
	// ErrConfigDrift is returned when a checkpoint is decoded under a disallowed config version drift.
	ErrConfigDrift = errors.New("checkpoint: config drift")
	// ErrIntegrityMismatch is returned when a checkpoint payload fails its integrity check.
	ErrIntegrityMismatch = errors.New("checkpoint: integrity mismatch")
	// ErrSchemaMismatch is returned when a checkpoint record uses an unsupported schema version.
	ErrSchemaMismatch = errors.New("checkpoint: schema mismatch")
)

// StateCodec serializes graph state into a stable checkpoint payload.
type StateCodec[S any] interface {
	Name() string
	Marshal(S) ([]byte, error)
	Unmarshal([]byte) (S, error)
}

// JSONCodec serializes state using encoding/json.
type JSONCodec[S any] struct{}

// Name returns the stable codec name stored in checkpoint records.
func (JSONCodec[S]) Name() string {
	return "json"
}

// Marshal serializes state as JSON.
func (JSONCodec[S]) Marshal(state S) ([]byte, error) {
	return json.Marshal(state)
}

// Unmarshal restores state from JSON.
func (JSONCodec[S]) Unmarshal(data []byte) (S, error) {
	var state S
	err := json.Unmarshal(data, &state)
	return state, err
}

// ConfigDriftPolicy controls how DecodeCheckpoint handles config-version changes.
type ConfigDriftPolicy int

const (
	// ConfigDriftDeny rejects a checkpoint when stored and current config versions differ.
	ConfigDriftDeny ConfigDriftPolicy = iota
	// ConfigDriftAllow decodes the checkpoint and annotates metadata with ConfigDrift.
	ConfigDriftAllow
)

// ConfigDrift describes a checkpoint restored under a different runtime configuration.
type ConfigDrift struct {
	StoredVersion  string `json:"stored_version,omitempty"`
	CurrentVersion string `json:"current_version,omitempty"`
}

// RecordMigrator upgrades one checkpoint record schema version to a newer version.
type RecordMigrator func(Record) (Record, error)

type decodeConfig struct {
	currentConfigVersion string
	driftPolicy          ConfigDriftPolicy
	migrations           map[string]RecordMigrator
}

// DecodeOption configures checkpoint decode, including migration and config drift checks.
type DecodeOption[S any] interface {
	applyDecode(*decodeConfig)
}

// Record is the stable, integrity-checked checkpoint storage representation.
type Record struct {
	ID            string                  `json:"id,omitempty"`
	SchemaVersion string                  `json:"schema_version,omitempty"`
	IDs           gopact.RuntimeIDs       `json:"ids,omitempty"`
	ThreadID      string                  `json:"thread_id,omitempty"`
	Step          int                     `json:"step,omitempty"`
	Node          string                  `json:"node,omitempty"`
	Phase         gopact.StepPhase        `json:"phase,omitempty"`
	State         []byte                  `json:"state,omitempty"`
	StateCodec    string                  `json:"state_codec,omitempty"`
	StateHash     string                  `json:"state_hash,omitempty"`
	Queue         []string                `json:"queue,omitempty"`
	Pending       *gopact.InterruptRecord `json:"pending,omitempty"`
	Effects       []gopact.EffectRecord   `json:"effects,omitempty"`
	ConfigVersion string                  `json:"config_version,omitempty"`
	CreatedAt     time.Time               `json:"created_at,omitempty"`
	Metadata      map[string]any          `json:"metadata,omitempty"`
}

// EncodeCheckpoint converts a typed graph checkpoint into a stable record.
func EncodeCheckpoint[S any](checkpoint graph.Checkpoint[S], codec StateCodec[S]) (Record, error) {
	if codec == nil {
		return Record{}, errors.New("checkpoint: state codec is required")
	}
	state, err := codec.Marshal(checkpoint.State)
	if err != nil {
		return Record{}, fmt.Errorf("checkpoint: encode state: %w", err)
	}
	threadID := checkpoint.ThreadID
	if threadID == "" {
		threadID = checkpoint.IDs.ThreadID
	}
	return Record{
		ID:            checkpoint.ID,
		SchemaVersion: SchemaVersion,
		IDs:           checkpoint.IDs,
		ThreadID:      threadID,
		Step:          checkpoint.Step,
		Node:          checkpoint.Node,
		Phase:         checkpoint.Phase,
		State:         append([]byte(nil), state...),
		StateCodec:    codec.Name(),
		StateHash:     hashState(state),
		Queue:         append([]string(nil), checkpoint.Queue...),
		Pending:       copyPending(checkpoint.Pending),
		Effects:       copyEffects(checkpoint.Effects),
		ConfigVersion: checkpoint.ConfigVersion,
		CreatedAt:     checkpoint.CreatedAt,
		Metadata:      copyMap(checkpoint.Metadata),
	}, nil
}

// DecodeCheckpoint verifies record integrity and decodes it into a typed graph checkpoint.
func DecodeCheckpoint[S any](record Record, codec StateCodec[S], opts ...DecodeOption[S]) (graph.Checkpoint[S], error) {
	cfg := decodeConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyDecode(&cfg)
		}
	}
	return decodeCheckpointWithConfig[S](record, codec, cfg)
}

func decodeCheckpointWithConfig[S any](record Record, codec StateCodec[S], cfg decodeConfig) (graph.Checkpoint[S], error) {
	var zero graph.Checkpoint[S]
	if codec == nil {
		return zero, errors.New("checkpoint: state codec is required")
	}
	migrated, err := migrateRecord(record, cfg.migrations)
	if err != nil {
		return zero, err
	}
	record = migrated
	if record.SchemaVersion != "" && record.SchemaVersion != SchemaVersion {
		return zero, fmt.Errorf("%w: got %q want %q", ErrSchemaMismatch, record.SchemaVersion, SchemaVersion)
	}
	if record.StateCodec != "" && record.StateCodec != codec.Name() {
		return zero, fmt.Errorf("%w: got %q want %q", ErrCodecMismatch, record.StateCodec, codec.Name())
	}
	if record.StateHash != "" && record.StateHash != hashState(record.State) {
		return zero, fmt.Errorf("%w: state hash does not match", ErrIntegrityMismatch)
	}
	state, err := codec.Unmarshal(record.State)
	if err != nil {
		return zero, fmt.Errorf("checkpoint: decode state: %w", err)
	}
	metadata, err := decodeMetadata(record, cfg)
	if err != nil {
		return zero, err
	}
	return graph.Checkpoint[S]{
		ID:            record.ID,
		IDs:           record.IDs,
		ThreadID:      record.ThreadID,
		Step:          record.Step,
		Node:          record.Node,
		Phase:         record.Phase,
		State:         state,
		Queue:         append([]string(nil), record.Queue...),
		Pending:       copyPending(record.Pending),
		Effects:       copyEffects(record.Effects),
		ConfigVersion: record.ConfigVersion,
		CreatedAt:     record.CreatedAt,
		Metadata:      metadata,
	}, nil
}

func migrateRecord(record Record, migrations map[string]RecordMigrator) (Record, error) {
	if record.SchemaVersion == "" || record.SchemaVersion == SchemaVersion {
		return record, nil
	}
	seen := map[string]struct{}{}
	for record.SchemaVersion != "" && record.SchemaVersion != SchemaVersion {
		if _, ok := seen[record.SchemaVersion]; ok {
			return Record{}, fmt.Errorf("checkpoint: migration cycle at schema %q", record.SchemaVersion)
		}
		seen[record.SchemaVersion] = struct{}{}
		migrate := migrations[record.SchemaVersion]
		if migrate == nil {
			return record, nil
		}
		from := record.SchemaVersion
		next, err := migrate(copyRecord(record))
		if err != nil {
			return Record{}, fmt.Errorf("checkpoint: migrate schema %q: %w", from, err)
		}
		if next.SchemaVersion == from {
			return Record{}, fmt.Errorf("checkpoint: migration from schema %q did not advance schema version", from)
		}
		record = next
	}
	return record, nil
}

func decodeMetadata(record Record, cfg decodeConfig) (map[string]any, error) {
	metadata := copyMap(record.Metadata)
	if record.ConfigVersion == "" || cfg.currentConfigVersion == "" || record.ConfigVersion == cfg.currentConfigVersion {
		return metadata, nil
	}
	if cfg.driftPolicy != ConfigDriftAllow {
		return nil, fmt.Errorf("%w: stored %q current %q", ErrConfigDrift, record.ConfigVersion, cfg.currentConfigVersion)
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[MetadataConfigDrift] = ConfigDrift{
		StoredVersion:  record.ConfigVersion,
		CurrentVersion: cfg.currentConfigVersion,
	}
	return metadata, nil
}

func hashState(state []byte) string {
	sum := sha256.Sum256(state)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func copyPending(in *gopact.InterruptRecord) *gopact.InterruptRecord {
	if in == nil {
		return nil
	}
	out := *in
	out.Metadata = copyMap(in.Metadata)
	return &out
}

func copyEffects(in []gopact.EffectRecord) []gopact.EffectRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.EffectRecord, len(in))
	for i, effect := range in {
		out[i] = effect
		out[i].DependsOn = append([]string(nil), effect.DependsOn...)
		out[i].Artifacts = copyArtifactRefs(effect.Artifacts)
		if effect.Sandbox != nil {
			sandbox := *effect.Sandbox
			sandbox.Command = append([]string(nil), effect.Sandbox.Command...)
			sandbox.Metadata = copyMap(effect.Sandbox.Metadata)
			out[i].Sandbox = &sandbox
		}
		out[i].Metadata = copyMap(effect.Metadata)
	}
	return out
}

func copyRecord(in Record) Record {
	out := in
	out.State = append([]byte(nil), in.State...)
	out.Queue = append([]string(nil), in.Queue...)
	out.Pending = copyPending(in.Pending)
	out.Effects = copyEffects(in.Effects)
	out.Metadata = copyMap(in.Metadata)
	return out
}

func copyArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Metadata = copyMap(ref.Metadata)
	}
	return out
}

func copyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyMigrations(in map[string]RecordMigrator) map[string]RecordMigrator {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]RecordMigrator, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
