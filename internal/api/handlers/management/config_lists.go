package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/access"
	configaccess "github.com/router-for-me/CLIProxyAPI/v6/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	ampsettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/amp"
	apikeysettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/apikey"
	oauthsettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/oauth"
	providersettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/providers"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

// refreshAPIKeyCache rebuilds the in-memory access provider cache from SQLite.
// Must be called after every API key write operation.
func (h *Handler) refreshAPIKeyCache() {
	if h == nil || h.cfg == nil {
		return
	}
	// Always update the global provider registry (used during config reload and service bootstrap).
	configaccess.Register(&h.cfg.SDKConfig)
	// Also update the live access manager provider snapshot so changes take effect immediately
	// without waiting for a full config reload.
	if h.accessManager != nil {
		_, _ = access.ApplyAccessProviders(h.accessManager, nil, h.cfg)
	}
}

func (h *Handler) apiKeySettings() *apikeysettings.Service {
	if h == nil {
		return apikeysettings.NewService(nil)
	}
	return apikeysettings.NewService(h.sanitizeAllowedChannelsForSave)
}

func providerSettingsService(h *Handler) *providersettings.Service {
	if h == nil {
		return providersettings.NewService(nil, nil)
	}
	return providersettings.NewService(h.cfg, h.validateChannelNames)
}

// Generic helpers for list[string]
func (h *Handler) putStringList(c *gin.Context, set func([]string), after func()) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []string
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []string `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	set(arr)
	if after != nil {
		after()
	}
	h.persist(c)
}

func (h *Handler) patchStringList(c *gin.Context, target *[]string, after func()) {
	var body struct {
		Old   *string `json:"old"`
		New   *string `json:"new"`
		Index *int    `json:"index"`
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if body.Index != nil && body.Value != nil && *body.Index >= 0 && *body.Index < len(*target) {
		(*target)[*body.Index] = *body.Value
		if after != nil {
			after()
		}
		h.persist(c)
		return
	}
	if body.Old != nil && body.New != nil {
		for i := range *target {
			if (*target)[i] == *body.Old {
				(*target)[i] = *body.New
				if after != nil {
					after()
				}
				h.persist(c)
				return
			}
		}
		*target = append(*target, *body.New)
		if after != nil {
			after()
		}
		h.persist(c)
		return
	}
	c.JSON(400, gin.H{"error": "missing fields"})
}

func (h *Handler) deleteFromStringList(c *gin.Context, target *[]string, after func()) {
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && idx >= 0 && idx < len(*target) {
			*target = append((*target)[:idx], (*target)[idx+1:]...)
			if after != nil {
				after()
			}
			h.persist(c)
			return
		}
	}
	if val := strings.TrimSpace(c.Query("value")); val != "" {
		out := make([]string, 0, len(*target))
		for _, v := range *target {
			if strings.TrimSpace(v) != val {
				out = append(out, v)
			}
		}
		*target = out
		if after != nil {
			after()
		}
		h.persist(c)
		return
	}
	c.JSON(400, gin.H{"error": "missing index or value"})
}

// api-keys (legacy simple list — now backed by SQLite)
func (h *Handler) GetAPIKeys(c *gin.Context) {
	c.JSON(200, gin.H{"api-keys": h.apiKeySettings().EnabledKeys()})
}
func (h *Handler) PutAPIKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []string
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []string `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := h.apiKeySettings().ReplaceKeys(arr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.refreshAPIKeyCache()
	c.JSON(200, gin.H{"status": "ok"})
}
func (h *Handler) PatchAPIKeys(c *gin.Context) {
	var body struct {
		Old *string `json:"old"`
		New *string `json:"new"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Old == nil || body.New == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := h.apiKeySettings().PatchKey(*body.Old, *body.New); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.refreshAPIKeyCache()
	c.JSON(200, gin.H{"status": "ok"})
}
func (h *Handler) DeleteAPIKeys(c *gin.Context) {
	if err := h.apiKeySettings().DeleteKey(c.Query("value")); err != nil {
		if errors.Is(err, apikeysettings.ErrMissingValue) {
			c.JSON(400, gin.H{"error": "missing value"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.refreshAPIKeyCache()
	c.JSON(200, gin.H{"status": "ok"})
}

func (h *Handler) GetAPIKeyPermissionProfiles(c *gin.Context) {
	profiles := h.apiKeySettings().PermissionProfiles()
	c.JSON(200, gin.H{
		"api-key-permission-profiles": profiles,
		"items":                       profiles,
	})
}

func (h *Handler) PutAPIKeyPermissionProfiles(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}

	var profiles []usage.APIKeyPermissionProfileRow
	if err = json.Unmarshal(data, &profiles); err != nil {
		var obj struct {
			Items []usage.APIKeyPermissionProfileRow `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		profiles = obj.Items
	}

	if err := h.apiKeySettings().ReplacePermissionProfiles(profiles); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.refreshAPIKeyCache()
	c.JSON(200, gin.H{"status": "ok"})
}

