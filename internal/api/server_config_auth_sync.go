package api

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/access"
	internalserviceapp "github.com/router-for-me/CLIProxyAPI/v6/internal/app/service"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func (s *Server) applyAccessConfig(oldCfg, newCfg *config.Config) {
	if s == nil || s.accessManager == nil || newCfg == nil {
		return
	}
	if _, err := access.ApplyAccessProviders(s.accessManager, oldCfg, newCfg); err != nil {
		return
	}
}

func (s *Server) syncConfigDerivedAuths(cfg *config.Config) {
	if s == nil || s.handlers == nil || s.handlers.AuthManager == nil || cfg == nil {
		return
	}
	internalserviceapp.SyncConfigDerivedAuths(cfg, s.handlers.AuthManager)
}
