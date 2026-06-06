package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTTL      = 10 * time.Minute
	MaxStateLength  = 128
	StatusCompleted = "__completed__"
)

var (
	ErrInvalidState    = errors.New("invalid oauth state")
	ErrUnsupportedFlow = errors.New("unsupported oauth provider")
	ErrNotPending      = errors.New("oauth session is not pending")
)

type Session struct {
	Provider  string
	Status    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Store struct {
	mu       sync.RWMutex
	ttl      time.Duration
	sessions map[string]Session
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{
		ttl:      ttl,
		sessions: make(map[string]Session),
	}
}

func (s *Store) purgeExpiredLocked(now time.Time) {
	for state, session := range s.sessions {
		if !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt) {
			delete(s.sessions, state)
		}
	}
}

func (s *Store) Register(state, provider string) {
	if s == nil {
		return
	}
	state = strings.TrimSpace(state)
	provider = strings.ToLower(strings.TrimSpace(provider))
	if state == "" || provider == "" {
		return
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked(now)
	s.sessions[state] = Session{
		Provider:  provider,
		Status:    "",
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
}

func (s *Store) SetError(state, message string) {
	if s == nil {
		return
	}
	state = strings.TrimSpace(state)
	message = strings.TrimSpace(message)
	if state == "" {
		return
	}
	if message == "" {
		message = "Authentication failed"
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked(now)
	session, ok := s.sessions[state]
	if !ok {
		return
	}
	session.Status = message
	session.ExpiresAt = now.Add(s.ttl)
	s.sessions[state] = session
}

func (s *Store) Complete(state string) {
	if s == nil {
		return
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked(now)
	session, ok := s.sessions[state]
	if !ok {
		return
	}
	session.Status = StatusCompleted
	session.ExpiresAt = now.Add(s.ttl)
	s.sessions[state] = session
}

func (s *Store) CompleteProvider(provider string) int {
	if s == nil {
		return 0
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return 0
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked(now)
	removed := 0
	for state, session := range s.sessions {
		if strings.EqualFold(session.Provider, provider) {
			if session.Status == StatusCompleted {
				continue
			}
			delete(s.sessions, state)
			removed++
		}
	}
	return removed
}

func (s *Store) Get(state string) (Session, bool) {
	if s == nil {
		return Session{}, false
	}
	state = strings.TrimSpace(state)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked(now)
	session, ok := s.sessions[state]
	return session, ok
}

func (s *Store) IsPending(state, provider string) bool {
	if s == nil {
		return false
	}
	state = strings.TrimSpace(state)
	provider = strings.ToLower(strings.TrimSpace(provider))
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purgeExpiredLocked(now)
	session, ok := s.sessions[state]
	if !ok {
		return false
	}
	if session.Status != "" {
		return false
	}
	if provider == "" {
		return true
	}
	return strings.EqualFold(session.Provider, provider)
}

func ValidateState(state string) error {
	trimmed := strings.TrimSpace(state)
	if trimmed == "" {
		return fmt.Errorf("%w: empty", ErrInvalidState)
	}
	if len(trimmed) > MaxStateLength {
		return fmt.Errorf("%w: too long", ErrInvalidState)
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return fmt.Errorf("%w: contains path separator", ErrInvalidState)
	}
	if strings.Contains(trimmed, "..") {
		return fmt.Errorf("%w: contains '..'", ErrInvalidState)
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("%w: invalid character", ErrInvalidState)
		}
	}
	return nil
}

func NormalizeProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "claude":
		return "anthropic", nil
	case "codex", "openai":
		return "codex", nil
	case "gemini", "google":
		return "gemini", nil
	case "iflow", "i-flow":
		return "iflow", nil
	case "antigravity", "anti-gravity":
		return "antigravity", nil
	case "qwen":
		return "qwen", nil
	default:
		return "", ErrUnsupportedFlow
	}
}

type callbackFilePayload struct {
	Code  string `json:"code"`
	State string `json:"state"`
	Error string `json:"error"`
}

func WriteCallbackFile(authDir, provider, state, code, errorMessage string) (string, error) {
	if strings.TrimSpace(authDir) == "" {
		return "", fmt.Errorf("auth dir is empty")
	}
	canonicalProvider, err := NormalizeProvider(provider)
	if err != nil {
		return "", err
	}
	if err := ValidateState(state); err != nil {
		return "", err
	}

	fileName := fmt.Sprintf(".oauth-%s-%s.oauth", canonicalProvider, state)
	filePath := filepath.Join(authDir, fileName)
	payload := callbackFilePayload{
		Code:  strings.TrimSpace(code),
		State: strings.TrimSpace(state),
		Error: strings.TrimSpace(errorMessage),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal oauth callback payload: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return "", fmt.Errorf("write oauth callback file: %w", err)
	}
	return filePath, nil
}

func (s *Store) WriteCallbackFileForPending(authDir, provider, state, code, errorMessage string) (string, error) {
	canonicalProvider, err := NormalizeProvider(provider)
	if err != nil {
		return "", err
	}
	if !s.IsPending(state, canonicalProvider) {
		return "", ErrNotPending
	}
	return WriteCallbackFile(authDir, canonicalProvider, state, code, errorMessage)
}
