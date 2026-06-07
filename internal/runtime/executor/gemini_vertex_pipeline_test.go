package executor

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestGeminiVertexBuildPayloadAppliesPayloadConfigAndSetsModel(t *testing.T) {
	executor := NewGeminiVertexExecutor(&config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{
						{Name: "gemini-2.5-pro", Protocol: "gemini"},
					},
					Params: map[string]any{
						"generationConfig.temperature": 0.1,
					},
				},
			},
		},
	})
	req := cliproxyexecutor.Request{
		Model:   "gemini-2.5-pro",
		Payload: []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("gemini")}

	execCtx := executor.newVertexExecutionContext(context.Background(), nil, req, opts, false)
	body, err := executor.buildVertexPayload(execCtx)
	if err != nil {
		t.Fatalf("buildVertexPayload returned error: %v", err)
	}

	if got := gjson.GetBytes(body, "model").String(); got != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want %q", got, "gemini-2.5-pro")
	}
	if got := gjson.GetBytes(body, "generationConfig.temperature").Float(); got != 0.1 {
		t.Fatalf("generationConfig.temperature = %v, want %v", got, 0.1)
	}
}

func TestGeminiVertexBuildCountTokensPayloadStripsUnsupportedFields(t *testing.T) {
	executor := NewGeminiVertexExecutor(&config.Config{})
	req := cliproxyexecutor.Request{
		Model: "gemini-2.5-pro",
		Payload: []byte(`{
			"model":"gemini-2.5-pro",
			"contents":[{"role":"user","parts":[{"text":"hi"}]}],
			"tools":[{"functionDeclarations":[{"name":"demo"}]}],
			"generationConfig":{"temperature":0.5},
			"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH"}]
		}`),
	}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("gemini")}

	execCtx := executor.newVertexExecutionContext(context.Background(), nil, req, opts, false)
	body, err := executor.buildVertexCountTokensPayload(execCtx)
	if err != nil {
		t.Fatalf("buildVertexCountTokensPayload returned error: %v", err)
	}

	for _, path := range []string{"tools", "generationConfig", "safetySettings"} {
		if gjson.GetBytes(body, path).Exists() {
			t.Fatalf("expected %s to be removed from payload: %s", path, string(body))
		}
	}
	if got := gjson.GetBytes(body, "model").String(); got != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want %q", got, "gemini-2.5-pro")
	}
}
