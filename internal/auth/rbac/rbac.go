package rbac

import (
	"fmt"
	"strings"
)

// Role represents an enterprise user or API role.
type Role string

const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleEngineer Role = "engineer"
	RoleAuditor  Role = "auditor"
	RoleReadOnly Role = "read_only"
)

// Permission defines a granular system capability.
type Permission string

const (
	PermOrgManage       Permission = "org:manage"
	PermBillingManage   Permission = "billing:manage"
	PermKMSManage       Permission = "kms:manage"
	PermAuditRead       Permission = "audit:read"
	PermKeysManageAll   Permission = "keys:manage:all"
	PermKeysManageOwn   Permission = "keys:manage:own"
	PermRoutingManage   Permission = "routing:manage"
	PermCatalogManage   Permission = "catalog:manage"
	PermModelInvoke     Permission = "model:invoke"
	PermConfigRead      Permission = "config:read"
	PermConfigWrite     Permission = "config:write"
	PermSpendView       Permission = "spend:view"
	PermMetricsView     Permission = "metrics:view"
	PermUsageViewOwn    Permission = "usage:view:own"
)

var rolePermissions = map[Role]map[Permission]bool{
	RoleOwner: {
		PermOrgManage:     true,
		PermBillingManage: true,
		PermKMSManage:     true,
		PermAuditRead:     true,
		PermKeysManageAll: true,
		PermKeysManageOwn: true,
		PermRoutingManage: true,
		PermCatalogManage: true,
		PermModelInvoke:   true,
		PermConfigRead:    true,
		PermConfigWrite:   true,
		PermSpendView:     true,
		PermMetricsView:   true,
		PermUsageViewOwn:  true,
	},
	RoleAdmin: {
		PermKeysManageAll: true,
		PermKeysManageOwn: true,
		PermRoutingManage: true,
		PermCatalogManage: true,
		PermModelInvoke:   true,
		PermConfigRead:    true,
		PermConfigWrite:   true,
		PermSpendView:     true,
		PermMetricsView:   true,
		PermAuditRead:     true,
		PermUsageViewOwn:  true,
	},
	RoleEngineer: {
		PermKeysManageOwn: true,
		PermModelInvoke:   true,
		PermUsageViewOwn:  true,
		PermConfigRead:    true,
	},
	RoleAuditor: {
		PermConfigRead:  true,
		PermSpendView:   true,
		PermMetricsView: true,
		PermAuditRead:   true,
	},
	RoleReadOnly: {
		PermConfigRead:  true,
		PermSpendView:   true,
		PermMetricsView: true,
		PermAuditRead:   true,
	},
}

// ParseRole normalizes role strings.
func ParseRole(roleStr string) (Role, error) {
	norm := strings.ToLower(strings.TrimSpace(roleStr))
	switch Role(norm) {
	case RoleOwner:
		return RoleOwner, nil
	case RoleAdmin:
		return RoleAdmin, nil
	case RoleEngineer:
		return RoleEngineer, nil
	case RoleAuditor, RoleReadOnly:
		return RoleAuditor, nil
	default:
		return "", fmt.Errorf("unknown role: %s", roleStr)
	}
}

// HasPermission checks if a role holds the specified permission.
func HasPermission(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[perm]
}

// CanInvokeModel checks whether the role is authorized to make model inference requests.
func CanInvokeModel(role Role) bool {
	return HasPermission(role, PermModelInvoke)
}

// CanManageKey checks whether the actor can manage a target virtual key.
func CanManageKey(actorRole Role, keyOwnerUserID, actorUserID string) bool {
	if HasPermission(actorRole, PermKeysManageAll) {
		return true
	}
	if HasPermission(actorRole, PermKeysManageOwn) && keyOwnerUserID != "" && keyOwnerUserID == actorUserID {
		return true
	}
	return false
}
