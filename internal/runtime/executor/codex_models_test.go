package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
)

func resetCodexModelsCacheForTest() {
	codexModelsCache.mu.Lock()
	codexModelsCache.models = nil
	codexModelsCache.mu.Unlock()
}

func TestParseCodexModelsFromDataAndSlug(t *testing.T) {
	body := []byte(`{
		"data": [
			{"id": "gpt-5.1-codex", "display_name": "GPT-5.1 Codex"},
			{"slug": "o3-pro", "title": "o3 Pro"},
			{"id": "gpt-5.1-codex"}
		]
	}`)
	models, ok := parseCodexModels(body, 1700000000)
	if !ok {
		t.Fatal("expected parse success")
	}
	if len(models) != 2 {
		t.Fatalf("len(models)=%d, want 2", len(models))
	}
	if models[0].ID != "gpt-5.1-codex" || models[0].Type != "codex" {
		t.Fatalf("models[0]=%+v", models[0])
	}
	if models[1].ID != "o3-pro" {
		t.Fatalf("models[1].ID=%q", models[1].ID)
	}
}

func TestParseCodexModelsLooseNested(t *testing.T) {
	body := []byte(`{
		"payload": {
			"models": [
				{"slug": "gpt-nested-1", "display_name": "Nested"},
				{"id": "not-a-model", "models": []}
			]
		}
	}`)
	models, ok := parseCodexModels(body, 1700000000)
	if !ok {
		t.Fatal("expected loose parse success")
	}
	found := false
	for _, m := range models {
		if m != nil && m.ID == "gpt-nested-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing nested model, got %v", models)
	}
}

func TestBuildCodexModelsURL(t *testing.T) {
	url, isManifest := buildCodexModelsURL("", false)
	if !isManifest || !strings.Contains(url, "chatgpt.com/backend-api/codex/models") {
		t.Fatalf("default oauth url=%q isManifest=%v", url, isManifest)
	}
	if !strings.Contains(url, "client_version=") {
		t.Fatalf("missing client_version in %q", url)
	}

	url, isManifest = buildCodexModelsURL("https://api.openai.com", true)
	if isManifest || url != "https://api.openai.com/v1/models" {
		t.Fatalf("api key openai url=%q isManifest=%v", url, isManifest)
	}

	url, isManifest = buildCodexModelsURL("https://gateway.example/v1", true)
	if isManifest || url != "https://gateway.example/v1/models" {
		t.Fatalf("compat url=%q isManifest=%v", url, isManifest)
	}

	url, isManifest = buildCodexModelsURL("https://chatgpt.com/backend-api", false)
	if !isManifest || !strings.Contains(url, "/codex/models") {
		t.Fatalf("chatgpt backend url=%q isManifest=%v", url, isManifest)
	}
}

func TestFetchCodexModelsAPIKeyCatalog(t *testing.T) {
	resetCodexModelsCacheForTest()
	t.Cleanup(resetCodexModelsCacheForTest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"codex-live-1","owned_by":"openai"}]}`))
	}))
	defer srv.Close()

	models := FetchCodexModels(context.Background(), &cliproxyauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"api_key":  "sk-test",
			"base_url": srv.URL,
		},
	}, nil)
	if len(models) != 1 || models[0].ID != "codex-live-1" {
		t.Fatalf("models=%v", models)
	}
}

func TestFetchCodexModelsFallsBackToCache(t *testing.T) {
	resetCodexModelsCacheForTest()
	t.Cleanup(resetCodexModelsCacheForTest)

	if ok := storeCodexModels([]*sdkmodelcatalog.ModelInfo{{ID: "cached-codex", OwnedBy: "openai", Type: "codex"}}); !ok {
		t.Fatal("cache seed failed")
	}
	models := FetchCodexModels(context.Background(), &cliproxyauth.Auth{Provider: "codex"}, nil)
	if len(models) != 1 || models[0].ID != "cached-codex" {
		t.Fatalf("fallback models=%v", models)
	}
}
