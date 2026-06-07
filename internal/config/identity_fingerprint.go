package config

import "strings"

const (
	// Defaults are intentionally aligned with upstream CLIProxyAPI's codex-tui behavior.
	// Update these when upstream codex-tui identity changes.
	DefaultCodexFingerprintUserAgent     = "codex-tui/0.118.0 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9 (codex-tui; 0.118.0)"
	DefaultCodexFingerprintVersion       = ""
	DefaultCodexFingerprintOriginator    = "codex-tui"
	DefaultCodexFingerprintWebsocketBeta = "responses_websockets=2026-02-06"
	DefaultCodexFingerprintSessionMode   = "per-request"

	DefaultClaudeFingerprintCLIVersion              = "2.1.88"
	DefaultClaudeFingerprintEntrypoint              = "cli"
	DefaultClaudeFingerprintAnthropicBeta           = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24"
	DefaultClaudeFingerprintStainlessPackageVersion = "0.74.0"
	DefaultClaudeFingerprintStainlessRuntimeVersion = "v22.13.0"
	DefaultClaudeFingerprintStainlessTimeout        = "600"
	DefaultClaudeFingerprintSessionMode             = "per-request"
)

// IdentityFingerprintConfig groups provider-specific upstream identity settings.
type IdentityFingerprintConfig struct {
	Codex  CodexIdentityFingerprintConfig  `yaml:"codex,omitempty" json:"codex,omitempty"`
	Claude ClaudeIdentityFingerprintConfig `yaml:"claude,omitempty" json:"claude,omitempty"`
}

// CodexIdentityFingerprintConfig configures Codex upstream identity headers.
type CodexIdentityFingerprintConfig struct {
	Enabled       bool              `yaml:"enabled" json:"enabled"`
	UserAgent     string            `yaml:"user-agent,omitempty" json:"user-agent,omitempty"`
	Version       string            `yaml:"version,omitempty" json:"version,omitempty"`
	Originator    string            `yaml:"originator,omitempty" json:"originator,omitempty"`
	WebsocketBeta string            `yaml:"websocket-beta,omitempty" json:"websocket-beta,omitempty"`
	SessionMode   string            `yaml:"session-mode,omitempty" json:"session-mode,omitempty"`
	SessionID     string            `yaml:"session-id,omitempty" json:"session-id,omitempty"`
	CustomHeaders map[string]string `yaml:"custom-headers,omitempty" json:"custom-headers,omitempty"`
}

// DefaultCodexIdentityFingerprint returns the recommended Codex identity template.
func DefaultCodexIdentityFingerprint() CodexIdentityFingerprintConfig {
	return CodexIdentityFingerprintConfig{
		Enabled:       false,
		UserAgent:     DefaultCodexFingerprintUserAgent,
		Version:       DefaultCodexFingerprintVersion,
		Originator:    DefaultCodexFingerprintOriginator,
		WebsocketBeta: DefaultCodexFingerprintWebsocketBeta,
		SessionMode:   DefaultCodexFingerprintSessionMode,
		CustomHeaders: map[string]string{},
	}
}

