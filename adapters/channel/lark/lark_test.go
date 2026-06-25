package lark

import (
	"context"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestTransferConvertsSurfaceMessageToTextPayload(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	transfer := NewTransfer()

	payload, err := transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type: gopact.SurfaceMessageMessage,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "hello"},
			{Type: gopact.SurfacePartStatus, Text: "working"},
		},
		SourceEvent: string(gopact.EventModelMessage),
		CreatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if payload.Target != Target {
		t.Fatalf("Target = %q, want %q", payload.Target, Target)
	}
	got, ok := payload.Data.(Payload)
	if !ok {
		t.Fatalf("payload data type = %T, want lark.Payload", payload.Data)
	}
	if got.MsgType != MsgTypeText {
		t.Fatalf("MsgType = %q, want text", got.MsgType)
	}
	if got.Content.Text != "hello\nworking" {
		t.Fatalf("content text = %q, want combined text", got.Content.Text)
	}
	if got.MessageID != "surface-1" || got.IDs.RunID != "run-1" || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("payload = %+v, want copied surface metadata", got)
	}
	if payload.Metadata["surface_type"] != string(gopact.SurfaceMessageMessage) {
		t.Fatalf("metadata = %+v, want surface type", payload.Metadata)
	}
}

func TestTransferConvertsApprovalToInteractiveCard(t *testing.T) {
	transfer := NewTransfer(WithActionSigner(ActionSignerFunc(func(_ context.Context, _ gopact.SurfaceMessage, action gopact.SurfaceAction) (string, error) {
		return "sig-" + action.ID, nil
	})))
	actionPayload := map[string]any{"approved": true}

	payload, err := transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-approval",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type: gopact.SurfaceMessageApproval,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "approve repo.write?"},
		},
		Actions: []gopact.SurfaceAction{
			{
				ID:          "approval-1",
				Type:        gopact.SurfaceActionResume,
				Label:       "Approve",
				IDs:         gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
				InterruptID: "interrupt-1",
				CallID:      "call-1",
				Payload:     actionPayload,
				Metadata:    map[string]any{"risk": "write"},
			},
		},
		Artifacts: []gopact.ArtifactRef{{ID: "artifact-1", Name: "patch.diff", URI: "file:///tmp/patch.diff"}},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	if got.MsgType != MsgTypeInteractive {
		t.Fatalf("MsgType = %q, want interactive", got.MsgType)
	}
	if got.Card == nil {
		t.Fatal("Card = nil, want interactive card")
	}
	if len(got.Card.Elements) != 3 {
		t.Fatalf("card elements count = %d, want text, artifacts, actions", len(got.Card.Elements))
	}
	actionElement := got.Card.Elements[2]
	if actionElement.Tag != "action" || len(actionElement.Actions) != 1 {
		t.Fatalf("action element = %+v, want one action button", actionElement)
	}
	button := actionElement.Actions[0]
	if button.Text.Content != "Approve" {
		t.Fatalf("button label = %q, want Approve", button.Text.Content)
	}
	if button.Value.Signature != "sig-approval-1" {
		t.Fatalf("button signature = %q, want signed action", button.Value.Signature)
	}
	if button.Value.InterruptID != "interrupt-1" || button.Value.CallID != "call-1" {
		t.Fatalf("button value = %+v, want interrupt/call identity", button.Value)
	}
	if !reflect.DeepEqual(button.Value.Payload, actionPayload) {
		t.Fatalf("button payload = %+v, want action payload", button.Value.Payload)
	}
}

func TestChannelSendsPayloadThroughInjectedSenderAndYieldsEvents(t *testing.T) {
	ctx := context.Background()
	var sent Payload
	channel, err := NewChannel(SenderFunc(func(_ context.Context, payload Payload) (SendResult, error) {
		sent = payload
		return SendResult{MessageID: "lark-message-1", Metadata: map[string]any{"chat_id": "chat-1"}}, nil
	}), WithEvents(func(_ context.Context) iter.Seq2[gopact.ChannelEvent, error] {
		return func(yield func(gopact.ChannelEvent, error) bool) {
			yield(gopact.ChannelEvent{
				Type: gopact.ChannelEventMessage,
				Text: "resume",
				IDs:  gopact.RuntimeIDs{RunID: "run-1"},
			}, nil)
		}
	}))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}

	payload := Payload{MsgType: MsgTypeText, Content: TextContent{Text: "hello"}}
	if err := channel.Send(ctx, gopact.ChannelPayload{Target: Target, Data: payload}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if sent.Content.Text != "hello" {
		t.Fatalf("sent payload = %+v, want text payload", sent)
	}

	var events []gopact.ChannelEvent
	for event, err := range channel.Events(ctx) {
		if err != nil {
			t.Fatalf("Events() error = %v", err)
		}
		events = append(events, event)
	}
	if len(events) != 1 || events[0].Channel != Target || events[0].Text != "resume" {
		t.Fatalf("events = %+v, want normalized lark event", events)
	}
}

