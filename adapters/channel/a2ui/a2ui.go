// Package a2ui provides A2UI-oriented transfer and channel adapters.
package a2ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// Target is the channel target name used by the A2UI adapter.
	Target gopact.ChannelTarget = "a2ui"

	// Version is the A2UI protocol version emitted by this adapter.
	Version = "v0.9"

	// MIMEType is the media type used when A2UI messages are transported by A2A
	// or another generic channel.
	MIMEType = "application/a2ui+json"

	defaultCatalogID = "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json"
)

var (
	// ErrClosed is returned when an operation targets a closed A2UI channel.
	ErrClosed = errors.New("a2ui: channel closed")
	// ErrWriterRequired is returned when a channel is created without a writer.
	ErrWriterRequired = errors.New("a2ui: writer is required")
	// ErrUnsupportedPayload is returned when a payload cannot be encoded as A2UI.
	ErrUnsupportedPayload = errors.New("a2ui: unsupported payload")
	// ErrValidationFailed is returned when an A2UI payload violates the adapter schema.
	ErrValidationFailed = errors.New("a2ui: validation failed")
)

// Payload is the A2UI representation of a surface message.
type Payload struct {
	MIMEType    string            `json:"mime_type,omitempty"`
	SurfaceID   string            `json:"surface_id,omitempty"`
	Version     string            `json:"version,omitempty"`
	Messages    []Message         `json:"messages,omitempty"`
	IDs         gopact.RuntimeIDs `json:"ids,omitempty"`
	SourceEvent string            `json:"source_event,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
}

// Message is one A2UI JSON message. Exactly one message body should be set.
type Message struct {
	Version          string            `json:"version"`
	CreateSurface    *CreateSurface    `json:"createSurface,omitempty"`
	UpdateComponents *UpdateComponents `json:"updateComponents,omitempty"`
	UpdateDataModel  *UpdateDataModel  `json:"updateDataModel,omitempty"`
	DeleteSurface    *DeleteSurface    `json:"deleteSurface,omitempty"`
}

// CreateSurface creates or resets an A2UI surface.
type CreateSurface struct {
	SurfaceID     string `json:"surfaceId"`
	CatalogID     string `json:"catalogId,omitempty"`
	SendDataModel bool   `json:"sendDataModel,omitempty"`
}

// UpdateComponents replaces or updates components on an A2UI surface.
type UpdateComponents struct {
	SurfaceID  string      `json:"surfaceId"`
	Components []Component `json:"components,omitempty"`
}

// UpdateDataModel updates an A2UI surface data model path.
type UpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path"`
	Value     any    `json:"value,omitempty"`
}

// DeleteSurface deletes an A2UI surface.
type DeleteSurface struct {
	SurfaceID string `json:"surfaceId"`
}

