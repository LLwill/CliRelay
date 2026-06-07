package serviceapp

import (
	"context"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/watcher/synthesizer"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/wsrelay"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
	log "github.com/sirupsen/logrus"
)

func SyncConfigDerivedAuths(cfg *config.Config, coreManager *coreauth.Manager) {
	if cfg == nil || coreManager == nil {
		return
	}

	ctx := coreauth.WithSkipPersist(context.Background())
	synth := synthesizer.NewConfigSynthesizer()
	auths, err := synth.Synthesize(&synthesizer.SynthesisContext{
		Config:      cfg,
		AuthDir:     cfg.AuthDir,
		Now:         time.Now(),
		IDGenerator: synthesizer.NewStableIDGenerator(),
	})
	if err != nil {
		log.WithError(err).Warn("failed to synthesize config auths during service config reload")
		return
	}

	desiredIDs := make(map[string]struct{}, len(auths))
	for _, next := range auths {
		if next == nil || strings.TrimSpace(next.ID) == "" {
			continue
		}
		desiredIDs[next.ID] = struct{}{}
		if existing, ok := coreManager.GetByID(next.ID); ok && existing != nil {
			next.CreatedAt = existing.CreatedAt
			next.LastRefreshedAt = existing.LastRefreshedAt
			next.NextRefreshAfter = existing.NextRefreshAfter
			_, err = coreManager.Update(ctx, next)
		} else {
			_, err = coreManager.Register(ctx, next)
		}
		if err != nil {
			log.WithError(err).Warnf("failed to apply config auth %s", next.ID)
		}
	}

	for _, existing := range coreManager.List() {
		if existing == nil || strings.TrimSpace(existing.ID) == "" {
			continue
		}
		if !isConfigDerivedAuth(existing) {
			continue
		}
		if _, stillConfigured := desiredIDs[existing.ID]; stillConfigured {
			continue
		}
		if existing.Disabled && existing.Status == coreauth.StatusDisabled {
			continue
		}
		disabled := existing.Clone()
		disabled.Disabled = true
		disabled.Status = coreauth.StatusDisabled
		disabled.StatusMessage = "removed via config update"
		disabled.UpdatedAt = time.Now()
		if _, err := coreManager.Update(ctx, disabled); err != nil {
			log.WithError(err).Warnf("failed to disable removed config auth %s", disabled.ID)
		}
	}
}

func isConfigDerivedAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth.Attributes["source"])), "config:")
}

func FetchAntigravityModels(ctx context.Context, auth *coreauth.Auth, cfg *config.Config) []*sdkmodelcatalog.ModelInfo {
	return executor.FetchAntigravityModels(ctx, auth, cfg)
}

func RegisterExecutorForAuth(coreManager *coreauth.Manager, cfg *config.Config, auth *coreauth.Auth, forceReplace bool, gateway WebsocketGateway) {
	if coreManager == nil || auth == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		if !forceReplace {
			existingExecutor, hasExecutor := coreManager.Executor("codex")
			if hasExecutor {
				if _, isCodexAutoExecutor := existingExecutor.(*executor.CodexAutoExecutor); isCodexAutoExecutor {
					return
				}
			}
		}
		coreManager.RegisterExecutor(executor.NewCodexAutoExecutor(cfg))
		return
	}
	if auth.Disabled {
		return
	}
	if compatProviderKey, _, isCompat := openAICompatInfoFromAuth(auth); isCompat {
		if compatProviderKey == "" {
			compatProviderKey = strings.ToLower(strings.TrimSpace(auth.Provider))
		}
		if compatProviderKey == "" {
			compatProviderKey = "openai-compatibility"
		}
		coreManager.RegisterExecutor(executor.NewOpenAICompatExecutor(compatProviderKey, cfg))
		return
	}
	switch strings.ToLower(auth.Provider) {
	case "gemini":
		coreManager.RegisterExecutor(executor.NewGeminiExecutor(cfg))
	case "vertex":
		coreManager.RegisterExecutor(executor.NewGeminiVertexExecutor(cfg))
	case "gemini-cli":
		coreManager.RegisterExecutor(executor.NewGeminiCLIExecutor(cfg))
	case "aistudio":
		if gateway != nil {
			relay, _ := gateway.RelayValue().(*wsrelay.Manager)
			if relay != nil {
				coreManager.RegisterExecutor(executor.NewAIStudioExecutor(cfg, auth.ID, relay))
			}
		}
		return
	case "antigravity":
		coreManager.RegisterExecutor(executor.NewAntigravityExecutor(cfg))
	case "claude":
		coreManager.RegisterExecutor(executor.NewClaudeExecutor(cfg))
	case "bedrock":
		coreManager.RegisterExecutor(executor.NewBedrockExecutor(cfg))
	case "opencode-go":
		coreManager.RegisterExecutor(executor.NewOpenCodeGoExecutor(cfg))
	case "qwen":
		coreManager.RegisterExecutor(executor.NewQwenExecutor(cfg))
	case "iflow":
		coreManager.RegisterExecutor(executor.NewIFlowExecutor(cfg))
	case "kimi":
		coreManager.RegisterExecutor(executor.NewKimiExecutor(cfg))
	default:
		providerKey := strings.ToLower(strings.TrimSpace(auth.Provider))
		if providerKey == "" {
			providerKey = "openai-compatibility"
		}
		coreManager.RegisterExecutor(executor.NewOpenAICompatExecutor(providerKey, cfg))
	}
}

func openAICompatInfoFromAuth(auth *coreauth.Auth) (providerKey string, compatName string, ok bool) {
	if auth == nil {
		return "", "", false
	}
	if len(auth.Attributes) > 0 {
		providerKey = strings.TrimSpace(auth.Attributes["provider_key"])
		compatName = strings.TrimSpace(auth.Attributes["compat_name"])
		if compatName != "" {
			if providerKey == "" {
				providerKey = compatName
			}
			return strings.ToLower(providerKey), compatName, true
		}
	}
	if strings.EqualFold(strings.TrimSpace(auth.Provider), "openai-compatibility") {
		return "openai-compatibility", strings.TrimSpace(auth.Label), true
	}
	return "", "", false
}
