package acp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"

	protocol "github.com/gopact-ai/acp"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/agent"
)

// NewAgent exposes target over an ACP connection.
func NewAgent(input io.ReadCloser, output io.Writer, target agent.Agent, options ...protocol.Option) (*protocol.Conn, error) {
	if isNil(target) {
		return nil, errors.New("gopact/acp: agent is nil")
	}
	identity := target.Identity()
	if identity.Name == "" || identity.Description == "" || identity.Version == "" {
		return nil, errors.New("gopact/acp: agent identity is incomplete")
	}
	return protocol.NewAgent(input, output, func(client *protocol.ClientCaller) protocol.AgentHandler {
		return &handler{
			target:   target,
			client:   client,
			identity: identity,
			sessions: make(map[protocol.SessionID]*prompt),
		}
	}, options...)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type handler struct {
	target   agent.Agent
	client   *protocol.ClientCaller
	identity agent.Identity

	mu       sync.Mutex
	ready    bool
	sessions map[protocol.SessionID]*prompt
}

type prompt struct {
	cancel   context.CancelFunc
	canceled atomic.Bool
}

func (h *handler) Initialize(_ context.Context, request *protocol.InitializeRequest) (*protocol.InitializeResponse, error) {
	if request.ProtocolVersion != protocol.ProtocolVersionV1 {
		return nil, &protocol.Error{Code: protocol.ErrorCodeInvalidParams, Message: "Unsupported protocol version"}
	}
	h.mu.Lock()
	h.ready = true
	h.mu.Unlock()
	return &protocol.InitializeResponse{
		ProtocolVersion:   protocol.ProtocolVersionV1,
		AgentCapabilities: &protocol.AgentCapabilities{},
		AgentInfo: &protocol.Implementation{
			Name:    h.identity.Name,
			Version: h.identity.Version,
		},
	}, nil
}

func (h *handler) NewSession(context.Context, *protocol.NewSessionRequest) (*protocol.NewSessionResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.ready {
		return nil, &protocol.Error{Code: protocol.ErrorCodeInvalidRequest, Message: "Agent is not initialized"}
	}
	var sessionID protocol.SessionID
	for exists := true; exists; _, exists = h.sessions[sessionID] {
		sessionID = protocol.SessionID("session-" + rand.Text())
	}
	h.sessions[sessionID] = nil
	return &protocol.NewSessionResponse{SessionID: sessionID}, nil
}

func (h *handler) Prompt(ctx context.Context, request *protocol.PromptRequest) (*protocol.PromptResponse, error) {
	callCtx, cancel := context.WithCancel(ctx)
	active := &prompt{cancel: cancel}
	if err := h.startPrompt(request.SessionID, active); err != nil {
		cancel()
		return nil, err
	}
	defer func() {
		cancel()
		h.finishPrompt(request.SessionID, active)
	}()

	input, err := promptRequest(request.Prompt)
	if err != nil {
		return nil, err
	}
	options := []gopact.RunOption{gopact.WithSessionID(string(request.SessionID))}
	if streaming, ok := h.target.(agent.StreamingAgent); ok {
		err = h.stream(callCtx, request.SessionID, streaming, input, options)
		return h.promptResult(active, err)
	}

	response, err := h.target.Invoke(callCtx, input, options...)
	if err != nil {
		return h.promptError(active, err)
	}
	return h.promptResult(active, h.sendResponse(callCtx, request.SessionID, response))
}

func (h *handler) Cancel(_ context.Context, notification *protocol.CancelNotification) error {
	h.mu.Lock()
	active, exists := h.sessions[notification.SessionID]
	h.mu.Unlock()
	if exists && active != nil {
		active.canceled.Store(true)
		active.cancel()
	}
	return nil
}

func (h *handler) startPrompt(sessionID protocol.SessionID, active *prompt) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, exists := h.sessions[sessionID]
	if !exists {
		return &protocol.Error{Code: protocol.ErrorCodeResourceNotFound, Message: "Session not found"}
	}
	if current != nil {
		return &protocol.Error{Code: protocol.ErrorCodeInvalidRequest, Message: "Session prompt already active"}
	}
	h.sessions[sessionID] = active
	return nil
}

func (h *handler) finishPrompt(sessionID protocol.SessionID, active *prompt) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[sessionID] == active {
		h.sessions[sessionID] = nil
	}
}

func (h *handler) promptError(active *prompt, err error) (*protocol.PromptResponse, error) {
	if active.canceled.Load() {
		return &protocol.PromptResponse{StopReason: protocol.StopReasonCanceled}, nil
	}
	return nil, err
}

