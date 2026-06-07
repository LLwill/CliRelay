package serviceapp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type lifecycleServerStub struct {
	err error
}

func (s lifecycleServerStub) Start() error {
	return s.err
}

func TestEnsureAuthDirCreatesMissingDirectory(t *testing.T) {
	authDir := filepath.Join(t.TempDir(), "auth")

	if err := EnsureAuthDir(authDir); err != nil {
		t.Fatalf("EnsureAuthDir returned error: %v", err)
	}

	info, err := os.Stat(authDir)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected created auth path to be a directory")
	}
}

func TestEnsureAuthDirRejectsRegularFile(t *testing.T) {
	root := t.TempDir()
	authFile := filepath.Join(root, "auth")
	if err := os.WriteFile(authFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := EnsureAuthDir(authFile); err == nil {
		t.Fatal("expected error when auth path is a file")
	}
}

func TestStartServerLoopReturnsServerError(t *testing.T) {
	wantErr := errors.New("boom")
	serverErr := StartServerLoop(lifecycleServerStub{err: wantErr})
	if serverErr == nil {
		t.Fatal("expected server error channel")
	}
	if got := <-serverErr; !errors.Is(got, wantErr) {
		t.Fatalf("StartServerLoop error = %v, want %v", got, wantErr)
	}
}
