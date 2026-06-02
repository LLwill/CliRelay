package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestParseOpenCodeGoUsageHTML(t *testing.T) {
	html := `<div data-slot="usage">
		<div>Rolling Usage</div><span>3%</span><span>Resets in 31 minutes</span>
		<div>Weekly Usage</div><span>1%</span><span>Resets in 5 days 16 hours</span>
		<div>Monthly Usage</div><span>0%</span><span>Resets in 29 days 0 hours</span>
	</div>`

	items := parseOpenCodeGoUsageHTML(html)
	if len(items) != 3 {
		t.Fatalf("usage item count = %d, want 3: %+v", len(items), items)
	}
	if items[0].Type != "rolling" || items[0].Percentage != 3 || items[0].ResetsIn != "31 minutes" {
		t.Fatalf("rolling item = %+v", items[0])
	}
	if items[1].Type != "weekly" || items[1].Percentage != 1 || items[1].ResetsIn != "5 days 16 hours" {
		t.Fatalf("weekly item = %+v", items[1])
	}
	if items[2].Type != "monthly" || items[2].Percentage != 0 || items[2].ResetsIn != "29 days 0 hours" {
		t.Fatalf("monthly item = %+v", items[2])
	}
}

func TestParseOpenCodeGoUsageHydrationHTML(t *testing.T) {
	html := `<script>
		rollingUsage:$R[1]={usagePercent:12.4,resetInSec:3720}
		weeklyUsage:$R[2]={resetInSec:432000,usagePercent:34}
		monthlyUsage:$R[3]={usagePercent:56,resetInSec:2505600}
	</script>`

	items := parseOpenCodeGoUsageHTML(html)
	if len(items) != 3 {
		t.Fatalf("usage item count = %d, want 3: %+v", len(items), items)
	}
	if items[0].Type != "rolling" || items[0].Percentage != 12 || items[0].ResetsIn != "1 hour 2 minutes" {
		t.Fatalf("rolling item = %+v", items[0])
	}
	if items[1].Type != "weekly" || items[1].Percentage != 34 || items[1].ResetsIn != "5 days" {
		t.Fatalf("weekly item = %+v", items[1])
	}
	if items[2].Type != "monthly" || items[2].Percentage != 56 || items[2].ResetsIn != "29 days" {
		t.Fatalf("monthly item = %+v", items[2])
	}
}

