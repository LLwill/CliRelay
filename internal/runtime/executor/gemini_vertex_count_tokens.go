package executor

import (
	"fmt"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// countTokensWithServiceAccount counts tokens using service account credentials.
func (e *GeminiVertexExecutor) countTokensWithServiceAccount(execCtx *ExecutionContext, auth *cliproxyauth.Auth, projectID, location string, saJSON []byte) (cliproxyexecutor.Response, error) {
	body, err := e.buildVertexCountTokensPayload(execCtx)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	baseURL := vertexBaseURL(location)
	url := fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, projectID, location, execCtx.BaseModel, "countTokens")
	return e.executeVertexCountTokens(execCtx, auth, body, url, "", saJSON)
}

// countTokensWithAPIKey handles token counting using API key credentials.
func (e *GeminiVertexExecutor) countTokensWithAPIKey(execCtx *ExecutionContext, auth *cliproxyauth.Auth, apiKey, baseURL string) (cliproxyexecutor.Response, error) {
	body, err := e.buildVertexCountTokensPayload(execCtx)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	url := fmt.Sprintf("%s/%s/publishers/google/models/%s:%s", baseURL, vertexAPIVersion, execCtx.BaseModel, "countTokens")
	return e.executeVertexCountTokens(execCtx, auth, body, url, apiKey, nil)
}
