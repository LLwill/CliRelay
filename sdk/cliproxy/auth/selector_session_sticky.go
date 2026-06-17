package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

const (
	sessionStickyTTL     = time.Hour
	sessionStickyMaxKeys = 4096
)

type sessionStickyBinding struct {
	authID    string
	expiresAt time.Time
}

// SessionStickySelector keeps stable client sessions on the same auth while it remains available.
type SessionStickySelector struct {
	mu       sync.Mutex
	bindings map[string]sessionStickyBinding
	fallback Selector
	maxKeys  int
}

func NewSessionStickySelector(fallback Selector) *SessionStickySelector {
	if fallback == nil {
		fallback = &RoundRobinSelector{}
	}
	return &SessionStickySelector{fallback: fallback}
}

func (s *SessionStickySelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	key := sessionStickySelectionKey(provider, opts, sessionStickyKey(opts))
	if key == "" {
		return s.pickFallback(ctx, provider, model, opts, auths)
	}

	now := time.Now()
	available, err := getAvailableAuths(auths, provider, model, now, false)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)

	if boundID := s.boundAuthID(key, now); boundID != "" {
		for _, auth := range available {
			if auth != nil && auth.ID == boundID {
				s.refreshBinding(key, boundID, now)
				return auth, nil
			}
		}
		s.deleteBinding(key)
	}

	selected, err := s.pickFallback(ctx, provider, model, opts, available)
	if err != nil {
		return nil, err
	}
	if selected != nil && strings.TrimSpace(selected.ID) != "" {
		s.refreshBinding(key, selected.ID, now)
	}
	return selected, nil
}

func (s *SessionStickySelector) pickFallback(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	if s == nil || s.fallback == nil {
		return (&RoundRobinSelector{}).Pick(ctx, provider, model, opts, auths)
	}
	return s.fallback.Pick(ctx, provider, model, opts, auths)
}

func (s *SessionStickySelector) boundAuthID(key string, now time.Time) string {
	if s == nil || key == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[key]
	if !ok {
		return ""
	}
	if !binding.expiresAt.IsZero() && now.After(binding.expiresAt) {
		delete(s.bindings, key)
		return ""
	}
	return binding.authID
}

func (s *SessionStickySelector) refreshBinding(key, authID string, now time.Time) {
	if s == nil || key == "" || authID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := s.maxKeys
	if limit <= 0 {
		limit = sessionStickyMaxKeys
	}
	if s.bindings == nil {
		s.bindings = make(map[string]sessionStickyBinding)
	} else if len(s.bindings) >= limit {
		s.bindings = make(map[string]sessionStickyBinding)
	}
	s.bindings[key] = sessionStickyBinding{
		authID:    authID,
		expiresAt: now.Add(sessionStickyTTL),
	}
}

func (s *SessionStickySelector) deleteBinding(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	delete(s.bindings, key)
	s.mu.Unlock()
}

func sessionStickySelectionKey(provider string, opts cliproxyexecutor.Options, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	scope := strings.TrimSpace(metadataStringValue(opts.Metadata, cliproxyexecutor.RouteGroupMetadataKey))
	if scope == "" {
		scope = strings.TrimSpace(metadataStringValue(opts.Metadata, "allowed-channel-groups"))
	}
	if scope == "" {
		scope = "default"
	}
	return strings.ToLower(strings.TrimSpace(provider)) + "|" + strings.TrimSpace(opts.SourceFormat.String()) + "|" + scope + "|" + sessionKey
}

func sessionStickyKey(opts cliproxyexecutor.Options) string {
	if key := metadataStringValue(opts.Metadata, cliproxyexecutor.SessionStickyMetadataKey); key != "" {
		return key
	}
	if key := metadataStringValue(opts.Metadata, cliproxyexecutor.ExecutionSessionMetadataKey); key != "" {
		return "execution:" + key
	}
	return bodySessionStickyKey(opts.OriginalRequest)
}

func bodySessionStickyKey(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	paths := []string{
		"session_id",
		"sessionId",
		"conversation_id",
		"conversationId",
		"prompt_cache_key",
		"promptCacheKey",
		"metadata.session_id",
		"metadata.conversation_id",
		"metadata.user_id.session_id",
		"metadata.user_id",
	}
	for _, path := range paths {
		if key := gjson.GetBytes(body, path).String(); strings.TrimSpace(key) != "" {
			return path + ":" + strings.TrimSpace(key)
		}
	}
	if text := cacheControlStickyText(body); text != "" {
		return "cache_control:" + shortStableHash(text)
	}
	if text := firstUserStickyText(body); text != "" {
		return "first_user:" + shortStableHash(text)
	}
	return ""
}

func cacheControlStickyText(body []byte) string {
	var builder strings.Builder
	appendCacheControlText(&builder, gjson.GetBytes(body, "system"))
	messages := gjson.GetBytes(body, "messages")
	if messages.IsArray() {
		messages.ForEach(func(_, msg gjson.Result) bool {
			appendCacheControlText(&builder, msg.Get("content"))
			return true
		})
	}
	return builder.String()
}

func appendCacheControlText(builder *strings.Builder, value gjson.Result) {
	if builder == nil || !value.Exists() {
		return
	}
	if value.IsArray() {
		value.ForEach(func(_, item gjson.Result) bool {
			if item.Get("cache_control.type").String() == "ephemeral" {
				appendContentText(builder, item)
			}
			return true
		})
		return
	}
	if value.Get("cache_control.type").String() == "ephemeral" {
		appendContentText(builder, value)
	}
}

func firstUserStickyText(body []byte) string {
	if input := gjson.GetBytes(body, "input"); input.Exists() {
		if input.Type == gjson.String {
			return strings.TrimSpace(input.String())
		}
		if input.IsArray() {
			if text := firstInputText(input); text != "" {
				return text
			}
		}
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	var text string
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "user" {
			return true
		}
		var builder strings.Builder
		appendContentText(&builder, msg.Get("content"))
		text = strings.TrimSpace(builder.String())
		return false
	})
	return text
}

func firstInputText(input gjson.Result) string {
	var text string
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Type == gjson.String {
			text = strings.TrimSpace(item.String())
			return false
		}
		switch item.Get("role").String() {
		case "user", "system", "developer":
			var builder strings.Builder
			appendContentText(&builder, item.Get("content"))
			text = strings.TrimSpace(builder.String())
			return text == ""
		default:
			if item.Get("type").String() == "input_text" {
				text = strings.TrimSpace(item.Get("text").String())
				return text == ""
			}
		}
		return true
	})
	return text
}

func appendContentText(builder *strings.Builder, value gjson.Result) {
	if builder == nil || !value.Exists() {
		return
	}
	if value.Type == gjson.String {
		_, _ = builder.WriteString(value.String())
		return
	}
	if text := strings.TrimSpace(value.Get("text").String()); text != "" {
		_, _ = builder.WriteString(text)
		return
	}
	if !value.IsArray() {
		return
	}
	value.ForEach(func(_, item gjson.Result) bool {
		if item.Type == gjson.String {
			_, _ = builder.WriteString(item.String())
			return true
		}
		switch item.Get("type").String() {
		case "text", "input_text":
			if text := item.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
		}
		return true
	})
}

func shortStableHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
