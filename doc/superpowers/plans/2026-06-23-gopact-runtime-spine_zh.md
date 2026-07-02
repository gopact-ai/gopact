# Gopact Runtime Spine Implementation Plan

<!-- gopact:doc-language: zh -->

[英文文档](./2026-06-23-gopact-runtime-spine.md)

## 中文

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build M1 Runtime Spine so `gopact` has stable SDK defaults, runtime identity, block-friendly messages, structured events, step-level export/import contracts, and a graph event stream that can resume from stable step boundaries.

**Architecture:** Keep root `gopact` as the provider-neutral contract and lightweight facade package. Let `graph` execute typed state and emit root `gopact.Event` values, while preserving the existing `Invoke` convenience API. Keep business harness, prompt, context, and loop strategies outside core; M1 only implements atomic process contracts.

**Tech Stack:** Go 1.24, standard library only for M1, generics, `iter.Seq2`, table-driven tests, `go test ./...`, `go vet ./...`, `gofmt`.

---

## Scope

This plan implements M1 only. It does not implement provider routing, tool registry, sandbox, memory, skill, MCP, A2A, channel adapters, or ReAct templates. Those remain M2+ work.

## Review Result

The design documents are internally consistent after the latest edits. No blocking design issues remain for M1. The important correction is that harness engineering, context engineering, loop engineering, and prompt engineering are business/template-layer concepts; core must expose step, event, checkpoint, import/export, resume, policy, and artifact primitives.

## File Structure

Create:

- `ids.go`: `RuntimeIDs` and identity helpers.
- `content.go`: `ContentPart`, typed content blocks, media and reasoning parts.
- `artifact.go`: `ArtifactRef`, `Artifact`, and `ArtifactScope`.
- `policy.go`: `PolicyDecision`.
- `step.go`: `StepSnapshot`, `StepExport`, `EffectRecord`, `StepPhase`.
- `export.go`: `RunExport`, `RunOutcome`, and export integrity types.
- `logger.go`: SDK `Logger`, `Attr`, default warn logger, log level.
- `setup.go`: global defaults snapshot, `Setup`, `Defaults`, setup options.
- `runner.go`: minimal root `Runner` facade accepting a runnable stream interface.
- `gopacttest/events.go`: event assertion helpers.
- `gopacttest/events_test.go`: tests for assertion helpers.

Modify:

- `message.go`: preserve `Content` compatibility while adding `Parts []ContentPart`.
- `event.go`: replace the small event model with structured event fields and compatibility constructors.
- `tool.go`: add `ToolResult.Artifacts []ArtifactRef` without changing `Tool` interface.
- `graph/graph.go`: add `Run` event stream, step snapshot creation, typed events, and keep `Invoke` as a wrapper.
- `graph/graph_test.go`: add stream, step, checkpoint, cancellation, and failure tests.
- `checkpoint/memory.go`: preserve current API.
- `README.md`: update the example after the new event stream API exists.
- `doc/design/index.md`: link this plan from the design entry.

## Task 1: Runtime Identity Contracts

**Files:**
- Create: `ids.go`
- Test: `ids_test.go`

- [ ] **Step 1: Write failing tests for `RuntimeIDs`**

Create `ids_test.go`:

```go
package gopact

import "testing"

func TestRuntimeIDsWithDefaultsPreservesProvidedValues(t *testing.T) {
	ids := RuntimeIDs{
		UserID:    "user-1",
		SessionID: "session-1",
		ThreadID:  "thread-1",
		RunID:     "run-1",
		AgentID:   "agent-1",
		AppID:     "app-1",
		CallID:    "call-1",
		TraceID:   "trace-1",
	}

	got := ids.WithDefaults(RuntimeIDs{
		UserID:   "default-user",
		ThreadID: "default-thread",
		RunID:    "default-run",
	})

	if got.UserID != "user-1" || got.ThreadID != "thread-1" || got.RunID != "run-1" {
		t.Fatalf("WithDefaults overwrote provided ids: %+v", got)
	}
}

func TestRuntimeIDsWithDefaultsFillsMissingValues(t *testing.T) {
	got := (RuntimeIDs{ThreadID: "thread-1"}).WithDefaults(RuntimeIDs{
		UserID:  "user-1",
		RunID:   "run-1",
		AgentID: "agent-1",
	})

	if got.UserID != "user-1" || got.ThreadID != "thread-1" || got.RunID != "run-1" || got.AgentID != "agent-1" {
		t.Fatalf("WithDefaults() = %+v", got)
	}
}
```

- [ ] **Step 2: Run the failing test**

Run:

```bash
go test ./... -run TestRuntimeIDs -count=1
```

Expected: FAIL because `RuntimeIDs` is undefined.

