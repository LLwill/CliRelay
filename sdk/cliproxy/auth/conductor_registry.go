package auth

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SetStore swaps the underlying persistence store.
func (m *Manager) SetStore(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
}

// SetRoundTripperProvider register a provider that returns a per-auth RoundTripper.
func (m *Manager) SetRoundTripperProvider(p RoundTripperProvider) {
	m.mu.Lock()
	m.rtProvider = p
	m.mu.Unlock()
}

// RegisterExecutor registers a provider executor with the manager.
func (m *Manager) RegisterExecutor(executor ProviderExecutor) {
	if executor == nil {
		return
	}
	provider := strings.TrimSpace(executor.Identifier())
	if provider == "" {
		return
	}

	var replaced ProviderExecutor
	m.mu.Lock()
	replaced = m.executors[provider]
	m.executors[provider] = executor
	m.mu.Unlock()

	if replaced == nil || replaced == executor {
		return
	}
	if closer, ok := replaced.(ExecutionSessionCloser); ok && closer != nil {
		closer.CloseExecutionSession(CloseAllExecutionSessionsID)
	}
}

// UnregisterExecutor removes the executor associated with the provider key.
func (m *Manager) UnregisterExecutor(provider string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return
	}
	m.mu.Lock()
	delete(m.executors, provider)
	m.mu.Unlock()
}

// Register inserts a new auth entry into the manager.
func (m *Manager) Register(ctx context.Context, auth *Auth) (*Auth, error) {
	if auth == nil {
		return nil, nil
	}
	if auth.ID == "" {
		auth.ID = uuid.NewString()
	}
	auth.EnsureIndex()
	snapshot := auth.Clone()
	m.mu.Lock()
	m.auths[auth.ID] = snapshot
	m.mu.Unlock()
	if err := m.persist(ctx, snapshot); err != nil {
		m.mu.Lock()
		delete(m.auths, auth.ID)
		m.mu.Unlock()
		m.rebuildAPIKeyModelAliasFromRuntimeConfig()
		return nil, err
	}
	m.rebuildAPIKeyModelAliasFromRuntimeConfig()
	m.hook.OnAuthRegistered(ctx, snapshot.Clone())
	return snapshot.Clone(), nil
}

// Update replaces an existing auth entry and notifies hooks.
func (m *Manager) Update(ctx context.Context, auth *Auth) (*Auth, error) {
	if auth == nil || auth.ID == "" {
		return nil, nil
	}
	var previous *Auth
	m.mu.Lock()
	if existing, ok := m.auths[auth.ID]; ok && existing != nil && !auth.indexAssigned && auth.Index == "" {
		auth.Index = existing.Index
		auth.indexAssigned = existing.indexAssigned
	}
	if existing, ok := m.auths[auth.ID]; ok && existing != nil {
		previous = existing.Clone()
	}
	auth.EnsureIndex()
	snapshot := auth.Clone()
	if previous != nil {
		preserveRuntimeFields(snapshot, previous)
		preserveAvailabilityRuntimeForUpdate(snapshot, previous, time.Now())
	}
	m.auths[auth.ID] = snapshot
	m.mu.Unlock()
	if err := m.persist(ctx, snapshot); err != nil {
		m.mu.Lock()
		if previous != nil {
			m.auths[auth.ID] = previous
		} else {
			delete(m.auths, auth.ID)
		}
		m.mu.Unlock()
		m.rebuildAPIKeyModelAliasFromRuntimeConfig()
		return nil, err
	}
	m.rebuildAPIKeyModelAliasFromRuntimeConfig()
	m.hook.OnAuthUpdated(ctx, snapshot.Clone())
	return snapshot.Clone(), nil
}

// Delete removes an auth entry from the runtime manager and persistence store.
func (m *Manager) Delete(ctx context.Context, id string) (*Auth, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	m.mu.Lock()
	previous, ok := m.auths[id]
	if !ok || previous == nil {
		m.mu.Unlock()
		return nil, nil
	}
	snapshot := previous.Clone()
	delete(m.auths, id)
	m.mu.Unlock()

	if err := m.deletePersist(ctx, snapshot); err != nil {
		m.mu.Lock()
		m.auths[id] = snapshot
		m.mu.Unlock()
		m.rebuildAPIKeyModelAliasFromRuntimeConfig()
		return nil, err
	}
	m.rebuildAPIKeyModelAliasFromRuntimeConfig()
	return snapshot.Clone(), nil
}

// Load resets manager state from the backing store.
func (m *Manager) Load(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items, err := m.loadPersistedAuths(ctx)
	if err != nil {
		return err
	}
	m.auths = make(map[string]*Auth, len(items))
	for _, auth := range items {
		if auth == nil || auth.ID == "" {
			continue
		}
		auth.EnsureIndex()
		m.auths[auth.ID] = auth.Clone()
	}
	cfg := m.currentRuntimeConfig()
	m.rebuildAPIKeyModelAliasLocked(cfg)
	return nil
}
