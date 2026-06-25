package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	defaultCallbackBuffer       = 64
	defaultCallbackMaxBodyBytes = 1 << 20
)

var (
	ErrCallbackBufferRequired = errors.New("lark: callback buffer must be positive")
	ErrCallbackQueueFull      = errors.New("lark: callback event queue full")
	ErrCallbackClosed         = errors.New("lark: callback source closed")
	ErrCallbackUnauthorized   = errors.New("lark: callback unauthorized")
	ErrActionUnauthorized     = errors.New("lark: action unauthorized")
	ErrUnsupportedCallback    = errors.New("lark: unsupported callback")
)

// CallbackRequest is the host-verifiable raw Lark callback request.
type CallbackRequest struct {
	Method     string
	Path       string
	Header     http.Header
	Body       []byte
	ReceivedAt time.Time
}

// CallbackVerifier verifies the raw Lark HTTP callback before it is decoded.
type CallbackVerifier interface {
	VerifyCallback(ctx context.Context, request CallbackRequest) error
}

// CallbackVerifierFunc adapts a function into a CallbackVerifier.
type CallbackVerifierFunc func(ctx context.Context, request CallbackRequest) error

// VerifyCallback calls f.
func (f CallbackVerifierFunc) VerifyCallback(ctx context.Context, request CallbackRequest) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	request.Header = copyHeader(request.Header)
	request.Body = append([]byte(nil), request.Body...)
	return f(ctx, request)
}

// ActionVerifier verifies an inbound card action value after callback decoding.
type ActionVerifier interface {
	VerifyAction(ctx context.Context, value ActionValue) error
}

// ActionVerifierFunc adapts a function into an ActionVerifier.
type ActionVerifierFunc func(ctx context.Context, value ActionValue) error

// VerifyAction calls f.
func (f ActionVerifierFunc) VerifyAction(ctx context.Context, value ActionValue) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f(ctx, copyActionValue(value))
}

type callbackConfig struct {
	buffer       int
	maxBodyBytes int64
	now          func() time.Time
	verifier     CallbackVerifier
	action       ActionVerifier
}

// CallbackOption configures a Lark callback source.
type CallbackOption func(*callbackConfig) error

// WithCallbackBuffer sets the inbound callback event queue size.
func WithCallbackBuffer(size int) CallbackOption {
	return func(cfg *callbackConfig) error {
		if size <= 0 {
			return ErrCallbackBufferRequired
		}
		cfg.buffer = size
		return nil
	}
}

// WithCallbackMaxBodyBytes sets the maximum callback request body size.
func WithCallbackMaxBodyBytes(size int64) CallbackOption {
	return func(cfg *callbackConfig) error {
		if size <= 0 {
			return errors.New("lark: callback max body bytes must be positive")
		}
		cfg.maxBodyBytes = size
		return nil
	}
}

// WithCallbackNow injects the clock used for fallback callback timestamps.
func WithCallbackNow(now func() time.Time) CallbackOption {
	return func(cfg *callbackConfig) error {
		if now != nil {
			cfg.now = now
		}
		return nil
	}
}

// WithCallbackVerifier verifies raw callback requests before decoding.
func WithCallbackVerifier(verifier CallbackVerifier) CallbackOption {
	return func(cfg *callbackConfig) error {
		cfg.verifier = verifier
		return nil
	}
}

// WithActionVerifier verifies decoded action values before publishing events.
func WithActionVerifier(verifier ActionVerifier) CallbackOption {
	return func(cfg *callbackConfig) error {
		cfg.action = verifier
		return nil
	}
}

// CallbackSource accepts Lark callbacks and exposes normalized channel events.
type CallbackSource struct {
	mu           sync.Mutex
	events       chan gopact.ChannelEvent
	maxBodyBytes int64
	now          func() time.Time
	verifier     CallbackVerifier
	action       ActionVerifier
	closed       bool
	closeOnce    sync.Once
}

var _ http.Handler = (*CallbackSource)(nil)