// Component is the minimal A2UI component shape used by the transfer.
type Component struct {
	ID        string         `json:"id"`
	Component string         `json:"component"`
	Text      any            `json:"text,omitempty"`
	Variant   string         `json:"variant,omitempty"`
	Children  []string       `json:"children,omitempty"`
	Child     string         `json:"child,omitempty"`
	Action    *ActionBinding `json:"action,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ActionBinding binds an A2UI component action to a renderer event.
type ActionBinding struct {
	Event *ActionEvent `json:"event,omitempty"`
}

// ActionEvent is the A2UI action event emitted by a component.
type ActionEvent struct {
	Name    string         `json:"name"`
	Context map[string]any `json:"context,omitempty"`
}

// Action is an inbound A2UI renderer action.
type Action struct {
	Name              string         `json:"name,omitempty"`
	SurfaceID         string         `json:"surfaceId,omitempty"`
	SourceComponentID string         `json:"sourceComponentId,omitempty"`
	Timestamp         time.Time      `json:"timestamp,omitempty"`
	Context           map[string]any `json:"context,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// ActionFrame is the top-level A2UI action frame emitted by a renderer.
type ActionFrame struct {
	Version string `json:"version,omitempty"`
	Action  Action `json:"action"`
}

// Catalog is a local A2UI component catalog known to the host.
type Catalog struct {
	ID         string                     `json:"catalogId"`
	Components map[string]ComponentSchema `json:"components,omitempty"`
}

// ComponentSchema is the first validation slice for an A2UI catalog component.
type ComponentSchema struct {
	Name   string            `json:"name,omitempty"`
	Schema gopact.JSONSchema `json:"schema,omitempty"`
}

// NewCatalog creates a local catalog from component names.
func NewCatalog(id string, components ...string) Catalog {
	catalog := Catalog{
		ID:         strings.TrimSpace(id),
		Components: make(map[string]ComponentSchema, len(components)),
	}
	for _, component := range components {
		name := strings.TrimSpace(component)
		if name != "" {
			catalog.Components[name] = ComponentSchema{Name: name}
		}
	}
	return catalog
}

// CatalogRegistry stores catalogs available to the host.
type CatalogRegistry struct {
	catalogs map[string]Catalog
}

// NewCatalogRegistry creates a registry from local catalogs.
func NewCatalogRegistry(catalogs ...Catalog) (*CatalogRegistry, error) {
	registry := &CatalogRegistry{catalogs: make(map[string]Catalog, len(catalogs))}
	for _, catalog := range catalogs {
		if err := registry.Register(catalog); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register adds or replaces a catalog.
func (r *CatalogRegistry) Register(catalog Catalog) error {
	if strings.TrimSpace(catalog.ID) == "" {
		return fmt.Errorf("%w: catalog id is required", ErrValidationFailed)
	}
	if r.catalogs == nil {
		r.catalogs = make(map[string]Catalog)
	}
	r.catalogs[catalog.ID] = copyCatalog(catalog)
	return nil
}

// Catalog returns a catalog by id.
func (r *CatalogRegistry) Catalog(id string) (Catalog, bool) {
	if r == nil {
		return Catalog{}, false
	}
	catalog, ok := r.catalogs[id]
	if !ok {
		return Catalog{}, false
	}
	return copyCatalog(catalog), true
}

// Select returns the first client-supported catalog known to this registry.
func (r *CatalogRegistry) Select(clientSupported []string) (Catalog, bool) {
	if r == nil {
		return Catalog{}, false
	}
	for _, catalogID := range clientSupported {
		if catalog, ok := r.Catalog(catalogID); ok {
			return catalog, true
		}
	}
	return Catalog{}, false
}

// Validator validates A2UI messages against local catalog structure.
type Validator struct {
	mu              sync.Mutex
	registry        *CatalogRegistry
	surfaceCatalogs map[string]Catalog
	schemaValidator gopact.JSONSchemaValidator
}

// ValidatorConfig configures an A2UI catalog validator.
type ValidatorConfig struct {
	Catalogs        []Catalog
	SchemaValidator gopact.JSONSchemaValidator
}

// NewValidator creates a catalog-backed message validator.
func NewValidator(catalogs ...Catalog) (*Validator, error) {
	return NewValidatorWithConfig(ValidatorConfig{Catalogs: catalogs})
}

// NewValidatorWithConfig creates a catalog-backed message validator from typed configuration.
func NewValidatorWithConfig(config ValidatorConfig) (*Validator, error) {
	registry, err := NewCatalogRegistry(config.Catalogs...)
	if err != nil {
		return nil, err
	}
	return &Validator{
		registry:        registry,
		surfaceCatalogs: make(map[string]Catalog),
		schemaValidator: config.SchemaValidator,
	}, nil
}

// ValidationError carries stable context for a validation failure.
type ValidationError struct {
	CatalogID   string
	SurfaceID   string
	ComponentID string
	Component   string
	Message     string
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e == nil {
		return ErrValidationFailed.Error()
	}
	if e.Component != "" {
		return fmt.Sprintf("%s: component %q in %q: %s", ErrValidationFailed, e.Component, e.ComponentID, e.Message)
	}
	if e.CatalogID != "" {
		return fmt.Sprintf("%s: catalog %q: %s", ErrValidationFailed, e.CatalogID, e.Message)
	}
	return fmt.Sprintf("%s: %s", ErrValidationFailed, e.Message)
}

// Unwrap returns the validation sentinel.
func (e *ValidationError) Unwrap() error {
	return ErrValidationFailed
}

// ValidateMessages validates message structure and component catalog membership.
func (v *Validator) ValidateMessages(messages []Message) error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	for _, message := range messages {
		if message.CreateSurface != nil {
			catalogID := message.CreateSurface.CatalogID
			catalog, ok := v.registry.Catalog(catalogID)
			if !ok {
				return &ValidationError{
					CatalogID: catalogID,
					SurfaceID: message.CreateSurface.SurfaceID,
					Message:   "catalog is not registered",
				}
			}
			v.surfaceCatalogs[message.CreateSurface.SurfaceID] = catalog
		}
		if message.UpdateComponents != nil {
			catalog, ok := v.surfaceCatalogs[message.UpdateComponents.SurfaceID]
			if !ok {
				return &ValidationError{
					SurfaceID: message.UpdateComponents.SurfaceID,
					Message:   "surface catalog is unknown",
				}
			}
			if err := validateComponents(catalog, message.UpdateComponents.SurfaceID, message.UpdateComponents.Components, v.schemaValidator); err != nil {
				return err
			}
		}
	}
	return nil
}

// SurfaceSnapshot is the renderer's current materialized view of one A2UI surface.
type SurfaceSnapshot struct {
	ID            string               `json:"id"`
	CatalogID     string               `json:"catalog_id,omitempty"`
	SendDataModel bool                 `json:"send_data_model,omitempty"`
	Components    map[string]Component `json:"components,omitempty"`
	DataModel     any                  `json:"data_model,omitempty"`
}

// Renderer materializes A2UI v0.9 messages into in-memory surface snapshots.
type Renderer struct {
	mu       sync.Mutex
	registry *CatalogRegistry
	surfaces map[string]SurfaceSnapshot
}

// NewRenderer creates an in-memory A2UI renderer. When catalogs are provided,
// component updates are validated against the surface catalog.
func NewRenderer(catalogs ...Catalog) (*Renderer, error) {
	var registry *CatalogRegistry
	if len(catalogs) > 0 {
		var err error
		registry, err = NewCatalogRegistry(catalogs...)
		if err != nil {
			return nil, err
		}
	}
	return &Renderer{
		registry: registry,
		surfaces: make(map[string]SurfaceSnapshot),
	}, nil
}

// ApplyPayload normalizes an A2UI channel payload and applies its messages.
func (r *Renderer) ApplyPayload(payload gopact.ChannelPayload) error {
	messages, err := normalizeMessages(payload)
	if err != nil {
		return err
	}
	return r.Apply(messages...)
}

// Apply applies A2UI messages in order.
func (r *Renderer) Apply(messages ...Message) error {
	if r == nil {
		return errors.New("a2ui: renderer is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, message := range messages {
		if err := r.applyLocked(message); err != nil {
			return err
		}
	}
	return nil
}

// Surface returns a copy of the current surface snapshot.
func (r *Renderer) Surface(id string) (SurfaceSnapshot, bool) {
	if r == nil {
		return SurfaceSnapshot{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	surface, ok := r.surfaces[id]
	if !ok {
		return SurfaceSnapshot{}, false
	}
	return copySurfaceSnapshot(surface), true
}

// Surfaces returns copies of all currently materialized surfaces.
func (r *Renderer) Surfaces() []SurfaceSnapshot {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SurfaceSnapshot, 0, len(r.surfaces))
	for _, surface := range r.surfaces {
		out = append(out, copySurfaceSnapshot(surface))
	}
	return out
}

func (r *Renderer) applyLocked(message Message) error {
	if message.CreateSurface != nil {
		return r.createSurfaceLocked(*message.CreateSurface)
	}
	if message.UpdateComponents != nil {
		return r.updateComponentsLocked(*message.UpdateComponents)
	}
	if message.UpdateDataModel != nil {
		return r.updateDataModelLocked(*message.UpdateDataModel)
	}
	if message.DeleteSurface != nil {
		delete(r.surfaces, message.DeleteSurface.SurfaceID)
	}
	return nil
}

func (r *Renderer) createSurfaceLocked(create CreateSurface) error {
	if strings.TrimSpace(create.SurfaceID) == "" {
		return &ValidationError{CatalogID: create.CatalogID, Message: "surface id is required"}
	}
	if r.registry != nil {
		if _, ok := r.registry.Catalog(create.CatalogID); !ok {
			return &ValidationError{
				CatalogID: create.CatalogID,
				SurfaceID: create.SurfaceID,
				Message:   "catalog is not registered",
			}
		}
	}
	r.surfaces[create.SurfaceID] = SurfaceSnapshot{
		ID:            create.SurfaceID,
		CatalogID:     create.CatalogID,
		SendDataModel: create.SendDataModel,
		Components:    make(map[string]Component),
	}
	return nil
}

func (r *Renderer) updateComponentsLocked(update UpdateComponents) error {
	surface, ok := r.surfaces[update.SurfaceID]
	if !ok {
		return &ValidationError{SurfaceID: update.SurfaceID, Message: "surface is unknown"}
	}
	components := copyComponentMap(surface.Components)
	for _, component := range update.Components {
		components[component.ID] = copyComponent(component)
	}
	if r.registry != nil {
		catalog, ok := r.registry.Catalog(surface.CatalogID)
		if !ok {
			return &ValidationError{
				CatalogID: surface.CatalogID,
				SurfaceID: surface.ID,
				Message:   "catalog is not registered",
			}
		}
		if err := validateComponents(catalog, surface.ID, componentMapValues(components), nil); err != nil {
			return err
		}
	}
	surface.Components = components
	r.surfaces[surface.ID] = surface
	return nil
}

func (r *Renderer) updateDataModelLocked(update UpdateDataModel) error {
	surface, ok := r.surfaces[update.SurfaceID]
	if !ok {
		return &ValidationError{SurfaceID: update.SurfaceID, Message: "surface is unknown"}
	}
	value := copyJSONLikeValue(update.Value)
	model, err := updateDataModelPath(surface.DataModel, update.Path, value)
	if err != nil {
		return err
	}
	surface.DataModel = model
	r.surfaces[surface.ID] = surface
	return nil
}

type transferConfig struct {
	catalogID               string
	catalogRegistry         *CatalogRegistry
	clientSupportedCatalogs []string
	sendDataModel           bool
	validator               *Validator
}

// TransferOption configures an A2UI transfer.
type TransferOption func(*transferConfig)

// WithCatalogID sets the A2UI component catalog used by createSurface.
func WithCatalogID(catalogID string) TransferOption {
	return func(cfg *transferConfig) {
		if strings.TrimSpace(catalogID) != "" {
			cfg.catalogID = catalogID
		}
	}
}

// WithCatalogRegistry sets the local catalog registry used to negotiate a
// client-supported A2UI catalog.
func WithCatalogRegistry(registry *CatalogRegistry) TransferOption {
	return func(cfg *transferConfig) {
		cfg.catalogRegistry = registry
	}
}

// WithClientSupportedCatalogs sets the renderer catalog preference order.
func WithClientSupportedCatalogs(catalogIDs ...string) TransferOption {
	return func(cfg *transferConfig) {
		cfg.clientSupportedCatalogs = cleanCatalogIDs(catalogIDs)
	}
}

// WithSendDataModel controls createSurface.sendDataModel.
func WithSendDataModel(send bool) TransferOption {
	return func(cfg *transferConfig) {
		cfg.sendDataModel = send
	}
}

// WithValidator validates generated A2UI messages before returning a payload.
func WithValidator(validator *Validator) TransferOption {
	return func(cfg *transferConfig) {
		cfg.validator = validator
	}
}

// Transfer converts gopact surface messages into A2UI payloads.
type Transfer struct {
	catalogID               string
	catalogRegistry         *CatalogRegistry
	clientSupportedCatalogs []string
	sendDataModel           bool
	validator               *Validator
}

var _ gopact.Transfer = (*Transfer)(nil)

// NewTransfer creates an A2UI transfer.
func NewTransfer(opts ...TransferOption) *Transfer {
	cfg := transferConfig{catalogID: defaultCatalogID}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Transfer{
		catalogID:               cfg.catalogID,
		catalogRegistry:         cfg.catalogRegistry,
		clientSupportedCatalogs: append([]string(nil), cfg.clientSupportedCatalogs...),
		sendDataModel:           cfg.sendDataModel,
		validator:               cfg.validator,
	}
}

// Name returns the transfer name.
func (t *Transfer) Name() string {
	return "a2ui"
}

// Supports reports whether target is the A2UI channel target.
func (t *Transfer) Supports(target gopact.ChannelTarget) bool {
	return target == "" || target == Target
}

// Convert converts one surface message into a typed A2UI payload.
func (t *Transfer) Convert(ctx context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.ChannelPayload{}, err
	}

	surfaceID := surfaceID(msg)
	catalogID := t.catalogIDForMessage()
	components := componentsFromMessage(msg)
	dataModel := dataModelFromMessage(msg)
	payload := Payload{
		MIMEType:  MIMEType,
		SurfaceID: surfaceID,
		Version:   Version,
		Messages: []Message{
			{
				Version: Version,
				CreateSurface: &CreateSurface{
					SurfaceID:     surfaceID,
					CatalogID:     catalogID,
					SendDataModel: t.sendDataModel,
				},
			},
			{
				Version: Version,
				UpdateComponents: &UpdateComponents{
					SurfaceID:  surfaceID,
					Components: components,
				},
			},
			{
				Version: Version,
				UpdateDataModel: &UpdateDataModel{
					SurfaceID: surfaceID,
					Path:      "/",
					Value:     dataModel,
				},
			},
		},
		IDs:         msg.IDs,
		SourceEvent: msg.SourceEvent,
		Metadata:    copyAnyMap(msg.Metadata),
		CreatedAt:   msg.CreatedAt,
	}
	if payload.Metadata == nil {
		payload.Metadata = make(map[string]any)
	}
	payload.Metadata["catalog_id"] = catalogID
	metadata := map[string]any{
		"surface_id":   surfaceID,
		"surface_type": string(msg.Type),
		"source_event": msg.SourceEvent,
		"mime_type":    MIMEType,
		"catalog_id":   catalogID,
	}
	if err := t.validator.ValidateMessages(payload.Messages); err != nil {
		return gopact.ChannelPayload{}, err
	}
	return gopact.ChannelPayload{
		Target:   Target,
		Data:     payload,
		Metadata: metadata,
	}, nil
}

func (t *Transfer) catalogIDForMessage() string {
	if t != nil && t.catalogRegistry != nil && len(t.clientSupportedCatalogs) > 0 {
		if catalog, ok := t.catalogRegistry.Select(t.clientSupportedCatalogs); ok {
			return catalog.ID
		}
	}
	if t != nil && strings.TrimSpace(t.catalogID) != "" {
		return t.catalogID
	}
	return defaultCatalogID
}

func cleanCatalogIDs(catalogIDs []string) []string {
	out := make([]string, 0, len(catalogIDs))
	for _, catalogID := range catalogIDs {
		catalogID = strings.TrimSpace(catalogID)
		if catalogID != "" {
			out = append(out, catalogID)
		}
	}
	return out
}

// ChannelEventFromAction converts an A2UI renderer action into a gopact channel event.
func ChannelEventFromAction(id string, action Action, createdAt time.Time) gopact.ChannelEvent {
	actionID := stringFromContext(action.Context, "action_id")
	if actionID == "" {
		actionID = action.Name
	}
	actionType := gopact.SurfaceActionType(stringFromContext(action.Context, "action_type"))
	if actionType == "" {
		actionType = gopact.SurfaceActionSubmit
	}
	payload := action.Context["payload"]
	ids := runtimeIDsFromValue(action.Context["ids"])
	if createdAt.IsZero() {
		createdAt = action.Timestamp
	}

	actionMetadata := copyAnyMap(action.Context)
	eventMetadata := copyAnyMap(action.Metadata)
	if eventMetadata == nil {
		eventMetadata = make(map[string]any)
	}
	eventMetadata["a2ui_surface_id"] = action.SurfaceID
	eventMetadata["a2ui_source_component_id"] = action.SourceComponentID
	eventMetadata["a2ui_action_name"] = action.Name
	if !action.Timestamp.IsZero() {
		eventMetadata["a2ui_action_timestamp"] = action.Timestamp
	}

	return gopact.ChannelEvent{
		ID:      id,
		Channel: Target,
		Type:    gopact.ChannelEventAction,
		IDs:     ids,
		Action: gopact.SurfaceAction{
			ID:          actionID,
			Type:        actionType,
			IDs:         ids,
			InterruptID: stringFromContext(action.Context, "interrupt_id"),
			CallID:      stringFromContext(action.Context, "call_id"),
			Payload:     payload,
			Metadata:    actionMetadata,
		},
		Payload:   payload,
		Metadata:  eventMetadata,
		CreatedAt: createdAt,
	}
}

// Channel writes outbound A2UI messages as JSON Lines and can decode inbound
// renderer actions from a host-owned reader.
type Channel struct {
	mu           sync.Mutex
	writer       io.Writer
	actionReader io.Reader
	history      *History
	validator    *Validator
	closed       bool
}

var _ gopact.Channel = (*Channel)(nil)

// ChannelOption configures an A2UI channel.
type ChannelOption func(*Channel)

// WithActionReader sets the JSON/JSONL action reader used by Events.
func WithActionReader(reader io.Reader) ChannelOption {
	return func(c *Channel) {
		c.actionReader = reader
	}
}

// WithHistory records successfully sent messages for later JSONL replay.
func WithHistory(history *History) ChannelOption {
	return func(c *Channel) {
		c.history = history
	}
}

// WithChannelValidator validates outbound payload messages before writing them.
func WithChannelValidator(validator *Validator) ChannelOption {
	return func(c *Channel) {
		c.validator = validator
	}
}

// NewChannel creates an A2UI JSONL channel backed by a host-owned writer.
func NewChannel(writer io.Writer, opts ...ChannelOption) (*Channel, error) {
	if writer == nil {
		return nil, ErrWriterRequired
	}
	channel := &Channel{writer: writer}
	for _, opt := range opts {
		if opt != nil {
			opt(channel)
		}
	}
	return channel, nil
}

// Name returns the channel name.
func (c *Channel) Name() string {
	return "a2ui"
}

// Send writes each A2UI message as one JSONL frame.
func (c *Channel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if c == nil || c.writer == nil {
		return ErrWriterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	messages, err := normalizeMessages(payload)
	if err != nil {
		return err
	}
	if err := c.validator.ValidateMessages(messages); err != nil {
		return err
	}
	for _, message := range messages {
		if err := writeMessage(c.writer, message); err != nil {
			return err
		}
	}
	if flusher, ok := c.writer.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return fmt.Errorf("a2ui: flush messages: %w", err)
		}
	}
	if c.history != nil {
		c.history.recordMessages(messages)
	}
	return nil
}

// Events decodes inbound A2UI action frames from the configured action reader.
func (c *Channel) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if err := ctx.Err(); err != nil {
			yield(gopact.ChannelEvent{}, err)
			return
		}
		if c == nil {
			return
		}
		c.mu.Lock()
		reader := c.actionReader
		closed := c.closed
		c.mu.Unlock()
		if closed || reader == nil {
			return
		}

		decoder := json.NewDecoder(reader)
		for {
			if err := ctx.Err(); err != nil {
				yield(gopact.ChannelEvent{}, err)
				return
			}
			var frame ActionFrame
			if err := decoder.Decode(&frame); err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				yield(gopact.ChannelEvent{}, fmt.Errorf("a2ui: decode action frame: %w", err))
				return
			}
			event := ChannelEventFromAction(actionFrameEventID(frame), frame.Action, time.Time{})
			if !yield(event, nil) {
				return
			}
		}
	}
}

