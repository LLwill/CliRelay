package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/claude"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/openai"
)

// unifiedModelsHandler creates a unified handler for the /v1/models endpoint
// that routes to different handlers based on the User-Agent header.
// If User-Agent starts with "claude-cli", it routes to Claude handler,
// otherwise it routes to OpenAI handler.
// It also filters the returned models based on the API key's allowed-models restriction.
func (s *Server) unifiedModelsHandler(openaiHandler *openai.OpenAIAPIHandler, claudeHandler *claude.ClaudeCodeAPIHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var allowedModels map[string]struct{}
		var allowedChannels map[string]struct{}
		allowedChannelGroups := allowedChannelGroupsFromAccessMetadata(c)
		routeCtx := pathRouteContextFromGin(c)
		routeGroup := ""
		if routeCtx != nil {
			routeGroup = routeCtx.Group
		}
		if metadataVal, exists := c.Get("accessMetadata"); exists {
			if metadata, ok := metadataVal.(map[string]string); ok {
				if allowedStr, exists := metadata["allowed-models"]; exists && allowedStr != "" {
					allowedModels = make(map[string]struct{})
					for _, m := range strings.Split(allowedStr, ",") {
						trimmed := strings.TrimSpace(m)
						if trimmed != "" {
							allowedModels[trimmed] = struct{}{}
						}
					}
					if len(allowedModels) == 0 {
						allowedModels = nil
					}
				}
				if allowedStr, exists := metadata["allowed-channels"]; exists && allowedStr != "" {
					allowedChannels = make(map[string]struct{})
					for _, channel := range strings.Split(allowedStr, ",") {
						trimmed := strings.ToLower(strings.TrimSpace(channel))
						if trimmed != "" {
							allowedChannels[trimmed] = struct{}{}
						}
					}
					if len(allowedChannels) == 0 {
						allowedChannels = nil
					}
				}
			}
		}

		scopedRoutingRestricted := s.hasScopedRoutingModelRestriction(routeGroup, allowedChannelGroups)
		if allowedModels == nil && allowedChannels == nil && allowedChannelGroups == nil && routeGroup == "" && !scopedRoutingRestricted {
			userAgent := c.GetHeader("User-Agent")
			if strings.HasPrefix(userAgent, "claude-cli") {
				claudeHandler.ClaudeModels(c)
			} else {
				openaiHandler.OpenAIModels(c)
			}
			return
		}

		recorder := &responseRecorder{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = recorder

		userAgent := c.GetHeader("User-Agent")
		if strings.HasPrefix(userAgent, "claude-cli") {
			claudeHandler.ClaudeModels(c)
		} else {
			openaiHandler.OpenAIModels(c)
		}

		var resp struct {
			Object string                   `json:"object"`
			Data   []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(recorder.body.Bytes(), &resp); err != nil {
			recorder.ResponseWriter.WriteHeader(recorder.statusCode)
			_, _ = recorder.ResponseWriter.Write(recorder.body.Bytes())
			return
		}

		filtered := make([]map[string]interface{}, 0, len(resp.Data))
		for _, model := range resp.Data {
			if id, ok := model["id"].(string); ok {
				if allowedModels != nil {
					if _, allowed := allowedModels[id]; !allowed {
						continue
					}
				}
				if allowedChannels != nil || allowedChannelGroups != nil || routeGroup != "" {
					if s.handlers == nil || s.handlers.AuthManager == nil || !s.handlers.AuthManager.CanServeModelWithScopes(id, allowedChannels, allowedChannelGroups, routeGroup) {
						continue
					}
				}
				if scopedRoutingRestricted && !s.modelAllowedByScopedRoutingGroups(id, routeGroup, allowedChannelGroups) {
					continue
				}
				filtered = append(filtered, model)
			}
		}
		resp.Data = filtered

		if routeCtx != nil && routeCtx.CcSwitch != nil {
			ccswitchModels := make(map[string]struct{})
			for _, mapping := range routeCtx.CcSwitch.ModelMappings {
				target := strings.TrimSpace(mapping.TargetModel)
				if target != "" {
					ccswitchModels[target] = struct{}{}
				}
			}
			if dm := strings.TrimSpace(routeCtx.CcSwitch.DefaultModel); dm != "" {
				ccswitchModels[dm] = struct{}{}
			}
			if len(ccswitchModels) > 0 {
				var ccswitchFiltered []map[string]interface{}
				for _, model := range resp.Data {
					if id, ok := model["id"].(string); ok {
						if _, exists := ccswitchModels[id]; exists {
							ccswitchFiltered = append(ccswitchFiltered, model)
						}
					}
				}
				resp.Data = ccswitchFiltered
			}
		}

		filteredJSON, err := json.Marshal(resp)
		if err != nil {
			recorder.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			return
		}

		recorder.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
		recorder.ResponseWriter.Header().Set("Content-Length", fmt.Sprintf("%d", len(filteredJSON)))
		recorder.ResponseWriter.WriteHeader(http.StatusOK)
		_, _ = recorder.ResponseWriter.Write(filteredJSON)
	}
}

// responseRecorder captures the response body for post-processing.
type responseRecorder struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
}