- [ ] **Step 3: Implement `RuntimeIDs`**

Create `ids.go`:

```go
package gopact

// RuntimeIDs carries stable identity across runs, threads, sessions, agents, and calls.
type RuntimeIDs struct {
	UserID       string `json:"user_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	AppID        string `json:"app_id,omitempty"`
	CallID       string `json:"call_id,omitempty"`
	ParentCallID string `json:"parent_call_id,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
}

// WithDefaults returns ids with empty fields filled from defaults.
func (ids RuntimeIDs) WithDefaults(defaults RuntimeIDs) RuntimeIDs {
	if ids.UserID == "" {
		ids.UserID = defaults.UserID
	}
	if ids.SessionID == "" {
		ids.SessionID = defaults.SessionID
	}
	if ids.ThreadID == "" {
		ids.ThreadID = defaults.ThreadID
	}
	if ids.RunID == "" {
		ids.RunID = defaults.RunID
	}
	if ids.AgentID == "" {
		ids.AgentID = defaults.AgentID
	}
	if ids.AppID == "" {
		ids.AppID = defaults.AppID
	}
	if ids.CallID == "" {
		ids.CallID = defaults.CallID
	}
	if ids.ParentCallID == "" {
		ids.ParentCallID = defaults.ParentCallID
	}
	if ids.TraceID == "" {
		ids.TraceID = defaults.TraceID
	}
	return ids
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./... -run TestRuntimeIDs -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ids.go ids_test.go
git commit -m "feat: add runtime identity contract"
```

## Task 2: Block-Friendly Message Contracts

**Files:**
- Create: `content.go`
- Modify: `message.go`
- Test: `message_test.go`

- [ ] **Step 1: Write failing tests for content parts**

Create `message_test.go`:

```go
package gopact

import "testing"

func TestMessageTextReturnsContentWhenPartsAreEmpty(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: "legacy content"}

	if got := msg.Text(); got != "legacy content" {
		t.Fatalf("Text() = %q, want legacy content", got)
	}
}

func TestMessageTextConcatenatesTextParts(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Parts: []ContentPart{
			TextPart("hello"),
			TextPart(" "),
			TextPart("world"),
		},
	}

	if got := msg.Text(); got != "hello world" {
		t.Fatalf("Text() = %q, want hello world", got)
	}
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./... -run 'TestMessageText' -count=1
```

Expected: FAIL because `ContentPart`, `TextPart`, and `Message.Text` are undefined.

- [ ] **Step 3: Add `ContentPart`**

Create `content.go`:

```go
package gopact

// ContentPartType identifies the kind of content carried by a Message.
type ContentPartType string

const (
	ContentPartText       ContentPartType = "text"
	ContentPartReasoning  ContentPartType = "reasoning"
	ContentPartToolCall   ContentPartType = "tool_call"
	ContentPartToolResult ContentPartType = "tool_result"
	ContentPartMedia      ContentPartType = "media"
)

// ContentPart is a provider-neutral block inside a Message.
type ContentPart struct {
	Type       ContentPartType `json:"type"`
	Text       string          `json:"text,omitempty"`
	Reasoning *ReasoningPart  `json:"reasoning,omitempty"`
	ToolCall  *ToolCall       `json:"tool_call,omitempty"`
	ToolResult *ToolResult    `json:"tool_result,omitempty"`
	Media      *MediaPart     `json:"media,omitempty"`
}

// ReasoningPart carries model reasoning metadata that is not shown by default.
type ReasoningPart struct {
	Text string `json:"text,omitempty"`
}

// MediaPart references a non-text artifact.
type MediaPart struct {
	Artifact ArtifactRef `json:"artifact"`
}

// TextPart creates a text content part.
func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartText, Text: text}
}
```

- [ ] **Step 4: Update `Message`**

Modify `message.go`:

```go
type Message struct {
	Role       Role          `json:"role"`
	Content    string        `json:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