// api-key-entries: backed by SQLite api_keys table
func (h *Handler) GetAPIKeyEntries(c *gin.Context) {
	rows := usage.EffectiveAPIKeyRows(usage.ListAPIKeys())
	entries := make([]config.APIKeyEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, r.ToConfigEntry())
	}
	c.JSON(200, gin.H{"api-key-entries": entries})
}

func (h *Handler) PutAPIKeyEntries(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.APIKeyEntry
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.APIKeyEntry `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	var rows []usage.APIKeyRow
	var auths []*coreauth.Auth
	if h != nil && h.authManager != nil {
		auths = h.authManager.List()
	}
	routingCfg := config.RoutingConfig{}
	if h != nil && h.cfg != nil {
		routingCfg = h.cfg.Routing
	}
	for _, entry := range arr {
		entry.AllowedChannelGroups = uniqueChannelGroups(entry.AllowedChannelGroups)
		cleanedChannels, errValidate := h.sanitizeAllowedChannelsForSave(entry.AllowedChannels)
		if errValidate != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
			return
		}
		entry.AllowedChannels = cleanedChannels
		validatedGroups, errValidate := h.validateAllowedChannelGroups(entry.AllowedChannelGroups)
		if errValidate != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
			return
		}
		entry.AllowedChannelGroups = validatedGroups
		if errValidate := validateRoutingAndAPIKeyRestrictions(&config.Config{
			SDKConfig: config.SDKConfig{
				APIKeyEntries: []config.APIKeyEntry{entry},
			},
			Routing: routingCfg,
		}, auths); errValidate != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
			return
		}
		rows = append(rows, usage.APIKeyRowFromConfig(entry))
	}
	if err := usage.ReplaceAllAPIKeys(rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.refreshAPIKeyCache()
	c.JSON(200, gin.H{"status": "ok"})
}

func (h *Handler) PatchAPIKeyEntry(c *gin.Context) {
	type apiKeyEntryPatch struct {
		Key                  *string   `json:"key"`
		Name                 *string   `json:"name"`
		PermissionProfileID  *string   `json:"permission-profile-id"`
		DailyLimit           *int      `json:"daily-limit"`
		TotalQuota           *int      `json:"total-quota"`
		SpendingLimit        *float64  `json:"spending-limit"`
		ConcurrencyLimit     *int      `json:"concurrency-limit"`
		RPMLimit             *int      `json:"rpm-limit"`
		TPMLimit             *int      `json:"tpm-limit"`
		AllowedModels        *[]string `json:"allowed-models"`
		AllowedChannels      *[]string `json:"allowed-channels"`
		AllowedChannelGroups *[]string `json:"allowed-channel-groups"`
		SystemPrompt         *string   `json:"system-prompt"`
		CreatedAt            *string   `json:"created-at"`
	}
	var body struct {
		Index *int              `json:"index"`
		Match *string           `json:"match"`
		Value *apiKeyEntryPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	// Find existing entry by index or match key
	var targetKey string
	if body.Match != nil {
		targetKey = strings.TrimSpace(*body.Match)
	}
	if targetKey == "" && body.Index != nil {
		rows := usage.ListAPIKeys()
		if *body.Index >= 0 && *body.Index < len(rows) {
			targetKey = rows[*body.Index].Key
		}
	}

	var entry usage.APIKeyRow
	if targetKey != "" {
		if existing := usage.GetAPIKey(targetKey); existing != nil {
			entry = *existing
		} else {
			// If setting a new key via patch, start fresh
			entry.Key = targetKey
		}
	} else {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	// Handle key deletion (empty key)
	if body.Value.Key != nil {
		trimmed := strings.TrimSpace(*body.Value.Key)
		if trimmed == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
			return
		}
		// Key change: delete old, insert new
		if trimmed != targetKey {
			if existing := usage.GetAPIKey(trimmed); existing != nil {
				c.JSON(http.StatusConflict, gin.H{"error": "api key already exists"})
				return
			}
			if err := usage.DeleteAPIKey(targetKey); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		entry.Key = trimmed
	}

	// Apply patches
	if body.Value.Name != nil {
		entry.Name = strings.TrimSpace(*body.Value.Name)
	}
	if body.Value.PermissionProfileID != nil {
		entry.PermissionProfileID = strings.TrimSpace(*body.Value.PermissionProfileID)
	}
	if body.Value.DailyLimit != nil {
		entry.DailyLimit = *body.Value.DailyLimit
	}
	if body.Value.TotalQuota != nil {
		entry.TotalQuota = *body.Value.TotalQuota
	}
	if body.Value.SpendingLimit != nil {
		entry.SpendingLimit = *body.Value.SpendingLimit
	}
	if body.Value.ConcurrencyLimit != nil {
		entry.ConcurrencyLimit = *body.Value.ConcurrencyLimit
	}
	if body.Value.RPMLimit != nil {
		entry.RPMLimit = *body.Value.RPMLimit
	}
	if body.Value.TPMLimit != nil {
		entry.TPMLimit = *body.Value.TPMLimit
	}
	if body.Value.AllowedModels != nil {
		entry.AllowedModels = append([]string(nil), (*body.Value.AllowedModels)...)
	}
	if body.Value.AllowedChannels != nil {
		validated, errValidate := h.sanitizeAllowedChannelsForSave(*body.Value.AllowedChannels)
		if errValidate != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
			return
		}
		entry.AllowedChannels = validated
	}
	if body.Value.AllowedChannelGroups != nil {
		validated, errValidate := h.validateAllowedChannelGroups(*body.Value.AllowedChannelGroups)
		if errValidate != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
			return
		}
		entry.AllowedChannelGroups = validated
	}
	var auths []*coreauth.Auth
	if h != nil && h.authManager != nil {
		auths = h.authManager.List()
	}
	routingCfg := config.RoutingConfig{}
	if h != nil && h.cfg != nil {
		routingCfg = h.cfg.Routing
	}
	if errValidate := validateRoutingAndAPIKeyRestrictions(&config.Config{
		SDKConfig: config.SDKConfig{
			APIKeyEntries: []config.APIKeyEntry{entry.ToConfigEntry()},
		},
		Routing: routingCfg,
	}, auths); errValidate != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
		return
	}
	if body.Value.SystemPrompt != nil {
		entry.SystemPrompt = strings.TrimSpace(*body.Value.SystemPrompt)
	}
	if body.Value.CreatedAt != nil {
		entry.CreatedAt = strings.TrimSpace(*body.Value.CreatedAt)
	}

	if err := usage.UpsertAPIKey(entry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.refreshAPIKeyCache()
	c.JSON(200, gin.H{"status": "ok"})
}

func (h *Handler) DeleteAPIKeyEntry(c *gin.Context) {
	deleteLogs := shouldDeleteAPIKeyLogs(c)
	if val := strings.TrimSpace(c.Query("key")); val != "" {
		if err := usage.DeleteAPIKey(val); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var logsDeleted int64
		if deleteLogs {
			logsDeleted, _ = usage.DeleteLogsByAPIKey(val)
		}
		h.refreshAPIKeyCache()
		c.JSON(200, gin.H{"status": "ok", "logs_deleted": logsDeleted})
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil {
			rows := usage.ListAPIKeys()
			if idx >= 0 && idx < len(rows) {
				keyVal := rows[idx].Key
				if err := usage.DeleteAPIKey(keyVal); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				var logsDeleted int64
				if deleteLogs {
					logsDeleted, _ = usage.DeleteLogsByAPIKey(keyVal)
				}
				h.refreshAPIKeyCache()
				c.JSON(200, gin.H{"status": "ok", "logs_deleted": logsDeleted})
				return
			}
		}
	}
	c.JSON(400, gin.H{"error": "missing key or index"})
}

func shouldDeleteAPIKeyLogs(c *gin.Context) bool {
	raw := strings.TrimSpace(c.Query("delete_logs"))
	if raw == "" {
		return true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return value
}

// gemini-api-key: []GeminiKey
func (h *Handler) GetGeminiKeys(c *gin.Context) {
	c.JSON(200, gin.H{"gemini-api-key": providerSettingsService(h).GeminiKeys()})
}
func (h *Handler) PutGeminiKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.GeminiKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.GeminiKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := providerSettingsService(h).ReplaceGeminiKeys(arr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}
func (h *Handler) PatchGeminiKey(c *gin.Context) {
	var body struct {
		Index *int                             `json:"index"`
		Match *string                          `json:"match"`
		Value *providersettings.GeminiKeyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchGeminiKey(body.Index, body.Match, *body.Value); err != nil {
		if errors.Is(err, providersettings.ErrItemNotFound) {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteGeminiKey(c *gin.Context) {
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		if providerSettingsService(h).DeleteGeminiKeyByAPIKey(val) {
			h.persist(c)
		} else {
			c.JSON(404, gin.H{"error": "item not found"})
		}
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && providerSettingsService(h).DeleteGeminiKeyByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}

// claude-api-key: []ClaudeKey
func (h *Handler) GetClaudeKeys(c *gin.Context) {
	c.JSON(200, gin.H{"claude-api-key": providerSettingsService(h).ClaudeKeys()})
}
func (h *Handler) PutClaudeKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.ClaudeKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.ClaudeKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := providerSettingsService(h).ReplaceClaudeKeys(arr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}
func (h *Handler) PatchClaudeKey(c *gin.Context) {
	var body struct {
		Index *int                             `json:"index"`
		Match *string                          `json:"match"`
		Value *providersettings.ClaudeKeyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchClaudeKey(body.Index, body.Match, *body.Value); err != nil {
		if errors.Is(err, providersettings.ErrItemNotFound) {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteClaudeKey(c *gin.Context) {
	if val := c.Query("api-key"); val != "" {
		providerSettingsService(h).DeleteClaudeKeyByAPIKey(val)
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && providerSettingsService(h).DeleteClaudeKeyByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}

// bedrock-api-key: []BedrockKey
func (h *Handler) GetBedrockKeys(c *gin.Context) {
	c.JSON(200, gin.H{"bedrock-api-key": providerSettingsService(h).BedrockKeys()})
}

func (h *Handler) PutBedrockKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.BedrockKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.BedrockKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := providerSettingsService(h).ReplaceBedrockKeys(arr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) PatchBedrockKey(c *gin.Context) {
	var body struct {
		Index *int                              `json:"index"`
		Match *string                           `json:"match"`
		Value *providersettings.BedrockKeyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchBedrockKey(body.Index, body.Match, *body.Value); err != nil {
		if errors.Is(err, providersettings.ErrItemNotFound) {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteBedrockKey(c *gin.Context) {
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		if providerSettingsService(h).DeleteBedrockKeyByAPIKey(val) {
			h.persist(c)
			return
		}
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	if val := strings.TrimSpace(c.Query("access-key-id")); val != "" {
		if providerSettingsService(h).DeleteBedrockKeyByAccessKeyID(val) {
			h.persist(c)
			return
		}
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	if val := strings.TrimSpace(c.Query("name")); val != "" {
		if providerSettingsService(h).DeleteBedrockKeyByName(val) {
			h.persist(c)
			return
		}
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && providerSettingsService(h).DeleteBedrockKeyByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key, access-key-id, name, or index"})
}

// opencode-go-api-key: []OpenCodeGoKey
func (h *Handler) GetOpenCodeGoKeys(c *gin.Context) {
	c.JSON(200, gin.H{"opencode-go-api-key": providerSettingsService(h).OpenCodeGoKeys()})
}

func (h *Handler) PutOpenCodeGoKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.OpenCodeGoKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.OpenCodeGoKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := providerSettingsService(h).ReplaceOpenCodeGoKeys(arr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) PatchOpenCodeGoKey(c *gin.Context) {
	var body struct {
		APIKey *string                           `json:"api-key"`
		Name   *string                           `json:"name"`
		Index  *int                              `json:"index"`
		Value  *providersettings.OpenCodeGoPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchOpenCodeGoKey(body.Index, body.APIKey, body.Name, *body.Value); err != nil {
		if errors.Is(err, providersettings.ErrItemNotFound) {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteOpenCodeGoKey(c *gin.Context) {
	if apiKey := strings.TrimSpace(c.Query("api-key")); apiKey != "" {
		if providerSettingsService(h).DeleteOpenCodeGoKeyByAPIKey(apiKey) {
			h.persist(c)
			return
		}
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	if name := strings.TrimSpace(c.Query("name")); name != "" {
		if providerSettingsService(h).DeleteOpenCodeGoKeyByName(name) {
			h.persist(c)
			return
		}
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && providerSettingsService(h).DeleteOpenCodeGoKeyByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key, name, or index"})
}

// openai-compatibility: []OpenAICompatibility
func (h *Handler) GetOpenAICompat(c *gin.Context) {
	c.JSON(200, gin.H{"openai-compatibility": providerSettingsService(h).OpenAICompatibility()})
}
func (h *Handler) PutOpenAICompat(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.OpenAICompatibility
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.OpenAICompatibility `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := providerSettingsService(h).ReplaceOpenAICompatibility(arr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}
func (h *Handler) PatchOpenAICompat(c *gin.Context) {
	var body struct {
		Name  *string                                    `json:"name"`
		Index *int                                       `json:"index"`
		Value *providersettings.OpenAICompatibilityPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchOpenAICompatibility(body.Index, body.Name, *body.Value); err != nil {
		if errors.Is(err, providersettings.ErrItemNotFound) {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteOpenAICompat(c *gin.Context) {
	if name := c.Query("name"); name != "" {
		providerSettingsService(h).DeleteOpenAICompatibilityByName(name)
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && providerSettingsService(h).DeleteOpenAICompatibilityByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing name or index"})
}

// vertex-api-key: []VertexCompatKey
func (h *Handler) GetVertexCompatKeys(c *gin.Context) {
	c.JSON(200, gin.H{"vertex-api-key": providerSettingsService(h).VertexCompatKeys()})
}
func (h *Handler) PutVertexCompatKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.VertexCompatKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.VertexCompatKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	providerSettingsService(h).ReplaceVertexCompatKeys(arr)
	h.persist(c)
}
func (h *Handler) PatchVertexCompatKey(c *gin.Context) {
	var body struct {
		Index *int                                `json:"index"`
		Match *string                             `json:"match"`
		Value *providersettings.VertexCompatPatch `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchVertexCompatKey(body.Index, body.Match, *body.Value); err != nil {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteVertexCompatKey(c *gin.Context) {
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		providerSettingsService(h).DeleteVertexCompatKeyByAPIKey(val)
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, errScan := fmt.Sscanf(idxStr, "%d", &idx)
		if errScan == nil && providerSettingsService(h).DeleteVertexCompatKeyByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}

func oauthSettingsService(h *Handler) *oauthsettings.Service {
	if h == nil {
		return oauthsettings.NewService(nil)
	}
	return oauthsettings.NewService(h.cfg)
}

// oauth-excluded-models: map[string][]string
func (h *Handler) GetOAuthExcludedModels(c *gin.Context) {
	c.JSON(200, gin.H{"oauth-excluded-models": oauthSettingsService(h).ExcludedModels()})
}

func (h *Handler) PutOAuthExcludedModels(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var entries map[string][]string
	if err = json.Unmarshal(data, &entries); err != nil {
		var wrapper struct {
			Items map[string][]string `json:"items"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		entries = wrapper.Items
	}
	setting := oauthSettingsService(h).SetExcludedModels(entries)
	h.persistRuntimeSetting(c, usage.RuntimeSettingOAuthExcludedModels, setting)
}

func (h *Handler) PatchOAuthExcludedModels(c *gin.Context) {
	var body struct {
		Provider *string  `json:"provider"`
		Models   []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Provider == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	setting, err := oauthSettingsService(h).PatchExcludedModels(*body.Provider, body.Models)
	if err == oauthsettings.ErrInvalidProvider {
		c.JSON(400, gin.H{"error": "invalid provider"})
		return
	}
	if err == oauthsettings.ErrProviderNotFound {
		c.JSON(404, gin.H{"error": "provider not found"})
		return
	}
	h.persistRuntimeSetting(c, usage.RuntimeSettingOAuthExcludedModels, setting)
}

func (h *Handler) DeleteOAuthExcludedModels(c *gin.Context) {
	setting, err := oauthSettingsService(h).DeleteExcludedModels(c.Query("provider"))
	if err == oauthsettings.ErrInvalidProvider {
		c.JSON(400, gin.H{"error": "missing provider"})
		return
	}
	if err == oauthsettings.ErrProviderNotFound {
		c.JSON(404, gin.H{"error": "provider not found"})
		return
	}
	h.persistRuntimeSetting(c, usage.RuntimeSettingOAuthExcludedModels, setting)
}

// oauth-model-alias: map[string][]OAuthModelAlias
func (h *Handler) GetOAuthModelAlias(c *gin.Context) {
	c.JSON(200, gin.H{"oauth-model-alias": oauthSettingsService(h).ModelAlias()})
}

func (h *Handler) PutOAuthModelAlias(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var entries map[string][]config.OAuthModelAlias
	if err = json.Unmarshal(data, &entries); err != nil {
		var wrapper struct {
			Items map[string][]config.OAuthModelAlias `json:"items"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		entries = wrapper.Items
	}
	setting := oauthSettingsService(h).SetModelAlias(entries)
	h.persistRuntimeSetting(c, usage.RuntimeSettingOAuthModelAlias, setting)
}

func (h *Handler) PatchOAuthModelAlias(c *gin.Context) {
	var body struct {
		Provider *string                  `json:"provider"`
		Channel  *string                  `json:"channel"`
		Aliases  []config.OAuthModelAlias `json:"aliases"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	channelRaw := ""
	if body.Channel != nil {
		channelRaw = *body.Channel
	} else if body.Provider != nil {
		channelRaw = *body.Provider
	}
	setting, err := oauthSettingsService(h).PatchModelAlias(channelRaw, body.Aliases)
	if err == oauthsettings.ErrInvalidChannel {
		c.JSON(400, gin.H{"error": "invalid channel"})
		return
	}
	if err == oauthsettings.ErrChannelNotFound {
		c.JSON(404, gin.H{"error": "channel not found"})
		return
	}
	h.persistRuntimeSetting(c, usage.RuntimeSettingOAuthModelAlias, setting)
}

func (h *Handler) DeleteOAuthModelAlias(c *gin.Context) {
	channel := strings.ToLower(strings.TrimSpace(c.Query("channel")))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(c.Query("provider")))
	}
	if channel == "" {
		c.JSON(400, gin.H{"error": "missing channel"})
		return
	}
	setting, err := oauthSettingsService(h).DeleteModelAlias(channel)
	if err == oauthsettings.ErrChannelNotFound {
		c.JSON(404, gin.H{"error": "channel not found"})
		return
	}
	h.persistRuntimeSetting(c, usage.RuntimeSettingOAuthModelAlias, setting)
}

// codex-api-key: []CodexKey
func (h *Handler) GetCodexKeys(c *gin.Context) {
	c.JSON(200, gin.H{"codex-api-key": providerSettingsService(h).CodexKeys()})
}
func (h *Handler) PutCodexKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.CodexKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.CodexKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if err := providerSettingsService(h).ReplaceCodexKeys(arr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}
func (h *Handler) PatchCodexKey(c *gin.Context) {
	var body struct {
		Index *int                            `json:"index"`
		Match *string                         `json:"match"`
		Value *providersettings.CodexKeyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := providerSettingsService(h).PatchCodexKey(body.Index, body.Match, *body.Value); err != nil {
		if errors.Is(err, providersettings.ErrItemNotFound) {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.persist(c)
}

func (h *Handler) DeleteCodexKey(c *gin.Context) {
	if val := c.Query("api-key"); val != "" {
		providerSettingsService(h).DeleteCodexKeyByAPIKey(val)
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && providerSettingsService(h).DeleteCodexKeyByIndex(idx) {
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}

func ampSettingsService(h *Handler) *ampsettings.Service {
	if h == nil {
		return ampsettings.NewService(nil)
	}
	return ampsettings.NewService(h.cfg)
}

// GetAmpCode returns the complete ampcode configuration.
func (h *Handler) GetAmpCode(c *gin.Context) {
	c.JSON(200, gin.H{"ampcode": ampSettingsService(h).Snapshot()})
}

// GetAmpUpstreamURL returns the ampcode upstream URL.
func (h *Handler) GetAmpUpstreamURL(c *gin.Context) {
	c.JSON(200, gin.H{"upstream-url": ampSettingsService(h).UpstreamURL()})
}

// PutAmpUpstreamURL updates the ampcode upstream URL.
func (h *Handler) PutAmpUpstreamURL(c *gin.Context) {
	h.updateStringField(c, func(v string) { ampSettingsService(h).SetUpstreamURL(v) })
}

// DeleteAmpUpstreamURL clears the ampcode upstream URL.
func (h *Handler) DeleteAmpUpstreamURL(c *gin.Context) {
	ampSettingsService(h).ClearUpstreamURL()
	h.persist(c)
}

// GetAmpUpstreamAPIKey returns the ampcode upstream API key.
func (h *Handler) GetAmpUpstreamAPIKey(c *gin.Context) {
	c.JSON(200, gin.H{"upstream-api-key": ampSettingsService(h).UpstreamAPIKey()})
}

// PutAmpUpstreamAPIKey updates the ampcode upstream API key.
func (h *Handler) PutAmpUpstreamAPIKey(c *gin.Context) {
	h.updateStringField(c, func(v string) { ampSettingsService(h).SetUpstreamAPIKey(v) })
}

// DeleteAmpUpstreamAPIKey clears the ampcode upstream API key.
func (h *Handler) DeleteAmpUpstreamAPIKey(c *gin.Context) {
	ampSettingsService(h).ClearUpstreamAPIKey()
	h.persist(c)
}

// GetAmpRestrictManagementToLocalhost returns the localhost restriction setting.
func (h *Handler) GetAmpRestrictManagementToLocalhost(c *gin.Context) {
	c.JSON(200, gin.H{"restrict-management-to-localhost": ampSettingsService(h).RestrictManagementToLocalhost()})
}

// PutAmpRestrictManagementToLocalhost updates the localhost restriction setting.
func (h *Handler) PutAmpRestrictManagementToLocalhost(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { ampSettingsService(h).SetRestrictManagementToLocalhost(v) })
}

// GetAmpModelMappings returns the ampcode model mappings.
func (h *Handler) GetAmpModelMappings(c *gin.Context) {
	c.JSON(200, gin.H{"model-mappings": ampSettingsService(h).ModelMappings()})
}

// PutAmpModelMappings replaces all ampcode model mappings.
func (h *Handler) PutAmpModelMappings(c *gin.Context) {
	var body struct {
		Value []config.AmpModelMapping `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	ampSettingsService(h).SetModelMappings(body.Value)
	h.persist(c)
}

// PatchAmpModelMappings adds or updates model mappings.
func (h *Handler) PatchAmpModelMappings(c *gin.Context) {
	var body struct {
		Value []config.AmpModelMapping `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	ampSettingsService(h).PatchModelMappings(body.Value)
	h.persist(c)
}

// DeleteAmpModelMappings removes specified model mappings by "from" field.
func (h *Handler) DeleteAmpModelMappings(c *gin.Context) {
	var body struct {
		Value []string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Value) == 0 {
		ampSettingsService(h).DeleteModelMappings(nil)
		h.persist(c)
		return
	}

	ampSettingsService(h).DeleteModelMappings(body.Value)
	h.persist(c)
}

// GetAmpForceModelMappings returns whether model mappings are forced.
func (h *Handler) GetAmpForceModelMappings(c *gin.Context) {
	c.JSON(200, gin.H{"force-model-mappings": ampSettingsService(h).ForceModelMappings()})
}

// PutAmpForceModelMappings updates the force model mappings setting.
func (h *Handler) PutAmpForceModelMappings(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { ampSettingsService(h).SetForceModelMappings(v) })
}

// GetAmpUpstreamAPIKeys returns the ampcode upstream API keys mapping.
func (h *Handler) GetAmpUpstreamAPIKeys(c *gin.Context) {
	c.JSON(200, gin.H{"upstream-api-keys": ampSettingsService(h).UpstreamAPIKeys()})
}

// PutAmpUpstreamAPIKeys replaces all ampcode upstream API keys mappings.
func (h *Handler) PutAmpUpstreamAPIKeys(c *gin.Context) {
	var body struct {
		Value []config.AmpUpstreamAPIKeyEntry `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	ampSettingsService(h).SetUpstreamAPIKeys(body.Value)
	h.persist(c)
}

// PatchAmpUpstreamAPIKeys adds or updates upstream API keys entries.
// Matching is done by upstream-api-key value.
func (h *Handler) PatchAmpUpstreamAPIKeys(c *gin.Context) {
	var body struct {
		Value []config.AmpUpstreamAPIKeyEntry `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	ampSettingsService(h).PatchUpstreamAPIKeys(body.Value)
	h.persist(c)
}

// DeleteAmpUpstreamAPIKeys removes specified upstream API keys entries.
// Body must be JSON: {"value": ["<upstream-api-key>", ...]}.
// If "value" is an empty array, clears all entries.
// If JSON is invalid or "value" is missing/null, returns 400 and does not persist any change.
func (h *Handler) DeleteAmpUpstreamAPIKeys(c *gin.Context) {
	var body struct {
		Value []string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	if body.Value == nil {
		c.JSON(400, gin.H{"error": "missing value"})
		return
	}

	// Empty array means clear all
	if len(body.Value) == 0 {
		_ = ampSettingsService(h).DeleteUpstreamAPIKeys(body.Value)
		h.persist(c)
		return
	}

	if err := ampSettingsService(h).DeleteUpstreamAPIKeys(body.Value); err != nil {
		c.JSON(400, gin.H{"error": "empty value"})
		return
	}
	h.persist(c)
}
