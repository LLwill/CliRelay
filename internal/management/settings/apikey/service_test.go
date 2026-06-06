package apikey

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "apikey-settings-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	t.Cleanup(func() {
		usage.CloseDB()
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
}

func TestReplaceKeysNormalizesAndListsEnabledKeys(t *testing.T) {
	setupTestDB(t)
	svc := NewService(nil)

	if err := svc.ReplaceKeys([]string{" sk-one ", "", "sk-two"}); err != nil {
		t.Fatalf("ReplaceKeys() error = %v, want nil", err)
	}
	if err := usage.UpsertAPIKey(usage.APIKeyRow{Key: "sk-disabled", Disabled: true}); err != nil {
		t.Fatalf("UpsertAPIKey(disabled): %v", err)
	}

	if got := svc.EnabledKeys(); !reflect.DeepEqual(got, []string{"sk-one", "sk-two"}) {
		t.Fatalf("EnabledKeys() = %#v, want sk-one/sk-two", got)
	}
}

func TestPatchAndDeleteKey(t *testing.T) {
	setupTestDB(t)
	svc := NewService(nil)

	if err := svc.PatchKey("", " sk-created "); err != nil {
		t.Fatalf("PatchKey(create) error = %v, want nil", err)
	}
	if got := usage.GetAPIKey("sk-created"); got == nil {
		t.Fatal("PatchKey(create) did not persist new key")
	}
	if err := svc.PatchKey(" sk-created ", " sk-renamed "); err != nil {
		t.Fatalf("PatchKey(rename) error = %v, want nil", err)
	}
	if got := usage.GetAPIKey("sk-created"); got != nil {
		t.Fatal("PatchKey(rename) kept old key")
	}
	if got := usage.GetAPIKey("sk-renamed"); got == nil {
		t.Fatal("PatchKey(rename) did not persist new key")
	}
	if err := svc.DeleteKey(" sk-renamed "); err != nil {
		t.Fatalf("DeleteKey() error = %v, want nil", err)
	}
	if got := usage.GetAPIKey("sk-renamed"); got != nil {
		t.Fatal("DeleteKey() kept deleted key")
	}
	if err := svc.DeleteKey(" "); !errors.Is(err, ErrMissingValue) {
		t.Fatalf("DeleteKey(blank) error = %v, want ErrMissingValue", err)
	}
}

func TestReplacePermissionProfilesValidatesAndSanitizes(t *testing.T) {
	setupTestDB(t)
	svc := NewService(func(channels []string) ([]string, error) {
		out := make([]string, 0, len(channels))
		for _, channel := range channels {
			if channel == "drop" {
				continue
			}
			out = append(out, channel)
		}
		return out, nil
	})

	err := svc.ReplacePermissionProfiles([]usage.APIKeyPermissionProfileRow{{
		ID:              " standard ",
		Name:            " Standard ",
		AllowedChannels: []string{"keep", "drop"},
	}})
	if err != nil {
		t.Fatalf("ReplacePermissionProfiles() error = %v, want nil", err)
	}

	got := svc.PermissionProfiles()
	if len(got) != 1 {
		t.Fatalf("PermissionProfiles() len = %d, want 1", len(got))
	}
	if got[0].ID != "standard" || got[0].Name != "Standard" {
		t.Fatalf("profile identity = %#v, want trimmed values", got[0])
	}
	if !reflect.DeepEqual(got[0].AllowedChannels, []string{"keep"}) {
		t.Fatalf("AllowedChannels = %#v, want keep", got[0].AllowedChannels)
	}
}

func TestReplacePermissionProfilesRejectsMissingIdentity(t *testing.T) {
	setupTestDB(t)
	svc := NewService(nil)

	if err := svc.ReplacePermissionProfiles([]usage.APIKeyPermissionProfileRow{{Name: "Name"}}); !errors.Is(err, ErrInvalidProfileID) {
		t.Fatalf("missing id error = %v, want ErrInvalidProfileID", err)
	}
	if err := svc.ReplacePermissionProfiles([]usage.APIKeyPermissionProfileRow{{ID: "standard"}}); !errors.Is(err, ErrInvalidProfileName) {
		t.Fatalf("missing name error = %v, want ErrInvalidProfileName", err)
	}
}