// Close closes the channel. The injected writer and reader lifecycles remain host-owned.
func (c *Channel) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func normalizeMessages(payload gopact.ChannelPayload) ([]Message, error) {
	switch data := payload.Data.(type) {
	case Payload:
		return append([]Message(nil), data.Messages...), nil
	case *Payload:
		if data == nil {
			return nil, ErrUnsupportedPayload
		}
		return append([]Message(nil), data.Messages...), nil
	case Message:
		return []Message{data}, nil
	case *Message:
		if data == nil {
			return nil, ErrUnsupportedPayload
		}
		return []Message{*data}, nil
	case []Message:
		return append([]Message(nil), data...), nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedPayload, payload.Data)
	}
}

// History stores outbound A2UI messages so a host can replay a renderer surface
// after reconnect or resume.
type History struct {
	mu       sync.Mutex
	limit    int
	messages []Message
}

// HistoryOption configures A2UI message history.
type HistoryOption func(*History)

// WithHistoryLimit keeps only the most recent limit messages. A non-positive
// limit keeps all messages.
func WithHistoryLimit(limit int) HistoryOption {
	return func(h *History) {
		h.limit = limit
	}
}

// NewHistory creates an A2UI message history.
func NewHistory(opts ...HistoryOption) *History {
	history := &History{}
	for _, opt := range opts {
		if opt != nil {
			opt(history)
		}
	}
	return history
}

