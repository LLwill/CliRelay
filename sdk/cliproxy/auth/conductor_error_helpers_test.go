package auth

import (
	"net/http"
	"testing"
)

type statusQuotaErrorStub struct {
	message      string
	status       int
	quotaWindow  string
	quotaMinutes int
}

func (e *statusQuotaErrorStub) Error() string {
	return e.message
}

func (e *statusQuotaErrorStub) StatusCode() int {
	return e.status
}

func (e *statusQuotaErrorStub) QuotaWindow() (string, int) {
	return e.quotaWindow, e.quotaMinutes
}

func TestErrorFromExecution_ExtractsStatusAndQuotaWindow(t *testing.T) {
	t.Parallel()

	err := &statusQuotaErrorStub{
		message:      "quota exceeded",
		status:       http.StatusTooManyRequests,
		quotaWindow:  "5h",
		quotaMinutes: 300,
	}

	got := errorFromExecution(err)
	if got == nil {
		t.Fatal("errorFromExecution() = nil")
	}
	if got.Message != "quota exceeded" {
		t.Fatalf("Message = %q, want quota exceeded", got.Message)
	}
	if got.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("HTTPStatus = %d, want %d", got.HTTPStatus, http.StatusTooManyRequests)
	}
	if got.QuotaWindow != "5h" || got.QuotaWindowMinutes != 300 {
		t.Fatalf("QuotaWindow = %q/%d, want 5h/300", got.QuotaWindow, got.QuotaWindowMinutes)
	}
}

func TestIsRequestInvalidError_RecognizesUnsupportedCodexModelPayload(t *testing.T) {
	t.Parallel()

	err := &Error{
		HTTPStatus: http.StatusBadRequest,
		Message:    `{"detail":"The 'gpt-5.1-codex' model is not supported when using Codex with a ChatGPT account."}`,
	}

	if !isRequestInvalidError(err) {
		t.Fatal("expected unsupported codex model payload to be treated as invalid request")
	}
}

func TestIsRequestInvalidError_IgnoresNonBadRequest(t *testing.T) {
	t.Parallel()

	err := &Error{
		HTTPStatus: http.StatusBadGateway,
		Message:    "upstream failed",
	}

	if isRequestInvalidError(err) {
		t.Fatal("expected non-400 upstream error to remain retryable/failover-eligible")
	}
}
