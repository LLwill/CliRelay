package cliproxy

import (
	"context"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	serviceapp "github.com/router-for-me/CLIProxyAPI/v6/sdkbridge/service"
)

func (s *Service) fetchClaudeRegistryModels(ctx context.Context, auth *coreauth.Auth, excluded []string) []*ModelInfo {
	fetchCtx := ctx
	if fetchCtx == nil {
		fetchCtx = context.Background()
	}
	// Model registration should not be aborted by unrelated caller cancellation
	// once the service has committed to refreshing the registry.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(fetchCtx), 15*time.Second)
	defer cancel()
	models := serviceapp.FetchClaudeModels(fetchCtx, auth, s.cfg)
	return applyExcludedModels(models, excluded)
}
