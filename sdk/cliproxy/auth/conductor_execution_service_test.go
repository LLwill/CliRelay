package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type executionMetadataExecutor struct {
	mu                 sync.Mutex
	seenModel          string
	seenRequestedModel string
	seenSelectedAuthID string
}

func (e *executionMetadataExecutor) Identifier() string { return "codex" }

func (e *executionMetadataExecutor) Execute(_ context.Context, _ *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seenModel = req.Model
	if value, _ := opts.Metadata[cliproxyexecutor.RequestedModelMetadataKey].(string); value != "" {
		e.seenRequestedModel = value
	}
	if value, _ := opts.Metadata[cliproxyexecutor.SelectedAuthMetadataKey].(string); value != "" {
		e.seenSelectedAuthID = value
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *executionMetadataExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, &Error{Code: "not_implemented", Message: "ExecuteStream not implemented"}
}

func (e *executionMetadataExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *executionMetadataExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *executionMetadataExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

type streamChunkFailureExecutor struct{}

func (e *streamChunkFailureExecutor) Identifier() string { return "codex" }

func (e *streamChunkFailureExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{Code: "not_implemented", Message: "Execute not implemented"}
}

func (e *streamChunkFailureExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{
		Err: &Error{
			Code:       "stream_failed",
			Message:    "stream chunk failed",
			HTTPStatus: http.StatusBadGateway,
		},
	}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *streamChunkFailureExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *streamChunkFailureExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *streamChunkFailureExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

type resultRecordingHook struct {
	NoopHook
	mu      sync.Mutex
	results []Result
}

func (h *resultRecordingHook) OnResult(_ context.Context, result Result) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = append(h.results, result)
}

func (h *resultRecordingHook) snapshot() []Result {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Result, len(h.results))
	copy(out, h.results)
	return out
}

func TestManagerExecute_PublishesRequestedModelMetadataAndSelectedAuth(t *testing.T) {
	t.Parallel()

	executor := &executionMetadataExecutor{}
	manager := NewManager(nil, &FillFirstSelector{}, nil)
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Status:   StatusActive,
		Prefix:   "team",
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	selectedByCallback := ""
	resp, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{
		Model: "team/gpt-5-codex",
	}, cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.SelectedAuthCallbackMetadataKey: func(id string) {
				selectedByCallback = id
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want ok", string(resp.Payload))
	}

	executor.mu.Lock()
	seenModel := executor.seenModel
	seenRequestedModel := executor.seenRequestedModel
	seenSelectedAuthID := executor.seenSelectedAuthID
	executor.mu.Unlock()

	if seenModel != "gpt-5-codex" {
		t.Fatalf("executor req.Model = %q, want gpt-5-codex", seenModel)
	}
	if seenRequestedModel != "team/gpt-5-codex" {
		t.Fatalf("requested model metadata = %q, want team/gpt-5-codex", seenRequestedModel)
	}
	if seenSelectedAuthID != auth.ID {
		t.Fatalf("selected auth metadata = %q, want %q", seenSelectedAuthID, auth.ID)
	}
	if selectedByCallback != auth.ID {
		t.Fatalf("selected auth callback = %q, want %q", selectedByCallback, auth.ID)
	}
}

func TestManagerExecuteStream_RecordsFailureFromChunkError(t *testing.T) {
	t.Parallel()

	hook := &resultRecordingHook{}
	manager := NewManager(nil, &FillFirstSelector{}, hook)
	manager.RegisterExecutor(&streamChunkFailureExecutor{})

	auth := &Auth{
		ID:       "stream-auth",
		Provider: "codex",
		Status:   StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	stream, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{
		Model: "gpt-5-codex",
	}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	if stream == nil {
		t.Fatal("ExecuteStream() stream = nil")
	}

	var chunkErr error
	for chunk := range stream.Chunks {
		chunkErr = chunk.Err
	}
	if chunkErr == nil {
		t.Fatal("expected stream chunk error")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		results := hook.snapshot()
		if len(results) == 1 {
			result := results[0]
			if result.Success {
				t.Fatalf("hook result Success = true, want false")
			}
			if result.AuthID != auth.ID {
				t.Fatalf("hook result AuthID = %q, want %q", result.AuthID, auth.ID)
			}
			if result.Provider != "codex" {
				t.Fatalf("hook result Provider = %q, want codex", result.Provider)
			}
			if result.Model != "gpt-5-codex" {
				t.Fatalf("hook result Model = %q, want gpt-5-codex", result.Model)
			}
			if result.Error == nil || result.Error.Message != chunkErr.Error() {
				t.Fatalf("hook result Error = %+v, want %q", result.Error, chunkErr.Error())
			}
			if result.Error.HTTPStatus != http.StatusBadGateway {
				t.Fatalf("hook result HTTPStatus = %d, want %d", result.Error.HTTPStatus, http.StatusBadGateway)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected 1 hook result, got %d", len(hook.snapshot()))
}
