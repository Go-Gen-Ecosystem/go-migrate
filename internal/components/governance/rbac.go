/**
 * @file rbac.go
 * @description Role-Based Access Control for migration governance
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/rbac.go
 */

package governance

import (
	"context"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// Permission represents a specific permission
type Permission string

const (
	PermissionMigrationExecute  Permission = "migration:execute"
	PermissionMigrationRollback Permission = "migration:rollback"
	PermissionMigrationApprove  Permission = "migration:approve"
	PermissionMigrationReject   Permission = "migration:reject"
	PermissionMigrationView     Permission = "migration:view"
	PermissionMigrationCreate   Permission = "migration:create"
	PermissionMigrationDelete   Permission = "migration:delete"
	PermissionPolicyManage      Permission = "policy:manage"
	PermissionRoleManage        Permission = "role:manage"
	PermissionAuditView         Permission = "audit:view"
)

// Role represents a user role with associated permissions
type Role struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
	Level       int          `json:"level"` // Higher level = more privileged
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// User represents a user in the system
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Active    bool      `json:"active"`
}

// RBACManager manages roles and permissions
type RBACManager struct {
	roles  map[string]*Role
	users  map[string]*User
	logger logging.ComponentLogger
}

// NewRBACManager creates a new RBAC manager
func NewRBACManager() *RBACManager {
	manager := &RBACManager{
		roles:  make(map[string]*Role),
		users:  make(map[string]*User),
		logger: logging.GetComponentLogger("rbac-manager"),
	}

	// Register default roles
	manager.registerDefaultRoles()

	return manager
}

