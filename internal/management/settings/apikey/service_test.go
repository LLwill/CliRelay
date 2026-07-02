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

func TestPatchEntryRenamePreservesStableID(t *testing.T) {
	setupTestDB(t)
	svc := NewService(nil)

	if err := usage.UpsertAPIKey(usage.APIKeyRow{ID: "key-1", Key: "sk-old", Name: "Old"}); err != nil {
		t.Fatalf("UpsertAPIKey(sk-old): %v", err)
	}

	newKey := "sk-new"
	newName := "Renamed"
	if err := svc.PatchEntry(&[]string{"key-1"}[0], nil, nil, EntryPatch{
		Key:  &newKey,
		Name: &newName,
	}); err != nil {
		t.Fatalf("PatchEntry() error = %v", err)
	}

	if got := usage.GetAPIKey("sk-old"); got != nil {
		t.Fatalf("old key should not remain addressable by key, got %#v", got)
	}
	got := usage.GetAPIKey("sk-new")
	if got == nil {
		t.Fatal("expected renamed API key to exist")
	}
	if got.ID != "key-1" {
		t.Fatalf("stable id = %q, want key-1", got.ID)
	}
	if got.Name != "Renamed" {
		t.Fatalf("name = %q, want Renamed", got.Name)
	}
}

func TestPatchEntryUpdatesDailySpendingLimit(t *testing.T) {
	setupTestDB(t)
	svc := NewService(nil)

	if err := usage.UpsertAPIKey(usage.APIKeyRow{ID: "key-1", Key: "sk-cost", DailySpendingLimit: 1}); err != nil {
		t.Fatalf("UpsertAPIKey(sk-cost): %v", err)
	}

	limit := 4.5
	if err := svc.PatchEntry(&[]string{"key-1"}[0], nil, nil, EntryPatch{DailySpendingLimit: &limit}); err != nil {
		t.Fatalf("PatchEntry() error = %v", err)
	}

	got := usage.GetAPIKey("sk-cost")
	if got == nil {
		t.Fatal("expected patched key")
	}
	if got.DailySpendingLimit != limit {
		t.Fatalf("DailySpendingLimit = %v, want %v", got.DailySpendingLimit, limit)
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

func TestRenameAndRemoveChannelRestrictions(t *testing.T) {
	setupTestDB(t)
	svc := NewService(nil)

	if err := usage.UpsertAPIKey(usage.APIKeyRow{
		Key:             "sk-channel",
		AllowedChannels: []string{"Team Old", "Other"},
	}); err != nil {
		t.Fatalf("UpsertAPIKey(sk-channel): %v", err)
	}
	if err := svc.ReplacePermissionProfiles([]usage.APIKeyPermissionProfileRow{{
		ID:              "standard",
		Name:            "Standard",
		AllowedChannels: []string{"Team Old", "Other"},
	}}); err != nil {
		t.Fatalf("ReplacePermissionProfiles() error = %v", err)
	}

	oldNameSet := map[string]struct{}{"team old": {}}
	if err := svc.RenameAllowedChannelRestrictions(oldNameSet, "Team New"); err != nil {
		t.Fatalf("RenameAllowedChannelRestrictions() error = %v", err)
	}
	if err := svc.RenamePermissionProfileChannelRestrictions(oldNameSet, "Team New"); err != nil {
		t.Fatalf("RenamePermissionProfileChannelRestrictions() error = %v", err)
	}

	key := usage.GetAPIKey("sk-channel")
	if key == nil || !reflect.DeepEqual(key.AllowedChannels, []string{"Team New", "Other"}) {
		t.Fatalf("renamed key channels = %#v, want Team New/Other", key)
	}
	profiles := svc.PermissionProfiles()
	if len(profiles) != 1 || !reflect.DeepEqual(profiles[0].AllowedChannels, []string{"Team New", "Other"}) {
		t.Fatalf("renamed profile channels = %#v, want Team New/Other", profiles)
	}

	removeSet := map[string]struct{}{"team new": {}}
	if err := svc.RemoveAllowedChannelRestrictions(removeSet); err != nil {
		t.Fatalf("RemoveAllowedChannelRestrictions() error = %v", err)
	}
	if err := svc.RemovePermissionProfileChannelRestrictions(removeSet); err != nil {
		t.Fatalf("RemovePermissionProfileChannelRestrictions() error = %v", err)
	}

	key = usage.GetAPIKey("sk-channel")
	if key == nil || !reflect.DeepEqual(key.AllowedChannels, []string{"Other"}) {
		t.Fatalf("removed key channels = %#v, want Other", key)
	}
	profiles = svc.PermissionProfiles()
	if len(profiles) != 1 || !reflect.DeepEqual(profiles[0].AllowedChannels, []string{"Other"}) {
		t.Fatalf("removed profile channels = %#v, want Other", profiles)
	}
}

func TestReplaceEntriesSanitizesAndValidates(t *testing.T) {
	setupTestDB(t)
	svc := NewService(
		func(channels []string) ([]string, error) {
			return []string{"known-channel"}, nil
		},
		WithChannelGroupValidator(func(groups []string) ([]string, error) {
			return groups, nil
		}),
		WithEntryValidator(func(entry config.APIKeyEntry) error {
			if !reflect.DeepEqual(entry.AllowedChannelGroups, []string{"pro"}) {
				t.Fatalf("AllowedChannelGroups = %#v, want normalized pro", entry.AllowedChannelGroups)
			}
			if !reflect.DeepEqual(entry.AllowedChannels, []string{"known-channel"}) {
				t.Fatalf("AllowedChannels = %#v, want sanitized channel", entry.AllowedChannels)
			}
			return nil
		}),
	)

	err := svc.ReplaceEntries([]config.APIKeyEntry{{
		Key:                  " sk-entry ",
		Name:                 " Entry ",
		AllowedChannels:      []string{"drop-me"},
		AllowedChannelGroups: []string{" PRO ", "pro"},
	}})
	if err != nil {
		t.Fatalf("ReplaceEntries() error = %v, want nil", err)
	}

	got := usage.GetAPIKey("sk-entry")
	if got == nil {
		t.Fatal("expected API key entry after replace")
	}
	if !reflect.DeepEqual(got.AllowedChannels, []string{"known-channel"}) {
		t.Fatalf("stored AllowedChannels = %#v, want sanitized channel", got.AllowedChannels)
	}
	if !reflect.DeepEqual(got.AllowedChannelGroups, []string{"pro"}) {
		t.Fatalf("stored AllowedChannelGroups = %#v, want normalized pro", got.AllowedChannelGroups)
	}
}

func TestPatchEntryValidationFailureKeepsOriginalKey(t *testing.T) {
	setupTestDB(t)
	svc := NewService(
		nil,
		WithEntryValidator(func(entry config.APIKeyEntry) error {
			return errors.New("invalid restriction")
		}),
	)

	if err := usage.UpsertAPIKey(usage.APIKeyRow{Key: "sk-old", Name: "Old"}); err != nil {
		t.Fatalf("UpsertAPIKey(sk-old): %v", err)
	}

	newKey := "sk-new"
	name := "Renamed"
	err := svc.PatchEntry(nil, nil, &[]string{"sk-old"}[0], EntryPatch{
		Key:  &newKey,
		Name: &name,
	})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("PatchEntry() error = %v, want ErrInvalidEntry", err)
	}
	if got := usage.GetAPIKey("sk-old"); got == nil || got.Name != "Old" {
		t.Fatalf("original key changed unexpectedly: %#v", got)
	}
	if got := usage.GetAPIKey("sk-new"); got != nil {
		t.Fatalf("new key should not exist after failed patch: %#v", got)
	}
}

func TestDeleteEntryByIndexDeletesLogsWhenRequested(t *testing.T) {
	setupTestDB(t)
	deletedKeys := make([]string, 0, 1)
	svc := NewService(
		nil,
		WithLogsDeleter(func(key string) (int64, error) {
			deletedKeys = append(deletedKeys, key)
			return 3, nil
		}),
	)

	if err := usage.UpsertAPIKey(usage.APIKeyRow{Key: "sk-delete"}); err != nil {
		t.Fatalf("UpsertAPIKey(sk-delete): %v", err)
	}

	result, err := svc.DeleteEntry("", nil, &[]int{0}[0], true)
	if err != nil {
		t.Fatalf("DeleteEntry() error = %v, want nil", err)
	}
	if result.LogsDeleted != 3 {
		t.Fatalf("LogsDeleted = %d, want 3", result.LogsDeleted)
	}
	if !reflect.DeepEqual(deletedKeys, []string{"sk-delete"}) {
		t.Fatalf("deletedKeys = %#v, want sk-delete", deletedKeys)
	}
	if got := usage.GetAPIKey("sk-delete"); got != nil {
		t.Fatalf("DeleteEntry() kept deleted key: %#v", got)
	}
}
