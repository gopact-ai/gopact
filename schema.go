package gopact

const (
	// RunExportJSONSchemaID is the stable JSON Schema id for RunExport v1.
	RunExportJSONSchemaID = "https://gopact.ai/schemas/run-export/v1.json"

	jsonSchemaDraft202012 = "https://json-schema.org/draft/2020-12/schema"
)

// RunExportJSONSchema returns the portable JSON Schema for RunExport v1.
func RunExportJSONSchema() JSONSchema {
	return JSONSchema(deepCopySchemaMap(runExportJSONSchema))
}

var runExportJSONSchema = map[string]any{
	"$id":                  RunExportJSONSchemaID,
	"$schema":              jsonSchemaDraft202012,
	"title":                "gopact RunExport",
	"type":                 "object",
	"additionalProperties": true,
	"required":             []any{"version", "ids", "outcome"},
	"properties": map[string]any{
		"version": map[string]any{
			"type":  "integer",
			"const": RunExportVersion,
		},
		"ids": map[string]any{
			"$ref": "#/$defs/runtime_ids",
		},
		"outcome": map[string]any{
			"type": "string",
			"enum": []any{string(RunCompleted), string(RunFailed), string(RunCanceled), string(RunInterrupted)},
		},
		"events": map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/$defs/event"},
		},
		"steps": map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/$defs/step_snapshot"},
		},
		"tasks": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "object", "additionalProperties": true},
		},
		"inputs": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "object", "additionalProperties": true},
		},
		"interventions": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "object", "additionalProperties": true},
		},
		"failures": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "object", "additionalProperties": true},
		},
		"entropy_audits": map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/$defs/entropy_audit"},
		},
		"verification_reports": map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/$defs/verification_report"},
		},
		"created_at": map[string]any{
			"type":   "string",
			"format": "date-time",
		},
		"metadata": map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		},
	},
	"$defs": map[string]any{
		"runtime_ids": map[string]any{
			"type":                 "object",
			"required":             []any{"run_id"},
			"additionalProperties": true,
			"properties": map[string]any{
				"user_id":    map[string]any{"type": "string"},
				"session_id": map[string]any{"type": "string"},
				"thread_id":  map[string]any{"type": "string"},
				"run_id":     map[string]any{"type": "string", "minLength": 1},
				"agent_id":   map[string]any{"type": "string"},
				"app_id":     map[string]any{"type": "string"},
				"call_id":    map[string]any{"type": "string"},
				"trace_id":   map[string]any{"type": "string"},
			},
		},
		"event": map[string]any{
			"type":                 "object",
			"required":             []any{"type"},
			"additionalProperties": true,
			"properties": map[string]any{
				"type":       map[string]any{"type": "string", "minLength": 1},
				"ids":        map[string]any{"$ref": "#/$defs/runtime_ids"},
				"run_id":     map[string]any{"type": "string"},
				"thread_id":  map[string]any{"type": "string"},
				"node":       map[string]any{"type": "string"},
				"step":       map[string]any{"type": "integer", "minimum": 0},
				"created_at": map[string]any{"type": "string", "format": "date-time"},
				"metadata":   map[string]any{"type": "object", "additionalProperties": true},
			},
		},
		"step_snapshot": map[string]any{
			"type":                 "object",
			"required":             []any{"id", "step", "node", "phase"},
			"additionalProperties": true,
			"properties": map[string]any{
				"id":           map[string]any{"type": "string", "minLength": 1},
				"step":         map[string]any{"type": "integer", "minimum": 0},
				"node":         map[string]any{"type": "string", "minLength": 1},
				"phase":        map[string]any{"type": "string", "enum": []any{string(StepPending), string(StepRunning), string(StepCompleted), string(StepFailed), string(StepCanceled), string(StepInterrupted)}},
				"ids":          map[string]any{"$ref": "#/$defs/runtime_ids"},
				"queue":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"effects":      map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
				"artifacts":    map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
				"started_at":   map[string]any{"type": "string", "format": "date-time"},
				"completed_at": map[string]any{"type": "string", "format": "date-time"},
				"metadata":     map[string]any{"type": "object", "additionalProperties": true},
			},
		},
		"verification_report": map[string]any{
			"type":                 "object",
			"required":             []any{"version", "ids", "outcome", "status", "checks", "created_at"},
			"additionalProperties": true,
			"properties": map[string]any{
				"version":       map[string]any{"type": "integer", "const": VerificationReportVersion},
				"ids":           map[string]any{"$ref": "#/$defs/runtime_ids"},
				"outcome":       map[string]any{"type": "string"},
				"status":        map[string]any{"type": "string", "enum": []any{string(VerificationStatusPassed), string(VerificationStatusFailed), string(VerificationStatusSkipped), string(VerificationStatusPartial)}},
				"checks":        map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/verification_check"}},
				"passed_count":  map[string]any{"type": "integer", "minimum": 0},
				"failed_count":  map[string]any{"type": "integer", "minimum": 0},
				"skipped_count": map[string]any{"type": "integer", "minimum": 0},
				"created_at":    map[string]any{"type": "string", "format": "date-time"},
				"metadata":      map[string]any{"type": "object", "additionalProperties": true},
			},
		},
		"verification_check": map[string]any{
			"type":                 "object",
			"required":             []any{"id", "status"},
			"additionalProperties": true,
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "minLength": 1},
				"name":     map[string]any{"type": "string"},
				"status":   map[string]any{"type": "string", "enum": []any{string(VerificationStatusPassed), string(VerificationStatusFailed), string(VerificationStatusSkipped)}},
				"summary":  map[string]any{"type": "string"},
				"evidence": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/verification_evidence"}},
				"metadata": map[string]any{"type": "object", "additionalProperties": true},
			},
		},
		"verification_evidence": map[string]any{
			"type":                 "object",
			"required":             []any{"type", "ref"},
			"additionalProperties": true,
			"properties": map[string]any{
				"type":     map[string]any{"type": "string", "minLength": 1},
				"ref":      map[string]any{"type": "string", "minLength": 1},
				"summary":  map[string]any{"type": "string"},
				"metadata": map[string]any{"type": "object", "additionalProperties": true},
			},
		},
		"entropy_audit": map[string]any{
			"type":                 "object",
			"required":             []any{"id", "status"},
			"additionalProperties": true,
			"properties": map[string]any{
				"id":         map[string]any{"type": "string", "minLength": 1},
				"status":     map[string]any{"type": "string"},
				"ids":        map[string]any{"$ref": "#/$defs/runtime_ids"},
				"findings":   map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}},
				"created_at": map[string]any{"type": "string", "format": "date-time"},
				"metadata":   map[string]any{"type": "object", "additionalProperties": true},
			},
		},
	},
}

func deepCopySchemaMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = deepCopySchemaValue(value)
	}
	return out
}

func deepCopySchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopySchemaMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = deepCopySchemaValue(item)
		}
		return out
	default:
		return typed
	}
}
