package executor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internallogging "github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

func TestStreamingAPIExchangeMaterializesResponseOnlyAtFinalization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx := context.WithValue(ginCtx.Request.Context(), util.ContextKeyGin, ginCtx)
	cfg := &config.Config{}
	cfg.RequestLogStorage.StoreContent = true

	recordAPIRequest(ctx, cfg, upstreamRequestLog{URL: "https://example.test/v1/responses", Method: http.MethodPost})
	recordAPIResponseMetadata(ctx, cfg, http.StatusOK, nil)
	chunk := bytes.Repeat([]byte("x"), 1024)
	for range 2048 {
		appendAPIResponseChunk(ctx, cfg, chunk)
	}

	if _, exists := ginCtx.Get(apiResponseKey); exists {
		t.Fatal("streaming chunks must not rebuild the legacy full API_RESPONSE value")
	}
	raw, exists := ginCtx.Get(apiAttemptsKey)
	if !exists {
		t.Fatal("expected incremental API exchange capture")
	}
	capture, ok := raw.(*apiExchangeCapture)
	if !ok {
		t.Fatalf("capture type = %T", raw)
	}
	t.Cleanup(func() { _ = capture.Close() })
	capture.mu.Lock()
	cachedBeforeFinalize := capture.cachedResponseSnapshot
	capture.mu.Unlock()
	if cachedBeforeFinalize != nil {
		t.Fatal("full response snapshot must stay unmaterialized during chunk ingestion")
	}
	capture.mu.Lock()
	spoolPath := capture.attempts[0].response.path
	capture.mu.Unlock()
	if spoolPath == "" {
		t.Fatal("large API exchange response should spill to a temporary file")
	}

	snapshot := internallogging.APIResponseSnapshot(ginCtx)
	if len(snapshot) < len(chunk)*2048 {
		t.Fatalf("snapshot length = %d, want at least %d", len(snapshot), len(chunk)*2048)
	}
	second := internallogging.APIResponseSnapshot(ginCtx)
	if !bytes.Equal(snapshot, second) {
		t.Fatal("finalized API response snapshot must be stable")
	}
	internallogging.CleanupAPIExchange(ginCtx)
	if _, err := os.Stat(spoolPath); !os.IsNotExist(err) {
		t.Fatalf("API exchange spool was not cleaned, stat err=%v", err)
	}
}

func BenchmarkAppendStreamingAPIExchangeChunk(b *testing.B) {
	gin.SetMode(gin.TestMode)
	chunk := bytes.Repeat([]byte("x"), 1024)
	cfg := &config.Config{}
	cfg.RequestLogStorage.StoreContent = true
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		ctx := context.WithValue(ginCtx.Request.Context(), util.ContextKeyGin, ginCtx)
		recordAPIRequest(ctx, cfg, upstreamRequestLog{URL: "https://example.test/v1/responses", Method: http.MethodPost})
		for range 1024 {
			appendAPIResponseChunk(ctx, cfg, chunk)
		}
		internallogging.CleanupAPIExchange(ginCtx)
	}
}
