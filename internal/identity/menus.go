package identity

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const MenuManagementCode = "system.menus"

type Menu struct {
	Code            string `json:"code"`
	ParentCode      string `json:"parent_code"`
	Type            string `json:"type"`
	Path            string `json:"path"`
	LabelKey        string `json:"label_key"`
	Icon            string `json:"icon"`
	PermissionCode  string `json:"permission_code"`
	SortOrder       int    `json:"sort_order"`
	Visible         bool   `json:"visible"`
	Enabled         bool   `json:"enabled"`
	SystemProtected bool   `json:"system_protected"`
	Version         int64  `json:"version"`
}

type MenuSeed struct {
	Code           string
	ParentCode     string
	Type           string
	Path           string
	LabelKey       string
	Icon           string
	PermissionCode string
	SortOrder      int
}

var MenuCatalog = []MenuSeed{
	{Code: "dashboard", Type: "menu", Path: "/dashboard", LabelKey: "shell.nav_dashboard", Icon: "layout-dashboard", PermissionCode: "dashboard.read", SortOrder: 10},
	{Code: "group.runtime", Type: "directory", LabelKey: "shell.nav_group_runtime", Icon: "activity", SortOrder: 20},
	{Code: "group.access", Type: "directory", LabelKey: "shell.nav_group_access", Icon: "bot", SortOrder: 30},
	{Code: "group.models", Type: "directory", LabelKey: "shell.nav_group_models", Icon: "layers", SortOrder: 40},
	{Code: "group.governance", Type: "directory", LabelKey: "shell.nav_group_governance", Icon: "users-round", SortOrder: 50},
	{Code: "group.system", Type: "directory", LabelKey: "shell.nav_group_system", Icon: "settings", SortOrder: 60},
	{Code: "runtime.monitor", ParentCode: "group.runtime", Type: "menu", Path: "/monitor", LabelKey: "shell.nav_monitor", Icon: "activity", PermissionCode: "monitor.read", SortOrder: 10},
	{Code: "runtime.request-logs", ParentCode: "group.runtime", Type: "menu", Path: "/monitor/request-logs", LabelKey: "shell.nav_request_logs", Icon: "scroll-text", PermissionCode: "request_logs.read", SortOrder: 20},
	{Code: "runtime.logs", ParentCode: "group.runtime", Type: "menu", Path: "/logs", LabelKey: "shell.nav_logs", Icon: "file-text", PermissionCode: "system.logs.read", SortOrder: 30},
	{Code: "runtime.system", ParentCode: "group.runtime", Type: "menu", Path: "/system", LabelKey: "shell.nav_system", Icon: "info", PermissionCode: "system.status.read", SortOrder: 40},
	{Code: "access.providers", ParentCode: "group.access", Type: "menu", Path: "/ai-providers", LabelKey: "shell.nav_ai_providers", Icon: "bot", PermissionCode: "providers.read", SortOrder: 10},
	{Code: "access.api-keys", ParentCode: "group.access", Type: "menu", Path: "/api-keys", LabelKey: "shell.nav_api_keys", Icon: "sparkles", PermissionCode: "api_keys.read", SortOrder: 20},
	{Code: "access.ccswitch", ParentCode: "group.access", Type: "menu", Path: "/ccswitch-import-settings", LabelKey: "shell.nav_ccswitch_import_settings", Icon: "arrow-down-to-line", PermissionCode: "system.config.read", SortOrder: 30},
	{Code: "models.catalog", ParentCode: "group.models", Type: "menu", Path: "/models", LabelKey: "shell.nav_models", Icon: "cpu", PermissionCode: "models.read", SortOrder: 10},
	{Code: "models.image-generation", ParentCode: "group.models", Type: "menu", Path: "/image-generation", LabelKey: "shell.nav_image_generation", Icon: "image", PermissionCode: "system.config.read", SortOrder: 20},
	{Code: "models.channel-groups", ParentCode: "group.models", Type: "menu", Path: "/channel-groups", LabelKey: "shell.nav_channel_groups", Icon: "layers", PermissionCode: "routing.read", SortOrder: 30},
	{Code: "models.proxies", ParentCode: "group.models", Type: "menu", Path: "/proxies", LabelKey: "shell.nav_proxies", Icon: "network", PermissionCode: "proxies.read", SortOrder: 40},
	{Code: "governance.tenants", ParentCode: "group.governance", Type: "menu", Path: "/tenants", LabelKey: "shell.nav_tenants", Icon: "building-2", PermissionCode: "platform.tenants.read", SortOrder: 10},
	{Code: "governance.users", ParentCode: "group.governance", Type: "menu", Path: "/users", LabelKey: "shell.nav_users", Icon: "user-round", PermissionCode: "tenant.users.read", SortOrder: 20},
	{Code: "governance.roles", ParentCode: "group.governance", Type: "menu", Path: "/roles", LabelKey: "shell.nav_roles", Icon: "shield-check", PermissionCode: "tenant.roles.read", SortOrder: 30},
	{Code: "governance.audit", ParentCode: "group.governance", Type: "menu", Path: "/audit-logs", LabelKey: "shell.nav_audit_logs", Icon: "file-text", PermissionCode: "tenant.audit.read", SortOrder: 40},
	{Code: "system.account-security", ParentCode: "group.system", Type: "menu", Path: "/account-security", LabelKey: "shell.nav_account_security", Icon: "shield-check", PermissionCode: "auth_files.read", SortOrder: 10},
	{Code: "system.api-key-permissions", ParentCode: "group.system", Type: "menu", Path: "/api-key-permissions", LabelKey: "shell.nav_api_key_permissions", Icon: "shield-check", PermissionCode: "api_key_profiles.read", SortOrder: 20},
	{Code: "system.config", ParentCode: "group.system", Type: "menu", Path: "/config", LabelKey: "shell.nav_config", Icon: "settings", PermissionCode: "system.config.read", SortOrder: 30},
	{Code: MenuManagementCode, ParentCode: "group.system", Type: "menu", Path: "/menu-management", LabelKey: "shell.nav_menu_management", Icon: "menu", PermissionCode: "platform.menus.read", SortOrder: 40},
}

