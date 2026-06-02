package management

import (
	"context"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

var (
	openCodeGoConsoleBaseURL = "https://opencode.ai"
	openCodeGoUsagePattern   = regexp.MustCompile(`(?i)(Rolling|Weekly|Monthly)\s+Usage\s+([0-9]{1,3})%\s+Resets\s+in\s+`)
	openCodeGoNumberPattern  = `(-?\d+(?:\.\d+)?)`
	openCodeGoUsageWindows   = []openCodeGoUsageWindowPattern{
		{usageType: "rolling", label: "Rolling", pctFirst: regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{[^}]*usagePercent:` + openCodeGoNumberPattern + `[^}]*resetInSec:` + openCodeGoNumberPattern + `[^}]*\}`), resetFirst: regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{[^}]*resetInSec:` + openCodeGoNumberPattern + `[^}]*usagePercent:` + openCodeGoNumberPattern + `[^}]*\}`)},
		{usageType: "weekly", label: "Weekly", pctFirst: regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{[^}]*usagePercent:` + openCodeGoNumberPattern + `[^}]*resetInSec:` + openCodeGoNumberPattern + `[^}]*\}`), resetFirst: regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{[^}]*resetInSec:` + openCodeGoNumberPattern + `[^}]*usagePercent:` + openCodeGoNumberPattern + `[^}]*\}`)},
		{usageType: "monthly", label: "Monthly", pctFirst: regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{[^}]*usagePercent:` + openCodeGoNumberPattern + `[^}]*resetInSec:` + openCodeGoNumberPattern + `[^}]*\}`), resetFirst: regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{[^}]*resetInSec:` + openCodeGoNumberPattern + `[^}]*usagePercent:` + openCodeGoNumberPattern + `[^}]*\}`)},
	}
	openCodeGoServerIDPattern = regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)
	openCodeGoTagPattern      = regexp.MustCompile(`(?s)<[^>]+>`)
	openCodeGoSpacePattern    = regexp.MustCompile(`\s+`)
)

const openCodeGoWorkspaceIDHint = "OpenCode Go workspace-id must be the /workspace/{id}/go URL segment from the dashboard address bar, usually starting with wrk_; workspace names like Default and server id hashes are not valid"

type openCodeGoUsageItem struct {
	Type       string `json:"type"`
	Label      string `json:"label"`
	Percentage int    `json:"percentage"`
	ResetsIn   string `json:"resets_in"`
}

type openCodeGoUsageWindowPattern struct {
	usageType  string
	label      string
	pctFirst   *regexp.Regexp
	resetFirst *regexp.Regexp
}

type openCodeGoUsageRequest struct {
	Index       *int    `json:"index"`
	APIKey      string  `json:"api-key"`
	Name        string  `json:"name"`
	WorkspaceID string  `json:"workspace-id"`
	AuthCookie  string  `json:"auth-cookie"`
	ProxyID     string  `json:"proxy-id"`
	ProxyURL    string  `json:"proxy-url"`
	TimeoutSec  float64 `json:"timeout_sec"`
}

// QueryOpenCodeGoUsage fetches the OpenCode Go dashboard page and parses usage limits.
func (h *Handler) QueryOpenCodeGoUsage(c *gin.Context) {
	var body openCodeGoUsageRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	entry := h.findOpenCodeGoEntry(body)
	workspaceID, workspaceErr := normalizeOpenCodeGoWorkspaceID(body.WorkspaceID)
	authCookie := strings.TrimSpace(body.AuthCookie)
	proxyID := strings.TrimSpace(body.ProxyID)
	proxyURL := strings.TrimSpace(body.ProxyURL)
	if entry != nil {
		if workspaceID == "" {
			workspaceID, workspaceErr = normalizeOpenCodeGoWorkspaceID(entry.WorkspaceID)
		}
		if authCookie == "" {
			authCookie = strings.TrimSpace(entry.AuthCookie)
		}
		if proxyID == "" {
			proxyID = strings.TrimSpace(entry.ProxyID)
		}
		if proxyURL == "" {
			proxyURL = strings.TrimSpace(entry.ProxyURL)
		}
	}
	if workspaceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workspace-id is required"})
		return
	}
	if workspaceErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": workspaceErr.Error()})
		return
	}
	authCookie = normalizeOpenCodeGoAuthCookie(authCookie)
	if authCookie == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth-cookie is required"})
		return
	}

	timeout := 20 * time.Second
	if body.TimeoutSec > 0 {
		timeout = time.Duration(body.TimeoutSec * float64(time.Second))
		if timeout < 3*time.Second {
			timeout = 3 * time.Second
		}
		if timeout > 60*time.Second {
			timeout = 60 * time.Second
		}
	}

	items, err := h.fetchOpenCodeGoUsage(c.Request.Context(), workspaceID, authCookie, proxyID, proxyURL, timeout)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"workspace_id": workspaceID,
		"usage":        items,
	})
}