func (h *handler) promptResult(active *prompt, err error) (*protocol.PromptResponse, error) {
	if err != nil || active.canceled.Load() {
		return h.promptError(active, err)
	}
	return &protocol.PromptResponse{StopReason: protocol.StopReasonEndTurn}, nil
}

func (h *handler) stream(ctx context.Context, sessionID protocol.SessionID, target agent.StreamingAgent, request agent.Request, options []gopact.RunOption) error {
	for chunk, err := range target.InvokeStream(ctx, request, options...) {
		if err != nil {
			return err
		}
		if err := h.sendParts(ctx, sessionID, chunkParts(chunk)); err != nil {
			return err
		}
	}
	return nil
}

func chunkParts(chunk agent.Chunk) []gopact.MessagePart {
	if len(chunk.Parts) == 0 && chunk.Text != "" {
		return []gopact.MessagePart{{Type: gopact.MessagePartTypeText, Text: chunk.Text}}
	}
	return chunk.Parts
}

func (h *handler) sendResponse(ctx context.Context, sessionID protocol.SessionID, response agent.Response) error {
	if err := h.sendParts(ctx, sessionID, response.Message.Parts); err != nil {
		return err
	}
	for _, artifact := range response.Artifacts {
		if err := h.sendArtifact(ctx, sessionID, artifact); err != nil {
			return err
		}
	}
	return nil
}

func promptRequest(blocks []protocol.ContentBlock) (agent.Request, error) {
	message := gopact.Message{Role: gopact.MessageRoleUser}
	request := agent.Request{Messages: []gopact.Message{message}}
	for _, block := range blocks {
		part, artifact, err := promptPart(block)
		if err != nil {
			return agent.Request{}, err
		}
		message.Parts = append(message.Parts, part)
		request.Artifacts = append(request.Artifacts, artifact...)
	}
	request.Messages[0] = message
	return request, nil
}

func promptPart(block protocol.ContentBlock) (gopact.MessagePart, []gopact.ArtifactRef, error) {
	switch block.Type {
	case protocol.ContentBlockTypeText:
		return gopact.MessagePart{Type: gopact.MessagePartTypeText, Text: block.Text}, nil, nil
	case protocol.ContentBlockTypeResourceLink:
		return resourceLinkPart(block)
	default:
		return gopact.MessagePart{}, nil, &protocol.Error{Code: protocol.ErrorCodeInvalidParams, Message: "Unsupported prompt content"}
	}
}

func resourceLinkPart(block protocol.ContentBlock) (gopact.MessagePart, []gopact.ArtifactRef, error) {
	if block.URI == nil {
		return gopact.MessagePart{}, nil, &protocol.Error{Code: protocol.ErrorCodeInvalidParams, Message: "Resource link URI is required"}
	}
	ref := gopact.ArtifactRef{URI: *block.URI, Kind: block.Name}
	return gopact.MessagePart{Type: gopact.MessagePartTypeArtifact, Ref: &ref}, []gopact.ArtifactRef{ref}, nil
}

func (h *handler) sendParts(ctx context.Context, sessionID protocol.SessionID, parts []gopact.MessagePart) error {
	for _, part := range parts {
		if err := h.sendPart(ctx, sessionID, part); err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) sendPart(ctx context.Context, sessionID protocol.SessionID, part gopact.MessagePart) error {
	switch {
	case part.Type == gopact.MessagePartTypeText && part.Ref == nil:
		return h.sendUpdate(ctx, sessionID, protocol.TextContentBlock(part.Text))
	case part.Type == gopact.MessagePartTypeArtifact && part.Ref != nil:
		return h.sendArtifact(ctx, sessionID, *part.Ref)
	default:
		return fmt.Errorf("gopact/acp: unsupported response message part %q", part.Type)
	}
}

func (h *handler) sendArtifact(ctx context.Context, sessionID protocol.SessionID, artifact gopact.ArtifactRef) error {
	name := artifact.Kind
	if name == "" {
		name = artifact.URI
	}
	return h.sendUpdate(ctx, sessionID, protocol.ResourceLinkContentBlock(name, artifact.URI))
}

func (h *handler) sendUpdate(ctx context.Context, sessionID protocol.SessionID, content protocol.ContentBlock) error {
	return h.client.Update(ctx, &protocol.SessionNotification{
		SessionID: sessionID,
		Update:    protocol.AgentMessageChunkSessionUpdate(content),
	})
}
