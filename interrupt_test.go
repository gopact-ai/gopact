package gopact

import (
	"errors"
	"testing"
)

func TestInterruptErrorMatchesErrInterrupted(t *testing.T) {
	record := InterruptRecord{
		ID:     "interrupt-1",
		Type:   InterruptInput,
		Reason: "need input",
	}

	err := Interrupt(record)
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Interrupt() error = %v, want ErrInterrupted", err)
	}

	var interruptErr *InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Interrupt() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.ID != "interrupt-1" || interruptErr.Record.Type != InterruptInput {
		t.Fatalf("interrupt record = %+v", interruptErr.Record)
	}
}

func TestInterruptRecordValidate(t *testing.T) {
	record := InterruptRecord{
		ID:     "interrupt-1",
		Type:   InterruptApproval,
		Reason: "approve tool call",
	}

	if err := record.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestInterruptRecordValidateRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		record InterruptRecord
	}{
		{name: "missing id", record: InterruptRecord{Type: InterruptInput}},
		{name: "missing type", record: InterruptRecord{ID: "interrupt-1"}},
		{name: "invalid type", record: InterruptRecord{ID: "interrupt-1", Type: InterruptType("unknown")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.record.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestValidateResumePayload(t *testing.T) {
	record := InterruptRecord{
		ID:   "interrupt-1",
		Type: InterruptInput,
		ResumeSchema: JSONSchema{
			"type":                 "object",
			"required":             []any{"answer", "confidence"},
			"additionalProperties": false,
			"properties": map[string]any{
				"answer":     map[string]any{"type": "string", "minLength": 1},
				"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				"tags": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
		},
	}

	tests := []struct {
		name    string
		payload any
		wantErr bool
	}{
		{
			name: "valid payload",
			payload: map[string]any{
				"answer":     "continue",
				"confidence": 0.8,
				"tags":       []string{"approved"},
			},
		},
		{
			name: "missing required property",
			payload: map[string]any{
				"answer": "continue",
			},
			wantErr: true,
		},
		{
			name: "wrong property type",
			payload: map[string]any{
				"answer":     42,
				"confidence": 0.8,
			},
			wantErr: true,
		},
		{
			name: "additional property denied",
			payload: map[string]any{
				"answer":     "continue",
				"confidence": 0.8,
				"extra":      true,
			},
			wantErr: true,
		},
		{
			name: "array item type mismatch",
			payload: map[string]any{
				"answer":     "continue",
				"confidence": 0.8,
				"tags":       []any{"approved", 1},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResumePayload(record, ResumeRequest{
				InterruptID: "interrupt-1",
				Payload:     tt.payload,
			})
			if tt.wantErr {
				if !errors.Is(err, ErrResumePayloadInvalid) {
					t.Fatalf("ValidateResumePayload() error = %v, want ErrResumePayloadInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateResumePayload() error = %v", err)
			}
		})
	}
}

func TestValidateResumePayloadAllowsEmptySchema(t *testing.T) {
	record := InterruptRecord{ID: "interrupt-1", Type: InterruptInput}
	err := ValidateResumePayload(record, ResumeRequest{
		InterruptID: "interrupt-1",
		Payload:     map[string]any{"anything": true},
	})
	if err != nil {
		t.Fatalf("ValidateResumePayload() error = %v", err)
	}
}

func TestValidateResumePayloadSupportsSchemaConstraintDepth(t *testing.T) {
	record := InterruptRecord{
		ID:   "interrupt-1",
		Type: InterruptInput,
		ResumeSchema: JSONSchema{
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
		},
	}

	tests := []struct {
		name    string
		payload any
		wantErr bool
	}{
		{
			name: "valid payload",
			payload: map[string]any{
				"ticket": "APP-123",
				"score":  0.5,
				"count":  10,
			},
		},
		{
			name: "pattern mismatch",
			payload: map[string]any{
				"ticket": "app-123",
				"score":  0.5,
				"count":  10,
			},
			wantErr: true,
		},
		{
			name: "exclusive minimum rejects equal",
			payload: map[string]any{
				"ticket": "APP-123",
				"score":  0,
				"count":  10,
			},
			wantErr: true,
		},
		{
			name: "exclusive maximum rejects equal",
			payload: map[string]any{
				"ticket": "APP-123",
				"score":  1,
				"count":  10,
			},
			wantErr: true,
		},
		{
			name: "multipleOf mismatch",
			payload: map[string]any{
				"ticket": "APP-123",
				"score":  0.5,
				"count":  12,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResumePayload(record, ResumeRequest{
				InterruptID: "interrupt-1",
				Payload:     tt.payload,
			})
			if tt.wantErr {
				if !errors.Is(err, ErrResumePayloadInvalid) {
					t.Fatalf("ValidateResumePayload() error = %v, want ErrResumePayloadInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateResumePayload() error = %v", err)
			}
		})
	}
}