// Record appends the messages from payload to history.
func (h *History) Record(payload gopact.ChannelPayload) error {
	messages, err := normalizeMessages(payload)
	if err != nil {
		return err
	}
	h.recordMessages(messages)
	return nil
}

// Messages returns a snapshot of recorded messages.
func (h *History) Messages() []Message {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return copyMessages(h.messages)
}

// Replay writes the recorded message snapshot as JSON Lines.
func (h *History) Replay(ctx context.Context, writer io.Writer) error {
	if writer == nil {
		return ErrWriterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, message := range h.Messages() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeMessage(writer, message); err != nil {
			return err
		}
	}
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return fmt.Errorf("a2ui: flush history: %w", err)
		}
	}
	return nil
}

func (h *History) recordMessages(messages []Message) {
	if h == nil || len(messages) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, copyMessages(messages)...)
	if h.limit > 0 && len(h.messages) > h.limit {
		h.messages = copyMessages(h.messages[len(h.messages)-h.limit:])
	}
}

func writeMessage(writer io.Writer, message Message) error {
	raw, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("a2ui: encode message: %w", err)
	}
	if _, err := writer.Write(raw); err != nil {
		return fmt.Errorf("a2ui: write message: %w", err)
	}
	if _, err := io.WriteString(writer, "\n"); err != nil {
		return fmt.Errorf("a2ui: write message newline: %w", err)
	}
	return nil
}

