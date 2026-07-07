package auth

import (
	"net/http"
	"strings"
	"time"
)

func preserveRuntimeFields(dst *Auth, src *Auth) {
	if dst == nil || src == nil {
		return
	}
	if dst.Runtime == nil {
		dst.Runtime = src.Runtime
	}
	if dst.Storage == nil {
		dst.Storage = src.Storage
	}
	if dst.Index == "" {
		dst.Index = src.Index
		dst.indexAssigned = src.indexAssigned
	}
	if dst.FileName == "" {
		dst.FileName = src.FileName
	}
}

func preserveAvailabilityRuntimeForUpdate(dst *Auth, src *Auth, now time.Time) {
	if dst == nil || src == nil {
		return
	}
	if dst.Disabled || dst.Status == StatusDisabled {
		return
	}

	preservedModelState := false
	if len(src.ModelStates) > 0 {
		if dst.ModelStates == nil {
			dst.ModelStates = make(map[string]*ModelState, len(src.ModelStates))
		}
		for model, srcState := range src.ModelStates {
			if !shouldPreserveModelRuntimeState(dst.ModelStates[model], srcState, now) {
				continue
			}
			if shouldDropOllamaCloudNotFoundRuntimeState(dst, srcState) {
				continue
			}
			dst.ModelStates[model] = srcState.Clone()
			preservedModelState = true
		}
	}

	if preservedModelState {
		updateAggregatedAvailability(dst, now)
		if dst.Status == "" || dst.Status == StatusUnknown || dst.Status == StatusActive {
			dst.Status = src.Status
		}
		if dst.StatusMessage == "" {
			dst.StatusMessage = src.StatusMessage
		}
		if dst.LastError == nil {
			dst.LastError = cloneError(src.LastError)
		}
	}

	if shouldDropOllamaCloudNotFoundAuthRuntimeState(dst, src) || !shouldPreserveAuthRuntimeState(dst, src, now) {
		return
	}
	dst.Unavailable = src.Unavailable
	dst.Status = src.Status
	dst.StatusMessage = src.StatusMessage
	dst.LastError = cloneError(src.LastError)
	dst.Quota = src.Quota
	dst.NextRetryAfter = src.NextRetryAfter
}

func shouldDropOllamaCloudNotFoundRuntimeState(auth *Auth, state *ModelState) bool {
	if auth == nil || state == nil || state.LastError == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "ollama-cloud") {
		return false
	}
	// Older builds misrouted Ollama Cloud through OpenAI-compatible chat and
	// persisted false 404 cooldowns; a real 404 will be recorded again.
	return state.LastError.HTTPStatus == http.StatusNotFound
}

func shouldDropOllamaCloudNotFoundAuthRuntimeState(dst *Auth, src *Auth) bool {
	if dst == nil || src == nil || src.LastError == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(dst.Provider), "ollama-cloud") {
		return false
	}
	return src.LastError.HTTPStatus == http.StatusNotFound
}
