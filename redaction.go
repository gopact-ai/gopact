package gopact

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// RedactionState records whether an event has passed through redaction.
type RedactionState struct {
	Applied bool     `json:"applied,omitempty"`
	Fields  []string `json:"fields,omitempty"`
}

// TextRedactor redacts sensitive text before data crosses an external boundary.
type TextRedactor interface {
	RedactText(ctx context.Context, text string) (string, error)
}

// TextRedactorFunc adapts a function into a TextRedactor.
type TextRedactorFunc func(ctx context.Context, text string) (string, error)

// RedactText calls f.
func (f TextRedactorFunc) RedactText(ctx context.Context, text string) (string, error) {
	if f == nil {
		return text, nil
	}
	return f(ctx, text)
}

// RedactModelRequest redacts text-bearing fields in a model request.
func RedactModelRequest(ctx context.Context, redactor TextRedactor, request ModelRequest) (ModelRequest, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if redactor == nil {
		return copyModelRequest(request), nil
	}
	request = copyModelRequest(request)
	for i := range request.Messages {
		message, err := redactMessage(ctx, redactor, request.Messages[i])
		if err != nil {
			return ModelRequest{}, err
		}
		request.Messages[i] = message
	}
	tools, err := redactToolSpecs(ctx, redactor, request.Tools)
	if err != nil {
		return ModelRequest{}, err
	}
	request.Tools = tools
	schema, err := redactJSONSchema(ctx, redactor, request.ResponseSchema)
	if err != nil {
		return ModelRequest{}, err
	}
	request.ResponseSchema = schema
	metadata, err := redactAnyMap(ctx, redactor, request.Metadata)
	if err != nil {
		return ModelRequest{}, err
	}
	request.Metadata = metadata
	return request, nil
}

// RedactModelResponse redacts text-bearing fields in a model response.
func RedactModelResponse(ctx context.Context, redactor TextRedactor, response ModelResponse) (ModelResponse, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if redactor == nil {
		return copyModelResponse(response), nil
	}
	response = copyModelResponse(response)
	message, err := redactMessage(ctx, redactor, response.Message)
	if err != nil {
		return ModelResponse{}, err
	}
	response.Message = message
	routeMetadata, err := redactAnyMap(ctx, redactor, response.Route.Metadata)
	if err != nil {
		return ModelResponse{}, err
	}
	response.Route.Metadata = routeMetadata
	metadata, err := redactAnyMap(ctx, redactor, response.Metadata)
	if err != nil {
		return ModelResponse{}, err
	}
	response.Metadata = metadata
	return response, nil
}

// ModelIORedactionMiddleware redacts model request and response payloads at the model boundary.
func ModelIORedactionMiddleware(redactor TextRedactor) ModelHandler {
	return func(c *ModelContext) error {
		if c == nil {
			c = NewModelContext(context.TODO(), ModelContextOptions{})
		}
		request, err := RedactModelRequest(c.Context, redactor, c.Request)
		if err != nil {
			return fmt.Errorf("gopact: redact model request: %w", err)
		}
		c.Request = request
		if err := c.Next(); err != nil {
			return err
		}
		response, err := RedactModelResponse(c.Context, redactor, c.Response)
		if err != nil {
			return fmt.Errorf("gopact: redact model response: %w", err)
		}
		c.Response = response
		return nil
	}
}

// RedactToolResult redacts text-bearing fields in a tool result.
func RedactToolResult(ctx context.Context, redactor TextRedactor, result ToolResult) (ToolResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if redactor == nil {
		return copyToolResult(result), nil
	}
	return redactToolResult(ctx, redactor, result)
}

// ToolResultRedactionMiddleware redacts a tool result after the tool handler chain runs.
func ToolResultRedactionMiddleware(redactor TextRedactor) ToolHandler {
	return func(c *ToolContext) error {
		if c == nil {
			c = NewToolContext(context.TODO(), ToolContextOptions{})
		}
		if err := c.Next(); err != nil {
			return err
		}
		result, err := RedactToolResult(c.Context, redactor, c.Result)
		if err != nil {
			return fmt.Errorf("gopact: redact tool result: %w", err)
		}
		c.Result = result
		return nil
	}
}

