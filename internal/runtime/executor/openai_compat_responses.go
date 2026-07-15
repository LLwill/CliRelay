package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const openAICompatCapabilityTTL = 24 * time.Hour

type openAICompatCapabilityEntry struct {
	mode      string
	expiresAt time.Time
}

type openAICompatCapabilityCache struct {
	mu      sync.RWMutex
	entries map[string]openAICompatCapabilityEntry
}

func (c *openAICompatCapabilityCache) get(key string) (string, bool) {
	if c == nil || key == "" {
		return "", false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			c.mu.Lock()
			delete(c.entries, key)
			c.mu.Unlock()
		}
		return "", false
	}
	return entry.mode, true
}

func (c *openAICompatCapabilityCache) set(key, mode string) {
	if c == nil || key == "" || (mode != "responses" && mode != "chat-completions") {
		return
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]openAICompatCapabilityEntry)
	}
	c.entries[key] = openAICompatCapabilityEntry{mode: mode, expiresAt: time.Now().Add(openAICompatCapabilityTTL)}
	c.mu.Unlock()
}

// Execute selects the configured OpenAI wire API. In auto mode a native
// Responses request is attempted first and Chat Completions is used only after
// a deterministic endpoint/schema rejection before any response bytes exist.
func (e *OpenAICompatExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	mode, probe, cacheKey := e.resolveOpenAICompatUpstreamMode(auth, req, opts)
	if mode != "responses" {
		return e.executeChatCompletions(ctx, auth, req, opts)
	}

	resp, err := e.executeResponses(ctx, auth, req, opts, probe)
	if err == nil {
		if probe {
			e.capabilities.set(cacheKey, "responses")
		}
		return resp, nil
	}
	if probe && shouldFallbackOpenAICompatResponses(err) {
		e.capabilities.set(cacheKey, "chat-completions")
		logWithRequestID(ctx).Infof("openai compat: upstream Responses unsupported for model %s; falling back to Chat Completions", thinking.ParseSuffix(req.Model).ModelName)
		return e.executeChatCompletions(ctx, auth, req, opts)
	}
	return resp, err
}

func (e *OpenAICompatExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	mode, probe, cacheKey := e.resolveOpenAICompatUpstreamMode(auth, req, opts)
	if mode != "responses" {
		return e.executeChatCompletionsStream(ctx, auth, req, opts)
	}

	result, err := e.executeResponsesStream(ctx, auth, req, opts, probe)
	if err == nil {
		if probe {
			e.capabilities.set(cacheKey, "responses")
		}
		return result, nil
	}
	if probe && shouldFallbackOpenAICompatResponses(err) {
		e.capabilities.set(cacheKey, "chat-completions")
		logWithRequestID(ctx).Infof("openai compat: upstream Responses unsupported for model %s; falling back to Chat Completions", thinking.ParseSuffix(req.Model).ModelName)
		return e.executeChatCompletionsStream(ctx, auth, req, opts)
	}
	return nil, err
}

func (e *OpenAICompatExecutor) resolveOpenAICompatUpstreamMode(auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (mode string, probe bool, cacheKey string) {
	// /responses/compact already has an explicit endpoint and must not be probed.
	if opts.Alt == "responses/compact" {
		return "chat-completions", false, ""
	}

	configured := ""
	if auth != nil && auth.Attributes != nil {
		configured = normalizeOpenAICompatUpstreamMode(auth.Attributes["upstream_api"])
	}
	if configured == "" {
		if compat := e.resolveCompatConfig(auth); compat != nil {
			configured = normalizeOpenAICompatUpstreamMode(compat.UpstreamAPI)
		}
	}
	if configured == "" {
		// Preserve all existing provider behavior unless auto/responses is opted in.
		configured = "chat-completions"
	}
	if configured != "auto" {
		return configured, false, ""
	}

	baseURL, _ := e.resolveCredentials(auth)
	authID := ""
	if auth != nil {
		authID = strings.TrimSpace(auth.ID)
	}
	cacheKey = strings.Join([]string{
		strings.ToLower(strings.TrimSuffix(strings.TrimSpace(baseURL), "/")),
		strings.ToLower(strings.TrimSpace(e.provider)),
		strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(req.Model).ModelName)),
		authID,
	}, "\x00")
	if cached, ok := e.capabilities.get(cacheKey); ok {
		return cached, false, cacheKey
	}
	return "responses", true, cacheKey
}

func normalizeOpenAICompatUpstreamMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto":
		return "auto"
	case "responses", "response":
		return "responses"
	case "chat-completions", "chat_completions", "chat", "completions":
		return "chat-completions"
	default:
		return ""
	}
}

func shouldFallbackOpenAICompatResponses(err error) bool {
	if err == nil {
		return false
	}
	var status statusErr
	if !errors.As(err, &status) {
		return false
	}
	switch status.code {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		message := strings.ToLower(status.msg)
		for _, marker := range []string{
			"unsupported endpoint",
			"endpoint is not supported",
			"unknown endpoint",
			"unknown request url",
			"unknown url",
			"no route",
			"responses api is not supported",
			"does not support responses",
			"unsupported tool type",
			"unknown tool type",
			"invalid tool type",
			"invalid value: 'custom'",
			"invalid value for 'type': 'custom'",
			"namespace",
			"custom tool",
			"tool_choice' is only allowed when 'tools' are specified",
		} {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	return false
}

func openAICompatResponsesTarget(source sdktranslator.Format) sdktranslator.Format {
	if source == sdktranslator.FormatOpenAIResponse {
		return sdktranslator.FormatOpenAIResponse
	}
	// The Codex wire schema is the Responses schema and already has translators
	// for the other public ingress formats.
	return sdktranslator.FormatCodex
}

func (e *OpenAICompatExecutor) executeResponses(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, probe bool) (resp cliproxyexecutor.Response, err error) {
	to := openAICompatResponsesTarget(opts.SourceFormat)
	execCtx := newExecutionContext(ctx, e.Identifier(), e.cfg, auth, req, opts, ExecutionOptions{
		TargetFormat:      to,
		TranslateAsStream: false,
	})
	reporter := execCtx.Reporter()
	suppressFailureTracking := false
	defer func() {
		if !suppressFailureTracking {
			reporter.trackFailure(execCtx.Context, &err)
		}
	}()

	baseURL, apiKey := e.resolveCredentials(auth)
	if baseURL == "" {
		return resp, statusErr{code: http.StatusUnauthorized, msg: "missing provider baseURL"}
	}

	body, originalTranslated := execCtx.TranslateRequestPair(req.Payload)
	body = execCtx.ApplyPayloadConfig(body, originalTranslated)
	body, err = thinking.ApplyThinking(body, req.Model, execCtx.SourceFormat.String(), to.String(), e.Identifier())
	if err != nil {
		return resp, err
	}
	body = e.overrideModel(body, execCtx.BaseModel)
	body, _ = sjson.SetBytes(body, "stream", false)
	body = applyProviderPromptCaching(body, req.Payload, auth, e.provider, execCtx.BaseModel, to, opts)

	url := strings.TrimSuffix(baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(execCtx.Context, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}
	applyOpenAICompatResponsesHeaders(httpReq, auth, apiKey, false)
	recorder := execCtx.Recorder()
	recorder.RecordRequest(url, http.MethodPost, httpReq.Header.Clone(), body)

	httpResp, err := execCtx.HTTPClient(0).Do(httpReq) //nolint:bodyclose // closed below
	if err != nil {
		recorder.RecordResponseError(err)
		reporter.publishFailureWithContent(execCtx.Context, string(req.Payload), err.Error())
		return resp, err
	}
	defer func() {
		if closeErr := httpResp.Body.Close(); closeErr != nil {
			log.Errorf("openai compat responses: close response body error: %v", closeErr)
		}
	}()
	recorder.RecordResponseMetadata(httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data := readUpstreamErrorBody(e.Identifier(), httpResp.Body)
		recorder.AppendResponseChunk(data)
		err = statusErr{code: httpResp.StatusCode, msg: string(data), headers: httpResp.Header.Clone()}
		if probe && shouldFallbackOpenAICompatResponses(err) {
			suppressFailureTracking = true
			return resp, err
		}
		reporter.publishFailureWithContent(execCtx.Context, string(req.Payload), string(data))
		return resp, err
	}

	data, err := readUpstreamResponseBody(e.Identifier(), httpResp.Body)
	if err != nil {
		recorder.RecordResponseError(err)
		return resp, err
	}
	recorder.AppendResponseChunk(data)
	reporter.publishWithContent(execCtx.Context, parseOpenAIUsage(data), string(req.Payload), string(data))
	reporter.ensurePublished(execCtx.Context)
	var param any
	out := sdktranslator.TranslateNonStream(execCtx.Context, to, execCtx.SourceFormat, req.Model, execCtx.OriginalPayload, body, data, &param)
	return cliproxyexecutor.Response{Payload: []byte(out), Headers: httpResp.Header.Clone()}, nil
}

func (e *OpenAICompatExecutor) executeResponsesStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, probe bool) (_ *cliproxyexecutor.StreamResult, err error) {
	to := openAICompatResponsesTarget(opts.SourceFormat)
	execCtx := newExecutionContext(ctx, e.Identifier(), e.cfg, auth, req, opts, ExecutionOptions{
		TargetFormat:      to,
		TranslateAsStream: true,
	})
	reporter := execCtx.Reporter()
	suppressFailureTracking := false
	defer func() {
		if !suppressFailureTracking {
			reporter.trackFailure(execCtx.Context, &err)
		}
	}()

	baseURL, apiKey := e.resolveCredentials(auth)
	if baseURL == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "missing provider baseURL"}
	}
	body, originalTranslated := execCtx.TranslateRequestPair(req.Payload)
	body = execCtx.ApplyPayloadConfig(body, originalTranslated)
	body, err = thinking.ApplyThinking(body, req.Model, execCtx.SourceFormat.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}
	body = e.overrideModel(body, execCtx.BaseModel)
	body, _ = sjson.SetBytes(body, "stream", true)
	body = applyProviderPromptCaching(body, req.Payload, auth, e.provider, execCtx.BaseModel, to, opts)

	url := strings.TrimSuffix(baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(execCtx.Context, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	applyOpenAICompatResponsesHeaders(httpReq, auth, apiKey, true)
	recorder := execCtx.Recorder()
	recorder.RecordRequest(url, http.MethodPost, httpReq.Header.Clone(), body)
	httpResp, err := execCtx.HTTPClient(0).Do(httpReq) //nolint:bodyclose // success body closed by goroutine
	if err != nil {
		recorder.RecordResponseError(err)
		reporter.publishFailureWithContent(execCtx.Context, string(req.Payload), err.Error())
		return nil, err
	}
	recorder.RecordResponseMetadata(httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data := readUpstreamErrorBody(e.Identifier(), httpResp.Body)
		if closeErr := httpResp.Body.Close(); closeErr != nil {
			log.Errorf("openai compat responses: close response body error: %v", closeErr)
		}
		recorder.AppendResponseChunk(data)
		err = statusErr{code: httpResp.StatusCode, msg: string(data), headers: httpResp.Header.Clone()}
		if probe && shouldFallbackOpenAICompatResponses(err) {
			suppressFailureTracking = true
			return nil, err
		}
		reporter.publishFailureWithContent(execCtx.Context, string(req.Payload), string(data))
		return nil, err
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	reporter.setInputContent(string(req.Payload))
	go func() {
		defer close(out)
		defer func() {
			if closeErr := httpResp.Body.Close(); closeErr != nil {
				log.Errorf("openai compat responses: close response body error: %v", closeErr)
			}
		}()
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800)
		var param any
		for scanner.Scan() {
			line := bytes.Clone(scanner.Bytes())
			recorder.AppendResponseChunk(line)
			reporter.appendOutputChunk(line)
			if detail, ok := parseOpenAIResponsesStreamUsage(line); ok {
				reporter.publish(execCtx.Context, detail)
			}
			if payload := responsesSSEData(line); len(payload) > 0 && gjson.ValidBytes(payload) {
				switch gjson.GetBytes(payload, "type").String() {
				case "response.failed", "error":
					streamErr := codexResponsesFailedStatusErr(payload)
					recorder.RecordResponseError(streamErr)
					reporter.publishFailure(execCtx.Context)
					out <- cliproxyexecutor.StreamChunk{Err: streamErr}
					return
				}
			}
			for _, chunk := range sdktranslator.TranslateStream(execCtx.Context, to, execCtx.SourceFormat, req.Model, execCtx.OriginalPayload, body, line, &param) {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunk)}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			recorder.RecordResponseError(scanErr)
			reporter.publishFailure(execCtx.Context)
			out <- cliproxyexecutor.StreamChunk{Err: scanErr}
			return
		}
		reporter.ensurePublished(execCtx.Context)
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}

func applyOpenAICompatResponsesHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("User-Agent", "cli-proxy-openai-compat")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
}
