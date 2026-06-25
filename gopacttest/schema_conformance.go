package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gopact-ai/gopact"
)

var ErrJSONSchemaValidatorConformanceFailed = errors.New("gopacttest: json schema validator conformance failed")

// JSONSchemaValidatorConformanceCase is one portable schema validator contract case.
type JSONSchemaValidatorConformanceCase struct {
	Name      string
	Schema    gopact.JSONSchema
	Value     any
	WantValid bool
}

// JSONSchemaValidatorConformanceResult is the observed result for one case.
type JSONSchemaValidatorConformanceResult struct {
	Case   JSONSchemaValidatorConformanceCase
	Passed bool
	Err    error
}

var portableJSONSchemaValidatorConformanceCases = []JSONSchemaValidatorConformanceCase{
	{
		Name: "accepts-required-object",
		Schema: gopact.JSONSchema{
			"type": "object",
			"required": []any{
				"name",
				"age",
			},
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "number", "minimum": float64(18)},
			},
			"additionalProperties": false,
		},
		Value: map[string]any{
			"name": "ada",
			"age":  float64(36),
		},
		WantValid: true,
	},
	{
		Name: "rejects-required-field",
		Schema: gopact.JSONSchema{
			"type":     "object",
			"required": []any{"name"},
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
		Value:     map[string]any{},
		WantValid: false,
	},
	{
		Name: "rejects-additional-property",
		Schema: gopact.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
		Value: map[string]any{
			"id":    "item-1",
			"extra": true,
		},
		WantValid: false,
	},
	{
		Name: "accepts-array-items-enum",
		Schema: gopact.JSONSchema{
			"type": "array",
			"items": map[string]any{
				"type": "string",
				"enum": []any{"read", "write"},
			},
			"minItems": float64(1),
			"maxItems": float64(3),
		},
		Value:     []any{"read", "write"},
		WantValid: true,
	},
	{
		Name: "rejects-enum-value",
		Schema: gopact.JSONSchema{
			"type": "string",
			"enum": []any{"open", "closed"},
		},
		Value:     "pending",
		WantValid: false,
	},
	{
		Name: "accepts-pattern",
		Schema: gopact.JSONSchema{
			"type":    "string",
			"pattern": "^[a-z]+-[0-9]+$",
		},
		Value:     "task-123",
		WantValid: true,
	},
	{
		Name: "rejects-pattern",
		Schema: gopact.JSONSchema{
			"type":    "string",
			"pattern": "^[a-z]+-[0-9]+$",
		},
		Value:     "Task 123",
		WantValid: false,
	},
	{
		Name: "accepts-exclusive-multiple-of-number",
		Schema: gopact.JSONSchema{
			"type":             "number",
			"exclusiveMinimum": float64(1),
			"exclusiveMaximum": float64(10),
			"multipleOf":       float64(0.5),
		},
		Value:     float64(7.5),
		WantValid: true,
	},
	{
		Name: "rejects-multiple-of-number",
		Schema: gopact.JSONSchema{
			"type":       "number",
			"multipleOf": float64(0.5),
		},
		Value:     float64(7.3),
		WantValid: false,
	},
	{
		Name: "accepts-const",
		Schema: gopact.JSONSchema{
			"const": "approved",
		},
		Value:     "approved",
		WantValid: true,
	},
	{
		Name: "rejects-const",
		Schema: gopact.JSONSchema{
			"const": "approved",
		},
		Value:     "rejected",
		WantValid: false,
	},
}

// PortableJSONSchemaValidatorConformanceCases returns the portable subset cases
// expected at gopact resume and component schema boundaries.
func PortableJSONSchemaValidatorConformanceCases() []JSONSchemaValidatorConformanceCase {
	out := make([]JSONSchemaValidatorConformanceCase, 0, len(portableJSONSchemaValidatorConformanceCases))
	for _, c := range portableJSONSchemaValidatorConformanceCases {
		out = append(out, copyJSONSchemaValidatorConformanceCase(c))
	}
	return out
}

// CheckPortableJSONSchemaValidatorConformance runs the portable schema validator
// contract cases. A nil validator checks gopact's built-in portable validator.
func CheckPortableJSONSchemaValidatorConformance(ctx context.Context, validator gopact.JSONSchemaValidator) []JSONSchemaValidatorConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	cases := PortableJSONSchemaValidatorConformanceCases()
	results := make([]JSONSchemaValidatorConformanceResult, 0, len(cases))
	for _, c := range cases {
		err := gopact.ValidateJSONSchemaValueWith(ctx, validator, c.Schema, c.Value)
		passed := (err == nil) == c.WantValid
		if !passed && err == nil {
			err = fmt.Errorf("%w: case %q accepted invalid value", ErrJSONSchemaValidatorConformanceFailed, c.Name)
		}
		results = append(results, JSONSchemaValidatorConformanceResult{
			Case:   c,
			Passed: passed,
			Err:    err,
		})
	}
	return results
}

// RequirePortableJSONSchemaValidatorConformance fails the test unless validator
// satisfies the portable schema subset used by gopact boundaries.
func RequirePortableJSONSchemaValidatorConformance(t testing.TB, validator gopact.JSONSchemaValidator) {
	t.Helper()

	for _, result := range CheckPortableJSONSchemaValidatorConformance(context.Background(), validator) {
		if !result.Passed {
			t.Fatalf("json schema validator conformance case %q failed: %v", result.Case.Name, result.Err)
		}
	}
}

func copyJSONSchemaValidatorConformanceCase(c JSONSchemaValidatorConformanceCase) JSONSchemaValidatorConformanceCase {
	return JSONSchemaValidatorConformanceCase{
		Name:      c.Name,
		Schema:    copyJSONSchema(c.Schema),
		Value:     copyJSONValue(c.Value),
		WantValid: c.WantValid,
	}
}

func copyJSONSchema(schema gopact.JSONSchema) gopact.JSONSchema {
	if len(schema) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(schema))
	for key, value := range schema {
		out[key] = copyJSONValue(value)
	}
	return out
}

func copyJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = copyJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = copyJSONValue(child)
		}
		return out
	default:
		return value
	}
}