// EventRedactionMiddleware redacts text fields before the event reaches sinks or subscribers.
func EventRedactionMiddleware(redactor TextRedactor) EventHandler {
	return func(c *EventContext) error {
		if c == nil {
			c = NewEventContext(context.TODO(), Event{})
		}
		if redactor == nil {
			return c.Next()
		}
		event, err := redactEvent(c.Context, redactor, c.Event)
		if err != nil {
			return fmt.Errorf("gopact: redact event: %w", err)
		}
		event.Redaction = RedactionState{Applied: true, Fields: redactionFields(c.Event)}
		c.Event = event
		return c.Next()
	}
}

func redactionFields(event Event) []string {
	fields := make([]string, 0)
	if event.Message != nil {
		fields = append(fields, "message.content")
		for i, part := range event.Message.Parts {
			if part.Type == ContentPartText || part.Type == ContentPartReasoning {
				fields = append(fields, fmt.Sprintf("message.parts.%d.text", i))
			}
			for key := range part.Metadata {
				fields = append(fields, "message.parts.metadata."+key)
			}
		}
		for i := range event.Message.ToolCalls {
			fields = append(fields, fmt.Sprintf("message.tool_calls.%d.arguments", i))
		}
	}
	if event.ToolCall != nil {
		fields = append(fields, "tool_call.arguments")
	}
	if event.Result != nil {
		fields = append(fields, "result.content")
		for key := range event.Result.Metadata {
			fields = append(fields, "result.metadata."+key)
		}
	}
	for key := range event.Metadata {
		fields = append(fields, "metadata."+key)
	}
	sort.Strings(fields)
	return fields
}

func redactEvent(ctx context.Context, redactor TextRedactor, event Event) (Event, error) {
	if event.Message != nil {
		message, err := redactMessage(ctx, redactor, *event.Message)
		if err != nil {
			return Event{}, err
		}
		event.Message = &message
	}
	if event.ToolCall != nil {
		toolCall, err := redactToolCall(ctx, redactor, *event.ToolCall)
		if err != nil {
			return Event{}, err
		}
		event.ToolCall = &toolCall
	}
	if event.Result != nil {
		result, err := redactToolResult(ctx, redactor, *event.Result)
		if err != nil {
			return Event{}, err
		}
		event.Result = &result
	}
	metadata, err := redactAnyMap(ctx, redactor, event.Metadata)
	if err != nil {
		return Event{}, err
	}
	event.Metadata = metadata
	return event, nil
}

func redactMessage(ctx context.Context, redactor TextRedactor, message Message) (Message, error) {
	content, err := redactor.RedactText(ctx, message.Content)
	if err != nil {
		return Message{}, err
	}
	message.Content = content

	if len(message.Parts) > 0 {
		message.Parts = append([]ContentPart(nil), message.Parts...)
		for i := range message.Parts {
			if message.Parts[i].Type == ContentPartText || message.Parts[i].Type == ContentPartReasoning {
				text, err := redactor.RedactText(ctx, message.Parts[i].Text)
				if err != nil {
					return Message{}, err
				}
				message.Parts[i].Text = text
			}
			metadata, err := redactAnyMap(ctx, redactor, message.Parts[i].Metadata)
			if err != nil {
				return Message{}, err
			}
			message.Parts[i].Metadata = metadata
		}
	}

	if len(message.ToolCalls) > 0 {
		message.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
		for i := range message.ToolCalls {
			toolCall, err := redactToolCall(ctx, redactor, message.ToolCalls[i])
			if err != nil {
				return Message{}, err
			}
			message.ToolCalls[i] = toolCall
		}
	}
	return message, nil
}

func redactToolCall(ctx context.Context, redactor TextRedactor, toolCall ToolCall) (ToolCall, error) {
	arguments, err := redactRawMessage(ctx, redactor, toolCall.Arguments)
	if err != nil {
		return ToolCall{}, err
	}
	toolCall.Arguments = arguments
	return toolCall, nil
}