// ClaudeIdentityFingerprintConfig configures Claude Code-style Anthropic OAuth identity.
type ClaudeIdentityFingerprintConfig struct {
	Enabled                 bool              `yaml:"enabled" json:"enabled"`
	CLIVersion              string            `yaml:"cli-version,omitempty" json:"cli-version,omitempty"`
	Entrypoint              string            `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	UserAgent               string            `yaml:"user-agent,omitempty" json:"user-agent,omitempty"`
	AnthropicBeta           string            `yaml:"anthropic-beta,omitempty" json:"anthropic-beta,omitempty"`
	StainlessPackageVersion string            `yaml:"stainless-package-version,omitempty" json:"stainless-package-version,omitempty"`
	StainlessRuntimeVersion string            `yaml:"stainless-runtime-version,omitempty" json:"stainless-runtime-version,omitempty"`
	StainlessTimeout        string            `yaml:"stainless-timeout,omitempty" json:"stainless-timeout,omitempty"`
	SessionMode             string            `yaml:"session-mode,omitempty" json:"session-mode,omitempty"`
	SessionID               string            `yaml:"session-id,omitempty" json:"session-id,omitempty"`
	DeviceID                string            `yaml:"device-id,omitempty" json:"device-id,omitempty"`
	CustomHeaders           map[string]string `yaml:"custom-headers,omitempty" json:"custom-headers,omitempty"`
}

// DefaultClaudeIdentityFingerprint returns the recommended Claude Code identity template.
func DefaultClaudeIdentityFingerprint() ClaudeIdentityFingerprintConfig {
	cliVersion := DefaultClaudeFingerprintCLIVersion
	entrypoint := DefaultClaudeFingerprintEntrypoint
	return ClaudeIdentityFingerprintConfig{
		Enabled:                 false,
		CLIVersion:              cliVersion,
		Entrypoint:              entrypoint,
		UserAgent:               BuildClaudeFingerprintUserAgent(cliVersion, entrypoint),
		AnthropicBeta:           DefaultClaudeFingerprintAnthropicBeta,
		StainlessPackageVersion: DefaultClaudeFingerprintStainlessPackageVersion,
		StainlessRuntimeVersion: DefaultClaudeFingerprintStainlessRuntimeVersion,
		StainlessTimeout:        DefaultClaudeFingerprintStainlessTimeout,
		SessionMode:             DefaultClaudeFingerprintSessionMode,
		CustomHeaders:           map[string]string{},
	}
}

// SanitizeIdentityFingerprint normalizes provider identity fingerprint config.
func (cfg *Config) SanitizeIdentityFingerprint() {
	if cfg == nil {
		return
	}
	cfg.IdentityFingerprint.Codex = NormalizeCodexIdentityFingerprint(cfg.IdentityFingerprint.Codex)
	cfg.IdentityFingerprint.Claude = NormalizeClaudeIdentityFingerprint(cfg.IdentityFingerprint.Claude)
}

// NormalizeCodexIdentityFingerprint trims user input and applies safe defaults
// for fields that participate in Codex upstream identity.
func NormalizeCodexIdentityFingerprint(in CodexIdentityFingerprintConfig) CodexIdentityFingerprintConfig {
	out := in
	out.UserAgent = strings.TrimSpace(out.UserAgent)
	out.Version = strings.TrimSpace(out.Version)
	out.Originator = strings.TrimSpace(out.Originator)
	out.WebsocketBeta = strings.TrimSpace(out.WebsocketBeta)
	out.SessionMode = strings.TrimSpace(strings.ToLower(out.SessionMode))
	out.SessionID = strings.TrimSpace(out.SessionID)

	if out.UserAgent == "" {
		out.UserAgent = DefaultCodexFingerprintUserAgent
	}
	if out.Version == "" {
		out.Version = DefaultCodexFingerprintVersion
	}
	if out.Originator == "" {
		out.Originator = DefaultCodexFingerprintOriginator
	}
	if out.WebsocketBeta == "" {
		out.WebsocketBeta = DefaultCodexFingerprintWebsocketBeta
	}
	if out.SessionMode == "" {
		out.SessionMode = DefaultCodexFingerprintSessionMode
	}
	if out.SessionMode != "server-stable" && out.SessionMode != "fixed" && out.SessionMode != "per-request" {
		out.SessionMode = DefaultCodexFingerprintSessionMode
	}

	if len(out.CustomHeaders) > 0 {
		cleaned := make(map[string]string, len(out.CustomHeaders))
		for key, value := range out.CustomHeaders {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			cleaned[key] = value
		}
		out.CustomHeaders = cleaned
	} else {
		out.CustomHeaders = map[string]string{}
	}
	return out
}

// BuildClaudeFingerprintUserAgent builds the Claude Code User-Agent value from
// the CLI version and entrypoint dimensions.
func BuildClaudeFingerprintUserAgent(cliVersion, entrypoint string) string {
	cliVersion = strings.TrimSpace(cliVersion)
	entrypoint = strings.TrimSpace(entrypoint)
	if cliVersion == "" {
		cliVersion = DefaultClaudeFingerprintCLIVersion
	}
	if entrypoint == "" {
		entrypoint = DefaultClaudeFingerprintEntrypoint
	}
	return "claude-cli/" + cliVersion + " (external, " + entrypoint + ")"
}

// NormalizeClaudeIdentityFingerprint trims user input and applies safe defaults
// for fields that participate in Claude Code-style Anthropic OAuth identity.
func NormalizeClaudeIdentityFingerprint(in ClaudeIdentityFingerprintConfig) ClaudeIdentityFingerprintConfig {
	out := in
	out.CLIVersion = strings.TrimSpace(out.CLIVersion)
	out.Entrypoint = strings.TrimSpace(out.Entrypoint)
	out.UserAgent = strings.TrimSpace(out.UserAgent)
	out.AnthropicBeta = strings.TrimSpace(out.AnthropicBeta)
	out.StainlessPackageVersion = strings.TrimSpace(out.StainlessPackageVersion)
	out.StainlessRuntimeVersion = strings.TrimSpace(out.StainlessRuntimeVersion)
	out.StainlessTimeout = strings.TrimSpace(out.StainlessTimeout)
	out.SessionMode = strings.TrimSpace(strings.ToLower(out.SessionMode))
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.DeviceID = strings.TrimSpace(out.DeviceID)

	if out.CLIVersion == "" {
		out.CLIVersion = DefaultClaudeFingerprintCLIVersion
	}
	if out.Entrypoint == "" {
		out.Entrypoint = DefaultClaudeFingerprintEntrypoint
	}
	if out.UserAgent == "" {
		out.UserAgent = BuildClaudeFingerprintUserAgent(out.CLIVersion, out.Entrypoint)
	}
	if out.AnthropicBeta == "" {
		out.AnthropicBeta = DefaultClaudeFingerprintAnthropicBeta
	}
	if out.StainlessPackageVersion == "" {
		out.StainlessPackageVersion = DefaultClaudeFingerprintStainlessPackageVersion
	}
	if out.StainlessRuntimeVersion == "" {
		out.StainlessRuntimeVersion = DefaultClaudeFingerprintStainlessRuntimeVersion
	}
	if out.StainlessTimeout == "" {
		out.StainlessTimeout = DefaultClaudeFingerprintStainlessTimeout
	}
	if out.SessionMode == "" {
		out.SessionMode = DefaultClaudeFingerprintSessionMode
	}
	if out.SessionMode != "server-stable" && out.SessionMode != "fixed" && out.SessionMode != "per-request" {
		out.SessionMode = DefaultClaudeFingerprintSessionMode
	}

	if len(out.CustomHeaders) > 0 {
		cleaned := make(map[string]string, len(out.CustomHeaders))
		for key, value := range out.CustomHeaders {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			cleaned[key] = value
		}
		out.CustomHeaders = cleaned
	} else {
		out.CustomHeaders = map[string]string{}
	}
	return out
}