// registerDefaultRoles registers default enterprise roles
func (m *RBACManager) registerDefaultRoles() {
	// Developer role - can create and view migrations
	m.AddRole(&Role{
		Name:        "developer",
		Description: "Can create and view migrations",
		Permissions: []Permission{
			PermissionMigrationCreate,
			PermissionMigrationView,
		},
		Level:     1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// DBA role - can execute, rollback, and approve migrations
	m.AddRole(&Role{
		Name:        "dba",
		Description: "Database Administrator - can execute and approve migrations",
		Permissions: []Permission{
			PermissionMigrationCreate,
			PermissionMigrationView,
			PermissionMigrationExecute,
			PermissionMigrationRollback,
			PermissionMigrationApprove,
			PermissionMigrationReject,
			PermissionAuditView,
		},
		Level:     3,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// Manager role - can approve/reject migrations
	m.AddRole(&Role{
		Name:        "manager",
		Description: "Can approve or reject migrations",
		Permissions: []Permission{
			PermissionMigrationView,
			PermissionMigrationApprove,
			PermissionMigrationReject,
			PermissionAuditView,
		},
		Level:     2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// Admin role - full permissions
	m.AddRole(&Role{
		Name:        "admin",
		Description: "System Administrator - full permissions",
		Permissions: []Permission{
			PermissionMigrationCreate,
			PermissionMigrationView,
			PermissionMigrationExecute,
			PermissionMigrationRollback,
			PermissionMigrationApprove,
			PermissionMigrationReject,
			PermissionMigrationDelete,
			PermissionPolicyManage,
			PermissionRoleManage,
			PermissionAuditView,
		},
		Level:     4,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// Auditor role - read-only access for compliance
	m.AddRole(&Role{
		Name:        "auditor",
		Description: "Can view migrations and audit logs for compliance",
		Permissions: []Permission{
			PermissionMigrationView,
			PermissionAuditView,
		},
		Level:     1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
}

// AddRole adds a new role
func (m *RBACManager) AddRole(role *Role) {
	m.roles[role.Name] = role
	m.logger.Debug("Role added", "role", role.Name, "permissions", len(role.Permissions))
}

// RemoveRole removes a role
func (m *RBACManager) RemoveRole(roleName string) error {
	if _, exists := m.roles[roleName]; !exists {
		return fmt.Errorf("role not found: %s", roleName)
	}
	delete(m.roles, roleName)
	return nil
}

// GetRole retrieves a role by name
func (m *RBACManager) GetRole(roleName string) (*Role, error) {
	role, exists := m.roles[roleName]
	if !exists {
		return nil, fmt.Errorf("role not found: %s", roleName)
	}
	return role, nil
}

// ListRoles lists all roles
func (m *RBACManager) ListRoles() []*Role {
	roles := make([]*Role, 0, len(m.roles))
	for _, role := range m.roles {
		roles = append(roles, role)
	}
	return roles
}

// AddUser adds a new user
func (m *RBACManager) AddUser(user *User) error {
	if _, exists := m.users[user.ID]; exists {
		return fmt.Errorf("user already exists: %s", user.ID)
	}

	// Validate roles
	for _, roleName := range user.Roles {
		if _, exists := m.roles[roleName]; !exists {
			return fmt.Errorf("invalid role: %s", roleName)
		}
	}

	m.users[user.ID] = user
	m.logger.Debug("User added", "user_id", user.ID, "roles", user.Roles)
	return nil
}

// RemoveUser removes a user
func (m *RBACManager) RemoveUser(userID string) error {
	if _, exists := m.users[userID]; !exists {
		return fmt.Errorf("user not found: %s", userID)
	}
	delete(m.users, userID)
	return nil
}

// GetUser retrieves a user by ID
func (m *RBACManager) GetUser(userID string) (*User, error) {
	user, exists := m.users[userID]
	if !exists {
		return nil, fmt.Errorf("user not found: %s", userID)
	}
	return user, nil
}

// AssignRole assigns a role to a user
func (m *RBACManager) AssignRole(userID string, roleName string) error {
	user, err := m.GetUser(userID)
	if err != nil {
		return err
	}

	if _, err := m.GetRole(roleName); err != nil {
		return err
	}

	// Check if already assigned
	for _, existingRole := range user.Roles {
		if existingRole == roleName {
			return fmt.Errorf("role %s already assigned to user %s", roleName, userID)
		}
	}

	user.Roles = append(user.Roles, roleName)
	user.UpdatedAt = time.Now()
	return nil
}

// RevokeRole revokes a role from a user
func (m *RBACManager) RevokeRole(userID string, roleName string) error {
	user, err := m.GetUser(userID)
	if err != nil {
		return err
	}

	newRoles := make([]string, 0, len(user.Roles))
	found := false
	for _, existingRole := range user.Roles {
		if existingRole != roleName {
			newRoles = append(newRoles, existingRole)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("role %s not assigned to user %s", roleName, userID)
	}

	user.Roles = newRoles
	user.UpdatedAt = time.Now()
	return nil
}

// HasPermission checks if a user has a specific permission
func (m *RBACManager) HasPermission(ctx context.Context, userID string, permission Permission) (bool, error) {
	user, err := m.GetUser(userID)
	if err != nil {
		return false, err
	}

	if !user.Active {
		return false, fmt.Errorf("user is not active: %s", userID)
	}

	// Check all user roles for the permission
	for _, roleName := range user.Roles {
		role, err := m.GetRole(roleName)
		if err != nil {
			m.logger.WithContext(ctx).Error(fmt.Sprintf("Failed to get role %s", roleName), err)
			continue
		}

		for _, perm := range role.Permissions {
			if perm == permission {
				return true, nil
			}
		}
	}

	return false, nil
}

// HasAnyPermission checks if a user has any of the specified permissions
func (m *RBACManager) HasAnyPermission(ctx context.Context, userID string, permissions []Permission) (bool, error) {
	for _, permission := range permissions {
		if hasPermission, err := m.HasPermission(ctx, userID, permission); err == nil && hasPermission {
			return true, nil
		}
	}
	return false, nil
}

// HasAllPermissions checks if a user has all of the specified permissions
func (m *RBACManager) HasAllPermissions(ctx context.Context, userID string, permissions []Permission) (bool, error) {
	for _, permission := range permissions {
		hasPermission, err := m.HasPermission(ctx, userID, permission)
		if err != nil {
			return false, err
		}
		if !hasPermission {
			return false, nil
		}
	}
	return true, nil
}

// GetUserPermissions returns all permissions for a user
func (m *RBACManager) GetUserPermissions(userID string) ([]Permission, error) {
	user, err := m.GetUser(userID)
	if err != nil {
		return nil, err
	}

	if !user.Active {
		return nil, fmt.Errorf("user is not active: %s", userID)
	}

	permissionSet := make(map[Permission]bool)
	for _, roleName := range user.Roles {
		role, err := m.GetRole(roleName)
		if err != nil {
			continue
		}

		for _, perm := range role.Permissions {
			permissionSet[perm] = true
		}
	}

	permissions := make([]Permission, 0, len(permissionSet))
	for perm := range permissionSet {
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

// GetHighestRoleLevel returns the highest role level for a user
func (m *RBACManager) GetHighestRoleLevel(userID string) (int, error) {
	user, err := m.GetUser(userID)
	if err != nil {
		return 0, err
	}

	maxLevel := 0
	for _, roleName := range user.Roles {
		role, err := m.GetRole(roleName)
		if err != nil {
			continue
		}

		if role.Level > maxLevel {
			maxLevel = role.Level
		}
	}

	return maxLevel, nil
}

// CanApprove checks if a user can approve migrations
func (m *RBACManager) CanApprove(ctx context.Context, userID string) (bool, error) {
	return m.HasPermission(ctx, userID, PermissionMigrationApprove)
}

// CanExecute checks if a user can execute migrations
func (m *RBACManager) CanExecute(ctx context.Context, userID string) (bool, error) {
	return m.HasPermission(ctx, userID, PermissionMigrationExecute)
}

// CanRollback checks if a user can rollback migrations
func (m *RBACManager) CanRollback(ctx context.Context, userID string) (bool, error) {
	return m.HasPermission(ctx, userID, PermissionMigrationRollback)
}

// RequirePermission checks if a user has a permission and returns an error if not
func (m *RBACManager) RequirePermission(ctx context.Context, userID string, permission Permission) error {
	hasPermission, err := m.HasPermission(ctx, userID, permission)
	if err != nil {
		return fmt.Errorf("failed to check permission: %w", err)
	}

	if !hasPermission {
		return fmt.Errorf("user %s does not have permission: %s", userID, permission)
	}

	return nil
}

// SeparationOfDuties enforces separation of duties
type SeparationOfDuties struct {
	manager *RBACManager
	logger  logging.ComponentLogger
}

// NewSeparationOfDuties creates a new separation of duties enforcer
func NewSeparationOfDuties(manager *RBACManager) *SeparationOfDuties {
	return &SeparationOfDuties{
		manager: manager,
		logger:  logging.GetComponentLogger("separation-of-duties"),
	}
}

// CanUserApproveOwnMigration checks if separation of duties allows self-approval
func (s *SeparationOfDuties) CanUserApproveOwnMigration(ctx context.Context, creatorID string, approverID string) (bool, error) {
	// Separation of duties: creator cannot approve their own migration
	if creatorID == approverID {
		s.logger.WithContext(ctx).Info(fmt.Sprintf("Separation of duties violation: user %s attempting to approve own migration", creatorID))
		return false, nil
	}

	return true, nil
}

// RequireDifferentApprovers ensures approvers are different from creator
func (s *SeparationOfDuties) RequireDifferentApprovers(ctx context.Context, creatorID string, approverID string) error {
	allowed, err := s.CanUserApproveOwnMigration(ctx, creatorID, approverID)
	if err != nil {
		return err
	}

	if !allowed {
		return fmt.Errorf("separation of duties violation: user %s cannot approve their own migration", creatorID)
	}

	return nil
}
