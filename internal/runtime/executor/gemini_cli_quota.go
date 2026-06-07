package executor

import (
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/geminicli"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func newGeminiStatusErr(statusCode int, body []byte) statusErr {
	err := statusErr{code: statusCode, msg: string(body)}
	if statusCode == http.StatusTooManyRequests {
		if retryAfter, parseErr := parseRetryDelay(body); parseErr == nil && retryAfter != nil {
			err.retryAfter = retryAfter
		}
	}
	return err
}

func geminiCLIProjectID(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if projectID, ok := auth.Metadata["project_id"].(string); ok {
			projectID = strings.TrimSpace(projectID)
			if projectID != "" {
				return projectID
			}
		}
	}
	if auth.Attributes != nil {
		projectID := strings.TrimSpace(auth.Attributes["project_id"])
		if projectID != "" {
			return projectID
		}
	}
	if runtime, ok := auth.Runtime.(*geminicli.VirtualCredential); ok && runtime != nil {
		return strings.TrimSpace(runtime.ProjectID)
	}
	return ""
}

func parseGeminiCLIQuotaProbe(auth *cliproxyauth.Auth, body []byte) *cliproxyauth.QuotaProbeResult {
	if len(body) == 0 {
		return nil
	}

	groupStatus := make(map[string]cliproxyauth.QuotaProbeModelResult)
	var (
		anyRecovered bool
		earliest     time.Time
	)

	for _, bucket := range gjson.GetBytes(body, "buckets").Array() {
		modelID := normalizeGeminiCLIQuotaModelID(bucket.Get("modelId").String())
		if modelID == "" {
			modelID = normalizeGeminiCLIQuotaModelID(bucket.Get("model_id").String())
		}
		if modelID == "" {
			continue
		}

		groupKey := geminiCLIQuotaGroupKey(modelID)
		status := groupStatus[groupKey]
		recovered := false

		if remainingFraction := bucket.Get("remainingFraction"); remainingFraction.Exists() && remainingFraction.Float() > 0 {
			recovered = true
		}
		if !recovered {
			if remainingFraction := bucket.Get("remaining_fraction"); remainingFraction.Exists() && remainingFraction.Float() > 0 {
				recovered = true
			}
		}
		if !recovered {
			if remainingAmount := bucket.Get("remainingAmount"); remainingAmount.Exists() && remainingAmount.Float() > 0 {
				recovered = true
			}
		}
		if !recovered {
			if remainingAmount := bucket.Get("remaining_amount"); remainingAmount.Exists() && remainingAmount.Float() > 0 {
				recovered = true
			}
		}

		if recovered {
			status.Recovered = true
			status.NextRecoverAt = time.Time{}
			groupStatus[groupKey] = status
			anyRecovered = true
			continue
		}

		resetAt := parseGeminiCLIQuotaReset(bucket)
		if !resetAt.IsZero() && (status.NextRecoverAt.IsZero() || resetAt.Before(status.NextRecoverAt)) {
			status.NextRecoverAt = resetAt
		}
		groupStatus[groupKey] = status
		if !resetAt.IsZero() && (earliest.IsZero() || resetAt.Before(earliest)) {
			earliest = resetAt
		}
	}

	blockedModels := make(map[string]struct{})
	if auth != nil {
		for modelID, state := range auth.ModelStates {
			if state == nil || !state.Quota.Exceeded {
				continue
			}
			key := canonicalGeminiCLIBlockedModel(modelID)
			if key == "" {
				continue
			}
			blockedModels[key] = struct{}{}
		}
	}

	if len(blockedModels) == 0 {
		return &cliproxyauth.QuotaProbeResult{
			Recovered:     anyRecovered,
			NextRecoverAt: earliest,
		}
	}

	modelResults := make(map[string]cliproxyauth.QuotaProbeModelResult, len(blockedModels))
	for modelID := range blockedModels {
		groupKey := geminiCLIQuotaGroupKey(modelID)
		if status, ok := groupStatus[groupKey]; ok {
			modelResults[modelID] = status
		}
	}
	return &cliproxyauth.QuotaProbeResult{Models: modelResults}
}

func normalizeGeminiCLIQuotaModelID(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if strings.HasPrefix(modelID, "projects/") {
		modelID = strings.TrimPrefix(modelID, "projects/")
		if idx := strings.Index(modelID, "/"); idx >= 0 {
			modelID = modelID[idx+1:]
		}
	}
	if idx := strings.LastIndex(modelID, "/models/"); idx >= 0 {
		modelID = modelID[idx+len("/models/"):]
	}
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		modelID = modelID[idx+1:]
	}
	return strings.TrimSpace(modelID)
}

func canonicalGeminiCLIBlockedModel(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	return normalizeGeminiCLIQuotaModelID(thinking.ParseSuffix(modelID).ModelName)
}

func geminiCLIQuotaGroupKey(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case strings.HasPrefix(modelID, "gemini-2.5-pro"):
		return "gemini-2.5-pro"
	case strings.HasPrefix(modelID, "gemini-2.5-flash-lite"):
		return "gemini-2.5-flash-lite"
	case strings.HasPrefix(modelID, "gemini-2.5-flash"):
		return "gemini-2.5-flash"
	case strings.HasPrefix(modelID, "gemini-2.0-flash"):
		return "gemini-2.0-flash"
	case strings.HasPrefix(modelID, "gemini-1.5-pro"):
		return "gemini-1.5-pro"
	case strings.HasPrefix(modelID, "gemini-1.5-flash"):
		return "gemini-1.5-flash"
	default:
		return modelID
	}
}

func parseGeminiCLIQuotaReset(bucket gjson.Result) time.Time {
	for _, key := range []string{"resetTime", "reset_time"} {
		raw := strings.TrimSpace(bucket.Get(key).String())
		if raw == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