// NewCallbackSource creates a host-owned Lark callback source.
func NewCallbackSource(opts ...CallbackOption) (*CallbackSource, error) {
	cfg := callbackConfig{
		buffer:       defaultCallbackBuffer,
		maxBodyBytes: defaultCallbackMaxBodyBytes,
		now:          time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	return &CallbackSource{
		events:       make(chan gopact.ChannelEvent, cfg.buffer),
		maxBodyBytes: cfg.maxBodyBytes,
		now:          cfg.now,
		verifier:     cfg.verifier,
		action:       cfg.action,
	}, nil
}

// ServeHTTP handles Lark URL verification and card action callbacks.
func (s *CallbackSource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "callback source is nil", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	receivedAt := now()
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	defer func() {
		_ = r.Body.Close()
	}()
	reader := io.Reader(r.Body)
	if s.maxBodyBytes > 0 {
		reader = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		http.Error(w, fmt.Sprintf("read callback: %v", err), http.StatusBadRequest)
		return
	}
	request := CallbackRequest{
		Method:     r.Method,
		Path:       r.URL.Path,
		Header:     copyHeader(r.Header),
		Body:       append([]byte(nil), body...),
		ReceivedAt: receivedAt,
	}
	if s.verifier != nil {
		if err := s.verifier.VerifyCallback(r.Context(), request); err != nil {
			http.Error(w, fmt.Errorf("%w: %v", ErrCallbackUnauthorized, err).Error(), httpStatusForError(err, http.StatusUnauthorized))
			return
		}
	}

	var envelope callbackEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		http.Error(w, fmt.Sprintf("decode callback: %v", err), http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "decode callback: trailing data", http.StatusBadRequest)
		return
	}
	if envelope.isURLVerification() {
		writeJSON(w, http.StatusOK, map[string]any{"challenge": envelope.Challenge})
		return
	}

	event, err := s.eventFromEnvelope(r.Context(), envelope, receivedAt)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrActionUnauthorized) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	if err := s.enqueue(event); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	} else {
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
	}
}

// Events returns normalized inbound Lark callback events.
func (s *CallbackSource) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if s == nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				yield(gopact.ChannelEvent{}, ctx.Err())
				return
			case event, ok := <-s.events:
				if !ok {
					return
				}
				if !yield(copyChannelEvent(event), nil) {
					return
				}
			}
		}
	}
}

// Close closes the callback event stream.
func (s *CallbackSource) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closed = true
		close(s.events)
	})
}

func (s *CallbackSource) enqueue(event gopact.ChannelEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrCallbackClosed
	}
	select {
	case s.events <- copyChannelEvent(event):
		return nil
	default:
		return ErrCallbackQueueFull
	}
}

func (s *CallbackSource) eventFromEnvelope(ctx context.Context, envelope callbackEnvelope, fallback time.Time) (gopact.ChannelEvent, error) {
	raw := envelope.actionValue()
	if len(raw) == 0 {
		return gopact.ChannelEvent{}, ErrUnsupportedCallback
	}
	value, err := decodeActionValue(raw)
	if err != nil {
		return gopact.ChannelEvent{}, fmt.Errorf("lark: decode action value: %w", err)
	}
	if s.action != nil {
		if err := s.action.VerifyAction(ctx, value); err != nil {
			return gopact.ChannelEvent{}, fmt.Errorf("%w: %v", ErrActionUnauthorized, err)
		}
	}
	createdAt := parseLarkTime(envelope.Header.CreateTime, fallback)
	event := ChannelEventFromActionValue(envelope.eventID(value, fallback), value, createdAt)
	enrichEventFromEnvelope(&event, envelope, value)
	return event, nil
}

