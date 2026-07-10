package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const usageReporterOutputMemoryLimit = 256 * 1024

type usageReporter struct {
	provider            string
	model               string
	upstreamModel       string
	visionFallbackModel string
	authID              string
	authIndex           string
	authSubjectID       string
	apiKey              string
	apiKeyID            string
	apiKeyName          string
	source              string
	channelName         string
	requestedAt         time.Time
	once                sync.Once
	contentMu           sync.Mutex

	// Content captured for log detail viewer
	inputContent  string
	inputPath     string
	outputContent string
	outputBuilder strings.Builder
	outputFile    *os.File
	outputPath    string
}

func newUsageReporter(ctx context.Context, provider, model, upstreamModel string, auth *cliproxyauth.Auth) *usageReporter {
	apiKey := apiKeyFromContext(ctx)
	reporter := &usageReporter{
		provider:      provider,
		model:         model,
		upstreamModel: upstreamModel,
		requestedAt:   time.Now(),
		apiKey:        apiKey,
		source:        resolveUsageSource(auth, apiKey),
	}
	if identity := internalusage.ResolveAPIKeyIdentity(apiKey); identity != nil {
		reporter.apiKeyID = identity.ID
		reporter.apiKeyName = identity.Name
	}
	if auth != nil {
		reporter.authID = auth.ID
		reporter.authIndex = auth.EnsureIndex()
		if identity := internalusage.ResolveAuthSubjectIdentity(auth); identity != nil {
			reporter.authSubjectID = identity.ID
		}
		reporter.channelName = strings.TrimSpace(auth.ChannelName())
	}
	return reporter
}

func (r *usageReporter) publish(ctx context.Context, detail coreusage.Detail) {
	r.publishWithOutcome(ctx, detail, false)
}

func (r *usageReporter) publishWithContent(ctx context.Context, detail coreusage.Detail, inputContent, outputContent string) {
	r.contentMu.Lock()
	r.setInputContentLocked(inputContent)
	r.outputContent = outputContent
	r.contentMu.Unlock()
	r.publishWithOutcome(ctx, detail, false)
}

// setModel overrides the reporter's model. It is intentionally NOT called from
// the OpenAI-compat executor: the upstream response's "model" field echoes a
// provider-internal path (e.g. accounts/fireworks/models/glm-5p2) that must not
// replace the clean request-time model used for logging and cost calculation.
// It is retained for explicit callers that have a verified-clean model string.
func (r *usageReporter) setModel(model string) {
	if r == nil {
		return
	}
	if model = strings.TrimSpace(model); model != "" {
		r.model = model
	}
}

func (r *usageReporter) setUpstreamModel(model string) {
	if r == nil {
		return
	}
	if model = strings.TrimSpace(model); model != "" {
		r.upstreamModel = model
	}
}

func (r *usageReporter) setVisionFallbackModel(model string) {
	if r == nil {
		return
	}
	if model = strings.TrimSpace(model); model != "" {
		r.visionFallbackModel = model
	}
}

// setInputContent stores the request payload for inclusion in usage records.
// Call before starting the streaming goroutine.
func (r *usageReporter) setInputContent(content string) {
	if r == nil {
		return
	}
	r.contentMu.Lock()
	defer r.contentMu.Unlock()
	r.setInputContentLocked(content)
}

// appendOutputChunk accumulates a streaming response line for inclusion in usage records.
func (r *usageReporter) appendOutputChunk(chunk []byte) {
	if r == nil || len(chunk) == 0 {
		return
	}
	r.contentMu.Lock()
	defer r.contentMu.Unlock()

	if r.outputFile == nil && r.outputBuilder.Len()+len(chunk)+1 > usageReporterOutputMemoryLimit {
		if err := r.spillOutputBuilderToFileLocked(); err != nil {
			log.Errorf("usage: spill streaming output to temp file: %v", err)
		}
	}

	if r.outputFile != nil {
		if _, err := r.outputFile.Write(chunk); err != nil {
			log.Errorf("usage: write streaming output chunk to temp file: %v", err)
			r.outputBuilder.Write(chunk)
			r.outputBuilder.WriteByte('\n')
			return
		}
		if _, err := r.outputFile.Write([]byte{'\n'}); err != nil {
			log.Errorf("usage: write streaming output newline to temp file: %v", err)
		}
		return
	}

	r.outputBuilder.Write(chunk)
	r.outputBuilder.WriteByte('\n')
}

func (r *usageReporter) publishFailure(ctx context.Context) {
	r.publishWithOutcome(ctx, coreusage.Detail{}, true)
}

