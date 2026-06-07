package auth

import (
	"context"
	"sync/atomic"
	"testing"
)

type countingStore struct {
	saveCount   atomic.Int32
	deleteCount atomic.Int32
}

type snapshotStore struct {
	entered       chan struct{}
	release       chan struct{}
	observedToken atomic.Value
}

func (s *countingStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *countingStore) Save(context.Context, *Auth) (string, error) {
	s.saveCount.Add(1)
	return "", nil
}

func (s *countingStore) Delete(context.Context, string) error {
	s.deleteCount.Add(1)
	return nil
}

func (s *snapshotStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *snapshotStore) Save(_ context.Context, auth *Auth) (string, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	token := ""
	if auth != nil && auth.Metadata != nil {
		if raw, ok := auth.Metadata["access_token"].(string); ok {
			token = raw
		}
	}
	s.observedToken.Store(token)
	return "", nil
}

func (s *snapshotStore) Delete(context.Context, string) error { return nil }

func TestWithSkipPersist_DisablesUpdatePersistence(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Update(context.Background(), auth); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 1 {
		t.Fatalf("expected 1 Save call, got %d", got)
	}

	ctxSkip := WithSkipPersist(context.Background())
	if _, err := mgr.Update(ctxSkip, auth); err != nil {
		t.Fatalf("Update(skipPersist) returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 1 {
		t.Fatalf("expected Save call count to remain 1, got %d", got)
	}
}

func TestWithSkipPersist_DisablesRegisterPersistence(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register(skipPersist) returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 0 {
		t.Fatalf("expected 0 Save calls, got %d", got)
	}
}

func TestDeleteRemovesAuthAndPersistsDelete(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register(skipPersist) returned error: %v", err)
	}

	deleted, err := mgr.Delete(context.Background(), "auth-1")
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if deleted == nil || deleted.ID != "auth-1" {
		t.Fatalf("deleted auth = %+v, want auth-1", deleted)
	}
	if _, ok := mgr.GetByID("auth-1"); ok {
		t.Fatal("expected auth to be removed from manager")
	}
	if got := store.deleteCount.Load(); got != 1 {
		t.Fatalf("expected 1 Delete call, got %d", got)
	}
}

func TestDeleteWithSkipPersistRemovesAuthWithoutDeletingStore(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register(skipPersist) returned error: %v", err)
	}

	if _, err := mgr.Delete(WithSkipPersist(context.Background()), "auth-1"); err != nil {
		t.Fatalf("Delete(skipPersist) returned error: %v", err)
	}
	if _, ok := mgr.GetByID("auth-1"); ok {
		t.Fatal("expected auth to be removed from manager")
	}
	if got := store.deleteCount.Load(); got != 0 {
		t.Fatalf("expected no Delete calls, got %d", got)
	}
}

func TestRuntimeOnlyAuthSkipsPersistence(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"type": "antigravity"},
		Attributes: map[string]string{
			"runtime_only": "true",
		},
	}

	if _, err := mgr.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 0 {
		t.Fatalf("expected runtime_only auth to skip Save, got %d", got)
	}

	if _, err := mgr.Delete(context.Background(), "auth-1"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if got := store.deleteCount.Load(); got != 0 {
		t.Fatalf("expected runtime_only auth to skip Delete, got %d", got)
	}
}

func TestAuthWithoutMetadataSkipsPersistence(t *testing.T) {
	store := &countingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
	}

	if _, err := mgr.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if got := store.saveCount.Load(); got != 0 {
		t.Fatalf("expected auth without metadata to skip Save, got %d", got)
	}

	if _, err := mgr.Delete(context.Background(), "auth-1"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if got := store.deleteCount.Load(); got != 0 {
		t.Fatalf("expected auth without metadata to skip Delete, got %d", got)
	}
}

func TestRegister_PersistsSnapshotInsteadOfCallerPointer(t *testing.T) {
	store := &snapshotStore{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"access_token": "initial"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := mgr.Register(context.Background(), auth); err != nil {
			t.Errorf("Register returned error: %v", err)
		}
	}()

	<-store.entered
	auth.Metadata["access_token"] = "mutated"
	close(store.release)
	<-done

	if got, _ := store.observedToken.Load().(string); got != "initial" {
		t.Fatalf("expected persisted token to stay at snapshot value, got %q", got)
	}
}

func TestUpdate_PersistsSnapshotInsteadOfCallerPointer(t *testing.T) {
	store := &snapshotStore{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "antigravity",
		Metadata: map[string]any{"access_token": "initial"},
	}

	if _, err := mgr.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := mgr.Update(context.Background(), auth); err != nil {
			t.Errorf("Update returned error: %v", err)
		}
	}()

	<-store.entered
	auth.Metadata["access_token"] = "mutated"
	close(store.release)
	<-done

	if got, _ := store.observedToken.Load().(string); got != "initial" {
		t.Fatalf("expected persisted token to stay at snapshot value, got %q", got)
	}
}
