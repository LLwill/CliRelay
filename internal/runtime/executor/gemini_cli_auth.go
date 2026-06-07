package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	geminiAuth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/gemini"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/geminicli"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var geminiOAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

func prepareGeminiCLITokenSource(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (oauth2.TokenSource, map[string]any, error) {
	metadata := geminiOAuthMetadata(auth)
	if auth == nil || metadata == nil {
		return nil, nil, fmt.Errorf("gemini-cli auth metadata missing")
	}

	var base map[string]any
	if tokenRaw, ok := metadata["token"].(map[string]any); ok && tokenRaw != nil {
		base = cloneMap(tokenRaw)
	} else {
		base = make(map[string]any)
	}

	var token oauth2.Token
	if len(base) > 0 {
		if raw, err := json.Marshal(base); err == nil {
			_ = json.Unmarshal(raw, &token)
		}
	}

	if token.AccessToken == "" {
		token.AccessToken = stringValue(metadata, "access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = stringValue(metadata, "refresh_token")
	}
	if token.TokenType == "" {
		token.TokenType = stringValue(metadata, "token_type")
	}
	if token.Expiry.IsZero() {
		if expiry := stringValue(metadata, "expiry"); expiry != "" {
			if ts, err := time.Parse(time.RFC3339, expiry); err == nil {
				token.Expiry = ts
			}
		}
	}

	conf := &oauth2.Config{
		ClientID:     "",
		ClientSecret: "",
		Scopes:       geminiOAuthScopes,
		Endpoint:     google.Endpoint,
	}

	if cfg == nil {
		cfg = &config.Config{}
	}
	clientID, clientSecret := resolveGeminiCLITokenOAuthClient(cfg, base, metadata)
	if strings.TrimSpace(clientID) == "" {
		return nil, nil, fmt.Errorf("gemini-cli oauth client-id missing (set config oauth-clients.gemini.client-id or env %s)", config.EnvGeminiOAuthClientID)
	}
	conf.ClientID = clientID
	conf.ClientSecret = clientSecret

	ctxToken := ctx
	if httpClient := newProxyAwareHTTPClient(ctx, cfg, auth, 0); httpClient != nil {
		ctxToken = context.WithValue(ctxToken, oauth2.HTTPClient, httpClient)
	}

	src := conf.TokenSource(ctxToken, &token)
	currentToken, err := src.Token()
	if err != nil {
		return nil, nil, err
	}
	updateGeminiCLITokenMetadata(auth, base, currentToken)
	return oauth2.ReuseTokenSource(currentToken, src), base, nil
}

func resolveGeminiCLITokenOAuthClient(cfg *config.Config, tokenData, metadata map[string]any) (string, string) {
	return geminiAuth.ResolveOAuthClientCredentials(cfg, tokenData, metadata)
}

func updateGeminiCLITokenMetadata(auth *cliproxyauth.Auth, base map[string]any, tok *oauth2.Token) {
	if auth == nil || tok == nil {
		return
	}
	merged := buildGeminiTokenMap(base, tok)
	fields := buildGeminiTokenFields(tok, merged)
	shared := geminicli.ResolveSharedCredential(auth.Runtime)
	if shared != nil {
		snapshot := shared.MergeMetadata(fields)
		if !geminicli.IsVirtual(auth.Runtime) {
			auth.Metadata = snapshot
		}
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	for k, v := range fields {
		auth.Metadata[k] = v
	}
}

func buildGeminiTokenMap(base map[string]any, tok *oauth2.Token) map[string]any {
	merged := cloneMap(base)
	if merged == nil {
		merged = make(map[string]any)
	}
	if raw, err := json.Marshal(tok); err == nil {
		var tokenMap map[string]any
		if err = json.Unmarshal(raw, &tokenMap); err == nil {
			for k, v := range tokenMap {
				merged[k] = v
			}
		}
	}
	return merged
}

func buildGeminiTokenFields(tok *oauth2.Token, merged map[string]any) map[string]any {
	fields := make(map[string]any, 5)
	if tok.AccessToken != "" {
		fields["access_token"] = tok.AccessToken
	}
	if tok.TokenType != "" {
		fields["token_type"] = tok.TokenType
	}
	if tok.RefreshToken != "" {
		fields["refresh_token"] = tok.RefreshToken
	}
	if !tok.Expiry.IsZero() {
		fields["expiry"] = tok.Expiry.Format(time.RFC3339)
	}
	if len(merged) > 0 {
		fields["token"] = cloneMap(merged)
	}
	return fields
}

func resolveGeminiProjectID(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if runtime := auth.Runtime; runtime != nil {
		if virtual, ok := runtime.(*geminicli.VirtualCredential); ok && virtual != nil {
			return strings.TrimSpace(virtual.ProjectID)
		}
	}
	return strings.TrimSpace(stringValue(auth.Metadata, "project_id"))
}

func geminiOAuthMetadata(auth *cliproxyauth.Auth) map[string]any {
	if auth == nil {
		return nil
	}
	if shared := geminicli.ResolveSharedCredential(auth.Runtime); shared != nil {
		if snapshot := shared.MetadataSnapshot(); len(snapshot) > 0 {
			return snapshot
		}
	}
	return auth.Metadata
}

func newHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client {
	return newProxyAwareHTTPClient(ctx, cfg, auth, timeout)
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		switch typed := v.(type) {
		case string:
			return typed
		case fmt.Stringer:
			return typed.String()
		}
	}
	return ""
}
