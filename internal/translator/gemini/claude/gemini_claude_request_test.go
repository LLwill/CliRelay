package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToGeminiSkipsEmptyPartsAndKeepsImages(t *testing.T) {
	raw := []byte(`{
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": ""}]},
			{"role": "user", "content": [
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AAAA"}},
				{"type": "text", "text": "describe it"}
			]}
		]
	}`)

	out := ConvertClaudeRequestToGemini("gemini-test", raw, true)
	if !gjson.ValidBytes(out) {
		t.Fatalf("invalid json: %s", out)
	}
	contents := gjson.GetBytes(out, "contents").Array()
	if len(contents) != 1 {
		t.Fatalf("contents len = %d, want 1", len(contents))
	}
	parts := contents[0].Get("parts").Array()
	if len(parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(parts))
	}
	if got := parts[0].Get("inlineData.mimeType").String(); got != "image/png" {
		t.Fatalf("image mimeType = %q, want image/png", got)
	}
	if got := parts[1].Get("text").String(); got != "describe it" {
		t.Fatalf("text = %q, want describe it", got)
	}
}
