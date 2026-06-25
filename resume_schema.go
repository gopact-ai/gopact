package gopact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"unicode/utf8"
)

// ErrResumePayloadInvalid marks a resume payload that does not satisfy the
// pending interrupt's ResumeSchema.
var ErrResumePayloadInvalid = errors.New("gopact: resume payload invalid")

// ErrJSONSchemaValidationFailed marks a value that does not satisfy the portable
// JSON Schema subset supported by this SDK.
var ErrJSONSchemaValidationFailed = errors.New("gopact: json schema validation failed")

// JSONSchemaValidator validates a value against a JSON schema.
type JSONSchemaValidator interface {
	ValidateJSONSchema(ctx context.Context, schema JSONSchema, value any) error
}

// JSONSchemaValidatorFunc adapts a function into a JSONSchemaValidator.
type JSONSchemaValidatorFunc func(ctx context.Context, schema JSONSchema, value any) error

// ValidateJSONSchema validates a value against a JSON schema.
func (f JSONSchemaValidatorFunc) ValidateJSONSchema(ctx context.Context, schema JSONSchema, value any) error {
	if f == nil {
		return errors.New("gopact: json schema validator function is nil")
	}
	return f(ctx, schema, value)
}

// ValidateResumePayload checks request.Payload against record.ResumeSchema.
//
// The validator intentionally supports a small portable JSON Schema subset for
// resumable SDK boundaries: type, required, properties, enum, const, items,
// pattern, min/max length, min/max items, min/max number, exclusive min/max,
// multipleOf, and additionalProperties.
func ValidateResumePayload(record InterruptRecord, request ResumeRequest) error {
	return ValidateResumePayloadWithValidator(context.TODO(), nil, record, request)
}

// ValidateResumePayloadWithValidator checks request.Payload against
// record.ResumeSchema using validator when it is provided.
func ValidateResumePayloadWithValidator(ctx context.Context, validator JSONSchemaValidator, record InterruptRecord, request ResumeRequest) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if len(record.ResumeSchema) == 0 {
		return nil
	}
	if err := ValidateJSONSchemaValueWith(ctx, validator, record.ResumeSchema, request.Payload); err != nil {
		return fmt.Errorf("%w: %w", ErrResumePayloadInvalid, err)
	}
	return nil
}

// ValidateJSONSchemaValue checks value against the portable JSON Schema subset
// supported by gopact SDK boundaries.
func ValidateJSONSchemaValue(schema JSONSchema, value any) error {
	return ValidateJSONSchemaValueWith(context.TODO(), nil, schema, value)
}

// ValidateJSONSchemaValueWith checks value against schema using validator when
// it is provided; otherwise it uses the built-in portable subset validator.
func ValidateJSONSchemaValueWith(ctx context.Context, validator JSONSchemaValidator, schema JSONSchema, value any) error {
	if len(schema) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if validator == nil {
		validator = portableJSONSchemaValidator{}
	}
	normalized, err := normalizeJSONSchemaValue(value)
	if err != nil {
		return fmt.Errorf("%w: value is not json compatible: %v", ErrJSONSchemaValidationFailed, err)
	}
	if err := validator.ValidateJSONSchema(ctx, schema, normalized); err != nil {
		if errors.Is(err, ErrJSONSchemaValidationFailed) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrJSONSchemaValidationFailed, err)
	}
	return nil
}

type portableJSONSchemaValidator struct{}

func (portableJSONSchemaValidator) ValidateJSONSchema(ctx context.Context, schema JSONSchema, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateJSONSchemaSubset(map[string]any(schema), value, "$"); err != nil {
		return fmt.Errorf("%w: %w", ErrJSONSchemaValidationFailed, err)
	}
	return nil
}