// publishFailureWithContent records a failed request together with the
// request payload and the upstream error response body so that the error
// is visible in the management UI error-detail modal.
func (r *usageReporter) publishFailureWithContent(ctx context.Context, inputContent, outputContent string) {
	if r == nil {
		return
	}
	if shouldSuppressUsageFailure(nil, outputContent) {
		return
	}
	r.contentMu.Lock()
	r.setInputContentLocked(inputContent)
	r.outputContent = outputContent
	r.contentMu.Unlock()
	r.publishWithOutcome(ctx, coreusage.Detail{}, true)
}

func (r *usageReporter) trackFailure(ctx context.Context, errPtr *error) {
	if r == nil || errPtr == nil {
		return
	}
	if *errPtr != nil {
		if shouldSuppressUsageFailure(*errPtr, "") {
			return
		}
		r.contentMu.Lock()
		if r.outputContent == "" && r.outputBuilder.Len() == 0 && r.outputFile == nil {
			r.outputContent = structuredUpstreamErrorJSON(*errPtr)
		}
		r.contentMu.Unlock()
		r.publishFailure(ctx)
	}
}

func shouldSuppressUsageFailure(err error, outputContent string) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(outputContent), "context canceled")
}

type upstreamBodyError interface {
	UpstreamErrorBody() []byte
}

func structuredUpstreamErrorJSON(err error) string {
	msg := ""
	if err != nil {
		msg = strings.TrimSpace(err.Error())
	}
	if msg == "" {
		msg = "Upstream request failed."
	}
	errorBody := map[string]any{
		"message": msg,
		"type":    "upstream_error",
	}
	if upstreamErr, ok := err.(upstreamBodyError); ok {
		upstreamBody := strings.TrimSpace(string(upstreamErr.UpstreamErrorBody()))
		if upstreamBody != "" {
			errorBody["upstream"] = parseStructuredUpstreamBody(upstreamBody)
		}
	}
	body := map[string]any{
		"error": errorBody,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return `{"error":{"message":"Upstream request failed.","type":"upstream_error"}}`
	}
	return string(data)
}

func parseStructuredUpstreamBody(body string) any {
	var decoded any
	if err := json.Unmarshal([]byte(body), &decoded); err == nil {
		return decoded
	}
	return body
}

func (r *usageReporter) publishWithOutcome(ctx context.Context, detail coreusage.Detail, failed bool) {
	if r == nil {
		return
	}
	if detail.TotalTokens == 0 {
		total := detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens
		if total > 0 {
			detail.TotalTokens = total
		}
	}
	if detail.InputTokens == 0 && detail.OutputTokens == 0 && detail.ReasoningTokens == 0 && detail.CachedTokens == 0 && detail.TotalTokens == 0 && !failed {
		return
	}
	r.once.Do(func() {
		inputContent, outputContent, inputPath, outputPath := r.finalizeContent()
		detailContent, detailPath := deferLargeContent("cliproxy-usage-detail-*", buildRequestDetailContent(ctx))
		latencyMs := time.Since(r.requestedAt).Milliseconds()
		if latencyMs < 0 {
			latencyMs = 0
		}
		firstTokenMs := firstTokenLatencyMsFromContext(ctx, r.requestedAt)
		apiIdentifier, requestID, responseStatus := usageRequestMetadata(ctx)
		coreusage.PublishRecord(ctx, coreusage.Record{
			Provider:            r.provider,
			Model:               r.model,
			UpstreamModel:       r.upstreamModel,
			VisionFallbackModel: r.visionFallbackModel,
			Source:              r.source,
			ChannelName:         r.channelName,
			APIKey:              r.apiKey,
			APIKeyID:            r.apiKeyID,
			APIKeyName:          r.apiKeyName,
			AuthID:              r.authID,
			AuthIndex:           r.authIndex,
			AuthSubjectID:       r.authSubjectID,
			RequestedAt:         r.requestedAt,
			LatencyMs:           latencyMs,
			FirstTokenMs:        firstTokenMs,
			Failed:              failed,
			APIIdentifier:       apiIdentifier,
			RequestID:           requestID,
			ResponseStatus:      responseStatus,
			Detail:              detail,
			InputContent:        inputContent,
			OutputContent:       outputContent,
			DetailContent:       detailContent,
			InputContentPath:    inputPath,
			OutputContentPath:   outputPath,
			DetailContentPath:   detailPath,
		})
	})
}

