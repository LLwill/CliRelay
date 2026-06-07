package cliproxy

import (
	"context"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	serviceapp "github.com/router-for-me/CLIProxyAPI/v6/sdkbridge/service"
	log "github.com/sirupsen/logrus"
)

func (s *Service) fetchAntigravityRegistryModels(ctx context.Context, auth *coreauth.Auth, excluded []string) []*ModelInfo {
	fetchCtx := ctx
	if fetchCtx == nil {
		// Model fetch can be called from service-owned refresh paths that have no
		// request scope; fall back to a service-owned root context in that case.
		fetchCtx = context.Background()
	}
	// Model registration should not be aborted by unrelated caller cancellation
	// once the service has committed to refreshing the registry.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(fetchCtx), 15*time.Second)
	defer cancel()
	models := serviceapp.FetchAntigravityModels(fetchCtx, auth, s.cfg)
	return applyExcludedModels(models, excluded)
}

func (s *Service) backfillAntigravityModels(source *coreauth.Auth, primaryModels []*ModelInfo) {
	if s == nil || s.coreManager == nil || len(primaryModels) == 0 {
		return
	}

	sourceID := ""
	if source != nil {
		sourceID = strings.TrimSpace(source.ID)
	}

	reg := GlobalModelRegistry()
	if reg == nil {
		return
	}
	for _, candidate := range s.coreManager.List() {
		if candidate == nil || candidate.Disabled {
			continue
		}
		candidateID := strings.TrimSpace(candidate.ID)
		if candidateID == "" || candidateID == sourceID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(candidate.Provider), "antigravity") {
			continue
		}
		if len(reg.GetModelsForClient(candidateID)) > 0 {
			continue
		}

		authKind := strings.ToLower(strings.TrimSpace(candidate.Attributes["auth_kind"]))
		if authKind == "" {
			if kind, _ := candidate.AccountInfo(); strings.EqualFold(kind, "api_key") {
				authKind = "apikey"
			}
		}
		excluded := s.oauthExcludedModels("antigravity", authKind)
		if candidate.Attributes != nil {
			if val, ok := candidate.Attributes["excluded_models"]; ok && strings.TrimSpace(val) != "" {
				excluded = strings.Split(val, ",")
			}
		}

		models := applyExcludedModels(primaryModels, excluded)
		models = applyOAuthModelAlias(s.cfg, "antigravity", authKind, models)
		if len(models) == 0 {
			continue
		}

		reg.RegisterClient(candidateID, "antigravity", applyModelPrefixes(models, candidate.Prefix, s.cfg != nil && s.cfg.ForceModelPrefix))
		log.Debugf("antigravity models backfilled for auth %s using primary model list", candidateID)
	}
}