func normalizeJSONSchemaValue(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func validateJSONSchemaSubset(schema map[string]any, value any, path string) error {
	if len(schema) == 0 {
		return nil
	}
	if _, ok := schema["$ref"]; ok {
		return fmt.Errorf("%s: $ref is not supported", path)
	}
	if expected, ok := schema["type"]; ok {
		if !jsonSchemaTypeMatches(expected, value) {
			return fmt.Errorf("%s: type %q does not match schema type %v", path, jsonSchemaType(value), expected)
		}
	}
	if constValue, ok := schema["const"]; ok && !jsonSchemaEqual(value, constValue) {
		return fmt.Errorf("%s: value does not match const", path)
	}
	if enumValues, ok := schema["enum"]; ok && !jsonSchemaEnumContains(enumValues, value) {
		return fmt.Errorf("%s: value is not in enum", path)
	}
	if pattern, ok := schema["pattern"].(string); ok {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: pattern requires string value", path)
		}
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("%s: pattern is invalid: %w", path, err)
		}
		if !expression.MatchString(text) {
			return fmt.Errorf("%s: string does not match pattern", path)
		}
	}
	if minLength, ok := jsonSchemaNumber(schema["minLength"]); ok {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: minLength requires string value", path)
		}
		if utf8.RuneCountInString(text) < int(minLength) {
			return fmt.Errorf("%s: string is shorter than minLength", path)
		}
	}
	if maxLength, ok := jsonSchemaNumber(schema["maxLength"]); ok {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: maxLength requires string value", path)
		}
		if utf8.RuneCountInString(text) > int(maxLength) {
			return fmt.Errorf("%s: string is longer than maxLength", path)
		}
	}
	if minimum, ok := jsonSchemaNumber(schema["minimum"]); ok {
		number, ok := jsonSchemaNumber(value)
		if !ok {
			return fmt.Errorf("%s: minimum requires numeric value", path)
		}
		if number < minimum {
			return fmt.Errorf("%s: number is below minimum", path)
		}
	}
	if exclusiveMinimum, ok := jsonSchemaNumber(schema["exclusiveMinimum"]); ok {
		number, ok := jsonSchemaNumber(value)
		if !ok {
			return fmt.Errorf("%s: exclusiveMinimum requires numeric value", path)
		}
		if number <= exclusiveMinimum {
			return fmt.Errorf("%s: number is not above exclusiveMinimum", path)
		}
	}
	if maximum, ok := jsonSchemaNumber(schema["maximum"]); ok {
		number, ok := jsonSchemaNumber(value)
		if !ok {
			return fmt.Errorf("%s: maximum requires numeric value", path)
		}
		if number > maximum {
			return fmt.Errorf("%s: number is above maximum", path)
		}
	}
	if exclusiveMaximum, ok := jsonSchemaNumber(schema["exclusiveMaximum"]); ok {
		number, ok := jsonSchemaNumber(value)
		if !ok {
			return fmt.Errorf("%s: exclusiveMaximum requires numeric value", path)
		}
		if number >= exclusiveMaximum {
			return fmt.Errorf("%s: number is not below exclusiveMaximum", path)
		}
	}
	if multipleOf, ok := jsonSchemaNumber(schema["multipleOf"]); ok {
		number, ok := jsonSchemaNumber(value)
		if !ok {
			return fmt.Errorf("%s: multipleOf requires numeric value", path)
		}
		if multipleOf <= 0 {
			return fmt.Errorf("%s: multipleOf must be greater than zero", path)
		}
		if !jsonSchemaMultipleOf(number, multipleOf) {
			return fmt.Errorf("%s: number is not a multipleOf value", path)
		}
	}
	if object, ok := value.(map[string]any); ok {
		if err := validateJSONSchemaObject(schema, object, path); err != nil {
			return err
		}
	}
	if err := validateJSONSchemaArray(schema, value, path); err != nil {
		return err
	}
	return nil
}

