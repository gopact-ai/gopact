package graph

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
)

func setNodeSchema(target map[string]gopact.JSONSchema, name string, schema gopact.JSONSchema) {
	if len(schema) == 0 {
		delete(target, name)
		return
	}
	target[name] = copyJSONSchema(schema)
}

func copyNodeSchemas(in map[string]gopact.JSONSchema) map[string]gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]gopact.JSONSchema, len(in))
	for name, schema := range in {
		out[name] = copyJSONSchema(schema)
	}
	return out
}

func copyJSONSchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	return gopact.JSONSchema(copySchemaMap(map[string]any(in)))
}

func copySchemaMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = copySchemaValue(value)
	}
	return out
}

func copySchemaValue(value any) any {
	switch typed := value.(type) {
	case gopact.JSONSchema:
		return copyJSONSchema(typed)
	case map[string]any:
		return copySchemaMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = copySchemaValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case []float64:
		return append([]float64(nil), typed...)
	case []bool:
		return append([]bool(nil), typed...)
	default:
		return typed
	}
}

func (r *Runnable[S]) validateNodeInput(ctx context.Context, validator gopact.JSONSchemaValidator, node string, state S) error {
	if err := validateSchemaGuard(ctx, validator, r.stateSchema, state, fmt.Sprintf("state at node %q input", node)); err != nil {
		return err
	}
	return validateSchemaGuard(ctx, validator, r.nodeInputSchemas[node], state, fmt.Sprintf("node %q input", node))
}

func (r *Runnable[S]) validateNodeOutput(ctx context.Context, validator gopact.JSONSchemaValidator, node string, state S) error {
	if err := validateSchemaGuard(ctx, validator, r.stateSchema, state, fmt.Sprintf("state at node %q output", node)); err != nil {
		return err
	}
	return validateSchemaGuard(ctx, validator, r.nodeOutputSchemas[node], state, fmt.Sprintf("node %q output", node))
}

func (r *Runnable[S]) validateResumedState(ctx context.Context, validator gopact.JSONSchemaValidator, node string, state S) error {
	if err := validateSchemaGuard(ctx, validator, r.stateSchema, state, fmt.Sprintf("resumed state from node %q", node)); err != nil {
		return err
	}
	if node == Start || node == End {
		return nil
	}
	return validateSchemaGuard(ctx, validator, r.nodeOutputSchemas[node], state, fmt.Sprintf("node %q resumed output", node))
}

func validateSchemaGuard(ctx context.Context, validator gopact.JSONSchemaValidator, schema gopact.JSONSchema, value any, label string) error {
	if len(schema) == 0 {
		return nil
	}
	if err := gopact.ValidateJSONSchemaValueWith(ctx, validator, schema, value); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrSchemaGuardFailed, label, err)
	}
	return nil
}
