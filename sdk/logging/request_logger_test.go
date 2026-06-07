package logging

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestSanitizeForFilenameNormalizesUnsafeCharacters(t *testing.T) {
	t.Parallel()

	logger := &FileRequestLogger{}
	got := logger.sanitizeForFilename("/v1/responses: latest?test")
	if got != "v1-responses-latest-test" {
		t.Fatalf("sanitizeForFilename = %q, want %q", got, "v1-responses-latest-test")
	}
}

func TestMaskSensitiveHeaderValueMasksSecrets(t *testing.T) {
	t.Parallel()

	if got := maskSensitiveHeaderValue("Authorization", "Bearer secret-token-value"); got != "Bearer secr...alue" {
		t.Fatalf("authorization mask = %q", got)
	}
	if got := maskSensitiveHeaderValue("x-api-key", "1234567890"); got != "1234...7890" {
		t.Fatalf("api key mask = %q", got)
	}
	if got := maskSensitiveHeaderValue("x-trace-id", "visible"); got != "visible" {
		t.Fatalf("non-sensitive header unexpectedly masked = %q", got)
	}
}

func TestDecompressResponseHandlesGzip(t *testing.T) {
	t.Parallel()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("hello gzip")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	logger := &FileRequestLogger{}
	got, err := logger.decompressResponse(map[string][]string{
		"Content-Encoding": {"gzip"},
	}, compressed.Bytes())
	if err != nil {
		t.Fatalf("decompressResponse returned error: %v", err)
	}
	if string(got) != "hello gzip" {
		t.Fatalf("decompressed body = %q, want %q", string(got), "hello gzip")
	}
}