func TestChannelEventFromActionValue(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	value := ActionValue{
		ActionID:    "approval-1",
		ActionType:  string(gopact.SurfaceActionResume),
		InterruptID: "interrupt-1",
		CallID:      "call-1",
		IDs:         gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Payload:     map[string]any{"approved": true},
		Signature:   "sig-approval-1",
		Metadata:    map[string]any{"risk": "write"},
	}

	event := ChannelEventFromActionValue("event-1", value, createdAt)
	if event.ID != "event-1" || event.Channel != Target || event.Type != gopact.ChannelEventAction {
		t.Fatalf("event identity = %+v, want lark action event", event)
	}
	if event.Action.ID != "approval-1" || event.Action.Type != gopact.SurfaceActionResume || event.Action.InterruptID != "interrupt-1" {
		t.Fatalf("event action = %+v, want resume action", event.Action)
	}
	if event.Metadata["lark_action_signature"] != "sig-approval-1" || event.Metadata["risk"] != "write" {
		t.Fatalf("event metadata = %+v, want signature and action metadata", event.Metadata)
	}
	resume, ok := event.ResumeRequest()
	if !ok {
		t.Fatal("ResumeRequest() ok = false, want true")
	}
	if resume.InterruptID != "interrupt-1" || resume.IDs.RunID != "run-1" {
		t.Fatalf("resume = %+v, want action identity", resume)
	}
}

func TestCallbackSourceHandlesURLVerification(t *testing.T) {
	source, err := NewCallbackSource()
	if err != nil {
		t.Fatalf("NewCallbackSource() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/lark/callback", strings.NewReader(`{
		"type":"url_verification",
		"challenge":"challenge-1"
	}`))
	source.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body=%q, want 200", response.StatusCode, raw)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(raw), `"challenge":"challenge-1"`) {
		t.Fatalf("body = %q, want challenge response", raw)
	}
}

