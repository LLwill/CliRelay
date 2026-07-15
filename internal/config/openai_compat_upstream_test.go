package config

import "testing"

func TestSanitizeOpenAICompatibilityNormalizesUpstreamAPI(t *testing.T) {
	cfg := &Config{OpenAICompatibility: []OpenAICompatibility{
		{Name: "auto", BaseURL: "https://auto.example/v1", UpstreamAPI: " AUTO "},
		{Name: "responses", BaseURL: "https://responses.example/v1", UpstreamAPI: "response"},
		{Name: "chat", BaseURL: "https://chat.example/v1", UpstreamAPI: "chat"},
		{Name: "legacy", BaseURL: "https://legacy.example/v1", UpstreamAPI: "invalid"},
	}}
	cfg.SanitizeOpenAICompatibility()

	want := []string{"auto", "responses", "chat-completions", ""}
	for i, expected := range want {
		if got := cfg.OpenAICompatibility[i].UpstreamAPI; got != expected {
			t.Fatalf("entry %d upstream-api = %q, want %q", i, got, expected)
		}
	}
}
