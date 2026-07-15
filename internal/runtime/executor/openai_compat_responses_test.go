package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func openAICompatAutoTestRequest() (cliproxyexecutor.Request, cliproxyexecutor.Options) {
	payload := []byte(`{
		"model":"gpt-test",
		"input":"hello",
		"tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}],
		"tool_choice":"auto"
	}`)
	return cliproxyexecutor.Request{Model: "gpt-test", Payload: payload}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAIResponse,
		OriginalRequest: payload,
	}
}

func openAICompatAutoTestAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{
		ID:       "auto-test-auth",
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":     strings.TrimSuffix(baseURL, "/") + "/v1",
			"api_key":      "test-key",
			"upstream_api": "auto",
		},
	}
}

func TestOpenAICompatAutoUsesNativeResponsesAndCachesCapability(t *testing.T) {
	var responsesCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		responsesCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if toolType := gjson.GetBytes(body, "tools.0.type").String(); toolType != "namespace" {
			t.Fatalf("native Responses request changed namespace tool: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","model":"gpt-test","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	exec := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	req, opts := openAICompatAutoTestRequest()
	auth := openAICompatAutoTestAuth(server.URL)
	for i := 0; i < 2; i++ {
		resp, err := exec.Execute(context.Background(), auth, req, opts)
		if err != nil {
			t.Fatalf("Execute #%d: %v", i+1, err)
		}
		if id := gjson.GetBytes(resp.Payload, "id").String(); id != "resp_1" {
			t.Fatalf("response id = %q", id)
		}
	}
	if got := responsesCalls.Load(); got != 2 {
		t.Fatalf("Responses calls = %d, want 2", got)
	}
}

func TestOpenAICompatAutoFallsBackToChatAndCachesCapability(t *testing.T) {
	var responsesCalls atomic.Int32
	var chatCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls.Add(1)
			http.Error(w, `{"error":{"message":"unknown endpoint"}}`, http.StatusNotFound)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if count := gjson.GetBytes(body, "tools.#").Int(); count < 1 {
				t.Fatalf("translated tools count = %d; payload=%s", count, body)
			}
			if choice := gjson.GetBytes(body, "tool_choice").String(); choice != "auto" {
				t.Fatalf("translated tool_choice = %q; payload=%s", choice, body)
			}
			found := false
			for _, tool := range gjson.GetBytes(body, "tools").Array() {
				if tool.Get("function.name").String() == "functions__exec" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("translated namespace function missing; payload=%s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chat_1","created":1,"model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	req, opts := openAICompatAutoTestRequest()
	auth := openAICompatAutoTestAuth(server.URL)
	for i := 0; i < 2; i++ {
		if _, err := exec.Execute(context.Background(), auth, req, opts); err != nil {
			t.Fatalf("Execute #%d: %v", i+1, err)
		}
	}
	if got := responsesCalls.Load(); got != 1 {
		t.Fatalf("Responses probe calls = %d, want 1", got)
	}
	if got := chatCalls.Load(); got != 2 {
		t.Fatalf("Chat calls = %d, want 2", got)
	}
}

func TestOpenAICompatAutoDoesNotFallbackOnServerError(t *testing.T) {
	var chatCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			chatCalls.Add(1)
		}
		http.Error(w, `{"error":{"message":"temporary failure"}}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	exec := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	req, opts := openAICompatAutoTestRequest()
	_, err := exec.Execute(context.Background(), openAICompatAutoTestAuth(server.URL), req, opts)
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if got := chatCalls.Load(); got != 0 {
		t.Fatalf("Chat calls = %d, want 0", got)
	}
}

func TestShouldFallbackOpenAICompatResponsesClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "not found", err: statusErr{code: http.StatusNotFound, msg: "not found"}, want: true},
		{name: "namespace schema", err: statusErr{code: http.StatusBadRequest, msg: "invalid tool type namespace"}, want: true},
		{name: "tools stripped", err: statusErr{code: http.StatusBadRequest, msg: "Invalid value for 'tool_choice': 'tool_choice' is only allowed when 'tools' are specified."}, want: true},
		{name: "generic bad request", err: statusErr{code: http.StatusBadRequest, msg: "invalid model"}, want: false},
		{name: "unauthorized", err: statusErr{code: http.StatusUnauthorized, msg: "bad key"}, want: false},
		{name: "rate limited", err: statusErr{code: http.StatusTooManyRequests, msg: "slow down"}, want: false},
		{name: "server error", err: statusErr{code: http.StatusInternalServerError, msg: "failed"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldFallbackOpenAICompatResponses(test.err); got != test.want {
				t.Fatalf("fallback = %t, want %t", got, test.want)
			}
		})
	}
}

func TestOpenAICompatAutoStreamFallsBackBeforeForwardingBytes(t *testing.T) {
	var responsesCalls atomic.Int32
	var chatCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls.Add(1)
			http.Error(w, `{"error":{"message":"responses api is not supported"}}`, http.StatusNotFound)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"chat_1\",\"created\":1,\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"id\":\"chat_1\",\"created\":1,\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec := NewOpenAICompatExecutor("openai-compatibility", &config.Config{})
	req, opts := openAICompatAutoTestRequest()
	opts.Stream = true
	result, err := exec.ExecuteStream(context.Background(), openAICompatAutoTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	var chunks int
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk: %v", chunk.Err)
		}
		if len(chunk.Payload) > 0 {
			chunks++
		}
	}
	if chunks == 0 {
		t.Fatal("expected translated stream chunks")
	}
	if responsesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("calls responses=%d chat=%d", responsesCalls.Load(), chatCalls.Load())
	}
}