func (h *Handler) findOpenCodeGoEntry(body openCodeGoUsageRequest) *config.OpenCodeGoKey {
	if h == nil || h.cfg == nil {
		return nil
	}
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.OpenCodeGoKey) {
		return &h.cfg.OpenCodeGoKey[*body.Index]
	}
	apiKey := strings.TrimSpace(body.APIKey)
	if apiKey != "" {
		for i := range h.cfg.OpenCodeGoKey {
			if strings.TrimSpace(h.cfg.OpenCodeGoKey[i].APIKey) == apiKey {
				return &h.cfg.OpenCodeGoKey[i]
			}
		}
	}
	name := strings.TrimSpace(body.Name)
	if name != "" {
		for i := range h.cfg.OpenCodeGoKey {
			if strings.TrimSpace(h.cfg.OpenCodeGoKey[i].Name) == name {
				return &h.cfg.OpenCodeGoKey[i]
			}
		}
	}
	return nil
}

func (h *Handler) fetchOpenCodeGoUsage(ctx context.Context, workspaceID, authCookie, proxyID, proxyURL string, timeout time.Duration) ([]openCodeGoUsageItem, error) {
	client := util.NewHTTPClient(timeout)
	if h != nil && h.cfg != nil {
		if resolved := strings.TrimSpace(h.cfg.ResolveProxyURL(proxyID, proxyURL)); resolved != "" {
			if transport := util.BuildProxyTransport(resolved, h.cfg.PreferIPv4); transport != nil {
				client.Transport = transport
			}
		}
	}

	pageURL := strings.TrimRight(openCodeGoConsoleBaseURL, "/") + "/workspace/" + url.PathEscape(workspaceID) + "/go"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "auth="+authCookie+"; oc_locale=en")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CliRelay OpenCode Go usage checker)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, openCodeGoUsageError("OpenCode Go usage page returned HTTP " + resp.Status)
	}
	items := parseOpenCodeGoUsageHTML(string(body))
	if len(items) == 0 {
		text := strings.ToLower(stripOpenCodeGoHTML(string(body)))
		if strings.Contains(text, "continue with github") || strings.Contains(text, "continue with google") {
			return nil, openCodeGoUsageError("OpenCode Go auth cookie is invalid or expired")
		}
		return nil, openCodeGoUsageError("OpenCode Go usage data was not found on the dashboard page")
	}
	return items, nil
}

type openCodeGoUsageError string

func (e openCodeGoUsageError) Error() string { return string(e) }

func normalizeOpenCodeGoAuthCookie(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "cookie:") {
		raw = strings.TrimSpace(raw[len("cookie:"):])
	}
	raw = strings.TrimSpace(raw)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "auth=") {
			return strings.TrimSpace(part[5:])
		}
	}
	if strings.Contains(raw, ";") && strings.Contains(raw, "=") {
		return ""
	}
	return raw
}

func normalizeOpenCodeGoWorkspaceID(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), `"'`)
	if raw == "" {
		return "", nil
	}
	if id := extractOpenCodeGoWorkspaceID(raw); id != "" {
		return id, nil
	}
	trimmed := strings.Trim(raw, "/")
	if strings.EqualFold(trimmed, "default") || openCodeGoServerIDPattern.MatchString(trimmed) {
		return trimmed, openCodeGoUsageError(openCodeGoWorkspaceIDHint)
	}
	return trimmed, nil
}

func extractOpenCodeGoWorkspaceID(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Path != "" {
		if id := extractOpenCodeGoWorkspaceIDFromPath(parsed.Path); id != "" {
			return id
		}
	}
	return extractOpenCodeGoWorkspaceIDFromPath(raw)
}

func extractOpenCodeGoWorkspaceIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part != "workspace" || i+1 >= len(parts) {
			continue
		}
		id := strings.TrimSpace(parts[i+1])
		if id == "" {
			continue
		}
		if unescaped, err := url.PathUnescape(id); err == nil {
			id = unescaped
		}
		return strings.TrimSpace(id)
	}
	return ""
}

func parseOpenCodeGoUsageHTML(body string) []openCodeGoUsageItem {
	if items := parseOpenCodeGoHydrationUsage(body); len(items) > 0 {
		return items
	}
	text := stripOpenCodeGoHTML(body)
	matches := openCodeGoUsagePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	items := make([]openCodeGoUsageItem, 0, len(matches))
	for i, match := range matches {
		if len(match) < 6 {
			continue
		}
		percentage := 0
		for _, ch := range text[match[4]:match[5]] {
			percentage = percentage*10 + int(ch-'0')
		}
		resetEnd := len(text)
		if i+1 < len(matches) {
			resetEnd = matches[i+1][0]
		}
		label := strings.TrimSpace(text[match[2]:match[3]])
		items = append(items, openCodeGoUsageItem{
			Type:       strings.ToLower(label),
			Label:      label,
			Percentage: percentage,
			ResetsIn:   strings.TrimSpace(text[match[1]:resetEnd]),
		})
	}
	return items
}

func parseOpenCodeGoHydrationUsage(body string) []openCodeGoUsageItem {
	items := make([]openCodeGoUsageItem, 0, len(openCodeGoUsageWindows))
	for _, window := range openCodeGoUsageWindows {
		percentage, resetInSec, ok := parseOpenCodeGoHydrationWindow(body, window)
		if !ok {
			continue
		}
		items = append(items, openCodeGoUsageItem{
			Type:       window.usageType,
			Label:      window.label,
			Percentage: percentage,
			ResetsIn:   formatOpenCodeGoResetIn(resetInSec),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func parseOpenCodeGoHydrationWindow(body string, window openCodeGoUsageWindowPattern) (int, int64, bool) {
	if match := window.pctFirst.FindStringSubmatch(body); len(match) == 3 {
		percentage, resetInSec, ok := parseOpenCodeGoHydrationNumbers(match[1], match[2])
		return percentage, resetInSec, ok
	}
	if match := window.resetFirst.FindStringSubmatch(body); len(match) == 3 {
		percentage, resetInSec, ok := parseOpenCodeGoHydrationNumbers(match[2], match[1])
		return percentage, resetInSec, ok
	}
	return 0, 0, false
}

func parseOpenCodeGoHydrationNumbers(usagePercentRaw, resetInSecRaw string) (int, int64, bool) {
	usagePercent, err := strconv.ParseFloat(usagePercentRaw, 64)
	if err != nil {
		return 0, 0, false
	}
	resetInSec, err := strconv.ParseFloat(resetInSecRaw, 64)
	if err != nil {
		return 0, 0, false
	}
	if usagePercent < 0 {
		usagePercent = 0
	}
	if resetInSec < 0 {
		resetInSec = 0
	}
	return int(math.Round(usagePercent)), int64(math.Round(resetInSec)), true
}

func formatOpenCodeGoResetIn(seconds int64) string {
	duration := time.Duration(seconds) * time.Second
	days := int(duration / (24 * time.Hour))
	duration -= time.Duration(days) * 24 * time.Hour
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	if days > 0 {
		if hours > 0 {
			return formatOpenCodeGoDurationPart(days, "day") + " " + formatOpenCodeGoDurationPart(hours, "hour")
		}
		return formatOpenCodeGoDurationPart(days, "day")
	}
	if hours > 0 {
		if minutes > 0 {
			return formatOpenCodeGoDurationPart(hours, "hour") + " " + formatOpenCodeGoDurationPart(minutes, "minute")
		}
		return formatOpenCodeGoDurationPart(hours, "hour")
	}
	if minutes > 0 {
		return formatOpenCodeGoDurationPart(minutes, "minute")
	}
	return formatOpenCodeGoDurationPart(int(seconds), "second")
}

func formatOpenCodeGoDurationPart(value int, unit string) string {
	suffix := unit
	if value != 1 {
		suffix += "s"
	}
	return strconv.Itoa(value) + " " + suffix
}

func stripOpenCodeGoHTML(body string) string {
	text := openCodeGoTagPattern.ReplaceAllString(body, " ")
	text = html.UnescapeString(text)
	text = openCodeGoSpacePattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
