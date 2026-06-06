package callback

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestStartOnAvailablePortFallsBackWhenPreferredBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on busy port: %v", err)
	}
	defer func() { _ = busy.Close() }()

	preferredPort := busy.Addr().(*net.TCPAddr).Port
	forwarder, actualPort, err := StartOnAvailablePort(preferredPort, "gemini", "http://example.test/google/callback")
	if err != nil {
		t.Fatalf("StartOnAvailablePort returned error: %v", err)
	}
	defer StopInstance(context.Background(), actualPort, forwarder)

	if actualPort == preferredPort {
		t.Fatalf("actualPort = preferredPort = %d, want fallback port", actualPort)
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(actualPort) + "/oauth2callback?code=abc&state=xyz")
	if err != nil {
		t.Fatalf("GET callback forwarder: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "http://example.test/google/callback") ||
		!strings.Contains(location, "code=abc") ||
		!strings.Contains(location, "state=xyz") {
		t.Fatalf("unexpected redirect location: %q", location)
	}
}
