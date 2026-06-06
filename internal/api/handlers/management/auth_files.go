package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/antigravity"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/claude"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	geminiAuth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/gemini"
	iflowauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/iflow"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kimi"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/qwen"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	managementauthfiles "github.com/router-for-me/CLIProxyAPI/v6/internal/management/authfiles"
	oauthcallback "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/callback"
	antigravityprovider "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/antigravity"
	claudeprovider "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/claude"
	codexprovider "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/codex"
	geminicli "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/geminicli"
	iflowprovider "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/iflow"
	kimiprovider "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/kimi"
	qwenprovider "github.com/router-for-me/CLIProxyAPI/v6/internal/management/oauth/providers/qwen"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	anthropicCallbackPort                     = 54545
	geminiCallbackPort                        = 8085
	codexCallbackPort                         = 1455
	oauthCallbackWaitTimeout                  = oauthSessionTTL
	managementOAuthProfileResponseLimit int64 = 64 << 10
)

func isWebUIRequest(c *gin.Context) bool {
	raw := strings.TrimSpace(c.Query("is_webui"))
	if raw == "" {
		return false
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (h *Handler) managementCallbackURL(path string) (string, error) {
	if h == nil || h.cfg == nil || h.cfg.Port <= 0 {
		return "", fmt.Errorf("server port is not configured")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	scheme := "http"
	if h.cfg.TLS.Enable {
		scheme = "https"
	}
	return fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, h.cfg.Port, path), nil
}

func (h *Handler) ListAuthFiles(c *gin.Context) {
	if h == nil {
		c.JSON(500, gin.H{"error": "handler not initialized"})
		return
	}
	if h.authManager == nil {
		h.listAuthFilesFromDisk(c)
		return
	}
	auths := h.authManager.List()
	files := make([]gin.H, 0, len(auths))
	for _, auth := range auths {
		if entry := h.buildAuthFileEntry(auth); entry != nil {
			files = append(files, entry)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		nameI, _ := files[i]["name"].(string)
		nameJ, _ := files[j]["name"].(string)
		return strings.ToLower(nameI) < strings.ToLower(nameJ)
	})
	c.JSON(200, gin.H{"files": files})
}

// GetAuthFileModels returns the models supported by a specific auth file
func (h *Handler) GetAuthFileModels(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}

	models := managementauthfiles.ListModelEntries(h.authManager, registry.GetGlobalRegistry(), name)
	c.JSON(200, gin.H{"models": models})
}

// List auth files from disk when the auth manager is unavailable.
func (h *Handler) listAuthFilesFromDisk(c *gin.Context) {
	files, err := managementauthfiles.ListDiskEntries(h.cfg.AuthDir, time.Now())
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("failed to read auth dir: %v", err)})
		return
	}
	c.JSON(200, gin.H{"files": files})
}

func (h *Handler) buildAuthFileEntry(auth *coreauth.Auth) gin.H {
	entry := managementauthfiles.BuildEntry(auth, managementauthfiles.EntryOptions{
		OnStatError: func(path string, err error) {
			log.WithError(err).Warnf("failed to stat auth file %s", path)
		},
	})
	if entry == nil {
		return nil
	}
	return gin.H(entry)
}

// Download single auth file by name
func (h *Handler) DownloadAuthFile(c *gin.Context) {
	name, errValidate := managementauthfiles.ValidateFileQueryName(c.Query("name"), true)
	if errValidate != nil {
		c.JSON(400, gin.H{"error": errValidate.Error()})
		return
	}
	full := managementauthfiles.FilePath(h.cfg.AuthDir, name)
	_, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(404, gin.H{"error": "file not found"})
		} else {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to read file: %v", err)})
		}
		return
	}
	c.FileAttachment(full, name)
}

// Upload auth file: multipart or raw JSON with ?name=
func (h *Handler) UploadAuthFile(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	ctx := c.Request.Context()
	if c.Request != nil && c.Request.Body != nil && c.Writer != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyutil.AuthFileBodyLimit+(64<<10))
	}
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		file, err := c.FormFile("file")
		if err != nil {
			if bodyutil.IsTooLarge(err) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
			return
		}
		if file.Size > bodyutil.AuthFileBodyLimit {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
			return
		}
		name, errValidate := managementauthfiles.ValidateUploadedFileName(file.Filename)
		if errValidate != nil {
			c.JSON(400, gin.H{"error": errValidate.Error()})
			return
		}
		dst := managementauthfiles.FilePath(h.cfg.AuthDir, name)
		if errSave := c.SaveUploadedFile(file, dst); errSave != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to save file: %v", errSave)})
			return
		}
		data, errRead := os.ReadFile(dst)
		if errRead != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to read saved file: %v", errRead)})
			return
		}
		if errReg := h.registerAuthFromFile(ctx, dst, data); errReg != nil {
			c.JSON(500, gin.H{"error": errReg.Error()})
			return
		}
		if errPersist := h.persistAuthFileChange(ctx, "Update auth "+name, dst); errPersist != nil {
			c.JSON(500, gin.H{"error": errPersist.Error()})
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
		return
	}
	name, errValidate := managementauthfiles.ValidateFileQueryName(c.Query("name"), true)
	if errValidate != nil {
		c.JSON(400, gin.H{"error": errValidate.Error()})
		return
	}
	data, err := bodyutil.ReadRequestBody(c, bodyutil.AuthFileBodyLimit)
	if err != nil {
		if bodyutil.IsTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	dst := managementauthfiles.FilePath(h.cfg.AuthDir, name)
	if errWrite := os.WriteFile(dst, data, 0o600); errWrite != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("failed to write file: %v", errWrite)})
		return
	}
	if err = h.registerAuthFromFile(ctx, dst, data); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if errPersist := h.persistAuthFileChange(ctx, "Update auth "+filepath.Base(name), dst); errPersist != nil {
		c.JSON(500, gin.H{"error": errPersist.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "ok"})
}

