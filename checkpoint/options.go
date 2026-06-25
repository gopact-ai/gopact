package checkpoint

// CodecOption is accepted by checkpoint stores that serialize graph state.
type CodecOption[S any] interface {
	MemoryOption[S]
	FileStoreOption[S]
	ObjectStoreOption[S]
	RowStoreOption[S]
}

type codecOption[S any] struct {
	codec StateCodec[S]
}

// StoreDecodeOption can configure both checkpoint stores and standalone decode calls.
type StoreDecodeOption[S any] interface {
	DecodeOption[S]
	MemoryOption[S]
	FileStoreOption[S]
	ObjectStoreOption[S]
	RowStoreOption[S]
}

type configVersionOption[S any] struct {
	version string
}

type configDriftPolicyOption[S any] struct {
	policy ConfigDriftPolicy
}

type recordMigrationOption[S any] struct {
	from    string
	migrate RecordMigrator
}

type objectPrefixOption[S any] struct {
	prefix string
}

var (
	_ CodecOption[struct{}]       = codecOption[struct{}]{}
	_ StoreDecodeOption[struct{}] = configVersionOption[struct{}]{}
	_ StoreDecodeOption[struct{}] = configDriftPolicyOption[struct{}]{}
	_ StoreDecodeOption[struct{}] = recordMigrationOption[struct{}]{}
	_ ObjectStoreOption[struct{}] = objectPrefixOption[struct{}]{}
)

// WithCodec sets the state codec used by a checkpoint store.
func WithCodec[S any](codec StateCodec[S]) CodecOption[S] {
	return codecOption[S]{codec: codec}
}

// WithConfigVersion sets the current runtime config version for checkpoint writes and reads.
func WithConfigVersion[S any](version string) StoreDecodeOption[S] {
	return configVersionOption[S]{version: version}
}

// WithCurrentConfigVersion sets the config version expected while decoding a checkpoint.
func WithCurrentConfigVersion[S any](version string) DecodeOption[S] {
	return configVersionOption[S]{version: version}
}

// WithConfigDriftPolicy sets how checkpoint decode handles config-version changes.
func WithConfigDriftPolicy[S any](policy ConfigDriftPolicy) StoreDecodeOption[S] {
	return configDriftPolicyOption[S]{policy: policy}
}

// WithRecordMigration registers a schema migration for checkpoint records.
func WithRecordMigration[S any](from string, migrate RecordMigrator) StoreDecodeOption[S] {
	return recordMigrationOption[S]{from: from, migrate: migrate}
}

// WithObjectPrefix scopes object checkpoint records under a storage key prefix.
func WithObjectPrefix[S any](prefix string) ObjectStoreOption[S] {
	return objectPrefixOption[S]{prefix: prefix}
}

func (o codecOption[S]) applyMemory(m *Memory[S]) {
	if o.codec != nil {
		m.codec = o.codec
	}
}

func (o codecOption[S]) applyFileStore(store *FileStore[S]) {
	if o.codec != nil {
		store.codec = o.codec
	}
}

func (o codecOption[S]) applyObjectStore(store *ObjectStore[S]) {
	if o.codec != nil {
		store.codec = o.codec
	}
}

func (o codecOption[S]) applyRowStore(store *RowStore[S]) {
	if o.codec != nil {
		store.codec = o.codec
	}
}

func (o configVersionOption[S]) applyDecode(cfg *decodeConfig) {
	cfg.currentConfigVersion = o.version
}

func (o configVersionOption[S]) applyMemory(m *Memory[S]) {
	m.configVersion = o.version
}

func (o configVersionOption[S]) applyFileStore(store *FileStore[S]) {
	store.configVersion = o.version
}

func (o configVersionOption[S]) applyObjectStore(store *ObjectStore[S]) {
	store.configVersion = o.version
}

func (o configVersionOption[S]) applyRowStore(store *RowStore[S]) {
	store.configVersion = o.version
}

func (o configDriftPolicyOption[S]) applyDecode(cfg *decodeConfig) {
	cfg.driftPolicy = o.policy
}

func (o configDriftPolicyOption[S]) applyMemory(m *Memory[S]) {
	m.driftPolicy = o.policy
}

func (o configDriftPolicyOption[S]) applyFileStore(store *FileStore[S]) {
	store.driftPolicy = o.policy
}

func (o configDriftPolicyOption[S]) applyObjectStore(store *ObjectStore[S]) {
	store.driftPolicy = o.policy
}

func (o configDriftPolicyOption[S]) applyRowStore(store *RowStore[S]) {
	store.driftPolicy = o.policy
}

func (o recordMigrationOption[S]) applyDecode(cfg *decodeConfig) {
	if o.from == "" || o.migrate == nil {
		return
	}
	if cfg.migrations == nil {
		cfg.migrations = make(map[string]RecordMigrator)
	}
	cfg.migrations[o.from] = o.migrate
}

func (o recordMigrationOption[S]) applyMemory(m *Memory[S]) {
	if o.from == "" || o.migrate == nil {
		return
	}
	if m.migrations == nil {
		m.migrations = make(map[string]RecordMigrator)
	}
	m.migrations[o.from] = o.migrate
}

func (o recordMigrationOption[S]) applyFileStore(store *FileStore[S]) {
	if o.from == "" || o.migrate == nil {
		return
	}
	if store.migrations == nil {
		store.migrations = make(map[string]RecordMigrator)
	}
	store.migrations[o.from] = o.migrate
}

func (o recordMigrationOption[S]) applyObjectStore(store *ObjectStore[S]) {
	if o.from == "" || o.migrate == nil {
		return
	}
	if store.migrations == nil {
		store.migrations = make(map[string]RecordMigrator)
	}
	store.migrations[o.from] = o.migrate
}

func (o recordMigrationOption[S]) applyRowStore(store *RowStore[S]) {
	if o.from == "" || o.migrate == nil {
		return
	}
	if store.migrations == nil {
		store.migrations = make(map[string]RecordMigrator)
	}
	store.migrations[o.from] = o.migrate
}

func (o objectPrefixOption[S]) applyObjectStore(store *ObjectStore[S]) {
	store.prefix = normalizeObjectPrefix(o.prefix)
}
