package gopact

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRunExportJSONSchemaDefinesStableCoreContract(t *testing.T) {
	schema := RunExportJSONSchema()
	if schema["$id"] != "https://gopact.ai/schemas/run-export/v1.json" {
		t.Fatalf("schema id = %#v, want run export v1 id", schema["$id"])
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("schema draft = %#v, want draft 2020-12", schema["$schema"])
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type = %#v, want object", schema["type"])
	}
	if !reflect.DeepEqual(schema["required"], []any{"version", "ids", "outcome"}) {
		t.Fatalf("schema required = %#v, want version/ids/outcome", schema["required"])
	}

	properties := schemaMap(t, schema, "properties")
	version := schemaMap(t, properties, "version")
	if version["const"] != float64(RunExportVersion) && version["const"] != RunExportVersion {
		t.Fatalf("version const = %#v, want %d", version["const"], RunExportVersion)
	}
	outcome := schemaMap(t, properties, "outcome")
	if !reflect.DeepEqual(outcome["enum"], []any{string(RunCompleted), string(RunFailed), string(RunCanceled), string(RunInterrupted)}) {
		t.Fatalf("outcome enum = %#v, want run outcomes", outcome["enum"])
	}
	if got := schemaMap(t, properties, "ids")["$ref"]; got != "#/$defs/runtime_ids" {
		t.Fatalf("ids ref = %#v, want runtime_ids ref", got)
	}
	if got := schemaMap(t, schemaMap(t, properties, "steps"), "items")["$ref"]; got != "#/$defs/step_snapshot" {
		t.Fatalf("steps item ref = %#v, want step_snapshot ref", got)
	}
	if got := schemaMap(t, schemaMap(t, properties, "events"), "items")["$ref"]; got != "#/$defs/event" {
		t.Fatalf("events item ref = %#v, want event ref", got)
	}

	defs := schemaMap(t, schema, "$defs")
	for _, name := range []string{"runtime_ids", "event", "step_snapshot", "verification_report", "entropy_audit"} {
		if _, ok := defs[name]; !ok {
			t.Fatalf("$defs missing %q in %#v", name, defs)
		}
	}
}

func TestRunExportJSONSchemaReturnsDeepCopy(t *testing.T) {
	first := RunExportJSONSchema()
	first["$id"] = "mutated"
	properties := schemaMap(t, first, "properties")
	schemaMap(t, properties, "outcome")["enum"] = []any{"mutated"}
	defs := schemaMap(t, first, "$defs")
	delete(defs, "event")

	second := RunExportJSONSchema()
	if second["$id"] != "https://gopact.ai/schemas/run-export/v1.json" {
		t.Fatalf("schema id after mutation = %#v, want original", second["$id"])
	}
	secondProperties := schemaMap(t, second, "properties")
	if !reflect.DeepEqual(schemaMap(t, secondProperties, "outcome")["enum"], []any{string(RunCompleted), string(RunFailed), string(RunCanceled), string(RunInterrupted)}) {
		t.Fatalf("outcome enum after mutation = %#v, want original", schemaMap(t, secondProperties, "outcome")["enum"])
	}
	if _, ok := schemaMap(t, second, "$defs")["event"]; !ok {
		t.Fatal("schema defs lost event after caller mutation")
	}
}

func TestRunExportJSONSchemaMarshalsToJSON(t *testing.T) {
	if _, err := json.Marshal(RunExportJSONSchema()); err != nil {
		t.Fatalf("Marshal(RunExportJSONSchema()) error = %v", err)
	}
}

func schemaMap(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := values[key]
	if !ok {
		t.Fatalf("missing schema key %q in %#v", key, values)
	}
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("schema key %q type = %T, want map[string]any", key, value)
	}
	return out
}
