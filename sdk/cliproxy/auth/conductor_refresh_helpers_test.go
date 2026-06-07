package auth

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseDurationValue_SupportsJSONNumberAndStringDuration(t *testing.T) {
	t.Parallel()

	if got := parseDurationValue(json.Number("90")); got != 90*time.Second {
		t.Fatalf("parseDurationValue(json.Number) = %v, want %v", got, 90*time.Second)
	}
	if got := parseDurationValue("1.5m"); got != 90*time.Second {
		t.Fatalf("parseDurationValue(duration string) = %v, want %v", got, 90*time.Second)
	}
	if got := parseDurationValue("45"); got != 45*time.Second {
		t.Fatalf("parseDurationValue(seconds string) = %v, want %v", got, 45*time.Second)
	}
}

func TestAuthTokenValue_ReadsNestedTokenMap(t *testing.T) {
	t.Parallel()

	auth := &Auth{
		Metadata: map[string]any{
			"Token": map[string]any{
				"refresh_token": "nested-refresh-token",
			},
		},
	}

	if got := authRefreshToken(auth); got != "nested-refresh-token" {
		t.Fatalf("authRefreshToken() = %q, want nested-refresh-token", got)
	}
}
