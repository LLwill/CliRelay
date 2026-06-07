// Package executor provides runtime execution capabilities for various AI service providers.
// This file implements the Vertex AI Gemini executor that talks to Google Vertex AI
// endpoints using service account credentials or API keys.
package executor

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/sjson"
)

const (
	// vertexAPIVersion aligns with current public Vertex Generative AI API.
	vertexAPIVersion = "v1"
)

// GeminiVertexExecutor sends requests to Vertex AI Gemini endpoints using service account credentials.
type GeminiVertexExecutor struct {
	cfg *config.Config
}

// NewGeminiVertexExecutor creates a new Vertex AI Gemini executor instance.
//
// Parameters:
//   - cfg: The application configuration
//
// Returns:
//   - *GeminiVertexExecutor: A new Vertex AI Gemini executor instance
func NewGeminiVertexExecutor(cfg *config.Config) *GeminiVertexExecutor {
	return &GeminiVertexExecutor{cfg: cfg}
}

// Identifier returns the executor identifier.
func (e *GeminiVertexExecutor) Identifier() string { return "vertex" }

// PrepareRequest injects Vertex credentials into the outgoing HTTP request.
func (e *GeminiVertexExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	apiKey, _ := vertexAPICreds(auth)
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("x-goog-api-key", apiKey)
		req.Header.Del("Authorization")
		return nil
	}
	_, _, saJSON, errCreds := vertexCreds(auth)
	if errCreds != nil {
		return errCreds
	}
	token, errToken := vertexAccessToken(req.Context(), e.cfg, auth, saJSON)
	if errToken != nil {
		return errToken
	}
	if strings.TrimSpace(token) == "" {
		return statusErr{code: http.StatusUnauthorized, msg: "missing access token"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Del("x-goog-api-key")
	return nil
}

// HttpRequest injects Vertex credentials into the request and executes it.
func (e *GeminiVertexExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("vertex executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute performs a non-streaming request to the Vertex AI API.
func (e *GeminiVertexExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	if opts.Alt == "responses/compact" {
		return resp, statusErr{code: http.StatusNotImplemented, msg: "/responses/compact not supported"}
	}
	execCtx := e.newVertexExecutionContext(ctx, auth, req, opts, false)
	// Try API key authentication first
	apiKey, baseURL := vertexAPICreds(auth)

	// If no API key found, fall back to service account authentication
	if apiKey == "" {
		projectID, location, saJSON, errCreds := vertexCreds(auth)
		if errCreds != nil {
			return resp, errCreds
		}
		return e.executeWithServiceAccount(execCtx, auth, projectID, location, saJSON)
	}

	// Use API key authentication
	return e.executeWithAPIKey(execCtx, auth, apiKey, baseURL)
}

// ExecuteStream performs a streaming request to the Vertex AI API.
func (e *GeminiVertexExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if opts.Alt == "responses/compact" {
		return nil, statusErr{code: http.StatusNotImplemented, msg: "/responses/compact not supported"}
	}
	execCtx := e.newVertexExecutionContext(ctx, auth, req, opts, true)
	// Try API key authentication first
	apiKey, baseURL := vertexAPICreds(auth)

	// If no API key found, fall back to service account authentication
	if apiKey == "" {
		projectID, location, saJSON, errCreds := vertexCreds(auth)
		if errCreds != nil {
			return nil, errCreds
		}
		return e.executeStreamWithServiceAccount(execCtx, auth, projectID, location, saJSON)
	}

	// Use API key authentication
	return e.executeStreamWithAPIKey(execCtx, auth, apiKey, baseURL)
}

// CountTokens counts tokens for the given request using the Vertex AI API.
func (e *GeminiVertexExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	execCtx := e.newVertexExecutionContext(ctx, auth, req, opts, false)
	// Try API key authentication first
	apiKey, baseURL := vertexAPICreds(auth)

	// If no API key found, fall back to service account authentication
	if apiKey == "" {
		projectID, location, saJSON, errCreds := vertexCreds(auth)
		if errCreds != nil {
			return cliproxyexecutor.Response{}, errCreds
		}
		return e.countTokensWithServiceAccount(execCtx, auth, projectID, location, saJSON)
	}

	// Use API key authentication
	return e.countTokensWithAPIKey(execCtx, auth, apiKey, baseURL)
}

// Refresh refreshes the authentication credentials (no-op for Vertex).
func (e *GeminiVertexExecutor) Refresh(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

// executeWithServiceAccount handles authentication using service account credentials.
// This method contains the original service account authentication logic.
func (e *GeminiVertexExecutor) executeWithServiceAccount(execCtx *ExecutionContext, auth *cliproxyauth.Auth, projectID, location string, saJSON []byte) (cliproxyexecutor.Response, error) {
	body, err := e.buildVertexPayload(execCtx)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	action := getVertexAction(execCtx.BaseModel, false)
	if execCtx.Request.Metadata != nil {
		if a, _ := execCtx.Request.Metadata["action"].(string); a == "countTokens" {
			action = "countTokens"
		}
	}
	baseURL := vertexBaseURL(location)
	url := fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, projectID, location, execCtx.BaseModel, action)
	if execCtx.Options.Alt != "" && action != "countTokens" {
		url = url + fmt.Sprintf("?$alt=%s", execCtx.Options.Alt)
	}
	body, _ = sjson.DeleteBytes(body, "session_id")
	return e.executeVertexNonStream(execCtx, auth, body, url, "", saJSON)
}

// executeWithAPIKey handles authentication using API key credentials.
func (e *GeminiVertexExecutor) executeWithAPIKey(execCtx *ExecutionContext, auth *cliproxyauth.Auth, apiKey, baseURL string) (cliproxyexecutor.Response, error) {
	body, err := e.buildVertexPayload(execCtx)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	action := getVertexAction(execCtx.BaseModel, false)
	if execCtx.Request.Metadata != nil {
		if a, _ := execCtx.Request.Metadata["action"].(string); a == "countTokens" {
			action = "countTokens"
		}
	}

	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	url := fmt.Sprintf("%s/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, execCtx.BaseModel, action)
	if execCtx.Options.Alt != "" && action != "countTokens" {
		url = url + fmt.Sprintf("?$alt=%s", execCtx.Options.Alt)
	}
	body, _ = sjson.DeleteBytes(body, "session_id")
	return e.executeVertexNonStream(execCtx, auth, body, url, apiKey, nil)
}

// executeStreamWithServiceAccount handles streaming authentication using service account credentials.
func (e *GeminiVertexExecutor) executeStreamWithServiceAccount(execCtx *ExecutionContext, auth *cliproxyauth.Auth, projectID, location string, saJSON []byte) (*cliproxyexecutor.StreamResult, error) {
	body, err := e.buildVertexPayload(execCtx)
	if err != nil {
		return nil, err
	}

	action := getVertexAction(execCtx.BaseModel, true)
	baseURL := vertexBaseURL(location)
	url := fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, projectID, location, execCtx.BaseModel, action)
	if !isImagenModel(execCtx.BaseModel) {
		if execCtx.Options.Alt == "" {
			url = url + "?alt=sse"
		} else {
			url = url + fmt.Sprintf("?$alt=%s", execCtx.Options.Alt)
		}
	}
	body, _ = sjson.DeleteBytes(body, "session_id")
	return e.executeVertexStream(execCtx, auth, body, url, "", saJSON)
}

// executeStreamWithAPIKey handles streaming authentication using API key credentials.
func (e *GeminiVertexExecutor) executeStreamWithAPIKey(execCtx *ExecutionContext, auth *cliproxyauth.Auth, apiKey, baseURL string) (*cliproxyexecutor.StreamResult, error) {
	body, err := e.buildVertexPayload(execCtx)
	if err != nil {
		return nil, err
	}

	action := getVertexAction(execCtx.BaseModel, true)
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	url := fmt.Sprintf("%s/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, execCtx.BaseModel, action)
	if !isImagenModel(execCtx.BaseModel) {
		if execCtx.Options.Alt == "" {
			url = url + "?alt=sse"
		} else {
			url = url + fmt.Sprintf("?$alt=%s", execCtx.Options.Alt)
		}
	}
	body, _ = sjson.DeleteBytes(body, "session_id")
	return e.executeVertexStream(execCtx, auth, body, url, apiKey, nil)
}