func redactToolResult(ctx context.Context, redactor TextRedactor, result ToolResult) (ToolResult, error) {
	content, err := redactor.RedactText(ctx, result.Content)
	if err != nil {
		return ToolResult{}, err
	}
	result.Content = content

	metadata, err := redactAnyMap(ctx, redactor, result.Metadata)
	if err != nil {
		return ToolResult{}, err
	}
	result.Metadata = metadata
	result.Artifacts = append([]ArtifactRef(nil), result.Artifacts...)
	result.Effects = copyEffectRecords(result.Effects)
	result.Events = copyEvents(result.Events)
	if result.Commit != nil {
		commit := *result.Commit
		metadata, err := redactAnyMap(ctx, redactor, result.Commit.Metadata)
		if err != nil {
			return ToolResult{}, err
		}
		commit.Metadata = metadata
		result.Commit = &commit
	}
	return result, nil
}

func redactToolSpecs(ctx context.Context, redactor TextRedactor, tools []ToolSpec) ([]ToolSpec, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := copyToolSpecs(tools)
	for i := range out {
		description, err := redactor.RedactText(ctx, out[i].Description)
		if err != nil {
			return nil, err
		}
		out[i].Description = description
		schema, err := redactJSONSchema(ctx, redactor, out[i].InputSchema)
		if err != nil {
			return nil, err
		}
		out[i].InputSchema = schema
	}
	return out, nil
}

func redactJSONSchema(ctx context.Context, redactor TextRedactor, schema JSONSchema) (JSONSchema, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	redacted, err := redactAnyMap(ctx, redactor, map[string]any(schema))
	if err != nil {
		return nil, err
	}
	return JSONSchema(redacted), nil
}

func copyModelRequest(request ModelRequest) ModelRequest {
	request.Messages = copyMessages(request.Messages)
	request.Tools = copyToolSpecs(request.Tools)
	request.ResponseSchema = copyJSONSchema(request.ResponseSchema)
	request.Capabilities = append([]Capability(nil), request.Capabilities...)
	request.Metadata = copyAnyMap(request.Metadata)
	return request
}

func copyModelResponse(response ModelResponse) ModelResponse {
	response.Message = copyMessage(response.Message)
	response.Route.Metadata = copyAnyMap(response.Route.Metadata)
	response.Events = copyEvents(response.Events)
	response.Metadata = copyAnyMap(response.Metadata)
	return response
}

func copyMessages(in []Message) []Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]Message, len(in))
	for i, message := range in {
		out[i] = copyMessage(message)
	}
	return out
}

func copyMessage(message Message) Message {
	message.Parts = copyContentParts(message.Parts)
	message.ToolCalls = copyToolCalls(message.ToolCalls)
	return message
}

func copyToolSpecs(in []ToolSpec) []ToolSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolSpec, len(in))
	for i, tool := range in {
		out[i] = tool
		out[i].InputSchema = copyJSONSchema(tool.InputSchema)
	}
	return out
}

func copyToolResult(result ToolResult) ToolResult {
	result.Artifacts = append([]ArtifactRef(nil), result.Artifacts...)
	result.Effects = copyEffectRecords(result.Effects)
	result.Events = copyEvents(result.Events)
	if result.Commit != nil {
		commit := *result.Commit
		commit.Metadata = copyAnyMap(result.Commit.Metadata)
		result.Commit = &commit
	}
	result.Metadata = copyAnyMap(result.Metadata)
	return result
}

func redactRawMessage(ctx context.Context, redactor TextRedactor, value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 {
		return nil, nil
	}
	redacted, err := redactor.RedactText(ctx, string(value))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(redacted), nil
}

func redactAnyMap(ctx context.Context, redactor TextRedactor, in map[string]any) (map[string]any, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		redacted, err := redactAny(ctx, redactor, value)
		if err != nil {
			return nil, err
		}
		out[key] = redacted
	}
	return out, nil
}

func redactAny(ctx context.Context, redactor TextRedactor, value any) (any, error) {
	switch v := value.(type) {
	case string:
		return redactor.RedactText(ctx, v)
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			redacted, err := redactor.RedactText(ctx, item)
			if err != nil {
				return nil, err
			}
			out[i] = redacted
		}
		return out, nil
	case map[string]any:
		return redactAnyMap(ctx, redactor, v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			redacted, err := redactAny(ctx, redactor, item)
			if err != nil {
				return nil, err
			}
			out[i] = redacted
		}
		return out, nil
	default:
		return value, nil
	}
}
