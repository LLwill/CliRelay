package runtimeconfig

import (
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

const (
	RuntimeSettingGeminiKeys           = "gemini-api-key"
	RuntimeSettingCodexKeys            = "codex-api-key"
	RuntimeSettingClaudeKeys           = "claude-api-key"
	RuntimeSettingBedrockKeys          = "bedrock-api-key"
	RuntimeSettingOpenCodeGoKeys       = "opencode-go-api-key"
	RuntimeSettingOpenAICompatibility  = "openai-compatibility"
	RuntimeSettingVertexCompatKeys     = "vertex-api-key"
	RuntimeSettingClaudeHeaderDefaults = "claude-header-defaults"
	RuntimeSettingKimiHeaderDefaults   = "kimi-header-defaults"
	RuntimeSettingIdentityFingerprint  = "identity-fingerprint"
	RuntimeSettingOAuthExcludedModels  = "oauth-excluded-models"
	RuntimeSettingOAuthModelAlias      = "oauth-model-alias"
	RuntimeSettingPayload              = "payload"
)

type Spec struct {
	Key        string
	Meaningful func(*config.Config) bool
	Value      func(*config.Config) any
	Apply      func(*config.Config, json.RawMessage) bool
}

func Specs() []Spec {
	return []Spec{
		{
			Key: RuntimeSettingGeminiKeys,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.GeminiKey) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{GeminiKey: append([]config.GeminiKey(nil), cfg.GeminiKey...)}
				holder.SanitizeGeminiKeys()
				return holder.GeminiKey
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.GeminiKey
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingGeminiKeys, err)
					return false
				}
				holder := &config.Config{GeminiKey: value}
				holder.SanitizeGeminiKeys()
				cfg.GeminiKey = holder.GeminiKey
				return true
			},
		},
		{
			Key: RuntimeSettingCodexKeys,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.CodexKey) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{CodexKey: append([]config.CodexKey(nil), cfg.CodexKey...)}
				holder.SanitizeCodexKeys()
				return holder.CodexKey
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.CodexKey
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingCodexKeys, err)
					return false
				}
				holder := &config.Config{CodexKey: value}
				holder.SanitizeCodexKeys()
				cfg.CodexKey = holder.CodexKey
				return true
			},
		},
		{
			Key: RuntimeSettingClaudeKeys,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.ClaudeKey) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{ClaudeKey: append([]config.ClaudeKey(nil), cfg.ClaudeKey...)}
				holder.SanitizeClaudeKeys()
				return holder.ClaudeKey
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.ClaudeKey
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingClaudeKeys, err)
					return false
				}
				holder := &config.Config{ClaudeKey: value}
				holder.SanitizeClaudeKeys()
				cfg.ClaudeKey = holder.ClaudeKey
				return true
			},
		},
		{
			Key: RuntimeSettingBedrockKeys,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.BedrockKey) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{BedrockKey: append([]config.BedrockKey(nil), cfg.BedrockKey...)}
				holder.SanitizeBedrockKeys()
				return holder.BedrockKey
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.BedrockKey
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingBedrockKeys, err)
					return false
				}
				holder := &config.Config{BedrockKey: value}
				holder.SanitizeBedrockKeys()
				cfg.BedrockKey = holder.BedrockKey
				return true
			},
		},
		{
			Key: RuntimeSettingOpenCodeGoKeys,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.OpenCodeGoKey) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{OpenCodeGoKey: append([]config.OpenCodeGoKey(nil), cfg.OpenCodeGoKey...)}
				holder.SanitizeOpenCodeGoKeys()
				return holder.OpenCodeGoKey
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.OpenCodeGoKey
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingOpenCodeGoKeys, err)
					return false
				}
				holder := &config.Config{OpenCodeGoKey: value}
				holder.SanitizeOpenCodeGoKeys()
				cfg.OpenCodeGoKey = holder.OpenCodeGoKey
				return true
			},
		},
		{
			Key: RuntimeSettingOpenAICompatibility,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.OpenAICompatibility) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{OpenAICompatibility: append([]config.OpenAICompatibility(nil), cfg.OpenAICompatibility...)}
				holder.SanitizeOpenAICompatibility()
				return holder.OpenAICompatibility
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.OpenAICompatibility
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingOpenAICompatibility, err)
					return false
				}
				holder := &config.Config{OpenAICompatibility: value}
				holder.SanitizeOpenAICompatibility()
				cfg.OpenAICompatibility = holder.OpenAICompatibility
				return true
			},
		},
		{
			Key: RuntimeSettingVertexCompatKeys,
			Meaningful: func(cfg *config.Config) bool {
				return len(cfg.VertexCompatAPIKey) > 0
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{VertexCompatAPIKey: append([]config.VertexCompatKey(nil), cfg.VertexCompatAPIKey...)}
				holder.SanitizeVertexCompatKeys()
				return holder.VertexCompatAPIKey
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value []config.VertexCompatKey
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingVertexCompatKeys, err)
					return false
				}
				holder := &config.Config{VertexCompatAPIKey: value}
				holder.SanitizeVertexCompatKeys()
				cfg.VertexCompatAPIKey = holder.VertexCompatAPIKey
				return true
			},
		},
		{
			Key: RuntimeSettingClaudeHeaderDefaults,
			Meaningful: func(cfg *config.Config) bool {
				return strings.TrimSpace(cfg.ClaudeHeaderDefaults.UserAgent) != "" ||
					strings.TrimSpace(cfg.ClaudeHeaderDefaults.PackageVersion) != "" ||
					strings.TrimSpace(cfg.ClaudeHeaderDefaults.RuntimeVersion) != "" ||
					strings.TrimSpace(cfg.ClaudeHeaderDefaults.Timeout) != ""
			},
			Value: func(cfg *config.Config) any {
				out := cfg.ClaudeHeaderDefaults
				out.UserAgent = strings.TrimSpace(out.UserAgent)
				out.PackageVersion = strings.TrimSpace(out.PackageVersion)
				out.RuntimeVersion = strings.TrimSpace(out.RuntimeVersion)
				out.Timeout = strings.TrimSpace(out.Timeout)
				return out
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value config.ClaudeHeaderDefaults
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingClaudeHeaderDefaults, err)
					return false
				}
				value.UserAgent = strings.TrimSpace(value.UserAgent)
				value.PackageVersion = strings.TrimSpace(value.PackageVersion)
				value.RuntimeVersion = strings.TrimSpace(value.RuntimeVersion)
				value.Timeout = strings.TrimSpace(value.Timeout)
				cfg.ClaudeHeaderDefaults = value
				return true
			},
		},
		{
			Key: RuntimeSettingKimiHeaderDefaults,
			Meaningful: func(cfg *config.Config) bool {
				return strings.TrimSpace(cfg.KimiHeaderDefaults.UserAgent) != "" ||
					strings.TrimSpace(cfg.KimiHeaderDefaults.Platform) != "" ||
					strings.TrimSpace(cfg.KimiHeaderDefaults.Version) != ""
			},
			Value: func(cfg *config.Config) any {
				out := cfg.KimiHeaderDefaults
				out.UserAgent = strings.TrimSpace(out.UserAgent)
				out.Platform = strings.TrimSpace(out.Platform)
				out.Version = strings.TrimSpace(out.Version)
				return out
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value config.KimiHeaderDefaults
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingKimiHeaderDefaults, err)
					return false
				}
				value.UserAgent = strings.TrimSpace(value.UserAgent)
				value.Platform = strings.TrimSpace(value.Platform)
				value.Version = strings.TrimSpace(value.Version)
				cfg.KimiHeaderDefaults = value
				return true
			},
		},
		{
			Key: RuntimeSettingIdentityFingerprint,
			Meaningful: func(cfg *config.Config) bool {
				return codexIdentityFingerprintMeaningful(cfg.IdentityFingerprint.Codex) ||
					claudeIdentityFingerprintMeaningful(cfg.IdentityFingerprint.Claude)
			},
			Value: func(cfg *config.Config) any {
				return config.IdentityFingerprintConfig{
					Codex:  config.NormalizeCodexIdentityFingerprint(cfg.IdentityFingerprint.Codex),
					Claude: config.NormalizeClaudeIdentityFingerprint(cfg.IdentityFingerprint.Claude),
				}
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value config.IdentityFingerprintConfig
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingIdentityFingerprint, err)
					return false
				}
				value.Codex = config.NormalizeCodexIdentityFingerprint(value.Codex)
				value.Claude = config.NormalizeClaudeIdentityFingerprint(value.Claude)
				cfg.IdentityFingerprint = value
				return true
			},
		},
		{
			Key: RuntimeSettingOAuthExcludedModels,
			Meaningful: func(cfg *config.Config) bool {
				return len(config.NormalizeOAuthExcludedModels(cfg.OAuthExcludedModels)) > 0
			},
			Value: func(cfg *config.Config) any {
				return config.NormalizeOAuthExcludedModels(cfg.OAuthExcludedModels)
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value map[string][]string
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingOAuthExcludedModels, err)
					return false
				}
				cfg.OAuthExcludedModels = config.NormalizeOAuthExcludedModels(value)
				return true
			},
		},
		{
			Key: RuntimeSettingOAuthModelAlias,
			Meaningful: func(cfg *config.Config) bool {
				return len(normalizeOAuthModelAliasSetting(cfg.OAuthModelAlias)) > 0
			},
			Value: func(cfg *config.Config) any {
				return normalizeOAuthModelAliasSetting(cfg.OAuthModelAlias)
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value map[string][]config.OAuthModelAlias
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingOAuthModelAlias, err)
					return false
				}
				cfg.OAuthModelAlias = normalizeOAuthModelAliasSetting(value)
				return true
			},
		},
		{
			Key: RuntimeSettingPayload,
			Meaningful: func(cfg *config.Config) bool {
				return payloadConfigMeaningful(cfg.Payload)
			},
			Value: func(cfg *config.Config) any {
				holder := &config.Config{Payload: cfg.Payload}
				holder.SanitizePayloadRules()
				return holder.Payload
			},
			Apply: func(cfg *config.Config, raw json.RawMessage) bool {
				var value config.PayloadConfig
				if err := json.Unmarshal(raw, &value); err != nil {
					log.Warnf("runtimeconfig: decode %s: %v", RuntimeSettingPayload, err)
					return false
				}
				holder := &config.Config{Payload: value}
				holder.SanitizePayloadRules()
				cfg.Payload = holder.Payload
				return true
			},
		},
	}
}