func actionFrameEventID(frame ActionFrame) string {
	if value, ok := frame.Action.Context["event_id"].(string); ok && value != "" {
		return value
	}
	if frame.Action.Name != "" {
		return frame.Action.Name
	}
	return "a2ui-action"
}

func copyMessages(in []Message) []Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]Message, len(in))
	for i, message := range in {
		out[i] = copyMessage(message)
	}
	return out
}

func copyCatalog(in Catalog) Catalog {
	out := Catalog{
		ID:         in.ID,
		Components: make(map[string]ComponentSchema, len(in.Components)),
	}
	for name, schema := range in.Components {
		schema.Schema = gopact.JSONSchema(copyJSONLikeMap(schema.Schema))
		out.Components[name] = schema
	}
	return out
}

func validateComponents(catalog Catalog, surfaceID string, components []Component, schemaValidator gopact.JSONSchemaValidator) error {
	componentIDs := make(map[string]struct{}, len(components))
	for _, component := range components {
		if strings.TrimSpace(component.ID) == "" {
			return &ValidationError{
				CatalogID: catalog.ID,
				SurfaceID: surfaceID,
				Component: component.Component,
				Message:   "component id is required",
			}
		}
		if _, ok := catalog.Components[component.Component]; !ok {
			return &ValidationError{
				CatalogID:   catalog.ID,
				SurfaceID:   surfaceID,
				ComponentID: component.ID,
				Component:   component.Component,
				Message:     "component is not in catalog",
			}
		}
		componentIDs[component.ID] = struct{}{}
	}
	for _, component := range components {
		if component.Child != "" {
			if _, ok := componentIDs[component.Child]; !ok {
				return &ValidationError{
					CatalogID:   catalog.ID,
					SurfaceID:   surfaceID,
					ComponentID: component.ID,
					Component:   component.Component,
					Message:     "child reference is unknown",
				}
			}
		}
		for _, childID := range component.Children {
			if _, ok := componentIDs[childID]; !ok {
				return &ValidationError{
					CatalogID:   catalog.ID,
					SurfaceID:   surfaceID,
					ComponentID: component.ID,
					Component:   component.Component,
					Message:     "child reference is unknown",
				}
			}
		}
	}
	for _, component := range components {
		schema := catalog.Components[component.Component]
		if len(schema.Schema) == 0 {
			continue
		}
		if err := validateComponentJSONSchema(schema.Schema, component, schemaValidator); err != nil {
			return &ValidationError{
				CatalogID:   catalog.ID,
				SurfaceID:   surfaceID,
				ComponentID: component.ID,
				Component:   component.Component,
				Message:     err.Error(),
			}
		}
	}
	return nil
}

