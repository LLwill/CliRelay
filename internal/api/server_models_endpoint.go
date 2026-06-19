package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	internalrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/routing"
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
					if !modelInSet(id, allowedModels) && !ccSwitchRequestModelAllowedForTarget(id, routeCtx, allowedModels) {
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
			resp.Data = filterModelsForCcSwitchRoute(resp.Data, routeCtx)
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

func ccSwitchRequestModelAllowedForTarget(id string, route *internalrouting.PathRouteContext, allowedModels map[string]struct{}) bool {
	id = strings.TrimSpace(id)
	if id == "" || route == nil || route.CcSwitch == nil || len(allowedModels) == 0 {
		return false
	}
	for _, mapping := range route.CcSwitch.ModelMappings {
		target := strings.TrimSpace(mapping.TargetModel)
		request := strings.TrimSpace(mapping.RequestModel)
		if target == "" || request == "" || !strings.EqualFold(target, id) {
			continue
		}
		if modelInSet(request, allowedModels) {
			return true
		}
	}
	return false
}

func filterModelsForCcSwitchRoute(models []map[string]interface{}, route *internalrouting.PathRouteContext) []map[string]interface{} {
	if route == nil || route.CcSwitch == nil {
		return models
	}
	if strings.EqualFold(strings.TrimSpace(route.CcSwitch.ClientType), "codex") {
		return filterCodexModelsForCcSwitchRoute(models, route.CcSwitch)
	}
	return filterTargetModelsForCcSwitchRoute(models, route.CcSwitch)
}

func filterTargetModelsForCcSwitchRoute(models []map[string]interface{}, ccSwitch *internalrouting.CcSwitchRouteContext) []map[string]interface{} {
	ccswitchModels := make(map[string]struct{})
	for _, mapping := range ccSwitch.ModelMappings {
		target := strings.TrimSpace(mapping.TargetModel)
		if target != "" {
			ccswitchModels[target] = struct{}{}
		}
	}
	if dm := strings.TrimSpace(ccSwitch.DefaultModel); dm != "" {
		ccswitchModels[dm] = struct{}{}
	}
	if len(ccswitchModels) == 0 {
		return models
	}
	filtered := make([]map[string]interface{}, 0, len(models))
	for _, model := range models {
		if id, ok := model["id"].(string); ok {
			if _, exists := ccswitchModels[id]; exists {
				filtered = append(filtered, model)
			}
		}
	}
	return filtered
}

func filterCodexModelsForCcSwitchRoute(models []map[string]interface{}, ccSwitch *internalrouting.CcSwitchRouteContext) []map[string]interface{} {
	targetToRequests := make(map[string][]string)
	for _, mapping := range ccSwitch.ModelMappings {
		request := strings.TrimSpace(mapping.RequestModel)
		target := strings.TrimSpace(mapping.TargetModel)
		if request == "" || target == "" {
			continue
		}
		key := strings.ToLower(target)
		targetToRequests[key] = appendUniqueModel(targetToRequests[key], request)
	}
	defaultModel := strings.TrimSpace(ccSwitch.DefaultModel)
	if len(targetToRequests) == 0 && defaultModel == "" {
		return models
	}

	filtered := make([]map[string]interface{}, 0, len(models))
	seen := make(map[string]struct{})
	for _, model := range models {
		id, ok := model["id"].(string)
		if !ok {
			continue
		}
		if requests := targetToRequests[strings.ToLower(strings.TrimSpace(id))]; len(requests) > 0 {
			for _, request := range requests {
				appendModelClone(&filtered, seen, model, request)
			}
			continue
		}
		if defaultModel != "" && strings.EqualFold(strings.TrimSpace(id), defaultModel) {
			appendModelClone(&filtered, seen, model, defaultModel)
		}
	}
	return filtered
}

func appendUniqueModel(models []string, model string) []string {
	for _, existing := range models {
		if strings.EqualFold(existing, model) {
			return models
		}
	}
	return append(models, model)
}

func appendModelClone(models *[]map[string]interface{}, seen map[string]struct{}, model map[string]interface{}, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	key := strings.ToLower(id)
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	clone := make(map[string]interface{}, len(model))
	for k, v := range model {
		clone[k] = v
	}
	clone["id"] = id
	*models = append(*models, clone)
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
