package a2ui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestTransferConvertsSurfaceMessageToA2UIPayload(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	transfer := NewTransfer(WithCatalogID("https://example.com/catalog.json"), WithSendDataModel(true))

	payload, err := transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID: "surface-1",
		IDs: gopact.RuntimeIDs{
			RunID:     "run-1",
			ThreadID:  "thread-1",
			SessionID: "session-1",
			AgentID:   "agent-1",
			UserID:    "user-1",
		},
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
	if payload.Metadata["mime_type"] != MIMEType || payload.Metadata["surface_type"] != string(gopact.SurfaceMessageMessage) {
		t.Fatalf("metadata = %+v, want A2UI mime type and surface type", payload.Metadata)
	}
	got, ok := payload.Data.(Payload)
	if !ok {
		t.Fatalf("payload data type = %T, want a2ui.Payload", payload.Data)
	}
	if got.MIMEType != MIMEType || got.Version != Version || got.SurfaceID != "surface-1" {
		t.Fatalf("payload identity = %+v, want A2UI v0.9 surface payload", got)
	}
	if got.IDs.RunID != "run-1" || got.SourceEvent != string(gopact.EventModelMessage) || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("payload metadata = %+v, want copied surface metadata", got)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages count = %d, want create, components, data model", len(got.Messages))
	}
	if got.Messages[0].Version != Version || got.Messages[0].CreateSurface == nil {
		t.Fatalf("first message = %+v, want createSurface", got.Messages[0])
	}
	if got.Messages[0].CreateSurface.CatalogID != "https://example.com/catalog.json" || !got.Messages[0].CreateSurface.SendDataModel {
		t.Fatalf("createSurface = %+v, want configured catalog and data model flag", got.Messages[0].CreateSurface)
	}
	components := got.Messages[1].UpdateComponents.Components
	if len(components) < 2 {
		t.Fatalf("components count = %d, want root and body components", len(components))
	}
	if components[0].ID != "root" || components[0].Component != "Column" {
		t.Fatalf("root component = %+v, want Column root", components[0])
	}
	if components[1].ID != "body" || components[1].Component != "Text" {
		t.Fatalf("body component = %+v, want Text body", components[1])
	}
	textBinding, ok := components[1].Text.(map[string]any)
	if !ok || textBinding["path"] != "/text" {
		t.Fatalf("body text binding = %+v, want path /text", components[1].Text)
	}
	model := got.Messages[2].UpdateDataModel
	if model == nil || model.Path != "/" {
		t.Fatalf("data model message = %+v, want root updateDataModel", got.Messages[2])
	}
	modelValue, ok := model.Value.(map[string]any)
	if !ok {
		t.Fatalf("data model value type = %T, want map", model.Value)
	}
	if modelValue["text"] != "hello\nworking" {
		t.Fatalf("data model text = %q, want rendered text", modelValue["text"])
	}
	ids, ok := modelValue["ids"].(gopact.RuntimeIDs)
	if !ok || ids.RunID != "run-1" || ids.SessionID != "session-1" {
		t.Fatalf("data model ids = %+v, want runtime ids", modelValue["ids"])
	}
}

func TestTransferConvertsActionsToA2UIButtons(t *testing.T) {
	actionPayload := map[string]any{"approved": true}
	payload, err := NewTransfer().Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-approval",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type: gopact.SurfaceMessageApproval,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "approve repo.write?"},
		},
		Actions: []gopact.SurfaceAction{{
			ID:          "approval-1",
			Type:        gopact.SurfaceActionResume,
			Label:       "Approve",
			IDs:         gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
			InterruptID: "interrupt-1",
			CallID:      "call-1",
			Payload:     actionPayload,
			Metadata:    map[string]any{"risk": "write"},
		}},
		Artifacts: []gopact.ArtifactRef{{ID: "artifact-1", Name: "patch.diff", URI: "file:///tmp/patch.diff"}},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	components := got.Messages[1].UpdateComponents.Components

	var button Component
	for _, component := range components {
		if component.ID == "action_0" {
			button = component
			break
		}
	}
	if button.ID == "" {
		t.Fatalf("components = %+v, want action button", components)
	}
	if button.Component != "Button" || button.Child != "action_0_label" {
		t.Fatalf("button component = %+v, want Button with label child", button)
	}
	if button.Action == nil || button.Action.Event == nil {
		t.Fatalf("button action = %+v, want event binding", button.Action)
	}
	if button.Action.Event.Name != "approval-1" {
		t.Fatalf("event name = %q, want action id", button.Action.Event.Name)
	}
	context := button.Action.Event.Context
	if context["action_id"] != "approval-1" ||
		context["action_type"] != string(gopact.SurfaceActionResume) ||
		context["interrupt_id"] != "interrupt-1" ||
		context["call_id"] != "call-1" {
		t.Fatalf("action context = %+v, want action identity", context)
	}
	if !reflect.DeepEqual(context["payload"], actionPayload) {
		t.Fatalf("action context payload = %+v, want action payload", context["payload"])
	}
	if metadata, ok := context["metadata"].(map[string]any); !ok || metadata["risk"] != "write" {
		t.Fatalf("action context metadata = %+v, want action metadata", context["metadata"])
	}
}