func validateComponentJSONSchema(schema gopact.JSONSchema, component Component, validator gopact.JSONSchemaValidator) error {
	return gopact.ValidateJSONSchemaValueWith(context.TODO(), validator, schema, component)
}

func copyMessage(in Message) Message {
	out := in
	if in.CreateSurface != nil {
		value := *in.CreateSurface
		out.CreateSurface = &value
	}
	if in.UpdateComponents != nil {
		value := *in.UpdateComponents
		value.Components = copyComponents(in.UpdateComponents.Components)
		out.UpdateComponents = &value
	}
	if in.UpdateDataModel != nil {
		value := *in.UpdateDataModel
		value.Value = copyJSONLikeValue(in.UpdateDataModel.Value)
		out.UpdateDataModel = &value
	}
	if in.DeleteSurface != nil {
		value := *in.DeleteSurface
		out.DeleteSurface = &value
	}
	return out
}

func copyComponents(in []Component) []Component {
	if len(in) == 0 {
		return nil
	}
	out := make([]Component, len(in))
	for i, component := range in {
		out[i] = copyComponent(component)
	}
	return out
}

func copyComponent(in Component) Component {
	out := in
	out.Text = copyJSONLikeValue(in.Text)
	out.Children = append([]string(nil), in.Children...)
	out.Metadata = copyJSONLikeMap(in.Metadata)
	if in.Action != nil {
		action := *in.Action
		if in.Action.Event != nil {
			event := *in.Action.Event
			event.Context = copyJSONLikeMap(in.Action.Event.Context)
			action.Event = &event
		}
		out.Action = &action
	}
	return out
}

