package identity

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

type AuditLog struct {
	ID           int64     `json:"id"`
	TenantID     *string   `json:"tenant_id"`
	ActorKind    string    `json:"actor_kind"`
	ActorUserID  *string   `json:"actor_user_id"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Result       string    `json:"result"`
	RequestID    string    `json:"request_id"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Service) RecordAudit(ctx context.Context, event AuditEvent) {
	if s == nil || s.db == nil {
		return
	}
	var tenantID, actorUserID, actorSessionID any
	if event.TenantID != "" {
		tenantID = event.TenantID
	}
	if event.ActorUserID != "" {
		actorUserID = event.ActorUserID
	}
	if event.ActorSessionID != "" {
		actorSessionID = event.ActorSessionID
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(auditCtx, `INSERT INTO audit_logs (tenant_id,actor_kind,actor_user_id,actor_session_id,action,resource_type,resource_id,result,request_id) VALUES (?,?,?,?,?,?,?,?,?)`, tenantID, event.ActorKind, actorUserID, actorSessionID, event.Action, event.ResourceType, event.ResourceID, event.Result, event.RequestID); err != nil {
		log.WithError(err).WithFields(log.Fields{"action": event.Action, "resource_type": event.ResourceType, "resource_id": event.ResourceID}).Error("identity: record audit event")
	}
}

func (s *Service) ListAuditLogs(ctx context.Context, tenantID string, platform bool) ([]AuditLog, error) {
	query := `SELECT id,tenant_id,actor_kind,actor_user_id,action,resource_type,resource_id,result,request_id,created_at FROM audit_logs`
	args := []any{}
	if !platform {
		query += ` WHERE tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AuditLog
	for rows.Next() {
		var item AuditLog
		var tenant, actor sql.NullString
		if err = rows.Scan(&item.ID, &tenant, &item.ActorKind, &actor, &item.Action, &item.ResourceType, &item.ResourceID, &item.Result, &item.RequestID, &item.CreatedAt); err != nil {
			return nil, err
		}
		if tenant.Valid {
			item.TenantID = &tenant.String
		}
		if actor.Valid {
			item.ActorUserID = &actor.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) AssignUserRoles(ctx context.Context, actor Principal, tenantID, userID string, roleIDs []string) error {
	if !actor.Has("tenant.users.assign_roles") && !actor.Has("platform.users.manage") {
		return ErrPermissionDenied
	}
	if userID == SystemUserID {
		return ErrProtectedResource
	}
	if err := ensureActorTenantScope(actor, tenantID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var userTenant string
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id FROM users WHERE id=? FOR UPDATE`, userID).Scan(&userTenant); err != nil {
		return err
	}
	if userTenant != tenantID {
		return ErrTenantScope
	}
	if err = ensureRolesDelegable(ctx, tx, actor, tenantID, roleIDs); err != nil {
		return err
	}
	keepsAdmin := false
	for _, roleID := range roleIDs {
		var protected bool
		if err = tx.QueryRowContext(ctx, `SELECT system_protected FROM roles WHERE id=? AND tenant_id=?`, roleID, tenantID).Scan(&protected); err != nil {
			return err
		}
		keepsAdmin = keepsAdmin || protected
	}
	if !keepsAdmin {
		if err = ensureNotLastTenantAdmin(ctx, tx, tenantID, userID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=?`, userID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,created_by)VALUES(?,?,?)`, userID, roleID, actor.User.ID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET updated_at=now(),version=version+1 WHERE id=?`, userID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.RecordAudit(ctx, AuditEvent{TenantID: tenantID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "user.roles.replace", ResourceType: "user", ResourceID: userID, Result: "success"})
	return nil
}

func (s *Service) ReplaceRoleUsers(ctx context.Context, actor Principal, tenantID, roleID string, userIDs []string, version int64) error {
	canManage := actor.Has("platform.users.manage") || (actor.Has("tenant.users.assign_roles") && actor.Has("tenant.roles.update"))
	if !canManage {
		return ErrPermissionDenied
	}
	if err := ensureActorTenantScope(actor, tenantID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var roleTenant string
	var protected bool
	var currentVersion int64
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id,system_protected,version FROM roles WHERE id=? FOR UPDATE`, roleID).Scan(&roleTenant, &protected, &currentVersion); err != nil {
		return err
	}
	if roleTenant != tenantID {
		return ErrTenantScope
	}
	if currentVersion != version {
		return ErrVersionConflict
	}
	if roleID == SystemRoleID {
		return ErrProtectedResource
	}
	if err = ensureRolesDelegable(ctx, tx, actor, tenantID, []string{roleID}); err != nil {
		return err
	}

	selected := make(map[string]struct{}, len(userIDs))
	activeSelected := 0
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			return fmt.Errorf("%w: user id is required", ErrValidation)
		}
		if userID == SystemUserID {
			return ErrProtectedResource
		}
		if _, exists := selected[userID]; exists {
			continue
		}
		var userTenant, status string
		if err = tx.QueryRowContext(ctx, `SELECT tenant_id,status FROM users WHERE id=?`, userID).Scan(&userTenant, &status); err != nil {
			return err
		}
		if userTenant != tenantID {
			return ErrTenantScope
		}
		selected[userID] = struct{}{}
		if status == "active" {
			activeSelected++
		}
	}
	if protected && activeSelected == 0 {
		return ErrProtectedResource
	}

	affected := make(map[string]struct{}, len(selected))
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM user_roles WHERE role_id=?`, roleID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var userID string
		if err = rows.Scan(&userID); err != nil {
			_ = rows.Close()
			return err
		}
		affected[userID] = struct{}{}
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for userID := range selected {
		affected[userID] = struct{}{}
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles WHERE role_id=?`, roleID); err != nil {
		return err
	}
	for userID := range selected {
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,created_by)VALUES(?,?,?)`, userID, roleID, actor.User.ID); err != nil {
			return err
		}
	}
	for userID := range affected {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET updated_at=now(),version=version+1 WHERE id=?`, userID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE roles SET updated_at=now(),version=version+1 WHERE id=?`, roleID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.RecordAudit(ctx, AuditEvent{TenantID: tenantID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "role.users.replace", ResourceType: "role", ResourceID: roleID, Result: "success"})
	return nil
}

func (s *Service) UpdateUserStatus(ctx context.Context, actor Principal, tenantID, userID, status string, version int64) (User, error) {
	if !actor.Has("tenant.users.update") && !actor.Has("platform.users.manage") {
		return User{}, ErrPermissionDenied
	}
	if userID == actor.User.ID || userID == SystemUserID {
		return User{}, ErrProtectedResource
	}
	if err := ensureActorTenantScope(actor, tenantID); err != nil {
		return User{}, err
	}
	if status != "active" && status != "disabled" && status != "locked" {
		return User{}, fmt.Errorf("%w: invalid user status", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if status != "active" {
		if err = ensureNotLastTenantAdmin(ctx, tx, tenantID, userID); err != nil {
			return User{}, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET status=?,updated_at=now(),version=version+1 WHERE id=? AND tenant_id=? AND version=?`, status, userID, tenantID, version)
	if err != nil {
		return User{}, err
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return User{}, ErrVersionConflict
	}
	if status != "active" {
		if _, err = tx.ExecContext(ctx, `UPDATE user_sessions SET revoked_at=now(),revoke_reason='account_status_changed' WHERE user_id=? AND revoked_at IS NULL`, userID); err != nil {
			return User{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	s.RecordAudit(ctx, AuditEvent{TenantID: tenantID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "user.status.update", ResourceType: "user", ResourceID: userID, Result: "success"})
	users, err := s.ListUsers(ctx, tenantID)
	if err != nil {
		return User{}, err
	}
	for _, user := range users {
		if user.ID == userID {
			return user, nil
		}
	}
	return User{}, sql.ErrNoRows
}

func (s *Service) DeleteUser(ctx context.Context, actor Principal, tenantID, userID string) error {
	if !actor.Has("tenant.users.delete") && !actor.Has("platform.users.manage") {
		return ErrPermissionDenied
	}
	if userID == actor.User.ID || userID == SystemUserID {
		return ErrProtectedResource
	}
	if err := ensureActorTenantScope(actor, tenantID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = ensureNotLastTenantAdmin(ctx, tx, tenantID, userID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=? AND tenant_id=?`, userID, tenantID)
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.RecordAudit(ctx, AuditEvent{TenantID: tenantID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "user.delete", ResourceType: "user", ResourceID: userID, Result: "success"})
	return nil
}

func (s *Service) DeleteRole(ctx context.Context, actor Principal, tenantID, roleID string) error {
	if !actor.Has("tenant.roles.delete") && !actor.Has("platform.roles.manage") {
		return ErrPermissionDenied
	}
	if err := ensureActorTenantScope(actor, tenantID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var protected bool
	if err = tx.QueryRowContext(ctx, `SELECT system_protected FROM roles WHERE id=? AND tenant_id=? FOR UPDATE`, roleID, tenantID).Scan(&protected); err != nil {
		return err
	}
	if protected {
		return ErrProtectedResource
	}
	var assigned int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles WHERE role_id=?`, roleID).Scan(&assigned); err != nil {
		return err
	}
	if assigned > 0 {
		return ErrProtectedResource
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM roles WHERE id=? AND tenant_id=?`, roleID, tenantID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.RecordAudit(ctx, AuditEvent{TenantID: tenantID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "role.delete", ResourceType: "role", ResourceID: roleID, Result: "success"})
	return nil
}

func (s *Service) DeleteTenant(ctx context.Context, actor Principal, tenantID string, version int64) (Tenant, error) {
	if !actor.Has("platform.tenants.update") {
		return Tenant{}, ErrPermissionDenied
	}
	if tenantID == SystemTenantID {
		return Tenant{}, ErrProtectedResource
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Tenant{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE tenants SET status='disabled',updated_at=now(),version=version+1 WHERE id=? AND version=?`, tenantID, version)
	if err != nil {
		return Tenant{}, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return Tenant{}, ErrVersionConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_sessions SET revoked_at=now(),revoke_reason='tenant_disabled' WHERE user_id IN (SELECT id FROM users WHERE tenant_id=?) AND revoked_at IS NULL`, tenantID); err != nil {
		return Tenant{}, err
	}
	if err = tx.Commit(); err != nil {
		return Tenant{}, err
	}
	s.RecordAudit(ctx, AuditEvent{TenantID: tenantID, ActorKind: actor.Kind, ActorUserID: actor.User.ID, ActorSessionID: actor.SessionID, Action: "tenant.disable", ResourceType: "tenant", ResourceID: tenantID, Result: "success"})
	return s.GetTenant(ctx, tenantID)
}

func ensureNotLastTenantAdmin(ctx context.Context, tx *sql.Tx, tenantID, userID string) error {
	var targetIsAdmin bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=? AND r.tenant_id=? AND r.system_protected=true)`, userID, tenantID).Scan(&targetIsAdmin); err != nil {
		return err
	}
	if !targetIsAdmin {
		return nil
	}
	var activeAdmins int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.tenant_id=? AND u.status='active' AND r.system_protected=true`, tenantID).Scan(&activeAdmins); err != nil {
		return err
	}
	if activeAdmins <= 1 {
		return ErrProtectedResource
	}
	return nil
}

func (s *Service) ValidateTenantAccess(ctx context.Context, tenantID string) error {
	_, err := s.TenantAccessDeadline(ctx, tenantID)
	return err
}

func (s *Service) TenantAccessDeadline(ctx context.Context, tenantID string) (*time.Time, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrTenantScope
	}
	tenant, err := s.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err = validateTenant(tenant, time.Now()); err != nil {
		return nil, err
	}
	if tenant.Type == "system" || tenant.ExpiresAt == nil {
		return nil, nil
	}
	deadline := tenant.ExpiresAt.UTC()
	return &deadline, nil
}
