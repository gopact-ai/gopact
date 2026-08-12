package acp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	protocol "github.com/gopact-ai/acp"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/agent"
)

func TestAgentRoundTrip(t *testing.T) {
	target := &testAgent{started: make(chan struct{})}
	agentTransport, clientTransport := net.Pipe()
	agentConn, err := NewAgent(agentTransport, agentTransport, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agentConn.Close() }()

	client := &testClient{}
	var caller *protocol.AgentCaller
	clientConn, err := protocol.NewClient(clientTransport, clientTransport, func(value *protocol.AgentCaller) protocol.ClientHandler {
		caller = value
		return client
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()

	if _, err := caller.NewSession(t.Context(), &protocol.NewSessionRequest{Cwd: t.TempDir()}); err == nil {
		t.Fatal("NewSession() before Initialize() error = nil")
	}
	initialized, err := caller.Initialize(t.Context(), &protocol.InitializeRequest{ProtocolVersion: protocol.ProtocolVersionV1})
	if err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != protocol.ProtocolVersionV1 || initialized.AgentInfo == nil ||
		initialized.AgentInfo.Name != "echo" || initialized.AgentInfo.Version != "v1" {
		t.Fatalf("Initialize() = %+v", initialized)
	}

	session, err := caller.NewSession(t.Context(), &protocol.NewSessionRequest{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID == "" {
		t.Fatal("NewSession() returned an empty session ID")
	}

	response, err := caller.Prompt(t.Context(), &protocol.PromptRequest{
		SessionID: session.SessionID,
		Prompt: []protocol.ContentBlock{
			protocol.TextContentBlock("hello"),
			protocol.ResourceLinkContentBlock("spec", "file:///tmp/spec.md"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != protocol.StopReasonEndTurn {
		t.Fatalf("Prompt() stop reason = %q", response.StopReason)
	}
	request, config := target.lastCall()
	if len(request.Messages) != 1 || request.Messages[0].Role != gopact.MessageRoleUser ||
		len(request.Messages[0].Parts) != 2 || request.Messages[0].Parts[0].Text != "hello" ||
		request.Messages[0].Parts[1].Ref == nil || request.Messages[0].Parts[1].Ref.URI != "file:///tmp/spec.md" ||
		request.Messages[0].Parts[1].Ref.Kind != "spec" || len(request.Artifacts) != 1 ||
		request.Artifacts[0].URI != "file:///tmp/spec.md" || request.Artifacts[0].Kind != "spec" {
		t.Fatalf("Invoke() request = %+v", request)
	}
	if config.SessionID != string(session.SessionID) {
		t.Fatalf("Invoke() session ID = %q", config.SessionID)
	}
	updates := client.snapshot()
	if len(updates) != 2 || updates[0].Update.SessionUpdate != protocol.SessionUpdateTypeAgentMessageChunk ||
		updates[0].Update.Content.(protocol.ContentBlock).Text != "hello back" ||
		updates[1].Update.Content.(protocol.ContentBlock).URI == nil ||
		*updates[1].Update.Content.(protocol.ContentBlock).URI != "artifact://answer" {
		t.Fatalf("session updates = %+v", updates)
	}

	target.block = true
	promptDone := make(chan *protocol.PromptResponse, 1)
	promptErr := make(chan error, 1)
	go func() {
		result, callErr := caller.Prompt(context.Background(), &protocol.PromptRequest{
			SessionID: session.SessionID,
			Prompt:    []protocol.ContentBlock{protocol.TextContentBlock("wait")},
		})
		promptDone <- result
		promptErr <- callErr
	}()
	<-target.started
	if err := caller.Cancel(t.Context(), &protocol.CancelNotification{SessionID: session.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err := <-promptErr; err != nil {
		t.Fatal(err)
	}
	if got := <-promptDone; got.StopReason != protocol.StopReasonCanceled {
		t.Fatalf("canceled Prompt() = %+v", got)
	}
}

func TestNewAgentRejectsNilTarget(t *testing.T) {
	for _, target := range []agent.Agent{nil, (*testAgent)(nil)} {
		left, right := net.Pipe()
		if _, err := NewAgent(left, left, target); err == nil {
			t.Fatal("NewAgent() error = nil")
		}
		_ = left.Close()
		_ = right.Close()
	}
}

func TestPromptRejectsUnknownSessionAndUnsupportedContent(t *testing.T) {
	h := &handler{target: &testAgent{}, sessions: make(map[protocol.SessionID]*prompt)}
	if _, err := h.Prompt(t.Context(), &protocol.PromptRequest{
		SessionID: "missing",
		Prompt:    []protocol.ContentBlock{protocol.TextContentBlock("hello")},
	}); err == nil {
		t.Fatal("Prompt() unknown session error = nil")
	}
	h.sessions["session-1"] = nil
	if _, err := h.Prompt(t.Context(), &protocol.PromptRequest{
		SessionID: "session-1",
		Prompt:    []protocol.ContentBlock{protocol.ImageContentBlock("image", "image/png")},
	}); err == nil {
		t.Fatal("Prompt() unsupported content error = nil")
	}
}

func TestSendPartReturnsProtocolErrorForUnsupportedResponse(t *testing.T) {
	h := &handler{}
	err := h.sendPart(t.Context(), "session-1", gopact.MessagePart{Type: "image"})
	var protocolErr *protocol.Error
	if !errors.As(err, &protocolErr) || protocolErr.Code != protocol.ErrorCodeInternalError {
		t.Fatalf("sendPart() error = %v", err)
	}
}

type testAgent struct {
	mu      sync.Mutex
	request agent.Request
	config  gopact.RunConfig
	block   bool
	started chan struct{}
}

func (*testAgent) Identity() agent.Identity {
	return agent.Identity{Name: "echo", Description: "echoes prompts", Version: "v1"}
}

func (target *testAgent) Invoke(ctx context.Context, request agent.Request, options ...gopact.RunOption) (agent.Response, error) {
	target.mu.Lock()
	target.request = request.Clone()
	target.config = gopact.ResolveRunOptions(options...)
	block := target.block
	target.mu.Unlock()
	if block {
		close(target.started)
		<-ctx.Done()
		return agent.Response{}, ctx.Err()
	}
	return agent.Response{
		Message:   gopact.Message{Role: gopact.MessageRoleAssistant, Parts: []gopact.MessagePart{{Type: gopact.MessagePartTypeText, Text: "hello back"}}},
		Artifacts: []gopact.ArtifactRef{{URI: "artifact://answer", Kind: "answer"}},
	}, nil
}

func (target *testAgent) lastCall() (agent.Request, gopact.RunConfig) {
	target.mu.Lock()
	defer target.mu.Unlock()
	return target.request.Clone(), target.config
}

type testClient struct {
	mu      sync.Mutex
	updates []protocol.SessionNotification
}

func (*testClient) RequestPermission(context.Context, *protocol.RequestPermissionRequest) (*protocol.RequestPermissionResponse, error) {
	return nil, errors.New("unexpected permission request")
}

func (client *testClient) Update(_ context.Context, notification *protocol.SessionNotification) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.updates = append(client.updates, *notification)
	return nil
}

func (client *testClient) snapshot() []protocol.SessionNotification {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]protocol.SessionNotification(nil), client.updates...)
}