// Text returns text content from Parts, falling back to legacy Content.
func (m Message) Text() string {
	if len(m.Parts) == 0 {
		return m.Content
	}
	var out string
	for _, part := range m.Parts {
		if part.Type == ContentPartText {
			out += part.Text
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./... -run 'TestMessageText' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add content.go message.go message_test.go
git commit -m "feat: add block-friendly message parts"
```

## Task 3: Artifact, Policy, and Step Contracts

**Files:**
- Create: `artifact.go`
- Create: `policy.go`
- Create: `step.go`
- Modify: `tool.go`
- Test: `step_test.go`

- [ ] **Step 1: Write failing tests for step export**

Create `step_test.go`:

```go
package gopact

import "testing"

func TestStepExportValidateRejectsMissingSnapshotID(t *testing.T) {
	export := StepExport{
		Snapshot: StepSnapshot{
			SchemaVersion: "v1",
			Step:          1,
			Node:          "plan",
			Phase:         StepPhaseAfterNode,
		},
	}

	if err := export.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing id error")
	}
}

func TestStepExportValidateAcceptsStableSnapshot(t *testing.T) {
	export := StepExport{
		Snapshot: StepSnapshot{
			ID:            "step-1",
			SchemaVersion: "v1",
			IDs:           RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
			Step:          1,
			Node:          "plan",
			Phase:         StepPhaseAfterNode,
			StateCodec:    "json",
		},
		Integrity: StepIntegrity{SHA256: "abc"},
	}

	if err := export.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./... -run TestStepExport -count=1
```

Expected: FAIL because step types are undefined.

- [ ] **Step 3: Add artifacts and policy contracts**

Create `artifact.go`:

```go
package gopact

// ArtifactScope describes who may observe an artifact reference.
type ArtifactScope string

const (
	ArtifactScopeRun     ArtifactScope = "run"
	ArtifactScopeThread  ArtifactScope = "thread"
	ArtifactScopeSession ArtifactScope = "session"
	ArtifactScopeUser    ArtifactScope = "user"
)

// ArtifactRef is a stable reference to a large or external payload.
type ArtifactRef struct {
	ID        string        `json:"id"`
	URI       string        `json:"uri,omitempty"`
	MediaType string        `json:"media_type,omitempty"`
	Name      string        `json:"name,omitempty"`
	Size      int64         `json:"size,omitempty"`
	SHA256    string        `json:"sha256,omitempty"`
	Scope     ArtifactScope `json:"scope,omitempty"`
}

// Artifact is an in-process artifact value before or after storage.
type Artifact struct {
	Ref   ArtifactRef `json:"ref"`
	Bytes []byte      `json:"-"`
}
```

Create `policy.go`:

```go
package gopact

// PolicyDecision records an authorization decision for an external action.
type PolicyDecision struct {
	Allowed         bool     `json:"allowed"`
	Reason          string   `json:"reason,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	RequireApproval bool     `json:"require_approval,omitempty"`
	Redact          []string `json:"redact,omitempty"`
}
```

- [ ] **Step 4: Add step contracts**

Create `step.go`:

```go
package gopact

import "errors"

// StepPhase describes where execution stopped relative to a node.
type StepPhase string

const (
	StepPhaseBeforeNode  StepPhase = "before_node"
	StepPhaseAfterNode   StepPhase = "after_node"
	StepPhaseInterrupted StepPhase = "interrupted"
	StepPhaseCanceled    StepPhase = "canceled"
	StepPhaseFailed      StepPhase = "failed"
)

// EffectRecord records a completed external side effect.
type EffectRecord struct {
	CallID         string `json:"call_id,omitempty"`
	Kind           string `json:"kind,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Completed      bool   `json:"completed"`
}

// StepSnapshot captures a stable graph step boundary.
type StepSnapshot struct {
	ID            string          `json:"id"`
	SchemaVersion string          `json:"schema_version"`
	IDs           RuntimeIDs     `json:"ids"`
	Step          int             `json:"step"`
	Node          string          `json:"node"`
	Phase         StepPhase       `json:"phase"`
	Input         []byte          `json:"input,omitempty"`
	Output        []byte          `json:"output,omitempty"`
	State         []byte          `json:"state,omitempty"`
	StateCodec    string          `json:"state_codec,omitempty"`
	Queue         []string        `json:"queue,omitempty"`
	Effects       []EffectRecord  `json:"effects,omitempty"`
	EventIDs      []string        `json:"event_ids,omitempty"`
	ConfigVersion string          `json:"config_version,omitempty"`
}

// StepIntegrity lets importers validate a StepExport before resuming.
type StepIntegrity struct {
	SHA256 string `json:"sha256,omitempty"`
}

// StepExport is the portable package for a stable graph step.
type StepExport struct {
	Snapshot  StepSnapshot  `json:"snapshot"`
	Artifacts []ArtifactRef `json:"artifacts,omitempty"`
	Integrity StepIntegrity `json:"integrity,omitempty"`
}

// Validate checks that a StepExport has enough identity to be imported.
func (e StepExport) Validate() error {
	if e.Snapshot.ID == "" {
		return errors.New("gopact: step export snapshot id is required")
	}
	if e.Snapshot.SchemaVersion == "" {
		return errors.New("gopact: step export schema version is required")
	}
	if e.Snapshot.Step <= 0 {
		return errors.New("gopact: step export step must be positive")
	}
	if e.Snapshot.Node == "" {
		return errors.New("gopact: step export node is required")
	}
	return nil
}
```

- [ ] **Step 5: Add artifact refs to tool results**

Modify `tool.go`:

```go
type ToolResult struct {
	Content   string         `json:"content,omitempty"`
	Artifacts []ArtifactRef  `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./... -run TestStepExport -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add artifact.go policy.go step.go step_test.go tool.go
git commit -m "feat: add artifact policy and step contracts"
```

## Task 4: SDK Defaults and Logger

**Files:**
- Create: `logger.go`
- Create: `setup.go`
- Test: `setup_test.go`

- [ ] **Step 1: Write failing tests for default logger behavior**

Create `setup_test.go`:

```go
package gopact

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDefaultsUseWarnLogLevel(t *testing.T) {
	resetDefaultsForTest(t)

	if got := Defaults().LogLevel; got != LevelWarn {
		t.Fatalf("default log level = %v, want %v", got, LevelWarn)
	}
}

func TestSetupOnlyAffectsFutureDefaults(t *testing.T) {
	resetDefaultsForTest(t)

	var buf bytes.Buffer
	logger := NewTextLogger(&buf, LevelDebug)
	if err := Setup(WithLogLevel(LevelDebug), WithLogger(logger)); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	Defaults().Logger.Debug(context.Background(), "debug message")

	if !strings.Contains(buf.String(), "debug message") {
		t.Fatalf("logger output = %q, want debug message", buf.String())
	}
}

func resetDefaultsForTest(t testing.TB) {
	t.Helper()
	old := Defaults()
	resetDefaults()
	t.Cleanup(func() {
		globalDefaults.Store(&defaultsHolder{snapshot: old})
	})
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./... -run 'TestDefaults|TestSetup' -count=1
```

Expected: FAIL because defaults and logger APIs are undefined.

- [ ] **Step 3: Add logger**

Create `logger.go`:

```go
package gopact

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

// LogLevel controls the minimum level emitted by a Logger.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Attr is a structured logging attribute.
type Attr struct {
	Key   string
	Value any
}

// Logger is the minimal logging interface used by gopact.
type Logger interface {
	Debug(ctx context.Context, msg string, attrs ...Attr)
	Info(ctx context.Context, msg string, attrs ...Attr)
	Warn(ctx context.Context, msg string, attrs ...Attr)
	Error(ctx context.Context, msg string, attrs ...Attr)
}

type textLogger struct {
	mu    sync.Mutex
	out   io.Writer
	level LogLevel
}

// NewTextLogger creates a small text logger for tests and local development.
func NewTextLogger(out io.Writer, level LogLevel) Logger {
	if out == nil {
		out = os.Stderr
	}
	return &textLogger{out: out, level: level}
}

func (l *textLogger) Debug(ctx context.Context, msg string, attrs ...Attr) {
	l.write(LevelDebug, "debug", msg)
}

func (l *textLogger) Info(ctx context.Context, msg string, attrs ...Attr) {
	l.write(LevelInfo, "info", msg)
}

func (l *textLogger) Warn(ctx context.Context, msg string, attrs ...Attr) {
	l.write(LevelWarn, "warn", msg)
}

func (l *textLogger) Error(ctx context.Context, msg string, attrs ...Attr) {
	l.write(LevelError, "error", msg)
}

func (l *textLogger) write(level LogLevel, label string, msg string) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.out, "%s %s\n", label, msg)
}
```

- [ ] **Step 4: Add setup defaults**

Create `setup.go`:

```go
package gopact

import (
	"os"
	"sync/atomic"
)

// DefaultsSnapshot is an immutable view of SDK process defaults.
type DefaultsSnapshot struct {
	Logger   Logger
	LogLevel LogLevel
}

type defaultsHolder struct {
	snapshot DefaultsSnapshot
}

var globalDefaults atomic.Pointer[defaultsHolder]

func init() {
	resetDefaults()
}

// Setup replaces SDK defaults for future runners.
func Setup(opts ...SetupOption) error {
	snapshot := Defaults()
	for _, opt := range opts {
		if opt != nil {
			if err := opt(&snapshot); err != nil {
				return err
			}
		}
	}
	globalDefaults.Store(&defaultsHolder{snapshot: snapshot})
	return nil
}

// Defaults returns the current immutable SDK defaults snapshot.
func Defaults() DefaultsSnapshot {
	holder := globalDefaults.Load()
	if holder == nil {
		return builtInDefaults()
	}
	return holder.snapshot
}

// SetupOption changes SDK process defaults.
type SetupOption func(*DefaultsSnapshot) error

// WithLogger sets the SDK default logger for future runners.
func WithLogger(logger Logger) SetupOption {
	return func(snapshot *DefaultsSnapshot) error {
		if logger != nil {
			snapshot.Logger = logger
		}
		return nil
	}
}

// WithLogLevel sets the SDK default log level for future runners.
func WithLogLevel(level LogLevel) SetupOption {
	return func(snapshot *DefaultsSnapshot) error {
		snapshot.LogLevel = level
		snapshot.Logger = NewTextLogger(os.Stderr, level)
		return nil
	}
}

func resetDefaults() {
	snapshot := builtInDefaults()
	globalDefaults.Store(&defaultsHolder{snapshot: snapshot})
}

func builtInDefaults() DefaultsSnapshot {
	return DefaultsSnapshot{
		Logger:   NewTextLogger(os.Stderr, LevelWarn),
		LogLevel: LevelWarn,
	}
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./... -run 'TestDefaults|TestSetup' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add logger.go setup.go setup_test.go
git commit -m "feat: add sdk defaults and logger"
```

## Task 5: Structured Events and Test Helpers

**Files:**
- Modify: `event.go`
- Create: `export.go`
- Create: `gopacttest/events.go`
- Create: `gopacttest/events_test.go`

- [ ] **Step 1: Write failing tests for event assertions**

Create `gopacttest/events_test.go`:

```go
package gopacttest

import (
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestAssertEventTypesPassesForExactOrder(t *testing.T) {
	events := []gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeStarted},
		{Type: gopact.EventNodeCompleted},
	}

	AssertEventTypes(t, events, gopact.EventRunStarted, gopact.EventNodeStarted, gopact.EventNodeCompleted)
}

func TestFilterByRunID(t *testing.T) {
	events := []gopact.Event{
		{IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		{IDs: gopact.RuntimeIDs{RunID: "run-2"}},
	}

	got := FilterByRunID(events, "run-1")
	if len(got) != 1 || got[0].IDs.RunID != "run-1" {
		t.Fatalf("FilterByRunID() = %+v", got)
	}
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./gopacttest -count=1
```

Expected: FAIL because `gopacttest` does not exist and event types are missing.

- [ ] **Step 3: Expand event types**

Modify `event.go` to include these additions while preserving existing event names that tests use:

```go
const (
	EventRunStarted       EventType = "run_started"
	EventRunCompleted     EventType = "run_completed"
	EventRunFailed        EventType = "run_failed"
	EventRunCanceled      EventType = "run_canceled"
	EventNodeStarted      EventType = "node_started"
	EventNodeCompleted    EventType = "node_completed"
	EventNodeFailed       EventType = "node_failed"
	EventStepSnapshot     EventType = "step_snapshot_created"
	EventStepExported     EventType = "step_exported"
	EventStepImported     EventType = "step_imported"
	EventModelMessage     EventType = "model_message"
	EventToolCall         EventType = "tool_call"
	EventToolResult       EventType = "tool_result"
	EventCheckpoint       EventType = "checkpoint"
	EventInterrupted      EventType = "interrupted"
)

type Event struct {
	ID           string         `json:"id,omitempty"`
	Type         EventType      `json:"type"`
	IDs          RuntimeIDs     `json:"ids,omitempty"`
	Sequence     int64          `json:"sequence,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	CallID       string         `json:"call_id,omitempty"`
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	Node         string         `json:"node,omitempty"`
	Message      *Message       `json:"message,omitempty"`
	ToolCall     *ToolCall      `json:"tool_call,omitempty"`
	Result       *ToolResult    `json:"result,omitempty"`
	Step         *StepSnapshot  `json:"step,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	Err          error          `json:"-"`
}
```

- [ ] **Step 4: Add run export**

Create `export.go`:

```go
package gopact

// RunOutcome summarizes how a run ended.
type RunOutcome string

const (
	RunOutcomeCompleted RunOutcome = "completed"
	RunOutcomeFailed    RunOutcome = "failed"
	RunOutcomeCanceled  RunOutcome = "canceled"
)

// RunExport is the portable process package for one run.
type RunExport struct {
	IDs             RuntimeIDs      `json:"ids"`
	Events          []Event         `json:"events,omitempty"`
	Steps           []StepExport    `json:"steps,omitempty"`
	Checkpoints     []StepSnapshot  `json:"checkpoints,omitempty"`
	Artifacts       []ArtifactRef   `json:"artifacts,omitempty"`
	PolicyDecisions []PolicyDecision `json:"policy_decisions,omitempty"`
	Outcome         RunOutcome      `json:"outcome,omitempty"`
}
```

- [ ] **Step 5: Add event assertions**

Create `gopacttest/events.go`:

```go
package gopacttest

import (
	"testing"

	"github.com/gopact-ai/gopact"
)

// AssertEventTypes fails the test when events do not match the expected type order.
func AssertEventTypes(t testing.TB, events []gopact.Event, expected ...gopact.EventType) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("event count = %d, want %d; events=%+v", len(events), len(expected), events)
	}
	for i := range expected {
		if events[i].Type != expected[i] {
			t.Fatalf("event[%d] = %s, want %s; events=%+v", i, events[i].Type, expected[i], events)
		}
	}
}

// FilterByRunID returns events for one run.
func FilterByRunID(events []gopact.Event, runID string) []gopact.Event {
	var out []gopact.Event
	for _, event := range events {
		if event.IDs.RunID == runID {
			out = append(out, event)
		}
	}
	return out
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./gopacttest -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add event.go export.go gopacttest/events.go gopacttest/events_test.go
git commit -m "feat: add structured events and test helpers"
```

## Task 6: Graph Event Stream and Step Snapshots

**Files:**
- Modify: `graph/graph.go`
- Modify: `graph/graph_test.go`

- [ ] **Step 1: Write failing tests for graph stream events**

Append to `graph/graph_test.go`:

```go
func TestGraphRunEmitsNodeAndStepEvents(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	var events []gopact.Event
	for event, err := range run.Run(ctx, traceState{}, WithThreadID("thread-1")) {
		if err != nil {
			t.Fatalf("Run() stream error = %v", err)
		}
		events = append(events, event)
	}

	gopacttest.AssertEventTypes(
		t,
		events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventStepSnapshot,
		gopact.EventRunCompleted,
	)
	if events[3].Step == nil || events[3].Step.Node != "one" {
		t.Fatalf("step event = %+v, want node one snapshot", events[3])
	}
}
```

Update imports in `graph/graph_test.go`:

```go
import (
	"context"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)
```

- [ ] **Step 2: Run the failing test**

Run:

```bash
go test ./graph -run TestGraphRunEmitsNodeAndStepEvents -count=1
```

Expected: FAIL because `Runnable.Run` is undefined.

- [ ] **Step 3: Add graph stream API**

Modify `graph/graph.go`:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/gopact-ai/gopact"
)
```

Add this method to `Runnable[S]`:

```go
// Run streams graph execution events from Start to End.
func (r *Runnable[S]) Run(ctx context.Context, initial S, opts ...InvokeOption) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		result, events, err := r.runCollect(ctx, initial, opts...)
		_ = result
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
		if err != nil {
			yield(gopact.Event{}, err)
		}
	}
}
```

Replace the body of `Invoke` with:

```go
func (r *Runnable[S]) Invoke(ctx context.Context, initial S, opts ...InvokeOption) (S, error) {
	result, _, err := r.runCollect(ctx, initial, opts...)
	return result, err
}
```

Add `runCollect`:

```go
func (r *Runnable[S]) runCollect(ctx context.Context, initial S, opts ...InvokeOption) (S, []gopact.Event, error) {
	var events []gopact.Event
	now := func() time.Time { return time.Now().UTC() }
	emit := func(event gopact.Event) {
		event.Sequence = int64(len(events) + 1)
		event.CreatedAt = now()
		events = append(events, event)
	}

	if r == nil {
		return initial, nil, errors.New("graph: nil runnable")
	}

	cfg := invokeConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	ids := gopact.RuntimeIDs{ThreadID: cfg.threadID}
	emit(gopact.Event{Type: gopact.EventRunStarted, IDs: ids})

	var checkpointer Checkpointer[S]
	if cfg.checkpointer != nil {
		cp, ok := cfg.checkpointer.(Checkpointer[S])
		if !ok {
			return initial, events, errors.New("graph: checkpointer state type mismatch")
		}
		checkpointer = cp
	}

	state := initial
	queue := append([]string(nil), r.edges[Start]...)
	step := 0

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			emit(gopact.Event{Type: gopact.EventRunCanceled, IDs: ids, Err: err})
			return state, events, err
		}
		if step >= r.maxSteps {
			err := fmt.Errorf("graph: exceeded max steps %d", r.maxSteps)
			emit(gopact.Event{Type: gopact.EventRunFailed, IDs: ids, Err: err})
			return state, events, err
		}

		name := queue[0]
		queue = queue[1:]
		if name == End {
			continue
		}

		node, ok := r.nodes[name]
		if !ok {
			err := fmt.Errorf("graph: missing node %q", name)
			emit(gopact.Event{Type: gopact.EventRunFailed, IDs: ids, Node: name, Err: err})
			return state, events, err
		}

		emit(gopact.Event{Type: gopact.EventNodeStarted, IDs: ids, Node: name})
		next, err := node(ctx, state)
		if err != nil {
			wrapped := fmt.Errorf("graph: node %q: %w", name, err)
			emit(gopact.Event{Type: gopact.EventNodeFailed, IDs: ids, Node: name, Err: wrapped})
			emit(gopact.Event{Type: gopact.EventRunFailed, IDs: ids, Node: name, Err: wrapped})
			return state, events, wrapped
		}
		state = next
		step++
		emit(gopact.Event{Type: gopact.EventNodeCompleted, IDs: ids, Node: name})

		snapshot := snapshotState(ids, step, name, state, r.edges[name])
		emit(gopact.Event{Type: gopact.EventStepSnapshot, IDs: ids, Node: name, Step: &snapshot})

		if checkpointer != nil {
			err := checkpointer.Put(ctx, Checkpoint[S]{
				ThreadID:  cfg.threadID,
				Step:      step,
				Node:      name,
				State:     state,
				CreatedAt: now(),
			})
			if err != nil {
				wrapped := fmt.Errorf("graph: checkpoint node %q: %w", name, err)
				emit(gopact.Event{Type: gopact.EventRunFailed, IDs: ids, Node: name, Err: wrapped})
				return state, events, wrapped
			}
		}

		queue = append(queue, r.edges[name]...)
	}

	emit(gopact.Event{Type: gopact.EventRunCompleted, IDs: ids})
	return state, events, nil
}
```

Add `snapshotState`:

```go
func snapshotState[S any](ids gopact.RuntimeIDs, step int, node string, state S, queue []string) gopact.StepSnapshot {
	var encoded []byte
	if b, err := json.Marshal(state); err == nil {
		encoded = b
	}
	return gopact.StepSnapshot{
		ID:            fmt.Sprintf("%s:%d:%s", ids.ThreadID, step, node),
		SchemaVersion: "v1",
		IDs:           ids,
		Step:          step,
		Node:          node,
		Phase:         gopact.StepPhaseAfterNode,
		State:         encoded,
		StateCodec:    "json",
		Queue:         append([]string(nil), queue...),
	}
}
```

- [ ] **Step 4: Run graph tests**

Run:

```bash
go test ./graph -run TestGraphRunEmitsNodeAndStepEvents -count=1
go test ./graph -count=1
```

Expected: PASS.

- [ ] **Step 5: Run full tests**

Run:

```bash
go test ./...
go vet ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add graph/graph.go graph/graph_test.go
git commit -m "feat: stream graph events and step snapshots"
```

## Task 7: Minimal Runner Facade

**Files:**
- Create: `runner.go`
- Test: `runner_test.go`

- [ ] **Step 1: Write failing tests for runner facade**

Create `runner_test.go`:

```go
package gopact

