package auth

import (
	"context"
	"strings"
)

type persistenceService struct {
	manager *Manager
}

func newPersistenceService(manager *Manager) persistenceService {
	return persistenceService{manager: manager}
}

func (m *Manager) persist(ctx context.Context, auth *Auth) error {
	return newPersistenceService(m).save(ctx, auth)
}

func (m *Manager) deletePersist(ctx context.Context, auth *Auth) error {
	return newPersistenceService(m).delete(ctx, auth)
}

func (m *Manager) loadPersistedAuths(ctx context.Context) ([]*Auth, error) {
	return newPersistenceService(m).list(ctx)
}

func (s persistenceService) save(ctx context.Context, auth *Auth) error {
	if !s.shouldPersist(ctx, auth) {
		return nil
	}
	_, err := s.manager.store.Save(ctx, auth)
	return err
}

func (s persistenceService) delete(ctx context.Context, auth *Auth) error {
	if s.manager == nil || s.manager.store == nil || auth == nil || auth.ID == "" {
		return nil
	}
	if !s.shouldPersist(ctx, auth) {
		return nil
	}
	return s.manager.store.Delete(ctx, auth.ID)
}

func (s persistenceService) list(ctx context.Context) ([]*Auth, error) {
	if s.manager == nil || s.manager.store == nil {
		return nil, nil
	}
	return s.manager.store.List(ctx)
}

func (s persistenceService) shouldPersist(ctx context.Context, auth *Auth) bool {
	if s.manager == nil || s.manager.store == nil || auth == nil {
		return false
	}
	if shouldSkipPersist(ctx) {
		return false
	}
	if auth.Attributes != nil {
		if v := strings.ToLower(strings.TrimSpace(auth.Attributes["runtime_only"])); v == "true" {
			return false
		}
	}
	return auth.Metadata != nil
}
