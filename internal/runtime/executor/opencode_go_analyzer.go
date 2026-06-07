package executor

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func (e *OpenCodeGoExecutor) newAnalyzer(auth *cliproxyauth.Auth) *vision.OpenCodeGoAnalyzer {
	apiKey := opencodeGoAPIKey(auth)
	model, ok := opencodeGoResolveAnalyzerModel(e.cfg, auth)
	if !ok || model == "" {
		return nil
	}
	return vision.NewOpenCodeGoAnalyzer(opencodeGoBaseURL, apiKey, model)
}

// opencodeGoResolveAnalyzerModel determines the model to use for the image
// analyzer, respecting excluded_models from both auth attributes and config.
// Returns (model, false) when no usable analyzer model is available.
func opencodeGoResolveAnalyzerModel(cfg *config.Config, auth *cliproxyauth.Auth) (string, bool) {
	model := opencodeGoVisionFallbackModel(cfg, auth)
	if model != "" {
		return model, true
	}
	if opencodeGoFallbackIsConfigured(cfg, auth) {
		return "", false
	}
	defaultModel := "qwen3.5-plus"
	if opencodeGoModelIsExcluded(defaultModel, cfg, auth) {
		return "", false
	}
	return defaultModel, true
}

// opencodeGoFallbackIsConfigured returns true when a fallback model name
// exists in config or auth attributes (regardless of exclusion).
func opencodeGoFallbackIsConfigured(cfg *config.Config, auth *cliproxyauth.Auth) bool {
	if auth == nil {
		return false
	}
	if auth.Attributes != nil {
		if strings.TrimSpace(auth.Attributes["vision_fallback_model"]) != "" {
			return true
		}
	}
	if cfg == nil {
		return false
	}
	apiKey := opencodeGoAPIKey(auth)
	if apiKey == "" {
		return false
	}
	for i := range cfg.OpenCodeGoKey {
		if strings.EqualFold(strings.TrimSpace(cfg.OpenCodeGoKey[i].APIKey), apiKey) {
			return strings.TrimSpace(cfg.OpenCodeGoKey[i].VisionFallbackModel) != ""
		}
	}
	return false
}

// opencodeGoModelIsExcluded checks if a model is excluded by either auth
// attribute excluded_models or config ExcludedModels for this API key.
func opencodeGoModelIsExcluded(model string, cfg *config.Config, auth *cliproxyauth.Auth) bool {
	if auth != nil && auth.Attributes != nil {
		if opencodeGoModelExcluded(model, auth.Attributes["excluded_models"]) {
			return true
		}
	}
	apiKey := opencodeGoAPIKey(auth)
	if apiKey == "" || cfg == nil {
		return false
	}
	for i := range cfg.OpenCodeGoKey {
		if strings.EqualFold(strings.TrimSpace(cfg.OpenCodeGoKey[i].APIKey), apiKey) {
			excluded := strings.Join(cfg.OpenCodeGoKey[i].ExcludedModels, ",")
			return opencodeGoModelExcluded(model, excluded)
		}
	}
	return false
}
