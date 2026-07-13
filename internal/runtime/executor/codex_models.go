package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
	log "github.com/sirupsen/logrus"
)

const (
	// Official ChatGPT Codex models manifest (OAuth). Aligned with sub2api:
	// https://chatgpt.com/backend-api/codex/models?client_version=...
	defaultCodexModelsManifestBase = "https://chatgpt.com/backend-api/codex"
	defaultCodexModelsClientVer    = "0.118.0"
)

var codexModelsCache struct {
	mu     sync.RWMutex
	models []*sdkmodelcatalog.ModelInfo
}

type codexModelsResponse struct {
	Data   []codexModelPayload `json:"data"`
	Models []codexModelPayload `json:"models"`
	// Some Codex manifest revisions nest models under items/list.
	Items []codexModelPayload `json:"items"`
}

type codexModelPayload struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Title       string `json:"title"`
	Object      string `json:"object"`
	OwnedBy     string `json:"owned_by"`
	Created     int64  `json:"created"`
}

func cloneCodexModels(models []*sdkmodelcatalog.ModelInfo) []*sdkmodelcatalog.ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*sdkmodelcatalog.ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil || strings.TrimSpace(model.ID) == "" {
			continue
		}
		clone := *model
		if len(model.SupportedGenerationMethods) > 0 {
			clone.SupportedGenerationMethods = append([]string(nil), model.SupportedGenerationMethods...)
		}
		if len(model.SupportedParameters) > 0 {
			clone.SupportedParameters = append([]string(nil), model.SupportedParameters...)
		}
		if model.Thinking != nil {
			thinkingClone := *model.Thinking
			if len(model.Thinking.Levels) > 0 {
				thinkingClone.Levels = append([]string(nil), model.Thinking.Levels...)
			}
			clone.Thinking = &thinkingClone
		}
		out = append(out, &clone)
	}
	return out
}

func storeCodexModels(models []*sdkmodelcatalog.ModelInfo) bool {
	cloned := cloneCodexModels(models)
	if len(cloned) == 0 {
		return false
	}
	codexModelsCache.mu.Lock()
	codexModelsCache.models = cloned
	codexModelsCache.mu.Unlock()
	return true
}

func loadCodexModels() []*sdkmodelcatalog.ModelInfo {
	codexModelsCache.mu.RLock()
	cloned := cloneCodexModels(codexModelsCache.models)
	codexModelsCache.mu.RUnlock()
	return cloned
}

func fallbackCodexModels() []*sdkmodelcatalog.ModelInfo {
	if models := loadCodexModels(); len(models) > 0 {
		log.Debugf("codex executor: using cached model list (%d models)", len(models))
		return models
	}
	return nil
}

// FetchCodexModels retrieves the live model list for a Codex auth.
//
// - OAuth / ChatGPT backend base: GET {base}/models?client_version=... (manifest)
// - API key with OpenAI-compatible base: GET {base}/v1/models or {base}/models
//
// Response body schema evolves with Codex client releases; parsing is intentionally
// tolerant (id/slug/name fields, data/models/items arrays).
func FetchCodexModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*sdkmodelcatalog.ModelInfo {
	if ctx == nil {
		ctx = context.Background()
	}
	token, baseURL := codexCreds(auth)
	token = strings.TrimSpace(token)
	if token == "" {
		return fallbackCodexModels()
	}

	useAPIKey := auth != nil && auth.Attributes != nil && strings.TrimSpace(auth.Attributes["api_key"]) != ""
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	modelsURL, isManifest := buildCodexModelsURL(baseURL, useAPIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return fallbackCodexModels()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	if isManifest {
		// Align with Codex CLI / sub2api manifest probe headers.
		req.Header.Set("Originator", codexOriginator)
		req.Header.Set("Version", defaultCodexModelsClientVer)
		req.Header.Set("User-Agent", codexUserAgent)
		if auth != nil && auth.Metadata != nil {
			if accountID, ok := auth.Metadata["account_id"].(string); ok && strings.TrimSpace(accountID) != "" {
				req.Header.Set("Chatgpt-Account-Id", strings.TrimSpace(accountID))
			}
		}
	}

	resp, err := newProxyAwareHTTPClient(ctx, cfg, auth, 0).Do(req)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Debugf("codex executor: models request failed: %v", err)
		}
		return fallbackCodexModels()
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("codex executor: close models response body error: %v", errClose)
		}
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, resp.Body)
		log.Debugf("codex executor: models request failed with status %d", resp.StatusCode)
		return fallbackCodexModels()
	}

	body, err := readUpstreamResponseBody("codex", resp.Body)
	if err != nil {
		log.Debugf("codex executor: models response read failed: %v", err)
		return fallbackCodexModels()
	}

	models, ok := parseCodexModels(body, time.Now().Unix())
	if !ok {
		log.Debug("codex executor: fetched empty or invalid model list; retaining cached model list")
		return fallbackCodexModels()
	}
	storeCodexModels(models)
	return models
}