// ensurePublished guarantees that a usage record is emitted exactly once.
// It is safe to call multiple times; only the first call wins due to once.Do.
// This is used to ensure request counting even when upstream responses do not
// include any usage fields (tokens), especially for streaming paths.
func (r *usageReporter) ensurePublished(ctx context.Context) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		inputContent, outputContent, inputPath, outputPath := r.finalizeContent()
		detailContent, detailPath := deferLargeContent("cliproxy-usage-detail-*", buildRequestDetailContent(ctx))
		latencyMs := time.Since(r.requestedAt).Milliseconds()
		if latencyMs < 0 {
			latencyMs = 0
		}
		firstTokenMs := firstTokenLatencyMsFromContext(ctx, r.requestedAt)
		apiIdentifier, requestID, responseStatus := usageRequestMetadata(ctx)
		coreusage.PublishRecord(ctx, coreusage.Record{
			Provider:            r.provider,
			Model:               r.model,
			UpstreamModel:       r.upstreamModel,
			VisionFallbackModel: r.visionFallbackModel,
			Source:              r.source,
			ChannelName:         r.channelName,
			APIKey:              r.apiKey,
			APIKeyID:            r.apiKeyID,
			APIKeyName:          r.apiKeyName,
			AuthID:              r.authID,
			AuthIndex:           r.authIndex,
			AuthSubjectID:       r.authSubjectID,
			RequestedAt:         r.requestedAt,
			LatencyMs:           latencyMs,
			FirstTokenMs:        firstTokenMs,
			Failed:              false,
			APIIdentifier:       apiIdentifier,
			RequestID:           requestID,
			ResponseStatus:      responseStatus,
			Detail:              coreusage.Detail{},
			InputContent:        inputContent,
			OutputContent:       outputContent,
			DetailContent:       detailContent,
			InputContentPath:    inputPath,
			OutputContentPath:   outputPath,
			DetailContentPath:   detailPath,
		})
	})
}

func (r *usageReporter) spillOutputBuilderToFileLocked() error {
	if r.outputFile != nil {
		return nil
	}
	file, err := os.CreateTemp("", "cliproxy-usage-output-*")
	if err != nil {
		return err
	}
	if r.outputBuilder.Len() > 0 {
		if _, err := file.WriteString(r.outputBuilder.String()); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			return err
		}
		r.outputBuilder.Reset()
	}
	r.outputFile = file
	r.outputPath = file.Name()
	return nil
}

func (r *usageReporter) finalizeContent() (string, string, string, string) {
	if r == nil {
		return "", "", "", ""
	}
	r.contentMu.Lock()
	defer r.contentMu.Unlock()

	output := r.outputContent
	if r.outputBuilder.Len() > 0 {
		output += r.outputBuilder.String()
		r.outputBuilder.Reset()
	}
	if r.outputFile != nil {
		if err := r.outputFile.Close(); err != nil {
			log.Errorf("usage: close streaming output temp file: %v", err)
		}
		r.outputFile = nil
	}
	if r.outputPath == "" {
		output, r.outputPath = deferLargeContent("cliproxy-usage-output-*", output)
	} else if output != "" {
		// Preserve the legacy ordering (direct content before streamed chunks)
		// without loading the existing spool back into memory.
		combined, err := os.CreateTemp("", "cliproxy-usage-output-combined-*")
		if err != nil {
			log.Errorf("usage: create combined output spool: %v", err)
		} else {
			oldPath := r.outputPath
			oldFile, errOpen := os.Open(oldPath)
			if errOpen != nil {
				_ = combined.Close()
				_ = os.Remove(combined.Name())
				log.Errorf("usage: open existing output spool: %v", errOpen)
			} else {
				_, errWrite := combined.WriteString(output)
				if errWrite == nil {
					_, errWrite = io.Copy(combined, oldFile)
				}
				_ = oldFile.Close()
				if errClose := combined.Close(); errWrite == nil {
					errWrite = errClose
				}
				if errWrite != nil {
					_ = os.Remove(combined.Name())
					log.Errorf("usage: combine output spool: %v", errWrite)
				} else {
					_ = os.Remove(oldPath)
					r.outputPath = combined.Name()
					output = ""
				}
			}
		}
	}

	input := r.inputContent
	inputPath := r.inputPath
	r.inputContent = ""
	r.inputPath = ""
	r.outputContent = ""
	outputPath := r.outputPath
	r.outputPath = ""
	return input, output, inputPath, outputPath
}

func (r *usageReporter) setInputContentLocked(content string) {
	if r.inputPath != "" {
		_ = os.Remove(r.inputPath)
		r.inputPath = ""
	}
	r.inputContent, r.inputPath = deferLargeContent("cliproxy-usage-input-*", content)
}

func deferLargeContent(pattern, content string) (string, string) {
	if len(content) <= usageReporterOutputMemoryLimit {
		return content, ""
	}
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		log.Errorf("usage: create deferred content file: %v", err)
		return content, ""
	}
	path := file.Name()
	if _, err = file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		log.Errorf("usage: write deferred content file: %v", err)
		return content, ""
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		log.Errorf("usage: close deferred content file: %v", err)
		return content, ""
	}
	return "", path
}
