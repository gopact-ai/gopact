package gopact

import "testing"

func TestContentPartConstructors(t *testing.T) {
	tests := []struct {
		name     string
		part     ContentPart
		wantType ContentPartType
		wantText string
		wantURI  string
	}{
		{name: "text", part: TextPart("hello"), wantType: ContentPartText, wantText: "hello"},
		{name: "reasoning", part: ReasoningPart("think"), wantType: ContentPartReasoning, wantText: "think"},
		{name: "image", part: ImagePart("file://image.png", "image/png"), wantType: ContentPartImage, wantURI: "file://image.png"},
		{name: "audio", part: AudioPart("file://audio.wav", "audio/wav"), wantType: ContentPartAudio, wantURI: "file://audio.wav"},
		{name: "file", part: FilePart("file://data.json", "application/json"), wantType: ContentPartFile, wantURI: "file://data.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.part.Type != tt.wantType || tt.part.Text != tt.wantText || tt.part.URI != tt.wantURI {
				t.Fatalf("part = %+v", tt.part)
			}
		})
	}
}
