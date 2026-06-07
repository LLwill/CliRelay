package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// NewServer creates and initializes a new API server instance.
// It wires the HTTP shell around already-constructed runtime managers and
// returns a ready-to-start server façade.
func NewServer(cfg *config.Config, authManager *auth.Manager, accessManager *sdkaccess.Manager, configFilePath string, opts ...ServerOption) *Server {
	optionState := newServerOptionState(opts)
	configureServerMode(cfg)
	engine := newServerEngine(cfg, optionState)
	requestLogger, toggle := configureRequestLoggerMiddleware(engine, cfg, configFilePath, optionState)
	s := newServerRuntimeState(engine, cfg, authManager, accessManager, configFilePath, requestLogger, toggle)
	s.installDynamicMiddleware(configFilePath)
	s.applyInitialRuntimeConfig(cfg, authManager)
	s.configureManagementHandler(cfg, configFilePath, authManager, accessManager, optionState)
	s.setupRoutes()
	s.registerBuiltinModules(cfg, accessManager)
	s.applyRouterConfigurator(optionState, cfg)
	s.configureInitialManagementRoutes(cfg)
	s.configureInitialKeepAlive(optionState)
	s.server = buildHTTPServer(cfg, engine)
	return s
}

func configureTrustedProxies(engine *gin.Engine, trustedProxies []string) {
	if engine == nil {
		return
	}

	proxies := make([]string, 0, len(trustedProxies))
	for _, proxy := range trustedProxies {
		if trimmed := strings.TrimSpace(proxy); trimmed != "" {
			proxies = append(proxies, trimmed)
		}
	}

	if len(proxies) == 0 {
		if err := engine.SetTrustedProxies(nil); err != nil {
			log.Warnf("failed to disable trusted proxies: %v", err)
		}
		return
	}

	if err := engine.SetTrustedProxies(proxies); err != nil {
		log.Warnf("failed to configure trusted proxies %v: %v; forwarded client IP headers will be ignored", proxies, err)
		if fallbackErr := engine.SetTrustedProxies(nil); fallbackErr != nil {
			log.Warnf("failed to disable trusted proxies after configuration error: %v", fallbackErr)
		}
	}
}