// Delete auth files: single by name or all
func (h *Handler) DeleteAuthFile(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	ctx := c.Request.Context()
	if managementauthfiles.IsDeleteAllValue(c.Query("all")) {
		entries, err := os.ReadDir(h.cfg.AuthDir)
		if err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to read auth dir: %v", err)})
			return
		}
		deleted := 0
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !managementauthfiles.IsJSONFileName(name) {
				continue
			}
			full := managementauthfiles.FilePath(h.cfg.AuthDir, name)
			deletedChannels := deletedAuthChannelIdentifiers(h.findAuthByNameOrID(name))
			if err = os.Remove(full); err == nil {
				if errDel := h.deleteTokenRecord(ctx, full); errDel != nil {
					c.JSON(500, gin.H{"error": errDel.Error()})
					return
				}
				deleted++
				h.removeAuth(ctx, full)
				if errCleanup := h.removeChannelReferences(deletedChannels); errCleanup != nil {
					c.JSON(500, gin.H{"error": errCleanup.Error()})
					return
				}
			}
		}
		c.JSON(200, gin.H{"status": "ok", "deleted": deleted})
		return
	}
	name, errValidate := managementauthfiles.ValidateFileQueryName(c.Query("name"), false)
	if errValidate != nil {
		c.JSON(400, gin.H{"error": errValidate.Error()})
		return
	}
	full := managementauthfiles.FilePath(h.cfg.AuthDir, name)
	deletedChannels := deletedAuthChannelIdentifiers(h.findAuthByNameOrID(name))
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			c.JSON(404, gin.H{"error": "file not found"})
		} else {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to remove file: %v", err)})
		}
		return
	}
	if err := h.deleteTokenRecord(ctx, full); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h.removeAuth(ctx, full)
	if err := h.removeChannelReferences(deletedChannels); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "ok"})
}

func (h *Handler) findAuthByNameOrID(name string) *coreauth.Auth {
	if h == nil {
		return nil
	}
	return managementauthfiles.FindByNameOrID(h.authManager, name)
}

func deletedAuthChannelIdentifiers(auth *coreauth.Auth) []string {
	return managementauthfiles.DeletedChannelIdentifiers(auth)
}

func (h *Handler) registerAuthFromFile(ctx context.Context, path string, data []byte) error {
	if h.authManager == nil {
		return nil
	}
	authDir := ""
	if h.cfg != nil {
		authDir = h.cfg.AuthDir
	}
	return managementauthfiles.Registrar{
		Manager: h.authManager,
		AuthDir: authDir,
	}.RegisterFile(ctx, path, data)
}

