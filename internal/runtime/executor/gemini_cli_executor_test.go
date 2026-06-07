package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestPrepareGeminiCLITokenSourceUsesStoredOAuthClientCredentials(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata: map[string]any{
			"token": map[string]any{
				"access_token":  "expired-access-token",
				"refresh_token": "stored-refresh-token",
				"token_type":    "Bearer",
				"expiry":        time.Now().Add(-time.Hour).Format(time.RFC3339),
				"client_id":     "stored-client-id",
				"client_secret": "stored-client-secret",
			},
		},
	}
	cfg := &config.Config{
		OAuthClients: config.OAuthClients{
			Gemini: config.OAuthClient{
				ClientID:     "configured-client-id",
				ClientSecret: "configured-client-secret",
			},
		},
	}
	ctx := context.WithValue(context.Background(), util.ContextKeyRoundTripper, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.ParseForm(); err != nil {
			t.Fatalf("ParseForm returned error: %v", err)
		}
		if got := req.Form.Get("client_id"); got != "stored-client-id" {
			t.Fatalf("refresh client_id = %q, want stored-client-id", got)
		}
		if got := req.Form.Get("client_secret"); got != "stored-client-secret" {
			t.Fatalf("refresh client_secret = %q, want stored-client-secret", got)
		}
		if got := req.Form.Get("refresh_token"); got != "stored-refresh-token" {
			t.Fatalf("refresh_token = %q, want stored-refresh-token", got)
		}

		body, _ := json.Marshal(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser{Reader: strings.NewReader(string(body))},
			Request:    req,
		}, nil
	}))

	_, _, err := prepareGeminiCLITokenSource(ctx, cfg, auth)
	if err != nil {
		t.Fatalf("prepareGeminiCLITokenSource returned error: %v", err)
	}
}

func TestResolveGeminiCLITokenOAuthClientUsesConfiguredSecretForMatchingStoredClientID(t *testing.T) {
	cfg := &config.Config{
		OAuthClients: config.OAuthClients{
			Gemini: config.OAuthClient{
				ClientID:     "custom-client-id",
				ClientSecret: "custom-client-secret",
			},
		},
	}

	clientID, clientSecret := resolveGeminiCLITokenOAuthClient(cfg, map[string]any{
		"client_id": "custom-client-id",
	}, nil)

	if clientID != "custom-client-id" {
		t.Fatalf("clientID = %q, want custom-client-id", clientID)
	}
	if clientSecret != "custom-client-secret" {
		t.Fatalf("clientSecret = %q, want custom-client-secret", clientSecret)
	}
}

func TestParseRetryDelayFromRetryInfo(t *testing.T) {
	body := []byte(`{
		"error": {
			"details": [
				{
					"@type": "type.googleapis.com/google.rpc.RetryInfo",
					"retryDelay": "2.5s"
				}
			]
		}
	}`)

	delay, err := parseRetryDelay(body)
	if err != nil {
		t.Fatalf("parseRetryDelay returned error: %v", err)
	}
	if delay == nil || *delay != 2500*time.Millisecond {
		t.Fatalf("delay = %v, want 2.5s", delay)
	}
}

func TestParseGeminiCLIQuotaProbeGroupsBlockedModels(t *testing.T) {
	auth := &cliproxyauth.Auth{
		ModelStates: map[string]*cliproxyauth.ModelState{
			"gemini-2.5-pro-preview-06-05": {
				Quota: cliproxyauth.QuotaState{Exceeded: true},
			},
			"gemini-2.5-flash": {
				Quota: cliproxyauth.QuotaState{Exceeded: true},
			},
		},
	}
	body := []byte(`{
		"buckets": [
			{
				"modelId": "projects/test/locations/us/models/gemini-2.5-pro",
				"remainingFraction": 0.3
			},
			{
				"modelId": "gemini-2.5-flash",
				"resetTime": "2026-06-07T12:00:00Z"
			}
		]
	}`)

	result := parseGeminiCLIQuotaProbe(auth, body)
	if result == nil {
		t.Fatal("parseGeminiCLIQuotaProbe returned nil")
	}

	proResult, ok := result.Models["gemini-2.5-pro-preview-06-05"]
	if !ok || !proResult.Recovered {
		t.Fatalf("pro model result = %+v, want recovered", proResult)
	}
	flashResult, ok := result.Models["gemini-2.5-flash"]
	if !ok {
		t.Fatal("missing gemini-2.5-flash result")
	}
	if flashResult.Recovered {
		t.Fatalf("flash result = %+v, want unrecovered", flashResult)
	}
	wantReset := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	if !flashResult.NextRecoverAt.Equal(wantReset) {
		t.Fatalf("flash NextRecoverAt = %v, want %v", flashResult.NextRecoverAt, wantReset)
	}
}

type ioNopCloser struct {
	*strings.Reader
}

func (c ioNopCloser) Close() error {
	return nil
}
