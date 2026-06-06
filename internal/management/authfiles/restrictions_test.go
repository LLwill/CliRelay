package authfiles

import (
	"net/http"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestBuildRestrictionPayloadIncludesModelRestriction(t *testing.T) {
	now := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)
	nextRetry := now.Add(30 * time.Minute)
	auth := &coreauth.Auth{
		ModelStates: map[string]*coreauth.ModelState{
			"gpt-5": {
				Status:         coreauth.StatusError,
				StatusMessage:  "unauthorized",
				Unavailable:    true,
				NextRetryAfter: nextRetry,
				LastError:      &coreauth.Error{Message: "unauthorized", HTTPStatus: http.StatusUnauthorized},
			},
		},
	}

	restrictions := BuildRestrictionPayload(auth, now)
	if len(restrictions) != 1 {
		t.Fatalf("restrictions length = %d, want 1", len(restrictions))
	}
	got := restrictions[0]
	if got["scope"] != "model" || got["model"] != "gpt-5" || got["http_status"] != http.StatusUnauthorized {
		t.Fatalf("restriction = %#v, want model gpt-5 401", got)
	}
	if retry, ok := got["next_retry_after"].(time.Time); !ok || !retry.Equal(nextRetry) {
		t.Fatalf("next_retry_after = %#v, want %v", got["next_retry_after"], nextRetry)
	}
}

func TestBuildRestrictionPayloadDedupesRepeatedQuotaErrors(t *testing.T) {
	now := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)
	nextRetry := now.Add(25 * time.Minute)
	nextRecover := now.Add(25 * time.Minute)

	makeModelState := func() *coreauth.ModelState {
		return &coreauth.ModelState{
			Status:         coreauth.StatusError,
			StatusMessage:  `{"error":{"type":"usage_limit_reached"}}`,
			Unavailable:    true,
			NextRetryAfter: nextRetry,
			LastError:      &coreauth.Error{Message: "usage limit reached", HTTPStatus: http.StatusTooManyRequests},
			Quota: coreauth.QuotaState{
				Exceeded:      true,
				Reason:        "quota",
				NextRecoverAt: nextRecover,
			},
		}
	}

	auth := &coreauth.Auth{
		Status:         coreauth.StatusError,
		StatusMessage:  `{"error":{"type":"usage_limit_reached"}}`,
		NextRetryAfter: nextRetry,
		LastError:      &coreauth.Error{Message: "usage limit reached", HTTPStatus: http.StatusTooManyRequests},
		Quota: coreauth.QuotaState{
			Exceeded:      true,
			Reason:        "quota",
			NextRecoverAt: nextRecover,
			Window:        "5h",
			WindowMinutes: 300,
		},
		ModelStates: map[string]*coreauth.ModelState{
			"gpt-5.4-mini": makeModelState(),
			"gpt-5.5":      makeModelState(),
		},
	}

	restrictions := BuildRestrictionPayload(auth, now)
	if len(restrictions) != 1 {
		t.Fatalf("restrictions length = %d, want 1", len(restrictions))
	}
	got := restrictions[0]
	if got["scope"] != "auth" || got["http_status"] != http.StatusTooManyRequests {
		t.Fatalf("restriction = %#v, want auth 429", got)
	}
	if got["quota_window"] != "5h" || got["quota_window_minutes"] != 300 {
		t.Fatalf("quota window = %#v/%#v, want 5h/300", got["quota_window"], got["quota_window_minutes"])
	}
	if _, hasModel := got["model"]; hasModel {
		t.Fatalf("restriction model = %#v, want no model field", got["model"])
	}
}

func TestDeduplicateRestrictionEntriesKeepsDistinctReasons(t *testing.T) {
	entries := []map[string]any{
		{"scope": "model", "status": coreauth.StatusError, "http_status": http.StatusTooManyRequests, "reason": "quota-a"},
		{"scope": "model", "status": coreauth.StatusError, "http_status": http.StatusTooManyRequests, "reason": "quota-b"},
	}

	got := DeduplicateRestrictionEntries(entries)
	if len(got) != 2 {
		t.Fatalf("deduped length = %d, want 2", len(got))
	}
}
