package executor

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestAntigravityBuildPayloadAppliesPayloadConfig(t *testing.T) {
	executor := NewAntigravityExecutor(&config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{
						{Name: "gemini-2.5-pro", Protocol: "antigravity"},
					},
					Params: map[string]any{
						"generationConfig.temperature": 0.2,
					},
				},
			},
		},
	})
	req := cliproxyexecutor.Request{
		Model:   "gemini-2.5-pro",
		Payload: []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`),
	}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("antigravity")}

	execCtx := executor.newAntigravityExecutionContext(context.Background(), nil, req, opts, false)
	body, err := executor.buildAntigravityPayload(execCtx)
	if err != nil {
		t.Fatalf("buildAntigravityPayload returned error: %v", err)
	}
	if got := gjson.GetBytes(body, "request.generationConfig.temperature").Float(); got != 0.2 {
		t.Fatalf("request.generationConfig.temperature = %v, want %v", got, 0.2)
	}
}

func TestAntigravityBuildCountTokensPayloadStripsFields(t *testing.T) {
	executor := NewAntigravityExecutor(&config.Config{})
	req := cliproxyexecutor.Request{
		Model: "gemini-2.5-pro",
		Payload: []byte(`{
			"request":{
				"contents":[{"role":"user","parts":[{"text":"hi"}]}],
				"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH"}]
			},
			"project":"demo",
			"model":"gemini-2.5-pro"
		}`),
	}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("antigravity")}

	execCtx := executor.newAntigravityExecutionContext(context.Background(), nil, req, opts, false)
	body, err := executor.buildAntigravityCountTokensPayload(execCtx)
	if err != nil {
		t.Fatalf("buildAntigravityCountTokensPayload returned error: %v", err)
	}
	for _, path := range []string{"project", "model", "request.safetySettings"} {
		if gjson.GetBytes(body, path).Exists() {
			t.Fatalf("expected %s to be removed from payload: %s", path, string(body))
		}
	}
}