func copyComponentMap(in map[string]Component) map[string]Component {
	if len(in) == 0 {
		return make(map[string]Component)
	}
	out := make(map[string]Component, len(in))
	for id, component := range in {
		out[id] = copyComponent(component)
	}
	return out
}

func componentMapValues(in map[string]Component) []Component {
	if len(in) == 0 {
		return nil
	}
	out := make([]Component, 0, len(in))
	for _, component := range in {
		out = append(out, copyComponent(component))
	}
	return out
}

func copySurfaceSnapshot(in SurfaceSnapshot) SurfaceSnapshot {
	out := in
	out.Components = copyComponentMap(in.Components)
	out.DataModel = copyJSONLikeValue(in.DataModel)
	return out
}

func updateDataModelPath(model any, path string, value any) (any, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return value, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, &ValidationError{Message: "data model path must be absolute"}
	}
	root, ok := copyJSONLikeValue(model).(map[string]any)
	if !ok || root == nil {
		root = make(map[string]any)
	}
	current := root
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		key := decodePathSegment(part)
		if key == "" {
			return nil, &ValidationError{Message: "data model path segment is required"}
		}
		if i == len(parts)-1 {
			current[key] = value
			return root, nil
		}
		next, ok := current[key].(map[string]any)
		if !ok || next == nil {
			next = make(map[string]any)
			current[key] = next
		}
		current = next
	}
	return root, nil
}

func decodePathSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~1", "/")
	segment = strings.ReplaceAll(segment, "~0", "~")
	return segment
}

func copyJSONLikeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return copyJSONLikeMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = copyJSONLikeValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case []gopact.SurfacePart:
		return copySurfaceParts(typed)
	case []gopact.SurfaceAction:
		return copySurfaceActions(typed)
	case []gopact.ArtifactRef:
		return copyArtifacts(typed)
	default:
		return value
	}
}

func copyJSONLikeMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = copyJSONLikeValue(value)
	}
	return out
}

func componentsFromMessage(msg gopact.SurfaceMessage) []Component {
	text := renderParts(msg.Parts)
	components := []Component{{
		ID:        "root",
		Component: "Column",
	}}
	children := make([]string, 0, 3)

	if strings.TrimSpace(text) != "" {
		components = append(components, Component{
			ID:        "body",
			Component: "Text",
			Text:      map[string]any{"path": "/text"},
		})
		children = append(children, "body")
	}

	if len(msg.Artifacts) > 0 {
		artifactChildren := make([]string, 0, len(msg.Artifacts))
		for i, artifact := range msg.Artifacts {
			id := componentID("artifact", i)
			components = append(components, Component{
				ID:        id,
				Component: "Text",
				Text:      artifactLabel(artifact),
				Metadata: map[string]any{
					"artifact_id":   artifact.ID,
					"artifact_uri":  artifact.URI,
					"artifact_mime": artifact.MIMEType,
				},
			})
			artifactChildren = append(artifactChildren, id)
		}
		components = append(components, Component{
			ID:        "artifacts",
			Component: "Column",
			Children:  artifactChildren,
		})
		children = append(children, "artifacts")
	}

	if len(msg.Actions) > 0 {
		actionChildren := make([]string, 0, len(msg.Actions))
		for i, action := range msg.Actions {
			labelID := componentID("action", i) + "_label"
			buttonID := componentID("action", i)
			components = append(components,
				Component{
					ID:        labelID,
					Component: "Text",
					Text:      actionLabel(action),
				},
				Component{
					ID:        buttonID,
					Component: "Button",
					Child:     labelID,
					Action: &ActionBinding{Event: &ActionEvent{
						Name:    actionEventName(action),
						Context: actionContext(action),
					}},
				},
			)
			actionChildren = append(actionChildren, buttonID)
		}
		components = append(components, Component{
			ID:        "actions",
			Component: "Row",
			Children:  actionChildren,
		})
		children = append(children, "actions")
	}

	components[0].Children = children
	return components
}