func TestCallbackSourceDecodesCardActionEvent(t *testing.T) {
	source, err := NewCallbackSource()
	if err != nil {
		t.Fatalf("NewCallbackSource() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/lark/callback", strings.NewReader(`{
		"schema":"2.0",
		"header":{"event_id":"event-1","create_time":"1782300000000"},
		"event":{
			"operator":{"open_id":"ou-user","user_id":"user-1"},
			"action":{
				"value":{
					"action_id":"devagent.review.approve",
					"action_type":"resume",
					"interrupt_id":"interrupt-1",
					"call_id":"call-1",
					"ids":{"run_id":"run-1","thread_id":"thread-1"},
					"payload":{"review_status":"approved"},
					"signature":"sig-1",
					"metadata":{"risk":"write"}
				}
			}
		}
	}`))
	source.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body=%q, want 202", response.StatusCode, raw)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := nextEvent(source.Events(ctx))
	if err != nil {
		t.Fatalf("nextEvent() error = %v", err)
	}
	if event.ID != "event-1" || event.Channel != Target || event.Type != gopact.ChannelEventAction {
		t.Fatalf("event identity = %+v, want lark action event", event)
	}
	if event.IDs.RunID != "run-1" || event.IDs.ThreadID != "thread-1" {
		t.Fatalf("event IDs = %+v, want action IDs", event.IDs)
	}
	if event.Action.ID != "devagent.review.approve" || event.Action.Type != gopact.SurfaceActionResume {
		t.Fatalf("event action = %+v, want resume action", event.Action)
	}
	if event.Action.InterruptID != "interrupt-1" || event.Action.CallID != "call-1" {
		t.Fatalf("event action identity = %+v, want interrupt/call ids", event.Action)
	}
	if event.Metadata["lark_open_id"] != "ou-user" || event.Metadata["lark_user_id"] != "user-1" {
		t.Fatalf("event metadata = %+v, want Lark operator ids", event.Metadata)
	}
	if event.Metadata["lark_action_signature"] != "sig-1" || event.Metadata["risk"] != "write" {
		t.Fatalf("event metadata = %+v, want action metadata", event.Metadata)
	}
}

func TestCallbackSourceVerifiesCallbackAndAction(t *testing.T) {
	var verifiedCallback CallbackRequest
	var verifiedAction ActionValue
	source, err := NewCallbackSource(
		WithCallbackVerifier(CallbackVerifierFunc(func(_ context.Context, request CallbackRequest) error {
			verifiedCallback = request
			if request.Header.Get("X-Lark-Signature") != "callback-sig" {
				t.Fatalf("callback signature header = %q, want callback-sig", request.Header.Get("X-Lark-Signature"))
			}
			if !strings.Contains(string(request.Body), `"signature":"action-sig"`) {
				t.Fatalf("callback body = %q, want action signature", request.Body)
			}
			return nil
		})),
		WithActionVerifier(ActionVerifierFunc(func(_ context.Context, value ActionValue) error {
			verifiedAction = value
			if value.Signature != "action-sig" {
				t.Fatalf("action signature = %q, want action-sig", value.Signature)
			}
			return nil
		})),
	)
	if err != nil {
		t.Fatalf("NewCallbackSource() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/lark/callback", strings.NewReader(`{
		"header":{"event_id":"event-verify"},
		"event":{"action":{"value":{"action_id":"approve","action_type":"resume","signature":"action-sig"}}}
	}`))
	request.Header.Set("X-Lark-Signature", "callback-sig")
	source.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body=%q, want 202", response.StatusCode, raw)
	}
	if verifiedCallback.Method != http.MethodPost || verifiedCallback.Path != "/lark/callback" {
		t.Fatalf("verified callback = %+v, want request identity", verifiedCallback)
	}
	if verifiedAction.ActionID != "approve" {
		t.Fatalf("verified action = %+v, want parsed action", verifiedAction)
	}
}

func TestCallbackSourceRejectsUnauthorizedCallback(t *testing.T) {
	source, err := NewCallbackSource(WithCallbackVerifier(CallbackVerifierFunc(func(_ context.Context, _ CallbackRequest) error {
		return ErrCallbackUnauthorized
	})))
	if err != nil {
		t.Fatalf("NewCallbackSource() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/lark/callback", strings.NewReader(`{
		"event":{"action":{"value":{"action_id":"approve","action_type":"resume"}}}
	}`))
	source.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusUnauthorized {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body=%q, want 401", response.StatusCode, raw)
	}
}

func TestCallbackSourceRejectsUnauthorizedAction(t *testing.T) {
	source, err := NewCallbackSource(WithActionVerifier(ActionVerifierFunc(func(_ context.Context, _ ActionValue) error {
		return ErrActionUnauthorized
	})))
	if err != nil {
		t.Fatalf("NewCallbackSource() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/lark/callback", strings.NewReader(`{
		"event":{"action":{"value":{"action_id":"approve","action_type":"resume"}}}
	}`))
	source.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body=%q, want 403", response.StatusCode, raw)
	}
}

func TestChannelRejectsInvalidInputs(t *testing.T) {
	if channel, err := NewChannel(nil); !errors.Is(err, ErrSenderRequired) || channel != nil {
		t.Fatalf("NewChannel(nil) channel=%v err=%v, want ErrSenderRequired", channel, err)
	}
	channel, err := NewChannel(SenderFunc(func(_ context.Context, _ Payload) (SendResult, error) {
		return SendResult{}, nil
	}))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}
	if err := channel.Send(context.Background(), gopact.ChannelPayload{Target: Target, Data: "bad"}); !errors.Is(err, ErrUnsupportedPayload) {
		t.Fatalf("Send(bad payload) error = %v, want ErrUnsupportedPayload", err)
	}
	if _, err := (SenderFunc(nil)).Send(context.Background(), Payload{}); !errors.Is(err, ErrSenderRequired) {
		t.Fatalf("nil SenderFunc error = %v, want ErrSenderRequired", err)
	}
}

func nextEvent(events iter.Seq2[gopact.ChannelEvent, error]) (gopact.ChannelEvent, error) {
	for event, err := range events {
		if err != nil {
			return gopact.ChannelEvent{}, err
		}
		return event, nil
	}
	return gopact.ChannelEvent{}, errors.New("event stream closed")
}
