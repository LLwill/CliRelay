package identity

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeUsername(t *testing.T) {
	if got := NormalizeUsername("  Admin.User  "); got != "admin.user" {
		t.Fatalf("NormalizeUsername() = %q", got)
	}
}

func TestHashPasswordPolicyAndVerification(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil || hash == "" {
		t.Fatalf("HashPassword() hash=%q err=%v", hash, err)
	}
}

func TestValidateTenant(t *testing.T) {
	now := time.Now().UTC()
	active := Tenant{Type: "standard", Status: "active", ExpiresAt: ptrTime(now.Add(time.Hour))}
	if err := validateTenant(active, now); err != nil {
		t.Fatalf("active tenant rejected: %v", err)
	}
	expired := active
	expired.ExpiresAt = ptrTime(now)
	if !errors.Is(validateTenant(expired, now), ErrTenantExpired) {
		t.Fatal("expected expired tenant")
	}
	suspended := active
	suspended.Status = "suspended"
	if !errors.Is(validateTenant(suspended, now), ErrTenantSuspended) {
		t.Fatal("expected suspended tenant")
	}
	if err := validateTenant(Tenant{Type: "system", Status: "active"}, now); err != nil {
		t.Fatalf("system tenant rejected: %v", err)
	}
}

func TestRandomTokenHashesOnlyStableToken(t *testing.T) {
	token, hash, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || hash == "" || token == hash {
		t.Fatalf("token/hash invalid token=%q hash=%q", token, hash)
	}
	if got := tokenHash(token); got != hash {
		t.Fatalf("tokenHash() = %q, want %q", got, hash)
	}
}

func TestEnsureActorTenantScope(t *testing.T) {
	actor := Principal{EffectiveTenant: Tenant{ID: "tenant-a"}}
	if err := ensureActorTenantScope(actor, "tenant-a"); err != nil {
		t.Fatalf("current tenant rejected: %v", err)
	}
	if err := ensureActorTenantScope(actor, "tenant-b"); !errors.Is(err, ErrTenantScope) {
		t.Fatalf("cross-tenant scope error = %v", err)
	}
	actor.PlatformAdmin = true
	if err := ensureActorTenantScope(actor, "tenant-b"); err != nil {
		t.Fatalf("platform admin tenant scope rejected: %v", err)
	}
}

func TestMenuCatalogReferencesExistingParents(t *testing.T) {
	seen := make(map[string]MenuSeed, len(MenuCatalog))
	for _, menu := range MenuCatalog {
		if menu.Code == "" {
			t.Fatalf("invalid menu code %q", menu.Code)
		}
		if _, ok := seen[menu.Code]; ok {
			t.Fatalf("duplicate menu code %q", menu.Code)
		}
		if menu.ParentCode != "" {
			parent, ok := seen[menu.ParentCode]
			if !ok {
				t.Fatalf("menu %s references parent %s before it is declared", menu.Code, menu.ParentCode)
			}
			// Nested menus under directories must form secondary routes under the parent path.
			if parent.Type == "directory" && parent.Path != "" && menu.Type == "menu" {
				prefix := strings.TrimRight(parent.Path, "/")
				if menu.Path != prefix && !strings.HasPrefix(menu.Path, prefix+"/") {
					t.Fatalf("menu %s path %q is not nested under parent path %q", menu.Code, menu.Path, prefix)
				}
			}
		}
		if menu.Type == "directory" {
			if menu.Path == "" {
				t.Fatalf("directory %s missing path prefix", menu.Code)
			}
			if menu.Component == "" {
				t.Fatalf("directory %s missing component", menu.Code)
			}
		}
		if menu.Type == "menu" && menu.Path == "" {
			t.Fatalf("menu %s missing path", menu.Code)
		}
		seen[menu.Code] = menu
	}
	if _, ok := seen[MenuManagementCode]; !ok {
		t.Fatal("menu management entry is missing")
	}
	// CC Switch import configs are tenant-scoped (routing.* API auth). Ordinary tenant
	// admins have routing.read but not platform system.config.read.
	if got := seen["access.ccswitch"].PermissionCode; got != "routing.read" {
		t.Fatalf("access.ccswitch permission = %q, want routing.read so tenant admins can see it", got)
	}
	// System info is a top-level leaf (not under 运行观测), pinned after all groups.
	systemInfo, ok := seen["runtime.system"]
	if !ok {
		t.Fatal("runtime.system menu is missing")
	}
	if systemInfo.ParentCode != "" {
		t.Fatalf("runtime.system parent = %q, want empty top-level parent", systemInfo.ParentCode)
	}
	if systemInfo.SortOrder < seen["group.system"].SortOrder {
		t.Fatalf("runtime.system sort_order %d must be after group.system %d", systemInfo.SortOrder, seen["group.system"].SortOrder)
	}
	// Model plaza is a tenant-facing available-models surface under models group.
	plaza, ok := seen["models.plaza"]
	if !ok {
		t.Fatal("models.plaza menu is missing")
	}
	if plaza.ParentCode != "group.models" {
		t.Fatalf("models.plaza parent = %q, want group.models", plaza.ParentCode)
	}
	if plaza.Path != "/models/plaza" {
		t.Fatalf("models.plaza path = %q, want /models/plaza", plaza.Path)
	}
	if plaza.Component != "model-plaza" {
		t.Fatalf("models.plaza component = %q, want model-plaza", plaza.Component)
	}
	if plaza.PermissionCode != "system.status.read" {
		t.Fatalf("models.plaza permission = %q, want system.status.read", plaza.PermissionCode)
	}
}

func TestGeneratedIdentifier(t *testing.T) {
	first := generatedIdentifier("tenant-")
	second := generatedIdentifier("tenant-")
	if !strings.HasPrefix(first, "tenant-") || len(first) != len("tenant-")+32 || first == second {
		t.Fatalf("generated identifiers first=%q second=%q", first, second)
	}
}

func TestSystemLogsDeletePermissionCatalog(t *testing.T) {
	var found *PermissionSeed
	for i := range PermissionCatalog {
		if PermissionCatalog[i].Code == "system.logs.delete" {
			found = &PermissionCatalog[i]
			break
		}
	}
	if found == nil {
		t.Fatal("system.logs.delete missing from PermissionCatalog")
	}
	if found.Scope != "platform" || found.Resource != "system_logs" || found.Action != "delete" || !found.Sensitive {
		t.Fatalf("system.logs.delete seed = %+v, want platform/system_logs/delete/sensitive", *found)
	}
}

func TestMenuCodeForPermission(t *testing.T) {
	menuCodes := make(map[string]bool, len(MenuCatalog))
	for _, menu := range MenuCatalog {
		menuCodes[menu.Code] = true
	}
	tests := map[string]string{
		"tenant.users.update":   "governance.users",
		"request_logs.delete":   "runtime.request-logs",
		"system.logs.delete":    "runtime.logs",
		"platform.menus.update": MenuManagementCode,
		"proxies.test":          "models.proxies",
	}
	for _, permission := range PermissionCatalog {
		got := menuCodeForPermission(permission)
		if got == "" {
			t.Errorf("permission %s has no menu mapping", permission.Code)
		} else if !menuCodes[got] {
			t.Errorf("permission %s references unknown menu %s", permission.Code, got)
		}
		want, ok := tests[permission.Code]
		if !ok {
			continue
		}
		if got != want {
			t.Errorf("menuCodeForPermission(%s)=%q want %q", permission.Code, got, want)
		}
		delete(tests, permission.Code)
	}
	if len(tests) != 0 {
		t.Fatalf("permissions missing from catalog: %v", tests)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
