package management

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPermissionForManagementRequest(t *testing.T) {
	tests := []struct{ method, path, want string }{
		{http.MethodGet, "/v0/management/tenants", "platform.tenants.read"},
		{http.MethodPost, "/v0/management/tenants", "platform.tenants.create"},
		{http.MethodGet, "/v0/management/users", "tenant.users.read"},
		{http.MethodPut, "/v0/management/users/u/roles", "tenant.users.assign_roles"},
		{http.MethodPut, "/v0/management/roles/r/users", "tenant.users.assign_roles"},
		{http.MethodGet, "/v0/management/menus", "platform.menus.read"},
		{http.MethodPatch, "/v0/management/menus/system.config", "platform.menus.update"},
		{http.MethodDelete, "/v0/management/usage/logs", "request_logs.delete"},
		{http.MethodGet, "/v0/management/usage/logs/1/content", "request_logs.content.read"},
		{http.MethodGet, "/v0/management/get-auth-status", "auth_files.oauth"},
		{http.MethodPost, "/v0/management/proxy-pool/check", "proxies.test"},
		{http.MethodPut, "/v0/management/config.yaml", "system.config.write"},
	}
	for _, test := range tests {
		if got := permissionForManagementRequest(test.method, test.path); got != test.want {
			t.Errorf("permissionForManagementRequest(%s,%s)=%q want %q", test.method, test.path, got, test.want)
		}
	}
}

func TestServiceCredentialCannotAccessTenantGovernance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, "", nil)
	h.SetLocalPassword("management-key")
	t.Cleanup(h.Close)

	router := gin.New()
	router.Use(h.Middleware())
	reached := false
	router.GET("/v0/management/tenants", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/management/tenants", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer management-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if reached {
		t.Fatal("service credential reached tenant governance handler")
	}
}