type callbackEnvelope struct {
	Type      string           `json:"type,omitempty"`
	Challenge string           `json:"challenge,omitempty"`
	Schema    string           `json:"schema,omitempty"`
	Header    callbackHeader   `json:"header,omitempty"`
	Event     callbackEvent    `json:"event,omitempty"`
	Action    callbackAction   `json:"action,omitempty"`
	EventID   string           `json:"event_id,omitempty"`
	Operator  callbackOperator `json:"operator,omitempty"`
	Value     json.RawMessage  `json:"value,omitempty"`
}

type callbackHeader struct {
	EventID    string          `json:"event_id,omitempty"`
	EventType  string          `json:"event_type,omitempty"`
	CreateTime json.RawMessage `json:"create_time,omitempty"`
}

type callbackEvent struct {
	Operator callbackOperator `json:"operator,omitempty"`
	Action   callbackAction   `json:"action,omitempty"`
}

type callbackAction struct {
	Value json.RawMessage `json:"value,omitempty"`
}

type callbackOperator struct {
	OpenID  string `json:"open_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	UnionID string `json:"union_id,omitempty"`
}

func (e callbackEnvelope) isURLVerification() bool {
	return e.Type == "url_verification" && e.Challenge != ""
}

func (e callbackEnvelope) actionValue() json.RawMessage {
	if len(e.Event.Action.Value) > 0 {
		return e.Event.Action.Value
	}
	if len(e.Action.Value) > 0 {
		return e.Action.Value
	}
	if len(e.Value) > 0 {
		return e.Value
	}
	return nil
}

func (e callbackEnvelope) eventID(value ActionValue, fallback time.Time) string {
	if e.Header.EventID != "" {
		return e.Header.EventID
	}
	if e.EventID != "" {
		return e.EventID
	}
	if value.CallID != "" {
		return "lark-callback-" + value.CallID
	}
	if value.ActionID != "" {
		return "lark-callback-" + value.ActionID
	}
	if fallback.IsZero() {
		fallback = time.Now()
	}
	return fmt.Sprintf("lark-callback-%d", fallback.UnixNano())
}

func decodeActionValue(raw json.RawMessage) (ActionValue, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ActionValue{}, ErrUnsupportedCallback
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return ActionValue{}, err
		}
		raw = []byte(strings.TrimSpace(text))
	}
	var value ActionValue
	if err := json.Unmarshal(raw, &value); err != nil {
		return ActionValue{}, err
	}
	return copyActionValue(value), nil
}

func enrichEventFromEnvelope(event *gopact.ChannelEvent, envelope callbackEnvelope, value ActionValue) {
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	if envelope.Schema != "" {
		event.Metadata["lark_schema"] = envelope.Schema
	}
	if envelope.Header.EventID != "" {
		event.Metadata["lark_event_id"] = envelope.Header.EventID
	}
	if envelope.Header.EventType != "" {
		event.Metadata["lark_event_type"] = envelope.Header.EventType
	}
	operator := envelope.Event.Operator
	if operator == (callbackOperator{}) {
		operator = envelope.Operator
	}
	if operator.OpenID != "" {
		event.Metadata["lark_open_id"] = operator.OpenID
	}
	if operator.UserID != "" {
		event.Metadata["lark_user_id"] = operator.UserID
		if event.IDs.UserID == "" {
			event.IDs.UserID = operator.UserID
		}
	}
	if operator.UnionID != "" {
		event.Metadata["lark_union_id"] = operator.UnionID
	}
	if event.IDs.CallID == "" && value.CallID != "" {
		event.IDs.CallID = value.CallID
	}
	event.Action.IDs = event.IDs
}

func parseLarkTime(raw json.RawMessage, fallback time.Time) time.Time {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	text := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return fallback
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fallback
	}
	switch {
	case n > 1_000_000_000_000:
		return time.UnixMilli(n)
	case n > 1_000_000_000:
		return time.Unix(n, 0)
	default:
		return fallback
	}
}

func httpStatusForError(err error, fallback int) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusRequestTimeout
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func copyActionValue(in ActionValue) ActionValue {
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyHeader(in http.Header) http.Header {
	if in == nil {
		return nil
	}
	out := make(http.Header, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