// PatchAuthFileStatus toggles the disabled state of an auth file
func (h *Handler) PatchAuthFileStatus(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req struct {
		Name     string `json:"name"`
		Disabled *bool  `json:"disabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if req.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "disabled is required"})
		return
	}

	ctx := c.Request.Context()

	targetAuth := h.findAuthByNameOrID(name)
	if targetAuth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}

	if errPatch := managementauthfiles.ApplyStatusPatch(targetAuth, *req.Disabled, time.Now()); errPatch != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errPatch.Error()})
		return
	}

	if _, err := h.authManager.Update(ctx, targetAuth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "disabled": *req.Disabled})
}

// PatchAuthFileFields updates editable fields of an auth file.
func (h *Handler) PatchAuthFileFields(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req managementauthfiles.FieldPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	ctx := c.Request.Context()

	targetAuth := h.findAuthByNameOrID(name)
	if targetAuth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}

	patchResult, errPatch := managementauthfiles.ApplyFieldPatch(targetAuth, req, managementauthfiles.FieldPatchOptions{
		ValidateLabel: h.validateAuthChannelName,
	})
	if errPatch != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errPatch.Error()})
		return
	}

	if _, err := h.authManager.Update(ctx, targetAuth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth: %v", err)})
		return
	}
	if path := strings.TrimSpace(managementauthfiles.Attribute(targetAuth, "path")); path != "" {
		if err := h.persistAuthFileChange(ctx, "Update auth "+targetAuth.FileName, path); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if len(patchResult.OldChannelIdentifiers) > 0 {
		if err := h.renameChannelReferences(patchResult.OldChannelIdentifiers, patchResult.NewChannelLabel); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) removeAuth(ctx context.Context, id string) {
	if h == nil || h.authManager == nil {
		return
	}
	authDir := ""
	if h.cfg != nil {
		authDir = h.cfg.AuthDir
	}
	managementauthfiles.RemoveFromManager(ctx, h.authManager, authDir, id)
}

func (h *Handler) deleteTokenRecord(ctx context.Context, path string) error {
	return h.authFileRepository().Delete(ctx, path)
}

func (h *Handler) authFileRepository() managementauthfiles.Repository {
	if h == nil {
		return managementauthfiles.Repository{}
	}
	store := h.tokenStore
	if store == nil {
		store = sdkAuth.GetTokenStore()
		h.tokenStore = store
	}
	baseDir := ""
	if h.cfg != nil {
		baseDir = h.cfg.AuthDir
	}
	return managementauthfiles.Repository{
		Store:        store,
		BaseDir:      baseDir,
		PostAuthHook: h.postAuthHook,
	}
}

func (h *Handler) persistAuthFileChange(ctx context.Context, message string, paths ...string) error {
	return h.authFileRepository().PersistChange(ctx, message, paths...)
}

func (h *Handler) saveTokenRecord(ctx context.Context, record *coreauth.Auth) (string, error) {
	return h.authFileRepository().Save(ctx, record)
}

func (h *Handler) RequestAnthropicToken(c *gin.Context) {
	ctx := detachedAuthContext(c)

	fmt.Println("Initializing Claude authentication...")

	// Generate PKCE codes
	pkceCodes, err := claude.GeneratePKCECodes()
	if err != nil {
		log.Errorf("Failed to generate PKCE codes: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PKCE codes"})
		return
	}

	// Generate random state parameter
	state, err := misc.GenerateRandomState()
	if err != nil {
		log.Errorf("Failed to generate state parameter: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	// Initialize Claude auth service
	anthropicAuth := claude.NewClaudeAuth(h.cfg)

	isWebUI := isWebUIRequest(c)
	redirectURI := claude.RedirectURI
	var forwarder *oauthcallback.Forwarder
	callbackPort := anthropicCallbackPort
	if isWebUI {
		if targetURL, errTarget := h.managementCallbackURL("/anthropic/callback"); errTarget != nil {
			log.WithError(errTarget).Warn("failed to compute anthropic callback target, falling back to Claude platform callback")
			redirectURI = claude.PlatformRedirectURI
		} else {
			var errStart error
			if forwarder, callbackPort, errStart = oauthcallback.StartOnAvailablePort(anthropicCallbackPort, "anthropic", targetURL); errStart != nil {
				log.WithError(errStart).Warn("failed to start anthropic callback forwarder, falling back to Claude platform callback")
				redirectURI = claude.PlatformRedirectURI
			} else {
				redirectURI = fmt.Sprintf("http://localhost:%d/callback", callbackPort)
			}
		}
	}

	// Generate authorization URL after choosing the redirect URI; the same value must be used for token exchange.
	authURL, state, err := anthropicAuth.GenerateAuthURLWithRedirectURI(state, pkceCodes, redirectURI)
	if err != nil {
		if forwarder != nil {
			oauthcallback.StopInstance(ctx, callbackPort, forwarder)
		}
		log.Errorf("Failed to generate authorization URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}

	RegisterOAuthSession(state, "anthropic")

	go func() {
		if forwarder != nil {
			defer oauthcallback.StopInstance(ctx, callbackPort, forwarder)
		}

		fmt.Println("Waiting for authentication callback...")
		// Keep the callback waiter alive for the full session lifetime.
		resultMap, errWait := WaitOAuthCallbackFile(h.cfg.AuthDir, "anthropic", state, oauthCallbackWaitTimeout)
		if errWait != nil {
			if errors.Is(errWait, errOAuthSessionNotPending) {
				return
			}
			SetOAuthSessionError(state, "Timeout waiting for OAuth callback")
			authErr := claude.NewAuthenticationError(claude.ErrCallbackTimeout, errWait)
			log.Error(claude.GetUserFriendlyMessage(authErr))
			return
		}
		if errStr := resultMap["error"]; errStr != "" {
			oauthErr := claude.NewOAuthError(errStr, "", http.StatusBadRequest)
			log.Error(claude.GetUserFriendlyMessage(oauthErr))
			SetOAuthSessionError(state, "Bad request")
			return
		}
		if resultMap["state"] != state {
			authErr := claude.NewAuthenticationError(claude.ErrInvalidState, fmt.Errorf("expected %s, got %s", state, resultMap["state"]))
			log.Error(claude.GetUserFriendlyMessage(authErr))
			SetOAuthSessionError(state, "State code error")
			return
		}

		// Parse code (Claude may append state after '#')
		rawCode := resultMap["code"]
		code := strings.Split(rawCode, "#")[0]

		// Exchange code for tokens using internal auth service
		bundle, errExchange := anthropicAuth.ExchangeCodeForTokensWithRedirectURI(ctx, code, state, pkceCodes, redirectURI)
		if errExchange != nil {
			authErr := claude.NewAuthenticationError(claude.ErrCodeExchangeFailed, errExchange)
			log.Errorf("Failed to exchange authorization code for tokens: %v", authErr)
			SetOAuthSessionError(state, "Failed to exchange authorization code for tokens")
			return
		}

		// Create token storage
		tokenStorage := anthropicAuth.CreateTokenStorage(bundle)
		record := claudeprovider.RecordFromTokenStorage(tokenStorage)
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		if bundle.APIKey != "" {
			fmt.Println("API key obtained and saved")
		}
		fmt.Println("You can now use Claude services through this CLI")
		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("anthropic")
	}()

	c.JSON(200, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestGeminiCLIToken(c *gin.Context) {
	ctx := detachedAuthContext(c)
	proxyHTTPClient := util.SetProxy(&h.cfg.SDKConfig, util.NewHTTPClient(util.DefaultHTTPClientTimeout))
	ctx = context.WithValue(ctx, oauth2.HTTPClient, proxyHTTPClient)

	// Optional project ID from query
	projectID := c.Query("project_id")

	fmt.Println("Initializing Google authentication...")

	clientID, clientSecret := h.cfg.OAuthClientCredentials(config.OAuthClientGemini)
	if strings.TrimSpace(clientID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "gemini oauth client-id not configured"})
		return
	}

	// OAuth2 configuration
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  fmt.Sprintf("http://localhost:%d/oauth2callback", geminiAuth.DefaultCallbackPort),
		Scopes:       geminiAuth.Scopes,
		Endpoint:     google.Endpoint,
	}

	state, errState := misc.GenerateRandomState()
	if errState != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	RegisterOAuthSession(state, "gemini")

	isWebUI := isWebUIRequest(c)
	var forwarder *oauthcallback.Forwarder
	callbackPort := geminiCallbackPort
	if isWebUI {
		targetURL, errTarget := h.managementCallbackURL("/google/callback")
		if errTarget != nil {
			log.WithError(errTarget).Error("failed to compute gemini callback target")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "callback server unavailable"})
			return
		}
		var errStart error
		if forwarder, callbackPort, errStart = oauthcallback.StartOnAvailablePort(geminiCallbackPort, "gemini", targetURL); errStart != nil {
			log.WithError(errStart).Error("failed to start gemini callback forwarder")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start callback server"})
			return
		}
		conf.RedirectURL = fmt.Sprintf("http://localhost:%d/oauth2callback", callbackPort)
	}

	// Build authorization URL after selecting the callback port.
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))

	go func() {
		if isWebUI {
			defer oauthcallback.StopInstance(ctx, callbackPort, forwarder)
		}

		fmt.Println("Waiting for authentication callback...")
		resultMap, errWait := WaitOAuthCallbackFile(h.cfg.AuthDir, "gemini", state, oauthCallbackWaitTimeout)
		if errWait != nil {
			if errors.Is(errWait, errOAuthSessionNotPending) {
				return
			}
			log.Error("oauth flow timed out")
			SetOAuthSessionError(state, "OAuth flow timed out")
			return
		}
		if errStr := resultMap["error"]; errStr != "" {
			log.Errorf("Authentication failed: %s", errStr)
			SetOAuthSessionError(state, "Authentication failed")
			return
		}
		authCode := resultMap["code"]
		if authCode == "" {
			log.Errorf("Authentication failed: code not found")
			SetOAuthSessionError(state, "Authentication failed: code not found")
			return
		}

		// Exchange authorization code for token
		token, err := conf.Exchange(ctx, authCode)
		if err != nil {
			log.Errorf("Failed to exchange token: %v", err)
			SetOAuthSessionError(state, "Failed to exchange token")
			return
		}

		requestedProjectID := strings.TrimSpace(projectID)

		// Create token storage (mirrors internal/auth/gemini createTokenStorage)
		authHTTPClient := conf.Client(ctx, token)
		req, errNewRequest := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil)
		if errNewRequest != nil {
			log.Errorf("Could not get user info: %v", errNewRequest)
			SetOAuthSessionError(state, "Could not get user info")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

		resp, errDo := authHTTPClient.Do(req)
		if errDo != nil {
			log.Errorf("Failed to execute request: %v", errDo)
			SetOAuthSessionError(state, "Failed to execute request")
			return
		}
		defer func() {
			if errClose := resp.Body.Close(); errClose != nil {
				log.Printf("warn: failed to close response body: %v", errClose)
			}
		}()

		bodyBytes, errReadBody := bodyutil.ReadAll(resp.Body, managementOAuthProfileResponseLimit)
		if errReadBody != nil {
			if bodyutil.IsTooLarge(errReadBody) {
				log.Error("Get user info response too large")
				SetOAuthSessionError(state, "Get user info response too large")
				return
			}
			log.Errorf("Could not read user info response: %v", errReadBody)
			SetOAuthSessionError(state, "Could not read user info response")
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Errorf("Get user info request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
			SetOAuthSessionError(state, fmt.Sprintf("Get user info request failed with status %d", resp.StatusCode))
			return
		}

		email := gjson.GetBytes(bodyBytes, "email").String()
		if email != "" {
			fmt.Printf("Authenticated user email: %s\n", email)
		} else {
			fmt.Println("Failed to get user email from token")
		}

		// Marshal/unmarshal oauth2.Token to generic map and enrich fields
		var ifToken map[string]any
		jsonData, _ := json.Marshal(token)
		if errUnmarshal := json.Unmarshal(jsonData, &ifToken); errUnmarshal != nil {
			log.Errorf("Failed to unmarshal token: %v", errUnmarshal)
			SetOAuthSessionError(state, "Failed to unmarshal token")
			return
		}

		ifToken = geminiAuth.EnrichOAuthTokenMap(ifToken, conf)

		ts := geminiAuth.GeminiTokenStorage{
			Token:     ifToken,
			ProjectID: requestedProjectID,
			Email:     email,
			Auto:      requestedProjectID == "",
		}

		// Initialize authenticated HTTP client via GeminiAuth to honor proxy settings
		gemAuth := geminiAuth.NewGeminiAuth()
		gemClient, errGetClient := gemAuth.GetAuthenticatedClient(ctx, &ts, h.cfg, &geminiAuth.WebLoginOptions{
			NoBrowser: true,
		})
		if errGetClient != nil {
			log.Errorf("failed to get authenticated client: %v", errGetClient)
			SetOAuthSessionError(state, "Failed to get authenticated client")
			return
		}
		fmt.Println("Authentication successful.")

		if strings.EqualFold(requestedProjectID, "ALL") {
			ts.Auto = false
			projects, errAll := geminicli.OnboardAllProjects(ctx, gemClient, &ts)
			if errAll != nil {
				log.Errorf("Failed to complete Gemini CLI onboarding: %v", errAll)
				SetOAuthSessionError(state, "Failed to complete Gemini CLI onboarding")
				return
			}
			if errVerify := geminicli.EnsureProjectsEnabled(ctx, gemClient, projects); errVerify != nil {
				log.Errorf("Failed to verify Cloud AI API status: %v", errVerify)
				SetOAuthSessionError(state, "Failed to verify Cloud AI API status")
				return
			}
			ts.ProjectID = strings.Join(projects, ",")
			ts.Checked = true
		} else if strings.EqualFold(requestedProjectID, "GOOGLE_ONE") {
			ts.Auto = false
			if errSetup := geminicli.PerformSetup(ctx, gemClient, &ts, ""); errSetup != nil {
				log.Errorf("Google One auto-discovery failed: %v", errSetup)
				SetOAuthSessionError(state, "Google One auto-discovery failed")
				return
			}
			if strings.TrimSpace(ts.ProjectID) == "" {
				log.Error("Google One auto-discovery returned empty project ID")
				SetOAuthSessionError(state, "Google One auto-discovery returned empty project ID")
				return
			}
			isChecked, errCheck := geminicli.CheckCloudAPIIsEnabled(ctx, gemClient, ts.ProjectID)
			if errCheck != nil {
				log.Errorf("Failed to verify Cloud AI API status: %v", errCheck)
				SetOAuthSessionError(state, "Failed to verify Cloud AI API status")
				return
			}
			ts.Checked = isChecked
			if !isChecked {
				log.Error("Cloud AI API is not enabled for the auto-discovered project")
				SetOAuthSessionError(state, "Cloud AI API not enabled")
				return
			}
		} else {
			if errEnsure := geminicli.EnsureProjectAndOnboard(ctx, gemClient, &ts, requestedProjectID); errEnsure != nil {
				log.Errorf("Failed to complete Gemini CLI onboarding: %v", errEnsure)
				SetOAuthSessionError(state, "Failed to complete Gemini CLI onboarding")
				return
			}

			if strings.TrimSpace(ts.ProjectID) == "" {
				log.Error("Onboarding did not return a project ID")
				SetOAuthSessionError(state, "Failed to resolve project ID")
				return
			}

			isChecked, errCheck := geminicli.CheckCloudAPIIsEnabled(ctx, gemClient, ts.ProjectID)
			if errCheck != nil {
				log.Errorf("Failed to verify Cloud AI API status: %v", errCheck)
				SetOAuthSessionError(state, "Failed to verify Cloud AI API status")
				return
			}
			ts.Checked = isChecked
			if !isChecked {
				log.Error("Cloud AI API is not enabled for the selected project")
				SetOAuthSessionError(state, "Cloud AI API not enabled")
				return
			}
		}

		record := geminicli.RecordFromTokenStorage(&ts)
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save token to file: %v", errSave)
			SetOAuthSessionError(state, "Failed to save token to file")
			return
		}

		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("gemini")
		fmt.Printf("You can now use Gemini CLI services through this CLI; token saved to %s\n", savedPath)
	}()

	c.JSON(200, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestCodexToken(c *gin.Context) {
	ctx := detachedAuthContext(c)

	fmt.Println("Initializing Codex authentication...")

	// Generate PKCE codes
	pkceCodes, err := codex.GeneratePKCECodes()
	if err != nil {
		log.Errorf("Failed to generate PKCE codes: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PKCE codes"})
		return
	}

	// Generate random state parameter
	state, err := misc.GenerateRandomState()
	if err != nil {
		log.Errorf("Failed to generate state parameter: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	// Initialize Codex auth service
	openaiAuth := codex.NewCodexAuth(h.cfg)

	// Generate authorization URL
	authURL, err := openaiAuth.GenerateAuthURL(state, pkceCodes)
	if err != nil {
		log.Errorf("Failed to generate authorization URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}

	RegisterOAuthSession(state, "codex")

	isWebUI := isWebUIRequest(c)
	var forwarder *oauthcallback.Forwarder
	if isWebUI {
		targetURL, errTarget := h.managementCallbackURL("/codex/callback")
		if errTarget != nil {
			log.WithError(errTarget).Warn("failed to compute codex callback target; continuing with manual callback submission")
		} else {
			var errStart error
			if forwarder, errStart = oauthcallback.Start(codexCallbackPort, "codex", targetURL); errStart != nil {
				log.WithError(errStart).Warn("failed to start codex callback forwarder; continuing with manual callback submission")
			}
		}
	}

	go func() {
		if forwarder != nil {
			defer oauthcallback.StopInstance(ctx, codexCallbackPort, forwarder)
		}

		// Wait for callback file
		resultMap, errWait := WaitOAuthCallbackFile(h.cfg.AuthDir, "codex", state, oauthCallbackWaitTimeout)
		if errWait != nil {
			if errors.Is(errWait, errOAuthSessionNotPending) {
				return
			}
			authErr := codex.NewAuthenticationError(codex.ErrCallbackTimeout, errWait)
			log.Error(codex.GetUserFriendlyMessage(authErr))
			SetOAuthSessionError(state, "Timeout waiting for OAuth callback")
			return
		}
		if errStr := resultMap["error"]; errStr != "" {
			oauthErr := codex.NewOAuthError(errStr, "", http.StatusBadRequest)
			log.Error(codex.GetUserFriendlyMessage(oauthErr))
			SetOAuthSessionError(state, "Bad Request")
			return
		}
		if resultMap["state"] != state {
			authErr := codex.NewAuthenticationError(codex.ErrInvalidState, fmt.Errorf("expected %s, got %s", state, resultMap["state"]))
			SetOAuthSessionError(state, "State code error")
			log.Error(codex.GetUserFriendlyMessage(authErr))
			return
		}
		code := resultMap["code"]

		log.Debug("Authorization code received, exchanging for tokens...")
		// Exchange code for tokens using internal auth service
		bundle, errExchange := openaiAuth.ExchangeCodeForTokens(ctx, code, pkceCodes)
		if errExchange != nil {
			authErr := codex.NewAuthenticationError(codex.ErrCodeExchangeFailed, errExchange)
			SetOAuthSessionError(state, "Failed to exchange authorization code for tokens")
			log.Errorf("Failed to exchange authorization code for tokens: %v", authErr)
			return
		}

		// Create token storage and persist
		tokenStorage := openaiAuth.CreateTokenStorage(bundle)
		record := codexprovider.RecordFromTokenStorage(tokenStorage)
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			return
		}
		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		if bundle.APIKey != "" {
			fmt.Println("API key obtained and saved")
		}
		fmt.Println("You can now use Codex services through this CLI")
		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("codex")
	}()

	c.JSON(200, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestAntigravityToken(c *gin.Context) {
	ctx := detachedAuthContext(c)

	fmt.Println("Initializing Antigravity authentication...")

	authSvc := antigravity.NewAntigravityAuth(h.cfg, nil)

	state, errState := misc.GenerateRandomState()
	if errState != nil {
		log.Errorf("Failed to generate state parameter: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	isWebUI := isWebUIRequest(c)
	var forwarder *oauthcallback.Forwarder
	callbackPort := antigravity.CallbackPort
	if isWebUI {
		targetURL, errTarget := h.managementCallbackURL("/antigravity/callback")
		if errTarget != nil {
			log.WithError(errTarget).Error("failed to compute antigravity callback target")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "callback server unavailable"})
			return
		}
		var errStart error
		if forwarder, callbackPort, errStart = oauthcallback.StartOnAvailablePort(antigravity.CallbackPort, "antigravity", targetURL); errStart != nil {
			log.WithError(errStart).Error("failed to start antigravity callback forwarder")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start callback server"})
			return
		}
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/oauth-callback", callbackPort)
	authURL := authSvc.BuildAuthURL(state, redirectURI)
	if strings.TrimSpace(authURL) == "" {
		if isWebUI {
			oauthcallback.StopInstance(ctx, callbackPort, forwarder)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "antigravity oauth client-id not configured"})
		return
	}

	RegisterOAuthSession(state, "antigravity")

	go func() {
		if isWebUI {
			defer oauthcallback.StopInstance(ctx, callbackPort, forwarder)
		}

		payload, errWait := WaitOAuthCallbackFile(h.cfg.AuthDir, "antigravity", state, oauthCallbackWaitTimeout)
		if errWait != nil {
			if errors.Is(errWait, errOAuthSessionNotPending) {
				return
			}
			log.Error("oauth flow timed out")
			SetOAuthSessionError(state, "OAuth flow timed out")
			return
		}
		if errStr := strings.TrimSpace(payload["error"]); errStr != "" {
			log.Errorf("Authentication failed: %s", errStr)
			SetOAuthSessionError(state, "Authentication failed")
			return
		}
		if payloadState := strings.TrimSpace(payload["state"]); payloadState != "" && payloadState != state {
			log.Errorf("Authentication failed: state mismatch")
			SetOAuthSessionError(state, "Authentication failed: state mismatch")
			return
		}
		authCode := strings.TrimSpace(payload["code"])
		if authCode == "" {
			log.Error("Authentication failed: code not found")
			SetOAuthSessionError(state, "Authentication failed: code not found")
			return
		}

		tokenResp, errToken := authSvc.ExchangeCodeForTokens(ctx, authCode, redirectURI)
		if errToken != nil {
			log.Errorf("Failed to exchange token: %v", errToken)
			SetOAuthSessionError(state, "Failed to exchange token")
			return
		}

		accessToken := strings.TrimSpace(tokenResp.AccessToken)
		if accessToken == "" {
			log.Error("antigravity: token exchange returned empty access token")
			SetOAuthSessionError(state, "Failed to exchange token")
			return
		}

		email, errInfo := authSvc.FetchUserInfo(ctx, accessToken)
		if errInfo != nil {
			log.Errorf("Failed to fetch user info: %v", errInfo)
			SetOAuthSessionError(state, "Failed to fetch user info")
			return
		}
		email = strings.TrimSpace(email)
		if email == "" {
			log.Error("antigravity: user info returned empty email")
			SetOAuthSessionError(state, "Failed to fetch user info")
			return
		}

		projectID := ""
		if accessToken != "" {
			fetchedProjectID, errProject := authSvc.FetchProjectID(ctx, accessToken)
			if errProject != nil {
				log.Warnf("antigravity: failed to fetch project ID: %v", errProject)
			} else {
				projectID = fetchedProjectID
				log.Infof("antigravity: obtained project ID %s", projectID)
			}
		}

		record := antigravityprovider.RecordFromTokenResponse(tokenResp, email, projectID, time.Now())
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save token to file: %v", errSave)
			SetOAuthSessionError(state, "Failed to save token to file")
			return
		}

		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("antigravity")
		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		if projectID != "" {
			fmt.Printf("Using GCP project: %s\n", projectID)
		}
		fmt.Println("You can now use Antigravity services through this CLI")
	}()

	c.JSON(200, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestQwenToken(c *gin.Context) {
	ctx := detachedAuthContext(c)

	fmt.Println("Initializing Qwen authentication...")

	state := fmt.Sprintf("gem-%d", time.Now().UnixNano())
	// Initialize Qwen auth service
	qwenAuth := qwen.NewQwenAuth(h.cfg)

	// Generate authorization URL
	deviceFlow, err := qwenAuth.InitiateDeviceFlow(ctx)
	if err != nil {
		log.Errorf("Failed to generate authorization URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}
	authURL := deviceFlow.VerificationURIComplete

	RegisterOAuthSession(state, "qwen")

	go func() {
		fmt.Println("Waiting for authentication...")
		tokenData, errPollForToken := qwenAuth.PollForToken(deviceFlow.DeviceCode, deviceFlow.CodeVerifier)
		if errPollForToken != nil {
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Printf("Authentication failed: %v\n", errPollForToken)
			return
		}

		// Create token storage
		tokenStorage := qwenAuth.CreateTokenStorage(tokenData)

		record := qwenprovider.RecordFromTokenStorage(tokenStorage, time.Now())
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use Qwen services through this CLI")
		CompleteOAuthSession(state)
	}()

	c.JSON(200, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestKimiToken(c *gin.Context) {
	ctx := detachedAuthContext(c)

	fmt.Println("Initializing Kimi authentication...")

	state := fmt.Sprintf("kmi-%d", time.Now().UnixNano())
	// Initialize Kimi auth service
	kimiAuth := kimi.NewKimiAuth(h.cfg)

	// Generate authorization URL
	deviceFlow, errStartDeviceFlow := kimiAuth.StartDeviceFlow(ctx)
	if errStartDeviceFlow != nil {
		log.Errorf("Failed to generate authorization URL: %v", errStartDeviceFlow)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}
	authURL := deviceFlow.VerificationURIComplete
	if authURL == "" {
		authURL = deviceFlow.VerificationURI
	}

	RegisterOAuthSession(state, "kimi")

	go func() {
		fmt.Println("Waiting for authentication...")
		authBundle, errWaitForAuthorization := kimiAuth.WaitForAuthorization(ctx, deviceFlow)
		if errWaitForAuthorization != nil {
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Printf("Authentication failed: %v\n", errWaitForAuthorization)
			return
		}

		// Create token storage
		tokenStorage := kimiAuth.CreateTokenStorage(authBundle)

		record := kimiprovider.RecordFromAuthBundle(tokenStorage, authBundle, time.Now())
		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		fmt.Println("You can now use Kimi services through this CLI")
		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("kimi")
	}()

	c.JSON(200, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestIFlowToken(c *gin.Context) {
	ctx := detachedAuthContext(c)

	fmt.Println("Initializing iFlow authentication...")

	state := fmt.Sprintf("ifl-%d", time.Now().UnixNano())
	authSvc := iflowauth.NewIFlowAuth(h.cfg)
	authURL, redirectURI := authSvc.AuthorizationURL(state, iflowauth.CallbackPort)

	RegisterOAuthSession(state, "iflow")

	isWebUI := isWebUIRequest(c)
	var forwarder *oauthcallback.Forwarder
	if isWebUI {
		targetURL, errTarget := h.managementCallbackURL("/iflow/callback")
		if errTarget != nil {
			log.WithError(errTarget).Error("failed to compute iflow callback target")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "callback server unavailable"})
			return
		}
		var errStart error
		if forwarder, errStart = oauthcallback.Start(iflowauth.CallbackPort, "iflow", targetURL); errStart != nil {
			log.WithError(errStart).Error("failed to start iflow callback forwarder")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to start callback server"})
			return
		}
	}

	go func() {
		if isWebUI {
			defer oauthcallback.StopInstance(ctx, iflowauth.CallbackPort, forwarder)
		}
		fmt.Println("Waiting for authentication...")

		resultMap, errWait := WaitOAuthCallbackFile(h.cfg.AuthDir, "iflow", state, oauthCallbackWaitTimeout)
		if errWait != nil {
			if errors.Is(errWait, errOAuthSessionNotPending) {
				return
			}
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Println("Authentication failed: timeout waiting for callback")
			return
		}

		if errStr := strings.TrimSpace(resultMap["error"]); errStr != "" {
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Printf("Authentication failed: %s\n", errStr)
			return
		}
		if resultState := strings.TrimSpace(resultMap["state"]); resultState != state {
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Println("Authentication failed: state mismatch")
			return
		}

		code := strings.TrimSpace(resultMap["code"])
		if code == "" {
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Println("Authentication failed: code missing")
			return
		}

		tokenData, errExchange := authSvc.ExchangeCodeForTokens(ctx, code, redirectURI)
		if errExchange != nil {
			SetOAuthSessionError(state, "Authentication failed")
			fmt.Printf("Authentication failed: %v\n", errExchange)
			return
		}

		tokenStorage := authSvc.CreateTokenStorage(tokenData)
		record := iflowprovider.RecordFromTokenStorage(tokenStorage, time.Now())

		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			log.Errorf("Failed to save authentication tokens: %v", errSave)
			return
		}

		fmt.Printf("Authentication successful! Token saved to %s\n", savedPath)
		if tokenStorage.APIKey != "" {
			fmt.Println("API key obtained and saved")
		}
		fmt.Println("You can now use iFlow services through this CLI")
		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("iflow")
	}()

	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestIFlowCookieToken(c *gin.Context) {
	ctx := requestAuthContext(c)

	var payload struct {
		Cookie string `json:"cookie"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "cookie is required"})
		return
	}

	cookieValue := strings.TrimSpace(payload.Cookie)

	if cookieValue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "cookie is required"})
		return
	}

	cookieValue, errNormalize := iflowauth.NormalizeCookie(cookieValue)
	if errNormalize != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": errNormalize.Error()})
		return
	}

	// Check for duplicate BXAuth before authentication
	bxAuth := iflowauth.ExtractBXAuth(cookieValue)
	if existingFile, err := iflowauth.CheckDuplicateBXAuth(h.cfg.AuthDir, bxAuth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to check duplicate"})
		return
	} else if existingFile != "" {
		existingFileName := filepath.Base(existingFile)
		c.JSON(http.StatusConflict, gin.H{"status": "error", "error": "duplicate BXAuth found", "existing_file": existingFileName})
		return
	}

	authSvc := iflowauth.NewIFlowAuth(h.cfg)
	tokenData, errAuth := authSvc.AuthenticateWithCookie(ctx, cookieValue)
	if errAuth != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": errAuth.Error()})
		return
	}

	tokenData.Cookie = cookieValue

	tokenStorage := authSvc.CreateCookieTokenStorage(tokenData)
	email := strings.TrimSpace(tokenStorage.Email)
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "failed to extract email from token"})
		return
	}

	record := iflowprovider.CookieRecordFromTokenStorage(tokenStorage, time.Now())

	savedPath, errSave := h.saveTokenRecord(ctx, record)
	if errSave != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to save authentication tokens"})
		return
	}

	fmt.Printf("iFlow cookie authentication successful. Token saved to %s\n", savedPath)
	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"saved_path": savedPath,
		"email":      email,
		"expired":    tokenStorage.Expire,
		"type":       tokenStorage.Type,
	})
}

func (h *Handler) GetAuthStatus(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if err := ValidateOAuthState(state); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid state"})
		return
	}

	_, status, ok := GetOAuthSession(state)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if status == oauthSessionStatusCompleted {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if status != "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "error": status})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "wait"})
}

// PopulateAuthContext extracts request info and adds it to the context
func PopulateAuthContext(ctx context.Context, c *gin.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil || c.Request == nil {
		return ctx
	}
	info := &coreauth.RequestInfo{
		Query:   c.Request.URL.Query(),
		Headers: c.Request.Header,
	}
	return coreauth.WithRequestInfo(ctx, info)
}

func requestAuthContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		if reqCtx := c.Request.Context(); reqCtx != nil {
			return PopulateAuthContext(reqCtx, c)
		}
	}
	return PopulateAuthContext(context.Background(), c)
}

func detachedAuthContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		if reqCtx := c.Request.Context(); reqCtx != nil {
			return PopulateAuthContext(context.WithoutCancel(reqCtx), c)
		}
	}
	return PopulateAuthContext(context.Background(), c)
}