func codexIdentityFingerprintMeaningful(fp config.CodexIdentityFingerprintConfig) bool {
	normalized := config.NormalizeCodexIdentityFingerprint(fp)
	defaults := config.DefaultCodexIdentityFingerprint()
	if normalized.Enabled || strings.TrimSpace(normalized.SessionID) != "" || len(normalized.CustomHeaders) > 0 {
		return true
	}
	return normalized.UserAgent != defaults.UserAgent ||
		normalized.Version != defaults.Version ||
		normalized.Originator != defaults.Originator ||
		normalized.WebsocketBeta != defaults.WebsocketBeta ||
		normalized.SessionMode != defaults.SessionMode
}

func claudeIdentityFingerprintMeaningful(fp config.ClaudeIdentityFingerprintConfig) bool {
	normalized := config.NormalizeClaudeIdentityFingerprint(fp)
	defaults := config.DefaultClaudeIdentityFingerprint()
	if normalized.Enabled || strings.TrimSpace(normalized.SessionID) != "" ||
		strings.TrimSpace(normalized.DeviceID) != "" || len(normalized.CustomHeaders) > 0 {
		return true
	}
	return normalized.CLIVersion != defaults.CLIVersion ||
		normalized.Entrypoint != defaults.Entrypoint ||
		normalized.UserAgent != defaults.UserAgent ||
		normalized.AnthropicBeta != defaults.AnthropicBeta ||
		normalized.StainlessPackageVersion != defaults.StainlessPackageVersion ||
		normalized.StainlessRuntimeVersion != defaults.StainlessRuntimeVersion ||
		normalized.StainlessTimeout != defaults.StainlessTimeout ||
		normalized.SessionMode != defaults.SessionMode
}

func normalizeOAuthModelAliasSetting(entries map[string][]config.OAuthModelAlias) map[string][]config.OAuthModelAlias {
	if len(entries) == 0 {
		return nil
	}
	holder := &config.Config{OAuthModelAlias: entries}
	holder.SanitizeOAuthModelAlias()
	if len(holder.OAuthModelAlias) == 0 {
		return nil
	}
	return holder.OAuthModelAlias
}

func payloadConfigMeaningful(payload config.PayloadConfig) bool {
	return len(payload.Default) > 0 ||
		len(payload.DefaultRaw) > 0 ||
		len(payload.Override) > 0 ||
		len(payload.OverrideRaw) > 0 ||
		len(payload.Filter) > 0
}