func seedMenus(ctx context.Context, tx *sql.Tx) error {
	for _, menu := range MenuCatalog {
		var parent any
		if menu.ParentCode != "" {
			parent = menu.ParentCode
		}
		var permission any
		if menu.PermissionCode != "" {
			permission = menu.PermissionCode
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO menus (code,parent_code,menu_type,path,label_key,icon,permission_code,sort_order,system_protected)
			VALUES (?,?,?,?,?,?,?,?,true)
			ON CONFLICT (code) DO UPDATE SET
			  parent_code=EXCLUDED.parent_code, menu_type=EXCLUDED.menu_type, path=EXCLUDED.path,
			  label_key=EXCLUDED.label_key, icon=EXCLUDED.icon, permission_code=EXCLUDED.permission_code,
			  system_protected=true, updated_at=now()
		`, menu.Code, parent, menu.Type, menu.Path, menu.LabelKey, menu.Icon, permission, menu.SortOrder); err != nil {
			return fmt.Errorf("identity: seed menu %s: %w", menu.Code, err)
		}
	}
	return nil
}

func (s *Service) ListMenus(ctx context.Context) ([]Menu, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code,parent_code,menu_type,path,label_key,icon,permission_code,sort_order,visible,enabled,system_protected,version FROM menus ORDER BY sort_order,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	menus := make([]Menu, 0)
	for rows.Next() {
		var menu Menu
		var parent, permission sql.NullString
		if err = rows.Scan(&menu.Code, &parent, &menu.Type, &menu.Path, &menu.LabelKey, &menu.Icon, &permission, &menu.SortOrder, &menu.Visible, &menu.Enabled, &menu.SystemProtected, &menu.Version); err != nil {
			return nil, err
		}
		if parent.Valid {
			menu.ParentCode = parent.String
		}
		if permission.Valid {
			menu.PermissionCode = permission.String
		}
		menus = append(menus, menu)
	}
	return menus, rows.Err()
}

func (s *Service) ListPrincipalMenus(ctx context.Context, principal Principal) ([]Menu, error) {
	menus, err := s.ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]Menu, len(menus))
	included := make(map[string]bool, len(menus))
	for _, menu := range menus {
		byCode[menu.Code] = menu
		if menu.Type == "menu" && (menu.PermissionCode == "" || principal.Has(menu.PermissionCode)) {
			included[menu.Code] = true
		}
	}
	for code := range included {
		for parent := byCode[code].ParentCode; parent != ""; parent = byCode[parent].ParentCode {
			included[parent] = true
		}
	}
	result := make([]Menu, 0, len(included))
	for _, menu := range menus {
		if included[menu.Code] {
			result = append(result, menu)
		}
	}
	return result, nil
}

func (s *Service) UpdateMenu(ctx context.Context, actor Principal, code string, visible, enabled bool, sortOrder int, version int64) (Menu, error) {
	if !actor.Has("platform.menus.update") {
		return Menu{}, ErrPermissionDenied
	}
	code = strings.TrimSpace(code)
	if code == "" || sortOrder < 0 || sortOrder > 10000 {
		return Menu{}, fmt.Errorf("%w: invalid menu input", ErrValidation)
	}
	if (code == MenuManagementCode || code == "group.system") && (!visible || !enabled) {
		return Menu{}, ErrProtectedResource
	}
	res, err := s.db.ExecContext(ctx, `UPDATE menus SET visible=?,enabled=?,sort_order=?,updated_at=now(),version=version+1 WHERE code=? AND version=?`, visible, enabled, sortOrder, code, version)
	if err != nil {
		return Menu{}, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return Menu{}, ErrVersionConflict
	}
	menus, err := s.ListMenus(ctx)
	if err != nil {
		return Menu{}, err
	}
	for _, menu := range menus {
		if menu.Code == code {
			s.RecordAudit(ctx, AuditEvent{TenantID: actor.EffectiveTenant.ID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "menu.update", ResourceType: "menu", ResourceID: code, Result: "success"})
			return menu, nil
		}
	}
	return Menu{}, sql.ErrNoRows
}
