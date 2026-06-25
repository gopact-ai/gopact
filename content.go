package gopact

// ContentPartType identifies the kind of content carried by a message part.
type ContentPartType string

const (
	// ContentPart values identify provider-neutral message part kinds.
	ContentPartText      ContentPartType = "text"
	ContentPartImage     ContentPartType = "image"
	ContentPartAudio     ContentPartType = "audio"
	ContentPartFile      ContentPartType = "file"
	ContentPartReasoning ContentPartType = "reasoning"
)

// ContentPart is a provider-neutral block in a message.
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	URI      string          `json:"uri,omitempty"`
	MIMEType string          `json:"mime_type,omitempty"`
	Name     string          `json:"name,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// TextPart creates a visible text content block.
func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartText, Text: text}
}

// ReasoningPart creates a non-display reasoning content block.
func ReasoningPart(text string) ContentPart {
	return ContentPart{Type: ContentPartReasoning, Text: text}
}

// ImagePart creates an image content block.
func ImagePart(uri, mimeType string) ContentPart {
	return ContentPart{Type: ContentPartImage, URI: uri, MIMEType: mimeType}
}

// AudioPart creates an audio content block.
func AudioPart(uri, mimeType string) ContentPart {
	return ContentPart{Type: ContentPartAudio, URI: uri, MIMEType: mimeType}
}

// FilePart creates a file content block.
func FilePart(uri, mimeType string) ContentPart {
	return ContentPart{Type: ContentPartFile, URI: uri, MIMEType: mimeType}
}