func buildCodexModelsURL(baseURL string, useAPIKey bool) (string, bool) {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if normalized == "" {
		// OAuth default: ChatGPT Codex models manifest.
		u := defaultCodexModelsManifestBase + "/models?client_version=" + url.QueryEscape(defaultCodexModelsClientVer)
		return u, true
	}

	lower := strings.ToLower(normalized)
	// Already a models endpoint.
	if strings.HasSuffix(lower, "/models") || strings.Contains(lower, "/models?") {
		return normalized, strings.Contains(lower, "backend-api/codex") || strings.Contains(lower, "chatgpt.com")
	}

	// ChatGPT / Codex backend style base.
	if strings.Contains(lower, "backend-api/codex") || strings.Contains(lower, "chatgpt.com") {
		if !strings.HasSuffix(lower, "/codex") && !strings.Contains(lower, "/codex/") {
			// e.g. https://chatgpt.com/backend-api
			if strings.HasSuffix(lower, "/backend-api") {
				normalized = normalized + "/codex"
			}
		}
		return normalized + "/models?client_version=" + url.QueryEscape(defaultCodexModelsClientVer), true
	}

	// API key / OpenAI-compatible base.
	if useAPIKey || strings.Contains(lower, "api.openai.com") {
		if strings.HasSuffix(lower, "/v1") {
			return normalized + "/models", false
		}
		if strings.HasSuffix(lower, "/v1/models") {
			return normalized, false
		}
		return normalized + "/v1/models", false
	}

	// Generic: treat as Codex-style base ending with /models.
	return normalized + "/models", strings.Contains(lower, "codex")
}

func parseCodexModels(body []byte, now int64) ([]*sdkmodelcatalog.ModelInfo, bool) {
	var decoded codexModelsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		var arrayResponse []codexModelPayload
		if arrayErr := json.Unmarshal(body, &arrayResponse); arrayErr != nil {
			// Last resort: walk arbitrary JSON for objects with id/slug fields.
			return parseCodexModelsLoose(body, now)
		}
		decoded.Data = arrayResponse
	}

	entries := decoded.Data
	if len(entries) == 0 {
		entries = decoded.Models
	}
	if len(entries) == 0 {
		entries = decoded.Items
	}
	if len(entries) == 0 {
		return parseCodexModelsLoose(body, now)
	}

	out := make([]*sdkmodelcatalog.ModelInfo, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, item := range entries {
		modelID := firstNonEmptyString(item.ID, item.Slug, item.Name)
		if modelID == "" {
			continue
		}
		key := strings.ToLower(modelID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		displayName := firstNonEmptyString(item.DisplayName, item.Title, item.Name, modelID)
		object := strings.TrimSpace(item.Object)
		if object == "" {
			object = "model"
		}
		ownedBy := strings.TrimSpace(item.OwnedBy)
		if ownedBy == "" {
			ownedBy = "openai"
		}
		created := item.Created
		if created == 0 {
			created = now
		}
		model := &sdkmodelcatalog.ModelInfo{
			ID:          modelID,
			Object:      object,
			Created:     created,
			OwnedBy:     ownedBy,
			Type:        "codex",
			DisplayName: displayName,
			Name:        modelID,
			Version:     modelID,
		}
		if static := sdkmodelcatalog.LookupStaticModelInfo(modelID); static != nil {
			if strings.TrimSpace(static.Description) != "" {
				model.Description = static.Description
			}
			if strings.TrimSpace(static.DisplayName) != "" {
				model.DisplayName = static.DisplayName
			}
			if static.Thinking != nil {
				thinkingClone := *static.Thinking
				if len(static.Thinking.Levels) > 0 {
					thinkingClone.Levels = append([]string(nil), static.Thinking.Levels...)
				}
				model.Thinking = &thinkingClone
			}
		}
		out = append(out, model)
	}
	return out, len(out) > 0
}

// parseCodexModelsLoose walks nested JSON maps/arrays looking for model-like objects.
// Codex manifests sometimes wrap the list under evolving keys.
func parseCodexModelsLoose(body []byte, now int64) ([]*sdkmodelcatalog.ModelInfo, bool) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false
	}
	seen := make(map[string]struct{})
	out := make([]*sdkmodelcatalog.ModelInfo, 0, 16)
	var walk func(v any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			id := firstNonEmptyString(
				stringFromAny(typed["id"]),
				stringFromAny(typed["slug"]),
				stringFromAny(typed["model"]),
				stringFromAny(typed["name"]),
			)
			// Heuristic: model-like object when id looks like a model slug and not a nested container-only map.
			if id != "" && (strings.Contains(id, "-") || strings.HasPrefix(strings.ToLower(id), "gpt") || strings.HasPrefix(strings.ToLower(id), "o")) {
				key := strings.ToLower(id)
				if _, exists := seen[key]; !exists {
					// Skip obvious non-model containers.
					if _, hasModels := typed["models"]; !hasModels {
						if _, hasData := typed["data"]; !hasData {
							seen[key] = struct{}{}
							display := firstNonEmptyString(
								stringFromAny(typed["display_name"]),
								stringFromAny(typed["title"]),
								stringFromAny(typed["name"]),
								id,
							)
							ownedBy := firstNonEmptyString(stringFromAny(typed["owned_by"]), "openai")
							out = append(out, &sdkmodelcatalog.ModelInfo{
								ID:          id,
								Object:      "model",
								Created:     now,
								OwnedBy:     ownedBy,
								Type:        "codex",
								DisplayName: display,
								Name:        id,
								Version:     id,
							})
						}
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(root)
	return out, len(out) > 0
}

func stringFromAny(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}