func TestCatalogRegistrySelectsClientPreferredCatalog(t *testing.T) {
	registry, err := NewCatalogRegistry(
		NewCatalog("basic", "Column", "Text", "Button"),
		NewCatalog("custom", "Panel"),
	)
	if err != nil {
		t.Fatalf("NewCatalogRegistry() error = %v", err)
	}

	catalog, ok := registry.Select([]string{"custom", "basic"})
	if !ok {
		t.Fatal("Select() ok = false, want true")
	}
	if catalog.ID != "custom" {
		t.Fatalf("selected catalog = %q, want custom", catalog.ID)
	}

	_, ok = registry.Select([]string{"missing"})
	if ok {
		t.Fatal("Select(missing) ok = true, want false")
	}
}

func TestTransferNegotiatesCatalogFromClientSupportedCatalogs(t *testing.T) {
	registry, err := NewCatalogRegistry(
		NewCatalog("basic", "Column", "Text", "Button"),
		NewCatalog("custom", "Column", "Text", "Button"),
	)
	if err != nil {
		t.Fatalf("NewCatalogRegistry() error = %v", err)
	}
	transfer := NewTransfer(
		WithCatalogID("basic"),
		WithCatalogRegistry(registry),
		WithClientSupportedCatalogs("missing", "custom", "basic"),
	)

	payload, err := transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-1",
		Type: gopact.SurfaceMessageMessage,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	if got.Messages[0].CreateSurface.CatalogID != "custom" {
		t.Fatalf("negotiated catalog = %q, want custom", got.Messages[0].CreateSurface.CatalogID)
	}
	if got.Metadata["catalog_id"] != "custom" {
		t.Fatalf("payload metadata catalog_id = %+v, want custom", got.Metadata["catalog_id"])
	}
}

func TestTransferFallsBackToConfiguredCatalogWhenNegotiationMisses(t *testing.T) {
	registry, err := NewCatalogRegistry(NewCatalog("basic", "Column", "Text", "Button"))
	if err != nil {
		t.Fatalf("NewCatalogRegistry() error = %v", err)
	}
	transfer := NewTransfer(
		WithCatalogID("basic"),
		WithCatalogRegistry(registry),
		WithClientSupportedCatalogs("missing"),
	)

	payload, err := transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-1",
		Type: gopact.SurfaceMessageMessage,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	if got.Messages[0].CreateSurface.CatalogID != "basic" {
		t.Fatalf("catalog = %q, want configured fallback basic", got.Messages[0].CreateSurface.CatalogID)
	}
}

func TestValidatorRejectsUnknownComponentsAndBrokenReferences(t *testing.T) {
	validator, err := NewValidator(NewCatalog("basic", "Column", "Text"))
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	err = validator.ValidateMessages([]Message{
		{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID: "surface-1",
				CatalogID: "basic",
			},
		},
		{
			Version: Version,
			UpdateComponents: &UpdateComponents{
				SurfaceID: "surface-1",
				Components: []Component{
					{ID: "root", Component: "Column", Children: []string{"body", "missing"}},
					{ID: "body", Component: "Text"},
					{ID: "bad", Component: "Button"},
				},
			},
		},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("ValidateMessages() error = %v, want ErrValidationFailed", err)
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("ValidateMessages() error type = %T, want *ValidationError", err)
	}
	if validationErr.Component != "Button" || validationErr.ComponentID != "bad" {
		t.Fatalf("validation error = %+v, want unknown Button component on bad", validationErr)
	}
}