func dataModelFromMessage(msg gopact.SurfaceMessage) map[string]any {
	return map[string]any{
		"message_id":   msg.ID,
		"type":         string(msg.Type),
		"text":         renderParts(msg.Parts),
		"ids":          msg.IDs,
		"parts":        copySurfaceParts(msg.Parts),
		"actions":      copySurfaceActions(msg.Actions),
		"artifacts":    copyArtifacts(msg.Artifacts),
		"source_event": msg.SourceEvent,
		"metadata":     copyAnyMap(msg.Metadata),
		"created_at":   msg.CreatedAt,
	}
}

func surfaceID(msg gopact.SurfaceMessage) string {
	if value, ok := msg.Metadata["a2ui_surface_id"].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	if msg.Target.SessionID != "" {
		return msg.Target.SessionID
	}
	if msg.ID != "" {
		return msg.ID
	}
	if msg.IDs.SessionID != "" {
		return msg.IDs.SessionID
	}
	if msg.IDs.ThreadID != "" {
		return msg.IDs.ThreadID
	}
	if msg.IDs.RunID != "" {
		return msg.IDs.RunID
	}
	return "surface"
}

func renderParts(parts []gopact.SurfacePart) string {
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		text := part.Text
		if text == "" {
			text = part.Name
		}
		if text == "" {
			text = part.URI
		}
		if strings.TrimSpace(text) != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func artifactLabel(artifact gopact.ArtifactRef) string {
	label := artifact.Name
	if label == "" {
		label = artifact.ID
	}
	if label == "" {
		label = artifact.URI
	}
	if artifact.URI != "" && artifact.URI != label {
		return label + " (" + artifact.URI + ")"
	}
	return label
}

func actionLabel(action gopact.SurfaceAction) string {
	if action.Label != "" {
		return action.Label
	}
	if action.Type != "" {
		return string(action.Type)
	}
	return action.ID
}

func actionEventName(action gopact.SurfaceAction) string {
	if action.ID != "" {
		return action.ID
	}
	if action.Type != "" {
		return string(action.Type)
	}
	return "action"
}

func actionContext(action gopact.SurfaceAction) map[string]any {
	return map[string]any{
		"action_id":    action.ID,
		"action_type":  string(action.Type),
		"interrupt_id": action.InterruptID,
		"call_id":      action.CallID,
		"ids":          action.IDs,
		"payload":      action.Payload,
		"metadata":     copyAnyMap(action.Metadata),
	}
}

func componentID(prefix string, index int) string {
	return prefix + "_" + strconv.Itoa(index)
}

func stringFromContext(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func runtimeIDsFromValue(value any) gopact.RuntimeIDs {
	switch ids := value.(type) {
	case gopact.RuntimeIDs:
		return ids
	case map[string]string:
		return gopact.RuntimeIDs{
			UserID:       ids["user_id"],
			SessionID:    ids["session_id"],
			ThreadID:     ids["thread_id"],
			RunID:        ids["run_id"],
			AgentID:      ids["agent_id"],
			AppID:        ids["app_id"],
			CallID:       ids["call_id"],
			ParentCallID: ids["parent_call_id"],
			TraceID:      ids["trace_id"],
		}
	case map[string]any:
		return gopact.RuntimeIDs{
			UserID:       stringFromAnyMap(ids, "user_id"),
			SessionID:    stringFromAnyMap(ids, "session_id"),
			ThreadID:     stringFromAnyMap(ids, "thread_id"),
			RunID:        stringFromAnyMap(ids, "run_id"),
			AgentID:      stringFromAnyMap(ids, "agent_id"),
			AppID:        stringFromAnyMap(ids, "app_id"),
			CallID:       stringFromAnyMap(ids, "call_id"),
			ParentCallID: stringFromAnyMap(ids, "parent_call_id"),
			TraceID:      stringFromAnyMap(ids, "trace_id"),
		}
	default:
		return gopact.RuntimeIDs{}
	}
}

func stringFromAnyMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func copySurfaceParts(in []gopact.SurfacePart) []gopact.SurfacePart {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.SurfacePart, len(in))
	for i, part := range in {
		out[i] = part
		out[i].Metadata = copyAnyMap(part.Metadata)
	}
	return out
}

func copySurfaceActions(in []gopact.SurfaceAction) []gopact.SurfaceAction {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.SurfaceAction, len(in))
	for i, action := range in {
		out[i] = action
		out[i].Metadata = copyAnyMap(action.Metadata)
	}
	return out
}

func copyArtifacts(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, artifact := range in {
		out[i] = artifact
		out[i].Metadata = copyAnyMap(artifact.Metadata)
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