func validateJSONSchemaArray(schema map[string]any, value any, path string) error {
	itemsSchema, hasItemsSchema := jsonSchemaMap(schema["items"])
	minItems, hasMinItems := jsonSchemaNumber(schema["minItems"])
	maxItems, hasMaxItems := jsonSchemaNumber(schema["maxItems"])
	if !hasItemsSchema && !hasMinItems && !hasMaxItems {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s: array constraints require array value", path)
	}
	if hasMinItems && len(items) < int(minItems) {
		return fmt.Errorf("%s: array has fewer items than minItems", path)
	}
	if hasMaxItems && len(items) > int(maxItems) {
		return fmt.Errorf("%s: array has more items than maxItems", path)
	}
	if hasItemsSchema {
		for i, item := range items {
			if err := validateJSONSchemaSubset(itemsSchema, item, fmt.Sprintf("%s/%d", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateJSONSchemaObject(schema map[string]any, object map[string]any, path string) error {
	properties := make(map[string]map[string]any)
	if rawProperties, ok := schema["properties"].(map[string]any); ok {
		for name, rawSchema := range rawProperties {
			if propertySchema, ok := jsonSchemaMap(rawSchema); ok {
				properties[name] = propertySchema
			}
		}
	}
	for _, name := range jsonSchemaStringList(schema["required"]) {
		if _, ok := object[name]; !ok {
			return fmt.Errorf("%s: required property %q is missing", path, name)
		}
	}
	for name, propertySchema := range properties {
		value, ok := object[name]
		if !ok {
			continue
		}
		if err := validateJSONSchemaSubset(propertySchema, value, path+"/"+name); err != nil {
			return err
		}
	}
	switch additional := schema["additionalProperties"].(type) {
	case bool:
		if !additional {
			for name := range object {
				if _, ok := properties[name]; !ok {
					return fmt.Errorf("%s: additional property %q is not allowed", path, name)
				}
			}
		}
	case map[string]any:
		for name, value := range object {
			if _, ok := properties[name]; ok {
				continue
			}
			if err := validateJSONSchemaSubset(additional, value, path+"/"+name); err != nil {
				return err
			}
		}
	case JSONSchema:
		additionalSchema := map[string]any(additional)
		for name, value := range object {
			if _, ok := properties[name]; ok {
				continue
			}
			if err := validateJSONSchemaSubset(additionalSchema, value, path+"/"+name); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonSchemaTypeMatches(expected any, value any) bool {
	actual := jsonSchemaType(value)
	switch typed := expected.(type) {
	case string:
		return typed == actual || typed == "number" && actual == "integer"
	case []any:
		for _, item := range typed {
			if itemType, ok := item.(string); ok && (itemType == actual || itemType == "number" && actual == "integer") {
				return true
			}
		}
	case []string:
		for _, itemType := range typed {
			if itemType == actual || itemType == "number" && actual == "integer" {
				return true
			}
		}
	}
	return false
}

func jsonSchemaType(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		if _, err := typed.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		if number, ok := jsonSchemaNumber(typed); ok && number == math.Trunc(number) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func jsonSchemaEnumContains(enumValues any, value any) bool {
	switch values := enumValues.(type) {
	case []any:
		for _, enumValue := range values {
			if jsonSchemaEqual(value, enumValue) {
				return true
			}
		}
	case []string:
		text, ok := value.(string)
		if !ok {
			return false
		}
		for _, enumValue := range values {
			if text == enumValue {
				return true
			}
		}
	}
	return false
}

func jsonSchemaEqual(a, b any) bool {
	aNumber, aIsNumber := jsonSchemaNumber(a)
	bNumber, bIsNumber := jsonSchemaNumber(b)
	if aIsNumber && bIsNumber {
		return aNumber == bNumber
	}
	return reflect.DeepEqual(a, b)
}

func jsonSchemaStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func jsonSchemaMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case JSONSchema:
		return map[string]any(typed), true
	default:
		return nil, false
	}
}

func jsonSchemaNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func jsonSchemaMultipleOf(number float64, divisor float64) bool {
	quotient := number / divisor
	return math.Abs(quotient-math.Round(quotient)) <= 1e-9
}