func TestValidatorRemembersSurfaceCatalogForIncrementalUpdates(t *testing.T) {
	validator, err := NewValidator(NewCatalog("basic", "Column", "Text"))
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	if err := validator.ValidateMessages([]Message{{
		Version: Version,
		CreateSurface: &CreateSurface{
			SurfaceID: "surface-1",
			CatalogID: "basic",
		},
	}}); err != nil {
		t.Fatalf("ValidateMessages(create) error = %v", err)
	}

	err = validator.ValidateMessages([]Message{{
		Version: Version,
		UpdateComponents: &UpdateComponents{
			SurfaceID: "surface-1",
			Components: []Component{
				{ID: "root", Component: "Column", Children: []string{"body"}},
				{ID: "body", Component: "Text"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("ValidateMessages(incremental update) error = %v", err)
	}
}

func TestValidatorAppliesComponentJSONSchema(t *testing.T) {
	validator, err := NewValidator(Catalog{
		ID: "basic",
		Components: map[string]ComponentSchema{
			"Column": {Name: "Column"},
			"Text": {
				Name: "Text",
				Schema: gopact.JSONSchema{
					"type":                 "object",
					"required":             []any{"id", "component", "text"},
					"additionalProperties": false,
					"properties": map[string]any{
						"id":        map[string]any{"type": "string", "minLength": 1},
						"component": map[string]any{"const": "Text"},
						"text": map[string]any{
							"type":                 "object",
							"required":             []any{"path"},
							"additionalProperties": false,
							"properties": map[string]any{
								"path": map[string]any{"type": "string", "minLength": 1},
							},
						},
						"variant": map[string]any{"enum": []any{"body", "h1"}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	if err := validator.ValidateMessages([]Message{
		{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID: "surface-1",
				CatalogID: "basic",
			},
		},
		{
			Version: Version,
			UpdateComponents: &UpdateComponents{
				SurfaceID: "surface-1",
				Components: []Component{
					{ID: "root", Component: "Column", Children: []string{"body"}},
					{ID: "body", Component: "Text", Text: map[string]any{"path": "/text"}, Variant: "body"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("ValidateMessages(valid schema component) error = %v", err)
	}

	err = validator.ValidateMessages([]Message{{
		Version: Version,
		UpdateComponents: &UpdateComponents{
			SurfaceID: "surface-1",
			Components: []Component{
				{ID: "body", Component: "Text", Text: map[string]any{"missing": "/text"}, Variant: "caption"},
			},
		},
	}})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("ValidateMessages(invalid schema component) error = %v, want ErrValidationFailed", err)
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("ValidateMessages(invalid schema component) error type = %T, want *ValidationError", err)
	}
	if validationErr.ComponentID != "body" || validationErr.Component != "Text" {
		t.Fatalf("validation error = %+v, want Text body schema failure", validationErr)
	}
}

func TestValidatorAppliesComponentJSONSchemaBounds(t *testing.T) {
	validator, err := NewValidator(Catalog{
		ID: "basic",
		Components: map[string]ComponentSchema{
			"Badge": {
				Name: "Badge",
				Schema: gopact.JSONSchema{
					"type":     "object",
					"required": []any{"id", "component", "text", "metadata"},
					"properties": map[string]any{
						"id":        map[string]any{"type": "string", "minLength": 1},
						"component": map[string]any{"const": "Badge"},
						"text":      map[string]any{"type": "string", "maxLength": 5},
						"metadata": map[string]any{
							"type":     "object",
							"required": []any{"tags", "priority"},
							"properties": map[string]any{
								"tags": map[string]any{
									"type":     "array",
									"minItems": 1,
									"maxItems": 2,
									"items": map[string]any{
										"type":      "string",
										"maxLength": 4,
									},
								},
								"priority": map[string]any{
									"type":    "integer",
									"maximum": 3,
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	if err := validator.ValidateMessages([]Message{
		{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID: "surface-1",
				CatalogID: "basic",
			},
		},
		{
			Version: Version,
			UpdateComponents: &UpdateComponents{
				SurfaceID: "surface-1",
				Components: []Component{
					{
						ID:        "badge",
						Component: "Badge",
						Text:      "short",
						Metadata:  map[string]any{"tags": []any{"ok"}, "priority": 3},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("ValidateMessages(valid bounded schema component) error = %v", err)
	}

	tests := []struct {
		name      string
		component Component
	}{
		{
			name: "text exceeds max length",
			component: Component{
				ID:        "badge",
				Component: "Badge",
				Text:      "too long",
				Metadata:  map[string]any{"tags": []any{"ok"}, "priority": 1},
			},
		},
		{
			name: "priority exceeds maximum",
			component: Component{
				ID:        "badge",
				Component: "Badge",
				Text:      "short",
				Metadata:  map[string]any{"tags": []any{"ok"}, "priority": 4},
			},
		},
		{
			name: "tags below min items",
			component: Component{
				ID:        "badge",
				Component: "Badge",
				Text:      "short",
				Metadata:  map[string]any{"tags": []any{}, "priority": 1},
			},
		},
		{
			name: "tags above max items",
			component: Component{
				ID:        "badge",
				Component: "Badge",
				Text:      "short",
				Metadata:  map[string]any{"tags": []any{"a", "b", "c"}, "priority": 1},
			},
		},
		{
			name: "tag exceeds item max length",
			component: Component{
				ID:        "badge",
				Component: "Badge",
				Text:      "short",
				Metadata:  map[string]any{"tags": []any{"longer"}, "priority": 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateMessages([]Message{{
				Version: Version,
				UpdateComponents: &UpdateComponents{
					SurfaceID:  "surface-1",
					Components: []Component{tt.component},
				},
			}})
			if !errors.Is(err, ErrValidationFailed) {
				t.Fatalf("ValidateMessages() error = %v, want ErrValidationFailed", err)
			}
		})
	}
}

func TestValidatorAppliesComponentJSONSchemaPortableSubset(t *testing.T) {
	validator, err := NewValidator(Catalog{
		ID: "basic",
		Components: map[string]ComponentSchema{
			"Badge": {
				Name: "Badge",
				Schema: gopact.JSONSchema{
					"type":     "object",
					"required": []any{"id", "component", "text", "metadata"},
					"properties": map[string]any{
						"id":        map[string]any{"type": "string", "pattern": "^badge-[0-9]+$"},
						"component": map[string]any{"const": "Badge"},
						"text":      map[string]any{"type": "string", "pattern": "^[A-Z]+$"},
						"metadata": map[string]any{
							"type":     "object",
							"required": []any{"priority", "step"},
							"properties": map[string]any{
								"priority": map[string]any{
									"type":             "number",
									"exclusiveMinimum": 0,
									"exclusiveMaximum": 10,
								},
								"step": map[string]any{
									"type":       "integer",
									"multipleOf": 5,
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	if err := validator.ValidateMessages([]Message{
		{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID: "surface-1",
				CatalogID: "basic",
			},
		},
		{
			Version: Version,
			UpdateComponents: &UpdateComponents{
				SurfaceID: "surface-1",
				Components: []Component{
					{
						ID:        "badge-123",
						Component: "Badge",
						Text:      "READY",
						Metadata:  map[string]any{"priority": 5, "step": 10},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("ValidateMessages(valid portable subset component) error = %v", err)
	}

	tests := []struct {
		name      string
		component Component
	}{
		{
			name: "id pattern mismatch",
			component: Component{
				ID:        "badge-x",
				Component: "Badge",
				Text:      "READY",
				Metadata:  map[string]any{"priority": 5, "step": 10},
			},
		},
		{
			name: "text pattern mismatch",
			component: Component{
				ID:        "badge-123",
				Component: "Badge",
				Text:      "ready",
				Metadata:  map[string]any{"priority": 5, "step": 10},
			},
		},
		{
			name: "priority equals exclusive minimum",
			component: Component{
				ID:        "badge-123",
				Component: "Badge",
				Text:      "READY",
				Metadata:  map[string]any{"priority": 0, "step": 10},
			},
		},
		{
			name: "priority equals exclusive maximum",
			component: Component{
				ID:        "badge-123",
				Component: "Badge",
				Text:      "READY",
				Metadata:  map[string]any{"priority": 10, "step": 10},
			},
		},
		{
			name: "step multipleOf mismatch",
			component: Component{
				ID:        "badge-123",
				Component: "Badge",
				Text:      "READY",
				Metadata:  map[string]any{"priority": 5, "step": 12},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateMessages([]Message{{
				Version: Version,
				UpdateComponents: &UpdateComponents{
					SurfaceID:  "surface-1",
					Components: []Component{tt.component},
				},
			}})
			if !errors.Is(err, ErrValidationFailed) {
				t.Fatalf("ValidateMessages() error = %v, want ErrValidationFailed", err)
			}
		})
	}
}

func TestTransferValidatesGeneratedPayloadAgainstCatalog(t *testing.T) {
	validator, err := NewValidator(NewCatalog("basic", "Column", "Text"))
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	transfer := NewTransfer(WithCatalogID("basic"), WithValidator(validator))

	_, err = transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-approval",
		Type: gopact.SurfaceMessageApproval,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "approve?"},
		},
		Actions: []gopact.SurfaceAction{{ID: "approve", Type: gopact.SurfaceActionResume}},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("Convert() error = %v, want ErrValidationFailed", err)
	}
}

func TestValidatorUsesInjectedComponentSchemaValidator(t *testing.T) {
	called := false
	validator, err := NewValidatorWithConfig(ValidatorConfig{
		Catalogs: []Catalog{
			{
				ID: "basic",
				Components: map[string]ComponentSchema{
					"Text": {
						Name: "Text",
						Schema: gopact.JSONSchema{
							"$ref": "#/$defs/text",
						},
					},
				},
			},
		},
		SchemaValidator: gopact.JSONSchemaValidatorFunc(func(_ context.Context, schema gopact.JSONSchema, value any) error {
			called = true
			if schema["$ref"] != "#/$defs/text" {
				t.Fatalf("schema = %+v, want text ref schema", schema)
			}
			component, ok := value.(map[string]any)
			if !ok || component["component"] != "Text" || component["id"] != "body" {
				t.Fatalf("value = %+v, want normalized Text component", value)
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewValidatorWithConfig() error = %v", err)
	}

	err = validator.ValidateMessages([]Message{
		{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID: "surface-1",
				CatalogID: "basic",
			},
		},
		{
			Version: Version,
			UpdateComponents: &UpdateComponents{
				SurfaceID: "surface-1",
				Components: []Component{
					{ID: "body", Component: "Text", Text: "hello"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateMessages() error = %v", err)
	}
	if !called {
		t.Fatal("validator called = false, want true")
	}
}

func TestRendererAppliesMessagesIntoSurfaceSnapshot(t *testing.T) {
	renderer, err := NewRenderer(NewCatalog("basic", "Column", "Text"))
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	err = renderer.Apply(
		Message{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID:     "surface-1",
				CatalogID:     "basic",
				SendDataModel: true,
			},
		},
		Message{
			Version: Version,
			UpdateComponents: &UpdateComponents{
				SurfaceID: "surface-1",
				Components: []Component{
					{ID: "root", Component: "Column", Children: []string{"body"}},
					{ID: "body", Component: "Text", Text: map[string]any{"path": "/text"}},
				},
			},
		},
		Message{
			Version: Version,
			UpdateDataModel: &UpdateDataModel{
				SurfaceID: "surface-1",
				Path:      "/",
				Value:     map[string]any{"text": "hello"},
			},
		},
		Message{
			Version: Version,
			UpdateDataModel: &UpdateDataModel{
				SurfaceID: "surface-1",
				Path:      "/status",
				Value:     "ready",
			},
		},
	)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	surface, ok := renderer.Surface("surface-1")
	if !ok {
		t.Fatal("Surface(surface-1) ok = false, want true")
	}
	if surface.ID != "surface-1" || surface.CatalogID != "basic" || !surface.SendDataModel {
		t.Fatalf("surface identity = %+v, want createSurface metadata", surface)
	}
	if len(surface.Components) != 2 || surface.Components["root"].Children[0] != "body" {
		t.Fatalf("surface components = %+v, want root/body snapshot", surface.Components)
	}
	model, ok := surface.DataModel.(map[string]any)
	if !ok || model["text"] != "hello" || model["status"] != "ready" {
		t.Fatalf("surface data model = %+v, want merged model", surface.DataModel)
	}
	model["text"] = "mutated"
	surface.Components["root"] = Component{ID: "root", Component: "Text"}
	again, ok := renderer.Surface("surface-1")
	if !ok {
		t.Fatal("Surface(surface-1) second ok = false, want true")
	}
	againModel := again.DataModel.(map[string]any)
	if againModel["text"] != "hello" || again.Components["root"].Component != "Column" {
		t.Fatalf("Surface() returned mutable state = %+v %+v", againModel, again.Components["root"])
	}
}

func TestRendererDeletesSurfaceAndRejectsUnknownSurfaceUpdates(t *testing.T) {
	renderer, err := NewRenderer(NewCatalog("basic", "Column"))
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	if err := renderer.Apply(Message{
		Version: Version,
		CreateSurface: &CreateSurface{
			SurfaceID: "surface-1",
			CatalogID: "basic",
		},
	}); err != nil {
		t.Fatalf("Apply(create) error = %v", err)
	}
	if err := renderer.Apply(Message{
		Version: Version,
		DeleteSurface: &DeleteSurface{
			SurfaceID: "surface-1",
		},
	}); err != nil {
		t.Fatalf("Apply(delete) error = %v", err)
	}
	if _, ok := renderer.Surface("surface-1"); ok {
		t.Fatal("Surface(surface-1) ok = true after delete, want false")
	}

	err = renderer.Apply(Message{
		Version: Version,
		UpdateDataModel: &UpdateDataModel{
			SurfaceID: "missing",
			Path:      "/",
			Value:     map[string]any{"text": "orphan"},
		},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("Apply(orphan update) error = %v, want ErrValidationFailed", err)
	}
}

func TestChannelEventFromAction(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)
	actionPayload := map[string]any{"approved": true}
	action := Action{
		Name:              "approval-1",
		SurfaceID:         "surface-approval",
		SourceComponentID: "action_0",
		Timestamp:         createdAt,
		Context: map[string]any{
			"action_id":    "approval-1",
			"action_type":  string(gopact.SurfaceActionResume),
			"interrupt_id": "interrupt-1",
			"call_id":      "call-1",
			"ids":          gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			"payload":      actionPayload,
			"metadata":     map[string]any{"risk": "write"},
		},
	}

	event := ChannelEventFromAction("event-1", action, createdAt)
	if event.ID != "event-1" || event.Channel != Target || event.Type != gopact.ChannelEventAction {
		t.Fatalf("event identity = %+v, want A2UI action event", event)
	}
	if event.Action.ID != "approval-1" || event.Action.Type != gopact.SurfaceActionResume || event.Action.InterruptID != "interrupt-1" {
		t.Fatalf("event action = %+v, want resume action", event.Action)
	}
	if event.Metadata["a2ui_surface_id"] != "surface-approval" || event.Metadata["a2ui_source_component_id"] != "action_0" {
		t.Fatalf("event metadata = %+v, want A2UI surface/component identity", event.Metadata)
	}
	if metadata, ok := event.Action.Metadata["metadata"].(map[string]any); !ok || metadata["risk"] != "write" {
		t.Fatalf("event action metadata = %+v, want action metadata", event.Action.Metadata)
	}
	resume, ok := event.ResumeRequest()
	if !ok {
		t.Fatal("ResumeRequest() ok = false, want true")
	}
	if resume.InterruptID != "interrupt-1" || resume.IDs.RunID != "run-1" || !reflect.DeepEqual(resume.Payload, actionPayload) {
		t.Fatalf("resume = %+v, want action identity and payload", resume)
	}
}

func TestChannelEventFromActionDecodesJSONContextIDs(t *testing.T) {
	timestamp := time.Date(2026, 6, 24, 12, 35, 0, 0, time.UTC)
	action := Action{
		Name:              "resume",
		SurfaceID:         "surface-approval",
		SourceComponentID: "action_0",
		Timestamp:         timestamp,
		Context: map[string]any{
			"action_type":  string(gopact.SurfaceActionResume),
			"interrupt_id": "interrupt-1",
			"ids": map[string]any{
				"user_id":    "user-1",
				"session_id": "session-1",
				"thread_id":  "thread-1",
				"run_id":     "run-1",
				"agent_id":   "agent-1",
				"call_id":    "call-1",
			},
		},
	}

	event := ChannelEventFromAction("event-1", action, time.Time{})
	if !event.CreatedAt.Equal(timestamp) {
		t.Fatalf("CreatedAt = %v, want action timestamp", event.CreatedAt)
	}
	if event.IDs.RunID != "run-1" || event.IDs.SessionID != "session-1" || event.IDs.CallID != "call-1" {
		t.Fatalf("event ids = %+v, want decoded JSON ids", event.IDs)
	}
}

func TestChannelWritesPayloadMessagesAsJSONLines(t *testing.T) {
	var out strings.Builder
	channel, err := NewChannel(&out)
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}
	payload, err := NewTransfer().Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1"},
		Type: gopact.SurfaceMessageMessage,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	if err := channel.Send(context.Background(), payload); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	lines := nonEmptyLines(out.String())
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want one JSONL frame per A2UI message", len(lines))
	}
	var first Message
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first JSONL frame decode error = %v", err)
	}
	if first.Version != Version || first.CreateSurface == nil || first.CreateSurface.SurfaceID != "surface-1" {
		t.Fatalf("first frame = %+v, want createSurface message", first)
	}
	var third Message
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Fatalf("third JSONL frame decode error = %v", err)
	}
	if third.UpdateDataModel == nil || third.UpdateDataModel.Path != "/" {
		t.Fatalf("third frame = %+v, want updateDataModel message", third)
	}
}

func TestHistoryRecordsAndReplaysChannelMessages(t *testing.T) {
	var out strings.Builder
	history := NewHistory()
	channel, err := NewChannel(&out, WithHistory(history))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}
	payload, err := NewTransfer().Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1"},
		Type: gopact.SurfaceMessageMessage,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	if err := channel.Send(context.Background(), payload); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	messages := history.Messages()
	if len(messages) != 3 {
		t.Fatalf("history message count = %d, want one entry per A2UI message", len(messages))
	}
	messages[0].Version = "mutated"
	if got := history.Messages()[0].Version; got != Version {
		t.Fatalf("history returned mutable backing message version = %q, want %q", got, Version)
	}
	model, ok := messages[2].UpdateDataModel.Value.(map[string]any)
	if !ok {
		t.Fatalf("history data model type = %T, want map", messages[2].UpdateDataModel.Value)
	}
	model["text"] = "mutated"
	snapshotModel, ok := history.Messages()[2].UpdateDataModel.Value.(map[string]any)
	if !ok || snapshotModel["text"] != "hello" {
		t.Fatalf("history returned mutable data model = %+v, want original text", snapshotModel)
	}

	var replay strings.Builder
	if err := history.Replay(context.Background(), &replay); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if got, want := nonEmptyLines(replay.String()), nonEmptyLines(out.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed lines = %v, want sent lines %v", got, want)
	}
}

func TestHistoryLimitKeepsMostRecentMessages(t *testing.T) {
	history := NewHistory(WithHistoryLimit(2))

	err := history.Record(gopact.ChannelPayload{Target: Target, Data: []Message{
		{Version: "one"},
		{Version: "two"},
		{Version: "three"},
	}})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	messages := history.Messages()
	if len(messages) != 2 || messages[0].Version != "two" || messages[1].Version != "three" {
		t.Fatalf("history messages = %+v, want two most recent messages", messages)
	}
}

func TestHistoryRejectsInvalidPayloadAndSkipsFailedSend(t *testing.T) {
	history := NewHistory()
	if err := history.Record(gopact.ChannelPayload{Target: Target, Data: "bad"}); !errors.Is(err, ErrUnsupportedPayload) {
		t.Fatalf("Record(bad payload) error = %v, want ErrUnsupportedPayload", err)
	}

	wantErr := errors.New("write failed")
	channel, err := NewChannel(errorWriter{err: wantErr}, WithHistory(history))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}
	err = channel.Send(context.Background(), gopact.ChannelPayload{Target: Target, Data: Message{Version: Version}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Send() error = %v, want %v", err, wantErr)
	}
	if messages := history.Messages(); len(messages) != 0 {
		t.Fatalf("history messages after failed send = %+v, want none", messages)
	}
}

func TestChannelValidatesBeforeWriteAndHistoryRecord(t *testing.T) {
	validator, err := NewValidator(NewCatalog("basic", "Column"))
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	history := NewHistory()
	var out strings.Builder
	channel, err := NewChannel(&out, WithChannelValidator(validator), WithHistory(history))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}

	err = channel.Send(context.Background(), gopact.ChannelPayload{
		Target: Target,
		Data: []Message{
			{
				Version: Version,
				CreateSurface: &CreateSurface{
					SurfaceID: "surface-1",
					CatalogID: "basic",
				},
			},
			{
				Version: Version,
				UpdateComponents: &UpdateComponents{
					SurfaceID: "surface-1",
					Components: []Component{
						{ID: "body", Component: "Text"},
					},
				},
			},
		},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("Send() error = %v, want ErrValidationFailed", err)
	}
	if out.String() != "" {
		t.Fatalf("writer output = %q, want empty when validation fails", out.String())
	}
	if messages := history.Messages(); len(messages) != 0 {
		t.Fatalf("history messages = %+v, want none when validation fails", messages)
	}
}

func TestChannelYieldsActionEventsFromJSONLines(t *testing.T) {
	timestamp := time.Date(2026, 6, 24, 13, 0, 0, 0, time.UTC)
	actionLine := `{"version":"v0.9","action":{"name":"approval-1","surfaceId":"surface-approval","sourceComponentId":"action_0","timestamp":"` +
		timestamp.Format(time.RFC3339) +
		`","context":{"action_id":"approval-1","action_type":"resume","interrupt_id":"interrupt-1","ids":{"run_id":"run-1","thread_id":"thread-1"},"payload":{"approved":true}}}}` + "\n"
	channel, err := NewChannel(io.Discard, WithActionReader(strings.NewReader(actionLine)))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}

	events, err := collectChannelEvents(channel.Events(context.Background()))
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events count = %d, want one action event", len(events))
	}
	event := events[0]
	if event.Channel != Target || event.Action.ID != "approval-1" || event.Action.InterruptID != "interrupt-1" {
		t.Fatalf("event = %+v, want decoded A2UI action", event)
	}
	if !event.CreatedAt.Equal(timestamp) || event.IDs.RunID != "run-1" {
		t.Fatalf("event time/ids = %+v %v, want action timestamp and runtime ids", event.IDs, event.CreatedAt)
	}
	resume, ok := event.ResumeRequest()
	if !ok {
		t.Fatal("ResumeRequest() ok = false, want true")
	}
	payloadMap, ok := resume.Payload.(map[string]any)
	if !ok || payloadMap["approved"] != true {
		t.Fatalf("resume payload = %+v, want approved payload", resume.Payload)
	}
}

func TestChannelRejectsInvalidInputsAndClosedSend(t *testing.T) {
	if channel, err := NewChannel(nil); !errors.Is(err, ErrWriterRequired) || channel != nil {
		t.Fatalf("NewChannel(nil) channel=%v err=%v, want ErrWriterRequired", channel, err)
	}

	channel, err := NewChannel(io.Discard)
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}
	if err := channel.Send(context.Background(), gopact.ChannelPayload{Target: Target, Data: "bad"}); !errors.Is(err, ErrUnsupportedPayload) {
		t.Fatalf("Send(bad payload) error = %v, want ErrUnsupportedPayload", err)
	}
	if err := channel.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := channel.Send(context.Background(), gopact.ChannelPayload{Target: Target, Data: Message{Version: Version}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send(closed) error = %v, want ErrClosed", err)
	}
}

func TestTransferSupportsA2UITarget(t *testing.T) {
	transfer := NewTransfer()
	if transfer.Name() != "a2ui" {
		t.Fatalf("Name() = %q, want a2ui", transfer.Name())
	}
	if !transfer.Supports("") || !transfer.Supports(Target) || transfer.Supports("lark") {
		t.Fatalf("Supports() did not honor A2UI target")
	}
}

func nonEmptyLines(value string) []string {
	scanner := bufio.NewScanner(strings.NewReader(value))
	lines := []string(nil)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func collectChannelEvents(seq iter.Seq2[gopact.ChannelEvent, error]) ([]gopact.ChannelEvent, error) {
	events := []gopact.ChannelEvent(nil)
	for event, err := range seq {
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
