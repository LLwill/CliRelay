package auth

import "strings"

func (m *Manager) normalizeProviders(providers []string) []string {
	if len(providers) == 0 {
		return nil
	}
	result := make([]string, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		p := strings.TrimSpace(strings.ToLower(provider))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		result = append(result, p)
	}
	return result
}

// List returns all auth entries currently known by the manager.
func (m *Manager) List() []*Auth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Auth, 0, len(m.auths))
	for _, auth := range m.auths {
		list = append(list, auth.Clone())
	}
	return list
}

func (m *Manager) SetSelector(selector Selector) {
	if m == nil {
		return
	}
	if selector == nil {
		selector = &RoundRobinSelector{}
	}
	m.mu.Lock()
	m.selector = selector
	if roundRobinSelector, ok := selector.(*RoundRobinSelector); ok {
		m.roundRobinSelector = roundRobinSelector
	}
	if fillFirstSelector, ok := selector.(*FillFirstSelector); ok {
		m.fillFirstSelector = fillFirstSelector
	}
	if sessionStickySelector, ok := selector.(*SessionStickySelector); ok {
		m.sessionStickySelector = sessionStickySelector
	}
	if m.roundRobinSelector == nil {
		m.roundRobinSelector = &RoundRobinSelector{}
	}
	if m.fillFirstSelector == nil {
		m.fillFirstSelector = &FillFirstSelector{}
	}
	if m.sessionStickySelector == nil {
		m.sessionStickySelector = NewSessionStickySelector(m.roundRobinSelector)
	}
	m.mu.Unlock()
}

func (m *Manager) selectorForRoutingScopeLocked(cfg *runtimeConfigSnapshot, routeGroup string, allowedGroups map[string]struct{}) Selector {
	if m == nil {
		return &RoundRobinSelector{}
	}
	switch scopedRoutingStrategy(cfg, routeGroup, allowedGroups) {
	case "session-sticky":
		if m.sessionStickySelector != nil {
			return m.sessionStickySelector
		}
		return NewSessionStickySelector(m.roundRobinSelector)
	case "fill-first":
		if m.fillFirstSelector != nil {
			return m.fillFirstSelector
		}
		return &FillFirstSelector{}
	case "round-robin":
		if m.roundRobinSelector != nil {
			return m.roundRobinSelector
		}
		return &RoundRobinSelector{}
	default:
		if m.selector != nil {
			return m.selector
		}
		return &RoundRobinSelector{}
	}
}

func authAllowedByChannels(auth *Auth, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	if auth == nil {
		return false
	}
	for _, identifier := range auth.ChannelIdentifiers() {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(identifier))]; ok {
			return true
		}
	}
	return false
}

// CanServeModelWithChannels reports whether at least one active auth in the allowed channel set supports modelID.
func (m *Manager) CanServeModelWithChannels(modelID string, allowed map[string]struct{}) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	return m.CanServeModelWithScopes(modelID, allowed, nil, "")
}

// GetByID retrieves an auth entry by its ID.
func (m *Manager) GetByID(id string) (*Auth, bool) {
	if id == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	auth, ok := m.auths[id]
	if !ok {
		return nil, false
	}
	return auth.Clone(), true
}

// Executor returns the registered provider executor for a provider key.
func (m *Manager) Executor(provider string) (ProviderExecutor, bool) {
	if m == nil {
		return nil, false
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	executor, ok := m.executors[provider]
	return executor, ok
}

// CloseExecutionSession asks all registered executors to release the supplied execution session.
func (m *Manager) CloseExecutionSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if m == nil || sessionID == "" {
		return
	}

	m.mu.RLock()
	executors := make([]ProviderExecutor, 0, len(m.executors))
	for _, exec := range m.executors {
		executors = append(executors, exec)
	}
	m.mu.RUnlock()

	for i := range executors {
		if closer, ok := executors[i].(ExecutionSessionCloser); ok && closer != nil {
			closer.CloseExecutionSession(sessionID)
		}
	}
}
