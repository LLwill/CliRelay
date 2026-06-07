package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	internalrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/routing"
)

func (s *Server) modelRestrictionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		allowedStr := ""
		if metadataVal, exists := c.Get("accessMetadata"); exists {
			if metadata, ok := metadataVal.(map[string]string); ok {
				allowedStr = strings.TrimSpace(metadata["allowed-models"])
			}
		}
		route := pathRouteContextFromGin(c)
		routeGroup := ""
		if route != nil {
			routeGroup = route.Group
		}
		allowedGroups := allowedChannelGroupsFromAccessMetadata(c)

		var ccSwitchAllowed map[string]struct{}
		if route != nil && route.CcSwitch != nil {
			ccSwitchAllowed = make(map[string]struct{})
			for _, mapping := range route.CcSwitch.ModelMappings {
				if t := strings.TrimSpace(mapping.TargetModel); t != "" {
					ccSwitchAllowed[t] = struct{}{}
				}
				if r := strings.TrimSpace(mapping.RequestModel); r != "" {
					ccSwitchAllowed[r] = struct{}{}
				}
			}
			if dm := strings.TrimSpace(route.CcSwitch.DefaultModel); dm != "" {
				ccSwitchAllowed[dm] = struct{}{}
			}
			if len(ccSwitchAllowed) == 0 {
				ccSwitchAllowed = nil
			}
		}

		hasScopedRestriction := s.hasScopedRoutingModelRestriction(routeGroup, allowedGroups)
		if allowedStr == "" && !hasScopedRestriction && ccSwitchAllowed == nil {
			c.Next()
			return
		}

		var allowedModels map[string]struct{}
		if allowedStr != "" {
			allowedModels = make(map[string]struct{})
			for _, m := range strings.Split(allowedStr, ",") {
				trimmed := strings.TrimSpace(m)
				if trimmed != "" {
					allowedModels[trimmed] = struct{}{}
				}
			}
		}
		if ccSwitchAllowed != nil {
			if len(allowedModels) == 0 {
				allowedModels = ccSwitchAllowed
			} else {
				for m := range allowedModels {
					if _, ok := ccSwitchAllowed[m]; !ok {
						delete(allowedModels, m)
					}
				}
			}
		}

		bodyBytes, err := bodyutil.ReadRequestBody(c, bodyutil.DefaultRequestBodyLimit)
		if err != nil {
			if bodyutil.IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.Next()
			return
		}

		var bodyObj struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(bodyBytes, &bodyObj); err != nil || bodyObj.Model == "" {
			c.Next()
			return
		}
		requestedModel := strings.TrimSpace(bodyObj.Model)
		routingModel := mapCcSwitchRequestModelForRestriction(requestedModel, route)

		if len(allowedModels) > 0 && !modelInSet(requestedModel, allowedModels) && !modelInSet(routingModel, allowedModels) {
			abortModelNotAllowed(c, requestedModel)
			return
		}
		if !s.modelAllowedByScopedRoutingGroups(routingModel, routeGroup, allowedGroups) {
			abortModelNotAllowed(c, requestedModel)
			return
		}

		c.Next()
	}
}

func abortModelNotAllowed(c *gin.Context, model string) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": map[string]interface{}{
			"message": fmt.Sprintf("model '%s' is not allowed for this API key", model),
			"type":    "forbidden",
			"code":    "model_not_allowed",
		},
	})
}

func modelInSet(model string, allowed map[string]struct{}) bool {
	model = strings.TrimSpace(model)
	if model == "" || len(allowed) == 0 {
		return false
	}
	_, ok := allowed[model]
	return ok
}

func mapCcSwitchRequestModelForRestriction(model string, route *internalrouting.PathRouteContext) string {
	model = strings.TrimSpace(model)
	if model == "" || route == nil || route.CcSwitch == nil {
		return model
	}
	for _, mapping := range route.CcSwitch.ModelMappings {
		if strings.EqualFold(strings.TrimSpace(mapping.RequestModel), model) {
			target := strings.TrimSpace(mapping.TargetModel)
			if target != "" {
				return target
			}
		}
	}
	return model
}

func (s *Server) hasScopedRoutingModelRestriction(routeGroup string, allowedGroups map[string]struct{}) bool {
	return s.scopedRoutingAllowedModels(routeGroup, allowedGroups) != nil
}

func (s *Server) modelAllowedByScopedRoutingGroups(model string, routeGroup string, allowedGroups map[string]struct{}) bool {
	allowedModels := s.scopedRoutingAllowedModels(routeGroup, allowedGroups)
	if allowedModels == nil {
		return true
	}
	return routeAllowedModelMatches(model, allowedModels)
}

func (s *Server) scopedRoutingAllowedModels(routeGroup string, allowedGroups map[string]struct{}) []string {
	if s == nil || s.cfg == nil {
		return nil
	}
	scopedGroups := make(map[string]struct{})
	if routeGroup = internalrouting.NormalizeGroupName(routeGroup); routeGroup != "" {
		scopedGroups[routeGroup] = struct{}{}
	} else {
		for group := range allowedGroups {
			if normalized := internalrouting.NormalizeGroupName(group); normalized != "" {
				scopedGroups[normalized] = struct{}{}
			}
		}
	}
	if len(scopedGroups) == 0 {
		if s.cfg.Routing.IncludeDefaultGroup {
			scopedGroups["default"] = struct{}{}
		}
	}
	if len(scopedGroups) == 0 {
		return nil
	}

	var allowedModels []string
	for _, group := range s.cfg.Routing.ChannelGroups {
		groupName := internalrouting.NormalizeGroupName(group.Name)
		if _, ok := scopedGroups[groupName]; !ok {
			continue
		}
		if len(group.AllowedModels) == 0 {
			return nil
		}
		allowedModels = append(allowedModels, group.AllowedModels...)
	}
	if len(allowedModels) == 0 {
		return nil
	}
	return allowedModels
}

func routeAllowedModelMatches(model string, allowedModels []string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, allowed := range allowedModels {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if strings.EqualFold(model, allowed) {
			return true
		}
		if idx := strings.Index(model, "/"); idx >= 0 && strings.EqualFold(strings.TrimSpace(model[idx+1:]), allowed) {
			return true
		}
	}
	return false
}