import (
	"context"
	"iter"
	"testing"
)

type fakeRunnable struct {
	events []Event
}

func (f fakeRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for _, event := range f.events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

func TestRunnerRunAppliesRuntimeIDs(t *testing.T) {
	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	var events []Event
	for event, err := range runner.Run(context.Background(), "input", WithRunID("run-1"), WithThreadID("thread-1")) {
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 1 || events[0].IDs.RunID != "run-1" || events[0].IDs.ThreadID != "thread-1" {
		t.Fatalf("events = %+v", events)
	}
}
```

- [ ] **Step 2: Run the failing test**

Run:

```bash
go test ./... -run TestRunnerRunAppliesRuntimeIDs -count=1
```

Expected: FAIL because `NewRunner`, `RunOption`, `WithRunID`, and `WithThreadID` are missing in root.

- [ ] **Step 3: Add root runner facade**

Create `runner.go`:

```go
package gopact

import (
	"context"
	"errors"
	"iter"
)

// Runnable is the minimal interface Runner needs from a workflow.
type Runnable interface {
	Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error]
}

// Runner executes one workflow with SDK defaults and run-scoped options.
type Runner struct {
	runnable Runnable
	defaults DefaultsSnapshot
}

// NewRunner creates a runner for a runnable workflow.
func NewRunner(runnable Runnable) (*Runner, error) {
	if runnable == nil {
		return nil, errors.New("gopact: runnable is required")
	}
	return &Runner{runnable: runnable, defaults: Defaults()}, nil
}

type runConfig struct {
	ids RuntimeIDs
}

// RunOption configures one runner invocation.
type RunOption func(*runConfig) error

// WithRunID sets the run id for one invocation.
func WithRunID(runID string) RunOption {
	return func(cfg *runConfig) error {
		cfg.ids.RunID = runID
		return nil
	}
}

// WithThreadID sets the thread id for one invocation.
func WithThreadID(threadID string) RunOption {
	return func(cfg *runConfig) error {
		cfg.ids.ThreadID = threadID
		return nil
	}
}

// Run streams events from the underlying workflow with run-scoped identity applied.
func (r *Runner) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if r == nil || r.runnable == nil {
			yield(Event{}, errors.New("gopact: nil runner"))
			return
		}
		cfg := runConfig{}
		for _, opt := range opts {
			if opt != nil {
				if err := opt(&cfg); err != nil {
					yield(Event{}, err)
					return
				}
			}
		}
		for event, err := range r.runnable.Run(ctx, input, opts...) {
			if err != nil {
				if !yield(Event{}, err) {
					return
				}
				continue
			}
			event.IDs = event.IDs.WithDefaults(cfg.ids)
			if !yield(event, nil) {
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run runner tests**

Run:

```bash
go test ./... -run TestRunnerRunAppliesRuntimeIDs -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add runner.go runner_test.go
git commit -m "feat: add minimal runner facade"
```

## Task 8: Documentation Examples and Final Verification

**Files:**
- Modify: `README.md`
- Create: `example_test.go`
- Modify: `doc/design/index.md` if implementation details changed during Tasks 1-7.

- [ ] **Step 1: Add compile-checked example**

Create `example_test.go`:

```go
package gopact_test

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func Example_graphInvoke() {
	ctx := context.Background()
	g := graph.New[state]()
	g.AddNode("plan", func(ctx context.Context, s state) (state, error) {
		s.Trace = append(s.Trace, "plan")
		return s, nil
	})
	g.AddEdge(graph.Start, "plan")
	g.AddEdge("plan", graph.End)

	run, err := g.Compile()
	if err != nil {
		panic(err)
	}

	store := checkpoint.NewMemory[state]()
	result, err := run.Invoke(ctx, state{}, graph.WithThreadID("thread-1"), graph.WithCheckpointer(store))
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Trace)
	// Output: [plan]
}

type state struct {
	Trace []string
}
```

- [ ] **Step 2: Run example**

Run:

```bash
go test ./... -run Example_graphInvoke -count=1
```

Expected: PASS.

- [ ] **Step 3: Update README example only if API changed**

If `graph.WithThreadID` or `graph.WithCheckpointer` signatures changed during implementation, update the README example to match the compiled example in `example_test.go`.

- [ ] **Step 4: Run full verification**

Run:

```bash
gofmt -w .
go test ./...
go vet ./...
git diff --check
```

Expected:

```text
go test ./... passes
go vet ./... has no output
git diff --check has no output
```

- [ ] **Step 5: Commit**

```bash
git add README.md doc/design/index.md example_test.go
git commit -m "docs: add runtime spine examples"
```

## Plan Self-Review

Spec coverage:

- SDK defaults and logger are covered by Task 4.
- Runtime identity is covered by Task 1.
- Block-friendly messages are covered by Task 2.
- Artifact, policy, and step contracts are covered by Task 3.
- Event stream, run export, and assertion helper are covered by Task 5.
- Graph event stream and step snapshots are covered by Task 6.
- Root runner facade is covered by Task 7.
- Compile-checked documentation is covered by Task 8.

Known constraints:

- Provider routing starts in M2.
- Tool registry and sandbox start in M3.
- TurnLoop is not complete in M1; M1 only creates the run-level spine it will use.
- Step import is specified by contracts in M1. Full cross-run import execution can be implemented in the follow-up TurnLoop/resume plan.

Placeholder scan:

- This plan contains no placeholder implementation steps.
- Every code-changing task includes concrete test code, implementation code, commands, expected results, and a commit point.

## Implementation Status

2026-06-23 first implementation pass:

- Implemented runtime identity contracts: `RuntimeIDs`, default merging, and zero detection.
- Implemented block-friendly message contracts: typed `ContentPart` constructors and `Message.Text`.
- Implemented artifact, policy, step snapshot, step export, and run export contracts.
- Implemented SDK defaults: `Setup`, `Defaults`, default warn-level logging, custom logger, log level, and default runtime IDs.
- Implemented structured event helpers with `RuntimeIDs`, step snapshots, compatibility `RunID`/`ThreadID` fields, and error text access.
- Implemented `gopacttest` event stream helpers.
- Implemented `graph.Run` as a step-level event stream while preserving `Invoke`.
- Implemented graph events for run started/completed/failed/canceled and node started/completed/failed.
- Implemented `graph.WithStepExport` for completed-step resume from a `StepExport`.
- Implemented root `Runner` facade for SDK default identity decoration.
- Added compile-checked example and updated README to prefer event-stream usage.

Verification completed:

- `go test ./... -count=1`
- `go vet ./...`

Not completed in this pass:

- The plan's per-task commit points were intentionally not executed as separate commits.
- Full cross-run step import execution remains in the follow-up TurnLoop/resume implementation.
