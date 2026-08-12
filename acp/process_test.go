package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	protocol "github.com/gopact-ai/acp"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/agent"
)

func TestAgentProcessE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentProcess$")
	command.WaitDelay = time.Second
	command.Env = append(os.Environ(), "GOPACT_ACP_HELPER=1")
	input, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		_ = output.Close()
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	client := &testClient{}
	var caller *protocol.AgentCaller
	conn, err := protocol.NewClient(input, output, func(value *protocol.AgentCaller) protocol.ClientHandler {
		caller = value
		return client
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	initialized, err := caller.Initialize(ctx, &protocol.InitializeRequest{ProtocolVersion: protocol.ProtocolVersionV1})
	if err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != protocol.ProtocolVersionV1 || initialized.AgentInfo == nil ||
		initialized.AgentInfo.Name != "process" || initialized.AgentInfo.Version != "v1" {
		t.Fatalf("Initialize() = %+v", initialized)
	}

	session, err := caller.NewSession(ctx, &protocol.NewSessionRequest{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	response, err := caller.Prompt(ctx, &protocol.PromptRequest{
		SessionID: session.SessionID,
		Prompt: []protocol.ContentBlock{
			protocol.TextContentBlock("hello process"),
			protocol.ResourceLinkContentBlock("spec", "file:///tmp/process.md"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != protocol.StopReasonEndTurn {
		t.Fatalf("Prompt() stop reason = %q", response.StopReason)
	}
	updates := client.snapshot()
	if len(updates) != 2 {
		t.Fatalf("session updates = %+v", updates)
	}
	text, textOK := updates[0].Update.Content.(protocol.ContentBlock)
	resource, resourceOK := updates[1].Update.Content.(protocol.ContentBlock)
	if updates[0].SessionID != session.SessionID || updates[1].SessionID != session.SessionID ||
		updates[0].Update.SessionUpdate != protocol.SessionUpdateTypeAgentMessageChunk ||
		updates[1].Update.SessionUpdate != protocol.SessionUpdateTypeAgentMessageChunk ||
		!textOK || text.Type != protocol.ContentBlockTypeText || text.Text != "hello process back:"+string(session.SessionID) ||
		!resourceOK || resource.Type != protocol.ContentBlockTypeResourceLink || resource.Name != "answer" ||
		resource.URI == nil || *resource.URI != "artifact://process" {
		t.Fatalf("session updates = %+v", updates)
	}

	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentProcess(t *testing.T) {
	if os.Getenv("GOPACT_ACP_HELPER") != "1" {
		t.Skip("not running as helper process")
	}
	conn, err := NewAgent(os.Stdin, os.Stdout, processTarget{})
	if err != nil {
		t.Fatal(err)
	}
	<-conn.Done()
	if err := conn.Err(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

type processTarget struct{}

func (processTarget) Identity() agent.Identity {
	return agent.Identity{Name: "process", Description: "process ACP agent", Version: "v1"}
}

func (processTarget) Invoke(_ context.Context, request agent.Request, options ...gopact.RunOption) (agent.Response, error) {
	config := gopact.ResolveRunOptions(options...)
	if config.SessionID == "" || len(request.Messages) != 1 || len(request.Messages[0].Parts) != 2 ||
		request.Messages[0].Parts[0].Text != "hello process" || request.Messages[0].Parts[1].Ref == nil ||
		request.Messages[0].Parts[1].Ref.URI != "file:///tmp/process.md" || request.Messages[0].Parts[1].Ref.Kind != "spec" ||
		len(request.Artifacts) != 1 || request.Artifacts[0].URI != "file:///tmp/process.md" || request.Artifacts[0].Kind != "spec" {
		return agent.Response{}, fmt.Errorf("unexpected request: %+v", request)
	}
	return agent.Response{
		Message:   gopact.Message{Role: gopact.MessageRoleAssistant, Parts: []gopact.MessagePart{{Type: gopact.MessagePartTypeText, Text: "hello process back:" + config.SessionID}}},
		Artifacts: []gopact.ArtifactRef{{URI: "artifact://process", Kind: "answer"}},
	}, nil
}
