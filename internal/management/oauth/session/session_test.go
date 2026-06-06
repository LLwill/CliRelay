package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompleteProviderRetainsCompletedSessions(t *testing.T) {
	store := NewStore(time.Minute)
	store.Register("completed-state", "codex")
	store.Register("stale-state", "codex")

	store.Complete("completed-state")
	removed := store.CompleteProvider("codex")
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	session, ok := store.Get("completed-state")
	if !ok {
		t.Fatal("expected completed session to remain queryable")
	}
	if session.Provider != "codex" || session.Status != StatusCompleted {
		t.Fatalf("session = %#v, want completed codex session", session)
	}
	if _, ok := store.Get("stale-state"); ok {
		t.Fatal("expected stale pending session to be removed")
	}
}

func TestValidateStateRejectsPathLikeValues(t *testing.T) {
	for _, state := range []string{"../state", "path/state", `path\state`, "bad state"} {
		if err := ValidateState(state); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("ValidateState(%q) error = %v, want ErrInvalidState", state, err)
		}
	}
}

func TestWriteCallbackFileForPendingWritesPayload(t *testing.T) {
	store := NewStore(time.Minute)
	store.Register("session-1", "gemini")
	authDir := t.TempDir()

	path, err := store.WriteCallbackFileForPending(authDir, "gemini", "session-1", " code ", " error ")
	if err != nil {
		t.Fatalf("WriteCallbackFileForPending() error = %v", err)
	}
	if filepath.Base(path) != ".oauth-gemini-session-1.oauth" {
		t.Fatalf("path = %q, want gemini callback file", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var payload callbackFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Code != "code" || payload.State != "session-1" || payload.Error != "error" {
		t.Fatalf("payload = %#v, want trimmed callback payload", payload)
	}
}

func TestWriteCallbackFileForPendingRejectsCompletedSession(t *testing.T) {
	store := NewStore(time.Minute)
	store.Register("session-1", "codex")
	store.Complete("session-1")

	_, err := store.WriteCallbackFileForPending(t.TempDir(), "codex", "session-1", "code", "")
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("WriteCallbackFileForPending() error = %v, want ErrNotPending", err)
	}
}
