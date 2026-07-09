package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	managementHandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

const oauthCallbackSuccessHTML = `<html><head><meta charset="utf-8"><title>Authentication successful</title><script>setTimeout(function(){window.close();},5000);</script></head><body><h1>Authentication successful!</h1><p>You can close this window.</p><p>This window will close automatically in 5 seconds.</p></body></html>`

func (s *Server) registerOAuthCallbackRoutes() {
	if s == nil || s.engine == nil {
		return
	}
	s.registerPendingOAuthCallbackRoute("/anthropic/callback", "anthropic")
	s.registerPendingOAuthCallbackRoute("/codex/callback", "codex")
	s.registerPendingOAuthCallbackRoute("/google/callback", "gemini")
	s.registerPendingOAuthCallbackRoute("/iflow/callback", "iflow")
	s.registerPendingOAuthCallbackRoute("/antigravity/callback", "antigravity")
	s.registerPendingOAuthCallbackRoute("/xai/callback", "xai")
}

func (s *Server) registerPendingOAuthCallbackRoute(path, provider string) {
	s.engine.GET(path, func(c *gin.Context) {
		code := c.Query("code")
		state := c.Query("state")
		errStr := c.Query("error")
		if errStr == "" {
			errStr = c.Query("error_description")
		}
		if state != "" {
			_, _ = managementHandlers.WriteOAuthCallbackFileForPendingSession(s.cfg.AuthDir, provider, state, code, errStr)
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, oauthCallbackSuccessHTML)
	})
}
