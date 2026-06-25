package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestValidateJSONSchemaValueSupportsPortableSubset(t *testing.T) {
	schema := JSONSchema{
		"type":                 "object",
		"required":             []any{"ticket", "score", "count"},
		"additionalProperties": false,
		"properties": map[string]any{
			"ticket": map[string]any{
				"type":    "string",
				"pattern": "^APP-[0-9]{3}$",
			},
			"score": map[string]any{
				"type":             "number",
				"exclusiveMinimum": 0,
				"exclusiveMaximum": 1,
			},
			"count": map[string]any{
				"type":       "integer",
				"multipleOf": 5,
			},
		},
	}

	if err := ValidateJSONSchemaValue(schema, map[string]any{
		"ticket": "APP-123",
		"score":  0.5,
		"count":  10,
	}); err != nil {
		t.Fatalf("ValidateJSONSchemaValue(valid payload) error = %v", err)
	}

	tests := []struct {
		name  string
		value any
	}{
		{
			name: "pattern mismatch",
			value: map[string]any{
				"ticket": "app-123",
				"score":  0.5,
				"count":  10,
			},
		},
		{
			name: "exclusive minimum rejects equal",
			value: map[string]any{
				"ticket": "APP-123",
				"score":  0,
				"count":  10,
			},
		},
		{
			name: "exclusive maximum rejects equal",
			value: map[string]any{
				"ticket": "APP-123",
				"score":  1,
				"count":  10,
			},
		},
		{
			name: "multipleOf mismatch",
			value: map[string]any{
				"ticket": "APP-123",
				"score":  0.5,
				"count":  12,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJSONSchemaValue(schema, tt.value)
			if !errors.Is(err, ErrJSONSchemaValidationFailed) {
				t.Fatalf("ValidateJSONSchemaValue() error = %v, want ErrJSONSchemaValidationFailed", err)
			}
		})
	}
}

func TestValidateJSONSchemaValueWithUsesInjectedValidator(t *testing.T) {
	called := false
	ctx := context.WithValue(context.Background(), schemaValidatorTestKey{}, "ctx-1")
	validator := JSONSchemaValidatorFunc(func(ctx context.Context, schema JSONSchema, value any) error {
		called = true
		if got := ctx.Value(schemaValidatorTestKey{}); got != "ctx-1" {
			t.Fatalf("validator context value = %v, want ctx-1", got)
		}
		if schema["$ref"] != "#/$defs/full" {
			t.Fatalf("schema = %+v, want injected $ref schema", schema)
		}
		payload, ok := value.(map[string]any)
		if !ok || payload["answer"] != "yes" {
			t.Fatalf("value = %+v, want answer payload", value)
		}
		return nil
	})

	err := ValidateJSONSchemaValueWith(ctx, validator, JSONSchema{
		"$ref": "#/$defs/full",
	}, map[string]any{"answer": "yes"})
	if err != nil {
		t.Fatalf("ValidateJSONSchemaValueWith() error = %v", err)
	}
	if !called {
		t.Fatal("validator called = false, want true")
	}
}

func TestValidateResumePayloadWithValidatorUsesInjectedValidator(t *testing.T) {
	called := false
	record := InterruptRecord{
		ID:   "interrupt-1",
		Type: InterruptInput,
		ResumeSchema: JSONSchema{
			"$ref": "#/$defs/resume",
		},
	}
	request := ResumeRequest{
		InterruptID: "interrupt-1",
		Payload:     map[string]any{"answer": "approved"},
	}
	validator := JSONSchemaValidatorFunc(func(ctx context.Context, schema JSONSchema, value any) error {
		called = true
		if schema["$ref"] != "#/$defs/resume" {
			t.Fatalf("schema = %+v, want resume ref schema", schema)
		}
		return nil
	})

	err := ValidateResumePayloadWithValidator(context.Background(), validator, record, request)
	if err != nil {
		t.Fatalf("ValidateResumePayloadWithValidator() error = %v", err)
	}
	if !called {
		t.Fatal("validator called = false, want true")
	}
}

type schemaValidatorTestKey struct{}
