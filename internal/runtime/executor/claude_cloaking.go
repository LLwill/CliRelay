package executor

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// getClientUserAgent extracts the client User-Agent from the gin context.
func getClientUserAgent(ctx context.Context) string {
	if ginCtx, ok := ctx.Value(util.ContextKeyGin).(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		return ginCtx.GetHeader("User-Agent")
	}
	return ""
}

// getCloakConfigFromAuth extracts cloak configuration from auth attributes.
// Returns (cloakMode, strictMode, sensitiveWords, cacheUserID).
func getCloakConfigFromAuth(auth *cliproxyauth.Auth) (string, bool, []string, bool) {
	if auth == nil || auth.Attributes == nil {
		return "auto", false, nil, false
	}

	cloakMode := auth.Attributes["cloak_mode"]
	if cloakMode == "" {
		cloakMode = "auto"
	}

	strictMode := strings.ToLower(auth.Attributes["cloak_strict_mode"]) == "true"

	var sensitiveWords []string
	if wordsStr := auth.Attributes["cloak_sensitive_words"]; wordsStr != "" {
		sensitiveWords = strings.Split(wordsStr, ",")
		for i := range sensitiveWords {
			sensitiveWords[i] = strings.TrimSpace(sensitiveWords[i])
		}
	}

	cacheUserID := strings.EqualFold(strings.TrimSpace(auth.Attributes["cloak_cache_user_id"]), "true")

	return cloakMode, strictMode, sensitiveWords, cacheUserID
}

// resolveClaudeKeyCloakConfig finds the matching ClaudeKey config and returns its CloakConfig.
func resolveClaudeKeyCloakConfig(cfg *config.Config, auth *cliproxyauth.Auth) *config.CloakConfig {
	if cfg == nil || auth == nil {
		return nil
	}

	apiKey, baseURL := claudeCreds(auth)
	if apiKey == "" {
		return nil
	}

	for i := range cfg.ClaudeKey {
		entry := &cfg.ClaudeKey[i]
		cfgKey := strings.TrimSpace(entry.APIKey)
		cfgBase := strings.TrimSpace(entry.BaseURL)

		if strings.EqualFold(cfgKey, apiKey) {
			if baseURL != "" && cfgBase != "" && !strings.EqualFold(cfgBase, baseURL) {
				continue
			}
			return entry.Cloak
		}
	}

	return nil
}

// injectFakeUserID generates and injects a fake user ID into the request metadata.
// When useCache is false, a new user ID is generated for every call.
func injectFakeUserID(payload []byte, apiKey string, useCache bool) []byte {
	generateID := func() string {
		if useCache {
			return cachedUserID(apiKey)
		}
		return generateFakeUserID()
	}

	metadata := gjson.GetBytes(payload, "metadata")
	if !metadata.Exists() {
		payload, _ = sjson.SetBytes(payload, "metadata.user_id", generateID())
		return payload
	}

	existingUserID := gjson.GetBytes(payload, "metadata.user_id").String()
	if existingUserID == "" || !isValidUserID(existingUserID) {
		payload, _ = sjson.SetBytes(payload, "metadata.user_id", generateID())
	}
	return payload
}

// checkSystemInstructionsWithMode injects Claude Code system prompt.
// In strict mode, it replaces all user system messages.
// In non-strict mode (default), it prepends to existing system messages.
func checkSystemInstructionsWithMode(payload []byte, strictMode bool) []byte {
	system := gjson.GetBytes(payload, "system")
	claudeCodeInstructions := `[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}]`

	if strictMode {
		payload, _ = sjson.SetRawBytes(payload, "system", []byte(claudeCodeInstructions))
		return payload
	}

	if system.IsArray() {
		if gjson.GetBytes(payload, "system.0.text").String() != "You are Claude Code, Anthropic's official CLI for Claude." {
			system.ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() == "text" {
					claudeCodeInstructions, _ = sjson.SetRaw(claudeCodeInstructions, "-1", part.Raw)
				}
				return true
			})
			payload, _ = sjson.SetRawBytes(payload, "system", []byte(claudeCodeInstructions))
		}
	} else {
		payload, _ = sjson.SetRawBytes(payload, "system", []byte(claudeCodeInstructions))
	}
	return payload
}

// applyCloaking applies cloaking transformations to the payload based on config and client.
// Cloaking includes: system prompt injection, fake user ID, and sensitive word obfuscation.
func applyCloaking(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, payload []byte, model string, apiKey string) []byte {
	clientUserAgent := getClientUserAgent(ctx)
	cloakCfg := resolveClaudeKeyCloakConfig(cfg, auth)

	var cloakMode string
	var strictMode bool
	var sensitiveWords []string
	var cacheUserID bool

	if cloakCfg != nil {
		cloakMode = cloakCfg.Mode
		strictMode = cloakCfg.StrictMode
		sensitiveWords = cloakCfg.SensitiveWords
		if cloakCfg.CacheUserID != nil {
			cacheUserID = *cloakCfg.CacheUserID
		}
	}

	if cloakMode == "" {
		attrMode, attrStrict, attrWords, attrCache := getCloakConfigFromAuth(auth)
		cloakMode = attrMode
		if !strictMode {
			strictMode = attrStrict
		}
		if len(sensitiveWords) == 0 {
			sensitiveWords = attrWords
		}
		if cloakCfg == nil || cloakCfg.CacheUserID == nil {
			cacheUserID = attrCache
		}
	} else if cloakCfg == nil || cloakCfg.CacheUserID == nil {
		_, _, _, attrCache := getCloakConfigFromAuth(auth)
		cacheUserID = attrCache
	}

	if !shouldCloak(cloakMode, clientUserAgent) {
		return payload
	}

	if !strings.HasPrefix(model, "claude-3-5-haiku") {
		payload = checkSystemInstructionsWithMode(payload, strictMode)
	}

	payload = injectFakeUserID(payload, apiKey, cacheUserID)

	if len(sensitiveWords) > 0 {
		matcher := buildSensitiveWordMatcher(sensitiveWords)
		payload = obfuscateSensitiveWords(payload, matcher)
	}

	return payload
}
