package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
)

func resetClaudeModelsCacheForTest() {
	claudeModelsCache.mu.Lock()
	claudeModelsCache.models = nil
	claudeModelsCache.mu.Unlock()
}

func TestParseClaudeModelsFromDataArray(t *testing.T) {
	body := []byte(`{
		"data": [
			{"id": "claude-sonnet-4-5", "type": "model", "display_name": "Claude Sonnet 4.5"},
			{"id": "claude-opus-4", "display_name": "Claude Opus 4"},
			{"id": "claude-sonnet-4-5"}
		]
	}`)
	models, ok := parseClaudeModels(body, 1700000000)
	if !ok {
		t.Fatal("expected parse success")
	}
	if len(models) != 2 {
		t.Fatalf("len(models)=%d, want 2 (deduped)", len(models))
	}
	if models[0].ID != "claude-sonnet-4-5" {
		t.Fatalf("models[0].ID=%q", models[0].ID)
	}
	if models[0].DisplayName != "Claude Sonnet 4.5" {
		t.Fatalf("display name = %q", models[0].DisplayName)
	}
	if models[0].OwnedBy != "claude" || models[0].Type != "claude" {
		t.Fatalf("owned/type = %q/%q", models[0].OwnedBy, models[0].Type)
	}
}

func TestBuildClaudeModelsURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://api.anthropic.com", "https://api.anthropic.com/v1/models"},
		{"https://api.anthropic.com/", "https://api.anthropic.com/v1/models"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com/v1/models"},
		{"https://gateway.example/v1/models", "https://gateway.example/v1/models"},
		{"https://gateway.example/proxy", "https://gateway.example/proxy/v1/models"},
	}
	for _, tc := range cases {
		if got := buildClaudeModelsURL(tc.in); got != tc.want {
			t.Fatalf("buildClaudeModelsURL(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFetchClaudeModelsParsesLiveCatalog(t *testing.T) {
	resetClaudeModelsCacheForTest()
	t.Cleanup(resetClaudeModelsCacheForTest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Fatalf("x-api-key=%q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("missing anthropic-version")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-live-1","display_name":"Live One"}]}`))
	}))
	defer srv.Close()

	models := FetchClaudeModels(context.Background(), &cliproxyauth.Auth{
		Provider: "claude",
		Attributes: map[string]string{
			"api_key":  "sk-ant-test",
			"base_url": srv.URL,
		},
	}, nil)
	if len(models) != 1 || models[0].ID != "claude-live-1" {
		t.Fatalf("models=%v", models)
	}
}

func TestFetchClaudeModelsFallsBackToCache(t *testing.T) {
	resetClaudeModelsCacheForTest()
	t.Cleanup(resetClaudeModelsCacheForTest)

	if ok := storeClaudeModels([]*sdkmodelcatalog.ModelInfo{{ID: "cached-claude", OwnedBy: "claude", Type: "claude"}}); !ok {
		t.Fatal("cache seed failed")
	}
	models := FetchClaudeModels(context.Background(), &cliproxyauth.Auth{
		Provider: "claude",
		// no credentials → fallback
	}, nil)
	if len(models) != 1 || models[0].ID != "cached-claude" {
		t.Fatalf("fallback models=%v", models)
	}
}

func TestFetchClaudeModelsOAuthBearer(t *testing.T) {
	resetClaudeModelsCacheForTest()
	t.Cleanup(resetClaudeModelsCacheForTest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got == "" {
			t.Fatal("expected oauth anthropic-beta")
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("unexpected x-api-key=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-oauth-1"}]}`))
	}))
	defer srv.Close()

	models := FetchClaudeModels(context.Background(), &cliproxyauth.Auth{
		Provider: "claude",
		Metadata: map[string]any{"access_token": "oauth-token"},
		Attributes: map[string]string{
			"base_url": srv.URL,
		},
	}, nil)
	if len(models) != 1 || models[0].ID != "claude-oauth-1" {
		t.Fatalf("models=%v", models)
	}
}
