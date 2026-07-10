package updateflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTriggerUpdateForwardsVersionAndReleaseMetadata(t *testing.T) {
	updater := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/update" {
			t.Fatalf("path = %q, want /v1/update", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		for key, want := range map[string]string{
			"current_version":      "main-old",
			"version":              "main-new",
			"commit_url":           "https://example.com/backend",
			"ui_commit_url":        "https://example.com/ui",
			"release_name":         "CliRelay v0.5.0",
			"release_tag":          "v0.5.0",
			"release_notes":        "latest changes",
			"release_published_at": "2026-07-10T07:30:00Z",
		} {
			if payload[key] != want {
				t.Fatalf("payload[%q] = %q, want %q", key, payload[key], want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"accepted","service":"clirelay","run_id":17}`))
	}))
	t.Cleanup(updater.Close)
	t.Setenv("CLIRELAY_UPDATER_URL", updater.URL)
	t.Setenv("CLIRELAY_UPDATER_TOKEN", "test-token")
	t.Setenv("CLIRELAY_TARGET_SERVICE", "clirelay")

	result, err := New(nil, Dependencies{}).TriggerUpdate(context.Background(), &CheckResponse{
		CurrentVersion:     "main-old",
		LatestVersion:      "main-new",
		LatestCommitURL:    "https://example.com/backend",
		LatestUICommitURL:  "https://example.com/ui",
		ReleaseName:        "CliRelay v0.5.0",
		ReleaseTag:         "v0.5.0",
		ReleaseNotes:       "latest changes",
		ReleasePublishedAt: "2026-07-10T07:30:00Z",
	})
	if err != nil {
		t.Fatalf("TriggerUpdate failed: %v", err)
	}
	if result.RunID != 17 || result.Status != "accepted" {
		t.Fatalf("result = %+v", result)
	}
}
