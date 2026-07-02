package configaccess

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestProviderEmitsDailySpendingLimitMetadata(t *testing.T) {
	cfg := &config.SDKConfig{
		APIKeyEntries: []config.APIKeyEntry{{
			Key:                "sk-daily-cost",
			DailySpendingLimit: 4.5,
		}},
	}
	p := newProvider("test", buildKeyConfigMap(cfg))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-daily-cost")
	res, authErr := p.Authenticate(context.Background(), req)
	if authErr != nil {
		t.Fatalf("Authenticate() error = %v", authErr)
	}
	if got := res.Metadata["daily-spending-limit"]; got != "4.500000" {
		t.Fatalf("daily-spending-limit metadata = %q, want 4.500000", got)
	}
}