func TestNormalizeOpenCodeGoAuthCookie(t *testing.T) {
	tests := map[string]string{
		"token":                        "token",
		" auth=abc123; oc_locale=en ":  "abc123",
		"Cookie: foo=bar; auth=abc; z": "abc",
		"cookie: auth=lowercase":       "lowercase",
		"foo=bar; session=not-an-auth": "",
		"token=with-padding":           "token=with-padding",
	}
	for input, want := range tests {
		if got := normalizeOpenCodeGoAuthCookie(input); got != want {
			t.Fatalf("normalizeOpenCodeGoAuthCookie(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeOpenCodeGoWorkspaceID(t *testing.T) {
	tests := map[string]string{
		"wrk_123": "wrk_123",
		" https://opencode.ai/workspace/wrk_123/go ":  "wrk_123",
		"/workspace/wrk_456/go":                       "wrk_456",
		"https://opencode.ai/workspace/wrk_789/usage": "wrk_789",
	}
	for input, want := range tests {
		got, err := normalizeOpenCodeGoWorkspaceID(input)
		if err != nil {
			t.Fatalf("normalizeOpenCodeGoWorkspaceID(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeOpenCodeGoWorkspaceID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeOpenCodeGoWorkspaceIDRejectsNamesAndServerIDs(t *testing.T) {
	tests := []string{
		"Default",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	for _, input := range tests {
		if _, err := normalizeOpenCodeGoWorkspaceID(input); err == nil {
			t.Fatalf("normalizeOpenCodeGoWorkspaceID(%q) error = nil, want validation error", input)
		}
	}
}

func TestQueryOpenCodeGoUsageFetchesDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspace/wrk_test/go" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Cookie"); got != "auth=token; oc_locale=en" {
			t.Fatalf("cookie = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<div data-slot="usage">
			Rolling Usage <strong>12%</strong> Resets in 2 hours
			Weekly Usage <strong>34%</strong> Resets in 4 days
			Monthly Usage <strong>56%</strong> Resets in 20 days
		</div>`))
	}))
	defer upstream.Close()

	prevBaseURL := openCodeGoConsoleBaseURL
	openCodeGoConsoleBaseURL = upstream.URL
	defer func() { openCodeGoConsoleBaseURL = prevBaseURL }()

	h := &Handler{cfg: &config.Config{
		OpenCodeGoKey: []config.OpenCodeGoKey{{
			APIKey:      "sk-go",
			Name:        "OpenCode Go",
			WorkspaceID: "wrk_test",
			AuthCookie:  "auth=token; oc_locale=zh-CN",
		}},
	}}

	body := []byte(`{"index":0}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var decoded struct {
		WorkspaceID string                `json:"workspace_id"`
		Usage       []openCodeGoUsageItem `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.WorkspaceID != "wrk_test" || len(decoded.Usage) != 3 || decoded.Usage[2].Percentage != 56 {
		t.Fatalf("response = %+v", decoded)
	}
}

func TestQueryOpenCodeGoUsageFetchesHydrationDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspace/wrk_test/go" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><script>
			rollingUsage:$R[10]={usagePercent:7,resetInSec:120}
			weeklyUsage:$R[11]={resetInSec:3600,usagePercent:8}
			monthlyUsage:$R[12]={usagePercent:9,resetInSec:86400}
		</script></html>`))
	}))
	defer upstream.Close()

	prevBaseURL := openCodeGoConsoleBaseURL
	openCodeGoConsoleBaseURL = upstream.URL
	defer func() { openCodeGoConsoleBaseURL = prevBaseURL }()

	h := &Handler{cfg: &config.Config{}}
	body := []byte(`{"workspace-id":"wrk_test","auth-cookie":"token"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var decoded struct {
		Usage []openCodeGoUsageItem `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(decoded.Usage) != 3 || decoded.Usage[0].Percentage != 7 || decoded.Usage[2].ResetsIn != "1 day" {
		t.Fatalf("response = %+v", decoded)
	}
}

func TestQueryOpenCodeGoUsageAcceptsDashboardURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspace/wrk_test/go" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`Rolling Usage 1% Resets in 1 hour Weekly Usage 2% Resets in 2 days Monthly Usage 3% Resets in 3 days`))
	}))
	defer upstream.Close()

	prevBaseURL := openCodeGoConsoleBaseURL
	openCodeGoConsoleBaseURL = upstream.URL
	defer func() { openCodeGoConsoleBaseURL = prevBaseURL }()

	h := &Handler{cfg: &config.Config{}}
	body := []byte(`{"workspace-id":"https://opencode.ai/workspace/wrk_test/go","auth-cookie":"token"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"workspace_id":"wrk_test"`)) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestQueryOpenCodeGoUsageRejectsWorkspaceName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{cfg: &config.Config{}}
	body := []byte(`{"workspace-id":"Default","auth-cookie":"token"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("/workspace/{id}/go")) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestQueryOpenCodeGoUsageReportsExpiredCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`Continue with GitHub Continue with Google`))
	}))
	defer upstream.Close()

	prevBaseURL := openCodeGoConsoleBaseURL
	openCodeGoConsoleBaseURL = upstream.URL
	defer func() { openCodeGoConsoleBaseURL = prevBaseURL }()

	h := &Handler{cfg: &config.Config{}}
	body := []byte(`{"workspace-id":"wrk_test","auth-cookie":"token"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("invalid or expired")) {
		t.Fatalf("body = %s", w.Body.String())
	}
}
