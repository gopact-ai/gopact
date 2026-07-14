package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type historySecretState struct {
	Secret string
}

func TestWorkflowNodeHistoryContainsMetadataWithoutBusinessPayload(t *testing.T) {
	const secret = "history-secret-4b7d9a"
	wf := New[historySecretState, historySecretState]("history-metadata")
	wf.Context(func(input historySecretState) historySecretState { return input })
	node := wf.Node("node", func(context.Context, historySecretState) (historySecretState, error) {
		return historySecretState{}, errors.New("provider rejected token " + secret)
	})
	wf.Entry(node)
	wf.Exit(node)

	var nodeEvents []gopact.Event
	_, err := wf.Invoke(t.Context(), historySecretState{Secret: secret}, gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if strings.HasPrefix(event.Type, "node.") {
			event.Payload = bytes.Clone(event.Payload)
			nodeEvents = append(nodeEvents, event)
		}
		return nil
	}))
	if err == nil {
		t.Fatal("Invoke() error = nil, want node failure")
	}
	if len(nodeEvents) != 2 {
		t.Fatalf("node events = %d, want started and failed", len(nodeEvents))
	}
	for _, event := range nodeEvents {
		assertNodeEventEnvelope(t, event)
		assertMetadataOnlyNodePayload(t, event.Payload, secretRepresentations(t, secret))
	}
	var failed map[string]any
	if err := json.Unmarshal(nodeEvents[1].Payload, &failed); err != nil {
		t.Fatal(err)
	}
	if failed["status"] != "failed" || failed["error"] != "failed" {
		t.Fatalf("failed metadata = %+v, want status/error classification failed", failed)
	}
}

func assertNodeEventEnvelope(t *testing.T, event gopact.Event) {
	t.Helper()
	if event.RunID == "" || event.RevisionID == "" || event.Sequence <= 0 || event.Timestamp.IsZero() ||
		event.Type == "" || event.Source != "workflow.node" || event.Origin == "" || event.NodeID == "" ||
		event.ActivationID == "" || event.AttemptID == "" || event.NodeExecutionVersion <= 0 {
		t.Fatalf("node event envelope metadata incomplete: %+v", event)
	}
}

func assertMetadataOnlyNodePayload(t *testing.T, payload []byte, secrets [][]byte) {
	t.Helper()
	for _, secret := range secrets {
		if bytes.Contains(payload, secret) {
			t.Fatalf("node payload %s contains secret representation %q", payload, secret)
		}
	}
	var facts map[string]any
	if err := json.Unmarshal(payload, &facts); err != nil {
		t.Fatalf("unmarshal node payload: %v", err)
	}
	assertFieldsAbsent(t, facts, payload, "input", "effective_input", "workflow_context", "output", "json", "gob")
	assertFieldsPresent(t, facts, payload, "node_name", "activation_id", "attempt", "activation_phase", "correlation", "status")
}

func assertFieldsAbsent(t *testing.T, facts map[string]any, payload []byte, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, exists := facts[field]; exists {
			t.Fatalf("node payload field %q exists in %s", field, payload)
		}
	}
}

func assertFieldsPresent(t *testing.T, facts map[string]any, payload []byte, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, exists := facts[field]; !exists {
			t.Fatalf("node payload field %q missing from %s", field, payload)
		}
	}
}

func secretRepresentations(t *testing.T, secret string) [][]byte {
	t.Helper()
	jsonSecret, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	var gobSecret bytes.Buffer
	if err := gob.NewEncoder(&gobSecret).Encode(secret); err != nil {
		t.Fatal(err)
	}
	raw := []byte(secret)
	return [][]byte{
		raw,
		jsonSecret,
		gobSecret.Bytes(),
		[]byte(base64.StdEncoding.EncodeToString(raw)),
		[]byte(base64.StdEncoding.EncodeToString(jsonSecret)),
		[]byte(base64.StdEncoding.EncodeToString(gobSecret.Bytes())),
	}
}

func TestNodeEventDoesNotSerializeBusinessValues(t *testing.T) {
	record := activationRecord{
		activation:           activation{id: "act-1", node: "node", input: make(chan int), correlation: CorrelationKey{ID: "run-1", Epoch: 1}},
		phase:                activationCompleted,
		attempt:              1,
		nodeExecutionVersion: 1,
		hasResult:            true,
		result:               nodeRunResult{output: make(chan int)},
	}
	event, err := record.nodeEvent(EventNodeCompleted, "")
	if err != nil {
		t.Fatalf("nodeEvent() error = %v", err)
	}
	if !json.Valid(event.Payload) {
		t.Fatalf("nodeEvent() payload = %q, want JSON metadata", event.Payload)
	}
}

func TestWorkflowCheckpointStillFailsClosedForUnencodableState(t *testing.T) {
	wf := New[chan int, chan int]("checkpoint-unencodable")
	node := wf.Node("node", func(_ context.Context, input chan int) (chan int, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	_, err := wf.Invoke(t.Context(), make(chan int))
	if err == nil || !strings.Contains(err.Error(), "encode checkpoint payload") {
		t.Fatalf("Invoke() error = %v, want checkpoint encoding failure", err)
	}
}

func TestWorkflowExplicitArtifactReferenceRemainsQueryable(t *testing.T) {
	store := NewMemoryStore()
	wf := New[string, string]("artifact-history", WithStore(store))
	node := wf.Node("node", func(ctx context.Context, input string) (string, error) {
		ref := gopact.ArtifactRef{URI: "artifact://report", Kind: "report", Digest: "sha256:abc123"}
		payload, err := json.Marshal(ref)
		if err != nil {
			return "", err
		}
		if err := Emit(ctx, gopact.Event{Type: "artifact.created", Payload: payload, PayloadRef: ref.URI}); err != nil {
			return "", err
		}
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("artifact-run")); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: "artifact-run"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, record := range records {
		if record.EventType != "artifact.created" {
			continue
		}
		var ref gopact.ArtifactRef
		if err := json.Unmarshal(record.Payload, &ref); err != nil {
			t.Fatalf("unmarshal artifact ref: %v", err)
		}
		if ref.Digest != "sha256:abc123" || record.PayloadRef != "artifact://report" {
			t.Fatalf("artifact record = %+v, ref = %+v", record, ref)
		}
		return
	}
	t.Fatal("artifact.created record not found")
}
